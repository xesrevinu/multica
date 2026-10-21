package orchestrator

import (
	"fmt"
	"sort"
	"strings"
)

// Handoff is the structured context the next assignee's run receives when an
// issue is sent back or re-run after a failure (review reject, check / CI
// failure, run failure, provider limit/auth, worktree collision, dead-letter,
// unblock). The engine fills the pure part from the snapshot; the runtime
// enriches it (review comments, failing check annotations, diff summary)
// before the transition row is written, so it is persisted on
// orchestrator_transition.decision->'handoff', rendered into the claimed
// run's prompt, and shown as a card in the issue chat.
type Handoff struct {
	Kind          string            `json:"kind"`
	FailureReason string            `json:"failure_reason,omitempty"` // agent_task_queue.failure_reason of the previous run
	Summary       string            `json:"summary"`
	To            string            `json:"to,omitempty"`   // agent name of the next assignee
	Role          string            `json:"role,omitempty"` // owner | reviewer | lead
	Attempts      map[string]int    `json:"attempts"`
	Attempt       int               `json:"attempt"` // the attempt counter the kind consumes (impl for rework, infra for run failures, ...)
	PrevHead      string            `json:"prev_head,omitempty"`
	ReviewedSHA   string            `json:"reviewed_sha,omitempty"`
	PR            string            `json:"pr,omitempty"`
	PrevRun       *HandoffRun       `json:"prev_run,omitempty"`
	DiffSummary   string            `json:"diff_summary,omitempty"`
	Review        []HandoffComment  `json:"review_comments,omitempty"`
	Checks        []HandoffCheck    `json:"checks,omitempty"`
	Extra         map[string]string `json:"extra,omitempty"`
}

type HandoffRun struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"` // tail excerpt
}

type HandoffComment struct {
	Source string `json:"source"` // issue | github_review | github_inline
	Author string `json:"author,omitempty"`
	Path   string `json:"path,omitempty"`
	Line   int    `json:"line,omitempty"`
	Body   string `json:"body"`
}

type HandoffCheck struct {
	Name        string   `json:"name"`
	Conclusion  string   `json:"conclusion"`
	URL         string   `json:"url,omitempty"`
	Annotations []string `json:"annotations,omitempty"`
	LogExcerpt  string   `json:"log_excerpt,omitempty"`
}

// Handoff kinds (failure_reason classification shown to the next agent).
const (
	HKReviewChanges     = "review_changes"
	HKCheckFailure      = "check_failure" // pre-merge check_cmd / required check red
	HKCIFailure         = "ci_failure"
	HKConflict          = "conflict"
	HKAcceptanceReject  = "acceptance_reject"
	HKRunFailure        = "run_failure"
	HKRunCancelled      = "run_cancelled"
	HKNoHandover        = "no_handover"
	HKProviderLimit     = "provider_limit"
	HKProviderAuth      = "provider_auth"
	HKWorktreeCollision = "worktree_collision"
	HKAttemptsExhausted = "attempts_exhausted"
	HKBranchMoved       = "branch_moved"
	HKDeadLetter        = "dead_letter"
	HKAgentBlocked      = "agent_blocked"
	HKUnblock           = "unblock"
)

// handoffTids are the transitions that send an issue back or re-run it
// because something went wrong.
var handoffTids = map[string]bool{"T3_rework": true, "T5_rerun": true, "T5q_provider_limit": true, "T6_block": true, "T7_unblock": true, "T3b_rereview": true}

// BuildHandoff derives the handoff for d from the snapshot. Pure. nil when d
// is not a send-back / failure re-run (e.g. a first start, a normal review).
func (sp *Spec) BuildHandoff(s *Snapshot, d *Decision) *Handoff {
	if d == nil || !handoffTids[d.Tid] {
		return nil
	}
	m := s.Issue.Meta
	h := &Handoff{Summary: d.Why, PrevHead: firstNonEmpty(s.Head, m["head_sha"]), ReviewedSHA: firstNonEmpty(m["reviewed_sha"], m["last_reviewed_sha"]), PR: m["pr"]}
	if r := s.LastRun; r != nil && s.ActiveRun == nil {
		h.PrevRun = &HandoffRun{ID: r.ID, Status: r.Status, Error: tail(r.Error, 1500)}
		if r.Status == "failed" {
			h.FailureReason = r.Reason
		}
	}
	h.Attempts = map[string]int{}
	for _, k := range []string{"attempt_impl", "attempt_review", "attempt_infra", "attempt_quota", "unblock_count"} {
		v := m[k]
		if nv, ok := d.Meta[k]; ok {
			v = nv
		}
		h.Attempts[strings.TrimPrefix(k, "attempt_")] = num(v)
	}
	why := strings.ToLower(d.Why)
	switch d.Tid {
	case "T3_rework":
		h.Role = "owner"
		h.Attempt = h.Attempts["impl"]
		switch {
		case strings.Contains(why, "verdict=changes"):
			h.Kind = HKReviewChanges
		case strings.HasPrefix(why, "acceptance rejected"):
			h.Kind = HKAcceptanceReject
		case strings.Contains(why, "checks_red"):
			h.Kind = HKCIFailure
		case strings.Contains(why, "conflict"):
			h.Kind = HKConflict
		default:
			h.Kind = HKCheckFailure
		}
		if h.Kind == HKReviewChanges {
			h.PrevRun = nil // the reviewer's run succeeded; its findings are the context
		}
	case "T5_rerun":
		h.Role = "owner"
		switch {
		case s.LastRun != nil && s.LastRun.Status == "failed":
			h.Kind, h.Attempt = HKRunFailure, h.Attempts["infra"]
		case s.LastRun != nil && s.LastRun.Status == "cancelled":
			h.Kind, h.Attempt = HKRunCancelled, h.Attempts["infra"]
		default:
			h.Kind, h.Attempt = HKNoHandover, h.Attempts["impl"]
		}
	case "T5q_provider_limit":
		h.Kind, h.Attempt = HKProviderLimit, h.Attempts["quota"]
		h.Role = roleOf(sp, s.Issue.Assignee)
	case "T3b_rereview":
		r := s.LastRun
		if r == nil || (r.Status != "failed" && r.Status != "cancelled") {
			return nil // ordinary re-review (no verdict / head moved): not a failure
		}
		h.Kind, h.Role, h.Attempt = HKRunFailure, "reviewer", h.Attempts["infra"]
	case "T6_block":
		h.Role = "lead"
		switch {
		case strings.HasPrefix(why, "dead-letter"):
			h.Kind = HKDeadLetter
		case strings.Contains(why, "provider auth"):
			h.Kind = HKProviderAuth
		case strings.Contains(why, "checked out by another worktree"):
			h.Kind = HKWorktreeCollision
		case strings.Contains(why, "attempts exhausted"):
			h.Kind = HKAttemptsExhausted
		case strings.Contains(why, "branch moved during review"):
			h.Kind = HKBranchMoved
		default:
			h.Kind = HKAgentBlocked
		}
	case "T7_unblock":
		h.Kind, h.Role = HKUnblock, "owner"
		h.Summary = "unblocked after: " + m["blocked_reason"]
		if note := m["unblock_note"]; note != "" {
			h.Extra = map[string]string{"unblock_note": note}
		}
		// the counters were just reset; keep what the issue went through.
		for _, k := range []string{"impl", "review", "infra"} {
			h.Attempts["prior_"+k] = num(m["attempt_"+k])
		}
		h.PrevRun = nil
	}
	h.To = d.Assignee
	if h.To == "" {
		h.To = s.Issue.Assignee
	}
	return h
}

func roleOf(sp *Spec, name string) string {
	switch {
	case name == sp.Roles.Reviewer:
		return "reviewer"
	case name == sp.Roles.Lead:
		return "lead"
	}
	return "owner"
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

// HandoffBriefBudget caps the rendered handoff block (bytes) so a noisy CI
// log cannot crowd the agent's prompt.
const HandoffBriefBudget = 6000

// Render is the markdown block injected into the next run's prompt.
func (h *Handoff) Render() string {
	if h == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## Orchestrator handoff: %s\n", h.Kind)
	fmt.Fprintf(&b, "This issue came back to you (%s). Address this before anything else.\n\n", h.Role)
	fmt.Fprintf(&b, "- reason: %s\n", short(h.Summary, 400))
	if h.FailureReason != "" {
		fmt.Fprintf(&b, "- failure_reason: %s\n", h.FailureReason)
	}
	keys := make([]string, 0, len(h.Attempts))
	for k := range h.Attempts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var att []string
	for _, k := range keys {
		att = append(att, fmt.Sprintf("%s=%d", k, h.Attempts[k]))
	}
	fmt.Fprintf(&b, "- attempt: %d (%s)\n", h.Attempt, strings.Join(att, " "))
	if h.PrevHead != "" {
		fmt.Fprintf(&b, "- previous head: %s", short(h.PrevHead, 12))
		if h.ReviewedSHA != "" {
			fmt.Fprintf(&b, " (reviewed: %s)", short(h.ReviewedSHA, 12))
		}
		b.WriteString("\n")
	}
	if h.PR != "" {
		fmt.Fprintf(&b, "- PR: %s\n", h.PR)
	}
	for k, v := range h.Extra {
		fmt.Fprintf(&b, "- %s: %s\n", k, short(v, 300))
	}
	if h.DiffSummary != "" {
		fmt.Fprintf(&b, "\n### Previous diff\n%s\n", short(h.DiffSummary, 800))
	}
	if len(h.Review) > 0 {
		b.WriteString("\n### Review findings\n")
		for _, c := range h.Review {
			loc := ""
			if c.Path != "" {
				loc = fmt.Sprintf(" `%s:%d`", c.Path, c.Line)
			}
			fmt.Fprintf(&b, "- [%s %s]%s %s\n", c.Source, c.Author, loc, short(oneLine(c.Body), 600))
		}
	}
	if len(h.Checks) > 0 {
		b.WriteString("\n### Failing checks\n")
		for _, c := range h.Checks {
			fmt.Fprintf(&b, "- %s: %s %s\n", c.Name, c.Conclusion, c.URL)
			for _, a := range c.Annotations {
				fmt.Fprintf(&b, "  - %s\n", short(oneLine(a), 300))
			}
			if c.LogExcerpt != "" {
				fmt.Fprintf(&b, "  ```\n%s\n  ```\n", tail(c.LogExcerpt, 1200))
			}
		}
	}
	if h.PrevRun != nil && h.PrevRun.Error != "" {
		fmt.Fprintf(&b, "\n### Previous run %s (%s)\n```\n%s\n```\n", short(h.PrevRun.ID, 8), h.PrevRun.Status, tail(h.PrevRun.Error, 1000))
	}
	out := b.String()
	if len(out) > HandoffBriefBudget {
		out = strings.ToValidUTF8(out[:HandoffBriefBudget], "") + "\n…(truncated)\n"
	}
	return out
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
