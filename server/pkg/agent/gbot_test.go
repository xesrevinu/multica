package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGbotResultMapsTerminalStates(t *testing.T) {
	completed := gbotResult([]byte(`{"status":"completed","sessionId":"m1","reply":"done","exitCode":0}`), nil, nil, time.Second)
	if completed.Status != "completed" || completed.SessionID != "m1" || completed.Output != "done" {
		t.Fatalf("completed = %#v", completed)
	}
	unknown := gbotResult([]byte(`{"status":"unknown","sessionId":"m1","reply":"","exitCode":1,"failureKind":"unknown_delivery"}`), nil, nil, time.Second)
	if unknown.Status != "failed" || !strings.Contains(unknown.Error, "unknown_delivery") {
		t.Fatalf("unknown = %#v", unknown)
	}
	missing := gbotResult([]byte("not-json"), nil, nil, time.Second)
	if missing.Status != "failed" || !strings.Contains(missing.Error, "gbot returned no task result") || !strings.Contains(missing.Error, "not-json") {
		t.Fatalf("missing = %#v", missing)
	}
	// Prefixed junk before the result document must not collapse a completed
	// marker into a bare process failure (OX-220: work done, exit status 1).
	prefixed := gbotResult(
		[]byte("starting…\n{\"status\":\"completed\",\"sessionId\":\"m2\",\"reply\":\"shipped\",\"exitCode\":0}\n"),
		nil, fmt.Errorf("exit status 1"), time.Second,
	)
	if prefixed.Status != "completed" || prefixed.SessionID != "m2" || prefixed.Output != "shipped" {
		t.Fatalf("prefixed completed = %#v", prefixed)
	}
	opaque := gbotResult(nil, []byte("Timed out before a terminal task marker.\n"), fmt.Errorf("exit status 1"), time.Second)
	if opaque.Status != "failed" || !strings.Contains(opaque.Error, "exit status 1") || !strings.Contains(opaque.Error, "Timed out") {
		t.Fatalf("opaque = %#v", opaque)
	}
}

func TestGbotIsADistinctRuntime(t *testing.T) {
	if !IsSupportedType("gbot") {
		t.Fatal("gbot is not a supported runtime")
	}
	if LaunchHeader("gbot") != "gbot bot task run" {
		t.Fatalf("header = %q", LaunchHeader("gbot"))
	}
	backend, err := New("gbot", Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := backend.(*gbotBackend); !ok {
		t.Fatalf("backend = %T", backend)
	}
}

func writeFakeGbot(t *testing.T, botsGet string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	argsLog := filepath.Join(dir, "args.log")
	script := "#!/bin/sh\necho \"$@\" >> " + argsLog + "\n" +
		"if [ \"$1\" = bot ] && [ \"$2\" = get ]; then\n" + botsGet + "\nfi\n" +
		"echo '{\"type\":\"heartbeat\",\"taskId\":\"task-1\"}' >&2\n" +
		"echo '{\"status\":\"completed\",\"sessionId\":\"m1\",\"reply\":\"ok\",\"exitCode\":0}'\n"
	path := filepath.Join(dir, "gbot")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, argsLog
}

func runFakeGbot(t *testing.T, exe string, env map[string]string) (*Result, []Message, error) {
	t.Helper()
	backend, err := New("gbot", Config{ExecutablePath: exe, TaskID: "task-1", Env: env})
	if err != nil {
		t.Fatal(err)
	}
	session, err := backend.Execute(context.Background(), "do the thing", ExecOptions{WorkspaceID: "ws-1", Cwd: t.TempDir(), Timeout: 20 * time.Second})
	if err != nil {
		return nil, nil, err
	}
	var msgs []Message
	for msg := range session.Messages {
		msgs = append(msgs, msg)
	}
	result := <-session.Result
	return &result, msgs, nil
}

func TestGbotRequiresBotName(t *testing.T) {
	exe, _ := writeFakeGbot(t, `echo '{"id":"b1","kind":"bot"}'; exit 0`)
	_, _, err := runFakeGbot(t, exe, map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "names no bot") {
		t.Fatalf("err = %v, want a missing bot name error (no silent fallback)", err)
	}
}

func TestGbotFailsWhenBotNotFound(t *testing.T) {
	exe, argsLog := writeFakeGbot(t, `echo 'No bot or group named' >&2; exit 1`)
	_, _, err := runFakeGbot(t, exe, map[string]string{"MULTICA_GBOT_BOT": "Missing Bot"})
	if err == nil || !strings.Contains(err.Error(), `"Missing Bot" not found`) {
		t.Fatalf("err = %v", err)
	}
	logged, _ := os.ReadFile(argsLog)
	if strings.Contains(string(logged), "bot task run") {
		t.Fatalf("task was submitted to a missing bot: %s", logged)
	}
}

func TestGbotPassesScopeWithoutSessionFile(t *testing.T) {
	exe, argsLog := writeFakeGbot(t, `echo '{"id":"b1","kind":"bot"}'; exit 0`)
	root := t.TempDir()
	result, msgs, err := runFakeGbot(t, exe, map[string]string{
		"MULTICA_GBOT_BOT":         "Heavy Rust Master",
		"MULTICA_ISSUE_IDENTIFIER": "OX-7",
		GbotArtifactRootEnv:        root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" {
		t.Fatalf("result = %#v", result)
	}
	logged, _ := os.ReadFile(argsLog)
	text := string(logged)
	for _, part := range []string{
		"bot get Heavy Rust Master --json",
		"--bot Heavy Rust Master",
		"--issue OX-7",
		"--task-id task-1",
		"--workspace-id ws-1",
		"do the thing",
	} {
		if !strings.Contains(text, part) {
			t.Fatalf("args missing %q:\n%s", part, text)
		}
	}
	if strings.Contains(text, "--session-file") {
		t.Fatalf("Multica must let gbot own the default session path, args:\n%s", text)
	}
	sawHeartbeat := false
	for _, msg := range msgs {
		if msg.Type == MessageStatus && strings.Contains(msg.Content, "heartbeat") {
			sawHeartbeat = true
			break
		}
	}
	if !sawHeartbeat {
		t.Fatalf("expected a heartbeat status message, got %#v", msgs)
	}
}

func TestGbotAlwaysPassesCurrentTaskID(t *testing.T) {
	// Scope reattach lives in gbot now: Multica must not rewrite --task-id
	// from an on-disk session (that duplicated the CLI state machine).
	exe, argsLog := writeFakeGbot(t, `echo '{"id":"b1","kind":"bot"}'; exit 0`)
	root := t.TempDir()
	result, _, err := runFakeGbot(t, exe, map[string]string{
		"MULTICA_GBOT_BOT":         "Heavy Rust Master",
		"MULTICA_ISSUE_IDENTIFIER": "OX-7",
		GbotArtifactRootEnv:        root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" {
		t.Fatalf("result = %#v", result)
	}
	logged, _ := os.ReadFile(argsLog)
	if !strings.Contains(string(logged), "--task-id task-1") {
		t.Fatalf("expected current Multica task id, args:\n%s", logged)
	}
}

func TestGbotZeroTimeoutImposesNoWallDeadline(t *testing.T) {
	// FX-179 regression: when ExecOptions.Timeout is unset/0, gbot must not
	// invent a 30m wall clock. runContext already treats 0 as no deadline;
	// Execute must pass opts.Timeout through unchanged (same as other backends).
	opts := ExecOptions{Timeout: 0}
	ctx, cancel := runContext(context.Background(), opts.Timeout)
	defer cancel()
	if deadline, ok := ctx.Deadline(); ok {
		t.Fatalf("Timeout=0 set a wall deadline %v; want none so long Multica gbot runs are not SIGKILLed", deadline)
	}

	exe, _ := writeFakeGbot(t, `echo '{"id":"b1","kind":"bot"}'; exit 0`)
	backend, err := New("gbot", Config{
		ExecutablePath: exe,
		TaskID:         "task-zero-timeout",
		Env:            map[string]string{"MULTICA_GBOT_BOT": "Grok Bot"},
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := backend.Execute(context.Background(), "ping", ExecOptions{
		WorkspaceID: "ws-1",
		Cwd:         t.TempDir(),
		Timeout:     0,
	})
	if err != nil {
		t.Fatal(err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("result = %#v", result)
	}
}
