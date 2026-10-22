package orchestrator

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func (f *fixture) api(method, path string) (int, map[string]any) {
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	req = req.WithContext(middleware.SetMemberContext(req.Context(), f.ws, db.Member{Role: "owner"}))
	req.Header.Set("X-User-ID", f.human)
	rec := httptest.NewRecorder()
	f.svc.Routes().ServeHTTP(rec, req)
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestIntegrationIssueViewsAndMemberActions(t *testing.T) {
	f := newFixture(t, itSpec)
	f.svc.ghOnce.Do(func() {}) // no GitHub App in tests
	happyAgents(f)
	orchID, orchKey := f.issue("orchestrated")
	_, bareKey := f.issue("bare")
	f.exec(`UPDATE issue SET project_id=NULL, priority='high' WHERE workspace_id=$1 AND title='bare'`, f.ws)
	childID, _ := f.issue("child")
	f.exec(`UPDATE issue SET parent_issue_id=$2, status='done' WHERE id=$1`, childID, orchID)
	var prID string
	if err := f.pool.QueryRowContext(f.ctx, `INSERT INTO github_pull_request (workspace_id, installation_id, repo_owner, repo_name, pr_number, title, state, html_url, branch, head_sha, pr_created_at, pr_updated_at, additions, deletions)
		VALUES ($1, 1, 'o', 'r', 9, 'pr', 'open', 'https://x/9', 'agent/x', 'abc', now(), now(), 5, 2) RETURNING id::text`, f.ws).Scan(&prID); err != nil {
		t.Fatal(err)
	}
	f.exec(`INSERT INTO issue_pull_request (issue_id, pull_request_id) VALUES ($1,$2)`, orchID, prID)
	f.svc.reconcile(f.ctx) // loads the team, starts T1

	code, body := f.api("GET", "/issues")
	if code != 200 {
		t.Fatalf("issues %d %v", code, body)
	}
	rows := body["issues"].([]any)
	byKey := map[string]map[string]any{}
	for _, r := range rows {
		m := r.(map[string]any)
		byKey[m["key"].(string)] = m
	}
	o, b := byKey[orchKey], byKey[bareKey]
	if o == nil || b == nil {
		t.Fatalf("missing rows: %v", body)
	}
	if o["orchestrated"] != true || o["pr"].(map[string]any)["number"].(float64) != 9 || o["children"].(map[string]any)["total"].(float64) != 1 {
		t.Fatalf("orchestrated row %v", o)
	}
	if o["worktree"].(map[string]any)["branch"] == "" {
		t.Fatalf("worktree branch missing: %v", o["worktree"])
	}
	if _, has := b["project"]; has || b["orchestrated"] != false || b["priority"] != "high" {
		t.Fatalf("bare row %v", b)
	}

	if code, body = f.api("GET", "/issues/"+orchKey+"/timeline"); code != 200 || len(body["transitions"].([]any)) == 0 {
		t.Fatalf("timeline %d %v", code, body)
	}
	if code, body = f.api("GET", "/issues/"+orchKey+"/pr"); code != 200 || body["live"] != false || body["summary"] == nil {
		t.Fatalf("pr %d %v", code, body)
	}
	if code, body = f.api("POST", "/issues/"+orchKey+"/pr/merge"); code != 409 || body["code"] != "engine_owned" {
		t.Fatalf("merge %d %v", code, body)
	}
	if code, _ = f.api("GET", "/issues/"+bareKey+"/pr"); code != 200 {
		t.Fatalf("bare pr %d", code)
	}

	// Pause: the engine only waits; resume continues; cancel ends it.
	if code, body = f.api("POST", "/flow/"+orchKey+"/pause"); code != 200 {
		t.Fatalf("pause %d %v", code, body)
	}
	before := len(f.tids(orchID))
	f.drive(5)
	if got := len(f.tids(orchID)); got != before {
		t.Fatalf("paused issue moved: %v", f.tids(orchID))
	}
	if code, _ = f.api("POST", "/flow/"+orchKey+"/resume"); code != 200 {
		t.Fatal("resume")
	}
	if code, _ = f.api("POST", "/flow/"+orchKey+"/cancel"); code != 200 {
		t.Fatal("cancel")
	}
	f.svc.reconcile(f.ctx)
	if st, _, m := f.state(orchID); st != StCancelled || m["cancel_requested"] != "" {
		t.Fatalf("after cancel: %s %v (%v)", st, m, f.tids(orchID))
	}
	if code, _ = f.api("POST", "/flow/"+orchKey+"/cancel"); code != 409 {
		t.Fatalf("second cancel %d", code)
	}
	if code, _ = f.api("POST", "/flow/"+bareKey+"/pause"); code != 409 {
		t.Fatalf("pause on non-orchestrated %d", code)
	}
}
