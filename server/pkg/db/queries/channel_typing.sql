-- name: RegisterChannelTypingReaction :one
-- Commit the complete external anchor BEFORE issuing Add. A terminal observer
-- can enumerate every input, independently of the one delivery trigger.
INSERT INTO channel_typing_reaction (
 id, workspace_id, chat_session_id, chat_message_id, installation_id,
 channel_message_id, installation_snapshot, cleanup_required, quota_slot
)
SELECT @id AS id, @workspace_id AS workspace_id, @chat_session_id AS chat_session_id,
 @chat_message_id AS chat_message_id, @installation_id AS installation_id,
 @channel_message_id AS channel_message_id, @installation_snapshot AS installation_snapshot,
 message.channel_typing_settled AS cleanup_required, slot.value AS quota_slot
FROM chat_message AS message
JOIN chat_session AS session ON session.id = message.chat_session_id
JOIN channel_installation AS installation ON installation.id = @installation_id
JOIN (
 SELECT seq.value AS value FROM (
  SELECT (d2.v * 100 + d1.v * 10 + d0.v + 1) AS value FROM (SELECT 0 AS v UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4 UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9) AS d2, (SELECT 0 AS v UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4 UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9) AS d1, (SELECT 0 AS v UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4 UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9) AS d0
 ) AS seq
 WHERE NOT EXISTS (SELECT 1 FROM channel_typing_reaction r
  WHERE r.workspace_id = @workspace_id AND r.quota_slot = seq.value
   AND r.cleaned_at IS NULL AND r.abandoned_at IS NULL)
 ORDER BY seq.value LIMIT 1
) AS slot ON 1 = 1
WHERE message.id = @chat_message_id
 AND session.id = @chat_session_id AND session.workspace_id = @workspace_id
 AND installation.workspace_id = @workspace_id AND installation.channel_type = 'feishu'
 AND message.channel_ingested AND message.role = 'user'
ON CONFLICT DO NOTHING RETURNING *;

-- name: FinishChannelTypingReactionAdd :one
-- cleanup_required is monotonic. A stale active read cannot undo another
-- replica's terminal decision; late HTTP completion re-arms compensation.
UPDATE channel_typing_reaction SET reaction_id = @reaction_id, add_finished = true,
 cleanup_required = cleanup_required OR @cleanup_required,
 cleaned_at = NULL, retry_after = now()
WHERE id = @id AND abandoned_at IS NULL AND created_at > datetime('now', '-7 days')
RETURNING *;

-- name: AcknowledgeChannelTypingReactionCleanup :exec
-- An unfinished Add may still create a reaction after an empty sweep. Retain
-- its anchor; only a known successful Add response can close cleanup forever.
UPDATE channel_typing_reaction
SET cleaned_at = CASE WHEN add_finished AND reaction_id <> '' THEN now() ELSE NULL END,
 retry_after = GREATEST(retry_after, datetime('now', '+30 seconds')),
 installation_snapshot = CASE WHEN add_finished AND reaction_id <> '' THEN '{}' ELSE installation_snapshot END
WHERE id = @id AND reaction_id = @reaction_id AND cleanup_required AND abandoned_at IS NULL;

-- name: ClaimChannelTypingReactionCleanup :many
-- The DB is the shared source of eligibility. Missing source rows after a
-- committed deletion are terminal too. No transaction takes remote I/O locks.
WITH candidates AS (
 SELECT r.id FROM channel_typing_reaction r
 WHERE r.cleaned_at IS NULL AND r.abandoned_at IS NULL AND r.retry_after <= now()
 AND r.created_at > datetime('now', '-7 days')
 AND (sqlc.narg('chat_session_id') IS NULL OR r.chat_session_id = sqlc.narg('chat_session_id'))
 AND (r.cleanup_required OR (NOT r.add_finished AND r.created_at < datetime('now', '-1 minutes')) OR NOT EXISTS (
  SELECT 1 FROM chat_message m
  JOIN chat_session s ON s.id = m.chat_session_id
  JOIN channel_installation i ON i.id = r.installation_id
  JOIN channel_chat_session_binding b ON b.chat_session_id = s.id AND b.installation_id = i.id
  LEFT JOIN agent_task_queue t ON t.id = m.task_id
  LEFT JOIN agent a ON a.id = t.agent_id
  WHERE m.id = r.chat_message_id AND s.id = r.chat_session_id
   AND s.workspace_id = r.workspace_id AND i.workspace_id = r.workspace_id
   AND i.channel_type = 'feishu' AND b.channel_type = 'feishu'
   AND s.status = 'active' AND i.status = 'active' AND b.retired_at IS NULL
   AND m.channel_ingested AND m.role = 'user' AND NOT m.channel_typing_settled
   -- A taskless badge has a bounded visual lifetime even if a crashed
   -- debouncer never flushes. Do not settle the input or cancel a future run.
   AND ((m.task_id IS NULL AND r.created_at > datetime('now', '-2 minutes')) OR (t.chat_session_id = s.id AND a.workspace_id = r.workspace_id
     AND t.status IN ('queued','dispatched','running','waiting_local_directory','deferred')))
 ))
 ORDER BY r.retry_after, r.id LIMIT 100
)
UPDATE channel_typing_reaction AS r SET  cleanup_required = (SELECT true FROM candidates c WHERE r.id = c.id),
 attempts = (SELECT attempts + 1 FROM candidates c WHERE r.id = c.id),
 retry_after = (SELECT datetime(now(), '+' || (LEAST(3600, 30 * power(2, LEAST(r.attempts, 7)))) || ' seconds') AS retry_after FROM candidates c WHERE r.id = c.id) WHERE EXISTS (SELECT 1 FROM candidates c WHERE r.id = c.id) RETURNING *;

-- name: SettleChannelTypingInputs :exec
-- The flush owns only this context's unowned inputs up to its captured cutoff.
-- Later inputs, even in the same session, are not settled by this failed run.
UPDATE chat_message SET  channel_typing_settled = (SELECT true FROM chat_message AS cutoff, chat_session AS session, channel_installation AS installation WHERE cutoff.id = @through_message_id AND cutoff.chat_session_id = @chat_session_id
 AND session.id = @chat_session_id AND session.workspace_id = @workspace_id
 AND installation.id = @installation_id AND installation.workspace_id = @workspace_id
 AND installation.channel_type = 'feishu'
 AND EXISTS (SELECT 1 FROM channel_chat_session_binding b WHERE b.chat_session_id = session.id
  AND b.installation_id = installation.id AND b.channel_type = 'feishu')
 AND chat_message.chat_session_id = session.id AND chat_message.task_id IS NULL
 AND chat_message.channel_ingested AND chat_message.role = 'user'
 AND COALESCE(chat_message.channel_context_revision, 1) = @context_revision
 AND (chat_message.created_at, chat_message.id) <= (cutoff.created_at, cutoff.id)) WHERE EXISTS (SELECT 1 FROM chat_message AS cutoff, chat_session AS session, channel_installation AS installation WHERE cutoff.id = @through_message_id AND cutoff.chat_session_id = @chat_session_id
 AND session.id = @chat_session_id AND session.workspace_id = @workspace_id
 AND installation.id = @installation_id AND installation.workspace_id = @workspace_id
 AND installation.channel_type = 'feishu'
 AND EXISTS (SELECT 1 FROM channel_chat_session_binding b WHERE b.chat_session_id = session.id
  AND b.installation_id = installation.id AND b.channel_type = 'feishu')
 AND chat_message.chat_session_id = session.id AND chat_message.task_id IS NULL
 AND chat_message.channel_ingested AND chat_message.role = 'user'
 AND COALESCE(chat_message.channel_context_revision, 1) = @context_revision
 AND (chat_message.created_at, chat_message.id) <= (cutoff.created_at, cutoff.id));

-- name: PruneChannelTypingReactionCleanup :exec
-- Each terminal outcome remains inspectable for seven days without credentials.
WITH candidates AS (
 SELECT id FROM channel_typing_reaction
 WHERE cleaned_at < datetime('now', '-7 days') OR abandoned_at < datetime('now', '-7 days')
 LIMIT 1000
)
DELETE FROM channel_typing_reaction WHERE id IN (SELECT c.id FROM candidates c);

-- name: ExpireChannelTypingReactionCleanup :many
-- Independent maintenance, including active/uncertain Adds: the cosmetic badge
-- has a seven-day maximum lifetime. Remote failure is NOT reported as success.
WITH candidates AS (
 SELECT id FROM channel_typing_reaction
 WHERE cleaned_at IS NULL AND abandoned_at IS NULL AND created_at <= datetime('now', '-7 days')
 ORDER BY created_at, id LIMIT 1000
)
UPDATE channel_typing_reaction AS r SET  abandoned_at = (SELECT now() FROM candidates c WHERE r.id = c.id), cleanup_required = (SELECT true FROM candidates c WHERE r.id = c.id),
 installation_snapshot = (SELECT '{}' FROM candidates c WHERE r.id = c.id) WHERE EXISTS (SELECT 1 FROM candidates c WHERE r.id = c.id) RETURNING r.id, r.workspace_id;

-- name: SkipChannelTypingReactionAdd :exec
-- No HTTP Add was issued for an already-settled input.
UPDATE channel_typing_reaction SET add_finished = true, cleanup_required = true,
 cleaned_at = now(), installation_snapshot = '{}' WHERE id = ?1 AND abandoned_at IS NULL;

-- name: ListRequestedChannelTypingReactions :many
SELECT id FROM channel_typing_reaction WHERE id IN (SELECT value FROM json_each(@ids)) AND cleanup_required;
