-- name: StartAgentTaskWithSupplement :one
-- Starting the task and recording the exact daemon/server capability handshake
-- are one state transition. A missing row is the fail-closed value for old
-- daemons, old servers, unsupported providers and application rollback.
-- Order: StartAgentTaskWithSupplement_Step2, then StartAgentTaskWithSupplement.
-- Step2 records the capability row. This statement moves the task to running.
UPDATE agent_task_queue
SET status = 'running',
    started_at = now(),
    wait_reason = NULL,
    prepare_lease_expires_at = NULL
WHERE agent_task_queue.id = @task_id
  AND agent_task_queue.status IN ('dispatched', 'waiting_local_directory')
  AND EXISTS (
      SELECT 1 FROM agent_runtime r WHERE r.id = agent_task_queue.runtime_id
  )
RETURNING *;

-- name: StartAgentTaskWithSupplement_Step2 :exec
INSERT INTO task_supplement_capability (task_id, workspace_id, issue_id, capability)
SELECT t.id AS task_id, r.workspace_id AS workspace_id, t.issue_id AS issue_id,
       'task-supplement-v1' AS capability
FROM agent_task_queue t
JOIN agent_runtime r ON r.id = t.runtime_id
WHERE t.id = @task_id
  AND t.status IN ('dispatched', 'waiting_local_directory', 'running')
  AND @enable_task_supplement
  AND r.provider IN ('codex', 'claude', 'grok')
  AND t.issue_id IS NOT NULL
ON CONFLICT DO NOTHING;

-- name: CreateTaskSupplement :one
-- Locking the exact task serializes against terminal transitions.
-- Comment creation, explicit task binding and run coverage then commit
-- together: a terminal-race loser creates nothing.
-- Order: CreateTaskSupplement_Step2 (bump issue), CreateTaskSupplement
-- (insert comment, returns the row), CreateTaskSupplement_Step3 (bind supplement).
INSERT INTO comment (
    issue_id, workspace_id, author_type, author_id, content, type, parent_id
)
SELECT t.issue_id AS issue_id, @workspace_id AS workspace_id, 'member' AS author_type,
       @author_id AS author_id, @content AS content, 'comment' AS type,
       t.trigger_comment_id AS parent_id
FROM agent_task_queue t
JOIN agent_runtime r ON r.id = t.runtime_id
JOIN task_supplement_capability cap ON cap.task_id = t.id
WHERE t.id = @task_id
  AND t.issue_id = @issue_id
  AND r.workspace_id = @workspace_id
  AND t.status = 'running'
  AND cap.capability = 'task-supplement-v1'
  AND r.provider IN ('codex', 'claude', 'grok')
RETURNING *,
  COALESCE((SELECT issue.revision FROM issue WHERE issue.id = comment.issue_id), 0) AS issue_revision,
  @task_id AS supplement_task_id,
  'pending' AS supplement_status,
  NULL AS supplement_failure_reason,
  NULL AS supplement_delivered_at;

-- name: CreateTaskSupplement_Step2 :exec
UPDATE issue
SET updated_at = now(),
    revision = revision + 1,
    last_activity_at = max(COALESCE(last_activity_at, updated_at), now())
WHERE issue.id = @issue_id
  AND issue.workspace_id = @workspace_id
  AND EXISTS (
      SELECT 1
      FROM agent_task_queue t
      JOIN agent_runtime r ON r.id = t.runtime_id
      JOIN task_supplement_capability cap ON cap.task_id = t.id
      WHERE t.id = @task_id
        AND t.issue_id = @issue_id
        AND r.workspace_id = @workspace_id
        AND t.status = 'running'
        AND cap.capability = 'task-supplement-v1'
        AND r.provider IN ('codex', 'claude', 'grok')
  );

-- name: CreateTaskSupplement_Step3 :exec
INSERT INTO task_supplement (
    task_id, workspace_id, issue_id, comment_id, author_id,
    client_request_id, status
)
SELECT @task_id AS task_id, @workspace_id AS workspace_id, @issue_id AS issue_id,
       c.id AS comment_id, @author_id AS author_id,
       @client_request_id AS client_request_id, 'pending' AS status
FROM comment c
WHERE c.issue_id = @issue_id
  AND c.workspace_id = @workspace_id
  AND c.author_type = 'member'
  AND c.author_id = @author_id
  AND c.content = @content
  AND c.type = 'comment'
  AND EXISTS (
      SELECT 1
      FROM agent_task_queue t
      JOIN agent_runtime r ON r.id = t.runtime_id
      JOIN task_supplement_capability cap ON cap.task_id = t.id
      WHERE t.id = @task_id
        AND t.issue_id = @issue_id
        AND r.workspace_id = @workspace_id
        AND t.status = 'running'
        AND cap.capability = 'task-supplement-v1'
        AND r.provider IN ('codex', 'claude', 'grok')
  )
ORDER BY c.created_at DESC, c.id DESC
LIMIT 1;

-- name: GetTaskSupplementByRequest :one
SELECT s.*
FROM task_supplement s
WHERE s.task_id = @task_id
  AND s.workspace_id = @workspace_id
  AND s.author_id = @author_id
  AND s.client_request_id = @client_request_id;

-- name: BindCommentTaskSupplement :one
-- Binds an ordinary member comment, already created through the normal comment
-- path, to the exact running turn its author chose. Locking the task
-- serializes against terminal transitions: when that turn ended first nothing
-- is bound - never a later turn of the same agent - and the caller keeps the
-- comment's normal trigger instead.
INSERT INTO task_supplement (
    task_id, workspace_id, issue_id, comment_id, author_id,
    client_request_id, status
)
SELECT t.id AS task_id, @workspace_id AS workspace_id, t.issue_id AS issue_id,
       @comment_id AS comment_id, @author_id AS author_id,
       @client_request_id AS client_request_id, 'pending' AS status
FROM agent_task_queue t
JOIN agent_runtime r ON r.id = t.runtime_id
JOIN task_supplement_capability cap ON cap.task_id = t.id
WHERE t.id = @task_id
  AND t.issue_id = @issue_id
  AND t.agent_id = @agent_id
  AND r.workspace_id = @workspace_id
  AND t.status = 'running'
  AND cap.capability = 'task-supplement-v1'
  AND r.provider IN ('codex', 'claude')
RETURNING *,
  (SELECT runtime_id FROM agent_task_queue WHERE id = task_supplement.task_id) AS runtime_id;

-- name: GetTaskSupplementForRun :one
SELECT * FROM task_supplement
WHERE comment_id = @comment_id AND task_id = @task_id AND workspace_id = @workspace_id;

-- name: CommentHasTaskSupplement :one
SELECT EXISTS (
    SELECT 1 FROM task_supplement
    WHERE comment_id = @comment_id AND workspace_id = @workspace_id
) AS bound;

-- name: GetTaskSupplementCapability :one
SELECT * FROM task_supplement_capability WHERE task_id = @task_id;

-- name: ListTaskSupplementsByCommentIDs :many
-- One comment may steer several runs; receipts carry the run's agent so a
-- client can say whose turn received it.
SELECT s.*, t.agent_id
FROM task_supplement s
LEFT JOIN agent_task_queue t ON t.id = s.task_id
WHERE s.workspace_id = @workspace_id
  AND s.comment_id IN (SELECT value FROM json_each(@comment_ids))
ORDER BY s.created_at, s.task_id;

-- name: ListTaskSupplementMetadata :many
SELECT cap.task_id, cap.capability,
       COALESCE((
           SELECT json_group_array(s.comment_id)
           FROM (
               SELECT s.comment_id AS comment_id
               FROM task_supplement s
               WHERE s.task_id = cap.task_id AND s.comment_id IS NOT NULL
               ORDER BY s.created_at, s.comment_id
           ) s
       ), '[]') AS comment_ids
FROM task_supplement_capability cap
WHERE cap.workspace_id = @workspace_id
  AND cap.task_id IN (SELECT value FROM json_each(@task_ids))
GROUP BY cap.task_id, cap.capability;

-- name: SettleTerminalTaskSupplements :execrows
-- Application terminal transitions call this in the same transaction as the
-- agent_task_queue update. Re-checking the persisted task status makes an
-- accidental early call a no-op while preserving the task-row -> supplement
-- lock order used by creation and delivery acknowledgement.
UPDATE task_supplement
SET status = 'failed',
    failure_reason = 'turn_ended',
    updated_at = now()
WHERE status IN ('pending', 'delivering')
  AND task_id IN (SELECT value FROM json_each(@task_ids))
  AND EXISTS (
      SELECT 1 FROM agent_task_queue task
      WHERE task.id = task_supplement.task_id
        AND task.status IN ('completed', 'failed', 'cancelled')
  );

-- name: ClaimNextTaskSupplement :one
WITH next AS (
    SELECT s.comment_id AS comment_id, s.task_id AS task_id
    FROM task_supplement s
    JOIN agent_task_queue t ON t.id = s.task_id
    JOIN task_supplement_capability cap ON cap.task_id = t.id
    WHERE s.task_id = @task_id
      AND s.status = 'pending'
      AND t.status = 'running'
      AND cap.capability = 'task-supplement-v1'
    ORDER BY s.created_at, s.comment_id
    LIMIT 1
)
UPDATE task_supplement
SET status = 'delivering',
    attempt_count = attempt_count + 1,
    failure_reason = NULL,
    updated_at = now()
WHERE task_supplement.task_id = @task_id
  AND task_supplement.comment_id = (SELECT next.comment_id FROM next)
RETURNING comment_id, attempt_count,
  (SELECT content FROM comment WHERE id = task_supplement.comment_id) AS content,
  COALESCE(NULLIF(trim((SELECT name FROM "user" WHERE id = task_supplement.author_id)), ''), 'a user') AS author_name;

-- name: AckTaskSupplementDelivered :one
-- The provider can accept input before completion while its HTTP acknowledgement
-- arrives afterward. Serialize with settlement and preserve that successful write.
UPDATE task_supplement
SET status = 'delivered',
    delivered_at = COALESCE(delivered_at, now()),
    failure_reason = NULL,
    updated_at = now()
WHERE task_supplement.task_id = @task_id
  AND task_supplement.comment_id = @comment_id
  AND EXISTS (SELECT 1 FROM agent_task_queue WHERE agent_task_queue.id = @task_id)
  AND (
      task_supplement.status IN ('delivering', 'delivered')
      OR (task_supplement.status = 'failed' AND task_supplement.failure_reason = 'turn_ended' AND task_supplement.attempt_count > 0)
  )
RETURNING *;

-- name: AckTaskSupplementFailed :one
UPDATE task_supplement
SET status = 'failed',
    failure_reason = @failure_reason,
    updated_at = now()
WHERE task_id = @task_id
  AND comment_id = @comment_id
  AND status = 'delivering'
RETURNING *;

-- name: RetryTaskSupplement :one
UPDATE task_supplement
SET status = 'pending',
    failure_reason = NULL,
    updated_at = now()
WHERE task_supplement.task_id = @task_id
  AND task_supplement.comment_id = @comment_id
  AND task_supplement.workspace_id = @workspace_id
  AND task_supplement.status = 'failed'
  AND EXISTS (
      SELECT 1 FROM comment
      WHERE comment.id = @comment_id AND comment.workspace_id = @workspace_id AND comment.deleted_at IS NULL
  )
  AND EXISTS (
      SELECT 1 FROM agent_task_queue
      WHERE agent_task_queue.id = @task_id AND agent_task_queue.issue_id = @issue_id AND agent_task_queue.status = 'running'
  )
  AND EXISTS (
      SELECT 1 FROM task_supplement_capability cap
      WHERE cap.task_id = @task_id AND cap.capability = 'task-supplement-v1'
  )
RETURNING *;

-- name: LockTaskSupplementsByComment :many
SELECT * FROM task_supplement
WHERE comment_id = @comment_id AND workspace_id = @workspace_id;

-- name: DeleteTaskSupplementByComment :exec
DELETE FROM task_supplement
WHERE comment_id = @comment_id AND workspace_id = @workspace_id;
