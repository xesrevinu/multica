package handler

import (
	"slices"
	"testing"
)

// The orchestrator pushes the daemon's lowercase branch (agent/att-66) and
// puts the uppercase key in the PR title. Both must claim ATT-66 once.
func TestPRLinkLowercaseBranchUppercaseTitle(t *testing.T) {
	got := prClaimedIdentifiers("feat(ops): add seven() export (ATT-66)", "", "agent/att-66")
	if len(got) != 1 || !slices.ContainsFunc(got, func(s string) bool { return s == "ATT-66" }) {
		t.Fatalf("identifiers = %v, want [ATT-66]", got)
	}
	if got := prClaimedIdentifiers("chore: tidy", "", "agent/att-66"); len(got) != 1 || got[0] != "ATT-66" {
		t.Fatalf("branch alone must still link: %v", got)
	}
}
