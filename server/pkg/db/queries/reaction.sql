-- name: AddReaction :one
-- Order: AddReaction, then AddReaction_Step2 when the reaction row was newly
-- inserted. SQLite cannot both insert and bump in one statement. This upsert
-- always returns the reaction row; comment_revision is 0 because the bump is
-- Step2 (skip Step2 when the row already existed).
INSERT INTO comment_reaction (comment_id, workspace_id, actor_type, actor_id, emoji)
VALUES (?1, ?2, ?3, ?4, ?5)
ON CONFLICT (comment_id, actor_type, actor_id, emoji) DO UPDATE
SET emoji = excluded.emoji
RETURNING *, 0 AS comment_revision;

-- name: AddReaction_Step2 :exec
UPDATE comment
SET revision = revision + 1
WHERE id = ?1;

-- name: RemoveReaction :one
-- Order: RemoveReaction_Step2, then RemoveReaction. Step2 bumps the comment
-- while the reaction row still exists. changed is 1 when that row was removed.
DELETE FROM comment_reaction
WHERE comment_id = ?1 AND actor_type = ?2 AND actor_id = ?3 AND emoji = ?4
RETURNING
  1 AS changed,
  COALESCE((SELECT comment.revision FROM comment WHERE comment.id = comment_reaction.comment_id), 0) AS comment_revision;

-- name: RemoveReaction_Step2 :exec
UPDATE comment
SET revision = revision + 1
WHERE comment.id = ?1
  AND EXISTS (
    SELECT 1 FROM comment_reaction
    WHERE comment_id = ?1 AND actor_type = ?2 AND actor_id = ?3 AND emoji = ?4
  );

-- name: ListReactionsByCommentIDs :many
SELECT * FROM comment_reaction
WHERE comment_id IN (SELECT value FROM json_each(?1))
ORDER BY created_at ASC;

-- name: DeleteCommentReactions :exec
-- Part of the comment delete transaction: reactions go with the comment.
DELETE FROM comment_reaction
WHERE comment_id = @comment_id AND workspace_id = @workspace_id;
