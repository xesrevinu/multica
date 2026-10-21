package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// githubTokenRenewBefore: a cached task token is reused until this close to
// expiry, so a long run always gets one with real time left.
const githubTokenRenewBefore = 10 * time.Minute

type cachedGitHubToken struct {
	token string
	exp   time.Time
}

type githubTokenCache struct {
	mu sync.Mutex
	m  map[string]cachedGitHubToken
}

func (c *githubTokenCache) get(taskID string, now time.Time) (cachedGitHubToken, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.m[taskID]
	if !ok || t.exp.IsZero() || t.exp.Sub(now) < githubTokenRenewBefore {
		return cachedGitHubToken{}, false
	}
	return t, true
}

func (c *githubTokenCache) put(taskID string, t cachedGitHubToken) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]cachedGitHubToken{}
	}
	// Bound the cache: drop anything already expired.
	now := time.Now()
	for k, v := range c.m {
		if !v.exp.After(now) {
			delete(c.m, k)
		}
	}
	c.m[taskID] = t
}

var agentGitHubTokens githubTokenCache

// taskGitHubToken returns a fresh-enough App token for a task's repo, minting
// through the server when the cached one is near expiry.
func (d *Daemon) taskGitHubToken(ctx context.Context, taskID, workDir string) (cachedGitHubToken, error) {
	if t, ok := agentGitHubTokens.get(taskID, time.Now()); ok {
		return t, nil
	}
	out, err := exec.Command("git", "-C", workDir, "remote", "get-url", "origin").Output()
	if err != nil {
		return cachedGitHubToken{}, err
	}
	mintCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	tok, exp, err := d.client.IssueTaskGitHubToken(mintCtx, taskID, strings.TrimSpace(string(out)))
	if err != nil {
		return cachedGitHubToken{}, err
	}
	t := cachedGitHubToken{token: tok, exp: exp}
	agentGitHubTokens.put(taskID, t)
	return t, nil
}

// githubTokenHandler serves the active task's App token to `multica
// github-token` (git credential helper and the gh shim), authenticated by the
// task credential exactly like /repo/checkout. Values are never logged.
func (d *Daemon) githubTokenHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		task, auth := d.activeRepoCheckoutTask(r)
		if auth != repoCheckoutAuthOK {
			d.writeRepoCheckoutAuthError(w, auth)
			return
		}
		if d.client == nil || task.WorkDir == "" {
			http.Error(w, "no github identity for this task", http.StatusNotFound)
			return
		}
		t, err := d.taskGitHubToken(r.Context(), task.TaskID, task.WorkDir)
		if err != nil {
			http.Error(w, "github app token unavailable", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]string{"token": t.token, "expires_at": t.exp.UTC().Format(time.RFC3339)})
	}
}

// ghShimScript refreshes GH_TOKEN from the daemon before every gh call, then
// runs the real gh. On failure it keeps whatever GH_TOKEN the run started with.
func ghShimScript(realGh string) string {
	return "#!/bin/sh\n# multica: refresh the GitHub App token, then run the real gh.\n" +
		"t=$(multica github-token 2>/dev/null) && [ -n \"$t\" ] && GH_TOKEN=$t && GITHUB_TOKEN=$t && export GH_TOKEN GITHUB_TOKEN\n" +
		"exec " + shellQuote(realGh) + " \"$@\"\n"
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// installGhShim writes the gh shim into dir (no secrets in it) and returns
// dir, or "" when gh is not on path.
func installGhShim(dir, path string) string {
	realGh := ""
	for _, p := range filepath.SplitList(path) {
		if p == "" || p == dir {
			continue
		}
		c := filepath.Join(p, "gh")
		if st, err := os.Stat(c); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			realGh = c
			break
		}
	}
	if realGh == "" {
		return ""
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	target := filepath.Join(dir, "gh")
	want := ghShimScript(realGh)
	if cur, err := os.ReadFile(target); err == nil && string(cur) == want {
		return dir
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, []byte(want), 0o755); err != nil {
		return ""
	}
	if err := os.Rename(tmp, target); err != nil {
		return ""
	}
	return dir
}
