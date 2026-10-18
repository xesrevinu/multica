package execenv

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	// worktrunkSwitchTimeout covers pre-start. bun install and the reflink of
	// a primary checkout's target/ both finish before wt returns when those
	// commands are pre-start hooks. gitTimeout is too short for that.
	worktrunkSwitchTimeout = 15 * time.Minute
	worktrunkRemoveTimeout = 2 * time.Minute
)

// worktrunkConfigPresent reports whether this checkout asks Worktrunk to own
// worktree creation. The daemon then uses `wt` so the path template and the
// hooks (dependency install, build-cache reflink, zvec copy and drop) are the
// same ones a person gets from `wt switch --create`.
func worktrunkConfigPresent(gitRoot string) bool {
	info, err := os.Stat(filepath.Join(gitRoot, ".config", "wt.toml"))
	return err == nil && !info.IsDir()
}

// addWorktrunkWorktree creates the task worktree with `wt switch` and returns
// the path wt chose. It does not pick a directory of its own.
func addWorktrunkWorktree(gitRoot string, plan taskBranchPlan, taskID string, logger *slog.Logger) (path, branch string, created bool, err error) {
	if _, lookErr := exec.LookPath("wt"); lookErr != nil {
		return "", "", false, fmt.Errorf("execenv: %s has .config/wt.toml, so its worktree has to be created with wt; wt is not on PATH: %w", gitRoot, lookErr)
	}
	checkedOut, err := branchCheckedOut(gitRoot, plan.name)
	if err != nil {
		return "", "", false, err
	}
	if checkedOut {
		if logger != nil {
			logger.Info("execenv: conversation branch is already checked out; forking a task branch",
				"git_root", gitRoot, "branch", plan.name)
		}
		return createWorktrunkBranch(gitRoot, plan.altName(taskID), plan.base, logger)
	}
	if plan.reset {
		if out, ferr := runGit(gitRoot, "branch", "-f", plan.name, plan.base); ferr != nil {
			return "", "", false, fmt.Errorf("execenv: reset branch %s onto %s: %s: %w", plan.name, plan.base, strings.TrimSpace(out), ferr)
		}
	}
	if plan.continues || plan.reset {
		path, err = switchWorktrunk(gitRoot, plan.name, "", false, logger)
		if err != nil && branchUnavailable(err.Error()) {
			return createWorktrunkBranch(gitRoot, plan.altName(taskID), plan.base, logger)
		}
		if err != nil {
			return "", "", false, err
		}
		return path, plan.name, !plan.continues, nil
	}
	path, err = switchWorktrunk(gitRoot, plan.name, plan.base, true, logger)
	if err != nil && branchUnavailable(err.Error()) {
		return createWorktrunkBranch(gitRoot, plan.altName(taskID), plan.base, logger)
	}
	if err != nil {
		return "", "", false, err
	}
	return path, plan.name, true, nil
}

func createWorktrunkBranch(gitRoot, branch, base string, logger *slog.Logger) (string, string, bool, error) {
	path, err := switchWorktrunk(gitRoot, branch, base, true, logger)
	if err != nil {
		return "", "", false, err
	}
	return path, branch, true, nil
}

// switchWorktrunk runs wt and returns the worktree directory that appeared.
// A failed wt that still left a worktree has that directory removed before
// the error is returned, so a hook failure does not occupy the branch.
func switchWorktrunk(gitRoot, branch, base string, create bool, logger *slog.Logger) (string, error) {
	before, err := worktreePathSet(gitRoot)
	if err != nil {
		return "", err
	}
	// --yes: the daemon has no terminal. Without it, wt refuses the project's
	// own pre-start hooks (the copy of node_modules, direnv, zvec) and the
	// task never gets a worktree. The hooks are the checkout's .config/wt.toml,
	// which the user already attached as this resource.
	args := []string{"-C", gitRoot, "switch", "--yes", "--no-cd"}
	if create {
		args = append(args, "--create")
		if base != "" {
			args = append(args, "--base", base)
		}
	}
	args = append(args, branch)
	out, runErr := runWorktrunk(worktrunkSwitchTimeout, args...)
	after, listErr := worktreePathSet(gitRoot)
	added := addedWorktreePaths(before, after)
	if runErr != nil {
		for _, path := range added {
			if rmErr := removeViaWorktrunk(gitRoot, path, logger); rmErr != nil && logger != nil {
				logger.Warn("execenv: could not remove the worktree left by a failed wt switch",
					"path", path, "error", rmErr)
			}
		}
		return "", fmt.Errorf("execenv: wt switch %s: %s: %w", branch, strings.TrimSpace(out), runErr)
	}
	if listErr != nil {
		return "", listErr
	}
	if len(added) != 1 {
		return "", fmt.Errorf("execenv: wt switch %s created %d worktrees, want 1", branch, len(added))
	}
	if logger != nil {
		logger.Info("execenv: worktree created with wt", "branch", branch, "path", added[0])
	}
	return added[0], nil
}

func removeViaWorktrunk(gitRoot, worktreePath string, logger *slog.Logger) error {
	if _, lookErr := exec.LookPath("wt"); lookErr != nil {
		return lookErr
	}
	// --no-delete-branch: the branch is the task's deliverable. --foreground
	// waits for post-remove, which drops the copied zvec slot. --force removes
	// a dirty tree. --yes runs those hooks without a prompt; the daemon has
	// no terminal, and skipping them would leave the copied slot behind.
	out, err := runWorktrunk(worktrunkRemoveTimeout, "-C", gitRoot, "remove", "--yes", "--no-delete-branch", "--force", "--foreground", worktreePath)
	if err != nil {
		return fmt.Errorf("wt remove %s: %s: %w", worktreePath, strings.TrimSpace(out), err)
	}
	if logger != nil {
		logger.Info("execenv: worktree removed with wt", "path", worktreePath)
	}
	return nil
}

func runWorktrunk(timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "wt", args...)
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func branchCheckedOut(gitRoot, branch string) (bool, error) {
	out, err := runGit(gitRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("execenv: list worktrees: %s: %w", strings.TrimSpace(out), err)
	}
	want := "branch refs/heads/" + branch
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == want {
			return true, nil
		}
	}
	return false, nil
}

func worktreePathSet(gitRoot string) (map[string]struct{}, error) {
	out, err := runGit(gitRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("execenv: list worktrees: %s: %w", strings.TrimSpace(out), err)
	}
	set := map[string]struct{}{}
	for _, line := range strings.Split(out, "\n") {
		path, ok := strings.CutPrefix(line, "worktree ")
		if ok && path != "" {
			set[path] = struct{}{}
		}
	}
	return set, nil
}

func addedWorktreePaths(before, after map[string]struct{}) []string {
	var added []string
	for path := range after {
		if _, ok := before[path]; !ok {
			added = append(added, path)
		}
	}
	return added
}
