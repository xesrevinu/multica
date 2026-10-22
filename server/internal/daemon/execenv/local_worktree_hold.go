package execenv

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	// IdleWorktreeCap is how many parked worktrees one repository keeps.
	// Older idle checkouts are removed first. A checkout a run is still
	// using is not counted and is never removed here.
	IdleWorktreeCap = 8

	worktreeHoldActive = "active"
	worktreeHoldIdle   = "idle"

	// IdleWorktreeRegistryEnv names the file listing repositories that
	// have a parked checkout. Unset disables the cache: runs still deliver
	// on the branch, and the worktree is removed at the end of the run.
	// The daemon points it at a file inside its workspaces root.
	IdleWorktreeRegistryEnv = "MULTICA_IDLE_WORKTREE_REGISTRY"
)

// worktreeHold is the daemon's claim on a Worktrunk checkout.
//
// active: a run has prepared this directory and has not finished. Idle GC
// must not take it. idle: Finalize committed the branch and left the
// directory in place so the next turn on this conversation can reuse it
// without running start hooks again.
type worktreeHold struct {
	Path    string      `json:"path"`
	Branch  string      `json:"branch"`
	GitRoot string      `json:"git_root"`
	Owner   branchOwner `json:"owner"`
	State   string      `json:"state"`
	At      time.Time   `json:"at"`
	// PID is the daemon that will run the agent, not the prepare helper.
	// Zero means the caller did not name a holder; an active hold with no
	// pid stays in use so a reap cannot guess that the run is over.
	PID int `json:"pid,omitempty"`
}

// claimIdleWorktree returns a parked checkout for this conversation, ready
// to replay into. ok is false when there is nothing safe to reuse: the
// branch is busy, the hold is not ours, or this turn has to start over
// because the branch was merged. A merged branch drops the parked checkout
// first, so the following create is not blocked by it.
func claimIdleWorktree(gitRoot string, plan taskBranchPlan, owner branchOwner, logger *slog.Logger) (string, bool, error) {
	hold, ok, err := readWorktreeHold(gitRoot, plan.name)
	if err != nil || !ok {
		return "", false, err
	}
	if hold.Owner != owner || hold.Branch != plan.name {
		return "", false, nil
	}
	checkedOut, err := worktreePathForBranch(gitRoot, plan.name)
	if err != nil {
		return "", false, err
	}
	if checkedOut == "" || !pathsMatch(checkedOut, hold.Path) {
		if clearErr := clearWorktreeHold(gitRoot, plan.name); clearErr != nil && logger != nil {
			logger.Warn("execenv: could not drop a stale worktree hold", "branch", plan.name, "error", clearErr)
		}
		return "", false, nil
	}
	if plan.reset {
		if err := removeLocalWorktreeDir(gitRoot, hold.Path, logger); err != nil {
			return "", false, err
		}
		if err := clearWorktreeHold(gitRoot, plan.name); err != nil {
			return "", false, err
		}
		if logger != nil {
			logger.Info("execenv: removed parked worktree; its branch is merged into HEAD", "branch", plan.name, "path", hold.Path)
		}
		return "", false, nil
	}
	if !plan.continues {
		return "", false, nil
	}
	if hold.State == worktreeHoldActive {
		if holdInUse(hold) {
			return "", false, nil
		}
		// The holder is gone. A dirty tree is the crashed run's only copy
		// of uncommitted work, so leave it checked out and let this turn
		// fork. A clean tree was already committed; reuse it.
		dirty, dirtyErr := worktreeIsDirty(hold.Path)
		if dirtyErr != nil || dirty {
			return "", false, nil
		}
	}
	if err := refreshCachedWorktree(hold.Path); err != nil {
		if logger != nil {
			logger.Warn("execenv: parked worktree could not be reused; removing it", "path", hold.Path, "error", err)
		}
		if rmErr := removeLocalWorktreeDir(gitRoot, hold.Path, logger); rmErr != nil {
			return "", false, rmErr
		}
		if clearErr := clearWorktreeHold(gitRoot, plan.name); clearErr != nil {
			return "", false, clearErr
		}
		return "", false, nil
	}
	if logger != nil {
		logger.Info("execenv: reusing parked worktree", "branch", plan.name, "path", hold.Path)
	}
	return hold.Path, true, nil
}

// refreshCachedWorktree puts a parked checkout back on its committed tip
// without touching ignored files. node_modules and the zvec symlink are
// ignored; deleting them is the cost this cache exists to avoid.
func refreshCachedWorktree(path string) error {
	if out, err := runGit(path, "reset", "--hard", "HEAD"); err != nil {
		return fmt.Errorf("reset parked worktree: %s: %w", out, err)
	}
	if out, err := runGit(path, "clean", "-fd"); err != nil {
		return fmt.Errorf("clean parked worktree: %s: %w", out, err)
	}
	return nil
}

func markWorktreeActive(gitRoot, path, branch string, owner branchOwner, pid int) error {
	return writeWorktreeHold(worktreeHold{
		Path:    path,
		Branch:  branch,
		GitRoot: gitRoot,
		Owner:   owner,
		State:   worktreeHoldActive,
		At:      time.Now(),
		PID:     pid,
	})
}

func parkIdleWorktree(w *LocalWorktree, now time.Time, logger *slog.Logger) error {
	if w == nil {
		return errors.New("park nil worktree")
	}
	if err := writeWorktreeHold(worktreeHold{
		Path:    w.Path,
		Branch:  w.Branch,
		GitRoot: w.GitRoot,
		Owner:   w.owner,
		State:   worktreeHoldIdle,
		At:      now,
	}); err != nil {
		return err
	}
	if _, err := reapIdleWorktreesInRepo(w.GitRoot, now, 0, IdleWorktreeCap, logger); err != nil {
		return err
	}
	if logger != nil {
		logger.Info("execenv: worktree parked for reuse", "branch", w.Branch, "path", w.Path)
	}
	return nil
}

// ReapIdleLocalWorktrees removes parked worktrees that have been idle longer
// than ttl, and any past IdleWorktreeCap within one repository. Checkouts
// whose hold is active are left alone, including when the holder pid has
// died and the tree is still dirty: that directory is the uncommitted work.
func ReapIdleLocalWorktrees(ttl time.Duration, cap int, now time.Time, logger *slog.Logger) (int, error) {
	roots, err := listIdleWorktreeRoots()
	if err != nil {
		return 0, err
	}
	removed := 0
	var errs []error
	for _, root := range roots {
		unlock, lockErr := lockGitRoot(root, logger)
		if lockErr != nil {
			errs = append(errs, lockErr)
			continue
		}
		n, reapErr := reapIdleWorktreesInRepo(root, now, ttl, cap, logger)
		n += reapDeliveredBranches(root, logger)
		unlock()
		removed += n
		if reapErr != nil {
			errs = append(errs, reapErr)
		}
	}
	return removed, errors.Join(errs...)
}

// reapIdleWorktreesInRepo runs while the caller holds the git-root lock.
// ttl <= 0 skips the age check and only enforces cap.
func reapIdleWorktreesInRepo(gitRoot string, now time.Time, ttl time.Duration, cap int, logger *slog.Logger) (int, error) {
	holds, err := listWorktreeHolds(gitRoot)
	if err != nil {
		return 0, err
	}
	if cap <= 0 {
		cap = IdleWorktreeCap
	}
	var idle []worktreeHold
	removed := 0
	var errs []error
	for _, hold := range holds {
		if hold.State != worktreeHoldIdle {
			continue
		}
		// Delivered (squash-merged and gone from the remote, or an ancestor
		// of main): the parked checkout and its branch are done. A dirty tree
		// is kept: it is the only copy of whatever is in it.
		if branchDelivered(gitRoot, hold.Branch) {
			if dirty, dErr := worktreeIsDirty(hold.Path); dErr == nil && !dirty {
				if err := removeHeldWorktree(hold, logger); err != nil {
					errs = append(errs, err)
					continue
				}
				dropBranch(gitRoot, hold.Branch, logger)
				removed++
				continue
			}
		}
		if ttl > 0 && now.Sub(hold.At) > ttl {
			if err := removeHeldWorktree(hold, logger); err != nil {
				errs = append(errs, err)
				continue
			}
			removed++
			continue
		}
		idle = append(idle, hold)
	}
	sort.Slice(idle, func(i, j int) bool { return idle[i].At.Before(idle[j].At) })
	for len(idle) > cap {
		if err := removeHeldWorktree(idle[0], logger); err != nil {
			errs = append(errs, err)
			break
		}
		removed++
		idle = idle[1:]
	}
	return removed, errors.Join(errs...)
}

func removeHeldWorktree(hold worktreeHold, logger *slog.Logger) error {
	if err := removeLocalWorktreeDir(hold.GitRoot, hold.Path, logger); err != nil {
		return err
	}
	if err := clearWorktreeHold(hold.GitRoot, hold.Branch); err != nil {
		return err
	}
	if logger != nil {
		logger.Info("execenv: reaped idle worktree", "branch", hold.Branch, "path", hold.Path)
	}
	return nil
}

func pathsMatch(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ai, bi)
}

func holdInUse(h worktreeHold) bool {
	if h.State != worktreeHoldActive {
		return false
	}
	if h.PID <= 0 {
		return true
	}
	return pidAlive(h.PID)
}

func worktreePathForBranch(gitRoot, branch string) (string, error) {
	out, err := runGit(gitRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return "", fmt.Errorf("execenv: list worktrees: %s: %w", out, err)
	}
	want := "branch refs/heads/" + branch
	var path string
	for _, line := range splitLines(out) {
		if p, ok := cutPrefix(line, "worktree "); ok {
			path = p
			continue
		}
		if line == want {
			return path, nil
		}
	}
	return "", nil
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func cutPrefix(s, prefix string) (string, bool) {
	if len(s) < len(prefix) || s[:len(prefix)] != prefix {
		return "", false
	}
	return s[len(prefix):], true
}

func gitCommonDir(gitRoot string) (string, error) {
	dir, err := runGitTrimmed(gitRoot, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(gitRoot, dir)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if resolved, evalErr := filepath.EvalSymlinks(abs); evalErr == nil {
		abs = resolved
	}
	return abs, nil
}

func worktreeHoldPath(gitRoot, branch string) (string, error) {
	common, err := gitCommonDir(gitRoot)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(branch))
	return filepath.Join(common, "multica-worktree-holds", hex.EncodeToString(sum[:])), nil
}

func readWorktreeHold(gitRoot, branch string) (worktreeHold, bool, error) {
	path, err := worktreeHoldPath(gitRoot, branch)
	if err != nil {
		return worktreeHold{}, false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return worktreeHold{}, false, nil
		}
		return worktreeHold{}, false, err
	}
	var hold worktreeHold
	if err := json.Unmarshal(data, &hold); err != nil {
		return worktreeHold{}, false, err
	}
	return hold, true, nil
}

func listWorktreeHolds(gitRoot string) ([]worktreeHold, error) {
	common, err := gitCommonDir(gitRoot)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(common, "multica-worktree-holds")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var holds []worktreeHold
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var hold worktreeHold
		if err := json.Unmarshal(data, &hold); err != nil {
			return nil, err
		}
		holds = append(holds, hold)
	}
	return holds, nil
}

func writeWorktreeHold(hold worktreeHold) error {
	path, err := worktreeHoldPath(hold.GitRoot, hold.Branch)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(hold)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	return rememberIdleWorktreeRoot(hold.GitRoot)
}

func clearWorktreeHold(gitRoot, branch string) error {
	if gitRoot == "" || branch == "" {
		return nil
	}
	path, err := worktreeHoldPath(gitRoot, branch)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	holds, err := listWorktreeHolds(gitRoot)
	if err != nil {
		return err
	}
	if len(holds) == 0 {
		return forgetIdleWorktreeRoot(gitRoot)
	}
	return nil
}

type idleWorktreeRegistry struct {
	Roots []string `json:"roots"`
}

func idleWorktreeRegistryPath() (string, error) {
	p := os.Getenv(IdleWorktreeRegistryEnv)
	if p == "" {
		return "", errIdleRegistryUnset
	}
	return p, nil
}

var errIdleRegistryUnset = errors.New("execenv: idle worktree registry is unset")

func withIdleRegistry(fn func(path string, reg *idleWorktreeRegistry) error) error {
	path, err := idleWorktreeRegistryPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	lock, err := openLockFile(path + ".lock")
	if err != nil {
		return err
	}
	defer lock.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		ok, lockErr := lockFileExclusiveNonBlocking(lock)
		if lockErr != nil {
			return lockErr
		}
		if ok {
			break
		}
		if time.Now().After(deadline) {
			return errors.New("execenv: idle worktree registry is busy")
		}
		time.Sleep(gitRootLockPoll)
	}
	defer unlockFile(lock)

	reg := &idleWorktreeRegistry{}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, reg); err != nil {
			return err
		}
	}
	if err := fn(path, reg); err != nil {
		return err
	}
	encoded, err := json.Marshal(reg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, encoded, 0o644)
}

func rememberIdleWorktreeRoot(gitRoot string) error {
	return withIdleRegistry(func(_ string, reg *idleWorktreeRegistry) error {
		for _, root := range reg.Roots {
			if root == gitRoot {
				return nil
			}
		}
		reg.Roots = append(reg.Roots, gitRoot)
		return nil
	})
}

func forgetIdleWorktreeRoot(gitRoot string) error {
	return withIdleRegistry(func(_ string, reg *idleWorktreeRegistry) error {
		kept := reg.Roots[:0]
		for _, root := range reg.Roots {
			if root != gitRoot {
				kept = append(kept, root)
			}
		}
		reg.Roots = kept
		return nil
	})
}

func listIdleWorktreeRoots() ([]string, error) {
	path, err := idleWorktreeRegistryPath()
	if errors.Is(err, errIdleRegistryUnset) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var roots []string
	err = withIdleRegistry(func(_ string, reg *idleWorktreeRegistry) error {
		roots = append([]string(nil), reg.Roots...)
		return nil
	})
	return roots, err
}

// reclaimParkedExclusiveWorktree hands the orchestrator branch's parked
// checkout to the task that now needs that branch. The orchestrator routes one
// task per issue at a time, so an idle hold on agent/<key> is an earlier phase
// (or an earlier conversation of the same owner) that finished and parked; its
// branch is the issue's deliverable, and continuing on it in place loses
// nothing. Before this, a later phase planned agent/<key>, found the parked
// checkout holding it and failed branch_occupied (OX-123), blocking the issue
// until someone removed the worktree by hand.
//
// It only reclaims a checkout the daemon itself parked for this workspace: an
// idle (or dead-holder) hold whose path is the worktree git says holds the
// branch, with a clean tree. A live run, a dirty tree (the only copy of
// uncommitted work), or a worktree the daemon has no hold for still fails
// branch_occupied as before.
func reclaimParkedExclusiveWorktree(gitRoot string, plan taskBranchPlan, workspaceID string, logger *slog.Logger) (taskBranchPlan, string, bool, error) {
	occupant, err := worktreePathForBranch(gitRoot, plan.name)
	if err != nil || occupant == "" {
		return plan, "", false, err
	}
	hold, found, err := readWorktreeHold(gitRoot, plan.name)
	if err != nil || !found {
		return plan, "", false, err
	}
	if hold.Branch != plan.name || !pathsMatch(occupant, hold.Path) || holdInUse(hold) {
		return plan, "", false, nil
	}
	if workspaceID != "" && hold.Owner.WorkspaceID != "" && hold.Owner.WorkspaceID != workspaceID {
		return plan, "", false, nil
	}
	if dirty, dirtyErr := worktreeIsDirty(hold.Path); dirtyErr != nil || dirty {
		if logger != nil {
			logger.Info("execenv: parked checkout of the orchestrator branch has uncommitted changes; not reclaiming it",
				"branch", plan.name, "path", hold.Path)
		}
		return plan, "", false, nil
	}
	// Merged into HEAD (or never moved off it): nothing on the branch is
	// unique, so drop the parked checkout and let this task recreate the
	// branch from HEAD, as claimIdleWorktree does for a merged branch.
	if _, mergedErr := runGit(gitRoot, "merge-base", "--is-ancestor", "refs/heads/"+plan.name, "HEAD"); mergedErr == nil {
		if err := removeLocalWorktreeDir(gitRoot, hold.Path, logger); err != nil {
			return plan, "", false, err
		}
		if err := clearWorktreeHold(gitRoot, plan.name); err != nil {
			return plan, "", false, err
		}
		if logger != nil {
			logger.Info("execenv: removed the parked checkout of a merged orchestrator branch; recreating it from HEAD",
				"branch", plan.name, "path", hold.Path)
		}
		plan.reset = true
		return plan, "", false, nil
	}
	// Unmerged work: continue it in place, replaying from the snapshot the
	// branch was recorded with. No record we can verify means we cannot
	// replay safely; leave the occupant alone.
	ref, err := readUserStateRef(gitRoot, plan.name)
	if err != nil || ref == "" {
		return plan, "", false, nil
	}
	record, err := readBranchRecord(gitRoot, ref)
	if err != nil || record.checkpoint == "" {
		return plan, "", false, nil
	}
	if _, err := runGit(gitRoot, "merge-base", "--is-ancestor", record.checkpoint, "refs/heads/"+plan.name); err != nil {
		return plan, "", false, nil
	}
	if err := refreshCachedWorktree(hold.Path); err != nil {
		return plan, "", false, err
	}
	tip, err := runGitTrimmed(hold.Path, "rev-parse", "HEAD")
	if err != nil {
		return plan, "", false, fmt.Errorf("execenv: rev-parse parked worktree: %w", err)
	}
	plan.base = tip
	plan.continues = true
	plan.priorState = record.state
	plan.priorCheckpoint = record.checkpoint
	if logger != nil {
		logger.Info("execenv: reclaiming the parked checkout of the orchestrator branch",
			"branch", plan.name, "path", hold.Path, "parked_by_agent", hold.Owner.AgentID)
	}
	return plan, hold.Path, true, nil
}
