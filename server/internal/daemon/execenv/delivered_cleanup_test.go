package execenv

import (
	"path/filepath"
	"strings"
	"testing"
)

// A squash-merged task branch whose remote is gone is delivered; a branch
// with unmerged work, or one still on the remote, is not.
func TestBranchDeliveredSurvivesSquashMerge(t *testing.T) {
	repo := newTestRepo(t)
	origin := filepath.Join(t.TempDir(), "origin.git")
	gitRun(t, repo, "init", "--bare", "-q", origin)
	gitRun(t, repo, "remote", "add", "origin", origin)
	main := strings.TrimSpace(gitRun(t, repo, "rev-parse", "--abbrev-ref", "HEAD"))
	gitRun(t, repo, "push", "-q", "origin", main+":main")

	// agent/ox-1: two commits, pushed, then squash-merged on main and deleted.
	gitRun(t, repo, "checkout", "-q", "-b", "agent/ox-1")
	writeFile(t, filepath.Join(repo, "a.txt"), "one\n")
	gitRun(t, repo, "add", "a.txt")
	gitRun(t, repo, "commit", "-q", "-m", "a1")
	writeFile(t, filepath.Join(repo, "a.txt"), "one\ntwo\n")
	gitRun(t, repo, "commit", "-q", "-am", "a2")
	gitRun(t, repo, "push", "-q", "origin", "agent/ox-1")
	if branchDelivered(repo, "agent/ox-1") {
		t.Fatal("open on the remote: must not count as delivered")
	}
	gitRun(t, repo, "checkout", "-q", main)
	// another commit lands on main first, so the squash is not a fast-forward
	writeFile(t, filepath.Join(repo, "other.txt"), "x\n")
	gitRun(t, repo, "add", "other.txt")
	gitRun(t, repo, "commit", "-q", "-m", "other")
	gitRun(t, repo, "merge", "-q", "--squash", "agent/ox-1")
	gitRun(t, repo, "commit", "-q", "-m", "OX-1 squash")
	gitRun(t, repo, "push", "-q", "origin", main+":main", ":agent/ox-1")
	gitRun(t, repo, "fetch", "-q", "--prune", "origin")
	if !branchDelivered(repo, "agent/ox-1") {
		t.Fatal("squash-merged and gone from the remote: want delivered")
	}

	// agent/ox-2: local unique work, never merged.
	gitRun(t, repo, "checkout", "-q", "-b", "agent/ox-2")
	writeFile(t, filepath.Join(repo, "b.txt"), "unique\n")
	gitRun(t, repo, "add", "b.txt")
	gitRun(t, repo, "commit", "-q", "-m", "b")
	gitRun(t, repo, "checkout", "-q", main)
	if branchDelivered(repo, "agent/ox-2") {
		t.Fatal("unmerged unique work: must not count as delivered")
	}

	// Sweep: records for both; only the delivered one goes.
	for _, b := range []string{"agent/ox-1", "agent/ox-2"} {
		gitRun(t, repo, "update-ref", userStateRef(b), "refs/heads/"+b)
	}
	if n := reapDeliveredBranches(repo, worktreeTestLogger()); n != 1 {
		t.Fatalf("reaped %d, want 1", n)
	}
	if out := gitRun(t, repo, "branch", "--list", "agent/ox-1"); strings.TrimSpace(out) != "" {
		t.Fatal("delivered branch kept")
	}
	if out := gitRun(t, repo, "branch", "--list", "agent/ox-2"); strings.TrimSpace(out) == "" {
		t.Fatal("unmerged branch dropped")
	}
	if out := gitRun(t, repo, "for-each-ref", userStateRef("agent/ox-1")); strings.TrimSpace(out) != "" {
		t.Fatal("record of the delivered branch kept")
	}
}
