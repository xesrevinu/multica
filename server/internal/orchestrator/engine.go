package orchestrator

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Issue is the engine's view of one issue.
type Issue struct {
	ID, Key, Title, Status string
	ParentID               string
	Stage                  int
	AssigneeType           string // agent | member | ""
	Assignee               string // agent name (for agents) or user id (members)
	Labels                 []string
	Meta                   map[string]string
	UpdatedAt              time.Time
}

// Run is the latest or active agent task of an issue.
type Run struct {
	ID        string
	Status    string // queued, dispatched, running, completed, failed, cancelled
	StartedAt *time.Time
	// LastActivity is the newest task_message time (nil if none yet). A run whose
	// agent is stuck inside one tool call (e.g. an rg over $HOME) emits nothing.
	LastActivity *time.Time
}

// Snapshot is everything Decide may look at. Collected by the runtime.
type Snapshot struct {
	Now       time.Time
	Issue     Issue
	ActiveRun *Run            // queued/dispatched/running task, if any
	LastRun   *Run            // newest finished task, if any
	Head      string          // current sha of the issue branch ("" = not pushed)
	Siblings  []Issue         // same project, for deps / stage barrier
	Busy      map[string]bool // owner agents with todo/in_progress work across the workspace
	FreeGB    int
	// SamePatch reports whether two commits carry the same change (git
	// patch-id of each commit's diff against its merge-base with main). nil
	// or false = unknown; the engine then falls back to a fresh review.
	SamePatch func(a, b string) bool
}

// ProbeResult is the outcome of a side-effecting gate (pre-check or merge).
type ProbeResult struct {
	OK      bool
	Kind    string // dry | merged | already_merged | conflict | checks_red | pending | stale | push_failed | no_pr
	Detail  string
	Pending bool
	SHA     string
	PR      string // PR url when the probe opened/found one (github precheck)
	PRNum   string
}

// Prober runs gates. merge=false is the review pre-check (never pushes).
type Prober func(s *Snapshot, merge bool) ProbeResult

// Decision is one transition. Status/Assignee empty = unchanged.
type Decision struct {
	Tid          string            `json:"tid"`
	Status       string            `json:"status,omitempty"`
	AssigneeKind string            `json:"assignee_kind,omitempty"` // agent | member
	Assignee     string            `json:"assignee,omitempty"`      // agent name or "@human"
	Meta         map[string]string `json:"meta,omitempty"`
	Rerun        bool              `json:"rerun,omitempty"`
	CancelRun    string            `json:"cancel_run,omitempty"`
	Notify       string            `json:"notify,omitempty"` // human inbox text
	Why          string            `json:"why,omitempty"`
	Wait         string            `json:"wait,omitempty"` // no transition; why we are waiting
}

// StartsRun reports whether applying d dispatches an agent run.
func (d *Decision) StartsRun() bool {
	return d.Rerun || (d.AssigneeKind == "agent" && d.Status != StDone && d.Status != StCancelled && d.Status != StBlocked)
}

// phaseStatus: phases only the engine writes → statuses they allow. A status
// outside this set means a crash mid-transition or an out-of-band edit (T9).
var phaseStatus = map[string][]string{
	PhImplementing:     {StTodo, StInProgress},
	PhChangesRequested: {StTodo, StInProgress},
	PhReviewing:        {StInReview},
	PhAwaitAcceptance:  {StInReview},
	PhMerged:           {StDone},
	PhCancelled:        {StCancelled},
}

func num(s string) int  { n, _ := strconv.Atoi(strings.TrimSpace(s)); return n }
func itoa(n int) string { return strconv.Itoa(n) }

// NeedsAcceptance reports whether the issue is gated on a human acceptance.
func (sp *Spec) NeedsAcceptance(i *Issue) bool {
	if v := i.Meta["acceptance_required"]; v == "true" || v == "1" || v == "yes" {
		return true
	}
	return hasLabel(i.Labels, sp.Acceptance.Labels) || hasLabel(i.Labels, sp.Acceptance.DefaultForLabels)
}

func hasLabel(have, want []string) bool {
	for _, l := range have {
		for _, a := range want {
			if strings.EqualFold(l, a) {
				return true
			}
		}
	}
	return false
}

// OwnerPool is the owner list for the issue: the first route whose labels
// match, else roles.owners.
func (sp *Spec) OwnerPool(i *Issue) []string {
	for _, r := range sp.Roles.Routes {
		if hasLabel(i.Labels, r.Labels) {
			return r.Owners
		}
	}
	return sp.Roles.Owners
}

// slotWait returns a wait reason when a needs:<slot> resource is full.
func (sp *Spec) slotWait(s *Snapshot) string {
	for _, l := range s.Issue.Labels {
		name, ok := strings.CutPrefix(strings.ToLower(l), "needs:")
		if !ok {
			continue
		}
		limit, ok := sp.Limits.Slots[name]
		if !ok {
			continue
		}
		used := 0
		for _, x := range s.Siblings {
			if x.Key != s.Issue.Key && (x.Status == StTodo || x.Status == StInProgress) && hasLabel(x.Labels, []string{"needs:" + name}) {
				used++
			}
		}
		if used >= limit {
			return fmt.Sprintf("%s slots %d/%d", name, used, limit)
		}
	}
	return ""
}

func (sp *Spec) isOwner(name string) bool {
	for _, o := range sp.Roles.Owners {
		if o == name {
			return true
		}
	}
	return false
}

func (sp *Spec) block(s *Snapshot, why string, extra map[string]string) *Decision {
	m := map[string]string{"phase": PhBlocked, "blocked_reason": why}
	for k, v := range extra {
		m[k] = v
	}
	return &Decision{Tid: "T6_block", Status: StBlocked, AssigneeKind: "agent", Assignee: sp.Roles.Lead, Meta: m, Why: why,
		Notify: fmt.Sprintf("%s blocked: %s", s.Issue.Key, why)}
}

func (sp *Spec) rework(s *Snapshot, why string, extra map[string]string) *Decision {
	m := s.Issue.Meta
	n := num(m["attempt_impl"]) + 1
	if n > sp.Limits.Impl {
		return sp.block(s, "impl attempts exhausted: "+why, nil)
	}
	meta := map[string]string{"phase": PhChangesRequested, "attempt_impl": itoa(n), "verdict": "", "blocked_reason": why}
	for k, v := range extra {
		meta[k] = v
	}
	owner := m["owner"]
	if owner == "" {
		owner = s.Issue.Assignee
	}
	return &Decision{Tid: "T3_rework", Status: StTodo, AssigneeKind: "agent", Assignee: owner, Meta: meta, Why: why}
}

func short(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n]
	}
	return s
}

// Decide is the whole machine: given a snapshot it returns at most one
// transition (nil = nothing to do). It is deterministic in (snapshot, probe).
func (sp *Spec) Decide(s *Snapshot, probe Prober) *Decision {
	i := &s.Issue
	m := i.Meta
	if m == nil {
		m = map[string]string{}
		i.Meta = m
	}
	st := i.Status
	if st == StDone || st == StCancelled {
		return nil
	}
	// T9: converge status to the engine-owned phase.
	if ok, has := phaseStatus[m["phase"]]; has && !contains(ok, st) {
		target := ok[0]
		d := &Decision{Tid: "T9_reconcile", Status: target, Why: fmt.Sprintf("status %s inconsistent with phase %s", st, m["phase"])}
		switch {
		case target == StTodo && m["owner"] != "" && i.Assignee != m["owner"]:
			d.AssigneeKind, d.Assignee = "agent", m["owner"]
		case target == StInReview && m["phase"] == PhAwaitAcceptance:
			d.AssigneeKind, d.Assignee = "member", "@human"
		case target == StInReview && i.Assignee != sp.Roles.Reviewer:
			d.AssigneeKind, d.Assignee = "agent", sp.Roles.Reviewer
		}
		return d
	}
	switch st {
	case StBacklog:
		return sp.decideBacklog(s)
	case StBlocked:
		switch m["phase"] {
		case PhUnblocked:
			owner := m["owner"]
			if owner == "" {
				owner = sp.OwnerPool(i)[0]
			}
			return &Decision{Tid: "T7_unblock", Status: StTodo, AssigneeKind: "agent", Assignee: owner,
				Meta: map[string]string{"phase": PhImplementing, "attempt_impl": "1", "attempt_review": "0", "attempt_infra": "0", "blocked_reason": "", "verdict": "", "acceptance": ""}}
		case PhCancelProposed:
			return &Decision{Tid: "T8_cancel", Status: StCancelled, Meta: map[string]string{"phase": PhCancelled}}
		}
		return &Decision{Wait: "blocked: waiting for phase=unblocked or phase=cancel_proposed (lead) or multica flow unblock"}
	}
	// todo / in_progress / in_review
	if s.ActiveRun != nil {
		if r := s.ActiveRun; r.StartedAt != nil && s.Now.Sub(*r.StartedAt) > time.Duration(sp.Limits.StuckMin)*time.Minute {
			return &Decision{Tid: "T5b_cancel_stuck", CancelRun: r.ID, Why: fmt.Sprintf("run %s running %dm > %dm", r.ID[:min(8, len(r.ID))], int(s.Now.Sub(*r.StartedAt).Minutes()), sp.Limits.StuckMin)}
		}
		if r := s.ActiveRun; r.Status == "running" && r.StartedAt != nil && sp.Limits.IdleMin > 0 {
			last := *r.StartedAt
			if r.LastActivity != nil && r.LastActivity.After(last) {
				last = *r.LastActivity
			}
			if idle := s.Now.Sub(last); idle > time.Duration(sp.Limits.IdleMin)*time.Minute {
				return &Decision{Tid: "T5b_cancel_stuck", CancelRun: r.ID, Why: fmt.Sprintf("run %s no tool output for %dm > idle_min %dm", r.ID[:min(8, len(r.ID))], int(idle.Minutes()), sp.Limits.IdleMin)}
			}
		}
		return nil
	}
	if m["phase"] == PhBlocked {
		why := m["blocked_reason"]
		if why == "" {
			why = "agent set phase=blocked"
		}
		return sp.block(s, why, nil)
	}
	head := s.Head
	if st == StTodo || st == StInProgress {
		if m["phase"] == PhReviewRequested {
			if head == "" {
				return &Decision{Tid: "T5_rerun_nobranch", Rerun: true, Why: "phase=review_requested but branch not pushed",
					Meta: map[string]string{"phase": PhImplementing, "attempt_infra": itoa(num(m["attempt_infra"]) + 1)}}
			}
			if num(m["attempt_review"]) >= sp.Limits.Review {
				return sp.block(s, "review attempts exhausted", nil)
			}
			extra := map[string]string{}
			if probe != nil {
				pc := probe(s, false)
				if !pc.OK && pc.Kind != "push_failed" && !pc.Pending {
					why := "precheck " + pc.Kind + ": " + short(pc.Detail, 200)
					return sp.rework(s, why, map[string]string{"head_sha": head})
				}
				if pc.PR != "" {
					extra["pr"], extra["pr_number"] = pc.PR, pc.PRNum
				}
			}
			meta := map[string]string{"phase": PhReviewing, "head_sha": head, "review_started_sha": head, "verdict": "", "reviewed_sha": "", "attempt_review": itoa(num(m["attempt_review"]) + 1)}
			for k, v := range extra {
				meta[k] = v
			}
			return &Decision{Tid: "T2_review", Status: StInReview, AssigneeKind: "agent", Assignee: sp.Roles.Reviewer, Meta: meta}
		}
		if i.AssigneeType != "agent" || !sp.isOwner(i.Assignee) {
			return nil // not ours to drive (e.g. a member picked it up)
		}
		failed := s.LastRun != nil && (s.LastRun.Status == "failed" || s.LastRun.Status == "cancelled")
		impl, infra := num(m["attempt_impl"]), num(m["attempt_infra"])
		if failed {
			infra++
		} else {
			impl++
		}
		if impl > sp.Limits.Impl || infra > sp.Limits.Infra {
			why := fmt.Sprintf("impl attempts exhausted (%d/%d)", impl-1, sp.Limits.Impl)
			if failed {
				why = fmt.Sprintf("infra attempts exhausted (%d/%d)", infra-1, sp.Limits.Infra)
			}
			return sp.block(s, why, map[string]string{"attempt_impl": itoa(min(impl, sp.Limits.Impl)), "attempt_infra": itoa(min(infra, sp.Limits.Infra))})
		}
		if s.LastRun == nil {
			return &Decision{Tid: "T5_rerun_norun", Rerun: true}
		}
		return &Decision{Tid: "T5_rerun", Rerun: true, Why: "last run " + s.LastRun.Status + " without hand-over",
			Meta: map[string]string{"attempt_impl": itoa(impl), "attempt_infra": itoa(infra)}}
	}
	// in_review
	if m["phase"] == PhAwaitAcceptance {
		switch a := m["acceptance"]; {
		case a == "ok":
			return sp.merge(s, probe)
		case strings.HasPrefix(a, "reject"):
			why := "acceptance rejected"
			if r := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(a, "reject"), ":")); r != "" {
				why += ": " + r
			}
			return sp.rework(s, why, map[string]string{"acceptance": ""})
		}
		if head != "" && m["reviewed_sha"] != head {
			if d := sp.carry(s, head); d != nil {
				return d
			}
			return sp.rereview(s, head, "head moved during acceptance")
		}
		return &Decision{Wait: "awaiting human acceptance"}
	}
	v := m["verdict"]
	if v == "" {
		// A reviewer run that failed or was cancelled (e.g. the daemon could
		// not prepare its worktree) is an infra attempt, not a review attempt.
		if r := s.LastRun; r != nil && (r.Status == "failed" || r.Status == "cancelled") {
			n := num(m["attempt_infra"]) + 1
			if n > sp.Limits.Infra {
				return sp.block(s, fmt.Sprintf("infra attempts exhausted on review (%d/%d)", n-1, sp.Limits.Infra), nil)
			}
			return &Decision{Tid: "T3b_rereview", Rerun: true, Meta: map[string]string{"attempt_infra": itoa(n)}, Why: "reviewer run " + r.Status + " (infra)"}
		}
		n := num(m["attempt_review"]) + 1
		if n > sp.Limits.Review {
			return sp.block(s, "review attempts exhausted (no verdict)", nil)
		}
		return &Decision{Tid: "T3b_rereview", Rerun: true, Meta: map[string]string{"attempt_review": itoa(n)}, Why: "reviewer run ended without verdict"}
	}
	if v == "approve" && (head == "" || m["reviewed_sha"] != head) {
		if d := sp.carry(s, head); d != nil {
			return d
		}
		return sp.rereview(s, head, "reviewed_sha != head")
	}
	if v != "approve" {
		return sp.rework(s, "reviewer verdict="+v, nil)
	}
	if sp.NeedsAcceptance(i) {
		// gate after checks/mergeability are green so the human sees a mergeable PR.
		if probe != nil {
			pc := probe(s, false)
			if pc.Pending {
				return &Decision{Wait: pc.Detail}
			}
			if !pc.OK && pc.Kind != "push_failed" && pc.Kind != "stale" {
				return sp.rework(s, pc.Kind+": "+short(pc.Detail, 200), nil)
			}
		}
		return &Decision{Tid: "T10_await_acceptance", AssigneeKind: "member", Assignee: "@human",
			Meta:   map[string]string{"phase": PhAwaitAcceptance, "acceptance": ""},
			Notify: fmt.Sprintf("%s (%s) is approved at %s and waits for your acceptance: `multica flow accept %s` or `multica flow reject %s --reason ...`", i.Key, i.Title, short(head, 12), i.Key, i.Key)}
	}
	return sp.merge(s, probe)
}

// carry keeps an approve verdict across a pure rebase / main merge: when the
// new head carries exactly the reviewed change (same patch-id), the verdict
// moves to the new head instead of spending another review round.
func (sp *Spec) carry(s *Snapshot, head string) *Decision {
	old := s.Issue.Meta["reviewed_sha"]
	if head == "" || old == "" || s.SamePatch == nil || !s.SamePatch(old, head) {
		return nil
	}
	return &Decision{Tid: "T3c_carry_verdict", Meta: map[string]string{"reviewed_sha": head, "head_sha": head, "verdict_carried_from": old},
		Why: fmt.Sprintf("same patch-id as reviewed %s (pure rebase)", short(old, 12))}
}

func (sp *Spec) rereview(s *Snapshot, head, why string) *Decision {
	n := num(s.Issue.Meta["attempt_review"]) + 1
	if n > sp.Limits.Review {
		return sp.block(s, "review attempts exhausted (stale)", nil)
	}
	return &Decision{Tid: "T3b_rereview", AssigneeKind: "agent", Assignee: sp.Roles.Reviewer, Status: StInReview, Rerun: s.Issue.Assignee == sp.Roles.Reviewer,
		Meta: map[string]string{"phase": PhReviewing, "verdict": "", "reviewed_sha": "", "review_started_sha": head, "head_sha": head, "attempt_review": itoa(n)}, Why: why}
}

func (sp *Spec) merge(s *Snapshot, probe Prober) *Decision {
	if probe == nil {
		return &Decision{Wait: "no prober"}
	}
	r := probe(s, true)
	switch {
	case r.Pending:
		return &Decision{Wait: r.Detail}
	case r.Kind == "stale" || r.Kind == "push_failed":
		return &Decision{Wait: r.Kind + ": " + r.Detail}
	case r.OK:
		return &Decision{Tid: "T4_done", Status: StDone, Meta: map[string]string{"phase": PhMerged, "merged_sha": r.SHA}}
	}
	return sp.rework(s, r.Kind+": "+short(r.Detail, 200), nil)
}

func (sp *Spec) decideBacklog(s *Snapshot) *Decision {
	i := &s.Issue
	m := i.Meta
	if m["phase"] == PhPlanned {
		return &Decision{Wait: "planned epic: its sub-issues carry the work"}
	}
	doneish := func(st string) bool { return st == StDone || st == StCancelled }
	by := map[string]Issue{}
	for _, x := range s.Siblings {
		by[x.Key] = x
	}
	for _, d := range strings.Split(m["deps"], ",") {
		if d = strings.TrimSpace(d); d != "" && !doneish(by[d].Status) {
			return nil
		}
	}
	if i.ParentID != "" && i.Stage > 0 {
		for _, x := range s.Siblings {
			if x.ParentID == i.ParentID && x.Stage > 0 && x.Stage < i.Stage && !doneish(x.Status) {
				return nil
			}
		}
	}
	if s.FreeGB >= 0 && s.FreeGB < sp.Limits.MinFreeGB {
		return &Decision{Wait: fmt.Sprintf("disk guard: %dGB free < %dGB", s.FreeGB, sp.Limits.MinFreeGB)}
	}
	if w := sp.slotWait(s); w != "" {
		return &Decision{Wait: "waiting: " + w}
	}
	owner := ""
	if o := m["owner"]; o != "" {
		if s.Busy[o] {
			return &Decision{Wait: "owner " + o + " busy"}
		}
		owner = o
	} else {
		for _, o := range sp.OwnerPool(i) {
			if !s.Busy[o] {
				owner = o
				break
			}
		}
	}
	if owner == "" {
		return &Decision{Wait: "all owners busy"}
	}
	return &Decision{Tid: "T1_start", Status: StTodo, AssigneeKind: "agent", Assignee: owner,
		Meta: map[string]string{"owner": owner, "phase": PhImplementing, "attempt_impl": "1", "attempt_review": "0", "attempt_infra": "0"}}
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}
