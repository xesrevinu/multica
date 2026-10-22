package orchestrator

import "fmt"

// Conflict resolution (T11 / T11a / T11b).
//
// A branch that conflicts with main is not the owner's rework: it is a
// mechanical merge. The reviewer agent runs it with the resolve-conflict
// skill in the issue's worktree (merge --no-ff origin/main, never rebase or
// force), runs the gates and fast-forward pushes, then writes
// conflict_result=resolved. When it cannot (a semantic conflict, gates broken
// by the merge) it aborts the merge, pushes nothing and writes
// conflict_result=cannot_resolve with conflict_notes; the engine hands the
// issue back to the owner with that context (T11b, a normal rework).
//
// attempt_conflict counts resolver runs in one episode (limit 2) and never
// touches attempt_impl / attempt_review. Provider limits use the usual
// no-attempt backoff.

// Engine-owned conflict bookkeeping.
const (
	metaAttemptConflict = "attempt_conflict"
	metaConflictFrom    = "conflict_from_phase" // phase to resume after T11a
	metaConflictHead    = "conflict_head"       // head the resolver started from
	metaConflictBack    = "conflict_handback"   // T11b already used this episode
	// Agent hand-over (resolve-conflict skill).
	metaConflictResult = "conflict_result" // resolved | cannot_resolve
	metaConflictNotes  = "conflict_notes"
)

// resolveConflict dispatches the reviewer to merge main in (T11), or returns
// nil when conflict resolution is off / exhausted so the caller falls back
// to today's rework.
func (sp *Spec) resolveConflict(s *Snapshot, head, why string) *Decision {
	m := s.Issue.Meta
	lim := sp.Limits.conflictLimit()
	if sp.Roles.Reviewer == "" || lim < 0 || head == "" {
		return nil
	}
	if m[metaConflictBack] == "true" {
		// The owner already had this episode back and it conflicts again:
		// plain rework (which caps on attempt_impl and blocks).
		return nil
	}
	n := num(m[metaAttemptConflict]) + 1
	if n > lim {
		return sp.conflictHandback(s, fmt.Sprintf("resolver attempts exhausted (%d/%d): %s", n-1, lim, why))
	}
	from := m["phase"]
	if from == PhResolvingConflict {
		from = m[metaConflictFrom]
	}
	return &Decision{Tid: "T11_resolve_conflict", Status: StInReview, AssigneeKind: "agent", Assignee: sp.Roles.Reviewer,
		Rerun: s.Issue.Assignee == sp.Roles.Reviewer,
		Meta: map[string]string{"phase": PhResolvingConflict, metaAttemptConflict: itoa(n), metaConflictFrom: from, metaConflictHead: head,
			metaConflictResult: "", metaConflictNotes: "", "attempt_quota": ""},
		Why: why}
}

// conflictHandback: T11b — the owner gets the conflict with the resolver's
// notes. Counts as an ordinary rework round (a semantic conflict is real work).
func (sp *Spec) conflictHandback(s *Snapshot, why string) *Decision {
	notes := s.Issue.Meta[metaConflictNotes]
	reason := "merge conflict with " + sp.mainName() + ": " + why
	if notes != "" {
		reason += " — resolver notes: " + short(notes, 600)
	}
	d := sp.rework(s, reason, map[string]string{metaConflictBack: "true", metaConflictResult: "", metaAttemptConflict: "0", metaConflictHead: "", metaConflictFrom: ""})
	if d.Tid == "T3_rework" {
		d.Tid = "T11b_conflict_handback"
	}
	return d
}

func (sp *Spec) mainName() string {
	if sp.Repo.Main != "" {
		return sp.Repo.Main
	}
	return "main"
}

// decideResolving: phase=resolving_conflict and no run active.
func (sp *Spec) decideResolving(s *Snapshot, probe Prober) *Decision {
	m := s.Issue.Meta
	head := s.Head
	switch m[metaConflictResult] {
	case "cannot_resolve":
		return sp.conflictHandback(s, "the resolver could not merge it")
	case "resolved":
	default:
		// No hand-over: a provider limit retries without an attempt; any
		// other ending (failed, cancelled, completed without result) is a
		// spent resolver attempt.
		if d := sp.providerFailure(s); d != nil {
			return d
		}
		if d := sp.branchOccupied(s); d != nil {
			return d
		}
		return sp.resolveConflict(s, head, "resolver run ended without conflict_result")
	}
	if head == "" || head == m[metaConflictHead] {
		return sp.resolveConflict(s, head, "conflict_result=resolved but the branch did not move")
	}
	if probe != nil {
		pc := probe(s, false)
		if d := waitStale(pc); d != nil {
			return d
		}
		if pc.Pending {
			return &Decision{Wait: pc.Detail}
		}
		if pc.Kind == "conflict" {
			// main moved again (or the merge was incomplete): another attempt.
			return sp.resolveConflict(s, head, "still conflicting after the resolver's merge: "+short(pc.Detail, 200))
		}
	}
	clear := map[string]string{metaAttemptConflict: "0", metaConflictHead: "", metaConflictFrom: "", metaConflictResult: "", metaConflictBack: "", "head_sha": head}
	// The approval survives when the PR's own change is unchanged (same
	// patch-id as what the reviewer approved): resume where it was.
	if old := m["reviewed_sha"]; m["verdict"] == "approve" && old != "" && s.SamePatch != nil && s.SamePatch(old, head) {
		meta := clear
		meta["reviewed_sha"], meta["verdict_carried_from"], meta["review_started_sha"] = head, old, head
		from := m[metaConflictFrom]
		if from == "" || from == PhResolvingConflict || from == PhReviewRequested {
			from = PhReviewing
		}
		meta["phase"] = from
		d := &Decision{Tid: "T11a_conflict_resolved", Meta: meta, Why: fmt.Sprintf("merged %s in; same patch-id as approved %s: approval carried", sp.mainName(), short(old, 12))}
		if from == PhAwaitAcceptance {
			d.AssigneeKind, d.Assignee = "member", "@human"
		}
		return d
	}
	// Otherwise the merged result needs a (re)review; not a review attempt.
	meta := clear
	meta["phase"], meta["verdict"], meta["reviewed_sha"], meta["review_started_sha"] = PhReviewing, "", "", head
	if prev := m["reviewed_sha"]; prev != "" {
		meta["last_reviewed_sha"] = prev
	}
	return &Decision{Tid: "T11a_conflict_resolved", Status: StInReview, AssigneeKind: "agent", Assignee: sp.Roles.Reviewer,
		Rerun: s.Issue.Assignee == sp.Roles.Reviewer, Meta: meta, Why: "merged " + sp.mainName() + " in: re-review the merged change"}
}
