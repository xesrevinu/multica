package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Team is one orchestrated project.
type Team struct {
	ProjectID, WorkspaceID string
	Spec                   *Spec
	SpecYAML               string
	Paused                 bool
	PausedReason           string
	HumanUserID            string
	UpdatedAt              time.Time
}

type store struct{ pool *pgxpool.Pool }

func (s *store) teams(ctx context.Context) ([]*Team, error) {
	rows, err := s.pool.Query(ctx, `SELECT project_id::text, workspace_id::text, spec, spec_yaml, paused, paused_reason, COALESCE(human_user_id::text,''), updated_at FROM orchestrator_team`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Team
	for rows.Next() {
		t := &Team{}
		var raw []byte
		if err := rows.Scan(&t.ProjectID, &t.WorkspaceID, &raw, &t.SpecYAML, &t.Paused, &t.PausedReason, &t.HumanUserID, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.Spec = &Spec{}
		if err := json.Unmarshal(raw, t.Spec); err != nil {
			return nil, fmt.Errorf("team %s: bad spec: %w", t.ProjectID, err)
		}
		t.Spec.Defaults()
		out = append(out, t)
	}
	return out, rows.Err()
}

func metaStrings(raw []byte) map[string]string {
	m := map[string]any{}
	_ = json.Unmarshal(raw, &m)
	out := make(map[string]string, len(m))
	for k, v := range m {
		switch x := v.(type) {
		case string:
			out[k] = x
		case float64:
			out[k] = strconv.FormatFloat(x, 'f', -1, 64)
		case bool:
			out[k] = strconv.FormatBool(x)
		case nil:
		default:
			b, _ := json.Marshal(x)
			out[k] = string(b)
		}
	}
	return out
}

const issueCols = `i.id::text, w.issue_prefix || '-' || i.number, i.title, i.status, COALESCE(i.parent_issue_id::text,''), COALESCE(i.stage,0),
	COALESCE(i.assignee_type,''), COALESCE(CASE WHEN i.assignee_type='agent' THEN a.name ELSE i.assignee_id::text END,''),
	COALESCE((SELECT array_agg(l.name) FROM issue_to_label il JOIN issue_label l ON l.id=il.label_id WHERE il.issue_id=i.id), '{}'),
	i.metadata, i.updated_at, i.revision, COALESCE(i.project_id::text,''), i.workspace_id::text`

const issueFrom = ` FROM issue i JOIN workspace w ON w.id=i.workspace_id LEFT JOIN agent a ON a.id=i.assignee_id AND i.assignee_type='agent' `

type issueRow struct {
	Issue
	Revision             int64
	ProjectID, Workspace string
}

func scanIssues(rows pgx.Rows) ([]issueRow, error) {
	defer rows.Close()
	var out []issueRow
	for rows.Next() {
		var r issueRow
		var raw []byte
		if err := rows.Scan(&r.ID, &r.Key, &r.Title, &r.Status, &r.ParentID, &r.Stage, &r.AssigneeType, &r.Assignee, &r.Labels, &raw, &r.UpdatedAt, &r.Revision, &r.ProjectID, &r.Workspace); err != nil {
			return nil, err
		}
		r.Meta = metaStrings(raw)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *store) projectIssues(ctx context.Context, projectID string) ([]issueRow, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+issueCols+issueFrom+`WHERE i.project_id=$1 ORDER BY i.number`, projectID)
	if err != nil {
		return nil, err
	}
	return scanIssues(rows)
}

func (s *store) issue(ctx context.Context, id string) (*issueRow, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+issueCols+issueFrom+`WHERE i.id=$1`, id)
	if err != nil {
		return nil, err
	}
	r, err := scanIssues(rows)
	if err != nil || len(r) == 0 {
		return nil, err
	}
	return &r[0], nil
}

// issueByKey resolves "ATT-12" or a UUID inside a workspace.
func (s *store) issueByKey(ctx context.Context, workspaceID, key string) (*issueRow, error) {
	q := `SELECT ` + issueCols + issueFrom + `WHERE i.workspace_id=$1 AND (i.id::text=$2 OR upper(w.issue_prefix || '-' || i.number)=upper($2))`
	rows, err := s.pool.Query(ctx, q, workspaceID, key)
	if err != nil {
		return nil, err
	}
	r, err := scanIssues(rows)
	if err != nil {
		return nil, err
	}
	if len(r) == 0 {
		return nil, fmt.Errorf("issue %s not found", key)
	}
	return &r[0], nil
}

// busyOwners: agents with todo/in_progress issues anywhere in the workspace.
func (s *store) busyOwners(ctx context.Context, workspaceID string) (map[string]bool, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT a.name FROM issue i JOIN agent a ON a.id=i.assignee_id WHERE i.workspace_id=$1 AND i.assignee_type='agent' AND i.status IN ('todo','in_progress')`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out[n] = true
	}
	return out, rows.Err()
}

var terminalTask = []string{"completed", "failed", "cancelled"}

func (s *store) runs(ctx context.Context, issueID string) (active, last *Run, err error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text, status, started_at FROM agent_task_queue WHERE issue_id=$1 ORDER BY created_at DESC LIMIT 20`, issueID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		r := &Run{}
		if err := rows.Scan(&r.ID, &r.Status, &r.StartedAt); err != nil {
			return nil, nil, err
		}
		if contains(terminalTask, r.Status) {
			if last == nil {
				last = r
			}
		} else if active == nil {
			active = r
		}
	}
	return active, last, rows.Err()
}

func (s *store) agentID(ctx context.Context, workspaceID, name string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `SELECT id::text FROM agent WHERE workspace_id=$1 AND name=$2 AND archived_at IS NULL LIMIT 1`, workspaceID, name).Scan(&id)
	if err == pgx.ErrNoRows {
		return "", fmt.Errorf("agent %q not found in workspace", name)
	}
	return id, err
}

// --- transition log ---------------------------------------------------------

// begin records the transition as started inside tx and merges its metadata,
// atomically. done=false with nil error means the idempotency key already
// completed (skip).
func (s *store) begin(ctx context.Context, t *Team, row *issueRow, d *Decision, source string) (logID int64, fresh bool, err error) {
	idem := fmt.Sprintf("%s|%s|%d", row.ID, d.Tid, row.Revision)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback(ctx)
	dj, _ := json.Marshal(d)
	var state string
	err = tx.QueryRow(ctx, `INSERT INTO orchestrator_transition (workspace_id, project_id, issue_id, tid, idem_key, decision, state, source)
		VALUES ($1,$2,$3,$4,$5,$6,'started',$7)
		ON CONFLICT (idem_key) DO UPDATE SET idem_key=EXCLUDED.idem_key RETURNING id, state`,
		t.WorkspaceID, t.ProjectID, row.ID, d.Tid, idem, dj, source).Scan(&logID, &state)
	if err != nil {
		return 0, false, err
	}
	if state == "done" {
		return logID, false, nil
	}
	if len(d.Meta) > 0 {
		set, del := map[string]any{}, []string{}
		for k, v := range d.Meta {
			if v == "" {
				del = append(del, k)
			} else if n, e := strconv.Atoi(v); e == nil && strings.HasPrefix(k, "attempt_") {
				set[k] = n
			} else {
				set[k] = v
			}
		}
		sj, _ := json.Marshal(set)
		if _, err := tx.Exec(ctx, `UPDATE issue SET metadata = (metadata || $2::jsonb) - $3::text[], revision = revision + 1, updated_at = now() WHERE id=$1`, row.ID, sj, del); err != nil {
			return 0, false, err
		}
	}
	return logID, true, tx.Commit(ctx)
}

func (s *store) finish(ctx context.Context, id int64, err error) {
	state, msg := "done", ""
	if err != nil {
		state, msg = "failed", err.Error()
	}
	_, _ = s.pool.Exec(ctx, `UPDATE orchestrator_transition SET state=$2, error=$3, finished_at=now() WHERE id=$1`, id, state, msg)
}

// --- failures / backoff -----------------------------------------------------

func (s *store) backoffOK(ctx context.Context, issueID string, now time.Time) bool {
	var next *time.Time
	_ = s.pool.QueryRow(ctx, `SELECT next_at FROM orchestrator_issue_state WHERE issue_id=$1`, issueID).Scan(&next)
	return next == nil || !next.After(now)
}

func (s *store) recordFailure(ctx context.Context, issueID string, err error, base, capS time.Duration) (int, time.Duration) {
	var n int
	_ = s.pool.QueryRow(ctx, `INSERT INTO orchestrator_issue_state (issue_id, failures, last_error) VALUES ($1,1,$2)
		ON CONFLICT (issue_id) DO UPDATE SET failures = orchestrator_issue_state.failures+1, last_error=$2, updated_at=now() RETURNING failures`, issueID, short(err.Error(), 500)).Scan(&n)
	wait := base << (n - 1)
	if wait > capS || wait <= 0 {
		wait = capS
	}
	_, _ = s.pool.Exec(ctx, `UPDATE orchestrator_issue_state SET next_at = now() + $2::interval WHERE issue_id=$1`, issueID, fmt.Sprintf("%d milliseconds", wait.Milliseconds()))
	return n, wait
}

func (s *store) clearFailure(ctx context.Context, issueID string) {
	_, _ = s.pool.Exec(ctx, `UPDATE orchestrator_issue_state SET failures=0, next_at=NULL, last_error='' WHERE issue_id=$1 AND failures>0`, issueID)
}

func (s *store) setWait(ctx context.Context, issueID, wait string) bool {
	tag, _ := s.pool.Exec(ctx, `INSERT INTO orchestrator_issue_state (issue_id, last_wait) VALUES ($1,$2)
		ON CONFLICT (issue_id) DO UPDATE SET last_wait=$2 WHERE orchestrator_issue_state.last_wait IS DISTINCT FROM $2`, issueID, wait)
	return tag.RowsAffected() > 0
}

func (s *store) remindDue(ctx context.Context, issueID string, every time.Duration) bool {
	tag, _ := s.pool.Exec(ctx, `INSERT INTO orchestrator_issue_state (issue_id, reminded_at) VALUES ($1, now())
		ON CONFLICT (issue_id) DO UPDATE SET reminded_at=now()
		WHERE orchestrator_issue_state.reminded_at IS NULL OR orchestrator_issue_state.reminded_at < now() - $2::interval`, issueID, fmt.Sprintf("%d seconds", int(every.Seconds())))
	return tag.RowsAffected() > 0
}

// --- event queue ------------------------------------------------------------

func (s *store) enqueue(ctx context.Context, issueID, kind string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO orchestrator_event (issue_id, kind) SELECT $1, $2 WHERE EXISTS (SELECT 1 FROM issue i JOIN orchestrator_team t ON t.project_id=i.project_id WHERE i.id=$1)`, issueID, kind)
	return err
}

// claim marks all pending events processed and returns their distinct issues.
func (s *store) claim(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `UPDATE orchestrator_event SET processed_at=now() WHERE processed_at IS NULL RETURNING issue_id::text`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	_, _ = s.pool.Exec(ctx, `DELETE FROM orchestrator_event WHERE processed_at < now() - interval '1 day'`)
	return out, rows.Err()
}
