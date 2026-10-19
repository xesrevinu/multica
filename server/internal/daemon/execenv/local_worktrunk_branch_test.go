package execenv

import "testing"

func TestBranchUnavailableDetachedWorktreeAtPath(t *testing.T) {
	msg := "✗ Cannot switch to agent/att-64-ef6db80479de — there's a detached worktree at the expected path ~/.cursor/worktrees/x/agent-att-64-ef6db80479de"
	if !branchUnavailable(msg) {
		t.Fatal("a worktree occupying the expected path must fork a task branch, not fail the task")
	}
}
