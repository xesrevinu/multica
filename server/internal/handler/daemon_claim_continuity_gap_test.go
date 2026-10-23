package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// claimContinuityGapProbe decodes just the continuity-gap fields off a claim
// response so the two MUL-5305 disclosure paths can be asserted end-to-end.
type claimContinuityGapProbe struct {
	Task *struct {
		ID                            string `json:"id"`
		PriorSessionID                string `json:"prior_session_id"`
		PriorSessionResumeUnavailable bool   `json:"prior_session_resume_unavailable"`
	} `json:"task"`
}

func claimOneTaskForRuntime(t *testing.T, runtimeID, daemonID string) claimContinuityGapProbe {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/claim", nil, testWorkspaceID, daemonID)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var probe claimContinuityGapProbe
	if err := json.NewDecoder(w.Body).Decode(&probe); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if probe.Task == nil {
		t.Fatal("expected a claimed task in the response")
	}
	return probe
}

// TestClaimTaskByRuntime_ChatRolloutMissingDisclosesGap is the claim-response
// half of MUL-5305 Must-fix 2 for chat: when the most recent terminal task on a
// chat session withheld its Codex session (rollout missing), the next chat claim
// resumes the older pointer but MUST still set prior_session_resume_unavailable
// so the run discloses the gap instead of silently continuing.

// TestClaimTaskByRuntime_RerunSourceRolloutMissingDisclosesGap is the
// claim-response half of MUL-5305 Must-fix 2 for manual rerun: when the exact
// source task withheld its Codex session, the rerun has nothing resumable from
// it and MUST disclose the gap rather than silently start fresh.
