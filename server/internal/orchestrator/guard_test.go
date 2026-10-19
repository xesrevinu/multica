package orchestrator

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
)

func TestGuard(t *testing.T) {
	s := New(Options{Bus: events.New()})
	s.disabled = false
	s.teams["p"] = &Team{ProjectID: "p", Spec: &Spec{}}
	s.svcUserID = "svc"
	cases := []struct {
		in      GuardInput
		refused bool
	}{
		{GuardInput{ProjectID: "other", CallerType: "agent", ToStatus: StDone}, false},
		{GuardInput{ProjectID: "p", CallerType: "agent", FromStatus: StTodo, ToStatus: StInProgress}, false},
		{GuardInput{ProjectID: "p", CallerType: "agent", FromStatus: StInProgress, ToStatus: StInReview}, true},
		{GuardInput{ProjectID: "p", CallerType: "agent", AssigneeChange: true}, true},
		{GuardInput{ProjectID: "p", CallerType: "member", UserID: "ray", ToStatus: StDone}, true},
		{GuardInput{ProjectID: "p", CallerType: "member", UserID: "ray", ToStatus: StDone, Override: true}, false},
		{GuardInput{ProjectID: "p", CallerType: "member", UserID: "svc", ToStatus: StDone}, false},
		{GuardInput{ProjectID: "p", CallerType: "agent"}, false},
	}
	for i, c := range cases {
		if got := s.CheckWrite(c.in) != ""; got != c.refused {
			t.Errorf("case %d: refused=%v want %v", i, got, c.refused)
		}
	}
	s.teams["p"].Paused = true
	if s.CheckWrite(GuardInput{ProjectID: "p", CallerType: "agent", ToStatus: StDone}) != "" {
		t.Error("paused team must not guard")
	}
	s.teams["p"].Paused = false
	if s.CheckAgentMeta("p", "attempt_impl", "1") == "" || s.CheckAgentMeta("p", "phase", `"merged"`) == "" || s.CheckAgentMeta("p", "phase", `"review_requested"`) != "" || s.CheckAgentMeta("p", "phase", `"unblocked"`) != "" || s.CheckAgentMeta("p", "phase", `"cancel_proposed"`) != "" || s.CheckAgentMeta("p", "verdict", `"approve"`) != "" || s.CheckAgentMeta("p", "risk", `"High"`) != "" || s.CheckAgentMeta("p", "risk", `"severe"`) == "" {
		t.Error("agent metadata guard")
	}
}

func ownsP() *Service {
	s := New(Options{Bus: events.New()})
	s.disabled = false
	s.teams["p"] = &Team{ProjectID: "p", Spec: &Spec{}}
	s.svcUserID = "svc"
	return s
}

func TestAgentsCannotForgeMemberConfirmations(t *testing.T) {
	s := ownsP()
	for _, k := range []string{"cancel_confirmed", "unblock_confirmed", "unblock_count", "unblock_escalated", "cancel_notified"} {
		if s.CheckAgentMeta("p", k, `"true"`) == "" {
			t.Fatalf("agent wrote %s", k)
		}
	}
}

func TestAgentMetaDeleteAndValues(t *testing.T) {
	s := ownsP()
	refused := func(w MetaWrite) bool { w.ProjectID = "p"; return s.CheckAgentMetaWrite(w) != "" }
	cases := []struct {
		name string
		w    MetaWrite
		want bool
	}{
		{"delete attempt counter", MetaWrite{Key: "attempt_impl", Delete: true}, true},
		{"delete acceptance", MetaWrite{Key: "acceptance", Delete: true}, true},
		{"delete paused", MetaWrite{Key: "paused", Delete: true}, true},
		{"delete phase", MetaWrite{Key: "phase", Delete: true}, true},
		{"delete risk", MetaWrite{Key: "risk", Delete: true}, true},
		{"delete own scratch key", MetaWrite{Key: "notes", Delete: true}, false},
		{"verdict approve", MetaWrite{Key: "verdict", Value: `"approve"`}, false},
		{"verdict changes", MetaWrite{Key: "verdict", Value: `"changes"`}, false},
		{"verdict approved", MetaWrite{Key: "verdict", Value: `"approved"`}, true},
		{"verdict lgtm", MetaWrite{Key: "verdict", Value: `"lgtm"`}, true},
		{"risk raise", MetaWrite{Key: "risk", Value: `"high"`, Current: map[string]string{"risk": "low"}}, false},
		{"risk lower", MetaWrite{Key: "risk", Value: `"low"`, Current: map[string]string{"risk": "high"}}, true},
		{"risk first set", MetaWrite{Key: "risk", Value: `"low"`}, false},
		{"implementing while blocked", MetaWrite{Key: "phase", Value: `"implementing"`, Status: StBlocked}, true},
		{"implementing while in_review", MetaWrite{Key: "phase", Value: `"implementing"`, Status: StInReview}, true},
		{"review_requested while in_progress", MetaWrite{Key: "phase", Value: `"review_requested"`, Status: StInProgress}, false},
		{"unblocked while blocked", MetaWrite{Key: "phase", Value: `"unblocked"`, Status: StBlocked}, false},
		{"unblocked while todo", MetaWrite{Key: "phase", Value: `"unblocked"`, Status: StTodo}, true},
		{"cancel_proposed while in_review", MetaWrite{Key: "phase", Value: `"cancel_proposed"`, Status: StInReview}, true},
		{"blocked while in_review", MetaWrite{Key: "phase", Value: `"blocked"`, Status: StInReview}, false},
	}
	for _, c := range cases {
		if got := refused(c.w); got != c.want {
			t.Errorf("%s: refused=%v want %v (%s)", c.name, got, c.want, s.CheckAgentMetaWrite(MetaWrite{ProjectID: "p", Key: c.w.Key, Value: c.w.Value, Status: c.w.Status, Current: c.w.Current, Delete: c.w.Delete}))
		}
	}
	if s.CheckAgentMetaWrite(MetaWrite{ProjectID: "other", Key: "attempt_impl", Delete: true}) != "" {
		t.Error("projects without a team are not guarded")
	}
}

func TestAgentControlLabels(t *testing.T) {
	s := ownsP()
	s.teams["p"].Spec = &Spec{Acceptance: Acceptance{Labels: []string{"needs-acceptance", "needs-Ray-signoff"}, DefaultForLabels: []string{"area:studio-app"}}}
	cases := []struct {
		label  string
		attach bool
		want   bool
	}{
		{"needs-Ray-signoff", false, true},
		{"NEEDS-RAY-SIGNOFF", false, true},
		{"area:studio-app", false, true},
		{"needs-acceptance", true, false}, // raising the gate is fine
		{"orchestrator-pause", true, true},
		{"orchestrator-pause", false, true},
		{"area:host", false, false},
		{"needs:cargo", true, false},
	}
	for _, c := range cases {
		if got := s.CheckAgentLabel("p", c.label, c.attach) != ""; got != c.want {
			t.Errorf("%s attach=%v: refused=%v want %v", c.label, c.attach, got, c.want)
		}
	}
	if s.CheckAgentLabel("other", "orchestrator-pause", true) != "" {
		t.Error("unorchestrated project")
	}
	s.teams["p"].Paused = true
	if s.CheckAgentLabel("p", "needs-Ray-signoff", false) != "" {
		t.Error("paused team: people took over, no guard")
	}
}

// `issue assign --no-start` stays 403: the orchestrator owns the assignee
// whether or not a run starts. The message names what was refused and why
// --no-start does not help.
func TestCheckWriteMessagesNameTheRefusal(t *testing.T) {
	s := ownsP()
	m := s.CheckWrite(GuardInput{ProjectID: "p", CallerType: "member", UserID: "u", FromStatus: StTodo, AssigneeChange: true})
	for _, want := range []string{"refused: assignee change", "--no-start does not lift this", "--force"} {
		if !strings.Contains(m, want) {
			t.Errorf("member message %q misses %q", m, want)
		}
	}
	if s.CheckWrite(GuardInput{ProjectID: "p", CallerType: "member", UserID: "u", FromStatus: StTodo, AssigneeChange: true, Override: true}) != "" {
		t.Error("member override must pass")
	}
	a := s.CheckWrite(GuardInput{ProjectID: "p", CallerType: "agent", UserID: "u", FromStatus: StTodo, ToStatus: StDone})
	if !strings.Contains(a, "refused: status todo → done") || !strings.Contains(a, "--no-start") {
		t.Errorf("agent message %q", a)
	}
}

// orchestrator-pause: only a human member adds it; a human or the engine
// removes it; agents never touch it - also while the team is paused, and also
// when it is supplied at issue creation (an attach).
func TestPauseLabelOnlyHumansAdd(t *testing.T) {
	s := ownsP()
	cases := []struct {
		name   string
		w      LabelWrite
		refuse bool
	}{
		{"human adds", LabelWrite{CallerType: "member", UserID: "u1", Attach: true}, false},
		{"human removes", LabelWrite{CallerType: "member", UserID: "u1"}, false},
		{"engine removes", LabelWrite{CallerType: "member", UserID: "svc"}, false},
		{"engine adds", LabelWrite{CallerType: "member", UserID: "svc", Attach: true}, true},
		{"agent adds", LabelWrite{CallerType: "agent", UserID: "a1", Attach: true}, true},
		{"agent removes", LabelWrite{CallerType: "agent", UserID: "a1"}, true},
	}
	for _, paused := range []bool{false, true} {
		s.teams["p"].Paused = paused
		for _, c := range cases {
			c.w.ProjectID, c.w.Label = "p", "Orchestrator-Pause"
			if got := s.CheckLabel(c.w) != ""; got != c.refuse {
				t.Errorf("paused=%v %s: refused=%v want %v", paused, c.name, got, c.refuse)
			}
		}
	}
	s.teams["p"].Paused = false
	s.teams["p"].Spec = &Spec{Acceptance: Acceptance{Labels: []string{"needs-acceptance"}}}
	if s.CheckLabel(LabelWrite{ProjectID: "p", CallerType: "agent", Label: "needs-acceptance", Attach: true}) != "" {
		t.Error("agents may add an acceptance label (at creation too)")
	}
	if s.CheckLabel(LabelWrite{ProjectID: "p", CallerType: "member", UserID: "u1", Label: "needs-acceptance"}) != "" {
		t.Error("members may remove an acceptance label")
	}
}
