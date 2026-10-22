-- name: CreateFeedback :one
INSERT INTO feedback (user_id, workspace_id, message, metadata)
VALUES (?1, sqlc.narg(workspace_id), ?2, ?3)
RETURNING *;

-- name: CountRecentFeedbackByUser :one
SELECT count(*) FROM feedback
WHERE user_id = ?1 AND created_at > datetime('now', '-1 hours');
