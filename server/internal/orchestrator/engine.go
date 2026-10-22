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
	// Description is read only by auto-promote (its 依赖:/Depends: line).
	Description string
	Meta        map[string]string
	UpdatedAt   time.Time
}

// Run is the latest or active agent task of an issue.
type Run struct {
	ID        string
	Status    string // queued, dispatched, running, completed, failed, cancelled
	StartedAt *time.Time
	// LastActivity is the newest task_message time (nil if none yet). A run whose
	// agent is stuck inside one tool call (e.g. an rg over $HOME) emits nothing.
	LastActivity *time.Time
	// Reason is agent_task_queue.failure_reason (pkg/taskfailure), "" if none.
	Reason    string
	CreatedAt *time.Time
	EndedAt   *time.Time
	// Backend is the agent runtime's provider (gbot, claude, codex, ...),
	// "" if unknown; it selects limits.idle_min_by_backend.
	Backend string
	// Error is agent_task_queue.error of a finished run ("" if none).
	Error string
}

// Snapshot is everything Decide may look at. Collected by the runtime.
type Snapshot struct {
	Now       time.Time
	Issue     Issue
	ActiveRun *Run            // queued/dispatched/running task, if any
	LastRun   *Run            // newest finished task, if any
	Head      string          // current sha of the issue branch ("" = not pushed)
	Siblings  []Issue         // same project, for deps / stage barrier
	Busy      map[string]bool // agents (owners and experts) with todo/in_progress work across the workspace
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
	// Handoff is the structured send-back context for the next run (BuildHandoff).
	Handoff *Handoff `json:"handoff,omitempty"`
}

// StartsRun reports whether applying d dispatches an agent run.
func (d *Decision) StartsRun() bool {
	return d.Rerun || (d.AssigneeKind == "agent" && d.Status != StDone && d.Status != StCancelled && d.Status != StBlocked)
}

// phaseStatus: phases only the engine writes → statuses they allow. A status
// outside this set means a crash mid-transition or an out-of-band edit (T9).
var phaseStatus = map[string][]string{
	PhImplementing:      {StTodo, StInProgress},
	PhChangesRequested:  {StTodo, StInProgress},
	PhReviewing:         {StInReview},
	PhAwaitAcceptance:   {StInReview},
	PhResolvingConflict: {StInReview},
	PhMerged:            {StDone},
	PhCancelled:         {StCancelled},
}

func num(s string) int  { n, _ := strconv.Atoi(strings.TrimSpace(s)); return n }
func itoa(n int) string { return strconv.Itoa(n) }

// NeedsAcceptance reports whether the issue is gated on a human acceptance.
func (sp *Spec) NeedsAcceptance(i *Issue) bool {
	if v := i.Meta["acceptance_required"]; v == "true" || v == "1" || v == "yes" {
		return true
	}
	// risk is the reviewer's tier for the change (low|medium|high); the change
	// sets the tier, not the author. high always needs a person.
	if strings.EqualFold(i.Meta["risk"], "high") {
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
// match, else roles.owners. Experts are not part of this pool; use Assign.
func (sp *Spec) OwnerPool(i *Issue) []string {
	for _, r := range sp.Roles.Routes {
		if hasLabel(i.Labels, r.Labels) {
			return r.Owners
		}
	}
	return sp.Roles.Owners
}

// MatchExpert returns the first expert whose when matches, skipping any
// expert already named in expert_done. Nil when none match. An issue with
// no labels matches only an explicit metadata opt-in, never a label trigger.
func (sp *Spec) MatchExpert(i *Issue) *Expert {
	if i == nil {
		return nil
	}
	for idx := range sp.Experts {
		e := &sp.Experts[idx]
		if expertDoneHas(i.Meta["expert_done"], e.Name) || !e.matches(i) {
			continue
		}
		return e
	}
	return nil
}

func (e *Expert) matches(i *Issue) bool {
	w := e.When
	if len(w.NotLabels) > 0 && hasLabel(i.Labels, w.NotLabels) {
		return false
	}
	for k, v := range w.Metadata {
		if i.Meta[k] != v {
			return false
		}
	}
	if len(w.Labels) > 0 {
		return hasLabel(i.Labels, w.Labels)
	}
	if len(w.Metadata) > 0 {
		return true
	}
	// not_labels alone must not claim an unlabelled issue.
	return len(i.Labels) > 0
}

// Assign picks the pool for an issue and, when an expert matched, that
// expert's name. Experts win over routes; no match uses OwnerPool.
func (sp *Spec) Assign(i *Issue) (pool []string, expert string) {
	if e := sp.MatchExpert(i); e != nil {
		return e.Agents, e.Name
	}
	return sp.OwnerPool(i), ""
}

func (sp *Spec) expertByName(name string) *Expert {
	if name == "" {
		return nil
	}
	for i := range sp.Experts {
		if sp.Experts[i].Name == name {
			return &sp.Experts[i]
		}
	}
	return nil
}

// expertNameFor is the matched expert when agent is in its pool.
func (sp *Spec) expertNameFor(i *Issue, agent string) string {
	e := sp.MatchExpert(i)
	if e == nil || !contains(e.Agents, agent) {
		return ""
	}
	return e.Name
}

func expertDoneHas(cur, name string) bool {
	if name == "" {
		return false
	}
	for _, p := range strings.Split(cur, ",") {
		if strings.TrimSpace(p) == name {
			return true
		}
	}
	return false
}

func appendDone(cur, name string) string {
	if expertDoneHas(cur, name) {
		return cur
	}
	if strings.TrimSpace(cur) == "" {
		return name
	}
	return cur + "," + name
}

func cloneMeta(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
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
			started := x.Status == StInProgress || (x.Status == StTodo && x.Meta["phase"] != "" && x.Meta["phase"] != PhPlanned)
			if x.Key != s.Issue.Key && started && hasLabel(x.Labels, []string{"needs:" + name}) {
				used++
			}
		}
		if used >= limit {
			return fmt.Sprintf("%s slots %d/%d", name, used, limit)
		}
	}
	return ""
}

// expertWait is the per-expert cap, same shape as slotWait: siblings already
// in todo or in_progress with expert=<name> count. Over the cap, T1 waits
// instead of falling through to owners.
func (sp *Spec) expertWait(s *Snapshot, e *Expert) string {
	if e == nil || e.Limits.Concurrent <= 0 {
		return ""
	}
	used := 0
	for _, x := range s.Siblings {
		if x.Key == s.Issue.Key {
			continue
		}
		if (x.Status == StTodo || x.Status == StInProgress) && x.Meta["expert"] == e.Name {
			used++
		}
	}
	if used >= e.Limits.Concurrent {
		return fmt.Sprintf("expert %s %d/%d", e.Name, used, e.Limits.Concurrent)
	}
	return ""
}

func (sp *Spec) isOwner(name string) bool {
	for _, o := range sp.Roles.Owners {
		if o == name {
			return true
		}
	}
	for _, e := range sp.Experts {
		if contains(e.Agents, name) {
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
		// round cap: another patch rarely converges; the lead reconsiders the approach.
		return sp.block(s, "impl attempts exhausted: "+why+" (round cap: reconsider the approach or split, not another patch)", nil)
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
	// Member flow controls (written only through /api/orchestrator, never by agents).
	if m["cancel_requested"] == "true" {
		d := &Decision{Tid: "T8b_member_cancel", Status: StCancelled, Meta: map[string]string{"phase": PhCancelled, "cancel_requested": "", "paused": ""}, Why: "cancelled by a member"}
		if s.ActiveRun != nil {
			d.CancelRun = s.ActiveRun.ID
		}
		return d
	}
	if m["paused"] == "true" {
		return &Decision{Wait: "paused by a member (resume to continue)"}
	}
	// Epics never get an agent: their status follows their sub-issues.
	if d, epic := sp.decideEpic(s); epic {
		return d
	}
	// Blocked takes precedence over T9: a blocked issue leaves only through
	// the lead's phase=unblocked / cancel_proposed (T7/T8) or a member's flow
	// action, never because some agent rewrote phase (e.g. implementing).
	if st == StBlocked {
		return sp.decideBlocked(s)
	}
	// Backlog = not planned. The engine never starts, reconciles or otherwise
	// touches a backlog issue (a member moving work back to backlog parks it);
	// T1 picks up todo issues only.
	if st == StBacklog {
		if m["phase"] == PhPlanned {
			return &Decision{Wait: "planned epic: its sub-issues carry the work"}
		}
		if sp.autoStartMode() != AutoStartOff {
			return sp.decidePromote(s)
		}
		return nil
	}
	if StartsHere(st, m) {
		return sp.decideStart(s)
	}
	// T1 commits its metadata before the assignee PUT; a crash in between
	// leaves a started todo issue with nobody on it.
	if st == StTodo && i.Assignee == "" && m["owner"] != "" && (m["phase"] == PhImplementing || m["phase"] == PhChangesRequested) {
		return &Decision{Tid: "T9_reconcile", Status: StTodo, AssigneeKind: "agent", Assignee: m["owner"], Why: "phase " + m["phase"] + " but unassigned (crash after the metadata commit?)"}
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
	// todo / in_progress / in_review
	if s.ActiveRun != nil {
		if d := sp.stuck(s); d != nil {
			return d
		}
		if s.ActiveRun.Status == "waiting_local_directory" {
			return &Decision{Wait: "queued for worktree: another task holds the checkout"}
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
	// "Resolve conflicts" (member, POST /flow/{key}/resolve-conflict).
	if m["conflict_requested"] == "true" {
		if d := sp.resolveConflict(s, s.Head, "a member asked to resolve conflicts with "+sp.mainName()); d != nil {
			d.Meta["conflict_requested"] = ""
			return d
		}
		return &Decision{Tid: "T9_reconcile", Meta: map[string]string{"conflict_requested": ""}, Why: "conflict resolution is off for this team or the issue has no branch"}
	}
	head := s.Head
	if st == StTodo || st == StInProgress {
		if m["phase"] == PhReviewRequested {
			if d := sp.consulted(s); d != nil {
				return d
			}
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
				if d := waitStale(pc); d != nil {
					return d
				}
				if !pc.OK && pc.Kind != "push_failed" && !pc.Pending {
					why := "precheck " + pc.Kind + ": " + short(pc.Detail, 200)
					if pc.Kind == "conflict" {
						if d := sp.resolveConflict(s, head, why); d != nil {
							return d
						}
					}
					return sp.rework(s, why, map[string]string{"head_sha": head})
				}
				if pc.PR != "" {
					extra["pr"], extra["pr_number"] = pc.PR, pc.PRNum
				}
			}
			meta := map[string]string{"phase": PhReviewing, "head_sha": head, "review_started_sha": head, "verdict": "", "reviewed_sha": "", "attempt_review": itoa(num(m["attempt_review"]) + 1), "attempt_quota": ""}
			if prev := m["reviewed_sha"]; prev != "" {
				meta["last_reviewed_sha"] = prev // the reviewer diffs only since here
			}
			for k, v := range extra {
				meta[k] = v
			}
			return &Decision{Tid: "T2_review", Status: StInReview, AssigneeKind: "agent", Assignee: sp.Roles.Reviewer, Meta: meta}
		}
		if i.AssigneeType != "agent" || !sp.isOwner(i.Assignee) {
			return nil // not ours to drive (e.g. a member picked it up)
		}
		if d := sp.branchOccupied(s); d != nil {
			return d
		}
		if d := sp.providerFailure(s); d != nil {
			return d
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
	if m["phase"] == PhResolvingConflict {
		return sp.decideResolving(s, probe)
	}
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
	// Reviewer and lead runs are read-only: while phase=reviewing no owner run
	// is active, so the branch must stay at review_started_sha. A pure rebase
	// or main merge (same patch-id) is tolerated; any other move is blocked
	// and named instead of silently re-reviewing someone else's push.
	if rs := m["review_started_sha"]; m["phase"] == PhReviewing && head != "" && rs != "" && head != rs && (s.SamePatch == nil || !s.SamePatch(rs, head)) {
		return sp.block(s, fmt.Sprintf("branch moved during review (%s -> %s) with no owner run active: reviewer/lead runs are read-only and only the owner may push; check who pushed, then `multica flow unblock %s`", short(rs, 12), short(head, 12), i.Key), nil)
	}
	v := strings.ToLower(strings.TrimSpace(m["verdict"]))
	if v == "" {
		// A reviewer run that failed or was cancelled (e.g. the daemon could
		// not prepare its worktree) is an infra attempt, not a review attempt.
		if d := sp.providerFailure(s); d != nil {
			return d
		}
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
	if v == "changes" {
		return sp.rework(s, "reviewer verdict=changes", nil)
	}
	if v != "approve" {
		// not a verdict the engine understands (the guard refuses new ones,
		// legacy values remain): ask the reviewer again rather than sending a
		// possibly-approved change back to the owner.
		return sp.rereview(s, head, fmt.Sprintf("unknown verdict %q (want approve or changes)", m["verdict"]))
	}
	if sp.NeedsAcceptance(i) {
		// gate after checks/mergeability are green so the human sees a mergeable PR.
		if probe != nil {
			pc := probe(s, false)
			if d := waitStale(pc); d != nil {
				return d
			}
			if pc.Pending {
				return &Decision{Wait: pc.Detail}
			}
			if !pc.OK && pc.Kind != "push_failed" {
				if pc.Kind == "conflict" {
					if d := sp.resolveConflict(s, head, pc.Kind+": "+short(pc.Detail, 200)); d != nil {
						return d
					}
				}
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
	meta := map[string]string{"phase": PhReviewing, "verdict": "", "reviewed_sha": "", "review_started_sha": head, "head_sha": head, "attempt_review": itoa(n)}
	if prev := s.Issue.Meta["reviewed_sha"]; prev != "" {
		meta["last_reviewed_sha"] = prev
	}
	return &Decision{Tid: "T3b_rereview", AssigneeKind: "agent", Assignee: sp.Roles.Reviewer, Status: StInReview, Rerun: s.Issue.Assignee == sp.Roles.Reviewer,
		Meta: meta, Why: why}
}

// waitStale: a stale probe (the PR / snapshot head lags the branch head) is
// the same everywhere - T2 pre-check, T10 pre-check and merge wait for
// GitHub to catch up; none of them reworks or reviews a sha GitHub has not seen.
func waitStale(r ProbeResult) *Decision {
	if r.Kind != "stale" {
		return nil
	}
	return &Decision{Wait: "stale: " + r.Detail}
}

func (sp *Spec) merge(s *Snapshot, probe Prober) *Decision {
	if probe == nil {
		return &Decision{Wait: "no prober"}
	}
	r := probe(s, true)
	switch {
	case waitStale(r) != nil:
		return waitStale(r)
	case r.Pending:
		return &Decision{Wait: r.Detail}
	case r.Kind == "push_failed":
		return &Decision{Wait: r.Kind + ": " + r.Detail}
	case r.OK:
		return &Decision{Tid: "T4_done", Status: StDone, Meta: map[string]string{"phase": PhMerged, "merged_sha": r.SHA}}
	}
	if r.Kind == "conflict" {
		if d := sp.resolveConflict(s, s.Head, r.Kind+": "+short(r.Detail, 200)); d != nil {
			return d
		}
	}
	return sp.rework(s, r.Kind+": "+short(r.Detail, 200), nil)
}

// stuck cancels an active run that ran past stuck_min, went silent past
// idle_min, or sat queued/dispatched (never started) past queued_min.
// nil = the run is healthy.
func (sp *Spec) stuck(s *Snapshot) *Decision {
	r := s.ActiveRun
	if r == nil {
		return nil
	}
	id := r.ID[:min(8, len(r.ID))]
	if r.StartedAt == nil {
		if q := sp.Limits.Queued(); q > 0 && r.CreatedAt != nil && s.Now.Sub(*r.CreatedAt) > time.Duration(q)*time.Minute {
			return &Decision{Tid: "T5b_cancel_stuck", CancelRun: r.ID, Why: fmt.Sprintf("run %s %s for %dm > queued_min %dm (runtime offline or saturated?)", id, r.Status, int(s.Now.Sub(*r.CreatedAt).Minutes()), q)}
		}
		return nil
	}
	if s.Now.Sub(*r.StartedAt) > time.Duration(sp.Limits.StuckMin)*time.Minute {
		return &Decision{Tid: "T5b_cancel_stuck", CancelRun: r.ID, Why: fmt.Sprintf("run %s running %dm > %dm", id, int(s.Now.Sub(*r.StartedAt).Minutes()), sp.Limits.StuckMin)}
	}
	if idleMin := sp.Limits.IdleFor(r.Backend); r.Status == "running" && idleMin > 0 {
		last := *r.StartedAt
		if r.LastActivity != nil && r.LastActivity.After(last) {
			last = *r.LastActivity
		}
		if idle := s.Now.Sub(last); idle > time.Duration(idleMin)*time.Minute {
			return &Decision{Tid: "T5b_cancel_stuck", CancelRun: r.ID, Why: fmt.Sprintf("run %s no tool output for %dm > idle_min %dm", id, int(idle.Minutes()), idleMin)}
		}
	}
	return nil
}

// Provider failure classes from pkg/taskfailure reasons.
func failureClass(reason string) string {
	switch {
	case strings.HasPrefix(reason, "agent_error.provider_quota_limit"), strings.HasPrefix(reason, "agent_error.provider_capacity_or_rate_limit"):
		return "limit"
	case strings.HasPrefix(reason, "agent_error.provider_auth_or_access"), reason == "runtime_access_denied":
		return "auth"
	}
	return ""
}

// providerFailure handles a failed last run whose failure_reason says the
// same retry cannot help yet: quota / rate limits retry with exponential
// backoff (1m, 2m, 4m … capped at 30m) and count no attempt; auth/access
// failures block at once with the reason. nil = an ordinary failure.
func (sp *Spec) providerFailure(s *Snapshot) *Decision {
	r := s.LastRun
	if r == nil || r.Status != "failed" {
		return nil
	}
	class := failureClass(r.Reason)
	// A run that timed out before it ever started was queued for capacity
	// (a local-directory / worktree lock, an agent slot), not a failed
	// attempt: retry with the same no-attempt backoff instead of burning
	// attempt_impl / attempt_infra until the issue blocks (OX-122).
	if class == "" && r.Reason == "timeout" && r.StartedAt == nil {
		class = "limit"
	}
	switch class {
	case "auth":
		return sp.block(s, "provider auth/access failure ("+r.Reason+"): fix the agent's provider credentials or runtime access, then `multica flow unblock "+s.Issue.Key+"`", nil)
	case "limit":
		n := num(s.Issue.Meta["attempt_quota"])
		wait := time.Minute << min(n, 5)
		if wait > 30*time.Minute {
			wait = 30 * time.Minute
		}
		if r.EndedAt != nil {
			if left := wait - s.Now.Sub(*r.EndedAt); left > 0 {
				return &Decision{Wait: fmt.Sprintf("provider limit (%s): retry %d in %s, no attempt counted", r.Reason, n+1, left.Round(time.Second))}
			}
		}
		d := &Decision{Tid: "T5q_provider_limit", Rerun: true, Meta: map[string]string{"attempt_quota": itoa(n + 1)},
			Why: fmt.Sprintf("last run hit a provider limit (%s): retry %d after %s backoff, no attempt counted", r.Reason, n+1, wait)}
		if n+1 == 3 {
			d.Notify = fmt.Sprintf("%s keeps hitting a provider limit (%s); the engine keeps retrying with backoff up to 30m", s.Issue.Key, r.Reason)
		}
		return d
	}
	return nil
}

// BranchOccupiedMarker mirrors execenv.BranchOccupiedMarker: the daemon
// starts a task's error with it when the issue's agent/<key> branch (or the
// path wt derives from it) is held by another worktree.
const BranchOccupiedMarker = "[branch_occupied]"

// branchOccupied: the owner's run could not get agent/<key> because another
// worktree holds it. Retrying cannot help and the daemon never forks or
// steals the branch, so block with the occupying path for a human to free.
func (sp *Spec) branchOccupied(s *Snapshot) *Decision {
	r := s.LastRun
	if r == nil || r.Status != "failed" {
		return nil
	}
	i := strings.Index(r.Error, BranchOccupiedMarker)
	if i < 0 {
		return nil
	}
	msg := r.Error[i+len(BranchOccupiedMarker):]
	path := "(unknown)"
	if _, after, ok := strings.Cut(msg, " is held by the worktree at "); ok {
		path, _, _ = strings.Cut(after, ";")
	}
	br := branchOf(sp, s.Issue.Key)
	return sp.block(s, fmt.Sprintf("%s is checked out by another worktree at %s: the orchestrator owns this branch and never forks or takes it over; free it (finish or remove that worktree, and do not use the agent/ prefix or an agent-<key> path for other work), then `multica flow unblock %s`", br, strings.TrimSpace(path), s.Issue.Key), nil)
}

// decideBlocked handles a blocked issue: the lead's answer (unblocked /
// cancel_proposed) or a member's flow action. Member-only confirmations live
// in engine-owned keys (unblock_confirmed, cancel_confirmed) that only the
// /api/orchestrator flow endpoints write.
func (sp *Spec) decideBlocked(s *Snapshot) *Decision {
	i := &s.Issue
	m := i.Meta
	// the lead's run on a blocked issue gets the same stuck/idle/queued
	// detection as any other run (it used to run unbounded).
	if d := sp.stuck(s); d != nil {
		d.Notify = fmt.Sprintf("%s: the lead's run on this blocked issue was cancelled: %s", i.Key, d.Why)
		return d
	}
	switch m["phase"] {
	case PhUnblocked:
		member := m["unblock_confirmed"] == "true"
		n := num(m["unblock_count"])
		if !member && n >= sp.Limits.MaxUnblocks() {
			if m["unblock_escalated"] == "true" {
				return &Decision{Wait: fmt.Sprintf("lead unblock limit %d reached: waiting for a member (multica flow unblock %s)", sp.Limits.MaxUnblocks(), i.Key)}
			}
			why := fmt.Sprintf("lead unblocked %d times (limit %d); a member decides now", n, sp.Limits.MaxUnblocks())
			return &Decision{Tid: "T7b_unblock_escalate", AssigneeKind: "member", Assignee: "@human", Why: why,
				Meta:   map[string]string{"phase": PhBlocked, "unblock_escalated": "true", "blocked_reason": why + " (last reason: " + short(m["blocked_reason"], 200) + ")"},
				Notify: fmt.Sprintf("%s: %s. `multica flow unblock %s` or `multica flow cancel %s`", i.Key, why, i.Key, i.Key)}
		}
		owner := m["owner"]
		expertName := ""
		if owner == "" {
			pool, ex := sp.Assign(i)
			expertName = ex
			if len(pool) > 0 {
				owner = pool[0]
			}
		}
		count := itoa(n + 1)
		if member {
			count = "0" // a person's unblock is a fresh start
		}
		meta := map[string]string{"phase": PhImplementing, "attempt_impl": "1", "attempt_review": "0", "attempt_infra": "0", "blocked_reason": "", "verdict": "", "acceptance": "",
			"unblock_count": count, "unblock_confirmed": "", "unblock_escalated": "", "cancel_notified": "", "attempt_quota": ""}
		if expertName != "" {
			meta["expert"] = expertName
		}
		return &Decision{Tid: "T7_unblock", Status: StTodo, AssigneeKind: "agent", Assignee: owner, Meta: meta}
	case PhCancelProposed:
		if m["cancel_confirmed"] == "true" {
			return &Decision{Tid: "T8_cancel", Status: StCancelled, Meta: map[string]string{"phase": PhCancelled, "cancel_confirmed": ""}}
		}
		if m["cancel_notified"] != "true" {
			return &Decision{Tid: "T8a_cancel_proposed", AssigneeKind: "member", Assignee: "@human", Why: "lead proposed cancel; waiting for a member to confirm",
				Meta:   map[string]string{"cancel_notified": "true"},
				Notify: fmt.Sprintf("%s: the lead proposes to cancel it (%s). Confirm with `multica flow cancel %s` (确认取消) or `multica flow unblock %s`", i.Key, short(m["blocked_reason"], 200), i.Key, i.Key)}
		}
		return &Decision{Wait: "cancel proposed: waiting for a member to confirm (multica flow cancel " + i.Key + ") or unblock"}
	}
	return &Decision{Wait: "blocked: waiting for phase=unblocked or phase=cancel_proposed (lead) or multica flow unblock"}
}

// StartsHere reports whether an issue is a T1 candidate: todo and not yet
// started by the engine (no phase). The runtime gathers busy owners, fresh
// siblings and free disk only for these.
func StartsHere(status string, meta map[string]string) bool {
	return status == StTodo && (meta["phase"] == "" || meta["phase"] == PhPlanned)
}

// NeedsAssignBusy reports whether a snapshot has to know which agents are
// already on todo/in_progress work: T1, and a consult handoff that is about
// to assign the next owner.
func (sp *Spec) NeedsAssignBusy(status string, meta map[string]string) bool {
	if sp.startCandidate(status, meta) {
		return true
	}
	if (status != StTodo && status != StInProgress) || meta["phase"] != PhReviewRequested {
		return false
	}
	e := sp.expertByName(meta["expert"])
	return e != nil && e.Mode == "consult"
}

// decideStart is T1: a todo issue the engine has not started yet.
func (sp *Spec) decideStart(s *Snapshot) *Decision {
	i := &s.Issue
	m := i.Meta
	if m["phase"] == PhPlanned {
		return &Decision{Wait: "planned epic: its sub-issues carry the work"}
	}
	if w := depsWait(s); w != "" {
		return &Decision{Wait: w}
	}
	if w := stageWait(s); w != "" {
		return &Decision{Wait: w}
	}
	if g := sp.Limits.FreeGB(); g > 0 && s.FreeGB >= 0 && s.FreeGB < g {
		return &Decision{Wait: fmt.Sprintf("disk guard: %dGB free < %dGB", s.FreeGB, g)}
	}
	if w := sp.slotWait(s); w != "" {
		return &Decision{Wait: "waiting: " + w}
	}
	// Someone else already holds it: a member, or an agent outside the owner
	// and expert pools. Not ours to start.
	held := i.Assignee != "" && i.AssigneeType == "agent" && sp.isOwner(i.Assignee)
	if i.Assignee != "" && !held {
		return nil
	}
	if held && s.ActiveRun != nil {
		// assigning an owner in todo already started its run: adopt it.
		meta := map[string]string{"owner": i.Assignee, "phase": PhImplementing, "attempt_impl": "1", "attempt_review": "0", "attempt_infra": "0"}
		if ex := sp.expertNameFor(i, i.Assignee); ex != "" {
			meta["expert"] = ex
		}
		return &Decision{Tid: "T1_start", Meta: meta, Why: "adopted the run that assigning " + i.Assignee + " started"}
	}
	if e := sp.MatchExpert(i); e != nil {
		if w := sp.expertWait(s, e); w != "" {
			return &Decision{Wait: "waiting: " + w}
		}
	}
	owner := ""
	expertName := ""
	if o := m["owner"]; o != "" {
		if s.Busy[o] {
			return &Decision{Wait: "owner " + o + " busy"}
		}
		owner = o
		expertName = sp.expertNameFor(i, o)
	} else if held {
		if s.Busy[i.Assignee] {
			return &Decision{Wait: "owner " + i.Assignee + " busy"}
		}
		owner = i.Assignee
		expertName = sp.expertNameFor(i, owner)
	} else {
		var pool []string
		pool, expertName = sp.Assign(i)
		for _, o := range pool {
			if !s.Busy[o] {
				owner = o
				break
			}
		}
	}
	if owner == "" {
		if expertName != "" {
			return &Decision{Wait: "expert " + expertName + " busy"}
		}
		return &Decision{Wait: "all owners busy"}
	}
	meta := map[string]string{"owner": owner, "phase": PhImplementing, "attempt_impl": "1", "attempt_review": "0", "attempt_infra": "0"}
	if expertName != "" {
		meta["expert"] = expertName
	}
	return &Decision{Tid: "T1_start", Status: StInProgress, AssigneeKind: "agent", Assignee: owner,
		// the same assignee does not dispatch a run by itself
		Rerun: i.AssigneeType == "agent" && i.Assignee == owner,
		Meta:  meta}
}

// consulted handles a consult expert's handoff (phase=review_requested).
// The issue returns to todo, expert_done records the expert, and assignment
// runs again through routes and owners (that expert will not match). A busy
// next pool waits; it does not fall back to a different pool.
func (sp *Spec) consulted(s *Snapshot) *Decision {
	i := &s.Issue
	m := i.Meta
	e := sp.expertByName(m["expert"])
	if e == nil || e.Mode != "consult" {
		return nil
	}
	ghost := *i
	gm := cloneMeta(m)
	gm["expert_done"] = appendDone(gm["expert_done"], e.Name)
	delete(gm, "expert")
	delete(gm, "owner")
	delete(gm, "phase")
	ghost.Meta = gm
	ghost.Assignee, ghost.AssigneeType = "", ""
	pool, next := sp.Assign(&ghost)
	if next != "" {
		if w := sp.expertWait(s, sp.expertByName(next)); w != "" {
			return &Decision{Wait: "waiting: " + w}
		}
	}
	owner := ""
	for _, o := range pool {
		if sp.agentFree(s, o) {
			owner = o
			break
		}
	}
	if owner == "" {
		why := "all owners busy"
		if next != "" {
			why = "expert " + next + " busy"
		}
		return &Decision{Wait: "consult " + e.Name + " done; " + why}
	}
	meta := map[string]string{
		"phase": PhImplementing, "owner": owner, "expert": next,
		"expert_done":    appendDone(m["expert_done"], e.Name),
		"attempt_impl":   "1",
		"attempt_review": "0",
		"attempt_infra":  "0",
		"verdict":        "",
	}
	return &Decision{
		Tid: "T_expert_consulted", Status: StTodo, AssigneeKind: "agent", Assignee: owner, Meta: meta,
		Why:   "consult " + e.Name + " finished; assigning " + owner,
		Rerun: i.AssigneeType == "agent" && i.Assignee == owner,
	}
}

// agentFree reports whether o can take this issue. The current assignee is
// free for the reassignment: this issue is what made them look busy, and
// their run has already finished.
func (sp *Spec) agentFree(s *Snapshot, o string) bool {
	if o == "" || !s.Busy[o] {
		return o != ""
	}
	i := &s.Issue
	return i.AssigneeType == "agent" && i.Assignee == o
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// shadowProbe stands in for the real gates in shadow mode: the review
// pre-check reports pending (nothing pushed, no PR, no CI rerun) and a merge
// reports what would happen without doing it.
func shadowProbe(_ *Snapshot, merge bool) ProbeResult {
	if merge {
		return ProbeResult{OK: true, Kind: "dry", Detail: "shadow: would merge"}
	}
	return ProbeResult{Pending: true, Kind: "pending", Detail: "shadow: gates not run"}
}
