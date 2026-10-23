package orchestrator

import (
	"strings"
	"testing"
	"time"
)

func spec(t *testing.T) *Spec {
	s, err := ParseSpec([]byte(`orchestrator:
  roles: {lead: lead, reviewer: rev, owners: [a, b]}
  repo: {mode: github, slug: x/y, required_checks: [test]}
`))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

var now = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

// fresh is a todo issue nobody started or holds yet: the T1 input.
func fresh() *Snapshot {
	s := snap(StTodo, map[string]string{})
	s.Issue.AssigneeType, s.Issue.Assignee = "", ""
	return s
}

func snap(st string, meta map[string]string) *Snapshot {
	return &Snapshot{Now: now, Issue: Issue{ID: "1", Key: "ATT-1", Status: st, Meta: meta, AssigneeType: "agent", Assignee: "a"}, Busy: map[string]bool{}, FreeGB: 100}
}

func ok(merge bool) Prober {
	return func(*Snapshot, bool) ProbeResult { return ProbeResult{OK: true, Kind: "merged", SHA: "m1"} }
}
func fail(kind string) Prober {
	return func(*Snapshot, bool) ProbeResult { return ProbeResult{Kind: kind, Detail: "boom"} }
}

func want(t *testing.T, d *Decision, tid, status, assignee string) {
	t.Helper()
	if d == nil {
		t.Fatalf("want %s, got nil", tid)
	}
	if d.Tid != tid || d.Status != status || d.Assignee != assignee {
		t.Fatalf("want %s/%s/%s, got %+v", tid, status, assignee, d)
	}
}

func TestHappyPath(t *testing.T) {
	sp := spec(t)
	s := snap(StTodo, map[string]string{})
	s.Issue.AssigneeType, s.Issue.Assignee = "", ""
	d := sp.Decide(s, nil)
	want(t, d, "T1_start", StInProgress, "a")
	if !d.StartsRun() || d.Meta["phase"] != PhImplementing {
		t.Fatal(d)
	}
	s = snap(StInProgress, map[string]string{"phase": PhReviewRequested, "owner": "a", "attempt_impl": "1", "attempt_review": "0"})
	s.Head = "h1"
	d = sp.Decide(s, func(*Snapshot, bool) ProbeResult { return ProbeResult{OK: true, Kind: "dry", PR: "u", PRNum: "7"} })
	want(t, d, "T2_review", StInReview, "rev")
	if d.Meta["head_sha"] != "h1" || d.Meta["attempt_review"] != "1" || d.Meta["pr_number"] != "7" {
		t.Fatal(d.Meta)
	}
	s = snap(StInReview, map[string]string{"phase": PhReviewing, "owner": "a", "verdict": "approve", "reviewed_sha": "h1"})
	s.Head = "h1"
	want(t, sp.Decide(s, ok(true)), "T4_done", StDone, "")
}

func TestReviewChangesAndStale(t *testing.T) {
	sp := spec(t)
	s := snap(StInReview, map[string]string{"phase": PhReviewing, "owner": "a", "verdict": "changes", "attempt_impl": "1"})
	s.Head = "h1"
	d := sp.Decide(s, ok(true))
	want(t, d, "T3_rework", StTodo, "a")
	if d.Meta["attempt_impl"] != "2" {
		t.Fatal(d.Meta)
	}
	s.Issue.Meta = map[string]string{"phase": PhReviewing, "verdict": "approve", "reviewed_sha": "old"}
	s.Issue.Assignee = "rev"
	d = sp.Decide(s, ok(true))
	want(t, d, "T3b_rereview", StInReview, "rev")
	if !d.Rerun {
		t.Fatal("stale approve on reviewer must rerun")
	}
	s.Issue.Meta = map[string]string{"phase": PhReviewing, "verdict": "changes", "attempt_impl": "3"}
	want(t, sp.Decide(s, ok(true)), "T6_block", StBlocked, "lead")
}

func TestNoVerdictRereviewThenBlock(t *testing.T) {
	sp := spec(t)
	s := snap(StInReview, map[string]string{"phase": PhReviewing, "attempt_review": "1"})
	d := sp.Decide(s, nil)
	if d.Tid != "T3b_rereview" || !d.Rerun {
		t.Fatal(d)
	}
	s.Issue.Meta["attempt_review"] = "3"
	want(t, sp.Decide(s, nil), "T6_block", StBlocked, "lead")
}

func TestPrecheckConflictAndMergeConflict(t *testing.T) {
	sp := spec(t)
	s := snap(StInProgress, map[string]string{"phase": PhReviewRequested, "owner": "a", "attempt_impl": "1"})
	s.Head = "h"
	d := sp.Decide(s, fail("conflict"))
	// A conflict goes to the reviewer's resolve-conflict run (T11), not the owner.
	want(t, d, "T11_resolve_conflict", StInReview, sp.Roles.Reviewer)
	if d.Meta["attempt_conflict"] != "1" || d.Meta["phase"] != PhResolvingConflict {
		t.Fatal(d.Meta)
	}
	off := spec(t)
	off.Limits.Conflict = -1
	d = off.Decide(s, fail("conflict"))
	want(t, d, "T3_rework", StTodo, "a")
	if !strings.Contains(d.Meta["blocked_reason"], "precheck conflict") {
		t.Fatal(d.Meta)
	}
	s = snap(StInReview, map[string]string{"phase": PhReviewing, "owner": "a", "verdict": "approve", "reviewed_sha": "h"})
	s.Head = "h"
	want(t, sp.Decide(s, fail("checks_red")), "T3_rework", StTodo, "a")
	pend := func(*Snapshot, bool) ProbeResult {
		return ProbeResult{Pending: true, Kind: "pending", Detail: "test=PENDING"}
	}
	if d := sp.Decide(s, pend); d.Tid != "" || d.Wait == "" {
		t.Fatal(d)
	}
}

func TestRerunAndExhaustion(t *testing.T) {
	sp := spec(t)
	s := snap(StInProgress, map[string]string{"phase": PhImplementing, "owner": "a", "attempt_impl": "1"})
	s.LastRun = &Run{ID: "r", Status: "completed"}
	d := sp.Decide(s, nil)
	if d.Tid != "T5_rerun" || d.Meta["attempt_impl"] != "2" {
		t.Fatal(d)
	}
	s.LastRun.Status = "failed"
	s.Issue.Meta["attempt_infra"] = "2"
	want(t, sp.Decide(s, nil), "T6_block", StBlocked, "lead")
	s.Issue.Meta["phase"] = PhReviewRequested
	s.Head = ""
	if d := sp.Decide(s, nil); d.Tid != "T5_rerun_nobranch" {
		t.Fatal(d)
	}
}

func TestStuck(t *testing.T) {
	sp := spec(t)
	s := snap(StInProgress, map[string]string{"phase": PhImplementing})
	st := now.Add(-21 * time.Minute)
	s.ActiveRun = &Run{ID: "run-12345678", Status: "running", StartedAt: &st}
	if d := sp.Decide(s, nil); d.Tid != "T5b_cancel_stuck" || d.CancelRun != "run-12345678" {
		t.Fatal(d)
	}
	s.ActiveRun.StartedAt = nil // queued never counts
	if d := sp.Decide(s, nil); d != nil {
		t.Fatal(d)
	}
}

func TestBacklogGuards(t *testing.T) {
	sp := spec(t)
	s := fresh()
	s.FreeGB = 5
	if d := sp.Decide(s, nil); d.Tid != "" || !strings.Contains(d.Wait, "disk") {
		t.Fatal(d)
	}
	s.FreeGB = 100
	s.Busy = map[string]bool{"a": true, "b": true}
	if d := sp.Decide(s, nil); d.Tid != "" {
		t.Fatal(d)
	}
	s.Busy = map[string]bool{"a": true}
	want(t, sp.Decide(s, nil), "T1_start", StInProgress, "b")
	// stage barrier
	s.Issue.ParentID, s.Issue.Stage = "p", 2
	s.Siblings = []Issue{{Key: "ATT-2", ParentID: "p", Stage: 1, Status: StInReview}}
	if d := sp.Decide(s, nil); d == nil || d.Tid != "" || d.Wait == "" {
		t.Fatal(d)
	}
	s.Siblings[0].Status = StDone
	want(t, sp.Decide(s, nil), "T1_start", StInProgress, "b")
	// deps
	s.Issue.Meta["deps"] = "ATT-9"
	s.Siblings = append(s.Siblings, Issue{Key: "ATT-9", Status: StTodo})
	if d := sp.Decide(s, nil); d == nil || d.Tid != "" || d.Wait == "" {
		t.Fatal(d)
	}
}

func TestBlockedUnblockCancelAndAgentBlock(t *testing.T) {
	sp := spec(t)
	s := snap(StBlocked, map[string]string{"phase": PhUnblocked, "owner": "b", "attempt_impl": "3"})
	d := sp.Decide(s, nil)
	want(t, d, "T7_unblock", StTodo, "b")
	if d.Meta["attempt_impl"] != "1" {
		t.Fatal(d)
	}
	s.Issue.Meta["phase"] = PhCancelProposed
	// a lead proposal waits for a member; only the flow API's confirmation cancels
	want(t, sp.Decide(s, nil), "T8a_cancel_proposed", "", "@human")
	s.Issue.Meta["cancel_notified"] = "true"
	if d := sp.Decide(s, nil); d == nil || d.Tid != "" || !strings.Contains(d.Wait, "cancel proposed") {
		t.Fatal(d)
	}
	s.Issue.Meta["cancel_confirmed"] = "true"
	want(t, sp.Decide(s, nil), "T8_cancel", StCancelled, "")
	s = snap(StInProgress, map[string]string{"phase": PhBlocked, "blocked_reason": "need creds"})
	d = sp.Decide(s, nil)
	want(t, d, "T6_block", StBlocked, "lead")
	if d.Notify == "" {
		t.Fatal("block must notify")
	}
}

func TestReconcileOutOfBand(t *testing.T) {
	sp := spec(t)
	s := snap(StDone, map[string]string{"phase": PhReviewing})
	if d := sp.Decide(s, nil); d != nil {
		t.Fatal("terminal statuses are never reverted", d)
	}
	s = snap(StTodo, map[string]string{"phase": PhReviewing})
	want(t, sp.Decide(s, nil), "T9_reconcile", StInReview, "rev")
	s = snap(StInReview, map[string]string{"phase": PhImplementing, "owner": "b"})
	want(t, sp.Decide(s, nil), "T9_reconcile", StTodo, "b")
}

func TestAcceptanceGate(t *testing.T) {
	sp := spec(t)
	s := snap(StInReview, map[string]string{"phase": PhReviewing, "owner": "a", "verdict": "approve", "reviewed_sha": "h"})
	s.Head = "h"
	s.Issue.Labels = []string{"needs-GUI-Master"}
	merged := false
	pr := func(_ *Snapshot, m bool) ProbeResult {
		merged = merged || m
		return ProbeResult{OK: true, Kind: "dry", SHA: "m"}
	}
	d := sp.Decide(s, pr)
	want(t, d, "T10_await_acceptance", "", "@human")
	if merged || d.Notify == "" || d.StartsRun() {
		t.Fatal("gate must not merge or start runs", d)
	}
	s.Issue.Meta["phase"] = PhAwaitAcceptance
	s.Issue.AssigneeType, s.Issue.Assignee = "member", "u1"
	if d := sp.Decide(s, pr); d.Wait == "" {
		t.Fatal(d)
	}
	s.Issue.Meta["acceptance"] = "reject: button misaligned"
	d = sp.Decide(s, pr)
	want(t, d, "T3_rework", StTodo, "a")
	if !strings.Contains(d.Why, "misaligned") {
		t.Fatal(d)
	}
	s.Issue.Meta["acceptance"] = "ok"
	want(t, sp.Decide(s, pr), "T4_done", StDone, "")
	if !merged {
		t.Fatal("accept must merge")
	}
}

func TestSpecValidation(t *testing.T) {
	for _, y := range []string{
		`roles: {lead: l, reviewer: r, owners: [r]}
repo: {mode: github, slug: a/b}`,
		`roles: {lead: l, reviewer: r, owners: [a]}
repo: {mode: github}`,
		`roles: {lead: l, reviewer: r, owners: [a]}
repo: {mode: local, bare: /x}
transitions: [{id: X, actor: agent, from: [in_review], to: [done]}]`,
	} {
		if _, err := ParseSpec([]byte(y)); err == nil {
			t.Fatalf("expected error for %s", y)
		}
	}
	s := spec(t)
	if !strings.Contains(s.Mermaid(StTodo), "T4_done") {
		t.Fatal(s.Mermaid(""))
	}
}

func TestIdle(t *testing.T) {
	sp := spec(t)
	s := snap(StInProgress, map[string]string{"phase": PhImplementing})
	st := now.Add(-12 * time.Minute)
	act := now.Add(-9 * time.Minute)
	s.ActiveRun = &Run{ID: "run-abcdef12", Status: "running", StartedAt: &st, LastActivity: &act}
	if d := sp.Decide(s, nil); d == nil || d.Tid != "T5b_cancel_stuck" || !strings.Contains(d.Why, "no tool output") {
		t.Fatal(d)
	}
	act = now.Add(-2 * time.Minute)
	if d := sp.Decide(s, nil); d != nil {
		t.Fatal("active run must not be cancelled", d)
	}
	// through ParseSpec/Defaults, as the server loads it
	neg, err := ParseSpec([]byte("roles: {lead: l, reviewer: r, owners: [a]}\nrepo: {mode: github, slug: x/y}\nlimits: {idle_min: -1}\n"))
	if err != nil {
		t.Fatal(err)
	}
	sp = neg
	act = now.Add(-11 * time.Minute)
	if d := sp.Decide(s, nil); d != nil {
		t.Fatal("negative idle_min disables", d)
	}
}

func TestCarryVerdict(t *testing.T) {
	sp := spec(t)
	s := snap(StInReview, map[string]string{"phase": PhReviewing, "verdict": "approve", "reviewed_sha": "aaaaaaa"})
	s.Issue.AssigneeType, s.Issue.Assignee = "agent", sp.Roles.Reviewer
	s.Head = "bbbbbbb"
	s.SamePatch = func(a, b string) bool { return a == "aaaaaaa" && b == "bbbbbbb" }
	d := sp.Decide(s, nil)
	if d == nil || d.Tid != "T3c_carry_verdict" || d.Meta["reviewed_sha"] != "bbbbbbb" || d.Rerun {
		t.Fatal(d)
	}
	s.SamePatch = func(a, b string) bool { return false }
	if d := sp.Decide(s, nil); d == nil || d.Tid != "T3b_rereview" {
		t.Fatal("changed patch must re-review", d)
	}
	s.SamePatch = nil
	if d := sp.Decide(s, nil); d == nil || d.Tid != "T3b_rereview" {
		t.Fatal("unknown must re-review", d)
	}
}

// A run that timed out while still queued (never started) counts no attempt:
// it retries with backoff like a provider limit instead of exhausting infra.
func TestQueuedTimeoutCountsNoAttempt(t *testing.T) {
	sp := spec(t)
	s := snap(StInProgress, map[string]string{"phase": PhImplementing, "owner": "a", "attempt_impl": "1", "attempt_infra": "2"})
	ended := now.Add(-10 * time.Minute)
	s.LastRun = &Run{ID: "r", Status: "failed", Reason: "timeout", EndedAt: &ended}
	d := sp.Decide(s, nil)
	if d.Tid != "T5q_provider_limit" || d.Meta["attempt_infra"] != "" || d.Meta["attempt_impl"] != "" {
		t.Fatalf("queued timeout: %+v", d)
	}
	st := now.Add(-20 * time.Minute)
	s.LastRun = &Run{ID: "r", Status: "failed", Reason: "timeout", StartedAt: &st, EndedAt: &ended}
	if d := sp.Decide(s, nil); d.Tid == "T5q_provider_limit" {
		t.Fatalf("a started run that timed out is a real failure: %+v", d)
	}
}

func TestMemberResolveConflictRequest(t *testing.T) {
	sp := spec(t)
	s := snap(StTodo, map[string]string{"phase": PhChangesRequested, "owner": "a", "conflict_requested": "true", "blocked_reason": "precheck conflict: x"})
	s.Head = "h"
	d := sp.Decide(s, nil)
	want(t, d, "T11_resolve_conflict", StInReview, sp.Roles.Reviewer)
	if d.Meta["conflict_requested"] != "" || d.Meta["conflict_from_phase"] != PhChangesRequested {
		t.Fatal(d.Meta)
	}
}

func TestBlockedTransitionsCancelActiveLeadRun(t *testing.T) {
	sp := spec(t)
	st := now.Add(-1 * time.Minute)
	s := snap(StBlocked, map[string]string{"phase": PhUnblocked, "owner": "a", "unblock_confirmed": "true"})
	s.ActiveRun = &Run{ID: "lead-run-1", Status: "running", StartedAt: &st}
	d := sp.Decide(s, nil)
	want(t, d, "T7_unblock", StTodo, "a")
	if d.CancelRun != "lead-run-1" {
		t.Fatalf("T7_unblock CancelRun = %q, want lead-run-1", d.CancelRun)
	}

	sCancel := snap(StBlocked, map[string]string{"phase": PhCancelProposed, "cancel_confirmed": "true"})
	sCancel.ActiveRun = &Run{ID: "lead-run-2", Status: "running", StartedAt: &st}
	dCancel := sp.Decide(sCancel, nil)
	if dCancel == nil || dCancel.Tid != "T8_cancel" || dCancel.CancelRun != "lead-run-2" {
		t.Fatalf("T8_cancel: %+v", dCancel)
	}
}
