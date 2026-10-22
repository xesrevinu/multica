-- name: ListProjects :many
SELECT * FROM project
WHERE workspace_id = ?1
  AND (sqlc.narg('status') IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('priority') IS NULL OR priority = sqlc.narg('priority'))
ORDER BY created_at DESC;

-- name: GetProjectInWorkspace :one
SELECT * FROM project
WHERE id = ?1 AND workspace_id = ?2;

-- name: LockProjectForChatSessionCreate :one
-- Conflicts with project deletion so a chat session cannot commit a soft
-- project reference after the delete transaction has swept existing sessions.
SELECT id FROM project
WHERE id = ?1 AND workspace_id = ?2;

-- name: LockProjectForDelete :one
-- Serializes project deletion with chat-session creation. The handler locks,
-- clears every soft chat reference, and deletes the project in one transaction.
SELECT id FROM project
WHERE id = ?1 AND workspace_id = ?2;

-- name: CreateProject :one
INSERT INTO project (
    workspace_id, title, description, icon, status,
    lead_type, lead_id, priority, start_date, due_date, workflow_id
) VALUES (
    ?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11
) RETURNING *;

-- name: UpdateProject :one
UPDATE project SET
    title = COALESCE(sqlc.narg('title'), title),
    description = CASE WHEN sqlc.arg('description_set') THEN sqlc.narg('description') ELSE description END,
    icon = CASE WHEN sqlc.arg('icon_set') THEN sqlc.narg('icon') ELSE icon END,
    status = COALESCE(sqlc.narg('status'), status),
    priority = COALESCE(sqlc.narg('priority'), priority),
    lead_type = CASE WHEN sqlc.arg('lead_type_set') THEN sqlc.narg('lead_type') ELSE lead_type END,
    lead_id = CASE WHEN sqlc.arg('lead_id_set') THEN sqlc.narg('lead_id') ELSE lead_id END,
    start_date = CASE WHEN sqlc.arg('start_date_set') THEN sqlc.narg('start_date') ELSE start_date END,
    due_date = CASE WHEN sqlc.arg('due_date_set') THEN sqlc.narg('due_date') ELSE due_date END,
    updated_at = now()
WHERE id = ?1
RETURNING *;

-- name: DeleteProject :exec
-- Defense-in-depth: workspace_id is a SQL-layer tenant guard. See DeleteIssue.
DELETE FROM project WHERE id = ?1 AND workspace_id = ?2;

-- name: CountIssuesByProject :one
SELECT count(*) FROM issue
WHERE project_id = ?1;

-- name: GetProjectIssueStats :many
SELECT project_id,
       count(*) AS total_count,
       SUM(CASE WHEN status IN (SELECT value FROM json_each(sqlc.arg('terminal_status_keys'))) THEN 1 ELSE 0 END) AS done_count
FROM issue
WHERE workspace_id = sqlc.arg('workspace_id')
  AND project_id IN (SELECT value FROM json_each(sqlc.arg('project_ids')))
GROUP BY project_id;
