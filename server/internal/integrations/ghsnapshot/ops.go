package ghsnapshot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// PR detail and write operations for the issue-centric UI. Error values carry
// GitHub's "message" field at most; never request or response bodies with
// credentials.

// APIError is a non-2xx REST response.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("github: %d %s", e.Status, e.Message) }

// Permissions returns the permissions granted to the installation token
// (minting one if needed).
func (c *Client) Permissions(ctx context.Context, installationID int64) (map[string]string, error) {
	if !c.Enabled() {
		return nil, errors.New("github app not configured")
	}
	if _, err := c.installationToken(ctx, installationID); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := map[string]string{}
	for k, v := range c.tokens[installationID].permissions {
		out[k] = v
	}
	return out, nil
}

// REST performs one REST call as the installation. out may be nil.
func (c *Client) REST(ctx context.Context, installationID int64, method, path string, body, out any) error {
	if !c.Enabled() {
		return errors.New("github app not configured")
	}
	token, err := c.installationToken(ctx, installationID)
	if err != nil {
		return err
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.apiBase, "/")+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0") {
		return rateLimitFromResponse(resp, c.now())
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var m struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &m)
		return &APIError{Status: resp.StatusCode, Message: m.Message}
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return errors.New("github: malformed response")
		}
	}
	return nil
}

// PRCommit is one commit of a PR.
type PRCommit struct {
	SHA     string    `json:"sha"`
	Message string    `json:"message"`
	Author  string    `json:"author"`
	Date    time.Time `json:"date"`
}

// PRCheck is one check run on the PR head.
type PRCheck struct {
	Name       string `json:"name"`
	Status     string `json:"status"`               // queued | in_progress | completed
	Conclusion string `json:"conclusion,omitempty"` // success | failure | ...
	URL        string `json:"url,omitempty"`
	App        string `json:"app,omitempty"`
}

// PRDetail is the live PR state shown on the PR card.
type PRDetail struct {
	Number         int        `json:"number"`
	Title          string     `json:"title"`
	State          string     `json:"state"` // open | closed | merged
	Draft          bool       `json:"draft"`
	URL            string     `json:"url"`
	HeadRef        string     `json:"head_ref"`
	HeadSHA        string     `json:"head_sha"`
	BaseRef        string     `json:"base_ref"`
	Mergeable      *bool      `json:"mergeable"`
	MergeableState string     `json:"mergeable_state"` // clean | dirty | behind | blocked | unstable | unknown
	BehindBy       int        `json:"behind_by"`
	AheadBy        int        `json:"ahead_by"`
	Additions      int        `json:"additions"`
	Deletions      int        `json:"deletions"`
	ChangedFiles   int        `json:"changed_files"`
	Commits        []PRCommit `json:"commits"`
	Checks         []PRCheck  `json:"checks"`
	MergedAt       *time.Time `json:"merged_at,omitempty"`
}

func repoPath(owner, repo string) string {
	return "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)
}

// FetchPRDetail reads the PR, its commits, head check runs and how far the
// branch is behind its base. Commits/checks/compare failures degrade to empty.
func (c *Client) FetchPRDetail(ctx context.Context, installationID int64, owner, repo string, number int) (*PRDetail, error) {
	base := repoPath(owner, repo)
	var pr struct {
		Number         int        `json:"number"`
		Title          string     `json:"title"`
		State          string     `json:"state"`
		Draft          bool       `json:"draft"`
		HTMLURL        string     `json:"html_url"`
		Merged         bool       `json:"merged"`
		MergedAt       *time.Time `json:"merged_at"`
		Mergeable      *bool      `json:"mergeable"`
		MergeableState string     `json:"mergeable_state"`
		Additions      int        `json:"additions"`
		Deletions      int        `json:"deletions"`
		ChangedFiles   int        `json:"changed_files"`
		Head           struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
	}
	if err := c.REST(ctx, installationID, http.MethodGet, fmt.Sprintf("%s/pulls/%d", base, number), nil, &pr); err != nil {
		return nil, err
	}
	d := &PRDetail{Number: pr.Number, Title: pr.Title, State: pr.State, Draft: pr.Draft, URL: pr.HTMLURL,
		HeadRef: pr.Head.Ref, HeadSHA: pr.Head.SHA, BaseRef: pr.Base.Ref, Mergeable: pr.Mergeable, MergeableState: pr.MergeableState,
		Additions: pr.Additions, Deletions: pr.Deletions, ChangedFiles: pr.ChangedFiles, MergedAt: pr.MergedAt,
		Commits: []PRCommit{}, Checks: []PRCheck{}}
	if pr.Merged {
		d.State = "merged"
	}
	var commits []struct {
		SHA    string `json:"sha"`
		Commit struct {
			Message string `json:"message"`
			Author  struct {
				Name string    `json:"name"`
				Date time.Time `json:"date"`
			} `json:"author"`
		} `json:"commit"`
		Author *struct {
			Login string `json:"login"`
		} `json:"author"`
	}
	if c.REST(ctx, installationID, http.MethodGet, fmt.Sprintf("%s/pulls/%d/commits?per_page=100", base, number), nil, &commits) == nil {
		for _, x := range commits {
			who := x.Commit.Author.Name
			if x.Author != nil && x.Author.Login != "" {
				who = x.Author.Login
			}
			msg, _, _ := strings.Cut(x.Commit.Message, "\n")
			d.Commits = append(d.Commits, PRCommit{SHA: x.SHA, Message: msg, Author: who, Date: x.Commit.Author.Date})
		}
	}
	if pr.Head.SHA != "" {
		var runs struct {
			CheckRuns []struct {
				Name       string `json:"name"`
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
				HTMLURL    string `json:"html_url"`
				App        *struct {
					Slug string `json:"slug"`
				} `json:"app"`
			} `json:"check_runs"`
		}
		if c.REST(ctx, installationID, http.MethodGet, fmt.Sprintf("%s/commits/%s/check-runs?per_page=100", base, url.PathEscape(pr.Head.SHA)), nil, &runs) == nil {
			for _, r := range runs.CheckRuns {
				ck := PRCheck{Name: r.Name, Status: r.Status, Conclusion: r.Conclusion, URL: r.HTMLURL}
				if r.App != nil {
					ck.App = r.App.Slug
				}
				d.Checks = append(d.Checks, ck)
			}
		}
	}
	if pr.Base.Ref != "" && pr.Head.SHA != "" && d.State == "open" {
		var cmp struct {
			AheadBy  int `json:"ahead_by"`
			BehindBy int `json:"behind_by"`
		}
		if c.REST(ctx, installationID, http.MethodGet, fmt.Sprintf("%s/compare/%s...%s", base, url.PathEscape(pr.Base.Ref), url.PathEscape(pr.Head.SHA)), nil, &cmp) == nil {
			d.AheadBy, d.BehindBy = cmp.AheadBy, cmp.BehindBy
		}
	}
	return d, nil
}

// UpdateBranch merges the base branch into the PR branch.
func (c *Client) UpdateBranch(ctx context.Context, installationID int64, owner, repo string, number int, expectedHead string) error {
	body := map[string]any{}
	if expectedHead != "" {
		body["expected_head_sha"] = expectedHead
	}
	return c.REST(ctx, installationID, http.MethodPut, fmt.Sprintf("%s/pulls/%d/update-branch", repoPath(owner, repo), number), body, nil)
}

// RerunFailedChecks re-runs the failed jobs of every failed workflow run on
// headSHA and returns how many runs were restarted.
func (c *Client) RerunFailedChecks(ctx context.Context, installationID int64, owner, repo, headSHA string) (int, error) {
	base := repoPath(owner, repo)
	var list struct {
		WorkflowRuns []struct {
			ID         int64  `json:"id"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
		} `json:"workflow_runs"`
	}
	if err := c.REST(ctx, installationID, http.MethodGet, base+"/actions/runs?per_page=50&head_sha="+url.QueryEscape(headSHA), nil, &list); err != nil {
		return 0, err
	}
	n := 0
	for _, r := range list.WorkflowRuns {
		if r.Status != "completed" || (r.Conclusion != "failure" && r.Conclusion != "timed_out" && r.Conclusion != "cancelled") {
			continue
		}
		if err := c.REST(ctx, installationID, http.MethodPost, fmt.Sprintf("%s/actions/runs/%d/rerun-failed-jobs", base, r.ID), nil, nil); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// SquashMerge squash-merges the PR, guarded by the expected head sha.
func (c *Client) SquashMerge(ctx context.Context, installationID int64, owner, repo string, number int, headSHA string) (string, error) {
	body := map[string]any{"merge_method": "squash"}
	if headSHA != "" {
		body["sha"] = headSHA
	}
	var out struct {
		SHA    string `json:"sha"`
		Merged bool   `json:"merged"`
	}
	if err := c.REST(ctx, installationID, http.MethodPut, fmt.Sprintf("%s/pulls/%d/merge", repoPath(owner, repo), number), body, &out); err != nil {
		return "", err
	}
	return out.SHA, nil
}

// PRAbilities derives which PR card actions the installation may perform.
type PRAbilities struct {
	ReadChecks   bool `json:"read_checks"`
	UpdateBranch bool `json:"update_branch"`
	Rerun        bool `json:"rerun"`
	Merge        bool `json:"merge"`
}

func AbilitiesFrom(perms map[string]string) PRAbilities {
	w := func(k string) bool { return perms[k] == "write" || perms[k] == "admin" }
	r := func(k string) bool { return w(k) || perms[k] == "read" }
	return PRAbilities{
		ReadChecks:   r("checks"),
		UpdateBranch: w("contents") && w("pull_requests"),
		Rerun:        w("actions"),
		Merge:        w("contents") && w("pull_requests"),
	}
}
