package orchestrator

import "testing"

func TestStartRespectsHolderAndBacklog(t *testing.T) {
	sp := spec(t)

	// backlog is never touched, even with engine phases on it
	b := snap(StBacklog, map[string]string{"phase": PhImplementing, "owner": "a"})
	if d := sp.Decide(b, nil); d != nil {
		t.Fatalf("backlog must be untouched, got %+v", d)
	}

	// a member holds the todo issue: not ours
	m := fresh()
	m.Issue.AssigneeType, m.Issue.Assignee = "member", "u1"
	if d := sp.Decide(m, nil); d != nil {
		t.Fatalf("member-held todo must be left alone, got %+v", d)
	}

	// an agent outside the owner pool holds it: not ours either
	l := fresh()
	l.Issue.AssigneeType, l.Issue.Assignee = "agent", "lead"
	if d := sp.Decide(l, nil); d != nil {
		t.Fatalf("non-owner agent must be left alone, got %+v", d)
	}

	// an owner already assigned without a run: same assignee does not
	// dispatch, so T1 re-runs it
	o := fresh()
	o.Issue.AssigneeType, o.Issue.Assignee = "agent", "b"
	d := sp.Decide(o, nil)
	want(t, d, "T1_start", StInProgress, "b")
	if !d.Rerun {
		t.Fatal("T1 onto the current assignee must re-run")
	}

	// unassigned: the assignee PUT dispatches, no extra rerun
	if d := sp.Decide(fresh(), nil); d.Rerun {
		t.Fatal("T1 onto a new assignee must not also re-run")
	}

	// owner assigned and its run already active: adopt, no PUT, no rerun
	r := fresh()
	r.Issue.AssigneeType, r.Issue.Assignee = "agent", "b"
	at := now
	r.ActiveRun = &Run{ID: "r1", Status: "running", StartedAt: &at}
	d = sp.Decide(r, nil)
	if d == nil || d.Tid != "T1_start" || d.Assignee != "" || d.Rerun || d.Meta["owner"] != "b" {
		t.Fatalf("want adopt, got %+v", d)
	}

	// crash between T1's metadata commit and the assignee PUT
	c := fresh()
	c.Issue.Meta = map[string]string{"phase": PhImplementing, "owner": "a", "attempt_impl": "1"}
	want(t, sp.Decide(c, nil), "T9_reconcile", StTodo, "a")
}
