package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Claim must report the chat session's real channel type for EVERY registered
// channel, not just Slack (MUL-4899).
//
// The binding lookup used to hardcode channel_type='slack', so a Feishu session
// — which writes the same channel_chat_session_binding row under
// channel_type='feishu' (lark/channel_store.go) — came back with an empty
// chat_channel_type, i.e. indistinguishable from a web chat. Downstream that
// mis-flag made the daemon inject `multica attachment upload` guidance into a
// conversation that cannot carry attachments at all.
//
// chat_in_thread stays Slack-only and is asserted as such: it selects between
// `multica chat history` and `multica chat thread`, and both endpoints are
// hardwired to h.SlackHistory (chat_history.go). There is no Feishu reader, so
// the flag has nothing to select between and must not imply one exists.

// seedChannelBinding binds sessionID to a group room on an IM channel of
// channelType, creating the installation the binding requires. This is the row
// shape both the Slack binder (slack/binding.go) and the Feishu store
// (lark/channel_store.go) write — identical but for channel_type, which is
// exactly why the claim lookup must not hardcode one value.
func seedChannelBinding(t *testing.T, ctx context.Context, agentID, sessionID, channelType, lastMessageID, lastThreadID string) {
	t.Helper()
	seedChannelBindingOfChatType(t, ctx, agentID, sessionID, channelType, "group", lastMessageID, lastThreadID)
}

// seedChannelBindingOfChatType is seedChannelBinding with the room shape
// (p2p / group) chosen by the caller — the column the claim must report so the
// per-turn prompt can tell the agent whether anyone else is reading.
func seedChannelBindingOfChatType(t *testing.T, ctx context.Context, agentID, sessionID, channelType, chatType, lastMessageID, lastThreadID string) {
	t.Helper()
	var installationID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO channel_installation (workspace_id, agent_id, channel_type, installer_user_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, testWorkspaceID, agentID, channelType, testUserID).Scan(&installationID); err != nil {
		t.Fatalf("seed %s installation: %v", channelType, err)
	}
	if _, err := testPool.Exec(ctx, `
		WITH binding AS (
			INSERT INTO channel_chat_session_binding
				(chat_session_id, installation_id, channel_type, channel_chat_id, chat_type, last_message_id, last_thread_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING chat_session_id, context_revision
		)
		INSERT INTO channel_chat_context_generation (chat_session_id, revision)
		SELECT chat_session_id, context_revision FROM binding
	`, sessionID, installationID, channelType, "C-TEST-"+channelType, chatType, lastMessageID, lastThreadID); err != nil {
		t.Fatalf("seed %s binding: %v", channelType, err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `
			DELETE FROM channel_task_delivery
			WHERE binding_id IN (
				SELECT id FROM channel_chat_session_binding WHERE chat_session_id = $1
			)
		`, sessionID)
		testPool.Exec(ctx, `DELETE FROM channel_chat_context_generation WHERE chat_session_id = $1`, sessionID)
		testPool.Exec(ctx, `DELETE FROM channel_chat_session_binding WHERE chat_session_id = $1`, sessionID)
		testPool.Exec(ctx, `DELETE FROM channel_installation WHERE id = $1`, installationID)
	})
}

func seedChannelTaskDelivery(t *testing.T, ctx context.Context, taskID, sessionID string) {
	t.Helper()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO channel_task_delivery (
			task_id, binding_id, installation_id, channel_type, channel_chat_id, chat_type,
			channel_message_id, channel_thread_id, route_revision, config
		)
		SELECT $1, id, installation_id, channel_type, channel_chat_id, chat_type,
		       last_message_id, last_thread_id, route_revision, config
		FROM channel_chat_session_binding WHERE chat_session_id = $2
	`, taskID, sessionID); err != nil {
		t.Fatalf("seed channel task delivery: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM channel_task_delivery WHERE task_id = $1`, taskID)
	})
}

// claimedChatChannel is the channel-awareness slice of a claim response.
type claimedChatChannel struct {
	ChannelType   string `json:"chat_channel_type"`
	ChatType      string `json:"chat_type"`
	InThread      bool   `json:"chat_in_thread"`
	DeliversFiles bool   `json:"chat_channel_delivers_files"`
}

// claimChatChannelFields claims the queued task for runtimeID and returns the
// channel-awareness fields the daemon reads off the claim response.
func claimChatChannelFields(t *testing.T, runtimeID string) claimedChatChannel {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "claim-channel-type")
	req = withURLParam(req, "runtimeId", runtimeID)

	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Task *claimedChatChannel `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if resp.Task == nil {
		t.Fatal("expected a claimed task")
	}
	return *resp.Task
}



// A channel whose adapter the claim handler does not name must still be
// reported. The lookup enumerated a fixed {slack, feishu} list, so every
// channel added after it — WeCom is the first — claimed as a web chat and
// inherited the web chat's `multica attachment upload` guidance, the exact
// MUL-4899 failure the fixed list was supposed to have ended.
//
// The list is what is under test here, not WeCom: the binding row shape is
// identical for every channel, so a handler that reads the row instead of
// guessing the channel_type cannot regress the next one either.



// The room shape rides the same binding row as the channel type and must reach
// the daemon with it. Without it the per-turn prompt has no way to know that one
// chat_session is a room shared by many people, and it described every chat run
// as a private 1:1 — the agent then had no input from which to weigh a wider
// audience. The column is written by the shared session service for every
// channel (channel/engine/session.go) and the claim copies it the same way for
// all of them, so each shape is asserted once, on different channels; the
// channel type itself is pinned by the ChannelType tests above.

// A web chat has no binding row, so there is no room shape to report. It comes
// back empty rather than fabricating "p2p": AudienceOf pairs that empty shape
// with an empty channel to infer web direct, while an external channel whose
// shape is empty remains unknown.

// requeueTaskForClaim puts the session's task back in the queue so the claim
// endpoint can hand it out (insertChannelChatTask creates it already running).
func requeueTaskForClaim(t *testing.T, ctx context.Context, sessionID string) {
	t.Helper()
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue
		SET status = 'queued', started_at = NULL, dispatched_at = NULL
		WHERE chat_session_id = $1
	`, sessionID); err != nil {
		t.Fatalf("requeue task: %v", err)
	}
}

// declareFileDeliveryForTest turns the deployment's file-delivery capability on
// for one test and puts it back afterwards. Production writes this map once at
// boot (cmd/server/router.go) and never again, so a test that changes it has to
// restore it or leak the capability into every claim that follows.
func declareFileDeliveryForTest(t *testing.T, channelTypes ...string) {
	t.Helper()
	prev := testHandler.channelFileDelivery
	t.Cleanup(func() { testHandler.channelFileDelivery = prev })
	testHandler.channelFileDelivery = map[string]bool{}
	for _, ct := range channelTypes {
		testHandler.DeclareChannelFileDelivery(ct)
	}
}

// Whether a file the agent produces reaches the conversation is the SERVER's
// answer, and this is where it is given.
//
// It cannot be worked out from the channel type, which is the shape the daemon
// used to have. Delivery needs two things — an adapter that goes back for the
// bound attachment, and object storage for it to go back to — and only the
// process that wired both knows whether it has them. A deployment running the
// WeCom adapter with no storage configured has the first and not the second, so
// "channel_type == wecom" answers true where the truth is false, and the agent
// is told to send a file that nothing will carry.
//
// The rows below are the same binding under different deployments, which is
// precisely what a channel-type answer cannot distinguish.
