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
	if path == "" {
		// No worktree holds it: wt refused because a plain directory (often a
		// leftover after the worktree was removed, e.g. an index cache written
		// back into it) sits where the worktree goes. Name it so the block says
		// what to clear instead of "(unknown)".
		if stray := strayDirectoryFrom(detail); stray != "" {
			path = stray + " (stray directory, not a git worktree)"
		} else if strings.Contains(detail, "already exists") {
			path = "(none: the branch exists without the daemon's ownership record; delete or re-record it, then unblock)"
		}
	}
	return &BranchOccupiedError{Branch: branch, Path: path, Detail: detail}
}

// strayDirectoryFrom pulls the path out of wt's "Directory already exists: <path>".
func strayDirectoryFrom(detail string) string {
	const marker = "Directory already exists: "
	i := strings.Index(detail, marker)
	if i < 0 {
		return ""
	}
	rest := detail[i+len(marker):]
	if j := strings.IndexAny(rest, "\n\r"); j >= 0 {
		rest = rest[:j]
	}
	return strings.TrimSpace(rest)
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
