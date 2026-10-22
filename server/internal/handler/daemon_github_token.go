package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	ghapp "github.com/multica-ai/multica/server/internal/github/app"
)

// agentGitHubPermissions is what an agent's token may do: push its branch,
// open and comment on PRs and issues, and read checks. No workflows,
// administration, secrets or statuses (the orchestrator publishes those).
var agentGitHubPermissions = map[string]string{
	"contents":      "write",
	"pull_requests": "write",
	"issues":        "write",
	"checks":        "read",
	"metadata":      "read",
}

var githubRepoURL = regexp.MustCompile(`^(?:https://github\.com/|git@github\.com:|ssh://git@github\.com/)([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+?)(?:\.git)?/?$`)

// parseGitHubRepo returns owner and repo for a github.com repo URL.
func parseGitHubRepo(raw string) (string, string, bool) {
	m := githubRepoURL.FindStringSubmatch(strings.TrimSpace(raw))
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// githubTokenMinter is the slice of the App client this handler needs.
type githubTokenMinter interface {
	Enabled() bool
	InstallationFor(ctx context.Context, owner, repo string) (int64, error)
	MintScoped(ctx context.Context, installationID int64, repo string, perms map[string]string) (ghapp.ScopedToken, error)
}

// githubMinter is swapped in tests.
var githubMinter = func() (githubTokenMinter, error) { return ghapp.Shared() }

// IssueTaskGitHubToken mints a short-lived GitHub App installation token for
// a running task, scoped to one repository of the task's workspace and to
// agentGitHubPermissions. The token is returned once and never logged.
func (h *Handler) IssueTaskGitHubToken(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req struct {
		Repo string `json:"repo"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	owner, repo, ok := parseGitHubRepo(req.Repo)
	if !ok {
		writeError(w, http.StatusBadRequest, "repo must be a github.com repository URL")
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	if !workspaceHasGitHubRepo(parseWorkspaceRepos([]byte(ws.Repos)), owner, repo) {
		writeError(w, http.StatusForbidden, "repo is not a repository of this workspace")
		return
	}
	minter, err := githubMinter()
	if err != nil || minter == nil || !minter.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "github app is not configured")
		return
	}
	id, err := minter.InstallationFor(r.Context(), owner, repo)
	if err != nil {
		writeError(w, http.StatusBadGateway, "github app is not installed on the repository")
		return
	}
	tok, err := minter.MintScoped(r.Context(), id, repo, agentGitHubPermissions)
	if err != nil {
		if errors.Is(err, ghapp.ErrDisabled) {
			writeError(w, http.StatusServiceUnavailable, "github app is not configured")
			return
		}
		writeError(w, http.StatusBadGateway, "github token mint failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token":      tok.Token,
		"expires_at": tok.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

func workspaceHasGitHubRepo(repos []RepoData, owner, repo string) bool {
	for _, rd := range repos {
		o, n, ok := parseGitHubRepo(rd.URL)
		if ok && strings.EqualFold(o, owner) && strings.EqualFold(n, repo) {
			return true
		}
	}
	return false
}
