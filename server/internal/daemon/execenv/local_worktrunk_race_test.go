package execenv

import (
	"reflect"
	"testing"
)

// A worktree another task's wt created during our switch is not ours.
func TestAddedWorktreeForBranchIgnoresConcurrentWorktrees(t *testing.T) {
	porcelain := "worktree /repo\nHEAD abc\nbranch refs/heads/main\n\n" +
		"worktree /wt/agent-ox-80-x\nHEAD def\nbranch refs/heads/agent/ox-80-x\n\n" +
		"worktree /wt/agent-ox-86\nHEAD 123\nbranch refs/heads/agent/ox-86\n\n" +
		"worktree /wt/detached\nHEAD 456\ndetached\n"
	after := parseWorktreeBranches(porcelain)
	if after["/wt/detached"] != "" || after["/repo"] != "main" {
		t.Fatalf("parse %v", after)
	}
	before := map[string]struct{}{"/repo": {}}
	got := addedWorktreeForBranch(before, after, "agent/ox-80-x")
	if !reflect.DeepEqual(got, []string{"/wt/agent-ox-80-x"}) {
		t.Fatalf("got %v", got)
	}
	if got := addedWorktreeForBranch(before, after, "agent/none"); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}
