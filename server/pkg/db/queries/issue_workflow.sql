-- Project workflows (MUL-7420). A workflow selects and orders statuses from
-- the workspace's issue_status catalog. Projects opt in through
-- project.workflow_id; a project without one uses the workspace's Default
-- workflow row. Each step also stores who an issue entering it is handed to.

-- name: ListIssueWorkflows :many
SELECT * FROM issue_workflow
WHERE workspace_id = sqlc.arg('workspace_id')
ORDER BY is_default DESC, lower(name), created_at, id;

-- name: GetDefaultIssueWorkflow :one
SELECT * FROM issue_workflow
WHERE workspace_id = sqlc.arg('workspace_id')
  AND is_default;

-- name: CreateDefaultIssueWorkflow :many
-- Returns no row when another request created the Default first.
INSERT INTO issue_workflow (workspace_id, name, description, initial_status_key, steps, is_default)
VALUES (
    sqlc.arg('workspace_id'),
    'Default',
    '',
    'todo',
    sqlc.arg('steps'),
    true
)
ON CONFLICT (workspace_id) WHERE is_default DO NOTHING
RETURNING *;

-- name: ListNamedIssueWorkflowSteps :many
-- The steps of every workflow except Default, for the Default snapshot.
SELECT steps FROM issue_workflow
WHERE workspace_id = sqlc.arg('workspace_id')
  AND NOT is_default;

-- name: RemoveStatusFromDefaultIssueWorkflow :exec
UPDATE issue_workflow SET
    steps = COALESCE((
        SELECT json_group_array(json(step_value))
        FROM (
            SELECT json_extract(issue_workflow.steps, '$[' || step.key || ']') AS step_value
            FROM json_each(issue_workflow.steps) AS step
            WHERE json_extract(issue_workflow.steps, '$[' || step.key || '].status_key') <> sqlc.arg('status_key')
            ORDER BY step.key
        )
    ), '[]'),
    updated_at = now()
WHERE workspace_id = sqlc.arg('workspace_id')
  AND is_default
  AND EXISTS (
      SELECT 1 FROM json_each(issue_workflow.steps) AS st
      WHERE json_extract(issue_workflow.steps, '$[' || st.key || '].status_key') = sqlc.arg('status_key')
  );

-- name: ListProjectsUsingDefaultIssueWorkflow :many
SELECT * FROM project
WHERE workspace_id = sqlc.arg('workspace_id')
  AND workflow_id IS NULL
ORDER BY created_at, id;

-- name: GetIssueWorkflow :one
SELECT * FROM issue_workflow
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id');

-- name: CreateIssueWorkflow :one
INSERT INTO issue_workflow (workspace_id, name, description, initial_status_key, steps)
VALUES (
    sqlc.arg('workspace_id'),
    sqlc.arg('name'),
    sqlc.arg('description'),
    sqlc.arg('initial_status_key'),
    sqlc.arg('steps')
)
RETURNING *;

-- name: UpdateIssueWorkflow :one
UPDATE issue_workflow SET
    name = sqlc.arg('name'),
    description = sqlc.arg('description'),
    initial_status_key = sqlc.arg('initial_status_key'),
    steps = sqlc.arg('steps'),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: DeleteIssueWorkflow :execrows
DELETE FROM issue_workflow
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
  AND NOT is_default;

-- name: DeleteIssueWorkflowsForWorkspace :exec
-- No foreign keys by project rule, so workspace teardown cleans up here.
DELETE FROM issue_workflow WHERE workspace_id = sqlc.arg('workspace_id');

-- name: ListIssueWorkflowNamesUsingStatusKey :many
SELECT name FROM issue_workflow
WHERE workspace_id = sqlc.arg('workspace_id')
  AND NOT is_default
  AND EXISTS (
      SELECT 1 FROM json_each(issue_workflow.steps) AS st
      WHERE json_extract(issue_workflow.steps, '$[' || st.key || '].status_key') = sqlc.arg('status_key')
  )
ORDER BY lower(name);

-- name: ListProjectsUsingIssueWorkflow :many
SELECT * FROM project
WHERE workspace_id = sqlc.arg('workspace_id')
  AND workflow_id = sqlc.arg('workflow_id')
ORDER BY created_at, id;

-- name: CountProjectIssuesByStatus :many
-- Status census for a workflow switch or step removal, including terminal
-- issues: every issue of the project must end on a status its workflow lists.
SELECT status, COUNT(*) AS issue_count
FROM issue
WHERE workspace_id = sqlc.arg('workspace_id')
  AND project_id IN (SELECT value FROM json_each(sqlc.arg('project_ids')))
GROUP BY status
ORDER BY status;

-- name: RemapProjectIssueStatus :many
-- Moves every issue of the given projects from one status to another as part
-- of an administrative workflow change. Mirrors UpdateIssueStatus: the issue
-- is re-ranked to the top of its new column, the revision advances, and a
-- duplicate mark only survives cancelled -> cancelled (never the case here,
-- because from and to differ). It deliberately touches nothing else, so it
-- never hands an issue off or starts a run.
UPDATE issue SET
    status = sqlc.arg('to_status'),
    duplicate_of_issue_id = NULL,
    position = (
        SELECT COALESCE(MIN(target.position), 0) - 1
        FROM issue AS target
        WHERE target.workspace_id = issue.workspace_id
          AND target.status = sqlc.arg('to_status')
    ),
    revision = issue.revision + 1,
    last_activity_at = GREATEST(COALESCE(issue.last_activity_at, issue.updated_at), now()),
    updated_at = now()
WHERE issue.workspace_id = sqlc.arg('workspace_id')
  AND issue.project_id IN (SELECT value FROM json_each(sqlc.arg('project_ids')))
  AND issue.status = sqlc.arg('from_status')
  AND issue.status <> sqlc.arg('to_status')
RETURNING *;

-- name: LockProjectForWorkflowChange :one
-- Serializes concurrent workflow switches of one project.
SELECT * FROM project
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id');

-- name: SetProjectWorkflow :one
UPDATE project SET
    workflow_id = sqlc.narg('workflow_id'),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;
