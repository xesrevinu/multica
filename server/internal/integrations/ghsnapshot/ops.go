package ghsnapshot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// PR detail and write operations for the issue-centric UI. Error values carry
// GitHub's "message" field at most; never request or response bodies with
// credentials.

// Permissions returns the permissions granted to the installation token
// (minting one if needed).
func (c *Client) Permissions(ctx context.Context, installationID int64) (map[string]string, error) {
	if !c.Enabled() {
		return nil, errors.New("github app not configured")
	}
	return c.app.Permissions(ctx, installationID)
}

// REST performs one REST call as the installation (conditional GETs and
// rate-limit tracking happen in the shared App client). out may be nil.
func (c *Client) REST(ctx context.Context, installationID int64, method, path string, body, out any) error {
	if !c.Enabled() {
		return errors.New("github app not configured")
	}
	return c.app.Do(ctx, installationID, method, path, body, out)
}

// PRCommit is one commit of a PR.
type PRCommit struct {
	SHA     string    `json:"sha"`
	Message string    `json:"message"`
	Author  string    `json:"author"`
	Avatar  string    `json:"avatar,omitempty"`
	Date    time.Time `json:"date"`
	Parents []string  `json:"parents"`
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
	Number         int               `json:"number"`
	Title          string            `json:"title"`
	State          string            `json:"state"` // open | closed | merged
	Draft          bool              `json:"draft"`
	URL            string            `json:"url"`
	HeadRef        string            `json:"head_ref"`
	HeadSHA        string            `json:"head_sha"`
	BaseRef        string            `json:"base_ref"`
	Mergeable      *bool             `json:"mergeable"`
	MergeableState string            `json:"mergeable_state"` // clean | dirty | behind | blocked | unstable | unknown
	BehindBy       int               `json:"behind_by"`
	AheadBy        int               `json:"ahead_by"`
	Additions      int               `json:"additions"`
	Deletions      int               `json:"deletions"`
	ChangedFiles   int               `json:"changed_files"`
	Commits        []PRCommit        `json:"commits"`
	Checks         []PRCheck         `json:"checks"`
	MergedAt       *time.Time        `json:"merged_at,omitempty"`
	Author         string            `json:"author,omitempty"`
	Labels         []string          `json:"labels"`
	Reviews        []PRReview        `json:"reviews"`
	ReviewComments []PRReviewComment `json:"review_comments"`
	// Conflicts lists files changed on both sides since the merge base when
	// GitHub reports the PR as dirty (a cheap stand-in for git's conflict set).
	Conflicts   []string `json:"conflicts"`
	HeadDeleted bool     `json:"head_deleted"`
}

// PRReview is one submitted review (approve / request changes / comment).
type PRReview struct {
	User        string    `json:"user"`
	Avatar      string    `json:"avatar,omitempty"`
	State       string    `json:"state"`
	Body        string    `json:"body"`
	SubmittedAt time.Time `json:"submitted_at"`
	URL         string    `json:"url,omitempty"`
}

// PRReviewComment is an inline (diff) review comment.
type PRReviewComment struct {
	ID        int64     `json:"id"`
	User      string    `json:"user"`
	Avatar    string    `json:"avatar,omitempty"`
	Path      string    `json:"path"`
	Line      int       `json:"line,omitempty"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	URL       string    `json:"url,omitempty"`
	InReplyTo int64     `json:"in_reply_to,omitempty"`
}

// PRFile is one changed file of a pull request.
type PRFile struct {
	Filename  string `json:"filename"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Patch     string `json:"patch,omitempty"`
	Previous  string `json:"previous_filename,omitempty"`
}

type ghUser struct {
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
}

func (u *ghUser) login() string {
	if u == nil {
		return ""
	}
	return u.Login
}

func (u *ghUser) avatar() string {
	if u == nil {
		return ""
	}
	return u.AvatarURL
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
		User   *ghUser `json:"user"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	if err := c.REST(ctx, installationID, http.MethodGet, fmt.Sprintf("%s/pulls/%d", base, number), nil, &pr); err != nil {
		return nil, err
	}
	d := &PRDetail{Number: pr.Number, Title: pr.Title, State: pr.State, Draft: pr.Draft, URL: pr.HTMLURL,
		HeadRef: pr.Head.Ref, HeadSHA: pr.Head.SHA, BaseRef: pr.Base.Ref, Mergeable: pr.Mergeable, MergeableState: pr.MergeableState,
		Additions: pr.Additions, Deletions: pr.Deletions, ChangedFiles: pr.ChangedFiles, MergedAt: pr.MergedAt,
		Commits: []PRCommit{}, Checks: []PRCheck{}, Author: pr.User.login(), Labels: []string{},
		Reviews: []PRReview{}, ReviewComments: []PRReviewComment{}, Conflicts: []string{}}
	for _, l := range pr.Labels {
		d.Labels = append(d.Labels, l.Name)
	}
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
		Author  *ghUser `json:"author"`
		Parents []struct {
			SHA string `json:"sha"`
		} `json:"parents"`
	}
	// The follow-up reads are independent and each fills its own fields of d,
	// so they run concurrently (a PR card was ~6 sequential round trips).
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if c.REST(ctx, installationID, http.MethodGet, fmt.Sprintf("%s/pulls/%d/commits?per_page=100", base, number), nil, &commits) == nil {
			for _, x := range commits {
				who := x.Commit.Author.Name
				if x.Author.login() != "" {
					who = x.Author.login()
				}
				msg, _, _ := strings.Cut(x.Commit.Message, "\n")
				pc := PRCommit{SHA: x.SHA, Message: msg, Author: who, Avatar: x.Author.avatar(), Date: x.Commit.Author.Date, Parents: []string{}}
				for _, p := range x.Parents {
					pc.Parents = append(pc.Parents, p.SHA)
				}
				d.Commits = append(d.Commits, pc)
			}
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
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
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		if pr.Base.Ref != "" && pr.Head.SHA != "" && d.State == "open" {
			var cmp struct {
				AheadBy         int `json:"ahead_by"`
				BehindBy        int `json:"behind_by"`
				MergeBaseCommit struct {
					SHA string `json:"sha"`
				} `json:"merge_base_commit"`
			}
			if c.REST(ctx, installationID, http.MethodGet, fmt.Sprintf("%s/compare/%s...%s", base, url.PathEscape(pr.Base.Ref), url.PathEscape(pr.Head.SHA)), nil, &cmp) == nil {
				d.AheadBy, d.BehindBy = cmp.AheadBy, cmp.BehindBy
				if pr.MergeableState == "dirty" && cmp.MergeBaseCommit.SHA != "" {
					d.Conflicts = c.overlap(ctx, installationID, base, number, cmp.MergeBaseCommit.SHA, pr.Base.Ref)
				}
			}
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		c.fillReviews(ctx, installationID, base, number, d)
	}()
	if d.State != "open" && pr.Head.Ref != "" {
		var ref struct{}
		if err := c.REST(ctx, installationID, http.MethodGet, base+"/git/ref/heads/"+pathRef(pr.Head.Ref), nil, &ref); err != nil {
			var ae *APIError
			d.HeadDeleted = errors.As(err, &ae) && ae.Status == 404
		}
	}
	wg.Wait()
	return d, nil
}

// pathRef escapes each segment of a ref so "agent/att-1" stays a path.
func pathRef(ref string) string {
	parts := strings.Split(ref, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// overlap returns files the PR changes that the base also changed since the
// merge base.
func (c *Client) overlap(ctx context.Context, installationID int64, base string, number int, mergeBase, baseRef string) []string {
	out := []string{}
	mine := map[string]bool{}
	files, err := c.ListPRFiles(ctx, installationID, base, number)
	if err != nil {
		return out
	}
	for _, f := range files {
		mine[f.Filename] = true
	}
	var cmp struct {
		Files []struct {
			Filename string `json:"filename"`
		} `json:"files"`
	}
	if c.REST(ctx, installationID, http.MethodGet, fmt.Sprintf("%s/compare/%s...%s", base, url.PathEscape(mergeBase), url.PathEscape(baseRef)), nil, &cmp) != nil {
		return out
	}
	for _, f := range cmp.Files {
		if mine[f.Filename] {
			out = append(out, f.Filename)
		}
	}
	return out
}

func (c *Client) fillReviews(ctx context.Context, installationID int64, base string, number int, d *PRDetail) {
	var reviews []struct {
		User        *ghUser   `json:"user"`
		State       string    `json:"state"`
		Body        string    `json:"body"`
		SubmittedAt time.Time `json:"submitted_at"`
		HTMLURL     string    `json:"html_url"`
	}
	if c.REST(ctx, installationID, http.MethodGet, fmt.Sprintf("%s/pulls/%d/reviews?per_page=100", base, number), nil, &reviews) == nil {
		for _, r := range reviews {
			if r.State == "PENDING" {
				continue
			}
			d.Reviews = append(d.Reviews, PRReview{User: r.User.login(), Avatar: r.User.avatar(), State: r.State, Body: r.Body, SubmittedAt: r.SubmittedAt, URL: r.HTMLURL})
		}
	}
	var comments []struct {
		ID        int64     `json:"id"`
		User      *ghUser   `json:"user"`
		Path      string    `json:"path"`
		Line      *int      `json:"line"`
		Body      string    `json:"body"`
		CreatedAt time.Time `json:"created_at"`
		HTMLURL   string    `json:"html_url"`
		InReplyTo int64     `json:"in_reply_to_id"`
	}
	if c.REST(ctx, installationID, http.MethodGet, fmt.Sprintf("%s/pulls/%d/comments?per_page=100", base, number), nil, &comments) == nil {
		for _, x := range comments {
			rc := PRReviewComment{ID: x.ID, User: x.User.login(), Avatar: x.User.avatar(), Path: x.Path, Body: x.Body, CreatedAt: x.CreatedAt, URL: x.HTMLURL, InReplyTo: x.InReplyTo}
			if x.Line != nil {
				rc.Line = *x.Line
			}
			d.ReviewComments = append(d.ReviewComments, rc)
		}
	}
}

// ListPRFiles returns the PR's changed files (first 300, GitHub's page cap
// ×3). base is repoPath(owner, repo).
func (c *Client) ListPRFiles(ctx context.Context, installationID int64, base string, number int) ([]PRFile, error) {
	out := []PRFile{}
	for page := 1; page <= 3; page++ {
		var files []PRFile
		if err := c.REST(ctx, installationID, http.MethodGet, fmt.Sprintf("%s/pulls/%d/files?per_page=100&page=%d", base, number, page), nil, &files); err != nil {
			if page == 1 {
				return nil, err
			}
			break
		}
		out = append(out, files...)
		if len(files) < 100 {
			break
		}
	}
	return out, nil
}

// PRFiles is ListPRFiles addressed by owner/repo.
func (c *Client) PRFiles(ctx context.Context, installationID int64, owner, repo string, number int) ([]PRFile, error) {
	return c.ListPRFiles(ctx, installationID, repoPath(owner, repo), number)
}

// AddLabels adds labels to the PR (an issue under the hood); existing labels
// stay. Unknown labels are created by GitHub with a default colour.
func (c *Client) AddLabels(ctx context.Context, installationID int64, owner, repo string, number int, labels []string) error {
	if len(labels) == 0 {
		return nil
	}
	return c.REST(ctx, installationID, http.MethodPost, fmt.Sprintf("%s/issues/%d/labels", repoPath(owner, repo), number), map[string]any{"labels": labels}, nil)
}

// DeleteBranch deletes a head branch (refs/heads/<ref>).
func (c *Client) DeleteBranch(ctx context.Context, installationID int64, owner, repo, ref string) error {
	return c.REST(ctx, installationID, http.MethodDelete, repoPath(owner, repo)+"/git/refs/heads/"+pathRef(ref), nil, nil)
}

// FetchPRHead returns the PR's current head commit.
func (c *Client) FetchPRHead(ctx context.Context, installationID int64, owner, repo string, number int) (string, error) {
	var pr struct {
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if err := c.REST(ctx, installationID, http.MethodGet, fmt.Sprintf("%s/pulls/%d", repoPath(owner, repo), number), nil, &pr); err != nil {
		return "", err
	}
	return pr.Head.SHA, nil
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

// SquashMerge squash-merges the PR, guarded by the expected head sha. The
// commit title is the PR title (no "Merge pull request #n"), the body a short
// summary from the PR description plus the App bot's Co-authored-by trailer.
func (c *Client) SquashMerge(ctx context.Context, installationID int64, owner, repo string, number int, headSHA string) (string, error) {
	var pr struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		Head  struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if err := c.REST(ctx, installationID, http.MethodGet, fmt.Sprintf("%s/pulls/%d", repoPath(owner, repo), number), nil, &pr); err != nil {
		return "", err
	}
	if headSHA == "" {
		headSHA = pr.Head.SHA
	}
	botTrailer := ""
	if b, err := c.app.Bot(ctx, installationID); err == nil {
		botTrailer = b.CoAuthoredBy()
	}
	// Agent commits are authored by the bot and credit the human; the squash
	// keeps those credits instead of the bot co-authoring its own commit.
	var commits []struct {
		Commit struct {
			Message string `json:"message"`
		} `json:"commit"`
	}
	var messages []string
	if err := c.REST(ctx, installationID, http.MethodGet, fmt.Sprintf("%s/pulls/%d/commits?per_page=100", repoPath(owner, repo), number), nil, &commits); err == nil {
		for _, cm := range commits {
			messages = append(messages, cm.Commit.Message)
		}
	}
	trailer := SquashCoAuthors(messages, botTrailer)
	title, msg := SquashCommitMessage(pr.Title, pr.Body, number, trailer)
	body := map[string]any{"merge_method": "squash", "commit_title": title, "commit_message": msg}
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

// SquashCoAuthors returns the Co-authored-by trailers to put on a squash: the
// distinct ones from the PR's commits, minus the bot's own; the bot trailer
// only when the commits credit nobody else.
func SquashCoAuthors(commitMessages []string, botTrailer string) string {
	seen := map[string]bool{}
	var out []string
	for _, m := range commitMessages {
		for _, l := range strings.Split(strings.ReplaceAll(m, "\r\n", "\n"), "\n") {
			t := strings.TrimSpace(l)
			if !strings.HasPrefix(strings.ToLower(t), "co-authored-by:") {
				continue
			}
			if botTrailer != "" && strings.EqualFold(t, botTrailer) {
				continue
			}
			k := strings.ToLower(t)
			if !seen[k] {
				seen[k] = true
				out = append(out, t)
			}
		}
	}
	if len(out) == 0 {
		return botTrailer
	}
	return strings.Join(out, "\n")
}

var htmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)

// SquashCommitMessage builds the squash commit: title = PR title, body = the
// first paragraph of the PR description (HTML comments and markdown headings
// dropped, at most 400 characters), a "PR #n" reference and an optional trailer.
func SquashCommitMessage(prTitle, prBody string, number int, trailer string) (string, string) {
	title := strings.TrimSpace(prTitle)
	summary := ""
	for _, para := range strings.Split(strings.ReplaceAll(htmlComment.ReplaceAllString(prBody, ""), "\r\n", "\n"), "\n\n") {
		// Markdown headings ("## Why") are section labels, not a summary.
		var keep []string
		for _, l := range strings.Split(para, "\n") {
			if t := strings.TrimSpace(l); t != "" && !strings.HasPrefix(t, "#") {
				keep = append(keep, t)
			}
		}
		if len(keep) > 0 {
			summary = strings.Join(keep, "\n")
			break
		}
	}
	if r := []rune(summary); len(r) > 400 {
		summary = strings.TrimSpace(string(r[:400])) + "…"
	}
	var parts []string
	if summary != "" {
		parts = append(parts, summary)
	}
	parts = append(parts, fmt.Sprintf("PR #%d", number))
	msg := strings.Join(parts, "\n\n")
	if trailer != "" && !strings.Contains(msg, trailer) {
		msg += "\n\n" + trailer
	}
	return title, msg
}

// PRAbilities derives which PR card actions the installation may perform.
type PRAbilities struct {
	ReadChecks   bool `json:"read_checks"`
	UpdateBranch bool `json:"update_branch"`
	Rerun        bool `json:"rerun"`
	Merge        bool `json:"merge"`
	Labels       bool `json:"labels"`
	DeleteBranch bool `json:"delete_branch"`
}

func AbilitiesFrom(perms map[string]string) PRAbilities {
	w := func(k string) bool { return perms[k] == "write" || perms[k] == "admin" }
	r := func(k string) bool { return w(k) || perms[k] == "read" }
	return PRAbilities{
		ReadChecks:   r("checks"),
		UpdateBranch: w("contents") && w("pull_requests"),
		Rerun:        w("actions"),
		Merge:        w("contents") && w("pull_requests"),
		Labels:       w("issues") || w("pull_requests"),
		DeleteBranch: w("contents"),
	}
}
