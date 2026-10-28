//go:build !windows

package processtree

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

const (
	detachedSessionHelperMode = "processtree-detached-session-helper"
	detachedSessionWriterMode = "processtree-detached-session-writer"
)

func testArgsAfterDoubleDash() []string {
	for i, arg := range os.Args {
		if arg == "--" && i+1 < len(os.Args) {
			return os.Args[i+1:]
		}
	}
	return nil
}

// TestDetachedSessionHelperProcess starts a child in a new session, then
// blocks. The parent cancels CombinedOutput and asserts that child never
// writes — the same shape as wt spawning a pre-start hook with setsid.
func TestDetachedSessionHelperProcess(t *testing.T) {
	args := testArgsAfterDoubleDash()
	if len(args) != 3 || args[0] != detachedSessionHelperMode {
		return
	}
	child := exec.Command(os.Args[0], "-test.run=^TestDetachedSessionWriterProcess$", "--", detachedSessionWriterMode, args[1])
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := child.Start(); err != nil {
		os.Exit(2)
	}
	if err := os.WriteFile(args[2], []byte("ready"), 0o600); err != nil {
		os.Exit(3)
	}
	time.Sleep(30 * time.Second)
}

func TestDetachedSessionWriterProcess(t *testing.T) {
	args := testArgsAfterDoubleDash()
	if len(args) != 2 || args[0] != detachedSessionWriterMode {
		return
	}
	time.Sleep(2 * time.Second)
	if err := os.WriteFile(args[1], []byte("leaked"), 0o600); err != nil {
		os.Exit(2)
	}
}

func TestKillTreeReapsSetsidChild(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	marker := filepath.Join(dir, "detached-survived")
	ready := filepath.Join(dir, "descendant-ready")
	cmd := exec.Command(os.Args[0],
		"-test.run=^TestDetachedSessionHelperProcess$",
		"--", detachedSessionHelperMode, marker, ready,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatal("helper did not start its detached descendant")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := KillTree(cmd.Process.Pid); err != nil {
		t.Fatalf("KillTree: %v", err)
	}
	_ = cmd.Wait()
	time.Sleep(2500 * time.Millisecond)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("setsid descendant survived KillTree, stat error = %v", err)
	}
}

func TestCombinedOutputKillsNewSessionDescendants(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	marker := filepath.Join(dir, "detached-survived")
	ready := filepath.Join(dir, "descendant-ready")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := time.Now()
	errCh := make(chan error, 1)
	go func() {
		_, err := CombinedOutput(ctx, exec.Command(os.Args[0],
			"-test.run=^TestDetachedSessionHelperProcess$",
			"--", detachedSessionHelperMode, marker, ready,
		), 250*time.Millisecond)
		errCh <- err
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			<-errCh
			t.Fatal("helper did not start its detached descendant")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	err := <-errCh
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("CombinedOutput error = %v, want context canceled", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("CombinedOutput took %s, want a hard bounded return", elapsed)
	}

	time.Sleep(2500 * time.Millisecond)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("setsid descendant survived process-tree cancellation, stat error = %v", err)
	}
}

func TestCombinedOutputKillsDescendantsHoldingOutput(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	marker := filepath.Join(dir, "descendant-survived")
	script := filepath.Join(dir, "helper.sh")
	body := "#!/bin/sh\n(sleep 0.5; echo leaked > \"$1\") &\nwait\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := CombinedOutput(ctx, exec.Command(script, marker), 250*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CombinedOutput error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("CombinedOutput took %s, want a hard bounded return", elapsed)
	}

	time.Sleep(600 * time.Millisecond)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("descendant survived process-tree cancellation, stat error = %v", err)
	}
}
