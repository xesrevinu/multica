package orchestrator

import (
	"strings"
	"testing"
)

// A review reject reaches the owner's next run as a structured handoff: the
// reviewer's finding, the previous head and the attempt, persisted on the
// transition and rendered for the claiming agent only.
func TestIntegrationReviewRejectHandoff(t *testing.T) {
	f := newFixture(t, itSpec)
	happyAgents(f)
	rounds := 0
	approve := f.script["rev"]
	f.script["rev"] = func(f *fixture, key string, m map[string]string) {
		rounds++
		if rounds > 1 {
			approve(f, key, m)
			return
		}
		f.gh.mu.Lock()
		h := f.gh.heads[key]
		f.gh.mu.Unlock()
		var issueID string
		_ = f.svc.opt.Pool.QueryRowContext(f.ctx, `SELECT id::text FROM issue WHERE workspace_id=$1 AND number=$2`, f.ws, strings.TrimPrefix(key, f.prefix+"-")).Scan(&issueID)
		rid, _ := f.svc.st.agentID(f.ctx, f.ws, "rev")
		f.exec(`INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content) VALUES ($1, $2, 'agent', $3, $4)`, issueID, f.ws, rid, "1 important: [important] src/sq.js:3 negative input returns NaN")
		m["verdict"], m["reviewed_sha"] = "changes", h
	}
	id, _ := f.issue("add sq")
	f.drive(12)
	wantSeq(t, f.tids(id), "T1_start T2_review T3_rework T2_review T4_done")
	var kind, to, head string
	if err := f.svc.opt.Pool.QueryRowContext(f.ctx, `SELECT decision->'handoff'->>'kind', decision->'handoff'->>'to', decision->'handoff'->>'prev_head' FROM orchestrator_transition WHERE issue_id=$1 AND tid='T3_rework'`, id).Scan(&kind, &to, &head); err != nil {
		t.Fatal(err)
	}
	if kind != HKReviewChanges || to != "a" || head == "" {
		t.Fatalf("persisted handoff kind=%q to=%q head=%q", kind, to, head)
	}
	var n int
	_ = f.svc.opt.Pool.QueryRowContext(f.ctx, `SELECT count(*) FROM orchestrator_transition WHERE issue_id=$1 AND tid='T3_rework' AND decision->'handoff'->'review_comments'->0->>'body' LIKE '%NaN%'`, id).Scan(&n)
	if n != 1 {
		t.Fatal("reviewer finding not captured in the handoff")
	}
}

func TestIntegrationHandoffBriefForClaimingAgent(t *testing.T) {
	f := newFixture(t, itSpec)
	id, _ := f.issue("brief")
	f.exec(`INSERT INTO orchestrator_transition (workspace_id, project_id, issue_id, tid, idem_key, decision, state) VALUES ($1,$2,$3,'T3_rework','x1',
		'{"tid":"T3_rework","assignee_kind":"agent","assignee":"a","handoff":{"kind":"ci_failure","summary":"precheck checks_red: test=FAILURE","to":"a","role":"owner","attempts":{"impl":2},"attempt":2}}','done')`, f.ws, f.proj, id)
	if b := f.svc.HandoffBrief(f.ctx, id, "a"); !strings.Contains(b, "handoff: ci_failure") || !strings.Contains(b, "attempt: 2") {
		t.Fatalf("brief for owner: %q", b)
	}
	if b := f.svc.HandoffBrief(f.ctx, id, "rev"); b != "" {
		t.Fatalf("brief leaked to another agent: %q", b)
	}
	// a later normal start (no handoff) supersedes it
	f.exec(`INSERT INTO orchestrator_transition (workspace_id, project_id, issue_id, tid, idem_key, decision, state) VALUES ($1,$2,$3,'T2_review','x2','{"tid":"T2_review","assignee_kind":"agent","assignee":"rev"}','done')`, f.ws, f.proj, id)
	if b := f.svc.HandoffBrief(f.ctx, id, "a"); b != "" {
		t.Fatalf("stale brief: %q", b)
	}
}
