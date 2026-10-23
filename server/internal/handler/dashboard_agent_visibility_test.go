package handler

import (
	"testing"
)

// dashboardAgentPresence is the expected membership of the three agent ids that
// matter in a per-agent dashboard response.
type dashboardAgentPresence struct {
	privateAgent bool
	publicAgent  bool
	sentinel     bool
}

// TestDashboardPerAgentRollupsFoldRestrictedAgents is the regression guard for
// MUL-5409: the three per-agent dashboard rollups used to authorize on
// workspace membership alone and return a bare agent_id for EVERY agent in the
// workspace, telling a plain member that someone else's private agent exists,
// how much it spends, how long it runs, and what it fails on.
//
// The contract now: rows for agents the caller may not view are folded onto the
// `__restricted_agents__` sentinel — never dropped — so
//
//   - no private agent's UUID appears in a plain member's response, and
//   - every aggregate still sums to exactly what the privileged view sums to,
//     which is what keeps the per-agent breakdown reconciling with the
//     workspace-level KPIs rendered beside it.
//
// A public_to agent is seeded alongside the private one so the test also proves
// the fold is selective rather than "the member sees nothing".

// TestDashboardPerAgentRollupsFoldSystemAgentCarriers covers the population the
// first version of the MUL-5409 fix missed. `kind = 'system'` agents — the
// hidden execution carriers behind agent-builder sessions — run real tasks and
// book real usage, and the three rollup queries aggregate over
// agent_task_queue / task_usage without any kind filter. But NO list endpoint
// returns them (ListAgents / ListAllAgents both filter `kind = 'user'`), so no
// client can resolve one to a name.
//
// That makes them the sharpest form of both bugs at once: a bare UUID that
// leaks one member's builder session to every other member, and — once the
// agent list loads — a live agent folded into the client's "Deleted agents"
// row. They are therefore restricted for EVERYONE, workspace owner included,
// which is what this test pins: the carrier's own owner must not get its UUID
// either, and the totals must survive the fold.

func dashboardAgentIDSet[T any](rows []T, idOf func(T) string) map[string]struct{} {
	out := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		out[idOf(r)] = struct{}{}
	}
	return out
}

func assertDashboardAgentPresence(
	t *testing.T,
	label string,
	ids map[string]struct{},
	want dashboardAgentPresence,
	privateAgentID, publicAgentID string,
) {
	t.Helper()
	check := func(what, id string, expected bool) {
		_, got := ids[id]
		if got != expected {
			t.Errorf("%s: %s (%s) present=%v, want %v", label, what, id, got, expected)
		}
	}
	check("private agent", privateAgentID, want.privateAgent)
	check("public agent", publicAgentID, want.publicAgent)
	check("restricted bucket", restrictedAgentsRowID, want.sentinel)
}
