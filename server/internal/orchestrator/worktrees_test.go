package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseShortstat(t *testing.T) {
	f, a, d := parseShortstat(" 3 files changed, 12 insertions(+), 4 deletions(-)\n")
	if f != 3 || a != 12 || d != 4 {
		t.Fatalf("%d %d %d", f, a, d)
	}
	if f, a, d = parseShortstat(" 1 file changed, 2 deletions(-)"); f != 1 || a != 0 || d != 2 {
		t.Fatalf("%d %d %d", f, a, d)
	}
	if f, _, _ = parseShortstat(""); f != 0 {
		t.Fatal("empty")
	}
}

func TestParseWorktreeListFromGit(t *testing.T) {
	ctx := context.Background()
	hub := t.TempDir()
	must := func(dir string, args ...string) {
		if r := run(ctx, dir, "git", args...); r.code != 0 {
			t.Fatalf("git %v: %s", args, r.err)
		}
	}
	must(hub, "init", "-q", "-b", "main")
	must(hub, "-c", "user.email=a@b", "-c", "user.name=a", "commit", "-q", "--allow-empty", "-m", "init")
	wt := filepath.Join(t.TempDir(), "att-1")
	must(hub, "worktree", "add", "-q", "-b", "agent/att-1", wt)
	if err := os.WriteFile(filepath.Join(wt, "x.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := run(ctx, hub, "git", "worktree", "list", "--porcelain")
	es := parseWorktreeList(res.out)
	if len(es) != 2 || es[1].branch != "agent/att-1" || es[1].head == "" {
		t.Fatalf("entries %+v", es)
	}
	ev, _ := filepath.EvalSymlinks(wt)
	got, _ := filepath.EvalSymlinks(es[1].path)
	if got != ev {
		t.Fatalf("path %s want %s", got, ev)
	}
}
