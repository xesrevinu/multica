package orchestrator

import "testing"

func pocketSpec(t *testing.T) *Spec {
	s, err := ParseSpec([]byte(`orchestrator:
  roles:
    lead: lead
    reviewer: rev
    owners: [heavy]
    routes:
      - {labels: [area:hard], owners: [gbot]}
      - {labels: [area:studio-app], owners: [front]}
  repo: {mode: github, slug: x/y}
  limits: {slots: {cargo: 2}, min_free_gb: 40}
  acceptance: {default_for_labels: [area:studio-app]}
`))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRoutesPickOwnerByLabel(t *testing.T) {
	sp := pocketSpec(t)
	if !sp.isOwner("gbot") || !sp.isOwner("front") {
		t.Fatal("route owners must join roles.owners")
	}
	s := snap(StBacklog, map[string]string{})
	s.Issue.Labels = []string{"Area:Hard"}
	want(t, sp.Decide(s, ok(false)), "T1_start", StTodo, "gbot")
	s = snap(StBacklog, map[string]string{})
	want(t, sp.Decide(s, ok(false)), "T1_start", StTodo, "heavy")
	s = snap(StBacklog, map[string]string{})
	s.Issue.Labels = []string{"area:hard"}
	s.Busy["gbot"] = true
	if d := sp.Decide(s, ok(false)); d == nil || d.Wait != "all owners busy" {
		t.Fatalf("routed pool busy must wait, not fall back: %+v", d)
	}
	b := snap(StBlocked, map[string]string{"phase": PhUnblocked})
	b.Issue.Labels = []string{"area:studio-app"}
	want(t, sp.Decide(b, ok(false)), "T7_unblock", StTodo, "front")
}

func TestCargoSlots(t *testing.T) {
	sp := pocketSpec(t)
	busy := func(k, st string, l ...string) Issue { return Issue{Key: k, Status: st, Labels: l} }
	s := snap(StBacklog, map[string]string{})
	s.Issue.Labels = []string{"needs:cargo"}
	s.Siblings = []Issue{busy("A", StTodo, "needs:cargo"), busy("B", StInProgress, "needs:cargo"), busy("C", StInProgress)}
	if d := sp.Decide(s, ok(false)); d == nil || d.Wait != "waiting: cargo slots 2/2" {
		t.Fatalf("want slot wait, got %+v", d)
	}
	s.Siblings[1].Status = StInReview
	want(t, sp.Decide(s, ok(false)), "T1_start", StTodo, "heavy")
	s = snap(StBacklog, map[string]string{})
	s.Siblings = []Issue{busy("A", StTodo, "needs:cargo"), busy("B", StTodo, "needs:cargo")}
	want(t, sp.Decide(s, ok(false)), "T1_start", StTodo, "heavy") // unlabelled ignores slots
}

func TestDefaultAcceptanceLabels(t *testing.T) {
	sp := pocketSpec(t)
	if !sp.NeedsAcceptance(&Issue{Labels: []string{"area:studio-app"}}) || !sp.NeedsAcceptance(&Issue{Labels: []string{"needs-GUI-Master"}}) {
		t.Fatal("default_for_labels and built-in labels must gate")
	}
	if sp.NeedsAcceptance(&Issue{Labels: []string{"area:host"}}) {
		t.Fatal("ungated label")
	}
	if sp.Limits.MinFreeGB != 40 {
		t.Fatal("min_free_gb")
	}
}

func TestValidateSettings(t *testing.T) {
	for _, y := range []string{
		"orchestrator:\n  roles: {lead: l, reviewer: r, owners: [a], routes: [{labels: [], owners: [a]}]}\n  repo: {slug: x/y}\n",
		"orchestrator:\n  roles: {lead: l, reviewer: r, owners: [a]}\n  repo: {slug: x/y}\n  limits: {slots: {cargo: 0}}\n",
		"orchestrator:\n  roles: {lead: l, reviewer: r, owners: [a], routes: [{labels: [x], owners: [r]}]}\n  repo: {slug: x/y}\n",
	} {
		if _, err := ParseSpec([]byte(y)); err == nil {
			t.Fatalf("want error for %q", y)
		}
	}
}

func TestBranchOfMatchesDaemonSlug(t *testing.T) {
	sp := spec(t)
	if got := branchOf(sp, "ATT-62"); got != "agent/att-62" {
		t.Fatalf("branchOf = %q", got)
	}
}

func TestFailedReviewerRunCountsAsInfra(t *testing.T) {
	sp := spec(t)
	s := snap(StInReview, map[string]string{"phase": PhReviewing, "attempt_review": "3"})
	s.Issue.Assignee = "rev"
	s.LastRun = &Run{ID: "r", Status: "failed"}
	d := sp.Decide(s, nil)
	if d == nil || d.Tid != "T3b_rereview" || d.Meta["attempt_infra"] != "1" || d.Meta["attempt_review"] != "" {
		t.Fatalf("failed reviewer run must not burn review attempts: %+v", d)
	}
	s.Issue.Meta["attempt_infra"] = "2"
	if d := sp.Decide(s, nil); d == nil || d.Tid != "T6_block" {
		t.Fatalf("want block after infra limit: %+v", d)
	}
}
