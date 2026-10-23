package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/issueproperty"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func makePropertyDef(propType string, options []PropertyOption) db.IssueProperty {
	cfg, _ := json.Marshal(PropertyConfig{Options: options})
	return db.IssueProperty{Type: propType, Config: string(cfg)}
}

// withIssuePropertyParams sets both chi URL params in one route context —
// withURLParam builds a fresh context per call, so chaining it would drop
// the first param.
func withIssuePropertyParams(req *http.Request, issueID, propertyID string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", issueID)
	rctx.URLParams.Add("propertyId", propertyID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func createTestProperty(t *testing.T, body map[string]any) PropertyResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/properties", body)
	testHandler.CreateProperty(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProperty: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created PropertyResponse
	json.NewDecoder(w.Body).Decode(&created)
	t.Cleanup(func() { deleteTestProperty(t, created.ID) })
	return created
}

// deleteTestProperty removes the row directly — the API only archives, but
// tests must not leak definitions into the shared workspace fixture (the
// 20-active cap and list assertions would couple unrelated tests).
func deleteTestProperty(t *testing.T, id string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `DELETE FROM issue_property WHERE id = $1`, id); err != nil {
		t.Fatalf("cleanup property %s: %v", id, err)
	}
}

func createPropertyTestIssue(t *testing.T, title string) string {
	t.Helper()
	var issueID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number)
		VALUES ($1, $2, 'todo', 'none', 'member', $3,
		        COALESCE((SELECT MAX(number) FROM issue WHERE workspace_id = $1), 0) + 1)
		RETURNING id
	`, testWorkspaceID, title, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create test issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})
	return issueID
}

func setIssuePropertyRaw(t *testing.T, issueID, propertyID string, value any) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+issueID+"/properties/"+propertyID, map[string]any{"value": value})
	req = withIssuePropertyParams(req, issueID, propertyID)
	testHandler.SetIssueProperty(w, req)
	return w
}

func TestPropertyDefinitionCRUD(t *testing.T) {
	created := createTestProperty(t, map[string]any{
		"name":        "Severity",
		"type":        "select",
		"description": "How bad it is",
		"icon":        "flag",
		"config": map[string]any{"options": []map[string]any{
			{"name": "Critical", "color": "EF4444"},
			{"name": "Minor", "color": "#6b7280"},
		}},
	})
	if created.Type != "select" || created.Icon != "flag" || len(created.Config.Options) != 2 {
		t.Fatalf("unexpected created property: %+v", created)
	}
	// Server assigns option ids and normalizes colors to lowercase #rrggbb.
	if created.Config.Options[0].ID == "" {
		t.Fatalf("option id not assigned: %+v", created.Config.Options[0])
	}
	if created.Config.Options[0].Color != "#ef4444" {
		t.Fatalf("color not normalized: %q", created.Config.Options[0].Color)
	}

	// Duplicate name (case-insensitive) → 409.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/properties", map[string]any{
		"name": "severity", "type": "text",
	})
	testHandler.CreateProperty(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate name: expected 409, got %d: %s", w.Code, w.Body.String())
	}

	// Rename + replace options, keeping the first option's id: values that
	// reference it must survive option-list edits.
	keepID := created.Config.Options[0].ID
	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/properties/"+created.ID, map[string]any{
		"name": "Sev",
		"icon": "shield",
		"config": map[string]any{"options": []map[string]any{
			{"id": keepID, "name": "Blocker", "color": "#ef4444"},
			{"name": "Trivial", "color": "#a1a1aa"},
		}},
	})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateProperty(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateProperty: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated PropertyResponse
	json.NewDecoder(w.Body).Decode(&updated)
	if updated.Name != "Sev" || updated.Icon != "shield" || updated.Config.Options[0].ID != keepID || updated.Config.Options[0].Name != "Blocker" {
		t.Fatalf("option id not preserved on update: %+v", updated.Config.Options)
	}

	// Empty string clears the optional icon without changing the definition.
	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/properties/"+created.ID, map[string]any{"icon": ""})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateProperty(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("clear icon: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	json.NewDecoder(w.Body).Decode(&updated)
	if updated.Icon != "" {
		t.Fatalf("icon not cleared: %q", updated.Icon)
	}

	// Archive → default list hides it, include_archived shows it.
	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/properties/"+created.ID, map[string]any{"archived": true})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateProperty(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("archive: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	listProperties := func(query string) []PropertyResponse {
		w := httptest.NewRecorder()
		testHandler.ListProperties(w, newRequest("GET", "/api/properties"+query, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("ListProperties%s: expected 200, got %d: %s", query, w.Code, w.Body.String())
		}
		var resp struct {
			Properties []PropertyResponse `json:"properties"`
		}
		json.NewDecoder(w.Body).Decode(&resp)
		return resp.Properties
	}
	contains := func(list []PropertyResponse, id string) bool {
		for _, p := range list {
			if p.ID == id {
				return true
			}
		}
		return false
	}
	if contains(listProperties(""), created.ID) {
		t.Fatalf("archived property still in default list")
	}
	if !contains(listProperties("?include_archived=true"), created.ID) {
		t.Fatalf("archived property missing from include_archived list")
	}
}

func TestPropertyDefinitionValidation(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
		want string
	}{
		{"reserved name", map[string]any{"name": "Due Date", "type": "text"}, "reserved"},
		{"invalid type", map[string]any{"name": "X" + uuid.NewString()[:8], "type": "formula"}, "invalid type"},
		{"icon too long", map[string]any{"name": "X" + uuid.NewString()[:8], "type": "text", "icon": strings.Repeat("x", maxPropertyIconLen+1)}, "icon must be"},
		{"icon control character", map[string]any{"name": "X" + uuid.NewString()[:8], "type": "text", "icon": "\t"}, "icon cannot contain"},
		{"emoji icon", map[string]any{"name": "X" + uuid.NewString()[:8], "type": "text", "icon": "🚨"}, "supported icon key"},
		{"unknown icon", map[string]any{"name": "X" + uuid.NewString()[:8], "type": "text", "icon": "not-an-icon"}, "supported icon key"},
		{"options on text", map[string]any{"name": "X" + uuid.NewString()[:8], "type": "text",
			"config": map[string]any{"options": []map[string]any{{"name": "a", "color": "#000000"}}}}, "does not accept options"},
		{"select without options", map[string]any{"name": "X" + uuid.NewString()[:8], "type": "select"}, "at least one option"},
		{"duplicate option names", map[string]any{"name": "X" + uuid.NewString()[:8], "type": "select",
			"config": map[string]any{"options": []map[string]any{
				{"name": "One", "color": "#000000"}, {"name": "one", "color": "#111111"},
			}}}, "duplicate option name"},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		testHandler.CreateProperty(w, newRequest("POST", "/api/properties", tc.body))
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), tc.want) {
			t.Fatalf("%s: expected 400 containing %q, got %d: %s", tc.name, tc.want, w.Code, w.Body.String())
		}
	}
}

// TestPropertyAdminGate verifies the two definition-management gates: agent
// actors are rejected outright (even though the fixture user is the workspace
// owner), while value writes from the same agent context succeed.
func TestPropertyAdminGate(t *testing.T) {
	// Agent actor (task_token path is trusted directly by resolveActor).
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/properties", map[string]any{"name": "AgentMade", "type": "text"})
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", uuid.NewString())
	testHandler.CreateProperty(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("agent CreateProperty: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	property := createTestProperty(t, map[string]any{"name": "AgentWritable" + uuid.NewString()[:8], "type": "text"})
	issueID := createPropertyTestIssue(t, "agent value write")

	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/issues/"+issueID+"/properties/"+property.ID, map[string]any{"value": "set by agent"})
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", uuid.NewString())
	req = withIssuePropertyParams(req, issueID, property.ID)
	testHandler.SetIssueProperty(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("agent SetIssueProperty: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestIssuePropertyValues(t *testing.T) {
	sel := createTestProperty(t, map[string]any{
		"name": "Env" + uuid.NewString()[:8], "type": "select",
		"config": map[string]any{"options": []map[string]any{
			{"name": "Staging", "color": "#22c55e"},
			{"name": "Production", "color": "#ef4444"},
		}},
	})
	multi := createTestProperty(t, map[string]any{
		"name": "Platforms" + uuid.NewString()[:8], "type": "multi_select",
		"config": map[string]any{"options": []map[string]any{
			{"name": "iOS", "color": "#3b82f6"},
			{"name": "Android", "color": "#22c55e"},
			{"name": "Web", "color": "#f59e0b"},
		}},
	})
	date := createTestProperty(t, map[string]any{"name": "Reviewed" + uuid.NewString()[:8], "type": "date"})
	link := createTestProperty(t, map[string]any{"name": "Spec" + uuid.NewString()[:8], "type": "url"})
	num := createTestProperty(t, map[string]any{"name": "Effort" + uuid.NewString()[:8], "type": "number"})

	issueID := createPropertyTestIssue(t, "property value matrix")

	// select: valid option id.
	if w := setIssuePropertyRaw(t, issueID, sel.ID, sel.Config.Options[0].ID); w.Code != http.StatusOK {
		t.Fatalf("select set: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	// select: unknown option → 400 listing legal ids (agents self-correct on this).
	if w := setIssuePropertyRaw(t, issueID, sel.ID, "nope"); w.Code != http.StatusBadRequest ||
		!strings.Contains(w.Body.String(), sel.Config.Options[0].ID) {
		t.Fatalf("select invalid: expected 400 listing option ids, got %d: %s", w.Code, w.Body.String())
	}

	// multi_select: duplicates dropped, order canonicalized to config order.
	webID, iosID := multi.Config.Options[2].ID, multi.Config.Options[0].ID
	w := setIssuePropertyRaw(t, issueID, multi.ID, []string{webID, iosID, webID})
	if w.Code != http.StatusOK {
		t.Fatalf("multi_select set: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Properties map[string]any `json:"properties"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	stored, _ := resp.Properties[multi.ID].([]any)
	if len(stored) != 2 || stored[0] != iosID || stored[1] != webID {
		t.Fatalf("multi_select not canonicalized to config order: %v", stored)
	}

	// date / url / number validation.
	if w := setIssuePropertyRaw(t, issueID, date.ID, "13/07/2026"); w.Code != http.StatusBadRequest {
		t.Fatalf("bad date: expected 400, got %d", w.Code)
	}
	if w := setIssuePropertyRaw(t, issueID, date.ID, "2026-07-13"); w.Code != http.StatusOK {
		t.Fatalf("good date: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w := setIssuePropertyRaw(t, issueID, link.ID, "javascript:alert(1)"); w.Code != http.StatusBadRequest {
		t.Fatalf("bad url: expected 400, got %d", w.Code)
	}
	if w := setIssuePropertyRaw(t, issueID, link.ID, "https://example.com/spec"); w.Code != http.StatusOK {
		t.Fatalf("good url: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w := setIssuePropertyRaw(t, issueID, num.ID, "3"); w.Code != http.StatusBadRequest {
		t.Fatalf("string into number: expected 400, got %d", w.Code)
	}
	if w := setIssuePropertyRaw(t, issueID, num.ID, 3.5); w.Code != http.StatusOK {
		t.Fatalf("good number: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// multi_text: duplicates dropped, caller order kept.
	listText := createTestProperty(t, map[string]any{"name": "Aliases" + uuid.NewString()[:8], "type": "multi_text"})
	wl := setIssuePropertyRaw(t, issueID, listText.ID, []string{"beta", "alpha", "beta"})
	if wl.Code != http.StatusOK {
		t.Fatalf("multi_text set: expected 200, got %d: %s", wl.Code, wl.Body.String())
	}
	var listResp struct {
		Properties map[string]any `json:"properties"`
	}
	json.NewDecoder(wl.Body).Decode(&listResp)
	storedList, _ := listResp.Properties[listText.ID].([]any)
	if len(storedList) != 2 || storedList[0] != "beta" || storedList[1] != "alpha" {
		t.Fatalf("multi_text not stored in caller order: %v", storedList)
	}

	// multi_url: entry-level http(s) validation.
	listURL := createTestProperty(t, map[string]any{"name": "Links" + uuid.NewString()[:8], "type": "multi_url"})
	if w := setIssuePropertyRaw(t, issueID, listURL.ID, []string{"https://example.com/a", "https://example.com/b"}); w.Code != http.StatusOK {
		t.Fatalf("good multi_url: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w := setIssuePropertyRaw(t, issueID, listURL.ID, []string{"https://example.com/a", "ftp://example.com"}); w.Code != http.StatusBadRequest {
		t.Fatalf("non-http(s) multi_url entry: expected 400, got %d", w.Code)
	}

	// Archived definitions reject new values but allow unset.
	warch := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/properties/"+sel.ID, map[string]any{"archived": true})
	req = withURLParam(req, "id", sel.ID)
	testHandler.UpdateProperty(warch, req)
	if warch.Code != http.StatusOK {
		t.Fatalf("archive: expected 200, got %d: %s", warch.Code, warch.Body.String())
	}
	if w := setIssuePropertyRaw(t, issueID, sel.ID, sel.Config.Options[1].ID); w.Code != http.StatusBadRequest {
		t.Fatalf("set on archived: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	wdel := httptest.NewRecorder()
	req = newRequest("DELETE", "/api/issues/"+issueID+"/properties/"+sel.ID, nil)
	req = withIssuePropertyParams(req, issueID, sel.ID)
	testHandler.DeleteIssueProperty(wdel, req)
	if wdel.Code != http.StatusOK {
		t.Fatalf("unset on archived: expected 200, got %d: %s", wdel.Code, wdel.Body.String())
	}
	// Fresh struct: json.Decode merges into a pre-populated map, which would
	// leave the earlier bag contents (including sel.ID) in place.
	var afterDelete struct {
		Properties map[string]any `json:"properties"`
	}
	json.NewDecoder(wdel.Body).Decode(&afterDelete)
	if _, present := afterDelete.Properties[sel.ID]; present {
		t.Fatalf("value not removed: %v", afterDelete.Properties)
	}
}

func TestValidatePropertyValueUnit(t *testing.T) {
	textDef := makePropertyDef("text", nil)
	if _, err := validatePropertyValue(textDef, json.RawMessage(`"  "`)); err == nil {
		t.Fatalf("blank text accepted")
	}
	if _, err := validatePropertyValue(textDef, json.RawMessage(`"`+strings.Repeat("x", 2001)+`"`)); err == nil {
		t.Fatalf("overlong text accepted")
	}
	if _, err := validatePropertyValue(textDef, json.RawMessage(`null`)); err == nil {
		t.Fatalf("null accepted")
	}
	boolDef := makePropertyDef("checkbox", nil)
	if _, err := validatePropertyValue(boolDef, json.RawMessage(`"true"`)); err == nil {
		t.Fatalf("string into checkbox accepted")
	}
	if _, err := validatePropertyValue(boolDef, json.RawMessage(`false`)); err != nil {
		t.Fatalf("false rejected: %v", err)
	}
}

func TestValidatePropertyListValuesUnit(t *testing.T) {
	validate := func(def db.IssueProperty, raw string) ([]byte, error) {
		return issueproperty.ValidateValue(def, json.RawMessage(raw))
	}
	textList := makePropertyDef("multi_text", nil)
	if _, err := validate(textList, `[]`); err == nil {
		t.Fatalf("empty multi_text array accepted")
	}
	if _, err := validate(textList, `"alpha"`); err == nil {
		t.Fatalf("bare string into multi_text accepted")
	}
	if _, err := validate(textList, `["alpha", 3]`); err == nil {
		t.Fatalf("non-string multi_text entry accepted")
	}
	if _, err := validate(textList, `["alpha", "  "]`); err == nil {
		t.Fatalf("blank multi_text entry accepted")
	}
	stored, err := validate(textList, `["alpha", "beta", "alpha"]`)
	if err != nil {
		t.Fatalf("valid multi_text rejected: %v", err)
	}
	if string(stored) != `["alpha","beta"]` {
		t.Fatalf("multi_text not deduped in caller order: %s", stored)
	}
	over := make([]string, issueproperty.MaxListValues+1)
	for i := range over {
		over[i] = fmt.Sprintf("v%d", i)
	}
	overRaw, _ := json.Marshal(over)
	if _, err := validate(textList, string(overRaw)); err == nil {
		t.Fatalf("over-cap multi_text accepted")
	}

	urlList := makePropertyDef("multi_url", nil)
	if _, err := validate(urlList, `["https://a.example", "javascript:alert(1)"]`); err == nil {
		t.Fatalf("non-http(s) multi_url entry accepted")
	}
	stored, err = validate(urlList, `["  https://a.example/x ", "https://b.example", "https://a.example/x"]`)
	if err != nil {
		t.Fatalf("valid multi_url rejected: %v", err)
	}
	if string(stored) != `["https://a.example/x","https://b.example"]` {
		t.Fatalf("multi_url not trimmed and deduped: %s", stored)
	}
}

func TestValidatePropertyNameReserved(t *testing.T) {
	for _, name := range []string{"status", "Priority", "due date", "Due_Date", "START DATE", "labels"} {
		if _, err := validatePropertyName(name); err == nil {
			t.Fatalf("reserved name %q accepted", name)
		}
	}
	if _, err := validatePropertyName("Severity"); err != nil {
		t.Fatalf("legit name rejected: %v", err)
	}
}

// TestPropertyOptionRemovalGuard: removing a select option still referenced
// by issues is rejected with a usage census; renames (same id) and removal
// of unused options pass.
func TestPropertyOptionRemovalGuard(t *testing.T) {
	property := createTestProperty(t, map[string]any{
		"name": "Guard" + uuid.NewString()[:8], "type": "select",
		"config": map[string]any{"options": []map[string]any{
			{"name": "Used", "color": "#ef4444"},
			{"name": "Unused", "color": "#6b7280"},
		}},
	})
	usedID := property.Config.Options[0].ID
	unusedID := property.Config.Options[1].ID
	issueID := createPropertyTestIssue(t, "option removal guard")
	if w := setIssuePropertyRaw(t, issueID, property.ID, usedID); w.Code != http.StatusOK {
		t.Fatalf("seed value: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	patchConfig := func(options []map[string]any) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := newRequest("PATCH", "/api/properties/"+property.ID, map[string]any{
			"config": map[string]any{"options": options},
		})
		req = withURLParam(req, "id", property.ID)
		testHandler.UpdateProperty(w, req)
		return w
	}

	// Dropping the in-use option → 409 naming it with the census.
	w := patchConfig([]map[string]any{{"id": unusedID, "name": "Unused", "color": "#6b7280"}})
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "Used") || !strings.Contains(w.Body.String(), "1 issues") {
		t.Fatalf("in-use removal: expected 409 with census, got %d: %s", w.Code, w.Body.String())
	}

	// Renaming the in-use option (id preserved) → 200.
	w = patchConfig([]map[string]any{
		{"id": usedID, "name": "Used (renamed)", "color": "#ef4444"},
		{"id": unusedID, "name": "Unused", "color": "#6b7280"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("rename: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Dropping only the unused option → 200.
	w = patchConfig([]map[string]any{{"id": usedID, "name": "Used (renamed)", "color": "#ef4444"}})
	if w.Code != http.StatusOK {
		t.Fatalf("unused removal: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestListIssuesPropertyFilterAndSort covers the server-side list support:
// containment filtering (select / multi_select / checkbox) beyond the first
// page window, and typed property sort expressions with missing-last
// semantics. The shared workspace fixture may hold foreign issues, so
// assertions check relative order / membership rather than exact lists.

// TestListIssuesSelectPropertySortOptionOrder pins the ordinal semantic of a
// select property sort: issues order by the OPTION ORDER of the definition
// (Low < Medium < High), not by the stored option-id string. The explicit
// option ids are chosen so their lexical order is the exact reverse of the
// option order — sorting by the raw stored value (the pre-fix behavior) puts
// High first and fails deterministically. The first id is deliberately
// uppercase: config validation stores an explicit id's original spelling, so
// the CASE arms must embed that spelling, not a re-serialized canonical form.
func TestListIssuesSelectPropertySortOptionOrder(t *testing.T) {
	const (
		lowID    = "EEEEEEEE-EEEE-4EEE-8EEE-EEEEEEEEEEEE" // lexically last
		mediumID = "99999999-9999-4999-8999-999999999999"
		highID   = "11111111-1111-4111-8111-111111111111" // lexically first
	)
	sel := createTestProperty(t, map[string]any{
		"name": "SO" + uuid.NewString()[:8], "type": "select",
		"config": map[string]any{"options": []map[string]any{
			{"id": lowID, "name": "Low", "color": "#6b7280"},
			{"id": mediumID, "name": "Medium", "color": "#f59e0b"},
			{"id": highID, "name": "High", "color": "#ef4444"},
		}},
	})

	low := dbfx.Issue(t, "select sort low")
	medium := dbfx.Issue(t, "select sort medium")
	high := dbfx.Issue(t, "select sort high")
	unset := dbfx.Issue(t, "select sort unset")
	for issueID, optionID := range map[string]string{low: lowID, medium: mediumID, high: highID} {
		if w := setIssuePropertyRaw(t, issueID, sel.ID, optionID); w.Code != http.StatusOK {
			t.Fatalf("seed select value: %d %s", w.Code, w.Body.String())
		}
	}

	listIssues := func(query string) map[string]int {
		t.Helper()
		var resp struct {
			Issues []IssueResponse `json:"issues"`
		}
		testutil.Call(t, testHandler.ListIssues, newRequest("GET", "/api/issues"+query, nil)).
			Want(http.StatusOK).JSON(&resp)
		out := make(map[string]int, len(resp.Issues))
		for i, issue := range resp.Issues {
			out[issue.ID] = i
		}
		return out
	}

	pos := listIssues("?limit=200&sort=property:" + sel.ID + "&direction=asc")
	for _, id := range []string{low, medium, high, unset} {
		if _, ok := pos[id]; !ok {
			t.Fatalf("asc sorted list missing seeded issue %s", id)
		}
	}
	if !(pos[low] < pos[medium] && pos[medium] < pos[high] && pos[high] < pos[unset]) {
		t.Fatalf("asc select sort not in option order: low=%d medium=%d high=%d unset=%d",
			pos[low], pos[medium], pos[high], pos[unset])
	}

	pos = listIssues("?limit=200&sort=property:" + sel.ID + "&direction=desc")
	if !(pos[high] < pos[medium] && pos[medium] < pos[low] && pos[low] < pos[unset]) {
		t.Fatalf("desc select sort not in reverse option order: high=%d medium=%d low=%d unset=%d",
			pos[high], pos[medium], pos[low], pos[unset])
	}
}

func TestParsePropertiesFilterNoValueUnit(t *testing.T) {
	defID := uuid.NewString()
	w := httptest.NewRecorder()
	var args []any
	addArg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	// "__none__" compiles to the marker object, and the predicate turns that
	// marker into a key-absence check rather than a containment pattern.
	groups, ok := parsePropertiesFilterParam(w, fmt.Sprintf(`{"%s":["__none__"]}`, defID))
	if !ok {
		t.Fatalf("no-value parse failed: %s", w.Body.String())
	}
	if len(groups) != 1 || len(groups[0]) != 1 {
		t.Fatalf("expected one group with one alternative, got %d / %d", len(groups), len(groups[0]))
	}
	if parsed, isMarker := parseNoPropertyValuePattern(groups[0][0]); !isMarker || parsed != defID {
		t.Fatalf("expected no-value marker for %s, got %q isMarker=%v", defID, parsed, isMarker)
	}
	sql := propertiesFilterPredicate(groups, addArg)
	if !strings.Contains(sql, "NOT (i.properties ? $1)") {
		t.Fatalf("no-value predicate wrong: %s", sql)
	}

	// A normal containment alternative is never mistaken for the marker.
	groups, ok = parsePropertiesFilterParam(w, fmt.Sprintf(`{"%s":["true"]}`, defID))
	if !ok {
		t.Fatalf("containment parse failed: %s", w.Body.String())
	}
	if _, isMarker := parseNoPropertyValuePattern(groups[0][0]); isMarker {
		t.Fatalf("checkbox true alternative misdetected as the marker")
	}

	// Mixed true + no value: containment OR key-absence.
	groups, ok = parsePropertiesFilterParam(w, fmt.Sprintf(`{"%s":["true","__none__"]}`, defID))
	if !ok {
		t.Fatalf("mixed parse failed: %s", w.Body.String())
	}
	args = nil
	sql = propertiesFilterPredicate(groups, addArg)
	if !strings.Contains(sql, "@> $1") || !strings.Contains(sql, "NOT (i.properties ? ") {
		t.Fatalf("mixed predicate wrong: %s", sql)
	}

	// Duplicate sentinels collapse to a single marker.
	groups, ok = parsePropertiesFilterParam(w, fmt.Sprintf(`{"%s":["__none__","__none__"]}`, defID))
	if !ok {
		t.Fatalf("duplicate parse failed: %s", w.Body.String())
	}
	if len(groups[0]) != 1 {
		t.Fatalf("duplicate sentinel produced %d alternatives, want 1", len(groups[0]))
	}

	// A numeric filter value also emits the stored jsonb number form, so a
	// number property matches the scalar instead of only the "3.5" string.
	groups, ok = parsePropertiesFilterParam(w, fmt.Sprintf(`{"%s":["3.5"]}`, defID))
	if !ok {
		t.Fatalf("numeric parse failed: %s", w.Body.String())
	}
	hasNumber := false
	for _, alt := range groups[0] {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(alt, &m); err != nil {
			continue
		}
		var num float64
		if err := json.Unmarshal(m[defID], &num); err == nil && num == 3.5 {
			hasNumber = true
		}
	}
	if !hasNumber {
		t.Fatalf("numeric filter value did not emit a jsonb number containment form: %v", groups[0])
	}
	// A date value must NOT be misread as a number.
	groups, ok = parsePropertiesFilterParam(w, fmt.Sprintf(`{"%s":["2026-08-19"]}`, defID))
	if !ok {
		t.Fatalf("date parse failed: %s", w.Body.String())
	}
	for _, alt := range groups[0] {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(alt, &m); err != nil {
			continue
		}
		var num float64
		if err := json.Unmarshal(m[defID], &num); err == nil {
			t.Fatalf("date value misread as a number: %v", alt)
		}
	}

	// NaN / Infinity parse as floats but are not valid JSON — they must be
	// skipped, not marshaled into a 400.
	for _, bad := range []string{"NaN", "Infinity", "-Infinity"} {
		groups, ok = parsePropertiesFilterParam(w, fmt.Sprintf(`{"%s":[%q]}`, defID, bad))
		if !ok {
			t.Fatalf("non-finite parse of %q failed: %s", bad, w.Body.String())
		}
		for _, alt := range groups[0] {
			var m map[string]json.RawMessage
			if err := json.Unmarshal(alt, &m); err != nil {
				continue
			}
			var num float64
			if err := json.Unmarshal(m[defID], &num); err == nil && (math.IsNaN(num) || math.IsInf(num, 0)) {
				t.Fatalf("non-finite value %q leaked a number containment form: %v", bad, alt)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Actor property types (MUL-6286)
// ---------------------------------------------------------------------------

// decodePropertiesBag reads the `{"properties": {...}}` envelope the value
// endpoints return. A fresh struct per call matters: json.Decode merges into a
// pre-populated map, so reusing one would keep keys from an earlier response.
func decodePropertiesBag(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var resp struct {
		Properties map[string]any `json:"properties"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode properties bag: %v", err)
	}
	return resp.Properties
}

func TestParseActorRefUnit(t *testing.T) {
	memberID := uuid.NewString()
	ref, err := parseActorRef("member:" + memberID)
	if err != nil {
		t.Fatalf("member reference rejected: %v", err)
	}
	if ref.Kind != "member" || ref.ID != memberID {
		t.Fatalf("unexpected parse: %+v", ref)
	}
	// String() is the canonical storage form, so it must round-trip exactly.
	if ref.String() != "member:"+memberID {
		t.Fatalf("String() round-trip broken: %q", ref.String())
	}

	// uuid.Parse also accepts uppercase, braces and the urn: form. Every
	// consumer compares reference strings exactly, so anything not stored in
	// canonical form would render as Unknown and never match a filter.
	canonical := "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
	for _, variant := range []string{
		strings.ToUpper(canonical),
		"{" + canonical + "}",
		"urn:uuid:" + canonical,
		strings.ReplaceAll(canonical, "-", ""),
	} {
		got, err := parseActorRef("member:" + variant)
		if err != nil {
			t.Fatalf("uuid variant %q rejected: %v", variant, err)
		}
		if got.String() != "member:"+canonical {
			t.Fatalf("uuid variant %q not canonicalized: %q", variant, got.String())
		}
	}

	cases := []struct {
		name  string
		value string
		want  string
	}{
		{"no colon", uuid.NewString(), `"<kind>:<uuid>"`},
		{"empty string", "", `"<kind>:<uuid>"`},
		// "agent" and "squad" are assignee kinds deliberately left out of the
		// V1 value range; both must read as unknown, not silently accepted.
		{"agent kind", "agent:" + uuid.NewString(), "unknown actor kind"},
		{"squad kind", "squad:" + uuid.NewString(), "unknown actor kind"},
		{"user kind", "user:" + uuid.NewString(), "unknown actor kind"},
		{"empty kind", ":" + uuid.NewString(), "unknown actor kind"},
		{"kind is case-sensitive", "Member:" + uuid.NewString(), "unknown actor kind"},
		{"non-uuid id", "member:not-a-uuid", "must be a UUID"},
		{"empty id", "member:", "must be a UUID"},
		{"nested kind", "member:agent:" + uuid.NewString(), "must be a UUID"},
	}
	for _, tc := range cases {
		_, err := parseActorRef(tc.value)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: expected error containing %q, got %v", tc.name, tc.want, err)
		}
	}
}

// TestParseActorRefListUnit pins the multi_actor list contract, including the
// one place it deliberately differs from multi_select: the caller's order
// survives instead of being canonicalized.
func TestParseActorRefListUnit(t *testing.T) {
	// Fixed ids so the insertion order below is neither ascending nor
	// descending — any sort applied to the result would reorder it.
	first := "member:11111111-1111-4111-8111-111111111111"
	second := "member:22222222-2222-4222-8222-222222222222"
	third := "member:00000000-0000-4000-8000-000000000000"

	if _, err := parseActorRefList(nil); err == nil {
		t.Fatalf("nil list accepted")
	}
	if _, err := parseActorRefList([]any{}); err == nil {
		t.Fatalf("empty array accepted")
	}

	refs, err := parseActorRefList([]any{first, second, third, second})
	if err != nil {
		t.Fatalf("valid list rejected: %v", err)
	}
	got := make([]string, len(refs))
	for i, ref := range refs {
		got[i] = ref.String()
	}
	if len(got) != 3 {
		t.Fatalf("duplicate not dropped: %v", got)
	}
	if got[0] != first || got[1] != second || got[2] != third {
		t.Fatalf("insertion order not preserved: %v", got)
	}
	// Explicit: unlike multi_select there is no canonical order to sort to.
	if sort.StringsAreSorted(got) {
		t.Fatalf("list was canonicalized to sorted order: %v", got)
	}

	over := make([]any, 0, maxPropertyActorValues+1)
	for i := 0; i < maxPropertyActorValues+1; i++ {
		over = append(over, "member:"+uuid.NewString())
	}
	_, err = parseActorRefList(over)
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("more than %d", maxPropertyActorValues)) {
		t.Fatalf("over-cap list: expected cap error, got %v", err)
	}
	if refs, err := parseActorRefList(over[:maxPropertyActorValues]); err != nil || len(refs) != maxPropertyActorValues {
		t.Fatalf("list at the cap rejected: %d refs, %v", len(refs), err)
	}

	if _, err := parseActorRefList([]any{first, 42}); err == nil {
		t.Fatalf("non-string element accepted")
	}
	if _, err := parseActorRefList([]any{first, "squad:" + uuid.NewString()}); err == nil {
		t.Fatalf("unknown kind inside a list accepted")
	}
}

func TestValidatePropertyValueActorUnit(t *testing.T) {
	actorDef := makePropertyDef("actor", nil)
	multiDef := makePropertyDef("multi_actor", nil)
	// Fixed ids: memberRef sorts AFTER secondRef, so the insertion order
	// asserted below is proof that no canonicalizing sort ran.
	memberRef := "member:99999999-9999-4999-8999-999999999999"
	secondRef := "member:00000000-0000-4000-8000-000000000000"

	stored, err := validatePropertyValue(actorDef, json.RawMessage(`"`+memberRef+`"`))
	if err != nil {
		t.Fatalf("actor value rejected: %v", err)
	}
	if string(stored) != `"`+memberRef+`"` {
		t.Fatalf("actor value not stored as a plain string: %s", stored)
	}

	// actor is a single reference: no array, no number, no object, no bare id.
	for _, raw := range []string{
		`["` + memberRef + `"]`,
		`3`,
		`true`,
		`{"kind":"member","id":"` + uuid.NewString() + `"}`,
		`null`,
		`"` + uuid.NewString() + `"`,
		`"agent:` + uuid.NewString() + `"`,
		`"squad:` + uuid.NewString() + `"`,
	} {
		if _, err := validatePropertyValue(actorDef, json.RawMessage(raw)); err == nil {
			t.Fatalf("actor accepted %s", raw)
		}
	}

	// multi_actor is always an array, even for a single reference.
	for _, raw := range []string{
		`"` + memberRef + `"`,
		`[]`,
		`3`,
		`[3]`,
		`null`,
	} {
		if _, err := validatePropertyValue(multiDef, json.RawMessage(raw)); err == nil {
			t.Fatalf("multi_actor accepted %s", raw)
		}
	}

	// Duplicates collapse and the caller's order survives.
	stored, err = validatePropertyValue(multiDef, json.RawMessage(
		`["`+memberRef+`","`+secondRef+`","`+memberRef+`"]`))
	if err != nil {
		t.Fatalf("multi_actor value rejected: %v", err)
	}
	if string(stored) != `["`+memberRef+`","`+secondRef+`"]` {
		t.Fatalf("multi_actor not deduped in insertion order: %s", stored)
	}
}

// TestPropertyActorDefinitionRejectsOptions: actor types are resolved against
// the workspace, not against a config option list, so a definition carrying
// options is a modelling mistake and must be refused.
func TestPropertyActorDefinitionRejectsOptions(t *testing.T) {
	for _, propType := range []string{"actor", "multi_actor"} {
		_, err := validatePropertyConfig(propType, &PropertyConfig{
			Options: []PropertyOption{{Name: "Anyone", Color: "#ef4444"}},
		})
		if err == nil || !strings.Contains(err.Error(), "does not accept options") {
			t.Fatalf("%s definition with options: expected rejection, got %v", propType, err)
		}
		cfg, err := validatePropertyConfig(propType, nil)
		if err != nil || string(cfg) != `{}` {
			t.Fatalf("%s definition without config: expected {}, got %s (%v)", propType, cfg, err)
		}
	}

	w := httptest.NewRecorder()
	testHandler.CreateProperty(w, newRequest("POST", "/api/properties", map[string]any{
		"name": "Owner" + uuid.NewString()[:8],
		"type": "actor",
		"config": map[string]any{"options": []map[string]any{
			{"name": "Anyone", "color": "#ef4444"},
		}},
	}))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "does not accept options") {
		t.Fatalf("CreateProperty actor+options: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestIssueActorPropertyValues drives the actor / multi_actor pair through the
// real endpoints: a reference must resolve to something that exists in this
// workspace, and the stored bag keeps the prefixed "<kind>:<uuid>" form.

// TestIssueActorPropertyFacets: actor and multi_actor values aggregate into
// table facets, which is what backs the "= me" filter in the header. Members
// are the only referenceable kind, and workspace membership is visible to
// every member, so there is no visibility gate to apply here — a plain member
// sees the same keys the owner does.

// TestIssuePropertyFacetNoValue verifies the checkbox facet reports an unset
// "__none__" bucket alongside "true"/"false", so the filter menu's "No value"
// option carries a real count. A brand-new select marker narrows the facet to
// exactly the two issues created here, keeping the counts deterministic in the
// shared fixture workspace.

// TestIssuePropertyFacetScalarTypes covers the facet branches added for
// text / number / date / url, which previously fell through to the 422
// default. A marker select narrows the facet to exactly two issues.

// createPropertyTestMember adds a second real member to the fixture workspace
// so multi_actor ordering can be asserted with two resolvable references.
func createPropertyTestMember(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	email := "actor-second-" + uuid.NewString()[:8] + "@multica.test"
	var userID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ('Actor Second Member', $1) RETURNING id`, email,
	).Scan(&userID); err != nil {
		t.Fatalf("create second member user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE email = $1`, email)
	})
	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		testWorkspaceID, userID,
	); err != nil {
		t.Fatalf("add second member: %v", err)
	}
	return userID
}

func TestParsePropertiesFilterOperatorUnit(t *testing.T) {
	defID := uuid.NewString()
	w := httptest.NewRecorder()
	var args []any
	addArg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	// A contains member compiles to the operator pattern (with the LIKE
	// wildcards escaped) and renders as an ILIKE predicate, never as a
	// containment pattern or the no-value marker.
	groups, ok := parsePropertiesFilterParam(w, fmt.Sprintf(`{"%s":[{"op":"contains","value":"50%% off"}]}`, defID))
	if !ok {
		t.Fatalf("contains parse failed: %s", w.Body.String())
	}
	if len(groups) != 1 || len(groups[0]) != 1 {
		t.Fatalf("expected one group with one alternative, got %d / %d", len(groups), len(groups[0]))
	}
	if _, isMarker := parseNoPropertyValuePattern(groups[0][0]); isMarker {
		t.Fatalf("operator alternative misdetected as the no-value marker")
	}
	pattern, isOp := parseOperatorPattern(groups[0][0])
	if !isOp || pattern.Op != "contains" || pattern.Def != defID {
		t.Fatalf("expected contains operator pattern, got %+v isOp=%v", pattern, isOp)
	}
	if pattern.Value != `50\% off` {
		t.Fatalf("contains value not LIKE-escaped: %q", pattern.Value)
	}
	args = nil
	sql := propertiesFilterPredicate(groups, addArg)
	if !strings.Contains(sql, "jsonb_typeof") || !strings.Contains(sql, "= 'string'") ||
		!strings.Contains(sql, "ILIKE") || strings.Contains(sql, "@>") {
		t.Fatalf("contains predicate wrong: %s", sql)
	}

	// Numeric ops validate their value and render the typed comparison.
	groups, ok = parsePropertiesFilterParam(w, fmt.Sprintf(`{"%s":[{"op":"gte","value":"3.5"}]}`, defID))
	if !ok {
		t.Fatalf("gte parse failed: %s", w.Body.String())
	}
	args = nil
	sql = propertiesFilterPredicate(groups, addArg)
	if !strings.Contains(sql, "CASE WHEN") || !strings.Contains(sql, "::numeric END >= $") ||
		!strings.Contains(sql, "::numeric)") {
		t.Fatalf("gte predicate wrong: %s", sql)
	}
	// The canonical decimal stays a string bind and is explicitly cast to
	// numeric, matching the static open_only path without float8 demotion.
	last := args[len(args)-1]
	if value, isString := last.(string); !isString || value != "3.5" {
		t.Fatalf("gte bind arg must be canonical string 3.5, got %T %v", last, last)
	}

	// ParseFloat accepts forms Postgres ::numeric rejects (hex-float,
	// underscores); the compiled pattern must store the canonical plain
	// decimal or the static open_only unroll would 500 on the cast.
	groups, ok = parsePropertiesFilterParam(w, fmt.Sprintf(`{"%s":[{"op":"gt","value":"0x1p4"}]}`, defID))
	if !ok {
		t.Fatalf("hex-float parse failed: %s", w.Body.String())
	}
	pattern, isOp = parseOperatorPattern(groups[0][0])
	if !isOp || pattern.Value != "16" {
		t.Fatalf("hex-float bound not canonicalized to 16: %+v isOp=%v", pattern, isOp)
	}

	// Date ops render the lexicographic string comparison.
	groups, ok = parsePropertiesFilterParam(w, fmt.Sprintf(`{"%s":[{"op":"before","value":"2026-02-01"}]}`, defID))
	if !ok {
		t.Fatalf("before parse failed: %s", w.Body.String())
	}
	args = nil
	sql = propertiesFilterPredicate(groups, addArg)
	if !strings.Contains(sql, "= 'string' AND") || !strings.Contains(sql, "< $") {
		t.Fatalf("before predicate wrong: %s", sql)
	}

	// An operator composes OR-style with legacy equality members and the
	// no-value sentinel in one group.
	groups, ok = parsePropertiesFilterParam(w, fmt.Sprintf(`{"%s":["3.5",{"op":"lt","value":"10"},"__none__"]}`, defID))
	if !ok {
		t.Fatalf("mixed parse failed: %s", w.Body.String())
	}
	// "3.5" expands to 3 containment forms, the operator is 1, the marker 1.
	if len(groups[0]) != 5 {
		t.Fatalf("expected 5 alternatives, got %d", len(groups[0]))
	}

	// Invalid members are rejected with a 400.
	for name, raw := range map[string]string{
		"unknown op":    `{"op":"regex","value":"x"}`,
		"empty value":   `{"op":"contains","value":""}`,
		"non-numeric":   `{"op":"gt","value":"abc"}`,
		"NaN":           `{"op":"lt","value":"NaN"}`,
		"bad date":      `{"op":"before","value":"02/01/2026"}`,
		"missing value": `{"op":"contains"}`,
	} {
		w = httptest.NewRecorder()
		_, ok := parsePropertiesFilterParam(w, fmt.Sprintf(`{"%s":[%s]}`, defID, raw))
		if ok {
			t.Fatalf("%s: expected rejection", name)
		}
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d", name, w.Code)
		}
	}
}


// TestPropertyContainsPrefilterCompilationUnit pins which contains needles get
// the bigram prefilter that migration 446's index serves. The prefilter matches
// against the jsonb text form of the whole properties object, so a needle that
// jsonb escapes on serialization (", \, control characters) must compile
// without it — the alternative would silently drop matching rows.
func TestPropertyContainsPrefilterCompilationUnit(t *testing.T) {
	defID := uuid.NewString()
	compile := func(t *testing.T, needle string) (propertyOperatorPattern, json.RawMessage, string) {
		t.Helper()
		raw, err := json.Marshal(map[string]any{defID: []any{map[string]any{"op": "contains", "value": needle}}})
		if err != nil {
			t.Fatalf("marshal filter: %v", err)
		}
		w := httptest.NewRecorder()
		groups, ok := parsePropertiesFilterParam(w, string(raw))
		if !ok {
			t.Fatalf("parse %q: %s", needle, w.Body.String())
		}
		pattern, isOp := parseOperatorPattern(groups[0][0])
		if !isOp {
			t.Fatalf("%q did not compile to an operator pattern", needle)
		}
		var args []any
		addArg := func(v any) string {
			args = append(args, v)
			return fmt.Sprintf("$%d", len(args))
		}
		return pattern, groups[0][0], propertiesFilterPredicate(groups, addArg)
	}

	for _, tc := range []struct {
		name          string
		needle        string
		wantPrefilter string
	}{
		{"plain ascii", "world", "world"},
		// LIKE wildcards are escaped identically on both sides, so they stay
		// prefilterable — the escape is a backslash in the pattern, not in the
		// serialized value.
		{"like wildcards", "50% off_now", `50\% off\_now`},
		{"cjk", "中文属性", "中文属性"},
		// Short needles are prefiltered too: pg_bigm indexes 1- and
		// 2-character keywords, which is the capability it exists for over
		// pg_trgm, and one CJK character is already a word.
		{"single character", "x", "x"},
		{"single cjk character", "文", "文"},
		{"double quote", `say "hi"`, ""},
		{"backslash", `C:\logs`, ""},
		{"tab", "col\tvalue", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pattern, alt, sql := compile(t, tc.needle)
			if pattern.Prefilter != tc.wantPrefilter {
				t.Fatalf("prefilter for %q: got %q, want %q", tc.needle, pattern.Prefilter, tc.wantPrefilter)
			}
			// The static open_only unroll keys off the presence of the JSON
			// member, so an unsafe needle must omit it rather than carry "".
			var members map[string]json.RawMessage
			if err := json.Unmarshal(alt, &members); err != nil {
				t.Fatalf("decode alternative: %v", err)
			}
			_, hasMember := members["prefilter"]
			if hasMember != (tc.wantPrefilter != "") {
				t.Fatalf("alternative %s: prefilter member present=%v, want %v", alt, hasMember, tc.wantPrefilter != "")
			}
			hasClause := strings.Contains(sql, "LOWER(i.properties::text) LIKE LOWER(")
			if hasClause != (tc.wantPrefilter != "") {
				t.Fatalf("predicate %s: prefilter clause present=%v, want %v", sql, hasClause, tc.wantPrefilter != "")
			}
			// The per-key check is authoritative on every path, prefiltered or
			// not.
			if !strings.Contains(sql, "ILIKE") || !strings.Contains(sql, "jsonb_typeof") {
				t.Fatalf("predicate lost its per-key contains check: %s", sql)
			}
		})
	}
}

// TestPropertyContainsPrefilterKeepsResultsExact runs the needles above against
// real rows on both serving paths. The prefilter is lossy by design, so the
// property under test is that it never changes an answer: needles jsonb escapes
// must still match, and a needle that only appears elsewhere in the object —
// under another definition, or in a definition id — must still miss.
