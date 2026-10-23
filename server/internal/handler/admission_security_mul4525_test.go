package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

)

// seedSecurityTestOwner creates a throwaway workspace member to own an agent, so
// the agent is NOT owned by testUserID (who must be able to VIEW but not INVOKE).
func seedSecurityTestOwner(t *testing.T, label string) string {
	t.Helper()
	ctx := context.Background()
	var ownerID string
	if err := testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		label, fmt.Sprintf("%s-%d@multica.test", label, time.Now().UnixNano())).Scan(&ownerID); err != nil {
		t.Fatalf("seed owner user: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, ownerID) })
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		testWorkspaceID, ownerID); err != nil {
		t.Fatalf("seed owner member: %v", err)
	}
	return ownerID
}

// readReasonCode pulls the stable reason_code out of a structured blocked-admission
// response body (MUL-4525).
func readReasonCode(t *testing.T, body []byte) string {
	t.Helper()
	var resp struct {
		Error      string `json:"error"`
		ReasonCode string `json:"reason_code"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode blocked response: %v (body=%s)", err, string(body))
	}
	return resp.ReasonCode
}

// TestSendChatMessage_InvokeRevokedAfterSessionCreate is the MUL-4525 must-fix 3
// chat acceptance test: a session created while the user could invoke the agent
// must stop sending the instant that invoke permission is revoked — even though
// the user (a workspace owner) can still VIEW the transcript. The refusal is a
// structured 403 with a stable reason_code, and NOTHING is persisted: no chat
// message, no task.

// TestRerunIssue_PrivateHistoricalAgent is the MUL-4525 must-fix 3 rerun
// acceptance test, driven through the REAL handler + canInvokeAgent (not an
// injected callback): a user who can see the issue but cannot invoke its private
// agent is refused with a structured 403 and mutates nothing; the agent's owner
// is allowed.
