package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	publicapiv1 "github.com/multica-ai/multica/server/pkg/publicapi/v1"
)

// These are the Action API's security properties, not its happy path. Each one
// is a way a plugin could exceed what the person using it can do, and none of
// them is visible by reading the handler — they only hold if the three checks
// run in the right order against the right inputs.

// installPluginForAction installs the reference plugin and returns its id,
// leaving the caller free to vary scopes.
func installPluginForAction(t *testing.T, scopes []string) string {
	t.Helper()
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)

	manifest := handlerTestManifest
	if scopes != nil {
		encoded, err := json.Marshal(scopes)
		if err != nil {
			t.Fatalf("encode scopes: %v", err)
		}
		manifest = strings.Replace(manifest,
			`"scopes": ["issues:read", "comments:write", "storage:user"]`,
			`"scopes": `+string(encoded), 1)
	}
	versionID := withLocalPluginSource(t, manifest)

	body, _ := json.Marshal(map[string]any{"version_id": versionID, "granted_scopes": scopes})
	recorder := httptest.NewRecorder()
	testHandler.InstallPlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins", body, map[string]string{"id": testWorkspaceID}))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("install status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var installed struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &installed); err != nil {
		t.Fatalf("decode installation: %v", err)
	}
	return installed.ID
}

func pluginActionRequest(method, path, installationID string, body any, params map[string]string) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	request := httptest.NewRequest(method, path, &buf)
	request.Header.Set("X-User-ID", testUserID)
	request.Header.Set("Content-Type", "application/json")
	if installationID != "" {
		request.Header.Set(pluginInstallationHeader, installationID)
	}
	routeContext := chi.NewRouteContext()
	for key, value := range params {
		routeContext.URLParams.Add(key, value)
	}
	return request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
}

func pluginInstallTokenRequest(method, path, token string, body any, params map[string]string) *http.Request {
	request := pluginActionRequest(method, path, "", body, params)
	request.Header.Del("X-User-ID")
	request.Header.Set("Authorization", "Bearer "+token)
	return request
}

func TestPluginActionRequiresTheFeatureFlag(t *testing.T) {
	withPluginsV1Flag(t, testHandler, false)
	recorder := httptest.NewRecorder()

	testHandler.GetPluginContext(recorder, pluginInstallTokenRequest(http.MethodGet, "/v1/context", "mpi_invalid", nil, nil))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s, want 403", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Retry-After"); got != "" {
		t.Fatalf("Retry-After=%q, want empty for a non-retryable feature gate", got)
	}
	var problem publicapiv1.Problem
	if err := json.Unmarshal(recorder.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode feature flag problem: %v", err)
	}
	if problem.Status != http.StatusForbidden || problem.Code != "plugin_api_disabled" || problem.Title != "Forbidden" {
		t.Fatalf("unexpected feature flag problem: %+v", problem)
	}
}










// createIssueInForeignWorkspace seeds a workspace the test user is NOT a member
// of, so a cross-tenant read has something real to fail against rather than a
// made-up uuid that would 404 for the wrong reason.
func createIssueInForeignWorkspace(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	var ownerID string
	if err := testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Foreign Owner', $1) RETURNING id`,
		"foreign-plugin-"+testUserID+"@multica.test").Scan(&ownerID); err != nil {
		t.Fatalf("seed foreign user: %v", err)
	}
	var workspaceID string
	if err := testPool.QueryRow(ctx, `INSERT INTO workspace (name, slug) VALUES ('Foreign', $1) RETURNING id`,
		"foreign-plugin-"+testUserID).Scan(&workspaceID); err != nil {
		t.Fatalf("seed foreign workspace: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`,
		workspaceID, ownerID); err != nil {
		t.Fatalf("seed foreign member: %v", err)
	}
	var issueID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id)
		 VALUES ($1, 'Foreign issue', 'todo', 'none', 'member', $2) RETURNING id`,
		workspaceID, ownerID).Scan(&issueID); err != nil {
		t.Fatalf("seed foreign issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id = $1`, workspaceID)
		testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1`, workspaceID)
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, ownerID)
	})
	return issueID
}
