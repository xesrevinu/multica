package daemon

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// applyAgentGitHubIdentity switches the task's gh/git identity from the
// daemon user to the GitHub App: GH_TOKEN/GITHUB_TOKEN carry a short-lived
// installation token scoped to the worktree's GitHub repo, and the worktree's
// own git config authors commits as the App bot and pushes with that token.
// Any failure leaves the daemon user's identity in place (logged, no values).
func (d *Daemon) applyAgentGitHubIdentity(ctx context.Context, taskID, workDir string, agentEnv map[string]string, logger *slog.Logger) {
	if !execenv.AgentGitHubAppEnabled() || workDir == "" || d.client == nil {
		return
	}
	out, err := exec.Command("git", "-C", workDir, "remote", "get-url", "origin").Output()
	if err != nil {
		return
	}
	repoURL := strings.TrimSpace(string(out))
	if !strings.Contains(repoURL, "github.com") {
		return
	}
	mintCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	token, exp, err := d.client.IssueTaskGitHubToken(mintCtx, taskID, repoURL)
	if err != nil {
		execenv.ClearWorktreeGitHubIdentity(workDir)
		if logger != nil {
			logger.Warn("github app identity unavailable; agent keeps the daemon user's gh/git identity", "error", err)
		}
		return
	}
	if err := execenv.ConfigureWorktreeGitHubIdentity(workDir); err != nil {
		execenv.ClearWorktreeGitHubIdentity(workDir)
		if logger != nil {
			logger.Warn("github app identity: worktree git config failed; agent keeps the daemon user's identity", "error", err)
		}
		return
	}
	agentGitHubTokens.put(taskID, cachedGitHubToken{token: token, exp: exp})
	if shim := installGhShim(ghShimDir(), agentEnv["PATH"]); shim != "" {
		agentEnv["PATH"] = shim + string(filepath.ListSeparator) + agentEnv["PATH"]
	}
	agentEnv["GH_TOKEN"] = token
	agentEnv["GITHUB_TOKEN"] = token
	// gh prefers GH_TOKEN over its stored login; keep it from prompting.
	agentEnv["GH_PROMPT_DISABLED"] = "1"
	if logger != nil {
		logger.Info("github app identity applied to agent", "expires_at", exp.UTC().Format(time.RFC3339))
	}
}

// ghShimDir holds the daemon's gh shim (a script with no secrets in it).
func ghShimDir() string {
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "multica", "gh-shim")
	}
	return filepath.Join(os.TempDir(), "multica-gh-shim")
}
