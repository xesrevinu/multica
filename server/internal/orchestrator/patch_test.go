package orchestrator

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestSamePatchRebase(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	g := func(args ...string) string {
		c := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
		c.Dir = dir
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, body string) {
		c := exec.Command("sh", "-c", "printf '%s' \"$1\" > \"$2\"", "sh", body, name)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
	}
	g("init", "-q", "-b", "main")
	write("a.txt", "one\n")
	g("add", ".")
	g("commit", "-qm", "base")
	g("checkout", "-qb", "agent/X")
	write("b.txt", "feature\n")
	g("add", ".")
	g("commit", "-qm", "feat")
	old := g("rev-parse", "HEAD")
	g("checkout", "-q", "main")
	write("c.txt", "main moved\n")
	g("add", ".")
	g("commit", "-qm", "main")
	g("checkout", "-q", "agent/X")
	g("rebase", "-q", "main")
	rebased := g("rev-parse", "HEAD")
	write("b.txt", "feature changed\n")
	g("commit", "-qam", "change")
	changed := g("rev-parse", "HEAD")

	r := &repoOps{}
	team := &Team{Spec: &Spec{Repo: RepoSpec{Mode: "local", Bare: dir, Main: "main"}}}
	ctx := context.Background()
	if old == rebased || !r.samePatch(ctx, team, old, rebased) {
		t.Fatal("pure rebase must keep the patch-id")
	}
	if r.samePatch(ctx, team, old, changed) {
		t.Fatal("changed content must differ")
	}
}
