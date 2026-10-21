package orchestrator

import (
	"testing"
	"time"
)

func TestAttentionAndSort(t *testing.T) {
	t0 := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	mk := func(key, status, phase, prio string, mins int, mod func(*IssueView)) *IssueView {
		v := &IssueView{Key: key, Status: status, Phase: phase, Priority: prio, Orchestrated: true, UpdatedAt: t0.Add(time.Duration(mins) * time.Minute)}
		if mod != nil {
			mod(v)
		}
		v.Attention = attentionOf(v, "me")
		return v
	}
	vs := []*IssueView{
		mk("done", StDone, PhMerged, "urgent", 50, nil),
		mk("queuedLow", StTodo, "", "low", 40, nil),
		mk("run", StInProgress, PhImplementing, "medium", 30, nil),
		mk("accNew", StInReview, PhAwaitAcceptance, "low", 20, nil),
		mk("blockedOld", StBlocked, PhBlocked, "none", 10, nil),
		mk("queuedUrgent", StBacklog, "", "urgent", 5, nil),
		mk("paused", StInProgress, PhImplementing, "high", 1, func(v *IssueView) { v.Paused = true }),
		mk("myReview", StInReview, "", "", 2, func(v *IssueView) {
			v.Orchestrated = false
			v.Assignee = &viewAssignee{Type: "member", ID: "me"}
			v.assigneeID = "me"
		}),
		mk("bare", "", "", "", 0, func(v *IssueView) { v.Orchestrated = false }),
	}
	sortIssueViews(vs)
	var got []string
	for _, v := range vs {
		got = append(got, v.Key+":"+v.Attention.Bucket)
	}
	want := []string{"myReview:needs_you", "blockedOld:needs_you", "accNew:needs_you", "run:running",
		"queuedUrgent:queued", "paused:queued", "queuedLow:queued", "bare:queued", "done:settled"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order\n got  %v\n want %v", got, want)
		}
	}
	if vs[1].Attention.Reason != "blocked" || vs[2].Attention.Reason != "acceptance" {
		t.Fatalf("reasons: %+v %+v", vs[1].Attention, vs[2].Attention)
	}
}

func TestAttemptsOf(t *testing.T) {
	if attemptsOf(map[string]string{}) != nil {
		t.Fatal("no attempts should be nil")
	}
	if a := attemptsOf(map[string]string{"attempt_impl": "2", "attempt_review": "1"}); a == nil || a.Impl != 2 || a.Review != 1 {
		t.Fatalf("got %+v", a)
	}
}
