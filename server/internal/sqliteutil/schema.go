package sqliteutil

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
)

// ApplySchema executes the SQLite script at path statement by statement.
// database/sql Exec runs a single statement, so scripts are split on
// semicolons that are outside strings and trigger BEGIN/END bodies.
// A missing file is an error; this function does not synthesize a schema.
func ApplySchema(ctx context.Context, db *sql.DB, path string) error {
	if db == nil {
		return errors.New("sqlite db is nil")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("sqlite schema path is empty")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("sqlite schema file not found at %q (expected server/schema/sqlite.sql; set MULTICA_SCHEMA_PATH to override)", path)
		}
		return fmt.Errorf("read sqlite schema %q: %w", path, err)
	}
	stmts, err := splitSQL(string(body))
	if err != nil {
		return fmt.Errorf("split sqlite schema %q: %w", path, err)
	}
	for i, stmt := range stmts {
		stmt = strings.Replace(stmt, "\n    id TEXT NOT NULL,\n", "\n    id TEXT DEFAULT (gen_random_uuid()) NOT NULL,\n", 1)
		stmt = strings.ReplaceAll(stmt, "strftime('%Y-%m-%dT%H:%M:%fZ', 'now')", "now()")
		stmt = strings.ReplaceAll(stmt, "strftime('%Y-%m-%dT%H:%M:%fZ', ", "datetime(")
		for _, col := range []string{"generation", "claim_token", "lease_token", "instance_id"} {
			stmt = strings.ReplaceAll(stmt, "\n    "+col+" TEXT NOT NULL", "\n    "+col+" TEXT DEFAULT (gen_random_uuid()) NOT NULL")
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("apply sqlite schema %q statement %d: %w", path, i+1, err)
		}
	}

	extraTriggers := []string{
		`CREATE TRIGGER IF NOT EXISTS agent_task_comment_thread_ai
AFTER INSERT ON agent_task_queue
BEGIN
    UPDATE agent_task_queue
    SET comment_thread_id = COALESCE(
        NULLIF(json_extract(NEW.context, '$.wakeup_id'), ''),
        (CASE WHEN NEW.trigger_comment_id IS NULL THEN NULL ELSE COALESCE((WITH RECURSIVE ancestors(id, parent_id, issue_id, depth) AS (SELECT c.id, c.parent_id, c.issue_id, 1 FROM comment c WHERE c.id = NEW.trigger_comment_id UNION ALL SELECT p.id, p.parent_id, p.issue_id, a.depth + 1 FROM ancestors a JOIN comment p ON p.id = a.parent_id AND p.issue_id = a.issue_id WHERE a.depth < 32) SELECT id FROM ancestors ORDER BY depth DESC LIMIT 1), NEW.trigger_comment_id) END)
    )
    WHERE id = NEW.id;
END`,
		`CREATE TRIGGER IF NOT EXISTS agent_task_comment_thread_au
AFTER UPDATE OF trigger_comment_id, context ON agent_task_queue
BEGIN
    UPDATE agent_task_queue
    SET comment_thread_id = COALESCE(
        NULLIF(json_extract(NEW.context, '$.wakeup_id'), ''),
        (CASE WHEN NEW.trigger_comment_id IS NULL THEN NULL ELSE COALESCE((WITH RECURSIVE ancestors(id, parent_id, issue_id, depth) AS (SELECT c.id, c.parent_id, c.issue_id, 1 FROM comment c WHERE c.id = NEW.trigger_comment_id UNION ALL SELECT p.id, p.parent_id, p.issue_id, a.depth + 1 FROM ancestors a JOIN comment p ON p.id = a.parent_id AND p.issue_id = a.issue_id WHERE a.depth < 32) SELECT id FROM ancestors ORDER BY depth DESC LIMIT 1), NEW.trigger_comment_id) END)
    )
    WHERE id = NEW.id;
END`,
		`CREATE TRIGGER IF NOT EXISTS trg_issue_delete_cascade
AFTER DELETE ON issue
BEGIN
    DELETE FROM inbox_item WHERE issue_id = OLD.id;
    DELETE FROM comment WHERE issue_id = OLD.id;
    DELETE FROM issue_subscriber WHERE issue_id = OLD.id;
    DELETE FROM issue_to_label WHERE issue_id = OLD.id;
    DELETE FROM agent_task_queue WHERE issue_id = OLD.id;
END`,
		`CREATE TRIGGER IF NOT EXISTS trg_issue_child_event_insert
AFTER INSERT ON issue
WHEN NEW.parent_issue_id IS NOT NULL
BEGIN
    INSERT INTO issue_child_event(workspace_id, parent_id, child_id, kind, source_task_id)
    VALUES(NEW.workspace_id, NEW.parent_issue_id, NEW.id, 'attached', NULLIF(current_setting('multica.source_task_id', 1), ''));
END`,
		`CREATE TRIGGER IF NOT EXISTS trg_issue_child_event_update
AFTER UPDATE OF status, parent_issue_id, stage ON issue
BEGIN
    INSERT INTO issue_child_event(workspace_id, parent_id, child_id, kind, source_task_id)
    SELECT OLD.workspace_id, OLD.parent_issue_id, OLD.id, 'detached', NULLIF(current_setting('multica.source_task_id', 1), '')
    WHERE (OLD.parent_issue_id IS NOT NEW.parent_issue_id) AND OLD.parent_issue_id IS NOT NULL;

    INSERT INTO issue_child_event(workspace_id, parent_id, child_id, kind, source_task_id)
    SELECT NEW.workspace_id, NEW.parent_issue_id, NEW.id, 'attached', NULLIF(current_setting('multica.source_task_id', 1), '')
    WHERE (OLD.parent_issue_id IS NOT NEW.parent_issue_id) AND NEW.parent_issue_id IS NOT NULL;

    INSERT INTO issue_child_event(workspace_id, parent_id, child_id, kind, source_task_id)
    SELECT NEW.workspace_id, NEW.parent_issue_id, NEW.id, 'closed', NULLIF(current_setting('multica.source_task_id', 1), '')
    WHERE (OLD.parent_issue_id IS NEW.parent_issue_id) AND NEW.parent_issue_id IS NOT NULL
      AND (OLD.status IS NOT NEW.status)
      AND (NEW.status IN ('done','cancelled') OR EXISTS(SELECT 1 FROM issue_status s WHERE s.workspace_id=NEW.workspace_id AND s.key=NEW.status AND s.category IN ('done','closed')))
      AND NOT (OLD.status IN ('done','cancelled') OR EXISTS(SELECT 1 FROM issue_status s WHERE s.workspace_id=OLD.workspace_id AND s.key=OLD.status AND s.category IN ('done','closed')));

    INSERT INTO issue_child_event(workspace_id, parent_id, child_id, kind, source_task_id)
    SELECT NEW.workspace_id, NEW.parent_issue_id, NEW.id, 'reopened', NULLIF(current_setting('multica.source_task_id', 1), '')
    WHERE (OLD.parent_issue_id IS NEW.parent_issue_id) AND NEW.parent_issue_id IS NOT NULL
      AND (OLD.status IS NOT NEW.status)
      AND NOT (NEW.status IN ('done','cancelled') OR EXISTS(SELECT 1 FROM issue_status s WHERE s.workspace_id=NEW.workspace_id AND s.key=NEW.status AND s.category IN ('done','closed')))
      AND (OLD.status IN ('done','cancelled') OR EXISTS(SELECT 1 FROM issue_status s WHERE s.workspace_id=OLD.workspace_id AND s.key=OLD.status AND s.category IN ('done','closed')));

    INSERT INTO issue_child_event(workspace_id, parent_id, child_id, kind, source_task_id)
    SELECT NEW.workspace_id, NEW.parent_issue_id, NEW.id, 'restaged', NULLIF(current_setting('multica.source_task_id', 1), '')
    WHERE (OLD.parent_issue_id IS NEW.parent_issue_id) AND NEW.parent_issue_id IS NOT NULL
      AND (OLD.stage IS NOT NEW.stage);
END`,
	}
	if strings.Contains(string(body), "CREATE TABLE agent_task_queue") {
		for _, trig := range extraTriggers {
			if _, err := db.ExecContext(ctx, trig); err != nil {
				return fmt.Errorf("apply extra sqlite trigger: %w", err)
			}
		}
	}
	return nil
}
