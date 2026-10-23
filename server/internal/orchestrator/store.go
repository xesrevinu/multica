package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"database/sql"
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

type store struct{ pool *sql.DB }

func parseSQLTime(s string) (time.Time, error) {
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse time %q", s)
}

type sqlTime struct{ dst *time.Time }

func (t sqlTime) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*t.dst = time.Time{}
		return nil
	case time.Time:
		*t.dst = v.UTC()
		return nil
	case string:
		if v == "" {
			*t.dst = time.Time{}
			return nil
		}
		parsed, err := parseSQLTime(v)
		if err != nil {
			return err
		}
		*t.dst = parsed
		return nil
	case []byte:
		return t.Scan(string(v))
	default:
		return fmt.Errorf("unsupported time scan type %T", src)
	}
}

type sqlTimePtr struct{ dst **time.Time }

func (t sqlTimePtr) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*t.dst = nil
		return nil
	case time.Time:
		utc := v.UTC()
		*t.dst = &utc
		return nil
	case string:
		if v == "" {
			*t.dst = nil
			return nil
		}
		parsed, err := parseSQLTime(v)
		if err != nil {
			return err
		}
		*t.dst = &parsed
		return nil
	case []byte:
		return t.Scan(string(v))
	default:
		return fmt.Errorf("unsupported time ptr scan type %T", src)
	}
}

type sqlLabels struct{ dst *[]string }

func (l sqlLabels) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*l.dst = nil
		return nil
	case string:
		if v == "" || v == "[]" || v == "{}" {
			*l.dst = nil
			return nil
		}
		var out []string
		if err := json.Unmarshal([]byte(v), &out); err != nil {
			return err
		}
		*l.dst = out
		return nil
	case []byte:
		return l.Scan(string(v))
	default:
		return fmt.Errorf("unsupported labels scan type %T", src)
	}
}

type sqlRawJSON struct{ dst *json.RawMessage }

func (r sqlRawJSON) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*r.dst = nil
		return nil
	case string:
		*r.dst = json.RawMessage(v)
		return nil
	case []byte:
		b := make([]byte, len(v))
		copy(b, v)
		*r.dst = json.RawMessage(b)
		return nil
	default:
		return fmt.Errorf("unsupported raw json scan type %T", src)
	}
}

func (s *store) teams(ctx context.Context) ([]*Team, error) {
	rows, err := s.pool.QueryContext(ctx, `SELECT project_id, workspace_id, spec, spec_yaml, paused, paused_reason, COALESCE(human_user_id,''), updated_at FROM orchestrator_team`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Team
	for rows.Next() {
		t := &Team{}
		var raw []byte
		if err := rows.Scan(&t.ProjectID, &t.WorkspaceID, &raw, &t.SpecYAML, &t.Paused, &t.PausedReason, &t.HumanUserID, sqlTime{&t.UpdatedAt}); err != nil {
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

const issueCols = `i.id, w.issue_prefix || '-' || i.number, i.title, i.status, COALESCE(i.parent_issue_id,''), COALESCE(i.stage,0),
	COALESCE(i.assignee_type,''), COALESCE(CASE WHEN i.assignee_type='agent' THEN a.name ELSE i.assignee_id END,''),
	COALESCE((SELECT json_group_array(l.name) FROM issue_to_label il JOIN issue_label l ON l.id=il.label_id WHERE il.issue_id=i.id), '[]'),
	i.metadata, COALESCE(i.description,''), i.updated_at, i.revision, COALESCE(i.project_id,''), i.workspace_id`

const issueFrom = ` FROM issue i JOIN workspace w ON w.id=i.workspace_id LEFT JOIN agent a ON a.id=i.assignee_id AND i.assignee_type='agent' `

type issueRow struct {
	Issue
	Revision             int64
	ProjectID, Workspace string
}

func scanIssues(rows *sql.Rows) ([]issueRow, error) {
	defer rows.Close()
	var out []issueRow
	for rows.Next() {
		var r issueRow
		var raw []byte
		if err := rows.Scan(&r.ID, &r.Key, &r.Title, &r.Status, &r.ParentID, &r.Stage, &r.AssigneeType, &r.Assignee, sqlLabels{&r.Labels}, &raw, &r.Description, sqlTime{&r.UpdatedAt}, &r.Revision, &r.ProjectID, &r.Workspace); err != nil {
			return nil, err
		}
		r.Meta = metaStrings(raw)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *store) projectIssues(ctx context.Context, projectID string) ([]issueRow, error) {
	rows, err := s.pool.QueryContext(ctx, `SELECT `+issueCols+issueFrom+`WHERE i.project_id=$1 ORDER BY i.number`, projectID)
	if err != nil {
		return nil, err
	}
	return scanIssues(rows)
}

func (s *store) issue(ctx context.Context, id string) (*issueRow, error) {
	rows, err := s.pool.QueryContext(ctx, `SELECT `+issueCols+issueFrom+`WHERE i.id=$1`, id)
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
	q := `SELECT ` + issueCols + issueFrom + `WHERE i.workspace_id=$1 AND (i.id=$2 OR upper(w.issue_prefix || '-' || i.number)=upper($2))`
	rows, err := s.pool.QueryContext(ctx, q, workspaceID, key)
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

// busyOwners: owner and expert agents with todo/in_progress issues anywhere
// in the workspace. The query is every assigned agent; Assign only consults
// names that are owners or experts, so other agents in the set are ignored.
func (s *store) busyOwners(ctx context.Context, workspaceID string) (map[string]bool, error) {
	rows, err := s.pool.QueryContext(ctx, `SELECT DISTINCT a.name FROM issue i JOIN agent a ON a.id=i.assignee_id WHERE i.workspace_id=$1 AND i.assignee_type='agent' AND i.status IN ('todo','in_progress')`, workspaceID)
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
	rows, err := s.pool.QueryContext(ctx, `SELECT q.id, q.status, q.started_at, (SELECT max(m.created_at) FROM task_message m WHERE m.task_id=q.id), COALESCE(q.failure_reason, ''), q.created_at, q.completed_at, COALESCE((SELECT lower(rt.provider) FROM agent_runtime rt WHERE rt.id=q.runtime_id), ''), COALESCE(q.error, '') FROM agent_task_queue q WHERE q.issue_id=$1 ORDER BY q.created_at DESC LIMIT 20`, issueID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		r := &Run{}
		if err := rows.Scan(&r.ID, &r.Status, sqlTimePtr{&r.StartedAt}, sqlTimePtr{&r.LastActivity}, &r.Reason, sqlTimePtr{&r.CreatedAt}, sqlTimePtr{&r.EndedAt}, &r.Backend, &r.Error); err != nil {
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

func (s *store) activeRunIDs(ctx context.Context, issueID string) ([]string, error) {
	rows, err := s.pool.QueryContext(ctx, `SELECT id FROM agent_task_queue WHERE issue_id=$1 AND status NOT IN ('completed','failed','cancelled') ORDER BY created_at DESC`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *store) agentID(ctx context.Context, workspaceID, name string) (string, error) {
	var id string
	err := s.pool.QueryRowContext(ctx, `SELECT id FROM agent WHERE workspace_id=$1 AND name=$2 AND archived_at IS NULL LIMIT 1`, workspaceID, name).Scan(&id)
	if err == sql.ErrNoRows {
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
	tx, err := s.pool.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()
	dj, _ := json.Marshal(d)
	var state string
	err = tx.QueryRowContext(ctx, `INSERT INTO orchestrator_transition (workspace_id, project_id, issue_id, tid, idem_key, decision, state, source)
		VALUES ($1,$2,$3,$4,$5,$6,'started',$7)
		ON CONFLICT (idem_key) DO UPDATE SET idem_key=EXCLUDED.idem_key RETURNING id, state`,
		t.WorkspaceID, t.ProjectID, row.ID, d.Tid, idem, string(dj), source).Scan(&logID, &state)
	if err != nil {
		return 0, false, err
	}
	if state == "done" {
		return logID, false, nil
	}
	if len(d.Meta) > 0 {
		patch := map[string]any{}
		for k, v := range d.Meta {
			if v == "" {
				patch[k] = nil
			} else if n, e := strconv.Atoi(v); e == nil && strings.HasPrefix(k, "attempt_") {
				patch[k] = n
			} else {
				patch[k] = v
			}
		}
		sj, _ := json.Marshal(patch)
		if _, err := tx.ExecContext(ctx, `UPDATE issue SET metadata = json_patch(COALESCE(metadata, '{}'), $2), revision = revision + 1, updated_at = now() WHERE id=$1`, row.ID, string(sj)); err != nil {
			return 0, false, err
		}
	}
	return logID, true, tx.Commit()
}

func (s *store) finish(ctx context.Context, id int64, err error) {
	state, msg := "done", ""
	if err != nil {
		state, msg = "failed", err.Error()
	}
	_, _ = s.pool.ExecContext(ctx, `UPDATE orchestrator_transition SET state=$2, error=$3, finished_at=now() WHERE id=$1`, id, state, msg)
}

// --- failures / backoff -----------------------------------------------------

func (s *store) backoffOK(ctx context.Context, issueID string, now time.Time) bool {
	var next *time.Time
	_ = s.pool.QueryRowContext(ctx, `SELECT next_at FROM orchestrator_issue_state WHERE issue_id=$1`, issueID).Scan(sqlTimePtr{&next})
	return next == nil || !next.After(now)
}

func (s *store) recordFailure(ctx context.Context, issueID string, err error, base, capS time.Duration) (int, time.Duration) {
	var n int
	_ = s.pool.QueryRowContext(ctx, `INSERT INTO orchestrator_issue_state (issue_id, failures, last_error) VALUES ($1,1,$2)
		ON CONFLICT (issue_id) DO UPDATE SET failures = orchestrator_issue_state.failures+1, last_error=$2, updated_at=now() RETURNING failures`, issueID, short(err.Error(), 500)).Scan(&n)
	wait := base << (n - 1)
	if wait > capS || wait <= 0 {
		wait = capS
	}
	_, _ = s.pool.ExecContext(ctx, `UPDATE orchestrator_issue_state SET next_at = $2 WHERE issue_id=$1`, issueID, time.Now().UTC().Add(wait).Format(time.RFC3339Nano))
	return n, wait
}

func (s *store) clearFailure(ctx context.Context, issueID string) {
	_, _ = s.pool.ExecContext(ctx, `UPDATE orchestrator_issue_state SET failures=0, next_at=NULL, last_error='', last_wait='' WHERE issue_id=$1 AND (failures>0 OR last_wait<>'')`, issueID)
}

func (s *store) setWait(ctx context.Context, issueID, wait string) bool {
	tag, _ := s.pool.ExecContext(ctx, `INSERT INTO orchestrator_issue_state (issue_id, last_wait) VALUES ($1,$2)
		ON CONFLICT (issue_id) DO UPDATE SET last_wait=$2 WHERE orchestrator_issue_state.last_wait IS DISTINCT FROM $2`, issueID, wait)
	return rowsAffected(tag) > 0
}

func (s *store) remindDue(ctx context.Context, issueID string, every time.Duration) bool {
	tag, _ := s.pool.ExecContext(ctx, `INSERT INTO orchestrator_issue_state (issue_id, reminded_at) VALUES ($1, now())
		ON CONFLICT (issue_id) DO UPDATE SET reminded_at=now()
		WHERE orchestrator_issue_state.reminded_at IS NULL OR orchestrator_issue_state.reminded_at < $2`, issueID, time.Now().UTC().Add(-every).Format(time.RFC3339Nano))
	return rowsAffected(tag) > 0
}

// --- event queue ------------------------------------------------------------

func (s *store) enqueue(ctx context.Context, issueID, kind string) error {
	_, err := s.pool.ExecContext(ctx, `INSERT INTO orchestrator_event (issue_id, kind) SELECT $1, $2 WHERE EXISTS (SELECT 1 FROM issue i JOIN orchestrator_team t ON t.project_id=i.project_id WHERE i.id=$1)`, issueID, kind)
	return err
}

// claim marks all pending events processed and returns their distinct issues.
func (s *store) claim(ctx context.Context) ([]string, error) {
	rows, err := s.pool.QueryContext(ctx, `UPDATE orchestrator_event SET processed_at=now() WHERE processed_at IS NULL RETURNING issue_id`)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	rowsErr := rows.Err()
	rows.Close()
	cutoff := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339Nano)
	_, _ = s.pool.ExecContext(ctx, `DELETE FROM orchestrator_event WHERE processed_at < $1`, cutoff)
	return out, rowsErr
}

// shadow records a decision a shadow team would have applied, once per
// (issue, transition, revision). It reports whether the row is new.
func (s *store) shadow(ctx context.Context, t *Team, row *issueRow, d *Decision, source string) bool {
	dj, _ := json.Marshal(d)
	tag, err := s.pool.ExecContext(ctx, `INSERT INTO orchestrator_transition (workspace_id, project_id, issue_id, tid, idem_key, decision, state, source, finished_at)
		VALUES ($1,$2,$3,$4,$5,$6,'shadow',$7,now()) ON CONFLICT (idem_key) DO NOTHING`,
		t.WorkspaceID, t.ProjectID, row.ID, d.Tid, fmt.Sprintf("shadow|%s|%s|%d", row.ID, d.Tid, row.Revision), string(dj), source)
	return err == nil && rowsAffected(tag) > 0
}

func rowsAffected(tag sql.Result) int64 {
	if tag == nil {
		return 0
	}
	n, _ := tag.RowsAffected()
	return n
}
