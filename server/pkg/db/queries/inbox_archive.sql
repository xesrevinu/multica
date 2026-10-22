-- name: ListArchivedInboxPage :many
-- Select narrow group representatives before loading bodies or comment anchors.
WITH newest AS (
    SELECT id, issue_id, created_at, is_read, actor
    FROM (
        SELECT i.id, i.issue_id, i.created_at, i.read AS is_read,
               CASE WHEN i.actor_type = 'system' THEN 'system'
                    ELSE i.actor_type || ':' || i.actor_id END AS actor,
               row_number() OVER (
                   PARTITION BY COALESCE(i.issue_id, i.id)
                   ORDER BY i.created_at DESC, i.id DESC
               ) AS rn
        FROM inbox_item i
        WHERE i.workspace_id = sqlc.arg(workspace_id)
          AND i.recipient_type = 'member'
          AND i.recipient_id = sqlc.arg(recipient_id)
          AND i.archived = true
          AND (sqlc.narg(group_id) IS NULL OR i.issue_id = sqlc.narg(group_id)
               OR (i.issue_id IS NULL AND i.id = sqlc.narg(group_id)))
          AND (i.issue_id IS NULL OR NOT EXISTS (
              SELECT 1 FROM inbox_item active
              WHERE active.workspace_id = i.workspace_id
                AND active.recipient_type = i.recipient_type
                AND active.recipient_id = i.recipient_id
                AND active.issue_id = i.issue_id AND active.archived = false
          ))
    )
    WHERE rn = 1
), projected AS (
    SELECT newest.id, newest.issue_id, newest.created_at, newest.is_read, newest.actor,
           iss.status AS issue_status, iss.priority AS issue_priority
    FROM newest
    LEFT JOIN issue iss ON iss.id = newest.issue_id
      AND iss.workspace_id = sqlc.arg(workspace_id)
), matched AS (
    SELECT projected.id, projected.issue_id, projected.created_at, projected.is_read, projected.actor,
           projected.issue_status, projected.issue_priority,
           ((SELECT count(*) FROM json_each(sqlc.arg(statuses))) = 0 OR projected.issue_status IN (SELECT value FROM json_each(sqlc.arg(statuses)))) AS status_match,
           ((SELECT count(*) FROM json_each(sqlc.arg(priorities))) = 0 OR projected.issue_priority IN (SELECT value FROM json_each(sqlc.arg(priorities)))) AS priority_match,
           ((SELECT count(*) FROM json_each(sqlc.arg(actors))) = 0 OR projected.actor IN (SELECT value FROM json_each(sqlc.arg(actors)))) AS actor_match,
           (sqlc.arg(unread_only) = false OR projected.is_read = false) AS read_match
    FROM projected
)
, selected AS (
    SELECT * FROM matched
    WHERE status_match AND priority_match AND actor_match AND read_match
      AND (sqlc.narg(before_time) IS NULL OR
           CAST(matched.created_at AS text) < CAST(sqlc.narg(before_time) AS text) OR
           (CAST(matched.created_at AS text) = CAST(sqlc.narg(before_time) AS text) AND CAST(matched.id AS text) < CAST(sqlc.narg(before_id) AS text)))
    ORDER BY created_at DESC, id DESC
    LIMIT sqlc.arg(page_limit)
)
SELECT sqlc.embed(i), selected.issue_status, selected.issue_priority,
       COALESCE((
           SELECT NULLIF(json_extract(a.details, '$.comment_id'), '')
           FROM inbox_item a
           WHERE i.issue_id IS NOT NULL AND a.issue_id = i.issue_id
             AND a.workspace_id = i.workspace_id AND a.recipient_type = i.recipient_type
             AND a.recipient_id = i.recipient_id AND a.archived = true
             AND NULLIF(json_extract(a.details, '$.comment_id'), '') IS NOT NULL
           ORDER BY a.created_at DESC, a.id DESC LIMIT 1
       ), '') AS comment_id
FROM selected
JOIN inbox_item i ON i.id = selected.id
ORDER BY i.created_at DESC, i.id DESC;

-- name: ArchivedInboxFacets :many
-- Select narrow group representatives before loading bodies or comment anchors.
WITH newest AS (
    SELECT id, issue_id, created_at, is_read, actor
    FROM (
        SELECT i.id, i.issue_id, i.created_at, i.read AS is_read,
               CASE WHEN i.actor_type = 'system' THEN 'system'
                    ELSE i.actor_type || ':' || i.actor_id END AS actor,
               row_number() OVER (
                   PARTITION BY COALESCE(i.issue_id, i.id)
                   ORDER BY i.created_at DESC, i.id DESC
               ) AS rn
        FROM inbox_item i
        WHERE i.workspace_id = sqlc.arg(workspace_id)
          AND i.recipient_type = 'member'
          AND i.recipient_id = sqlc.arg(recipient_id)
          AND i.archived = true
          AND (i.issue_id IS NULL OR NOT EXISTS (
              SELECT 1 FROM inbox_item active
              WHERE active.workspace_id = i.workspace_id
                AND active.recipient_type = i.recipient_type
                AND active.recipient_id = i.recipient_id
                AND active.issue_id = i.issue_id AND active.archived = false
          ))
    )
    WHERE rn = 1
), projected AS (
    SELECT newest.id, newest.issue_id, newest.created_at, newest.is_read, newest.actor,
           iss.status AS issue_status, iss.priority AS issue_priority
    FROM newest
    LEFT JOIN issue iss ON iss.id = newest.issue_id
      AND iss.workspace_id = sqlc.arg(workspace_id)
), matched AS (
    SELECT projected.id, projected.issue_id, projected.created_at, projected.is_read, projected.actor,
           projected.issue_status, projected.issue_priority,
           ((SELECT count(*) FROM json_each(sqlc.arg(statuses))) = 0 OR projected.issue_status IN (SELECT value FROM json_each(sqlc.arg(statuses)))) AS status_match,
           ((SELECT count(*) FROM json_each(sqlc.arg(priorities))) = 0 OR projected.issue_priority IN (SELECT value FROM json_each(sqlc.arg(priorities)))) AS priority_match,
           ((SELECT count(*) FROM json_each(sqlc.arg(actors))) = 0 OR projected.actor IN (SELECT value FROM json_each(sqlc.arg(actors)))) AS actor_match,
           (sqlc.arg(unread_only) = false OR projected.is_read = false) AS read_match
    FROM projected
)
SELECT 'statuses' AS dimension, issue_status AS "key",
       SUM(CASE WHEN priority_match AND actor_match AND read_match THEN 1 ELSE 0 END) AS count
FROM matched WHERE issue_status IS NOT NULL GROUP BY issue_status
UNION ALL
SELECT 'priorities', issue_priority,
       SUM(CASE WHEN status_match AND actor_match AND read_match THEN 1 ELSE 0 END)
FROM matched WHERE issue_priority IS NOT NULL GROUP BY issue_priority
UNION ALL
SELECT 'actors', actor,
       SUM(CASE WHEN status_match AND priority_match AND read_match THEN 1 ELSE 0 END)
FROM matched WHERE actor IS NOT NULL GROUP BY actor
UNION ALL
SELECT 'unread', 'unread',
       SUM(CASE WHEN status_match AND priority_match AND actor_match AND is_read = false THEN 1 ELSE 0 END)
FROM matched;
