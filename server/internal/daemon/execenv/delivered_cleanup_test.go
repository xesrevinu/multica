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

func TestBranchDeliveredSurvivesBinaryAndContextShiftSquash(t *testing.T) {
	repo := newTestRepo(t)
	origin := filepath.Join(t.TempDir(), "origin.git")
	gitRun(t, repo, "init", "--bare", "-q", origin)
	gitRun(t, repo, "remote", "add", "origin", origin)
	main := strings.TrimSpace(gitRun(t, repo, "rev-parse", "--abbrev-ref", "HEAD"))
	writeFile(t, filepath.Join(repo, "shared.txt"), "line1\nline2\nline3\nline4\n")
	gitRun(t, repo, "add", "shared.txt")
	gitRun(t, repo, "commit", "-q", "-m", "seed shared")
	gitRun(t, repo, "push", "-q", "origin", main+":main")

	// 1. agent/ox-177: adds a binary file, squash-merged on main.
	gitRun(t, repo, "checkout", "-q", "-b", "agent/ox-177")
	writeFile(t, filepath.Join(repo, "asset.bin"), "PK\x00\x01\x02\xffbinary-fixture\x00")
	gitRun(t, repo, "add", "asset.bin")
	gitRun(t, repo, "commit", "-q", "-m", "add binary asset")
	gitRun(t, repo, "push", "-q", "origin", "agent/ox-177")

	// 2. agent/ox-129: modifies line4 of shared.txt while an intermediate commit on
	// main modifies line1 (changing the 3-line unified diff context around line4).
	gitRun(t, repo, "checkout", "-q", main)
	gitRun(t, repo, "checkout", "-q", "-b", "agent/ox-129")
	writeFile(t, filepath.Join(repo, "shared.txt"), "line1\nline2\nline3\nline4-ox129\n")
	gitRun(t, repo, "commit", "-q", "-am", "ox-129 change")
	gitRun(t, repo, "push", "-q", "origin", "agent/ox-129")

	// Main advances on line1 first, then squash-merges both branches and deletes remotes.
	gitRun(t, repo, "checkout", "-q", main)
	writeFile(t, filepath.Join(repo, "shared.txt"), "line1-main\nline2\nline3\nline4\n")
	gitRun(t, repo, "commit", "-q", "-am", "main context shift")

	gitRun(t, repo, "merge", "-q", "--squash", "agent/ox-177")
	gitRun(t, repo, "commit", "-q", "-m", "feat: binary asset (OX-177)")

	gitRun(t, repo, "merge", "-q", "--squash", "agent/ox-129")
	gitRun(t, repo, "commit", "-q", "-m", "feat: context shift (OX-129)")

	gitRun(t, repo, "push", "-q", "origin", main+":main", ":agent/ox-177", ":agent/ox-129")
	gitRun(t, repo, "fetch", "-q", "--prune", "origin")

	if !branchDelivered(repo, "agent/ox-177") {
		t.Fatal("binary squash-merged branch must count as delivered")
	}
	if !branchDelivered(repo, "agent/ox-129") {
		t.Fatal("context-shifted squash-merged branch must count as delivered")
	}

	// Also verify orphaned local-state ref (agent/ox-121) is pruned during reapDeliveredBranches.
	mainTip := strings.TrimSpace(gitRun(t, repo, "rev-parse", "HEAD"))
	gitRun(t, repo, "update-ref", userStateRef("agent/ox-177"), "refs/heads/agent/ox-177")
	gitRun(t, repo, "update-ref", userStateRef("agent/ox-129"), "refs/heads/agent/ox-129")
	gitRun(t, repo, "update-ref", userStateRef("agent/ox-121"), mainTip)

	if n := reapDeliveredBranches(repo, worktreeTestLogger()); n != 2 {
		t.Fatalf("reaped %d branches, want 2", n)
	}
	for _, b := range []string{"agent/ox-177", "agent/ox-129", "agent/ox-121"} {
		if out := gitRun(t, repo, "for-each-ref", userStateRef(b)); strings.TrimSpace(out) != "" {
			t.Fatalf("state ref for %s still present: %s", b, out)
		}
	}
}
