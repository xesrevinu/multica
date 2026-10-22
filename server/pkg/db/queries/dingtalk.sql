-- DingTalk-specific installation identity operations. The underlying channel_*
-- tables are shared, but these replacement semantics belong to DingTalk's BYO
-- AppKey model and deliberately stay out of the shared channel query surface.

-- name: ListDingTalkUserBindingsForMember :many
-- Returns only the requesting Multica member's DingTalk identities. The
-- installation list is member-visible, so returning every member's staff id
-- here would expose staff ID values more broadly than necessary.
SELECT installation_id, channel_user_id
FROM channel_user_binding
WHERE workspace_id = sqlc.arg(workspace_id)
  AND multica_user_id = sqlc.arg(multica_user_id)
  AND channel_type = 'dingtalk'
ORDER BY bound_at DESC, id ASC;

-- name: LockDingTalkInstallationOwner :exec
-- Serializes install / replacement decisions for one logical
-- (workspace, agent, channel) slot. A different-AppKey replacement deletes the
-- old row and inserts a fresh installation id; the advisory lock closes the
-- gap where two concurrent replacements could otherwise miss each other's new
-- row and update it in place, carrying identity state across robot boundaries.
SELECT 1;

-- name: GetDingTalkInstallationOwnerForUpdate :one
-- Reads the current robot identity after LockDingTalkInstallationOwner has
-- serialized the logical owner slot. app_id is non-null for every DingTalk
-- installation; COALESCE treats malformed legacy config as a different robot,
-- which safely replaces it instead of preserving unknown identity state.
SELECT id, COALESCE(config ->> 'app_id', '') AS app_id
FROM channel_installation
WHERE workspace_id = sqlc.arg(workspace_id)
  AND agent_id = sqlc.arg(agent_id)
  AND channel_type = 'dingtalk';

-- name: UpsertDingTalkBotIdentity :one
-- Bot identity and the optional permission issue belong to the installation,
-- not to any one group. Keep this as a separate command so the mixed-version
-- compatibility trigger can observe the completed command state when the
-- group-presence command runs next.
INSERT INTO dingtalk_bot_identity (
    workspace_id, installation_id, bot_name, bot_identity_issue
)
SELECT
    c.workspace_id AS workspace_id,
    c.id AS installation_id,
    COALESCE(NULLIF(sqlc.arg(bot_name), ''), COALESCE(identity.bot_name, '')) AS bot_name,
    CASE
        WHEN sqlc.arg(bot_identity_issue) <> '' THEN sqlc.arg(bot_identity_issue)
        WHEN sqlc.arg(bot_name) <> '' THEN ''
        ELSE COALESCE(identity.bot_identity_issue, '')
    END AS bot_identity_issue
FROM channel_installation c
LEFT JOIN dingtalk_bot_identity identity ON identity.installation_id = c.id
WHERE c.id = sqlc.arg(installation_id)
  AND c.workspace_id = sqlc.arg(workspace_id)
  AND c.channel_type = 'dingtalk'
  AND c.status = 'active'
ON CONFLICT (installation_id) DO UPDATE SET
    workspace_id = excluded.workspace_id,
    bot_name = excluded.bot_name,
    bot_identity_issue = excluded.bot_identity_issue,
    updated_at = CASE
        WHEN dingtalk_bot_identity.workspace_id IS NOT excluded.workspace_id
          OR dingtalk_bot_identity.bot_name IS NOT excluded.bot_name
          OR dingtalk_bot_identity.bot_identity_issue IS NOT excluded.bot_identity_issue
        THEN now()
        ELSE dingtalk_bot_identity.updated_at
    END
RETURNING installation_id;

-- name: RecordDingTalkGroupPresence :one
-- Records an observed bot/group pair without carrying routing state. The
-- installation identity is copied only for compatibility with the preceding
-- draft; readers use dingtalk_bot_identity as the authority.
-- Order: RecordDingTalkGroupPresence, then RecordDingTalkGroupPresence_Step2.
-- Step2 copies the current identity onto the installation's other groups.
INSERT INTO dingtalk_group_presence (
    workspace_id, installation_id, conversation_id, conversation_title,
    bot_name, bot_identity_issue
)
SELECT
    c.workspace_id AS workspace_id,
    c.id AS installation_id,
    sqlc.arg(conversation_id) AS conversation_id,
    sqlc.arg(conversation_title) AS conversation_title,
    identity.bot_name AS bot_name,
    identity.bot_identity_issue AS bot_identity_issue
FROM channel_installation c
JOIN dingtalk_bot_identity identity ON identity.installation_id = c.id
WHERE c.id = sqlc.arg(installation_id)
  AND c.workspace_id = sqlc.arg(workspace_id)
  AND c.channel_type = 'dingtalk'
  AND c.status = 'active'
ON CONFLICT (installation_id, conversation_id) DO UPDATE SET
    conversation_title = CASE
        WHEN excluded.conversation_title <> '' THEN excluded.conversation_title
        ELSE dingtalk_group_presence.conversation_title
    END,
    bot_name = excluded.bot_name,
    bot_identity_issue = excluded.bot_identity_issue,
    updated_at = CASE
        WHEN (
            dingtalk_group_presence.conversation_title IS NOT excluded.conversation_title
            AND excluded.conversation_title <> ''
        )
           OR dingtalk_group_presence.bot_name IS NOT excluded.bot_name
           OR dingtalk_group_presence.bot_identity_issue IS NOT excluded.bot_identity_issue
        THEN now()
        ELSE dingtalk_group_presence.updated_at
    END
RETURNING dingtalk_group_presence.installation_id;

-- name: RecordDingTalkGroupPresence_Step2 :exec
UPDATE dingtalk_group_presence
SET bot_name = (
        SELECT identity.bot_name FROM dingtalk_bot_identity identity
        WHERE identity.installation_id = dingtalk_group_presence.installation_id
    ),
    bot_identity_issue = (
        SELECT identity.bot_identity_issue FROM dingtalk_bot_identity identity
        WHERE identity.installation_id = dingtalk_group_presence.installation_id
    ),
    updated_at = now()
WHERE dingtalk_group_presence.installation_id = sqlc.arg(installation_id)
  AND dingtalk_group_presence.conversation_id <> sqlc.arg(conversation_id)
  AND EXISTS (
      SELECT 1 FROM dingtalk_bot_identity identity
      WHERE identity.installation_id = dingtalk_group_presence.installation_id
        AND (
            dingtalk_group_presence.bot_name IS NOT identity.bot_name
            OR dingtalk_group_presence.bot_identity_issue IS NOT identity.bot_identity_issue
        )
  );

-- name: RecordDingTalkGroupActivity :one
-- Advances activity only after the deduplicated inbound message is durable.
-- Presence observation runs first, so a forgotten group is recreated before
-- this update executes.
UPDATE dingtalk_group_presence
SET last_active_at = now(), mention_count = mention_count + 1, updated_at = now()
WHERE installation_id = sqlc.arg(installation_id)
  AND conversation_id = sqlc.arg(conversation_id)
RETURNING installation_id;

-- name: ListDingTalkGroupPresencesByWorkspace :many
-- Active rows are the default inventory. Inactive rows are loaded only when a
-- client expands one installation's historical section.
SELECT
    presence.conversation_id,
    presence.conversation_title,
    presence.installation_id,
    installation.agent_id,
    COALESCE(identity.bot_name, presence.bot_name) AS bot_name,
    COALESCE(identity.bot_identity_issue, presence.bot_identity_issue) AS bot_identity_issue,
    presence.last_active_at,
    presence.mention_count
FROM dingtalk_group_presence presence
JOIN channel_installation installation ON installation.id = presence.installation_id
LEFT JOIN dingtalk_bot_identity identity ON identity.installation_id = presence.installation_id
WHERE installation.workspace_id = sqlc.arg(workspace_id)
  AND installation.channel_type = 'dingtalk'
  AND installation.status = 'active'
  AND (NOT sqlc.arg(filter_by_agent) OR installation.agent_id = sqlc.arg(agent_id))
  AND (NOT sqlc.arg(filter_by_installation) OR installation.id = sqlc.arg(filter_installation_id))
  AND (
      (NOT sqlc.arg(include_inactive) AND presence.last_active_at >= sqlc.arg(active_since))
      OR (sqlc.arg(include_inactive) AND (presence.last_active_at < sqlc.arg(active_since) OR presence.last_active_at IS NULL))
  )
ORDER BY presence.last_active_at DESC, presence.conversation_title ASC,
         presence.conversation_id ASC, installation.installed_at ASC, installation.id ASC
LIMIT NULLIF(sqlc.arg(page_limit), 0)
OFFSET sqlc.arg(page_offset);

-- name: CountInactiveDingTalkGroupPresencesByWorkspace :many
SELECT presence.installation_id, installation.agent_id, count(*) AS group_count
FROM dingtalk_group_presence presence
JOIN channel_installation installation ON installation.id = presence.installation_id
WHERE installation.workspace_id = sqlc.arg(workspace_id)
  AND installation.channel_type = 'dingtalk'
  AND installation.status = 'active'
  AND (presence.last_active_at < sqlc.arg(active_since) OR presence.last_active_at IS NULL)
  AND (NOT sqlc.arg(filter_by_agent) OR installation.agent_id = sqlc.arg(agent_id))
GROUP BY presence.installation_id, installation.agent_id;

-- name: ListDingTalkBotIdentitiesByWorkspace :many
-- Identity is installation-owned, so forgetting or collapsing groups cannot
-- change the connection label.
SELECT
    installation.id AS installation_id,
    installation.agent_id,
    identity.bot_name,
    identity.bot_identity_issue
FROM channel_installation installation
JOIN dingtalk_bot_identity identity ON identity.installation_id = installation.id
WHERE installation.workspace_id = sqlc.arg(workspace_id)
  AND installation.channel_type = 'dingtalk'
  AND installation.status = 'active'
  AND (NOT sqlc.arg(filter_by_agent) OR installation.agent_id = sqlc.arg(agent_id));

-- name: ForgetDingTalkGroupPresence :one
DELETE FROM dingtalk_group_presence
WHERE dingtalk_group_presence.workspace_id = sqlc.arg(workspace_id)
  AND dingtalk_group_presence.installation_id = sqlc.arg(installation_id)
  AND dingtalk_group_presence.conversation_id = sqlc.arg(conversation_id)
  AND EXISTS (
      SELECT 1 FROM channel_installation installation
      WHERE installation.id = dingtalk_group_presence.installation_id
        AND installation.workspace_id = sqlc.arg(workspace_id)
        AND installation.channel_type = 'dingtalk'
  )
RETURNING installation_id;

-- name: DeleteDingTalkInstallationForReplacement :one
-- Retires an installation when the same agent is connected with a DIFFERENT
-- AppKey. A senderStaffId is scoped to one DingTalk organization, so none of the
-- old installation's identity, token, session, dedup, or outbound state may
-- cross into the new robot. The caller inserts a fresh installation in the same
-- transaction, giving the replacement a new installation_id that also fences
-- late writes and replies from the old connection.
--
-- Chat sessions themselves remain as history, but their channel bindings and
-- outbound cards are removed. Audit and media-intent rows remain useful for
-- diagnostics / reconciliation, so their installation references are detached.
-- Order: DeleteDingTalkInstallationForReplacement_Step2 through Step13, then
-- this statement. The steps clear child rows while the installation still
-- matches; this statement deletes it and returns the id.
DELETE FROM channel_installation
WHERE id = sqlc.arg(installation_id)
  AND workspace_id = sqlc.arg(workspace_id)
  AND agent_id = sqlc.arg(agent_id)
  AND channel_type = 'dingtalk'
RETURNING id;

-- name: DeleteDingTalkInstallationForReplacement_Step2 :exec
DELETE FROM dingtalk_group_presence
WHERE installation_id = sqlc.arg(installation_id)
  AND EXISTS (
      SELECT 1 FROM channel_installation ci
      WHERE ci.id = sqlc.arg(installation_id)
        AND ci.workspace_id = sqlc.arg(workspace_id)
        AND ci.agent_id = sqlc.arg(agent_id)
        AND ci.channel_type = 'dingtalk'
  );

-- name: DeleteDingTalkInstallationForReplacement_Step3 :exec
DELETE FROM dingtalk_bot_identity
WHERE installation_id = sqlc.arg(installation_id)
  AND EXISTS (
      SELECT 1 FROM channel_installation ci
      WHERE ci.id = sqlc.arg(installation_id)
        AND ci.workspace_id = sqlc.arg(workspace_id)
        AND ci.agent_id = sqlc.arg(agent_id)
        AND ci.channel_type = 'dingtalk'
  );

-- name: DeleteDingTalkInstallationForReplacement_Step4 :exec
DELETE FROM dingtalk_group_route
WHERE installation_id = sqlc.arg(installation_id)
  AND EXISTS (
      SELECT 1 FROM channel_installation ci
      WHERE ci.id = sqlc.arg(installation_id)
        AND ci.workspace_id = sqlc.arg(workspace_id)
        AND ci.agent_id = sqlc.arg(agent_id)
        AND ci.channel_type = 'dingtalk'
  );

-- name: DeleteDingTalkInstallationForReplacement_Step5 :exec
DELETE FROM channel_task_delivery
WHERE installation_id = sqlc.arg(installation_id)
  AND EXISTS (
      SELECT 1 FROM channel_installation ci
      WHERE ci.id = sqlc.arg(installation_id)
        AND ci.workspace_id = sqlc.arg(workspace_id)
        AND ci.agent_id = sqlc.arg(agent_id)
        AND ci.channel_type = 'dingtalk'
  );

-- name: DeleteDingTalkInstallationForReplacement_Step6 :exec
DELETE FROM channel_outbound_message
WHERE installation_id = sqlc.arg(installation_id)
  AND EXISTS (
      SELECT 1 FROM channel_installation ci
      WHERE ci.id = sqlc.arg(installation_id)
        AND ci.workspace_id = sqlc.arg(workspace_id)
        AND ci.agent_id = sqlc.arg(agent_id)
        AND ci.channel_type = 'dingtalk'
  );

-- name: DeleteDingTalkInstallationForReplacement_Step7 :exec
DELETE FROM channel_outbound_card_message
WHERE chat_session_id IN (
    SELECT chat_session_id FROM channel_chat_session_binding
    WHERE installation_id = sqlc.arg(installation_id)
)
  AND EXISTS (
      SELECT 1 FROM channel_installation ci
      WHERE ci.id = sqlc.arg(installation_id)
        AND ci.workspace_id = sqlc.arg(workspace_id)
        AND ci.agent_id = sqlc.arg(agent_id)
        AND ci.channel_type = 'dingtalk'
  );

-- name: DeleteDingTalkInstallationForReplacement_Step8 :exec
DELETE FROM channel_chat_session_binding
WHERE installation_id = sqlc.arg(installation_id)
  AND EXISTS (
      SELECT 1 FROM channel_installation ci
      WHERE ci.id = sqlc.arg(installation_id)
        AND ci.workspace_id = sqlc.arg(workspace_id)
        AND ci.agent_id = sqlc.arg(agent_id)
        AND ci.channel_type = 'dingtalk'
  );

-- name: DeleteDingTalkInstallationForReplacement_Step9 :exec
DELETE FROM channel_binding_token
WHERE installation_id = sqlc.arg(installation_id)
  AND EXISTS (
      SELECT 1 FROM channel_installation ci
      WHERE ci.id = sqlc.arg(installation_id)
        AND ci.workspace_id = sqlc.arg(workspace_id)
        AND ci.agent_id = sqlc.arg(agent_id)
        AND ci.channel_type = 'dingtalk'
  );

-- name: DeleteDingTalkInstallationForReplacement_Step10 :exec
DELETE FROM channel_user_binding
WHERE installation_id = sqlc.arg(installation_id)
  AND EXISTS (
      SELECT 1 FROM channel_installation ci
      WHERE ci.id = sqlc.arg(installation_id)
        AND ci.workspace_id = sqlc.arg(workspace_id)
        AND ci.agent_id = sqlc.arg(agent_id)
        AND ci.channel_type = 'dingtalk'
  );

-- name: DeleteDingTalkInstallationForReplacement_Step11 :exec
DELETE FROM channel_inbound_message_dedup
WHERE installation_id = sqlc.arg(installation_id)
  AND EXISTS (
      SELECT 1 FROM channel_installation ci
      WHERE ci.id = sqlc.arg(installation_id)
        AND ci.workspace_id = sqlc.arg(workspace_id)
        AND ci.agent_id = sqlc.arg(agent_id)
        AND ci.channel_type = 'dingtalk'
  );

-- name: DeleteDingTalkInstallationForReplacement_Step12 :exec
UPDATE channel_inbound_audit
SET installation_id = NULL
WHERE installation_id = sqlc.arg(installation_id)
  AND EXISTS (
      SELECT 1 FROM channel_installation ci
      WHERE ci.id = sqlc.arg(installation_id)
        AND ci.workspace_id = sqlc.arg(workspace_id)
        AND ci.agent_id = sqlc.arg(agent_id)
        AND ci.channel_type = 'dingtalk'
  );

-- name: DeleteDingTalkInstallationForReplacement_Step13 :exec
UPDATE channel_media_pending_object
SET installation_id = NULL
WHERE installation_id = sqlc.arg(installation_id)
  AND EXISTS (
      SELECT 1 FROM channel_installation ci
      WHERE ci.id = sqlc.arg(installation_id)
        AND ci.workspace_id = sqlc.arg(workspace_id)
        AND ci.agent_id = sqlc.arg(agent_id)
        AND ci.channel_type = 'dingtalk'
  );
