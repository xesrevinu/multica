package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"


	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

// hookHandlerTestManifest declares the three host-driven triggers. Its endpoint
// is never reached in these tests — they cover the checks BEFORE the request
// leaves, which is where a wrong answer is a security bug rather than a broken
// integration. The wire format and signature are covered end-to-end against a
// live server in internal/service.
const hookHandlerTestManifest = `{
  "manifest_version": 1,
  "key": "com.example.hooked",
  "name": "Hooked",
  "version": "1.0.0",
  "author": { "name": "example" },
  "scopes": ["issues:read", "comments:write", "net:example.com"],
  "contributes": {
    "hooks": [{
      "key": "summarize",
      "name": "Summarize",
      "description": "Compress the discussion.",
      "triggers": ["ui", "manual", "event"],
      "events": ["issue.created"],
      "transport": { "type": "http", "url": "https://example.com/hooks/summarize" }
    }, {
      "key": "manual_only",
      "name": "Manual only",
      "description": "Only ever picked from a menu.",
      "triggers": ["manual"],
      "transport": { "type": "http", "url": "https://example.com/hooks/manual" }
    }]
  }
}`

func installHookPlugin(t *testing.T) string {
	t.Helper()
	versionID := withLocalPluginSource(t, hookHandlerTestManifest)
	body, _ := json.Marshal(map[string]any{
		"version_id":     versionID,
		"granted_scopes": []string{"issues:read", "comments:write", "net:example.com"},
	})
	recorder := httptest.NewRecorder()
	testHandler.InstallPlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins", body, map[string]string{"id": testWorkspaceID}))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("install hook plugin: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var installed struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &installed); err != nil {
		t.Fatalf("decode installation: %v", err)
	}
	return installed.ID
}

func invokeHookRequest(installationID, hookKey string, payload map[string]any) *http.Request {
	body, _ := json.Marshal(payload)
	request := httptest.NewRequest(http.MethodPost, "/api/plugin-bridge/v1/hooks/"+hookKey, bytes.NewReader(body))
	request.Header.Set("X-User-ID", testUserID)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(pluginInstallationHeader, installationID)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("key", hookKey)
	return request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
}

// `event` is dispatched by the host, off the event bus, precisely so that its
// writes carry the plugin's identity rather than a person's. A browser asking
// for one would be electing to run as somebody it is not.

// A hook may only be reached through a trigger its own manifest declared, even
// when the host supports that trigger in general.


// The flag gates the hook endpoint like every other plugin route: fail closed,
// not merely hidden from the UI.
func TestInvokePluginHookRequiresTheFeatureFlag(t *testing.T) {
	withPluginsV1Flag(t, testHandler, false)
	recorder := httptest.NewRecorder()
	testHandler.InvokePluginHook(recorder, invokeHookRequest("11111111-1111-1111-1111-111111111111", "summarize", map[string]any{"trigger": "manual"}))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s, want 403", recorder.Code, recorder.Body.String())
	}
}

// A disabled installation is off, not hidden. A stale tab must not keep
// invoking hooks after an admin switches the plugin off.

// The install token is returned once and stored only as a hash, so the same
// token must never come back from a second rotation, and the old one must stop
// working the moment a new one is issued.




// A garbage token must not resolve to some installation by accident — the
// lookup is by hash, so an empty or malformed value must be refused before it
// reaches the query.
func TestAuthenticateInstallTokenRefusesMalformedValues(t *testing.T) {
	for _, token := range []string{"", "mpi_", "not-a-token", "mpc_something", "Bearer mpi_x"} {
		if _, err := testHandler.PluginService.AuthenticateInstallToken(context.Background(), token); err == nil {
			t.Fatalf("token %q must not authenticate", token)
		}
	}
}

func rotateToken(t *testing.T, installationID string) pluginTokenResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	testHandler.RotatePluginToken(recorder, pluginHandlerRequest(http.MethodPost, "/token", nil, map[string]string{
		"id": testWorkspaceID, "installationId": installationID,
	}))
	if recorder.Code != http.StatusOK {
		t.Fatalf("rotate: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var issued pluginTokenResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &issued); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	return issued
}

// The feature flag gates the EVENT path, observed where it matters: does a
// request actually leave for the plugin's endpoint.
//
// Every other plugin surface is refused by a handler that can read the flag off
// a request. An event hook has none — the dispatcher is subscribed at startup
// and runs on a worker — so without a check inside it, turning plugins off
// would hide the UI and refuse the Action API while the outbound calls kept
// going. That is fail-open on the one path that reaches a third party.
//
// Needs a database because the dispatcher only calls anything when a real,
// enabled installation declares an event hook.

// flagLookupSignal reports each evaluation of one flag, so a test can tell a
// worker has reached its flag check instead of waiting out a window for a call
// that should never come.
type flagLookupSignal struct {
	featureflag.Provider
	key    string
	looked chan<- struct{}
}

func (p flagLookupSignal) Lookup(ctx context.Context, key string) (featureflag.Decision, bool) {
	decision, found := p.Provider.Lookup(ctx, key)
	if key == p.key {
		select {
		case p.looked <- struct{}{}:
		default:
		}
	}
	return decision, found
}
