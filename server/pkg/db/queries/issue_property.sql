-- name: ListIssueProperties :many
-- usage_count = number of issues in the workspace that currently carry a
-- value for this property. `properties ? id` is a seq scan today; fine at
-- the 20-definition / small-workspace scale this feature targets.
SELECT p.*,
    (
        SELECT COUNT(*) FROM issue i
        WHERE i.workspace_id = p.workspace_id
          AND json_type(i.properties, '$.' || p.id) IS NOT NULL
    ) AS usage_count
FROM issue_property p
WHERE p.workspace_id = sqlc.arg('workspace_id')
  AND (sqlc.arg('include_archived') OR p.archived_at IS NULL)
ORDER BY p.position ASC, LOWER(p.name) ASC;

-- name: GetIssueProperty :one
SELECT * FROM issue_property
WHERE id = ?1 AND workspace_id = ?2;

-- name: CountActiveIssueProperties :one
SELECT COUNT(*) FROM issue_property
WHERE workspace_id = ?1 AND archived_at IS NULL;

-- name: CreateIssueProperty :one
-- New definitions append to the end of the catalog: position = max + 1.
INSERT INTO issue_property (workspace_id, name, type, description, icon, config, position)
SELECT sqlc.arg('workspace_id') AS workspace_id,
       sqlc.arg('name') AS name,
       sqlc.arg('type') AS type,
       sqlc.arg('description') AS description,
       sqlc.arg('icon') AS icon,
       sqlc.arg('config') AS config,
       (COALESCE((SELECT MAX(position) FROM issue_property WHERE workspace_id = sqlc.arg('workspace_id')), 0) + 1) AS position
RETURNING *;

-- name: UpdateIssueProperty :one
-- `type` is deliberately immutable - changing it would silently invalidate
-- existing values. archived_set/archived_at implement tri-state semantics:
-- archived_set=false leaves archived_at untouched.
UPDATE issue_property SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    icon = COALESCE(sqlc.narg('icon'), icon),
    config = COALESCE(sqlc.narg('config'), config),
    archived_at = CASE WHEN sqlc.arg('archived_set') THEN sqlc.narg('archived_at') ELSE archived_at END,
    updated_at = now()
WHERE id = ?1 AND workspace_id = ?2
RETURNING *;

-- name: SetIssuePropertyValue :one
-- Single-key atomic write: concurrent writers on different property keys
-- never clobber each other. Unlike SetIssueMetadataKey, a no-op still updates
-- and returns the issue row.
UPDATE issue
SET properties = json_set(COALESCE(properties, '{}'), '$.' || sqlc.arg('key'), json(sqlc.arg('value'))),
    revision = revision + CASE WHEN json_extract(properties, '$.' || sqlc.arg('key')) IS NOT json_extract(sqlc.arg('value'), '$') THEN 1 ELSE 0 END,
    last_activity_at = CASE
        WHEN json_extract(properties, '$.' || sqlc.arg('key')) IS NOT json_extract(sqlc.arg('value'), '$')
        THEN GREATEST(COALESCE(last_activity_at, updated_at), now())
        ELSE last_activity_at
    END,
    updated_at = CASE WHEN json_extract(properties, '$.' || sqlc.arg('key')) IS NOT json_extract(sqlc.arg('value'), '$') THEN now() ELSE updated_at END
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: DeleteIssuePropertyValue :one
UPDATE issue
SET properties = json_remove(properties, '$.' || sqlc.arg('key')),
    revision = revision + CASE WHEN json_type(properties, '$.' || sqlc.arg('key')) IS NOT NULL THEN 1 ELSE 0 END,
    last_activity_at = CASE
        WHEN json_type(properties, '$.' || sqlc.arg('key')) IS NOT NULL
        THEN GREATEST(COALESCE(last_activity_at, updated_at), now())
        ELSE last_activity_at
    END,
    updated_at = CASE WHEN json_type(properties, '$.' || sqlc.arg('key')) IS NOT NULL THEN now() ELSE updated_at END
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: CountIssuesUsingPropertyOptions :many
-- option_ids is a JSON array of option id strings.
SELECT json_extract(opt.value, '$') AS option_id, COUNT(i.id) AS usage_count
FROM json_each(sqlc.arg('option_ids')) AS opt
LEFT JOIN issue i
  ON i.workspace_id = sqlc.arg('workspace_id')
 AND (
      json_extract(i.properties, '$.' || sqlc.arg('property_key')) = json_extract(opt.value, '$')
      OR EXISTS (
          SELECT 1
          FROM json_each(
              CASE WHEN json_type(i.properties, '$.' || sqlc.arg('property_key')) IN ('array', 'object')
                   THEN json_extract(i.properties, '$.' || sqlc.arg('property_key'))
                   ELSE '[]' END
          ) AS el
          WHERE json_extract(el.value, '$') = json_extract(opt.value, '$')
             OR el.key = json_extract(opt.value, '$')
      )
 )
GROUP BY json_extract(opt.value, '$')
HAVING COUNT(i.id) > 0;
