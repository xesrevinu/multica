package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildWebEnvOverridesPortAndRemoteAPIURL(t *testing.T) {
	t.Setenv("PORT", "8080")
	t.Setenv("FRONTEND_PORT", "")
	t.Setenv("WEB_PORT", "")
	t.Setenv("REMOTE_API_URL", "")

	env := buildWebEnv([]string{"PORT=8080", "FOO=bar"})
	joined := "\n" + strings.Join(env, "\n") + "\n"
	if !strings.Contains(joined, "\nPORT=3000\n") {
		t.Fatalf("expected PORT=3000 in web env, got:\n%s", joined)
	}
	if strings.Contains(joined, "\nPORT=8080\n") {
		t.Fatalf("expected PORT=8080 to be stripped from web env, got:\n%s", joined)
	}
	if !strings.Contains(joined, "\nREMOTE_API_URL=http://127.0.0.1:8080\n") {
		t.Fatalf("expected REMOTE_API_URL=http://127.0.0.1:8080 in web env, got:\n%s", joined)
	}
}

func TestEnsureSQLiteParentDirCreatesDirectory(t *testing.T) {
	tmp := t.TempDir()
	dbFile := filepath.Join(tmp, "nested", "db", "multica.db")
	if err := ensureSQLiteParentDir("file:" + dbFile); err != nil {
		t.Fatalf("ensureSQLiteParentDir failed: %v", err)
	}
	if st, err := os.Stat(filepath.Dir(dbFile)); err != nil || !st.IsDir() {
		t.Fatalf("expected parent dir to exist, stat err=%v", err)
	}
}
