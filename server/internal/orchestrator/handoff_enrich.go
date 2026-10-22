package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/ghsnapshot"
)

// enrichHandoff adds what the snapshot does not carry: the reviewer's
// findings (issue comments since the review started + GitHub reviews/inline
// comments), failing check runs with annotations and output excerpt, and a
// diff summary of the previous head. Best effort and bounded: a GitHub or DB
// error leaves the field empty, it never fails the transition.
func (s *Service) enrichHandoff(ctx context.Context, t *Team, row *issueRow, h *Handoff) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	sp := t.Spec
	if h.Kind == HKReviewChanges || h.Kind == HKAcceptanceReject {
		h.Review = append(h.Review, s.reviewerComments(ctx, t, row)...)
	}
	var g *ghRepo
	if sp.Repo.Mode != "local" && s.github().Enabled() {
		if r, ok := s.repo.(*repoOps); ok {
			g, _ = r.ghRepo(ctx, sp)
		}
	}
	prNum, _ := strconv.Atoi(row.Meta["pr_number"])
	if g != nil && prNum > 0 {
		if h.Kind == HKReviewChanges || h.Kind == HKAcceptanceReject {
			if d, err := g.c.FetchPRDetail(ctx, g.inst, g.owner, g.name, prNum); err == nil {
				for _, rv := range d.Reviews {
					if strings.TrimSpace(rv.Body) != "" && rv.State != "APPROVED" {
						h.Review = append(h.Review, HandoffComment{Source: "github_review", Author: rv.User, Body: short(rv.Body, 1500)})
					}
				}
				for _, c := range d.ReviewComments {
					if c.InReplyTo == 0 {
						h.Review = append(h.Review, HandoffComment{Source: "github_inline", Author: c.User, Path: c.Path, Line: c.Line, Body: short(c.Body, 1000)})
					}
				}
			}
		}
		if files, err := g.c.PRFiles(ctx, g.inst, g.owner, g.name, prNum); err == nil {
			h.DiffSummary = diffSummary(files)
		}
	}
	if len(h.Review) > 12 {
		h.Review = h.Review[len(h.Review)-12:]
	}
	if g != nil && h.PrevHead != "" && (h.Kind == HKCIFailure || h.Kind == HKCheckFailure) {
		h.Checks = failingChecks(ctx, g, h.PrevHead, sp.Repo.RequiredChecks)
	}
	if h.DiffSummary == "" && sp.Repo.Hub != "" && h.PrevHead != "" {
		o := run(ctx, sp.Repo.Hub, "git", "diff", "--shortstat", sp.Repo.Main+"..."+h.PrevHead)
		if o.code == 0 {
			h.DiffSummary = strings.TrimSpace(o.out)
		}
	}
	slog.Info("orchestrator: handoff", "issue", row.Key, "kind", h.Kind, "review", len(h.Review), "checks", len(h.Checks))
}

// reviewerComments: the reviewer agent's issue comments since the latest
// T2_review / T3b_rereview (its findings on this round), newest last.
func (s *Service) reviewerComments(ctx context.Context, t *Team, row *issueRow) []HandoffComment {
	rid, err := s.st.agentID(ctx, t.WorkspaceID, t.Spec.Roles.Reviewer)
	if err != nil {
		return nil
	}
	rows, err := s.opt.Pool.QueryContext(ctx, `SELECT content FROM comment WHERE issue_id=$1 AND author_type='agent' AND author_id=$2 AND deleted_at IS NULL
		AND created_at >= COALESCE((SELECT max(created_at) FROM orchestrator_transition WHERE issue_id=$1 AND tid IN ('T2_review','T3b_rereview')), 'epoch')
		ORDER BY created_at DESC LIMIT 5`, row.ID, rid)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []HandoffComment
	for rows.Next() {
		var c string
		if rows.Scan(&c) == nil && strings.TrimSpace(c) != "" {
			out = append([]HandoffComment{{Source: "issue", Author: t.Spec.Roles.Reviewer, Body: short(c, 2500)}}, out...)
		}
	}
	return out
}

func diffSummary(files []ghsnapshot.PRFile) string {
	add, del := 0, 0
	var names []string
	for i, f := range files {
		add += f.Additions
		del += f.Deletions
		if i < 15 {
			names = append(names, fmt.Sprintf("%s (+%d -%d)", f.Filename, f.Additions, f.Deletions))
		}
	}
	s := fmt.Sprintf("%d files, +%d -%d: %s", len(files), add, del, strings.Join(names, ", "))
	if len(files) > 15 {
		s += fmt.Sprintf(", … %d more", len(files)-15)
	}
	return s
}

// failingChecks lists red check runs on sha (required ones first, at most 5)
// with their annotations and output text excerpt.
func failingChecks(ctx context.Context, g *ghRepo, sha string, required []string) []HandoffCheck {
	var runs struct {
		CheckRuns []struct {
			ID         int64  `json:"id"`
			Name       string `json:"name"`
			Conclusion string `json:"conclusion"`
			HTMLURL    string `json:"html_url"`
			Output     struct {
				Title   string `json:"title"`
				Summary string `json:"summary"`
				Text    string `json:"text"`
			} `json:"output"`
		} `json:"check_runs"`
	}
	if err := g.do(ctx, http.MethodGet, "/commits/"+sha+"/check-runs?per_page=100&filter=latest", nil, &runs); err != nil {
		return nil
	}
	var out []HandoffCheck
	for pass := 0; pass < 2; pass++ {
		for _, c := range runs.CheckRuns {
			switch c.Conclusion {
			case "failure", "timed_out", "cancelled", "action_required":
			default:
				continue
			}
			if (pass == 0) != contains(required, c.Name) || len(out) >= 5 {
				continue
			}
			hc := HandoffCheck{Name: c.Name, Conclusion: c.Conclusion, URL: c.HTMLURL}
			var ann []struct {
				Path      string `json:"path"`
				StartLine int    `json:"start_line"`
				Level     string `json:"annotation_level"`
				Message   string `json:"message"`
			}
			if g.do(ctx, http.MethodGet, fmt.Sprintf("/check-runs/%d/annotations?per_page=10", c.ID), nil, &ann) == nil {
				for _, a := range ann {
					hc.Annotations = append(hc.Annotations, fmt.Sprintf("%s %s:%d %s", a.Level, a.Path, a.StartLine, a.Message))
				}
			}
			hc.LogExcerpt = strings.TrimSpace(strings.Join([]string{c.Output.Title, c.Output.Summary, c.Output.Text}, "\n"))
			out = append(out, hc)
		}
	}
	return out
}

// HandoffBrief is the handoff block for a run the daemon is claiming on
// issueID by agentName: the newest run-starting transition's handoff, when it
// was addressed to that agent. "" otherwise (normal start, stale handoff).
func (s *Service) HandoffBrief(ctx context.Context, issueID, agentName string) string {
	if s == nil || s.opt.Pool == nil {
		return ""
	}
	var raw []byte
	err := s.opt.Pool.QueryRowContext(ctx, `SELECT decision FROM orchestrator_transition WHERE issue_id=$1 AND state IN ('started','done')
		AND (decision->>'rerun' = 'true' OR decision->>'assignee_kind' = 'agent') ORDER BY id DESC LIMIT 1`, issueID).Scan(&raw)
	if err != nil {
		return ""
	}
	var d Decision
	if json.Unmarshal(raw, &d) != nil || d.Handoff == nil {
		return ""
	}
	if d.Handoff.To != "" && agentName != "" && d.Handoff.To != agentName {
		return ""
	}
	return d.Handoff.Render()
}
