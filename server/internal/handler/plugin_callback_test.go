package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

// The identity model, exercised through the path that actually decides it.
//
// A hook handler calls back with a token, and the token — not the request body,
// not a header — says who the resulting writes belong to. This is the whole
// reason the callback token exists instead of handing a plugin's server the
// install token and trusting it to say who it is acting for.

func callbackRequest(token, method, path string, body any, params map[string]string) *http.Request {
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	// No X-User-ID: a plugin's own server has no session. The token is the
	// entire credential, which is what these tests are about.
	request.Header.Set("Authorization", "Bearer "+token)
	routeContext := chi.NewRouteContext()
	for key, value := range params {
		routeContext.URLParams.Add(key, value)
	}
	return request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
}

func issueCallbackToken(t *testing.T, installationID string, actor service.HookActor) string {
	t.Helper()
	installation, err := testHandler.PluginService.InstallationForWorkspace(
		context.Background(), parseUUID(testWorkspaceID), installationID)
	if err != nil {
		t.Fatalf("load installation: %v", err)
	}
	token, err := testHandler.PluginService.Callbacks.Issue(context.Background(), service.HookInvocation{
		Installation: installation,
		Hook:         plugincontract.Hook{Key: "summarize"},
		Trigger:      plugincontract.TriggerManual,
		Actor:        actor,
	})
	if err != nil {
		t.Fatalf("issue callback token: %v", err)
	}
	return token
}

func withCallbackTokens(t *testing.T) {
	t.Helper()
	previous := testHandler.PluginService.Callbacks
	testHandler.PluginService.Callbacks = service.NewCallbackTokens()
	t.Cleanup(func() { testHandler.PluginService.Callbacks = previous })
}

// A ui/manual hook ran because a person asked. Its writes stay that person's,
// with via_plugin_id recording that a plugin produced them — permission-wise
// the write is the user's, audit-wise it stays traceable to the plugin.

// An event hook has nobody behind it. Attributing its writes to the last member
// who happened to touch the issue would be a lie the audit trail cannot undo.

// A grant is good for the whole invocation, not one request. A handler that
// reads an issue and then comments on it makes two calls, and the single-use
// version of this refused the second — the failure a live run surfaced.

// storage:user is per-member state. A caller with no member has no such scope
// to resolve, and falling through would key every plugin-actor write to the
// zero UUID — one shared bucket masquerading as somebody's private one.

// /context has no scope requirement, but it must not invent a user for a caller
// that has none — a handler reading `user` would believe it is acting for
// somebody.

// latestComment reads the row directly. The response body reports what the
// handler decided to return; the row is what was actually written, and
// attribution is only meaningful as stored.
func latestComment(t *testing.T, issueID string) db.Comment {
	t.Helper()
	var comment db.Comment
	err := testPool.QueryRow(context.Background(),
		`SELECT id, author_type, author_id, via_plugin_id FROM comment WHERE issue_id = $1 ORDER BY created_at DESC LIMIT 1`,
		issueID,
	).Scan(&comment.ID, &comment.AuthorType, &comment.AuthorID, &comment.ViaPluginID)
	if err != nil {
		t.Fatalf("read the written comment: %v", err)
	}
	return comment
}

// The narrowing that was only a comment until Fabel 5's review.
//
// A hook called about one issue has no business reaching another. Without this
// check the grant was worth every issue in the workspace its actor could see,
// for the whole five minutes it lived — and both the type's doc comment and the
// PR description claimed otherwise.

// An invocation with no issue produces an unscoped grant. That is not a hole —
// it simply means there was no issue to narrow to — and the workspace and
// membership checks still apply.
