package orchestrator

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The hub (local main checkout) fast-forwards to origin/main before a run and
// is left alone when the user has local edits.
func TestRefreshHub(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	g := func(dir string, args ...string) string {
		c := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
		c.Dir = dir
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	origin, hub, other := filepath.Join(root, "origin.git"), filepath.Join(root, "hub"), filepath.Join(root, "other")
	g(root, "init", "-q", "--bare", "-b", "main", origin)
	g(root, "clone", "-q", origin, other)
	g(other, "commit", "-q", "--allow-empty", "-m", "base")
	g(other, "push", "-q", "origin", "HEAD:main")
	g(root, "clone", "-q", origin, hub)
	g(other, "commit", "-q", "--allow-empty", "-m", "merged PR")
	g(other, "push", "-q", "origin", "HEAD:main")
	want := g(other, "rev-parse", "HEAD")

	r := &repoOps{}
	sp := &Spec{Repo: RepoSpec{Hub: hub, Main: "main"}}
	r.refreshHub(context.Background(), sp, true)
	if got := g(hub, "rev-parse", "HEAD"); got != want {
		t.Fatalf("hub not fast-forwarded: %s want %s", got, want)
	}

	// dirty hub: fetched, not moved
	g(other, "commit", "-q", "--allow-empty", "-m", "next")
	g(other, "push", "-q", "origin", "HEAD:main")
	c := exec.Command("sh", "-c", "echo x > f && git add f")
	c.Dir = hub
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	r.refreshHub(context.Background(), sp, true)
	if got := g(hub, "rev-parse", "HEAD"); got != want {
		t.Fatalf("dirty hub must not move: %s", got)
	}
}
