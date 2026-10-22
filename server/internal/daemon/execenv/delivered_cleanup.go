package execenv

import (
	"bytes"
	"log/slog"
	"os/exec"
	"strings"
)

// Cleanup of delivered task branches.
//
// The orchestrator squash-merges agent/<key> and the remote branch is deleted,
// but nothing on the daemon side ever learned that: reap only enforced the
// idle cap/TTL, `wt remove --no-delete-branch` kept the local branch, and the
// refs/multica/local-state record pinned it. A squash merge is never an
// ancestor of main, so every "is it merged" check said no, and done issues
// left a parked checkout (with its target/), a local branch and a record
// behind until the cap pushed them out — the branch and record forever.
//
// branchDelivered answers "does this branch carry anything main lacks?" in a
// way that survives squash merges, and is conservative everywhere else.

const deliveredScanLimit = "400" // main commits after the merge base to compare

func remoteMainRef(gitRoot string) string {
	for _, ref := range []string{"refs/remotes/origin/main", "refs/remotes/origin/master"} {
		if out, err := runGitTrimmed(gitRoot, "rev-parse", "--verify", "--quiet", ref); err == nil && out != "" {
			return ref
		}
	}
	return ""
}

// branchDelivered: the branch is an ancestor of origin/main, or it is no
// longer on the remote and its whole change (merge base → tip) is either empty
// or matches, by stable patch-id, a single commit on origin/main (the squash).
// Anything it cannot prove returns false: the branch is kept.
func branchDelivered(gitRoot, branch string) bool {
	main := remoteMainRef(gitRoot)
	if main == "" || branch == "" {
		return false
	}
	ref := "refs/heads/" + branch
	if _, err := runGit(gitRoot, "merge-base", "--is-ancestor", ref, main); err == nil {
		return true
	}
	if out, err := runGitTrimmed(gitRoot, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+branch); err == nil && out != "" {
		return false // still open on the remote
	}
	base, err := runGitTrimmed(gitRoot, "merge-base", ref, main)
	if err != nil || base == "" {
		return false
	}
	diff, err := runGitStdout(gitRoot, "diff", "--binary", base, ref)
	if err != nil {
		return false
	}
	if strings.TrimSpace(diff) == "" {
		return true // nothing beyond main (e.g. a baseline-only research branch)
	}
	want := patchIDs(gitRoot, []byte(diff))
	if len(want) != 1 {
		return false
	}
	log, err := runGitStdout(gitRoot, "log", "-p", "--no-merges", "--max-count="+deliveredScanLimit, base+".."+main)
	if err != nil {
		return false
	}
	for _, id := range patchIDs(gitRoot, []byte(log)) {
		if id == want[0] {
			return true
		}
	}
	return false
}

// patchIDs runs `git patch-id --stable` and returns the ids in order.
func patchIDs(gitRoot string, patch []byte) []string {
	cmd := exec.Command("git", "-C", gitRoot, "patch-id", "--stable")
	cmd.Stdin = bytes.NewReader(patch)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var ids []string
	for _, line := range strings.Split(string(out), "\n") {
		if f := strings.Fields(line); len(f) >= 1 {
			ids = append(ids, f[0])
		}
	}
	return ids
}

// reapDeliveredBranches drops daemon-created agent/ branches (they carry a
// local-state record) that are not checked out anywhere and are delivered,
// together with their record. Runs under the git-root lock.
func reapDeliveredBranches(gitRoot string, logger *slog.Logger) int {
	out, err := runGitTrimmed(gitRoot, "for-each-ref", "--format=%(refname:strip=3)", localStateRefPrefix)
	if err != nil || out == "" {
		return 0
	}
	removed := 0
	for _, branch := range strings.Split(out, "\n") {
		branch = strings.TrimSpace(branch)
		if branch == "" || !strings.HasPrefix(branch, "agent/") {
			continue
		}
		if tip, err := runGitTrimmed(gitRoot, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err != nil || tip == "" {
			continue // orphaned record: pruneOrphanedStateRefs handles it
		}
		if path, err := worktreePathForBranch(gitRoot, branch); err != nil || path != "" {
			continue
		}
		if !branchDelivered(gitRoot, branch) {
			continue
		}
		dropBranch(gitRoot, branch, logger)
		removed++
		if logger != nil {
			logger.Info("execenv: dropped delivered task branch", "git_root", gitRoot, "branch", branch)
		}
	}
	return removed
}
