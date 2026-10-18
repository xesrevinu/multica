package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A checkout with .config/wt.toml is created and removed through wt, so the
// directory is whichever path the wt on PATH chose and the branch stays after
// removal. The test supplies that wt: the real one would run this repo's hooks.
func TestWorktrunkWorktreeUsesWtForCreateAndRemove(t *testing.T) {
	repo := newTestRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".config", "wt.toml"), []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	logPath := filepath.Join(t.TempDir(), "wt.log")
	destRoot := filepath.Join(t.TempDir(), "worktrees")
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$WT_LOG"
gitroot=
base=HEAD
create=0
remove=0
prev=
last=
for arg in "$@"; do
  case "$prev" in
    -C) gitroot=$arg ;;
    --base) base=$arg ;;
  esac
  case "$arg" in
    --create) create=1 ;;
    remove) remove=1 ;;
  esac
  prev=$arg
  last=$arg
done
if [ "$remove" = 1 ]; then
  git -C "$gitroot" worktree remove --force "$last"
  exit 0
fi
san=$(printf '%s' "$last" | tr '/\\' '--')
dest="$WT_DEST_ROOT/$(basename "$gitroot")/$san"
mkdir -p "$(dirname "$dest")"
if [ "$create" = 1 ]; then
  git -C "$gitroot" worktree add -b "$last" "$dest" "$base"
else
  git -C "$gitroot" worktree add "$dest" "$last"
fi
`
	if err := os.WriteFile(filepath.Join(bin, "wt"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WT_LOG", logPath)
	t.Setenv("WT_DEST_ROOT", destRoot)

	head := gitRun(t, repo, "rev-parse", "HEAD")
	plan := taskBranchPlan{name: "agent/align-check", base: head}
	path, branch, created, err := addWorktrunkWorktree(repo, plan, "task-1", worktreeTestLogger())
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if branch != "agent/align-check" || !created {
		t.Fatalf("branch %q created %v, want agent/align-check created", branch, created)
	}
	wantDir := filepath.Join(destRoot, filepath.Base(repo), "agent-align-check")
	gotPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	wantPath, err := filepath.EvalSymlinks(wantDir)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != wantPath {
		t.Fatalf("path = %q, want %q", gotPath, wantPath)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "--create") || !strings.Contains(string(log), "--yes") || !strings.Contains(string(log), "agent/align-check") {
		t.Fatalf("wt was not asked to create the branch without a prompt:\n%s", log)
	}

	if err := removeLocalWorktreeDir(repo, path, worktreeTestLogger()); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
		t.Fatalf("worktree directory still present: %v", statErr)
	}
	if got := gitRun(t, repo, "branch", "--list", "agent/align-check"); got != "agent/align-check" && !strings.Contains(got, "agent/align-check") {
		t.Fatalf("branch was deleted with the worktree: %q", got)
	}
	log, err = os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "remove") || !strings.Contains(string(log), "--yes") || !strings.Contains(string(log), "--no-delete-branch") {
		t.Fatalf("wt remove did not keep the branch:\n%s", log)
	}
}
