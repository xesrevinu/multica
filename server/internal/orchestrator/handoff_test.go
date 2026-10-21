package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hoSpec() *Spec {
	sp := &Spec{Roles: Roles{Lead: "lead", Reviewer: "rev", Owners: []string{"a"}}, Repo: RepoSpec{Mode: "github", Slug: "x/y"}}
	sp.Defaults()
	return sp
}

func TestBuildHandoffClassification(t *testing.T) {
	sp := hoSpec()
	base := func() *Snapshot {
		return &Snapshot{Issue: Issue{Key: "ATT-1", Assignee: "a", AssigneeType: "agent", Status: StInReview,
			Meta: map[string]string{"owner": "a", "attempt_impl": "1", "head_sha": "h1", "pr": "https://github.com/x/y/pull/7"}}, Head: "h1"}
	}
	cases := []struct {
		d    *Decision
		run  *Run
		kind string
		role string
	}{
		{&Decision{Tid: "T3_rework", Why: "reviewer verdict=changes", Assignee: "a"}, nil, HKReviewChanges, "owner"},
		{&Decision{Tid: "T3_rework", Why: "precheck checks_red: test=FAILURE", Assignee: "a"}, nil, HKCIFailure, "owner"},
		{&Decision{Tid: "T3_rework", Why: "precheck conflict: x", Assignee: "a"}, nil, HKConflict, "owner"},
		{&Decision{Tid: "T3_rework", Why: "precheck check_failed: npm test exited 1", Assignee: "a"}, nil, HKCheckFailure, "owner"},
		{&Decision{Tid: "T3_rework", Why: "acceptance rejected: ugly", Assignee: "a"}, nil, HKAcceptanceReject, "owner"},
		{&Decision{Tid: "T6_block", Why: "dead-letter: 5 consecutive orchestrator failures (x)", Assignee: "lead"}, nil, HKDeadLetter, "lead"},
		{&Decision{Tid: "T6_block", Why: "provider auth/access failure (agent_error.provider_auth_or_access)", Assignee: "lead"}, &Run{Status: "failed", Reason: "agent_error.provider_auth_or_access"}, HKProviderAuth, "lead"},
		{&Decision{Tid: "T6_block", Why: "impl attempts exhausted (3/3)", Assignee: "lead"}, nil, HKAttemptsExhausted, "lead"},
		{&Decision{Tid: "T6_block", Why: "branch moved during review (a -> b)", Assignee: "lead"}, nil, HKBranchMoved, "lead"},
		{&Decision{Tid: "T5_rerun", Rerun: true}, &Run{ID: "r1", Status: "cancelled"}, HKRunCancelled, "owner"},
		{&Decision{Tid: "T5_rerun", Rerun: true}, &Run{ID: "r1", Status: "completed"}, HKNoHandover, "owner"},
		{&Decision{Tid: "T3b_rereview", Rerun: true}, &Run{ID: "r1", Status: "failed", Reason: "agent_error.unknown"}, HKRunFailure, "reviewer"},
	}
	for _, c := range cases {
		s := base()
		s.LastRun = c.run
		h := sp.BuildHandoff(s, c.d)
		if h == nil || h.Kind != c.kind || h.Role != c.role {
			t.Fatalf("%s %q: got %+v, want %s/%s", c.d.Tid, c.d.Why, h, c.kind, c.role)
		}
		if h.PrevHead != "h1" || h.PR == "" {
			t.Fatalf("%s: missing head/pr: %+v", c.d.Tid, h)
		}
	}
	// not send-backs
	for _, d := range []*Decision{{Tid: "T1_start"}, {Tid: "T2_review"}, {Tid: "T4_done"}, {Tid: "T3b_rereview", Rerun: true}} {
		if h := sp.BuildHandoff(base(), d); h != nil {
			t.Fatalf("%s: unexpected handoff %+v", d.Tid, h)
		}
	}
}

func TestHandoffRenderGoldenAndBudget(t *testing.T) {
	h := &Handoff{Kind: HKCIFailure, Role: "owner", To: "a", Summary: "precheck checks_red: test=FAILURE",
		Attempts: map[string]int{"impl": 2, "review": 1, "infra": 0, "quota": 0, "unblock_count": 0}, Attempt: 2,
		PrevHead: "0123456789abcdef", ReviewedSHA: "fedcba9876543210", PR: "https://github.com/x/y/pull/7",
		DiffSummary: "2 files, +10 -3: src/a.js (+8 -1), test/a.test.js (+2 -2)",
		Review:      []HandoffComment{{Source: "github_inline", Author: "rev", Path: "src/a.js", Line: 12, Body: "[important] off-by-one\nloop skips last"}},
		Checks:      []HandoffCheck{{Name: "test", Conclusion: "failure", URL: "https://github.com/x/y/runs/1", Annotations: []string{"failure test/a.test.js:4 expected 3 got 2"}, LogExcerpt: "1 failing"}},
	}
	got := h.Render()
	golden := filepath.Join("testdata", "handoff", "ci_failure.golden.md")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		_ = os.MkdirAll(filepath.Dir(golden), 0o755)
		_ = os.WriteFile(golden, []byte(got), 0o644)
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("render drift (UPDATE_GOLDEN=1 to accept):\n%s", got)
	}
	h.Checks[0].LogExcerpt = strings.Repeat("x", 50000)
	for i := 0; i < 40; i++ {
		h.Review = append(h.Review, HandoffComment{Source: "issue", Body: strings.Repeat("y", 900)})
	}
	if n := len(h.Render()); n > HandoffBriefBudget+40 {
		t.Fatalf("brief %d bytes exceeds budget %d", n, HandoffBriefBudget)
	}
}
