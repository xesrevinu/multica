package execenv

import (
	"os"
	"path/filepath"
	"testing"
)

func strp(s string) *string { return &s }

func TestApplyDirenvVarsKeepsDaemonOwnedKeys(t *testing.T) {
	env := map[string]string{"PATH": "/mbin:/usr/bin", "MULTICA_TOKEN": "keep", "HOME": "/h"}
	applyDirenvVars(env, map[string]*string{
		"PATH":          strp("/nix/cargo/bin:/usr/bin:/mbin"),
		"MULTICA_TOKEN": strp("evil"),
		"HOME":          strp("/elsewhere"),
		"CARGO_HOME":    strp("/c"),
		"GONE":          nil,
	}, "/mbin")
	if env["PATH"] != "/mbin:/nix/cargo/bin:/usr/bin" {
		t.Fatalf("PATH = %q", env["PATH"])
	}
	if env["MULTICA_TOKEN"] != "keep" || env["HOME"] != "/h" {
		t.Fatalf("protected keys overridden: %v", env)
	}
	if env["CARGO_HOME"] != "/c" {
		t.Fatalf("CARGO_HOME not applied")
	}
	if _, ok := env["GONE"]; ok {
		t.Fatalf("unset var must not be added")
	}
}

func TestLoadDirenvEnvNoEnvrcIsNoop(t *testing.T) {
	env := map[string]string{"PATH": "/x"}
	if LoadDirenvEnv(t.TempDir(), env, "", nil) || env["PATH"] != "/x" {
		t.Fatalf("expected no-op")
	}
}

func TestLoadDirenvEnvFallsBackOnFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".envrc"), []byte("export FOO=bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := t.TempDir()
	// A direnv that fails like a blocked .envrc: error on stderr, no stdout.
	script := "#!/bin/sh\necho 'direnv: error .envrc is blocked' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(fake, "direnv"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fake)
	env := map[string]string{"PATH": "/x"}
	if LoadDirenvEnv(dir, env, "", nil) || env["PATH"] != "/x" {
		t.Fatalf("expected fallback, got %v", env)
	}
}

func TestLoadDirenvEnvAppliesExport(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".envrc"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := t.TempDir()
	script := "#!/bin/sh\necho '{\"FOO\":\"bar\",\"PATH\":\"/tool/bin:/usr/bin\"}'\n"
	if err := os.WriteFile(filepath.Join(fake, "direnv"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fake)
	env := map[string]string{"PATH": "/mbin"}
	if !LoadDirenvEnv(dir, env, "/mbin", nil) {
		t.Fatal("expected load")
	}
	if env["FOO"] != "bar" || env["PATH"] != "/mbin:/tool/bin:/usr/bin" {
		t.Fatalf("env = %v", env)
	}
}
