package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/github/app"
	"github.com/multica-ai/multica/server/internal/integrations/ghsnapshot"
)

// ghRepo is the engine's view of one GitHub repository through the shared
// GitHub App client (no `gh` CLI on the server). Calls go out as the App
// installation; conditional GETs and the rate-limit budget live in app.Client.
type ghRepo struct {
	c           *ghsnapshot.Client
	inst        int64
	owner, name string
}

var errNoApp = errors.New("GitHub App not configured (GITHUB_APP_ID / GITHUB_APP_PRIVATE_KEY): github mode needs it")

func (r *repoOps) ghRepo(ctx context.Context, sp *Spec) (*ghRepo, error) {
	owner, name, ok := strings.Cut(sp.Repo.Slug, "/")
	if !ok || owner == "" || name == "" {
		return nil, fmt.Errorf("repo slug %q is not owner/name", sp.Repo.Slug)
	}
	c := r.svc.github()
	if !c.Enabled() {
		return nil, errNoApp
	}
	inst, err := c.App().InstallationFor(ctx, owner, name)
	if err != nil {
		return nil, fmt.Errorf("GitHub App installation for %s: %w", sp.Repo.Slug, err)
	}
	return &ghRepo{c: c, inst: inst, owner: owner, name: name}, nil
}

func (g *ghRepo) do(ctx context.Context, method, path string, body, out any) error {
	return g.c.REST(ctx, g.inst, method, "/repos/"+g.owner+"/"+g.name+path, body, out)
}

// lowBudget reports whether the installation has used >90% of its hourly
// REST budget; the engine then leans on the webhook-fed snapshot.
func (g *ghRepo) lowBudget() bool {
	return g != nil && g.c.App().Rate(g.inst).Low(time.Now())
}

// branchHead returns the sha of a branch, "" when it does not exist.
func (g *ghRepo) branchHead(ctx context.Context, branch string) (string, error) {
	var b struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	err := g.do(ctx, http.MethodGet, "/branches/"+url.PathEscape(branch), nil, &b)
	if app.IsStatus(err, http.StatusNotFound) {
		return "", nil
	}
	return b.Commit.SHA, err
}

type ghPR struct {
	Number         int    `json:"number"`
	HTMLURL        string `json:"html_url"`
	State          string `json:"state"`
	Merged         bool   `json:"merged"`
	MergeCommitSHA string `json:"merge_commit_sha"`
	Mergeable      *bool  `json:"mergeable"`
	MergeableState string `json:"mergeable_state"`
	Head           struct {
		SHA string `json:"sha"`
	} `json:"head"`
}

// mergeable maps the REST fields to the gate's MERGEABLE / CONFLICTING /
// UNKNOWN vocabulary (GitHub computes mergeability lazily: null = unknown).
func (p *ghPR) mergeable() string {
	switch {
	case p.Mergeable == nil:
		return "UNKNOWN"
	case *p.Mergeable:
		return "MERGEABLE"
	default:
		return "CONFLICTING"
	}
}

func (g *ghRepo) openPRFor(ctx context.Context, branch string) (*ghPR, error) {
	var l []ghPR
	q := url.Values{"state": {"open"}, "head": {g.owner + ":" + branch}, "per_page": {"5"}}
	if err := g.do(ctx, http.MethodGet, "/pulls?"+q.Encode(), nil, &l); err != nil {
		return nil, err
	}
	if len(l) == 0 {
		return nil, nil
	}
	return &l[0], nil
}

func (g *ghRepo) createPR(ctx context.Context, head, base, title, body string) (*ghPR, error) {
	var pr ghPR
	err := g.do(ctx, http.MethodPost, "/pulls", map[string]any{"head": head, "base": base, "title": title, "body": body}, &pr)
	return &pr, err
}

func (g *ghRepo) pr(ctx context.Context, number string) (*ghPR, error) {
	var pr ghPR
	err := g.do(ctx, http.MethodGet, "/pulls/"+url.PathEscape(number), nil, &pr)
	return &pr, err
}

// checkStates returns check name → state for a commit in the gate's
// vocabulary: check runs give PENDING until completed, then the upper-cased
// conclusion; commit statuses give their upper-cased state.
func (g *ghRepo) checkStates(ctx context.Context, sha string) (map[string]string, error) {
	out := map[string]string{}
	var runs struct {
		CheckRuns []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
		} `json:"check_runs"`
	}
	if err := g.do(ctx, http.MethodGet, "/commits/"+sha+"/check-runs?per_page=100", nil, &runs); err != nil {
		return nil, err
	}
	for _, c := range runs.CheckRuns {
		st := "PENDING"
		if c.Status == "completed" {
			st = strings.ToUpper(c.Conclusion)
		}
		if _, seen := out[c.Name]; !seen {
			out[c.Name] = st
		}
	}
	var combined struct {
		Statuses []struct {
			Context string `json:"context"`
			State   string `json:"state"`
		} `json:"statuses"`
	}
	if err := g.do(ctx, http.MethodGet, "/commits/"+sha+"/status", nil, &combined); err != nil {
		return nil, err
	}
	for _, s := range combined.Statuses {
		if _, seen := out[s.Context]; !seen {
			out[s.Context] = strings.ToUpper(s.State)
		}
	}
	return out, nil
}

// rateLimited turns a GitHub back-off into a pending gate result so the
// engine retries later instead of counting an infra failure.
func rateLimited(err error, res ProbeResult) (ProbeResult, bool) {
	var rl *app.RateLimitError
	if errors.As(err, &rl) {
		res.Kind, res.Pending, res.Detail = "pending", true, rl.Error()
		return res, true
	}
	return res, false
}

// deleteBranch drops a merged head branch; already gone (404, or 422 when the
// repo auto-deletes head branches) is not an error.
func (g *ghRepo) deleteBranch(ctx context.Context, branch string) error {
	err := g.c.DeleteBranch(ctx, g.inst, g.owner, g.name, branch)
	if app.IsStatus(err, http.StatusNotFound) || app.IsStatus(err, http.StatusUnprocessableEntity) {
		return nil
	}
	return err
}
