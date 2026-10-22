-- name: ListLabels :many
SELECT l.*,
    CASE l.resource_type
        WHEN 'issue' THEN (SELECT COUNT(*) FROM issue_to_label x WHERE x.label_id = l.id)
        WHEN 'agent' THEN (SELECT COUNT(*) FROM agent_to_label x WHERE x.label_id = l.id)
        WHEN 'skill' THEN (SELECT COUNT(*) FROM skill_to_label x WHERE x.label_id = l.id)
        ELSE 0
    END AS usage_count
FROM issue_label l
WHERE l.workspace_id = sqlc.arg('workspace_id')
  AND l.resource_type = sqlc.arg('resource_type')
ORDER BY LOWER(name) ASC;

-- name: GetLabel :one
SELECT * FROM issue_label
WHERE id = ?1 AND workspace_id = ?2;

-- name: CreateLabel :one
INSERT INTO issue_label (workspace_id, resource_type, name, description, color)
VALUES (?1, ?2, ?3, ?4, ?5)
RETURNING *;

-- name: UpdateLabel :one
UPDATE issue_label SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    color = COALESCE(sqlc.narg('color'), color),
    updated_at = now()
WHERE id = ?1 AND workspace_id = ?2
RETURNING *;

-- name: DeleteLabel :one
-- :one RETURNING id so the handler distinguishes pgx.ErrNoRows (-> 404) from
-- infrastructure errors (-> 500), and avoids a TOCTOU precheck.
DELETE FROM issue_label
WHERE id = ?1 AND workspace_id = ?2
RETURNING id;

-- The resource-label junctions deliberately have no foreign keys. Keeping
-- their cleanup in the same application transaction as the owner deletion
-- avoids database-level cascades with unreviewed locking and audit behavior.

-- name: DeleteIssueLabelAssignmentsByLabel :exec
DELETE FROM issue_to_label WHERE label_id = ?1;

-- name: DeleteAgentLabelAssignmentsByLabel :exec
DELETE FROM agent_to_label WHERE label_id = ?1;

-- name: DeleteSkillLabelAssignmentsByLabel :exec
DELETE FROM skill_to_label WHERE label_id = ?1;

-- name: DeleteAgentLabelAssignmentsByAgent :exec
DELETE FROM agent_to_label WHERE agent_id = ?1;

-- name: DeleteSkillLabelAssignmentsBySkill :exec
DELETE FROM skill_to_label WHERE skill_id = ?1;

-- The single-entity cleanups above cover one agent/skill at a time. The runtime
-- variant below covers runtime and runtime-profile bulk hard deletes, where the
-- owning agents disappear without passing through a per-entity delete.
-- Workspace-wide cleanup lives in DeleteWorkspace so it is atomic with that
-- workspace's existing multi-table teardown.

-- name: DeleteAgentLabelAssignmentsBySystemRuntimeAgents :exec
-- Runtime teardown hard-deletes the system agents bound to the runtime (user
-- agents are unbound and kept since MUL-5559). Clear only those agents' label
-- links so none survive the agent hard-delete - a surviving unbound agent must
-- keep its labels.
DELETE FROM agent_to_label
WHERE agent_id IN (SELECT id FROM agent WHERE runtime_id = ?1 AND kind = 'system');

-- name: AttachLabelToIssueOnCreate :exec
INSERT INTO issue_to_label (issue_id, label_id)
SELECT sqlc.arg('issue_id') AS issue_id, sqlc.arg('label_id') AS label_id
WHERE EXISTS (
    SELECT 1 FROM issue
    WHERE issue.id = sqlc.arg('issue_id')
      AND issue.workspace_id = sqlc.arg('workspace_id')
)
  AND EXISTS (
    SELECT 1 FROM issue_label
    WHERE issue_label.id = sqlc.arg('label_id')
      AND issue_label.workspace_id = sqlc.arg('workspace_id')
      AND issue_label.resource_type = 'issue'
)
  AND NOT EXISTS (
    SELECT 1 FROM issue_to_label
    WHERE issue_to_label.issue_id = sqlc.arg('issue_id')
      AND issue_to_label.label_id = sqlc.arg('label_id')
)
ON CONFLICT DO NOTHING;

-- name: AttachLabelToIssueOnCreate_Step2 :exec
UPDATE issue
SET last_activity_at = GREATEST(COALESCE(last_activity_at, updated_at), now())
WHERE issue.id = sqlc.arg('issue_id')
  AND issue.workspace_id = sqlc.arg('workspace_id')
  AND changes() = 1;

-- name: AttachLabelToIssue :one
INSERT INTO issue_to_label (issue_id, label_id)
SELECT sqlc.arg('issue_id') AS issue_id, sqlc.arg('label_id') AS label_id
WHERE EXISTS (
    SELECT 1 FROM issue i
    WHERE i.id = sqlc.arg('issue_id')
      AND i.workspace_id = sqlc.arg('workspace_id')
)
  AND EXISTS (
    SELECT 1 FROM issue_label l
    WHERE l.id = sqlc.arg('label_id')
      AND l.workspace_id = sqlc.arg('workspace_id')
      AND l.resource_type = 'issue'
)
ON CONFLICT (issue_id, label_id) DO NOTHING
RETURNING
    (SELECT src.changed FROM (SELECT 1 AS changed) AS src) AS changed,
    (SELECT src.issue_revision FROM (
        SELECT COALESCE((SELECT revision FROM issue WHERE id = sqlc.arg('issue_id')), 0) AS issue_revision
    ) AS src) AS issue_revision;

-- name: AttachLabelToIssue_Step2 :exec
UPDATE issue
SET revision = revision + 1,
    last_activity_at = GREATEST(COALESCE(last_activity_at, updated_at), now())
WHERE id = sqlc.arg('issue_id')
  AND workspace_id = sqlc.arg('workspace_id')
  AND changes() = 1
  AND EXISTS (
      SELECT 1 FROM issue_to_label
      WHERE issue_id = sqlc.arg('issue_id') AND label_id = sqlc.arg('label_id')
  );

-- name: DetachLabelFromIssue :one
DELETE FROM issue_to_label
WHERE issue_id = sqlc.arg('issue_id')
  AND label_id = sqlc.arg('label_id')
  AND EXISTS (
      SELECT 1 FROM issue i
      WHERE i.id = sqlc.arg('issue_id')
        AND i.workspace_id = sqlc.arg('workspace_id')
  )
RETURNING
    (SELECT src.changed FROM (SELECT 1 AS changed) AS src) AS changed,
    (SELECT src.issue_revision FROM (SELECT CAST(0 AS INTEGER) AS issue_revision) AS src) AS issue_revision;

-- name: DetachLabelFromIssue_Step2 :exec
UPDATE issue
SET revision = revision + 1,
    last_activity_at = GREATEST(COALESCE(last_activity_at, updated_at), now())
WHERE id = sqlc.arg('issue_id')
  AND workspace_id = sqlc.arg('workspace_id')
  AND changes() = 1;

-- name: ListLabelsByIssue :many
-- Workspace filter at the SQL layer (mirrors GetProjectInWorkspace). Any caller
-- that passes the wrong workspace gets an empty list rather than leaking labels.
SELECT l.*
FROM issue_label l
JOIN issue_to_label il ON il.label_id = l.id
WHERE il.issue_id = sqlc.arg('issue_id')
  AND l.workspace_id = sqlc.arg('workspace_id')
  AND l.resource_type = 'issue'
ORDER BY LOWER(l.name) ASC;

-- name: ListLabelsForIssues :many
-- Bulk variant: fetch labels for many issues in one round-trip so the issue
-- list endpoints can fold labels into each row without N+1 queries from the
-- client. Workspace-guarded the same way as ListLabelsByIssue.
SELECT il.issue_id, l.*
FROM issue_label l
JOIN issue_to_label il ON il.label_id = l.id
WHERE il.issue_id IN (SELECT value FROM json_each(sqlc.arg('issue_ids')))
  AND l.workspace_id = sqlc.arg('workspace_id')
  AND l.resource_type = 'issue'
ORDER BY il.issue_id, LOWER(l.name) ASC;

-- name: ListLabelsByAgent :many
SELECT l.*
FROM issue_label l
JOIN agent_to_label atl ON atl.label_id = l.id
WHERE atl.agent_id = sqlc.arg('agent_id')
  AND l.workspace_id = sqlc.arg('workspace_id')
  AND l.resource_type = 'agent'
ORDER BY LOWER(l.name) ASC;

-- name: ListLabelsForAgents :many
SELECT atl.agent_id, l.*
FROM issue_label l
JOIN agent_to_label atl ON atl.label_id = l.id
WHERE atl.agent_id IN (SELECT value FROM json_each(sqlc.arg('agent_ids')))
  AND l.workspace_id = sqlc.arg('workspace_id')
  AND l.resource_type = 'agent'
ORDER BY atl.agent_id, LOWER(l.name) ASC;

-- name: AttachLabelToAgent :exec
INSERT INTO agent_to_label (agent_id, label_id)
SELECT sqlc.arg('agent_id') AS agent_id, sqlc.arg('label_id') AS label_id
WHERE EXISTS (
    SELECT 1 FROM agent a
    WHERE a.id = sqlc.arg('agent_id')
      AND a.workspace_id = sqlc.arg('workspace_id')
)
AND EXISTS (
    SELECT 1 FROM issue_label l
    WHERE l.id = sqlc.arg('label_id')
      AND l.workspace_id = sqlc.arg('workspace_id')
      AND l.resource_type = 'agent'
)
ON CONFLICT DO NOTHING;

-- name: DetachLabelFromAgent :exec
DELETE FROM agent_to_label
WHERE agent_id = sqlc.arg('agent_id')
  AND label_id = sqlc.arg('label_id')
  AND EXISTS (
      SELECT 1 FROM agent a
      WHERE a.id = sqlc.arg('agent_id')
        AND a.workspace_id = sqlc.arg('workspace_id')
  );

-- name: ListLabelsBySkill :many
SELECT l.*
FROM issue_label l
JOIN skill_to_label stl ON stl.label_id = l.id
WHERE stl.skill_id = sqlc.arg('skill_id')
  AND l.workspace_id = sqlc.arg('workspace_id')
  AND l.resource_type = 'skill'
ORDER BY LOWER(l.name) ASC;

-- name: ListLabelsForSkills :many
SELECT stl.skill_id, l.*
FROM issue_label l
JOIN skill_to_label stl ON stl.label_id = l.id
WHERE stl.skill_id IN (SELECT value FROM json_each(sqlc.arg('skill_ids')))
  AND l.workspace_id = sqlc.arg('workspace_id')
  AND l.resource_type = 'skill'
ORDER BY stl.skill_id, LOWER(l.name) ASC;

-- name: AttachLabelToSkill :exec
INSERT INTO skill_to_label (skill_id, label_id)
SELECT sqlc.arg('skill_id') AS skill_id, sqlc.arg('label_id') AS label_id
WHERE EXISTS (
    SELECT 1 FROM skill s
    WHERE s.id = sqlc.arg('skill_id')
      AND s.workspace_id = sqlc.arg('workspace_id')
)
AND EXISTS (
    SELECT 1 FROM issue_label l
    WHERE l.id = sqlc.arg('label_id')
      AND l.workspace_id = sqlc.arg('workspace_id')
      AND l.resource_type = 'skill'
)
ON CONFLICT DO NOTHING;

-- name: DetachLabelFromSkill :exec
DELETE FROM skill_to_label
WHERE skill_id = sqlc.arg('skill_id')
  AND label_id = sqlc.arg('label_id')
  AND EXISTS (
      SELECT 1 FROM skill s
      WHERE s.id = sqlc.arg('skill_id')
        AND s.workspace_id = sqlc.arg('workspace_id')
  );
