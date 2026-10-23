package orchestrator

// Integration layer: the real runtime (snapshot → Decide → actuator →
// transition log, activity rows, inbox) on a real Postgres, with a fake API
// router (what the server does on PUT/rerun/cancel), fake agents (scripted
// owner/reviewer behaviour on top of agent_task_queue) and a fake GitHub
// (branch heads, pre-check and merge results). No daemon, no LLM, no network.
//
//	eval "$(scripts/orch-testdb.sh | tail -1)"
//	go test ./internal/orchestrator -run Integration
//
// Skipped when ORCH_TEST_DATABASE_URL is unset or unreachable.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
		"strings"
	"sync"
	"testing"
	"time"

	"database/sql"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// --- fake GitHub --------------------------------------------------------------

type fakeGH struct {
	mu       sync.Mutex
	heads    map[string]string // issue key → branch head
	precheck ProbeResult
	merge    ProbeResult
	merged   []string
	same     map[string]bool
}

func newFakeGH() *fakeGH {
	return &fakeGH{heads: map[string]string{}, same: map[string]bool{},
		precheck: ProbeResult{OK: true, Kind: "dry", PR: "https://github.com/x/y/pull/1", PRNum: "1"},
		merge:    ProbeResult{OK: true, Kind: "merged", SHA: "m1"}}
}

func (g *fakeGH) head(_ context.Context, _ *Spec, key string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.heads[key], nil
}
func (g *fakeGH) samePatch(_ context.Context, _ *Team, a, b string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.same[a+"|"+b]
}
func (g *fakeGH) refreshHub(context.Context, *Spec, bool) {}
func (g *fakeGH) prober(context.Context, *Team) Prober {
	return func(s *Snapshot, merge bool) ProbeResult {
		g.mu.Lock()
		defer g.mu.Unlock()
		if merge {
			if g.merge.OK {
				g.merged = append(g.merged, s.Issue.Key)
			}
			return g.merge
		}
		return g.precheck
	}
}

// --- fake API + agents ----------------------------------------------------------

type agentFn func(f *fixture, key string, meta map[string]string) // what one run does

type fixture struct {
	t       *testing.T
	ctx     context.Context
	pool    *sql.DB
	svc     *Service
	gh      *fakeGH
	ws      string
	proj    string
	human   string
	prefix  string
	agents  map[string]string // name → id
	names   map[string]string // id → name
	script  map[string]agentFn
	runs    int
	runtime string
}

// ServeHTTP is the fake API: it does what the real handlers do for the three
// calls the actuator makes, including "assigning an agent queues its run".
func (f *fixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/") // api issues <id> [rerun | tasks <tid> cancel]
	if len(parts) < 3 || parts[0] != "api" || parts[1] != "issues" {
		http.Error(w, "unexpected "+r.URL.Path, 404)
		return
	}
	id := parts[2]
	var body map[string]any
	b, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(b, &body)
	switch {
	case r.Method == "PUT" && len(parts) == 3:
		if st, ok := body["status"].(string); ok {
			f.exec(`UPDATE issue SET status=$2, revision=revision+1, updated_at=now() WHERE id=$1`, id, st)
		}
		if typ, ok := body["assignee_type"].(string); ok {
			aid, _ := body["assignee_id"].(string)
			f.exec(`UPDATE issue SET assignee_type=$2, assignee_id=$3, revision=revision+1, updated_at=now() WHERE id=$1`, id, typ, aid)
			if typ == "agent" {
				f.queue(id, aid)
			}
		}
	case r.Method == "POST" && len(parts) == 4 && parts[3] == "rerun":
		var aid string
		_ = f.pool.QueryRowContext(f.ctx, `SELECT COALESCE(assignee_id,'') FROM issue WHERE id=$1`, id).Scan(&aid)
		f.queue(id, aid)
	case r.Method == "POST" && len(parts) == 6 && parts[5] == "cancel":
		f.exec(`UPDATE agent_task_queue SET status='cancelled', completed_at=now() WHERE id=$1`, parts[4])
	default:
		http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, 404)
		return
	}
	w.WriteHeader(200)
}

func (f *fixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.ExecContext(f.ctx, q, args...); err != nil {
		f.t.Fatalf("%s: %v", q, err)
	}
}

func (f *fixture) queue(issueID, agentID string) {
	f.exec(`INSERT INTO agent_task_queue (id, agent_id, issue_id, status, runtime_id) VALUES (gen_random_uuid(), $1, $2, 'queued', $3)`, agentID, issueID, f.runtime)
}

// work runs every queued task: the scripted agent acts, then the task ends.
func (f *fixture) work() int {
	rows, err := f.pool.QueryContext(f.ctx, `SELECT q.id, q.agent_id, q.issue_id, w.issue_prefix || '-' || i.number
		FROM agent_task_queue q JOIN issue i ON i.id=q.issue_id JOIN workspace w ON w.id=i.workspace_id
		WHERE q.status='queued' AND i.workspace_id=$1 ORDER BY q.created_at`, f.ws)
	if err != nil {
		f.t.Fatal(err)
	}
	type task struct{ id, agent, issue, key string }
	var ts []task
	for rows.Next() {
		var x task
		_ = rows.Scan(&x.id, &x.agent, &x.issue, &x.key)
		ts = append(ts, x)
	}
	rows.Close()
	for _, x := range ts {
		f.runs++
		f.exec(`UPDATE agent_task_queue SET status='running', started_at=now() WHERE id=$1`, x.id)
		meta := map[string]string{}
		if fn := f.script[f.names[x.agent]]; fn != nil {
			fn(f, x.key, meta)
		}
		status := "completed"
		if meta["_run"] != "" {
			status = meta["_run"]
			delete(meta, "_run")
		}
		f.writeMeta(x.issue, meta)
		f.exec(`UPDATE agent_task_queue SET status=$2, completed_at=now() WHERE id=$1`, x.id, status)
	}
	return len(ts)
}

// writeMeta is an agent's `multica issue metadata set` (phase last).
func (f *fixture) writeMeta(issueID string, meta map[string]string) {
	if len(meta) == 0 {
		return
	}
	b, _ := json.Marshal(meta)
	f.exec(`UPDATE issue SET metadata = json_patch(COALESCE(metadata, '{}'), $2), revision=revision+1, updated_at=now() WHERE id=$1`, issueID, string(b))
}

func newFixture(t *testing.T, specYAML string) *fixture {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	pool := testutil.OpenTestDB(t)
	f := &fixture{t: t, ctx: ctx, pool: pool, gh: newFakeGH(), agents: map[string]string{}, names: map[string]string{}, script: map[string]agentFn{}}
	sfx := fmt.Sprintf("%d", rand.Int63())
	f.prefix = "T" + sfx[:4]
	q := func(sql string, args ...any) string {
		var id string
		if err := pool.QueryRowContext(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return id
	}
	f.human = q(`INSERT INTO "user" (id, name, email) VALUES (gen_random_uuid(), 'Ray', $1) RETURNING id`, "ray+"+sfx+"@example.com")
	f.ws = q(`INSERT INTO workspace (id, name, slug, issue_prefix) VALUES (gen_random_uuid(), 'it', $1, $2) RETURNING id`, "it-"+sfx, f.prefix)
	t.Cleanup(func() {
		_, _ = pool.ExecContext(context.Background(), `DELETE FROM workspace WHERE id=$1`, f.ws)
		_, _ = pool.ExecContext(context.Background(), `DELETE FROM "user" WHERE id=$1`, f.human)
	})
	f.exec(`INSERT INTO member (id, workspace_id, user_id, role) VALUES (gen_random_uuid(), $1, $2, 'owner')`, f.ws, f.human)
	f.runtime = q(`INSERT INTO agent_runtime (id, workspace_id, name, provider, runtime_mode) VALUES (gen_random_uuid(), $1, 'it', 'fake', 'local') RETURNING id`, f.ws)
	for _, n := range []string{"lead", "rev", "a", "b"} {
		id := q(`INSERT INTO agent (id, workspace_id, name, runtime_mode, runtime_id) VALUES (gen_random_uuid(), $1, $2, 'local', $3) RETURNING id`, f.ws, n, f.runtime)
		f.agents[n], f.names[id] = id, n
	}
	f.proj = q(`INSERT INTO project (id, workspace_id, title) VALUES (gen_random_uuid(), $1, 'it') RETURNING id`, f.ws)
	sp, err := ParseSpec([]byte(specYAML))
	if err != nil {
		t.Fatal(err)
	}
	f.exec(`INSERT INTO orchestrator_team (project_id, workspace_id, spec, spec_yaml, human_user_id) VALUES ($1,$2,$3,$4,$5)`, f.proj, f.ws, string(sp.JSON()), specYAML, f.human)

	f.svc = New(Options{Pool: pool, Bus: events.New(), Queries: db.New(pool)})
	f.svc.disabled = false
	f.svc.router = f
	f.svc.repo = f.gh
	if err := f.svc.ensureServiceUser(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ensureMember(ctx, f.ws); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) issue(title string, labels ...string) (id, key string) {
	var n int
	if err := f.pool.QueryRowContext(f.ctx, `INSERT INTO issue (id, workspace_id, project_id, title, status, creator_type, creator_id, number)
		VALUES (gen_random_uuid(), $1,$2,$3,'todo','member',$4, COALESCE((SELECT max(number) FROM issue WHERE workspace_id=$1),0)+1) RETURNING id, number`,
		f.ws, f.proj, title, f.human).Scan(&id, &n); err != nil {
		f.t.Fatal(err)
	}
	for _, l := range labels {
		var lid string
		_, _ = f.pool.ExecContext(f.ctx, `INSERT INTO issue_label (id, workspace_id, name, color) VALUES (gen_random_uuid(), $1, $2, '#888') ON CONFLICT DO NOTHING`, f.ws, l)
		if err := f.pool.QueryRowContext(f.ctx, `SELECT id FROM issue_label WHERE workspace_id=$1 AND name=$2 LIMIT 1`, f.ws, l).Scan(&lid); err != nil {
			f.t.Fatal(err)
		}
		f.exec(`INSERT INTO issue_to_label (label_id, issue_id) VALUES ($1,$2)`, lid, id)
	}
	return id, fmt.Sprintf("%s-%d", f.prefix, n)
}

// drive alternates engine passes and agent work until nothing moves.
func (f *fixture) drive(max int) {
	for i := 0; i < max; i++ {
		f.svc.reconcile(f.ctx)
		if f.work() == 0 {
			f.svc.reconcile(f.ctx)
			if f.work() == 0 {
				return
			}
		}
	}
}

func (f *fixture) tids(issueID string) []string {
	rows, err := f.pool.QueryContext(f.ctx, `SELECT tid || CASE WHEN state='done' THEN '' ELSE '('||state||')' END FROM orchestrator_transition WHERE issue_id=$1 ORDER BY id`, issueID)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		out = append(out, normTid(s))
	}
	return out
}

func (f *fixture) state(issueID string) (status, assignee string, meta map[string]string) {
	var raw []byte
	if err := f.pool.QueryRowContext(f.ctx, `SELECT i.status, COALESCE(a.name, i.assignee_id, ''), i.metadata FROM issue i LEFT JOIN agent a ON a.id=i.assignee_id WHERE i.id=$1`, issueID).Scan(&status, &assignee, &raw); err != nil {
		f.t.Fatal(err)
	}
	return status, assignee, metaStrings(raw)
}

func (f *fixture) count(q string, args ...any) int {
	var n int
	if err := f.pool.QueryRowContext(f.ctx, q, args...).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func wantSeq(t *testing.T, got []string, want string) {
	t.Helper()
	if strings.Join(got, " ") != want {
		t.Fatalf("transitions\n got: %s\nwant: %s", strings.Join(got, " "), want)
	}
}

const itSpec = `orchestrator:
  roles: {lead: lead, reviewer: rev, owners: [a, b]}
  repo: {mode: github, slug: x/y}
`

// owner pushes head h-<key>-<n> and hands over; reviewer approves the head.
func happyAgents(f *fixture) {
	n := 0
	f.script["a"] = func(f *fixture, key string, m map[string]string) {
		n++
		h := fmt.Sprintf("h-%s-%d", key, n)
		f.gh.mu.Lock()
		f.gh.heads[key] = h
		f.gh.mu.Unlock()
		m["head_sha"], m["phase"] = h, PhReviewRequested
	}
	f.script["b"] = f.script["a"]
	f.script["rev"] = func(f *fixture, key string, m map[string]string) {
		f.gh.mu.Lock()
		h := f.gh.heads[key]
		f.gh.mu.Unlock()
		m["verdict"], m["reviewed_sha"] = "approve", h
	}
}

func TestIntegrationHappyPath(t *testing.T) {
	f := newFixture(t, itSpec)
	happyAgents(f)
	id, key := f.issue("add sq")
	f.drive(10)
	wantSeq(t, f.tids(id), "T1_start T2_review T4_done")
	st, _, m := f.state(id)
	if st != StDone || m["merged_sha"] != "m1" || m["pr"] == "" {
		t.Fatalf("final state %s %v", st, m)
	}
	if n := f.count(`SELECT count(*) FROM activity_log WHERE issue_id=$1 AND action='orchestrator_transition'`, id); n != 3 {
		t.Fatalf("timeline rows = %d, want 3", n)
	}
	if len(f.gh.merged) != 1 || f.gh.merged[0] != key {
		t.Fatalf("merged %v", f.gh.merged)
	}
	if f.runs != 2 {
		t.Fatalf("agent runs = %d, want 2 (owner + reviewer)", f.runs)
	}
}

func TestIntegrationAcceptanceRejectThenAccept(t *testing.T) {
	f := newFixture(t, itSpec)
	happyAgents(f)
	id, _ := f.issue("screen", "needs-GUI-Master")
	f.drive(10)
	wantSeq(t, f.tids(id), "T1_start T2_review T10_await_acceptance")
	st, who, _ := f.state(id)
	if st != StInReview || who != f.human {
		t.Fatalf("gate: %s %s", st, who)
	}
	if n := f.count(`SELECT count(*) FROM inbox_item WHERE issue_id=$1 AND recipient_id=$2 AND type='orchestrator_alert'`, id, f.human); n != 1 {
		t.Fatalf("inbox items = %d", n)
	}
	f.writeMeta(id, map[string]string{"acceptance": "reject: overlaps title"})
	f.drive(10)
	f.writeMeta(id, map[string]string{"acceptance": "ok"})
	f.drive(10)
	wantSeq(t, f.tids(id), "T1_start T2_review T10_await_acceptance T3_rework T2_review T10_await_acceptance T4_done")
}

func TestIntegrationOutOfBandEditConverges(t *testing.T) {
	f := newFixture(t, itSpec)
	happyAgents(f)
	f.script["rev"] = nil // reviewer never answers: issue rests in review
	id, _ := f.issue("oob")
	f.svc.reconcile(f.ctx)
	f.work()
	f.svc.reconcile(f.ctx)
	f.exec(`UPDATE issue SET status='todo', revision=revision+1 WHERE id=$1`, id)
	f.svc.reconcile(f.ctx)
	st, _, _ := f.state(id)
	if st != StInReview {
		t.Fatalf("status %s, want converged back to in_review", st)
	}
	got := f.tids(id)
	if len(got) < 3 || got[2] != "T9_reconcile" {
		t.Fatalf("want T9 after the edit: %v", got)
	}
}

func TestIntegrationDeadLetterBlocks(t *testing.T) {
	f := newFixture(t, itSpec)
	happyAgents(f)
	f.svc.opt.BackoffBase, f.svc.opt.BackoffCap = time.Millisecond, time.Millisecond
	t.Setenv("MULTICA_ORCHESTRATOR_FAULT", "T2_review:99,T9_reconcile:99")
	faultMu.Lock()
	faultLeft = map[string]int{}
	faultMu.Unlock()
	id, _ := f.issue("dead letter")
	f.svc.reconcile(f.ctx)
	f.work()
	for i := 0; i < 8; i++ {
		time.Sleep(5 * time.Millisecond)
		f.svc.reconcile(f.ctx)
	}
	st, who, m := f.state(id)
	if st != StBlocked || who != "lead" || !strings.HasPrefix(m["blocked_reason"], "dead-letter") {
		t.Fatalf("want dead-letter block to lead, got %s %s %v", st, who, m)
	}
}

func TestIntegrationShadowChangesNothing(t *testing.T) {
	f := newFixture(t, strings.Replace(itSpec, "orchestrator:\n", "orchestrator:\n  shadow: true\n", 1))
	happyAgents(f)
	id, _ := f.issue("shadow")
	f.drive(3)
	f.drive(3)
	wantSeq(t, f.tids(id), "T1_start(shadow)")
	st, who, m := f.state(id)
	if st != StTodo || who != "" || len(m) != 0 || f.runs != 0 {
		t.Fatalf("shadow mutated: %s %q %v runs=%d", st, who, m, f.runs)
	}
	if f.svc.Owns(f.proj) {
		t.Fatal("shadow team must not own writes")
	}
}

func TestIntegrationConcurrentPassesApplyOnce(t *testing.T) {
	f := newFixture(t, itSpec)
	id, _ := f.issue("race")
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); f.svc.reconcile(f.ctx) }()
	}
	wg.Wait()
	wantSeq(t, f.tids(id), "T1_start")
	if n := f.count(`SELECT count(*) FROM agent_task_queue WHERE issue_id=$1`, id); n != 1 {
		t.Fatalf("runs queued = %d, want 1", n)
	}
}

func TestIntegrationCargoSlotsAcrossIssues(t *testing.T) {
	f := newFixture(t, `orchestrator:
  roles: {lead: lead, reviewer: rev, owners: [a, b]}
  repo: {mode: github, slug: x/y}
  limits: {slots: {cargo: 1}}
`)
	x, _ := f.issue("c1", "needs:cargo")
	y, _ := f.issue("c2", "needs:cargo")
	f.svc.reconcile(f.ctx)
	// T1 keeps the issue in todo and assigns the owner, so "started" means
	// an owner was assigned (fixture issues are created in todo).
	_, ax, _ := f.state(x)
	_, ay, _ := f.state(y)
	if (ax != "") == (ay != "") {
		t.Fatalf("exactly one cargo issue may start: assignees %q %q", ax, ay)
	}
}
