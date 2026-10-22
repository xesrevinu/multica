package orchestrator

import (
	"strings"
	"testing"
)

func promoteSpec(t *testing.T) *Spec {
	sp := spec(t)
	sp.AutoStart = AutoStartOn
	return sp
}

func backlog(desc string, sib ...Issue) *Snapshot {
	s := snap(StBacklog, map[string]string{})
	s.Issue.AssigneeType, s.Issue.Assignee = "", ""
	s.Issue.Description = desc
	s.Siblings = append([]Issue{s.Issue}, sib...)
	return s
}

func TestParseDeps(t *testing.T) {
	for _, c := range []struct {
		desc         string
		keys         int
		found, valid bool
	}{
		{"x\n依赖: OX-173（M0）、OX-129（N2）（stage 2）\n", 2, true, true},
		{"依赖：无（stage 1）", 0, true, true},
		{"Depends on: ATT-2", 1, true, true},
		{"依赖: 核心体积基线", 0, true, false},
		{"no line", 0, false, true},
	} {
		k, f, ok := parseDeps(c.desc)
		if len(k) != c.keys || f != c.found || ok != c.valid {
			t.Errorf("%q: keys=%v found=%v ok=%v", c.desc, k, f, ok)
		}
	}
}

func TestAutoPromoteDispatchesStraightToInProgress(t *testing.T) {
	sp := promoteSpec(t)
	s := backlog("依赖: ATT-2", Issue{ID: "2", Key: "ATT-2", Status: StDone})
	d := sp.Decide(s, nil)
	want(t, d, "T0_dispatch", StInProgress, "a")
	if !d.StartsRun() || d.Meta["phase"] != PhImplementing {
		t.Fatal(d)
	}
}

func TestAutoPromoteOff(t *testing.T) {
	sp := spec(t)
	if d := sp.Decide(backlog("依赖: 无"), nil); d != nil {
		t.Fatalf("opt-in only: %+v", d)
	}
}

func TestAutoPromoteWaits(t *testing.T) {
	sp := promoteSpec(t)
	if d := sp.Decide(backlog("依赖: ATT-2", Issue{ID: "2", Key: "ATT-2", Status: StInProgress}), nil); d == nil || d.Tid != "" || !strings.Contains(d.Wait, "ATT-2") {
		t.Fatalf("dep not done: %+v", d)
	}
	if d := sp.Decide(backlog("依赖: ATT-9"), nil); d == nil || d.Tid != "" || d.Wait == "" {
		t.Fatalf("unknown dep: want a wait, got %+v", d)
	}
	if d := sp.Decide(backlog("依赖: 基线"), nil); d == nil || d.Tid != "" || d.Wait == "" {
		t.Fatalf("unparseable: want a wait, got %+v", d)
	}
	// epic
	if d := sp.Decide(backlog("依赖: 无", Issue{ID: "3", Key: "ATT-3", ParentID: "1", Status: StTodo}), nil); d == nil || d.Tid != "" {
		t.Fatalf("backlog epic is never moved: %+v", d)
	}
	// stage barrier
	s := backlog("依赖: 无", Issue{ID: "4", Key: "ATT-4", ParentID: "p", Stage: 1, Status: StInProgress})
	s.Issue.ParentID, s.Issue.Stage = "p", 2
	if d := sp.Decide(s, nil); d == nil || d.Tid != "" || d.Wait != "waiting: stage 1 sibling ATT-4 not done" {
		t.Fatalf("stage: %+v", d)
	}
	// honored assignee busy
	s = backlog("依赖: 无")
	s.Issue.AssigneeType, s.Issue.Assignee = "agent", "b"
	s.Busy["b"] = true
	if d := sp.Decide(s, nil); d == nil || d.Tid != "" {
		t.Fatalf("assignee busy: want wait, got %+v", d)
	}
	delete(s.Busy, "b")
	want(t, sp.Decide(s, nil), "T0_dispatch", StInProgress, "b")
}

func TestAutoStartDryRunOnlyReports(t *testing.T) {
	sp := spec(t)
	sp.AutoStart = AutoStartDryRun
	d := sp.Decide(backlog("依赖: ATT-2", Issue{ID: "2", Key: "ATT-2", Status: StDone}), nil)
	if d == nil || d.Tid != "" || d.Wait != "auto-start dry run: would start → a" {
		t.Fatalf("%+v", d)
	}
	d = sp.Decide(backlog("依赖: ATT-2", Issue{ID: "2", Key: "ATT-2", Status: StTodo}), nil)
	if d == nil || d.Tid != "" || !strings.Contains(d.Wait, "would not start (waiting: dependency ATT-2") {
		t.Fatalf("%+v", d)
	}
}

func TestEpicAggregatesChildren(t *testing.T) {
	sp := spec(t)
	kids := func(st ...string) []Issue {
		var out []Issue
		for n, x := range st {
			out = append(out, Issue{ID: "c" + itoa(n), Key: "ATT-" + itoa(10+n), ParentID: "1", Status: x})
		}
		return out
	}
	s := snap(StInProgress, map[string]string{})
	s.Issue.AssigneeType, s.Issue.Assignee = "", ""
	s.Siblings = kids(StDone, StInProgress, StBacklog)
	if d := sp.Decide(s, nil); d == nil || d.Tid != "" || d.Wait != "epic: 1/3 sub-issues done" {
		t.Fatalf("%+v", d)
	}
	s.Siblings = kids(StDone, StCancelled, StDone)
	want(t, sp.Decide(s, nil), "T_epic_done", StDone, "")
	s.Issue.Labels = []string{"needs-acceptance"}
	sp.Acceptance.Labels = []string{"needs-acceptance"}
	want(t, sp.Decide(s, nil), "T_epic_acceptance", StInReview, "@human")
	s.Issue.Status, s.Issue.Meta = StInReview, map[string]string{"phase": PhAwaitAcceptance, "acceptance": "ok"}
	want(t, sp.Decide(s, nil), "T_epic_done", StDone, "")
	// backlog epic with nothing started stays put
	s = snap(StBacklog, map[string]string{})
	s.Siblings = kids(StBacklog, StBacklog)
	if d := sp.Decide(s, nil); d == nil || d.Tid != "" {
		t.Fatalf("%+v", d)
	}
}

func TestTodoReadsDescriptionDeps(t *testing.T) {
	sp := spec(t)
	s := fresh()
	s.Issue.Description = "依赖: ATT-2（M0）"
	s.Siblings = []Issue{s.Issue, {ID: "2", Key: "ATT-2", Status: StInProgress}}
	if d := sp.Decide(s, nil); d == nil || d.Wait != "waiting: dependency ATT-2 not done" {
		t.Fatalf("%+v", d)
	}
	s.Siblings[1].Status = StDone
	want(t, sp.Decide(s, nil), "T1_start", StInProgress, "a")
}

func TestQueuedForWorktreeIsVisible(t *testing.T) {
	sp := spec(t)
	s := snap(StInProgress, map[string]string{"phase": PhImplementing, "owner": "a", "attempt_impl": "1"})
	c := now
	s.ActiveRun = &Run{ID: "r", Status: "waiting_local_directory", CreatedAt: &c}
	if d := sp.Decide(s, nil); d == nil || d.Wait != "queued for worktree: another task holds the checkout" {
		t.Fatalf("%+v", d)
	}
}

func TestStartedIssueWithFollowUpChildIsNotAnEpic(t *testing.T) {
	sp := spec(t)
	s := snap(StInProgress, map[string]string{"phase": PhReviewRequested, "owner": "a", "attempt_impl": "1", "attempt_review": "0"})
	s.Head = "h1"
	s.Siblings = []Issue{{ID: "c", Key: "ATT-9", ParentID: "1", Status: StBacklog}}
	if d := sp.Decide(s, ok(false)); d == nil || d.Tid != "T2_review" {
		t.Fatalf("want review, got %+v", d)
	}
}
