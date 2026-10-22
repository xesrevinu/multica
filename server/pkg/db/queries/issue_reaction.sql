-- name: AddIssueReaction :one
INSERT INTO issue_reaction (issue_id, workspace_id, actor_type, actor_id, emoji)
VALUES (?1, ?2, ?3, ?4, ?5)
ON CONFLICT (issue_id, actor_type, actor_id, emoji) DO NOTHING
RETURNING id, issue_id, workspace_id, actor_type, actor_id, emoji, created_at,
    (SELECT src.issue_revision FROM (SELECT CAST(0 AS INTEGER) AS issue_revision) AS src) AS issue_revision;

-- name: AddIssueReaction_Step2 :exec
-- Bump the issue only when the insert above actually wrote a row.
UPDATE issue
SET revision = revision + 1
WHERE id = ?1
  AND changes() = 1;

-- name: RemoveIssueReaction :one
DELETE FROM issue_reaction
WHERE issue_id = ?1 AND actor_type = ?2 AND actor_id = ?3 AND emoji = ?4
RETURNING
    (SELECT src.changed FROM (SELECT 1 AS changed) AS src) AS changed,
    (SELECT src.issue_revision FROM (SELECT CAST(0 AS INTEGER) AS issue_revision) AS src) AS issue_revision;

-- name: RemoveIssueReaction_Step2 :exec
UPDATE issue
SET revision = revision + 1
WHERE id = ?1
  AND changes() = 1;

-- name: ListIssueReactions :many
SELECT * FROM issue_reaction
WHERE issue_id = ?1
ORDER BY created_at ASC;
