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

// The orchestrator branch belongs to the issue: a reviewer or lead (another
// agent) reuses the owner's parked, clean checkout of agent/<key> in place
// instead of forking a fresh rv-/fingerprint worktree per phase.
func TestExclusiveBranchOtherAgentReusesIssueCheckout(t *testing.T) {
	repo := newTestRepo(t)
	installFakeWt(t)
	enableWorktrunk(t, repo)
	params := func(task, agent string) LocalWorktreeParams {
		return LocalWorktreeParams{
			LocalPath: repo, EnvRoot: t.TempDir(), AgentName: "J", TaskID: task, ConversationKey: "ATT-62",
			WorkspaceID: testBranchOwner.WorkspaceID, AgentID: agent, ConversationID: testBranchOwner.ConversationID,
			ExclusiveBranch: "agent/att-62", HolderPID: os.Getpid(),
		}
	}
	owner, err := PrepareLocalWorktree(params(turnOneTask, testBranchOwner.AgentID), worktreeTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(owner.WorkDir, "work.txt"), "owner\n")
	finalizeOK(t, owner)
	reviewer, err := PrepareLocalWorktree(params(turnTwoTask, "reviewer-agent"), worktreeTestLogger())
	if err != nil {
		t.Fatalf("reviewer prepare: %v", err)
	}
	if reviewer.Branch != "agent/att-62" || reviewer.Path != owner.Path {
		t.Fatalf("reviewer = branch %q path %q; want the issue checkout %q", reviewer.Branch, reviewer.Path, owner.Path)
	}
	if got := readFile(t, filepath.Join(reviewer.WorkDir, "work.txt")); got != "owner\n" {
		t.Fatalf("owner's work = %q", got)
	}
	if out := gitRun(t, repo, "branch", "--list", "agent/att-62-*"); strings.TrimSpace(out) != "" {
		t.Fatalf("forked a task branch: %s", out)
	}
	reviewer.Discard(worktreeTestLogger())
}

// OX-123: a later task on the orchestrator branch (another conversation of
// the owner, or a later phase) found the earlier phase's parked, clean
// checkout holding agent/<key> and failed branch_occupied. It now continues
// in that checkout; a dirty parked checkout still blocks.
func TestExclusiveBranchReclaimsParkedCleanCheckout(t *testing.T) {
	repo := newTestRepo(t)
	installFakeWt(t)
	enableWorktrunk(t, repo)
	params := func(task, conv string) LocalWorktreeParams {
		return LocalWorktreeParams{
			LocalPath: repo, EnvRoot: t.TempDir(), AgentName: "J", TaskID: task, ConversationKey: "ATT-62",
			WorkspaceID: testBranchOwner.WorkspaceID, AgentID: testBranchOwner.AgentID, ConversationID: conv,
			ExclusiveBranch: "agent/att-62", HolderPID: os.Getpid(),
		}
	}
	first, err := PrepareLocalWorktree(params(turnOneTask, testBranchOwner.ConversationID), worktreeTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(first.WorkDir, "work.txt"), "owner\n")
	finalizeOK(t, first)
	if _, err := os.Stat(first.Path); err != nil {
		t.Fatalf("expected a parked checkout: %v", err)
	}
	second, err := PrepareLocalWorktree(params(turnTwoTask, "another-conversation"), worktreeTestLogger())
	if err != nil {
		t.Fatalf("second prepare: %v", err)
	}
	if second.Branch != "agent/att-62" || second.Path != first.Path || !second.Continued {
		t.Fatalf("second = branch %q path %q continued %v; want the parked checkout %q", second.Branch, second.Path, second.Continued, first.Path)
	}
	if got := readFile(t, filepath.Join(second.WorkDir, "work.txt")); got != "owner\n" {
		t.Fatalf("earlier phase's work = %q", got)
	}
	if out := gitRun(t, repo, "branch", "--list", "agent/att-62-*"); strings.TrimSpace(out) != "" {
		t.Fatalf("forked a task branch: %s", out)
	}
	second.Discard(worktreeTestLogger())
}

func TestExclusiveBranchDirtyParkedCheckoutStillBlocks(t *testing.T) {
	repo := newTestRepo(t)
	installFakeWt(t)
	enableWorktrunk(t, repo)
	params := func(task, conv string) LocalWorktreeParams {
		return LocalWorktreeParams{
			LocalPath: repo, EnvRoot: t.TempDir(), AgentName: "J", TaskID: task, ConversationKey: "ATT-62",
			WorkspaceID: testBranchOwner.WorkspaceID, AgentID: testBranchOwner.AgentID, ConversationID: conv,
			ExclusiveBranch: "agent/att-62", HolderPID: os.Getpid(),
		}
	}
	first, err := PrepareLocalWorktree(params(turnOneTask, testBranchOwner.ConversationID), worktreeTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(first.WorkDir, "work.txt"), "owner\n")
	finalizeOK(t, first)
	writeFile(t, filepath.Join(first.Path, "uncommitted.txt"), "keep me\n")
	_, err = PrepareLocalWorktree(params(turnTwoTask, "another-conversation"), worktreeTestLogger())
	var occ *BranchOccupiedError
	if !errors.As(err, &occ) {
		t.Fatalf("want BranchOccupiedError for a dirty parked checkout, got %v", err)
	}
	if got := readFile(t, filepath.Join(first.Path, "uncommitted.txt")); got != "keep me\n" {
		t.Fatalf("uncommitted work touched: %q", got)
	}
}

// The OX-123 shape: the parked checkout's branch sits at HEAD (merged / never
// moved). It is dropped and the branch recreated, instead of branch_occupied.
func TestExclusiveBranchMergedParkedCheckoutIsReplaced(t *testing.T) {
	repo := newTestRepo(t)
	installFakeWt(t)
	enableWorktrunk(t, repo)
	params := func(task, conv string) LocalWorktreeParams {
		return LocalWorktreeParams{
			LocalPath: repo, EnvRoot: t.TempDir(), AgentName: "J", TaskID: task, ConversationKey: "ATT-62",
			WorkspaceID: testBranchOwner.WorkspaceID, AgentID: testBranchOwner.AgentID, ConversationID: conv,
			ExclusiveBranch: "agent/att-62", HolderPID: os.Getpid(),
		}
	}
	first, err := PrepareLocalWorktree(params(turnOneTask, testBranchOwner.ConversationID), worktreeTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(first.WorkDir, "work.txt"), "owner\n")
	finalizeOK(t, first)
	gitRun(t, repo, "merge", "--ff-only", "agent/att-62")
	second, err := PrepareLocalWorktree(params(turnTwoTask, "another-conversation"), worktreeTestLogger())
	if err != nil {
		t.Fatalf("second prepare: %v", err)
	}
	if second.Branch != "agent/att-62" {
		t.Fatalf("second branch = %q", second.Branch)
	}
	second.Discard(worktreeTestLogger())
}
