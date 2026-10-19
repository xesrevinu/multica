package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/integrations/ghsnapshot"
)

func (s *Service) github() *ghsnapshot.Client {
	s.ghOnce.Do(func() {
		c, err := ghsnapshot.NewClientFromEnv()
		if err != nil {
			slog.Warn("orchestrator: github app client", "err", err)
		}
		s.gh = c
	})
	return s.gh
}

type prTarget struct {
	installation int64
	owner, repo  string
	number       int
}

// prFor resolves the issue's linked PR (as in the issue list) and its App
// installation. Returns the view so callers can check mode/orchestration.
func (s *Service) prFor(r *http.Request) (*IssueView, *prTarget, int, string) {
	v, err := s.viewByKey(r, chi.URLParam(r, "key"))
	if err != nil || v == nil {
		return nil, nil, 404, "issue not found"
	}
	if v.PR == nil {
		return v, nil, 0, ""
	}
	t := &prTarget{number: v.PR.Number}
	// ?number= picks another PR linked to the issue (the PR list).
	if n, aerr := strconv.Atoi(r.URL.Query().Get("number")); aerr == nil && n > 0 {
		t.number = n
	}
	err = s.opt.Pool.QueryRow(r.Context(), `SELECT g.installation_id, g.repo_owner, g.repo_name FROM issue_pull_request ip JOIN github_pull_request g ON g.id=ip.pull_request_id
		WHERE ip.issue_id=$1 AND g.pr_number=$2 AND g.workspace_id=$3 ORDER BY g.pr_updated_at DESC LIMIT 1`, v.ID, t.number, wsID(r)).Scan(&t.installation, &t.owner, &t.repo)
	if err != nil {
		return v, nil, 0, ""
	}
	return v, t, 0, ""
}

// hPR: GET /issues/{key}/pr — live PR detail plus which actions the App's
// permissions allow. Local-mode teams get {mode:"local"} and no PR card.
func (s *Service) hPR(w http.ResponseWriter, r *http.Request) {
	v, t, code, msg := s.prFor(r)
	if code != 0 {
		writeErr(w, code, "not_found", msg)
		return
	}
	out := map[string]any{"mode": "github", "orchestrated": v.Orchestrated, "pr": nil, "summary": v.PR,
		"can": ghsnapshot.PRAbilities{}, "live": false}
	if v.Orchestrated && v.Mode == "local" {
		out["mode"] = "local"
		writeJSON(w, 200, out)
		return
	}
	gh := s.github()
	if t == nil || !gh.Enabled() {
		writeJSON(w, 200, out)
		return
	}
	d, err := gh.FetchPRDetail(r.Context(), t.installation, t.owner, t.repo, t.number)
	if err != nil {
		out["error"] = err.Error()
		writeJSON(w, 200, out)
		return
	}
	perms, _ := gh.Permissions(r.Context(), t.installation)
	can := ghsnapshot.AbilitiesFrom(perms)
	if v.Orchestrated {
		// Merging an orchestrated issue belongs to the engine (after Accept).
		can.Merge = false
	}
	out["pr"], out["can"], out["live"] = d, can, true
	writeJSON(w, 200, out)
}

// hPROp runs one PR card action through the GitHub App.
func (s *Service) hPROp(op string) http.HandlerFunc {
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
		if v.Orchestrated && v.Mode == "local" {
			writeErr(w, 409, "local_mode", "local-mode team has no GitHub PR")
			return
		}
		if op == "merge" && v.Orchestrated {
			writeErr(w, 409, "engine_owned", "the orchestrator merges this issue after acceptance; use Accept")
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
		allowed := map[string]bool{"update-branch": can.UpdateBranch, "rerun-checks": can.Rerun, "merge": can.Merge}[op]
		if !allowed {
			writeErr(w, 403, "github_permission", "the GitHub App installation lacks permission for "+op)
			return
		}
		// The head the member saw on the card (optimistic concurrency); the
		// stored snapshot lags pushes, so without one use the live head.
		var body struct {
			HeadSHA string `json:"head_sha"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body)
		head := strings.TrimSpace(body.HeadSHA)
		if head == "" {
			hd, herr := gh.FetchPRHead(r.Context(), t.installation, t.owner, t.repo, t.number)
			if herr != nil {
				writeGHErr(w, herr)
				return
			}
			head = hd
		}
		res := map[string]any{"op": op, "ok": true}
		switch op {
		case "update-branch":
			err = gh.UpdateBranch(r.Context(), t.installation, t.owner, t.repo, t.number, head)
		case "rerun-checks":
			var n int
			n, err = gh.RerunFailedChecks(r.Context(), t.installation, t.owner, t.repo, head)
			res["rerun"] = n
		case "merge":
			var sha string
			sha, err = gh.SquashMerge(r.Context(), t.installation, t.owner, t.repo, t.number, head)
			res["sha"] = sha
		}
		if err != nil {
			writeGHErr(w, err)
			return
		}
		go s.kickIssue(v.ID, "pr:"+op)
		writeJSON(w, 200, res)
	}
}

func writeGHErr(w http.ResponseWriter, err error) {
	var ae *ghsnapshot.APIError
	if errors.As(err, &ae) {
		code := 502
		if ae.Status == 403 || ae.Status == 404 || ae.Status == 405 || ae.Status == 409 || ae.Status == 422 {
			code = 409
		}
		writeErr(w, code, "github", ae.Message)
		return
	}
	writeErr(w, 502, "github", err.Error())
}

func (s *Service) kickIssue(issueID, why string) {
	_ = s.st.enqueue(context.Background(), issueID, why)
	s.Kick()
}
