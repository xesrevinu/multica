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
	unknown := gbotResult([]byte(`{"status":"unknown","sessionId":"m1","reply":"","exitCode":1}`), nil, nil, time.Second)
	if unknown.Status != "failed" || unknown.Error == "" {
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
	if LaunchHeader("gbot") != "gbot task run" {
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
		"if [ \"$1\" = bots ]; then\n" + botsGet + "\nfi\n" +
		"echo '{\"status\":\"completed\",\"sessionId\":\"m1\",\"reply\":\"ok\",\"exitCode\":0}'\n"
	path := filepath.Join(dir, "gbot")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, argsLog
}

func runFakeGbot(t *testing.T, exe string, env map[string]string) (*Result, error) {
	t.Helper()
	backend, err := New("gbot", Config{ExecutablePath: exe, TaskID: "task-1", Env: env})
	if err != nil {
		t.Fatal(err)
	}
	session, err := backend.Execute(context.Background(), "do the thing", ExecOptions{WorkspaceID: "ws-1", Cwd: t.TempDir(), Timeout: 20 * time.Second})
	if err != nil {
		return nil, err
	}
	for range session.Messages {
	}
	result := <-session.Result
	return &result, nil
}

func TestGbotRequiresBotName(t *testing.T) {
	exe, _ := writeFakeGbot(t, `echo '{"id":"b1","kind":"bot"}'; exit 0`)
	_, err := runFakeGbot(t, exe, map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "names no bot") {
		t.Fatalf("err = %v, want a missing bot name error (no silent fallback)", err)
	}
}

func TestGbotFailsWhenBotNotFound(t *testing.T) {
	exe, argsLog := writeFakeGbot(t, `echo 'No bot or group named' >&2; exit 1`)
	_, err := runFakeGbot(t, exe, map[string]string{"MULTICA_GBOT_BOT": "Missing Bot"})
	if err == nil || !strings.Contains(err.Error(), `"Missing Bot" not found`) {
		t.Fatalf("err = %v", err)
	}
	logged, _ := os.ReadFile(argsLog)
	if strings.Contains(string(logged), "task run") {
		t.Fatalf("task was submitted to a missing bot: %s", logged)
	}
}

func TestGbotUsesTaskEnvAndArtifactRoot(t *testing.T) {
	exe, argsLog := writeFakeGbot(t, `echo '{"id":"b1","kind":"bot"}'; exit 0`)
	root := t.TempDir()
	result, err := runFakeGbot(t, exe, map[string]string{
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
	want := filepath.Join(root, "multica", "gbot", "ws-1", "heavy-rust-master", "ox-7.session.json")
	for _, part := range []string{"bots get Heavy Rust Master --json", "--bot Heavy Rust Master", "--issue OX-7", "--session-file " + want, "do the thing"} {
		if !strings.Contains(text, part) {
			t.Fatalf("args missing %q:\n%s", part, text)
		}
	}
}

func TestGbotSessionFileWithoutIssueIsPerTask(t *testing.T) {
	got := gbotSessionFile(map[string]string{GbotArtifactRootEnv: "/r"}, "WS", "Grok Bot", "", "t9")
	if got != filepath.Join("/r", "multica", "gbot", "ws", "grok-bot", "task-t9.session.json") {
		t.Fatalf("got %s", got)
	}
}

func TestGbotAttachTaskIDReusesAcceptedInFlight(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ox-7.session.json")
	body := `{"version":1,"taskId":"old-task","workspaceId":"ws-1","bot":"Heavy Rust Master","issue":"OX-7","delivery":"accepted"}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := gbotAttachTaskID(path, "ws-1", "Heavy Rust Master", "OX-7", "new-task")
	if got != "old-task" {
		t.Fatalf("got %q, want old-task (reattach)", got)
	}
}

func TestGbotAttachTaskIDLeavesTerminalAlone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ox-7.session.json")
	body := `{"version":1,"taskId":"old-task","workspaceId":"ws-1","bot":"Heavy Rust Master","issue":"OX-7","delivery":"accepted","status":"completed"}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := gbotAttachTaskID(path, "ws-1", "Heavy Rust Master", "OX-7", "new-task")
	if got != "new-task" {
		t.Fatalf("got %q, want new-task after terminal status", got)
	}
}

func TestGbotExecuteReattachesAcceptedSession(t *testing.T) {
	exe, argsLog := writeFakeGbot(t, `echo '{"id":"b1","kind":"bot"}'; exit 0`)
	root := t.TempDir()
	session := filepath.Join(root, "multica", "gbot", "ws-1", "heavy-rust-master", "ox-7.session.json")
	if err := os.MkdirAll(filepath.Dir(session), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"version":1,"taskId":"old-task","workspaceId":"ws-1","bot":"Heavy Rust Master","issue":"OX-7","delivery":"accepted"}`
	if err := os.WriteFile(session, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := runFakeGbot(t, exe, map[string]string{
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
	if !strings.Contains(string(logged), "--task-id old-task") {
		t.Fatalf("expected reattach of old-task, args:\n%s", logged)
	}
	if strings.Contains(string(logged), "--task-id task-1") {
		t.Fatalf("minted a new task id into a busy thread:\n%s", logged)
	}
}
