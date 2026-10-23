package maintenance

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

const compatIssueStatusDDL = `
DROP TABLE IF EXISTS issue_status;
CREATE TABLE issue_status (
    id TEXT DEFAULT (gen_random_uuid()) NOT NULL,
    workspace_id TEXT NOT NULL,
    "key" TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT DEFAULT '' NOT NULL,
    category TEXT NOT NULL,
    color TEXT NOT NULL,
    is_system INTEGER DEFAULT 0 NOT NULL,
    "position" REAL DEFAULT 0 NOT NULL,
    archived_at TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    icon TEXT DEFAULT '' NOT NULL,
    CONSTRAINT issue_status_category_check CHECK ((category IN ('backlog','todo','in_progress','in_review','blocked','cancelled','unstarted','started','done','closed')))
);
CREATE UNIQUE INDEX issue_status_pkey_uidx ON issue_status (id);
`

func fixture(t *testing.T) (*sql.DB, *Service) {
	t.Helper()
	pool := testutil.OpenTestDB(t)
	ctx := context.Background()
	if _, err := pool.ExecContext(ctx, compatIssueStatusDDL); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.ExecContext(ctx, "INSERT INTO schema_migrations(version) VALUES ('478_issue_status_category_expand')"); err != nil {
		t.Fatal(err)
	}
	return pool, NewService(pool, StatusCategory{})
}

func statusID(i int) string { return fmt.Sprintf("00000000-0000-0000-0000-%012d", i) }

func seed(t *testing.T, pool *sql.DB, mode string) {
	t.Helper()
	dbfx := testutil.New(pool, uuid.NewString(), uuid.NewString())
	keys := []string{"backlog", "todo", "in_progress", "in_review", "blocked", "done", "cancelled"}
	for i, key := range keys {
		category := key
		if mode == "new" || (mode == "mixed" && i%2 == 0) {
			category = categoryMapping[key]
		}
		dbfx.Insert(t, "issue_status", testutil.Cols{"id": statusID(i + 1), "workspace_id": dbfx.WorkspaceID,
			"key": key, "name": key, "category": category, "color": "#123456", "is_system": true})
		dbfx.Insert(t, "issue_status", testutil.Cols{"id": statusID(i + 8), "workspace_id": uuid.NewString(),
			"key": "custom_" + key, "name": key, "category": category, "color": "#123456", "archived_at": time.Now().UTC().Format(time.RFC3339Nano)})
	}
}

func request(dry bool) CreateRequest {
	return CreateRequest{Type: StatusCategoryType, Version: 1, Scope: "database", IdempotencyKey: uuid.NewString(),
		DryRun: &dry, Options: Options{BatchSize: 3, DelayMS: 1, LockTimeoutMS: 50, StatementTimeoutMS: 1000},
		Parameters: asJSON(statusParameters{WritersUpgraded: !dry})}
}

func create(t *testing.T, s *Service, dry bool) Job {
	t.Helper()
	j, err := s.Create(context.Background(), request(dry))
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func step(t *testing.T, s *Service, j Job) Job {
	t.Helper()
	for i := 0; i < 5; i++ {
		next, err := s.Mutate(context.Background(), j.ID, j.Revision, "advance")
		if errors.Is(err, ErrThrottled) {
			time.Sleep(2 * time.Millisecond)
			continue
		}
		if err != nil {
			t.Fatalf("advance: %v; job=%+v", err, next)
		}
		return next
	}
	t.Fatal("throttled beyond bounded retry")
	return Job{}
}

func finish(t *testing.T, s *Service, j Job) Job {
	t.Helper()
	for i := 0; i < 30; i++ {
		if j.Status == "completed" {
			return j
		}
		j = step(t, s, j)
	}
	t.Fatal("job failed to terminate")
	return Job{}
}

func snapshot(t *testing.T, pool *sql.DB, excludeCategory bool) string {
	t.Helper()
	rows, err := pool.QueryContext(context.Background(), `SELECT id, workspace_id, key, name, description, category, color, is_system, position, COALESCE(archived_at,''), created_at, updated_at, icon FROM issue_status ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var list []map[string]any
	for rows.Next() {
		var id, ws, k, name, desc, cat, color, arch, ca, ua, icon string
		var sys int
		var pos float64
		if err := rows.Scan(&id, &ws, &k, &name, &desc, &cat, &color, &sys, &pos, &arch, &ca, &ua, &icon); err != nil {
			t.Fatal(err)
		}
		m := map[string]any{
			"id": id, "workspace_id": ws, "key": k, "name": name, "description": desc,
			"color": color, "is_system": sys, "position": pos, "archived_at": arch,
			"created_at": ca, "updated_at": ua, "icon": icon,
		}
		if !excludeCategory {
			m["category"] = cat
		}
		list = append(list, m)
	}
	b, _ := json.Marshal(list)
	return string(b)
}

func TestCategoryBackfillOldMixedNewAndDryRun(t *testing.T) {
	for _, mode := range []string{"old", "mixed", "new"} {
		t.Run(mode, func(t *testing.T) {
			pool, s := fixture(t)
			seed(t, pool, mode)
			before := snapshot(t, pool, false)
			identity := snapshot(t, pool, true)
			dry := finish(t, s, create(t, s, true))
			if snapshot(t, pool, false) != before {
				t.Fatal("dry-run changed catalog")
			}
			var report statusProgress
			if err := decode(dry.Progress, &report); err != nil {
				t.Fatal(err)
			}
			if report.Scanned != 14 || report.System != 7 || report.Archived != 7 || report.Updated != 0 {
				t.Fatalf("dry report: %+v", report)
			}
			j := create(t, s, false)
			for j.Status != "completed" {
				var p statusProgress
				_ = decode(j.Progress, &p)
				next := step(t, s, j)
				var np statusProgress
				_ = decode(next.Progress, &np)
				if np.Scanned-p.Scanned > 3 || np.Updated-p.Updated > 3 || np.Verified-p.Verified > 3 {
					t.Fatal("unbounded batch")
				}
				s = NewService(pool, StatusCategory{})
				j = next
			}
			if snapshot(t, pool, true) != identity {
				t.Fatal("identity, archive, timestamps or business fields changed")
			}
			var remaining int
			if err := pool.QueryRowContext(context.Background(), "SELECT count(*) FROM issue_status WHERE category != issue_status_category(category)").Scan(&remaining); err != nil || remaining != 0 {
				t.Fatalf("remaining=%d: %v", remaining, err)
			}
			var p statusProgress
			_ = decode(j.Progress, &p)
			if p.Verified != 14 || p.Updated != report.WouldUpdate {
				t.Fatalf("progress=%+v dry=%+v", p, report)
			}
			again := finish(t, s, create(t, s, false))
			_ = decode(again.Progress, &p)
			if p.Updated != 0 {
				t.Fatal("second pass rewrote canonical rows")
			}
		})
	}
}

func TestJobIdempotencyVersionPauseAndDatabaseThrottle(t *testing.T) {
	pool, s := fixture(t)
	seed(t, pool, "old")
	ctx := context.Background()
	req := request(false)
	req.Options.DelayMS = 60000
	j, err := s.Create(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	same, err := s.Create(ctx, req)
	if err != nil || same.ID != j.ID {
		t.Fatalf("create retry: %+v %v", same, err)
	}
	different := req
	different.Options.BatchSize++
	if _, err = s.Create(ctx, different); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed duplicate request: %v", err)
	}
	other := request(false)
	if _, err = s.Create(ctx, other); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate active scope: %v", err)
	}
	next := step(t, s, j)
	replay, err := NewService(pool, StatusCategory{}).Mutate(ctx, j.ID, j.Revision, "advance")
	if !errors.Is(err, ErrConflict) || replay.Revision != next.Revision {
		t.Fatalf("lost-response replay advanced: %+v %v", replay, err)
	}
	if _, err = s.Mutate(ctx, j.ID, next.Revision, "advance"); !errors.Is(err, ErrThrottled) {
		t.Fatalf("cross-replica delay: %v", err)
	}
	paused, err := s.Mutate(ctx, j.ID, next.Revision, "pause")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Mutate(ctx, j.ID, paused.Revision, "advance"); !errors.Is(err, ErrConflict) {
		t.Fatalf("paused advanced: %v", err)
	}
	resumed, err := s.Mutate(ctx, j.ID, paused.Revision, "resume")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Mutate(ctx, j.ID, resumed.Revision, "advance"); !errors.Is(err, ErrThrottled) {
		t.Fatal("resume bypassed delay")
	}
	cancelled, err := s.Mutate(ctx, j.ID, resumed.Revision, "cancel")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Mutate(ctx, j.ID, cancelled.Revision, "resume"); !errors.Is(err, ErrConflict) {
		t.Fatal("terminal history reopened")
	}
	if _, err = s.Create(ctx, other); err != nil {
		t.Fatal("new run after cancellation:", err)
	}
}

type wrappedProcessor struct {
	StatusCategory
	hook func(context.Context, *sql.Tx, Job) error
}

func (p wrappedProcessor) Step(ctx context.Context, tx *sql.Tx, j Job) (json.RawMessage, json.RawMessage, json.RawMessage, bool, error) {
	cp, progress, result, done, err := p.StatusCategory.Step(ctx, tx, j)
	if err == nil {
		err = p.hook(ctx, tx, j)
	}
	return cp, progress, result, done, err
}

func TestLockTimeoutRollsBackDataAndCheckpoint(t *testing.T) {
	pool, s := fixture(t)
	seed(t, pool, "old")
	j := create(t, s, false)
	ctx := context.Background()
	before := snapshot(t, pool, false)
	failing := NewService(pool, wrappedProcessor{hook: func(context.Context, *sql.Tx, Job) error {
		return errors.New("simulated lock timeout")
	}})
	failed, err := failing.Mutate(ctx, j.ID, j.Revision, "advance")
	if err == nil || failed.Status != "paused" || failed.LastError == "" {
		t.Fatalf("expected paused timeout: %+v %v", failed, err)
	}
	if string(failed.Checkpoint) != "{}" || string(failed.Progress) != "{}" || snapshot(t, pool, false) != before {
		t.Fatal("partial data/checkpoint committed")
	}
	resumed, err := s.Mutate(ctx, j.ID, failed.Revision, "resume")
	if err != nil {
		t.Fatal(err)
	}
	finish(t, s, resumed)
}

func TestConcurrentReplicaAndConnectionLoss(t *testing.T) {
	pool, s := fixture(t)
	seed(t, pool, "old")
	j := create(t, s, false)
	entered := make(chan struct{})
	release := make(chan struct{})
	crashing := NewService(pool, wrappedProcessor{hook: func(_ context.Context, _ *sql.Tx, _ Job) error {
		close(entered)
		<-release
		return errors.New("connection lost")
	}})
	result := make(chan error, 1)
	go func() { _, err := crashing.Mutate(context.Background(), j.ID, j.Revision, "advance"); result <- err }()
	<-entered
	_, err := NewService(pool, StatusCategory{}).Mutate(context.Background(), j.ID, j.Revision, "advance")
	if !errors.Is(err, ErrBusy) {
		close(release)
		<-result
		t.Fatalf("concurrent replica: %v", err)
	}
	close(release)
	if err := <-result; err == nil {
		t.Fatal("connection loss returned success")
	}
	var category string
	if err := pool.QueryRowContext(context.Background(), "SELECT category FROM issue_status WHERE id=$1", statusID(1)).Scan(&category); err != nil || category != "backlog" {
		t.Fatalf("uncommitted update survived: %s %v", category, err)
	}
}

func TestValidationDetectsResidualBehindCursorAndBadPrerequisites(t *testing.T) {
	pool, s := fixture(t)
	seed(t, pool, "old")
	ctx := context.Background()
	req := request(false)
	req.Parameters = asJSON(statusParameters{})
	if _, err := s.Create(ctx, req); !errors.Is(err, ErrInvalid) {
		t.Fatal("missing writer attestation accepted")
	}
	j := create(t, s, false)
	for {
		var cp statusCheckpoint
		_ = decode(j.Checkpoint, &cp)
		if cp.Phase == "verify" {
			break
		}
		j = step(t, s, j)
	}
	if _, err := pool.ExecContext(ctx, "UPDATE issue_status SET category='backlog' WHERE id=$1", statusID(1)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	failed, err := s.Mutate(ctx, j.ID, j.Revision, "advance")
	if err == nil || failed.Status != "paused" || !strings.Contains(failed.LastError, "legacy category remains") {
		t.Fatalf("false completion: %+v %v", failed, err)
	}
	if _, err := pool.ExecContext(ctx, "UPDATE maintenance_job SET job_version=2 WHERE id=$1", j.ID); err != nil {
		t.Fatal(err)
	}
	resumed, err := s.Mutate(ctx, j.ID, failed.Revision, "resume")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Mutate(ctx, j.ID, resumed.Revision, "advance"); !errors.Is(err, ErrConflict) {
		t.Fatalf("unsupported checkpoint version: %v", err)
	}
}

func TestPreflightChecksActualSchemaAndLedger(t *testing.T) {
	pool, s := fixture(t)
	ctx := context.Background()
	if _, err := pool.ExecContext(ctx, "DELETE FROM schema_migrations"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, request(true)); err == nil {
		t.Fatal("missing 478 accepted")
	}
	if _, err := pool.ExecContext(ctx, "INSERT INTO schema_migrations(version) VALUES ('478_issue_status_category_expand')"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.ExecContext(ctx, `DROP TABLE issue_status; CREATE TABLE issue_status (id TEXT NOT NULL, workspace_id TEXT NOT NULL, key TEXT NOT NULL, name TEXT NOT NULL, description TEXT DEFAULT '' NOT NULL, category TEXT NOT NULL CHECK(category IN ('backlog','todo','in_progress','in_review','blocked','done','cancelled')), color TEXT NOT NULL, is_system INTEGER DEFAULT 0 NOT NULL, position REAL DEFAULT 0 NOT NULL, archived_at TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, icon TEXT DEFAULT '' NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, request(true)); err == nil {
		t.Fatal("ledger hid incompatible CHECK")
	}
}

func TestUnknownStoredDataCannotComplete(t *testing.T) {
	pool, s := fixture(t)
	seed(t, pool, "old")
	ctx := context.Background()
	if _, err := pool.ExecContext(ctx, "PRAGMA ignore_check_constraints = ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.ExecContext(ctx, "UPDATE issue_status SET category='unexpected' WHERE id='00000000-0000-0000-0000-000000000008'"); err != nil {
		t.Fatal(err)
	}
	_, _ = pool.ExecContext(ctx, "PRAGMA ignore_check_constraints = OFF")
	j := create(t, s, true)
	for i := 0; i < 10; i++ {
		time.Sleep(2 * time.Millisecond)
		next, err := s.Mutate(ctx, j.ID, j.Revision, "advance")
		if err != nil {
			if next.Status != "paused" || !strings.Contains(next.LastError, "unknown category") {
				t.Fatalf("unexpected error: %+v %v", next, err)
			}
			return
		}
		j = next
	}
	t.Fatal("invalid data accepted")
}

func TestBoundedKeysetUsesExistingIndex(t *testing.T) {
	pool, _ := fixture(t)
	ctx := context.Background()
	rows, err := pool.QueryContext(ctx, `EXPLAIN QUERY PLAN SELECT id,key,category,is_system,archived_at IS NOT NULL
 FROM issue_status WHERE id>'80000000-0000-0000-0000-000000000000' ORDER BY issue_status.id LIMIT 100`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(detail + "\n")
	}
	if !strings.Contains(plan.String(), "issue_status_pkey_uidx") {
		t.Fatalf("keyset page did not use bounded index access: %s", plan.String())
	}
}

type counterProcessor struct{}

func (counterProcessor) Type() string { return "example_repair" }
func (counterProcessor) Version() int { return 2 }
func (counterProcessor) Validate(scope string, dry bool, p json.RawMessage) (json.RawMessage, error) {
	if scope != "workspace:example" {
		return nil, ErrInvalid
	}
	var params struct {
		Label string `json:"label"`
	}
	if err := decode(p, &params); err != nil {
		return nil, err
	}
	return asJSON(params), nil
}
func (counterProcessor) Preflight(context.Context, *sql.Tx) error { return nil }
func (counterProcessor) Step(ctx context.Context, tx *sql.Tx, j Job) (json.RawMessage, json.RawMessage, json.RawMessage, bool, error) {
	return asJSON(map[string]any{"timestamp": "2026-09-15T00:00:00Z", "sequence": 4}),
		asJSON(map[string]int{"repaired": 4}), asJSON(map[string]bool{"verified": true}), true, nil
}

func TestSharedTableSupportsDifferentProcessorAndCursor(t *testing.T) {
	pool, s := fixture(t)
	category := create(t, s, true)
	service := NewService(pool, StatusCategory{}, counterProcessor{})
	j, err := service.Create(context.Background(), CreateRequest{Type: "example_repair", Version: 2, Scope: "workspace:example",
		IdempotencyKey: uuid.NewString(), Parameters: asJSON(map[string]string{"label": "example"})})
	if err != nil {
		t.Fatal(err)
	}
	j = step(t, service, j)
	if j.Status != "completed" || !strings.Contains(string(j.Checkpoint), "timestamp") {
		t.Fatalf("generic processor failed: %+v", j)
	}
	unchanged, err := s.Get(context.Background(), category.ID)
	if err != nil || unchanged.Revision != 0 {
		t.Fatal("other job was modified")
	}
}

func TestContractPreservesAuditAndRejectsCategoryProcessor(t *testing.T) {
	pool, s := fixture(t)
	seed(t, pool, "old")
	ctx := context.Background()
	completed := finish(t, s, create(t, s, false))
	pending := create(t, s, false)
	if _, err := pool.ExecContext(ctx, `DROP TABLE issue_status; CREATE TABLE issue_status (id TEXT NOT NULL, workspace_id TEXT NOT NULL, key TEXT NOT NULL, name TEXT NOT NULL, description TEXT DEFAULT '' NOT NULL, category TEXT NOT NULL CHECK(category IN ('unstarted','started','done','closed')), color TEXT NOT NULL, is_system INTEGER DEFAULT 0 NOT NULL, position REAL DEFAULT 0 NOT NULL, archived_at TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, icon TEXT DEFAULT '' NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	audit, err := s.Get(ctx, completed.ID)
	if err != nil || string(audit.Result) != string(completed.Result) || audit.Status != "completed" || audit.Revision != completed.Revision {
		t.Fatalf("audit changed: %+v %v", audit, err)
	}
	if _, err := s.Create(ctx, request(true)); err == nil {
		t.Fatal("contracted schema accepted v1 category job")
	}
	failed, err := s.Mutate(ctx, pending.ID, pending.Revision, "advance")
	if err == nil || failed.Status != "paused" {
		t.Fatalf("pre-contract pending job did not fail closed: %+v %v", failed, err)
	}
}
