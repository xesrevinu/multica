-- name: CreateAttachment :one
-- Order: CreateAttachment, then CreateAttachment_Step2, then CreateAttachment_Step3.
-- Step2/Step3 bump the owning issue and comment. The revision columns here are
-- the values those steps persist.
INSERT INTO attachment (
  id, workspace_id, issue_id, comment_id, chat_session_id, task_id, source_context_id,
  uploader_type, uploader_id, filename, url, content_type, size_bytes
)
VALUES (
  ?1, ?2, sqlc.narg(issue_id), sqlc.narg(comment_id), sqlc.narg(chat_session_id), sqlc.narg(task_id), sqlc.narg(source_context_id),
  ?3, ?4, ?5, ?6, ?7, ?8
)
RETURNING *,
  COALESCE((SELECT revision + 1 FROM issue WHERE id = sqlc.narg(issue_id)), 0) AS issue_revision,
  COALESCE((SELECT revision + 1 FROM comment WHERE id = sqlc.narg(comment_id)), 0) AS comment_revision;

-- name: CreateAttachment_Step2 :exec
UPDATE issue
SET revision = revision + 1
WHERE id = sqlc.narg(issue_id);

-- name: CreateAttachment_Step3 :exec
UPDATE comment
SET revision = revision + 1
WHERE id = sqlc.narg(comment_id);

-- name: ListAttachmentsByIssue :many
SELECT * FROM attachment
WHERE issue_id = ?1 AND workspace_id = ?2
ORDER BY created_at ASC;

-- name: ListAttachmentsByComment :many
SELECT * FROM attachment
WHERE comment_id = ?1 AND workspace_id = ?2
ORDER BY created_at ASC;

-- name: GetAttachment :one
SELECT * FROM attachment
WHERE id = ?1 AND workspace_id = ?2;

-- name: GetAttachmentByIDOnly :one
-- Used by the download endpoint, which derives workspace context from the
-- attachment row itself rather than from request headers/query params. The
-- caller still has to verify the requester is a member of the returned
-- workspace_id before serving the bytes - this query is access-neutral on
-- purpose so a self-contained URL like /api/attachments/{id}/download can
-- work as a native <img>/<video> resource load (no header attachment).
SELECT * FROM attachment
WHERE id = ?1;

-- name: ListAttachmentsByCommentIDs :many
SELECT * FROM attachment
WHERE comment_id IN (SELECT value FROM json_each(?1)) AND workspace_id = ?2
ORDER BY created_at ASC;

-- name: ListSourceContextIssueAttachments :many
-- Source snapshots keep issue-owned attachments separate from comment-owned
-- attachments. In this schema a comment attachment retains issue_id, so the
-- generic ListAttachmentsByIssue query is intentionally too broad here.
SELECT * FROM attachment
WHERE workspace_id = sqlc.arg(workspace_id)
  AND issue_id = sqlc.arg(issue_id)
  AND comment_id IS NULL
  AND source_context_id IS NULL
ORDER BY created_at ASC, id ASC;

-- name: ListSourceContextCommentAttachments :many
-- The issue predicate is a defense-in-depth owner guard for damaged rows; the
-- comment ids were already derived from the guarded thread history.
SELECT * FROM attachment
WHERE workspace_id = sqlc.arg(workspace_id)
  AND issue_id = sqlc.arg(issue_id)
  AND comment_id IN (SELECT value FROM json_each(sqlc.arg(comment_ids)))
  AND source_context_id IS NULL
ORDER BY created_at ASC, id ASC;

-- name: ListAttachmentURLsByIssueOrComments :many
SELECT a.url FROM attachment a
WHERE a.issue_id = ?1
   OR a.comment_id IN (SELECT c.id FROM comment c WHERE c.issue_id = ?1);

-- name: ListAttachmentURLsByCommentID :many
SELECT url FROM attachment
WHERE comment_id = ?1;

-- name: DeleteCommentAttachments :many
-- Part of the comment delete transaction: removes the deleted comment's
-- attachments and returns their storage URLs for cleanup after commit.
DELETE FROM attachment
WHERE comment_id = @comment_id AND workspace_id = @workspace_id
RETURNING url;

-- name: LinkAttachmentsToComment :exec
UPDATE attachment
SET comment_id = ?1
WHERE issue_id = ?2
  AND comment_id IS NULL
  AND source_context_id IS NULL
  AND id IN (SELECT value FROM json_each(?3));

-- name: ReplaceCommentAttachments :execrows
UPDATE attachment
SET comment_id = CASE
  WHEN id IN (SELECT value FROM json_each(sqlc.arg(attachment_ids))) THEN ?1
  ELSE NULL
END
WHERE issue_id = ?2
  AND source_context_id IS NULL
  AND (
    (comment_id = ?1 AND NOT id IN (SELECT value FROM json_each(sqlc.arg(attachment_ids))))
    OR (comment_id IS NULL AND id IN (SELECT value FROM json_each(sqlc.arg(attachment_ids))))
  );

-- name: LinkAttachmentsToChatMessage :many
UPDATE attachment
SET chat_message_id = sqlc.arg(chat_message_id),
    chat_session_id = sqlc.arg(chat_session_id)
WHERE workspace_id = sqlc.arg(workspace_id)
  AND issue_id IS NULL
  AND comment_id IS NULL
  AND chat_message_id IS NULL
  AND source_context_id IS NULL
  AND (
    chat_session_id IS NULL
    OR chat_session_id = sqlc.arg(chat_session_id)
  )
  AND uploader_type = sqlc.arg(uploader_type)
  AND uploader_id = sqlc.arg(uploader_id)
  AND id IN (SELECT value FROM json_each(sqlc.arg(attachment_ids)))
RETURNING id;

-- name: DetachAttachmentsFromUserChatMessageByTask :many
-- When an empty chat task is cancelled, its user message is deleted. The
-- attachment FK is ON DELETE CASCADE, so without this the bound rows would be
-- destroyed and a restored draft could never re-bind them. Detach first
-- (chat_message_id -> NULL, keep chat_session_id) so the rows survive as
-- workspace/session-scoped unattached attachments and re-send can re-link them.
UPDATE attachment
SET chat_message_id = NULL
WHERE chat_message_id IN (
  SELECT id FROM chat_message WHERE chat_message.task_id = ?1 AND role = 'user'
)
RETURNING *;

-- name: CountUnboundChatAttachmentsForTask :one
-- How many attachments the agent produced for this chat task that are still
-- unbound to any owner. Lets CompleteTask create an assistant message (and
-- bind them) even when the agent's text output was empty but it uploaded files.
SELECT COUNT(*) FROM attachment
WHERE workspace_id = sqlc.arg(workspace_id)
  AND task_id = sqlc.arg(task_id)
  AND issue_id IS NULL
  AND comment_id IS NULL
  AND chat_message_id IS NULL
  AND source_context_id IS NULL;

-- name: BindChatAttachmentsToMessage :many
-- Bind a chat agent's task-scoped attachments to the assistant reply it just
-- produced. Only rows still unclaimed by any owner (issue/comment/chat_message)
-- are eligible, so an attachment already linked elsewhere is never stolen.
-- Returns the bound ids for logging.
UPDATE attachment
SET chat_message_id = sqlc.arg(chat_message_id)
WHERE workspace_id = sqlc.arg(workspace_id)
  AND task_id = sqlc.arg(task_id)
  AND issue_id IS NULL
  AND comment_id IS NULL
  AND chat_message_id IS NULL
  AND source_context_id IS NULL
RETURNING id;

-- name: ListAttachmentsByChatMessage :many
SELECT * FROM attachment
WHERE chat_message_id = ?1 AND workspace_id = ?2
ORDER BY created_at ASC;

-- name: ListAttachmentsByChatMessageIDs :many
SELECT * FROM attachment
WHERE chat_message_id IN (SELECT value FROM json_each(?1)) AND workspace_id = ?2
ORDER BY created_at ASC;

-- name: LockAttachmentsForIssueLink :many
-- Issue updates bind attachments and then touch the owner row. Only rows that
-- belong to no issue yet are eligible, and nothing reaches those through an
-- issue - not teardown's cascade, not DeleteAttachment, which takes the owning
-- issue first - so locking them before the owner cannot deadlock.
-- Attachments that DO belong to the issue are locked after it; see
-- LockAttachmentsForCommentLink.
SELECT id FROM attachment
WHERE workspace_id = sqlc.arg(workspace_id)
  AND issue_id IS NULL
  AND source_context_id IS NULL
  AND id IN (SELECT value FROM json_each(sqlc.arg(attachment_ids)))
ORDER BY id;

-- name: LockAttachmentsForCommentLink :many
-- CreateComment binds attachments in the transaction that created the comment,
-- after the CreateComment statement has taken the issue row: issue -> comment
-- -> child, the order every owner-first mutation here uses. This pins the
-- requested set under that lock and returns the ids still eligible, so the
-- caller can refuse before the comment is committed when a requested
-- attachment was deleted while the issue lock was contended.
SELECT id FROM attachment
WHERE workspace_id = sqlc.arg(workspace_id)
  AND issue_id = sqlc.arg(issue_id)
  AND comment_id IS NULL
  AND source_context_id IS NULL
  AND id IN (SELECT value FROM json_each(sqlc.arg(attachment_ids)))
ORDER BY id;

-- name: LockAttachmentRow :one
-- Locks an attachment that has no owner to lock instead - a chat, avatar or
-- still-unbound upload. Reading it under its own lock is what keeps it from
-- gaining an owner between the read and the write, which would put the write
-- back in the attachment -> issue order issue teardown deadlocks with.
SELECT * FROM attachment
WHERE id = ?1 AND workspace_id = ?2;

-- name: LinkAttachmentsToIssue :one
-- Order: LinkAttachmentsToIssue, then LinkAttachmentsToIssue_Step2.
-- Step2 bumps the issue when bump_revision is true and a row was linked.
-- issue_revision is that post-bump value, or 0 when Step2 does not bump.
UPDATE attachment
SET issue_id = sqlc.arg(issue_id)
WHERE attachment.workspace_id = sqlc.arg(workspace_id)
  AND attachment.issue_id IS NULL
  AND attachment.source_context_id IS NULL
  AND attachment.id IN (SELECT value FROM json_each(sqlc.arg(attachment_ids)))
RETURNING
  (SELECT count(*) AS c FROM attachment linked
    WHERE linked.workspace_id = sqlc.arg(workspace_id)
      AND linked.issue_id = sqlc.arg(issue_id)
      AND linked.source_context_id IS NULL
      AND linked.id IN (SELECT value FROM json_each(sqlc.arg(attachment_ids)))
  ) AS linked_count,
  iif(
    sqlc.arg(bump_revision),
    COALESCE((SELECT issue.revision + 1 AS rev FROM issue WHERE issue.id = sqlc.arg(issue_id)), 0),
    0
  ) AS issue_revision;

-- name: LinkAttachmentsToIssue_Step2 :exec
UPDATE issue
SET revision = revision + 1,
    updated_at = now()
WHERE issue.id = sqlc.arg(issue_id)
  AND sqlc.arg(bump_revision)
  AND EXISTS (
    SELECT 1 FROM attachment
    WHERE attachment.workspace_id = sqlc.arg(workspace_id)
      AND attachment.issue_id = sqlc.arg(issue_id)
      AND attachment.source_context_id IS NULL
      AND attachment.id IN (SELECT value FROM json_each(sqlc.arg(attachment_ids)))
  );

-- name: DeleteAttachment :one
-- Order: DeleteAttachment_Step2, DeleteAttachment_Step3, then DeleteAttachment.
-- The steps bump issue and comment revisions while the attachment row still
-- exists. This statement removes the row and returns those post-bump revisions.
DELETE FROM attachment
WHERE attachment.id = ?1 AND attachment.workspace_id = ?2
  AND attachment.source_context_id IS NULL
RETURNING
  1 AS changed,
  COALESCE((SELECT issue.revision FROM issue WHERE issue.id = attachment.issue_id), 0) AS issue_revision,
  COALESCE((SELECT comment.revision FROM comment WHERE comment.id = attachment.comment_id), 0) AS comment_revision;

-- name: DeleteAttachment_Step2 :exec
UPDATE issue
SET revision = revision + 1
WHERE issue.id = (
  SELECT attachment.issue_id FROM attachment
  WHERE attachment.id = ?1 AND attachment.workspace_id = ?2 AND attachment.source_context_id IS NULL AND attachment.issue_id IS NOT NULL
);

-- name: DeleteAttachment_Step3 :exec
UPDATE comment
SET revision = revision + 1
WHERE comment.id = (
  SELECT attachment.comment_id FROM attachment
  WHERE attachment.id = ?1 AND attachment.workspace_id = ?2 AND attachment.source_context_id IS NULL AND attachment.comment_id IS NOT NULL
);

-- name: ListAttachmentsByIDs :many
SELECT * FROM attachment
WHERE id IN (SELECT value FROM json_each(sqlc.arg(attachment_ids))) AND workspace_id = sqlc.arg(workspace_id)
ORDER BY created_at ASC;

-- name: CreateSourceContextAttachment :one
INSERT INTO attachment (
  id, workspace_id, source_context_id, uploader_type, uploader_id,
  filename, url, content_type, size_bytes
) VALUES (
  sqlc.arg(id), sqlc.arg(workspace_id), sqlc.arg(source_context_id),
  sqlc.arg(uploader_type), sqlc.arg(uploader_id), sqlc.arg(filename),
  sqlc.arg(url), sqlc.arg(content_type), sqlc.arg(size_bytes)
)
RETURNING *;

-- name: ListAttachmentsBySourceContext :many
SELECT * FROM attachment
WHERE workspace_id = sqlc.arg(workspace_id)
  AND source_context_id = sqlc.arg(source_context_id)
ORDER BY created_at ASC, id ASC;

-- name: DeleteAttachmentsBySourceContext :many
DELETE FROM attachment
WHERE workspace_id = sqlc.arg(workspace_id)
  AND source_context_id = sqlc.arg(source_context_id)
RETURNING *;

-- name: ListSourceContextAttachmentURLsByWorkspace :many
SELECT url FROM attachment
WHERE workspace_id = sqlc.arg(workspace_id)
  AND source_context_id IS NOT NULL
ORDER BY id;

-- name: DeleteSourceContextAttachmentsByWorkspace :exec
DELETE FROM attachment
WHERE workspace_id = sqlc.arg(workspace_id)
  AND source_context_id IS NOT NULL;
