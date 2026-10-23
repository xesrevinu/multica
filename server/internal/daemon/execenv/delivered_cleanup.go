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

const (
	deliveredScanLimit      = "400" // main commits after the merge base to compare
	deliveredMergeTreeLimit = 64    // max non-key commits to test with git merge-tree
)

func remoteMainRef(gitRoot string) string {
	for _, ref := range []string{"refs/remotes/origin/main", "refs/remotes/origin/master"} {
		if out, err := runGitTrimmed(gitRoot, "rev-parse", "--verify", "--quiet", ref); err == nil && out != "" {
			return ref
		}
	}
	return ""
}

// branchDelivered: the branch is an ancestor of origin/main, or it is no
// longer on the remote and its whole change (merge base → tip) is either empty,
// matches by stable binary patch-id a single commit on origin/main, or 3-way
// merges into a commit's parent on origin/main to produce that commit's exact
// tree (covering squash merges after main advanced or touched adjacent/shared
// lines). Anything it cannot prove returns false: the branch is kept.
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
	if len(want) == 1 {
		if logOut, err := runGitStdout(gitRoot, "log", "-p", "--binary", "--no-merges", "--max-count="+deliveredScanLimit, base+".."+main); err == nil {
			for _, id := range patchIDs(gitRoot, []byte(logOut)) {
				if id == want[0] {
					return true
				}
			}
		}
	}
	return squashMergedInRange(gitRoot, ref, branch, base, main)
}

// squashMergedInRange checks whether any commit S in base..main is the 3-way
// squash merge of ref onto S^ (i.e. `git merge-tree --write-tree S^ ref` equals
// `S^{tree}`). Commits whose subject mentions the task key (e.g. "OX-129") are
// tested first so the common case runs a single merge-tree call.
func squashMergedInRange(gitRoot, ref, branch, base, main string) bool {
	out, err := runGitTrimmed(gitRoot, "log", "--no-merges", "--format=%H\t%T\t%P\t%s", "--max-count="+deliveredScanLimit, base+".."+main)
	if err != nil || out == "" {
		return false
	}
	type candidate struct {
		tree   string
		parent string
	}
	key := strings.ToLower(strings.TrimPrefix(branch, "agent/"))
	var prio, rest []candidate
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "\t", 4)
		if len(parts) < 4 {
			continue
		}
		parent := strings.Fields(parts[2])
		if len(parent) != 1 || parts[1] == "" {
			continue
		}
		c := candidate{tree: parts[1], parent: parent[0]}
		if key != "" && strings.Contains(strings.ToLower(parts[3]), key) {
			prio = append(prio, c)
		} else {
			rest = append(rest, c)
		}
	}
	for _, c := range prio {
		if mergeTreeMatches(gitRoot, c.parent, ref, c.tree) {
			return true
		}
	}
	if len(rest) > deliveredMergeTreeLimit {
		rest = rest[:deliveredMergeTreeLimit]
	}
	for _, c := range rest {
		if mergeTreeMatches(gitRoot, c.parent, ref, c.tree) {
			return true
		}
	}
	return false
}

func mergeTreeMatches(gitRoot, parent, ref, wantTree string) bool {
	if parent == "" || wantTree == "" {
		return false
	}
	got, err := runGitTrimmed(gitRoot, "merge-tree", "--write-tree", parent, ref)
	return err == nil && got == wantTree
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
// together with their record, and prunes any orphaned local-state records whose
// branch was already deleted. Runs under the git-root lock.
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
			continue // orphaned record: pruneOrphanedStateRefs below drops it
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
	pruneOrphanedStateRefs(gitRoot, logger)
	return removed
}
