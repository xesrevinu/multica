package execenv

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An orchestrated issue's agent/<key> branch is exclusive: when another
// worktree holds it the task fails naming that worktree instead of forking
// agent/<key>-<task>, which the orchestrator would never look at.
func TestPrepareLocalWorktreeExclusiveBranchFailsInsteadOfForking(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	params := func(task string) LocalWorktreeParams {
		return LocalWorktreeParams{
			LocalPath: repo, EnvRoot: t.TempDir(), AgentName: "J", TaskID: task, ConversationKey: "ATT-62",
			WorkspaceID: testBranchOwner.WorkspaceID, AgentID: testBranchOwner.AgentID, ConversationID: testBranchOwner.ConversationID,
			ExclusiveBranch: "agent/att-62",
		}
	}
	first, err := PrepareLocalWorktree(params(turnOneTask), worktreeTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	if first.Branch != "agent/att-62" {
		t.Fatalf("first branch = %q", first.Branch)
	}
	_, err = PrepareLocalWorktree(params(turnTwoTask), worktreeTestLogger())
	var occ *BranchOccupiedError
	if !errors.As(err, &occ) {
		t.Fatalf("want BranchOccupiedError, got %v", err)
	}
	held := first.Path
	if real, rerr := filepath.EvalSymlinks(held); rerr == nil {
		held = real // git reports the canonical path (/private/var on macOS)
	}
	if occ.Branch != "agent/att-62" || occ.Path != held {
		t.Fatalf("occupant = %+v, want %s", occ, held)
	}
	if !strings.Contains(err.Error(), BranchOccupiedMarker) || !strings.Contains(err.Error(), "is held by the worktree at "+held+";") {
		t.Fatalf("error text the engine parses: %v", err)
	}
	if out := gitRun(t, repo, "branch", "--list", "agent/att-62-*"); strings.TrimSpace(out) != "" {
		t.Fatalf("forked a task branch anyway: %s", out)
	}
	finalizeOK(t, first)
}

func TestOccupantFromPorcelain(t *testing.T) {
	porcelain := strings.Join([]string{
		"worktree /src/multica", "HEAD aaa", "branch refs/heads/main", "",
		"worktree /home/k/.cursor/worktrees/multica/agent-att-62", "HEAD bbb", "branch refs/heads/agent-att-62", "",
		"worktree /home/k/.cursor/worktrees/multica/other", "HEAD ccc", "branch refs/heads/agent/att-70", "",
	}, "\n")
	for branch, want := range map[string]string{
		"agent/att-70": "/home/k/.cursor/worktrees/multica/other",        // checked out there
		"agent/att-62": "/home/k/.cursor/worktrees/multica/agent-att-62", // wt sanitize collision
		"agent/att-99": "",
	} {
		if got := occupantFromPorcelain(porcelain, branch); got != want {
			t.Errorf("%s: got %q want %q", branch, got, want)
		}
	}
}

// A reviewer or lead is a different agent from the issue owner: the owner's
// recorded agent/<key> is not theirs to continue, and they run on their own
// fingerprinted fork as before instead of failing branch_occupied (OX-108).
func TestPrepareLocalWorktreeExclusiveBranchOtherAgentForks(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	params := func(task, agent string) LocalWorktreeParams {
		return LocalWorktreeParams{
			LocalPath: repo, EnvRoot: t.TempDir(), AgentName: "J", TaskID: task, ConversationKey: "ATT-62",
			WorkspaceID: testBranchOwner.WorkspaceID, AgentID: agent, ConversationID: testBranchOwner.ConversationID,
			ExclusiveBranch: "agent/att-62",
		}
	}
	owner, err := PrepareLocalWorktree(params(turnOneTask, testBranchOwner.AgentID), worktreeTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	if owner.Branch != "agent/att-62" {
		t.Fatalf("owner branch = %q", owner.Branch)
	}
	if err := os.WriteFile(filepath.Join(owner.Path, "work.txt"), []byte("owner\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	finalizeOK(t, owner) // produced work: agent/att-62 stays, recorded to the owner
	if out := gitRun(t, repo, "branch", "--list", "agent/att-62"); strings.TrimSpace(out) == "" {
		t.Fatal("owner branch gone after finalize")
	}
	reviewer, err := PrepareLocalWorktree(params(turnTwoTask, "reviewer-agent"), worktreeTestLogger())
	if err != nil {
		t.Fatalf("reviewer prepare: %v", err)
	}
	if !strings.HasPrefix(reviewer.Branch, "agent/att-62-") {
		t.Fatalf("reviewer branch = %q, want its own fork", reviewer.Branch)
	}
	finalizeOK(t, reviewer)
}
