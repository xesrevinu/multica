package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParseYieldedModelsUsesDeclaredSelectorAndThinking(t *testing.T) {
	models, err := parseYieldedModels([]byte(`{"models":[
		{"selector":"magpie/group/daily-work","provider":"magpie","id":"group/daily-work","name":"Daily","reasoning":true,"thinking":["high","low"]},
		{"selector":"magpie/group/misc-work","provider":"magpie","id":"group/misc-work","name":"Misc","reasoning":false,"thinking":null}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("models = %#v", models)
	}
	if models[0].ID != "magpie/group/daily-work" || models[0].Provider != "magpie" {
		t.Fatalf("first = %#v", models[0])
	}
	if models[0].Thinking == nil || len(models[0].Thinking.SupportedLevels) != 2 {
		t.Fatalf("thinking = %#v", models[0].Thinking)
	}
	if models[0].Thinking.SupportedLevels[0].Value != "low" || models[0].Thinking.SupportedLevels[1].Value != "high" {
		t.Fatalf("order = %#v", models[0].Thinking.SupportedLevels)
	}
	if models[1].Thinking != nil {
		t.Fatalf("non-reasoning model should have no picker, got %#v", models[1].Thinking)
	}
}

func TestYieldedExecuteSendsDeclaredModel(t *testing.T) {
	dir := t.TempDir()
	trace := filepath.Join(dir, "turn.json")
	argsPath := filepath.Join(dir, "args")
	bin := filepath.Join(dir, "yielded-agent")
	script := `#!/bin/sh
printf '%s\n' "$@" > "$YIELD_ARGS"
cat > "$YIELD_TRACE"
printf '%s\n' '{"type":"text","delta":"ok"}'
printf '%s\n' '{"type":"done","sessionId":"ses_yielded","text":"ok","stop":"end_turn"}'
`
	writeTestExecutable(t, bin, []byte(script))
	backend, err := New("yielded", Config{
		ExecutablePath: bin,
		Logger:         slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		Env:            map[string]string{"YIELD_TRACE": trace, "YIELD_ARGS": argsPath},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "ship it", ExecOptions{
		Cwd:           dir,
		Model:         "magpie/group/daily-work",
		ThinkingLevel: "high",
		Timeout:       20 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	result := <-session.Result
	if result.Status != "completed" || result.Output != "ok" || result.SessionID != "ses_yielded" {
		t.Fatalf("result = %#v", result)
	}
	body, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ship it" {
		t.Fatalf("prompt = %q", body)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(args)), "\n")
	for _, want := range []string{"--model", "magpie/group/daily-work", "--thinking", "high"} {
		if !slices.Contains(got, want) {
			t.Fatalf("argv missing %q:\n%s", want, got)
		}
	}
	for _, blocked := range []string{"-p", "--print", "--mode"} {
		if slices.Contains(got, blocked) {
			t.Fatalf("argv still contains %q:\n%s", blocked, got)
		}
	}
}

func TestDiscoverYieldedModelsUsesListModels(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	bin := filepath.Join(dir, "yielded-agent")
	script := `#!/bin/sh
printf '%s\n' "$@" > "$YIELD_ARGS"
printf '%s\n' '{"models":[{"selector":"magpie/group/daily-work","provider":"magpie","id":"group/daily-work","name":"Daily","reasoning":true,"thinking":["low"]}]}'
`
	writeTestExecutable(t, bin, []byte(script))
	t.Setenv("YIELD_ARGS", argsPath)
	models, err := discoverYieldedModels(context.Background(), NewCommand(bin, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "magpie/group/daily-work" {
		t.Fatalf("models = %#v", models)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(args)) != "--list-models" {
		t.Fatalf("argv = %q", args)
	}
}

func TestYieldedMissingSessionIsResumeRejected(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "yielded-agent")
	script := `#!/bin/sh
cat >/dev/null
printf '%s\n' '{"type":"error","phase":"startup","message":"session not found"}'
`
	writeTestExecutable(t, bin, []byte(script))
	backend, err := New("yielded", Config{
		ExecutablePath: bin,
		Logger:         slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "continue", ExecOptions{
		Cwd:             dir,
		ResumeSessionID: "missing-session",
		Timeout:         20 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	result := <-session.Result
	if result.Status != "failed" || result.Error != "session not found" || !result.ResumeRejected {
		t.Fatalf("result = %#v", result)
	}
	if result.SessionID != "" {
		t.Fatalf("SessionID = %q, want empty so the missing id is not recorded", result.SessionID)
	}
}

func TestYieldedStartupErrorWithoutResumeIsNotRejected(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "yielded-agent")
	script := `#!/bin/sh
cat >/dev/null
printf '%s\n' '{"type":"error","phase":"startup","message":"session not found"}'
`
	writeTestExecutable(t, bin, []byte(script))
	backend, err := New("yielded", Config{
		ExecutablePath: bin,
		Logger:         slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "continue", ExecOptions{Cwd: dir, Timeout: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	result := <-session.Result
	if result.ResumeRejected {
		t.Fatalf("fresh run must not set ResumeRejected, result = %#v", result)
	}
}
