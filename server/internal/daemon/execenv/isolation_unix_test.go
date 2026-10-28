//go:build !windows

package execenv

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	unixDetachedPrepareHelperMode = "unix-detached-prepare-helper"
	unixDetachedPrepareWriterMode = "unix-detached-prepare-writer"
)

func unixTestArgsAfterDoubleDash() []string {
	for i, arg := range os.Args {
		if arg == "--" && i+1 < len(os.Args) {
			return os.Args[i+1:]
		}
	}
	return nil
}

// TestUnixDetachedPrepareHelperProcess is the production failure shape: a
// prepare helper that starts a hook in a new session, then blocks. Killing
// only the helper PGID left that hook (cp of node_modules) as PPID 1.
func TestUnixDetachedPrepareHelperProcess(t *testing.T) {
	args := unixTestArgsAfterDoubleDash()
	if len(args) != 3 || args[0] != unixDetachedPrepareHelperMode {
		return
	}
	var request preparationRequest
	if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
		os.Exit(2)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestUnixDetachedPrepareWriterProcess$", "--", unixDetachedPrepareWriterMode, args[1])
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := child.Start(); err != nil {
		os.Exit(3)
	}
	if err := os.WriteFile(args[2], []byte("ready"), 0o600); err != nil {
		os.Exit(4)
	}
	for {
		time.Sleep(time.Hour)
	}
}

func TestUnixDetachedPrepareWriterProcess(t *testing.T) {
	args := unixTestArgsAfterDoubleDash()
	if len(args) != 2 || args[0] != unixDetachedPrepareWriterMode {
		return
	}
	time.Sleep(2 * time.Second)
	if err := os.WriteFile(args[1], []byte("leaked"), 0o600); err != nil {
		os.Exit(2)
	}
}

func TestPrepareIsolated_KillsNewSessionDescendant(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "old-attempt-marker")
	ready := filepath.Join(dir, "descendant-ready")
	command := []string{
		os.Args[0],
		"-test.run=^TestUnixDetachedPrepareHelperProcess$",
		"--",
		unixDetachedPrepareHelperMode,
		marker,
		ready,
	}
	params := PrepareParams{
		WorkspacesRoot:  filepath.Join(dir, "workspaces"),
		WorkspaceID:     "a05b0e10-ee7a-4603-a72d-a548b2390cb2",
		WorkspaceSlug:   strings.Repeat("workspace", 20),
		TaskID:          "12345678-1111-7222-8333-444444444444",
		IssueIdentifier: strings.Repeat("issue", 20),
		Provider:        "claude",
		Task:            TaskContextForEnv{IssueID: "issue-unix-tree"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := PrepareIsolated(ctx, command, params, nil)
		result <- err
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			<-result
			t.Fatal("helper did not start its detached descendant")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("PrepareIsolated error = %v, want context canceled", err)
	}

	time.Sleep(2500 * time.Millisecond)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("timed-out helper descendant survived and wrote marker: %v", err)
	}
}

// TestPrepareIsolated_PermanentFIFOBlockThenImmediateRetry is the lifecycle
// regression for MUL-4923. The first real Codex Prepare blocks forever opening
// a shared config FIFO. After the deadline, there must be no reader left on
// that FIFO before an immediate retry mutates the same env root.
func TestPrepareIsolated_PermanentFIFOBlockThenImmediateRetry(t *testing.T) {
	sharedCodexHome := t.TempDir()
	t.Setenv("CODEX_HOME", sharedCodexHome)
	fifo := filepath.Join(sharedCodexHome, "config.json")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("create config FIFO: %v", err)
	}

	params := PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "ws-isolated-prepare",
		TaskID:         "11111111-2222-3333-4444-555555555555",
		AgentName:      "isolation-test",
		Provider:       "codex",
		Task: TaskContextForEnv{
			IssueID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
			AgentID: "ffffffff-1111-2222-3333-444444444444",
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	startedAt := time.Now()
	_, err := PrepareIsolated(ctx, preparationHelperTestCommand(), params, logger)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("PrepareIsolated error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 3*time.Second {
		t.Fatalf("PrepareIsolated took %s, want a bounded process termination", elapsed)
	}
	if _, err := os.Stat(PredictRootDir(RootDirParams{WorkspacesRoot: params.WorkspacesRoot, WorkspaceID: params.WorkspaceID, WorkspaceSlug: params.WorkspaceSlug, TaskID: params.TaskID, IssueIdentifier: params.IssueIdentifier})); err != nil {
		t.Fatalf("first helper did not reach environment preparation before timeout: %v", err)
	}

	// A non-blocking FIFO writer succeeds only while a reader is still alive.
	// ENXIO therefore proves the timed-out helper was reaped before ownership
	// returned to the caller; there is no old worker that can resume later.
	writer, writerErr := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0)
	if writerErr == nil {
		writer.Close()
		t.Fatal("timed-out preparation helper still has the FIFO open for reading")
	}
	if !errors.Is(writerErr, syscall.ENXIO) {
		t.Fatalf("probe FIFO reader: %v, want ENXIO", writerErr)
	}

	// Retry immediately against the same root. It must be the sole writer and
	// complete successfully after the pathological source is replaced.
	if err := os.Remove(fifo); err != nil {
		t.Fatalf("remove config FIFO: %v", err)
	}
	if err := os.WriteFile(fifo, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("replace config FIFO: %v", err)
	}
	retryCtx, retryCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer retryCancel()
	env, err := PrepareIsolated(retryCtx, preparationHelperTestCommand(), params, logger)
	if err != nil {
		t.Fatalf("immediate retry PrepareIsolated: %v", err)
	}
	if env == nil || env.RootDir != PredictRootDir(RootDirParams{WorkspacesRoot: params.WorkspacesRoot, WorkspaceID: params.WorkspaceID, WorkspaceSlug: params.WorkspaceSlug, TaskID: params.TaskID, IssueIdentifier: params.IssueIdentifier}) {
		t.Fatalf("retry environment = %#v, want the predicted root", env)
	}
}
