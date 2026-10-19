package orchestrator

import (
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
	if s.CheckAgentMeta("p", "attempt_impl", "1") == "" || s.CheckAgentMeta("p", "phase", `"merged"`) == "" || s.CheckAgentMeta("p", "phase", `"review_requested"`) != "" || s.CheckAgentMeta("p", "verdict", `"approve"`) != "" {
		t.Error("agent metadata guard")
	}
}
