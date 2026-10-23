package orchestrator

import (
	"net/http"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/ghsnapshot"
)

type linkedPR struct {
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	State     string     `json:"state"`
	URL       string     `json:"url"`
	Branch    string     `json:"branch,omitempty"`
	Repo      string     `json:"repo"`
	Author    string     `json:"author,omitempty"`
	MergedAt  *time.Time `json:"merged_at,omitempty"`
	UpdatedAt time.Time  `json:"updated_at"`
	Primary   bool       `json:"primary"`
}

// hPRList: GET /issues/{key}/prs — every PR linked to the issue, newest first.
func (s *Service) hPRList(w http.ResponseWriter, r *http.Request) {
	v, _, code, msg := s.prFor(r)
	if code != 0 {
		writeErr(w, code, "not_found", msg)
		return
	}
	rows, err := s.opt.Pool.QueryContext(r.Context(), `SELECT g.pr_number, g.title, g.state, g.html_url, COALESCE(g.branch,''), g.repo_owner||'/'||g.repo_name,
		COALESCE(g.author_login,''), g.merged_at, g.pr_updated_at
		FROM issue_pull_request ip JOIN github_pull_request g ON g.id=ip.pull_request_id
		WHERE ip.issue_id=$1 AND g.workspace_id=$2 ORDER BY g.pr_updated_at DESC LIMIT 50`, v.ID, wsID(r))
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	defer rows.Close()
	out := []linkedPR{}
	for rows.Next() {
		var p linkedPR
		if err := rows.Scan(&p.Number, &p.Title, &p.State, &p.URL, &p.Branch, &p.Repo, &p.Author, sqlTimePtr{&p.MergedAt}, sqlTime{&p.UpdatedAt}); err != nil {
			writeErr(w, 500, "db", err.Error())
			return
		}
		p.Primary = v.PR != nil && v.PR.Number == p.Number
		out = append(out, p)
	}
	writeJSON(w, 200, map[string]any{"prs": out})
}

// hPRFiles: GET /issues/{key}/pr/files — the "Files changed" tab.
func (s *Service) hPRFiles(w http.ResponseWriter, r *http.Request) {
	v, t, code, msg := s.prFor(r)
	if code != 0 {
		writeErr(w, code, "not_found", msg)
		return
	}
	if v.Orchestrated && v.Mode == "local" {
		writeJSON(w, 200, map[string]any{"mode": "local", "files": []any{}})
		return
	}
	gh := s.github()
	if t == nil || !gh.Enabled() {
		writeJSON(w, 200, map[string]any{"mode": "github", "files": []any{}, "live": false})
		return
	}
	files, err := gh.PRFiles(r.Context(), t.installation, t.owner, t.repo, t.number)
	if err != nil {
		writeGHErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"mode": "github", "files": files, "live": true, "number": t.number})
}

// hPRManage runs the management ops that are not card-primary:
// sync-labels pushes the issue's labels onto the PR; delete-branch removes the
// head branch once the PR is merged or closed.
func (s *Service) hPRManage(op string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v, t, code, msg := s.prFor(r)
		if code != 0 {
			writeErr(w, code, "not_found", msg)
			return
		}
		if t == nil {
			writeErr(w, 409, "no_pr", "issue has no linked GitHub pull request")
			return
		}
		gh := s.github()
		if !gh.Enabled() {
			writeErr(w, 503, "github_app_disabled", "GitHub App is not configured")
			return
		}
		perms, err := gh.Permissions(r.Context(), t.installation)
		if err != nil {
			writeErr(w, 502, "github", err.Error())
			return
		}
		can := ghsnapshot.AbilitiesFrom(perms)
		switch op {
		case "sync-labels":
			if !can.Labels {
				writeErr(w, 403, "github_permission", "the GitHub App installation lacks issues/pull_requests write")
				return
			}
			if err := gh.AddLabels(r.Context(), t.installation, t.owner, t.repo, t.number, v.Labels); err != nil {
				writeGHErr(w, err)
				return
			}
			writeJSON(w, 200, map[string]any{"op": op, "ok": true, "labels": v.Labels})
		case "delete-branch":
			if !can.DeleteBranch {
				writeErr(w, 403, "github_permission", "the GitHub App installation lacks contents write")
				return
			}
			d, err := gh.FetchPRDetail(r.Context(), t.installation, t.owner, t.repo, t.number)
			if err != nil {
				writeGHErr(w, err)
				return
			}
			if d.State == "open" {
				writeErr(w, 409, "pr_open", "the pull request is still open")
				return
			}
			if d.HeadRef == "" || d.HeadRef == d.BaseRef {
				writeErr(w, 409, "bad_ref", "refusing to delete the base branch")
				return
			}
			if err := gh.DeleteBranch(r.Context(), t.installation, t.owner, t.repo, d.HeadRef); err != nil {
				writeGHErr(w, err)
				return
			}
			writeJSON(w, 200, map[string]any{"op": op, "ok": true, "branch": d.HeadRef})
		}
	}
}
