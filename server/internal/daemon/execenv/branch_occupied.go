package execenv

import (
	"fmt"
	"path/filepath"
	"strings"
)

// BranchOccupiedMarker starts the error text of a task whose orchestrator
// branch is held by another worktree. The orchestrator engine looks for it in
// agent_task_queue.error (orchestrator.branchOccupied) and blocks the issue
// with the occupying path; keep the two in sync.
const BranchOccupiedMarker = "[branch_occupied]"

// BranchOccupiedError: the orchestrator's branch (agent/<key>) or the path wt
// derives from it is taken by another worktree. The daemon does not fork a
// task branch and does not touch the occupant.
type BranchOccupiedError struct {
	Branch string
	Path   string // occupying worktree, "" if it could not be identified
	Detail string // git / wt output
}

func (e *BranchOccupiedError) Error() string {
	path := e.Path
	if path == "" {
		path = "(unknown)"
	}
	msg := fmt.Sprintf("%s orchestrator branch %s is held by the worktree at %s; not forking a task branch and not touching it", BranchOccupiedMarker, e.Branch, path)
	if d := strings.TrimSpace(e.Detail); d != "" {
		msg += " (" + d + ")"
	}
	return msg
}

func branchOccupied(gitRoot, branch, detail string) error {
	path, _ := occupantOf(gitRoot, branch)
	return &BranchOccupiedError{Branch: branch, Path: path, Detail: detail}
}

// occupantOf finds the worktree that blocks branch, from
// `git worktree list --porcelain`: the one that has the branch checked out,
// or else one sitting at the directory wt would use for it
// ({{ branch | sanitize }}: "/" and "\" become "-", so agent/att-62 and a
// hand-made agent-att-62 share a path).
func occupantOf(gitRoot, branch string) (string, error) {
	out, err := runGit(gitRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return "", fmt.Errorf("execenv: list worktrees: %s: %w", strings.TrimSpace(out), err)
	}
	return occupantFromPorcelain(out, branch), nil
}

func occupantFromPorcelain(porcelain, branch string) string {
	want := "branch refs/heads/" + branch
	sanitized := strings.NewReplacer("/", "-", "\\", "-").Replace(branch)
	var cur, bySanitized string
	for _, line := range strings.Split(porcelain, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "worktree "):
			cur = strings.TrimPrefix(line, "worktree ")
			if bySanitized == "" && filepath.Base(cur) == sanitized {
				bySanitized = cur
			}
		case line == want:
			return cur
		}
	}
	return bySanitized
}
