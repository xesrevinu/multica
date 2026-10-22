-- name: ListWorkspaces :many
SELECT w.id, w.name, w.slug, w.description, w.settings,
       w.created_at, w.updated_at, w.context, w.repos,
       w.issue_prefix, w.issue_counter, w.avatar_url, w.attribution_fail_closed
FROM member m
JOIN workspace w ON w.id = m.workspace_id
WHERE m.user_id = ?1
ORDER BY w.created_at ASC;

-- name: ListDaemonWorkspaces :many
-- Daemons only need the membership set and display name to discover which
-- workspaces should have local runtimes. Keep this projection intentionally
-- narrow so the periodic consistency check never reads UI-only JSON/text
-- columns such as settings, repos, or context.
SELECT w.id, w.name
FROM member m
JOIN workspace w ON w.id = m.workspace_id
WHERE m.user_id = ?1
ORDER BY w.id ASC;

-- name: GetDaemonWorkspace :one
-- Workspace-scoped daemon tokens do not carry a user ID. This narrow lookup
-- lets them use the same endpoint without widening their token scope.
SELECT id, name
FROM workspace
WHERE id = ?1;

-- name: GetWorkspace :one
SELECT * FROM workspace
WHERE id = ?1;

-- name: GetWorkspaceBySlug :one
SELECT * FROM workspace
WHERE slug = ?1;

-- name: GetWorkspaceAttributionFailClosed :one
-- Lean read of the fail-closed attribution policy for the enqueue hot path
-- (MUL-4302  3.5), avoiding a full workspace-row fetch.
SELECT attribution_fail_closed FROM workspace
WHERE id = ?1;

-- name: CreateWorkspace :one
INSERT INTO workspace (id, name, slug, description, context, issue_prefix)
VALUES (gen_random_uuid(), ?1, ?2, ?3, ?4, ?5)
RETURNING *;

-- name: UpdateWorkspace :one
UPDATE workspace SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    context = COALESCE(sqlc.narg('context'), context),
    settings = COALESCE(sqlc.narg('settings'), settings),
    repos = COALESCE(sqlc.narg('repos'), repos),
    issue_prefix = COALESCE(sqlc.narg('issue_prefix'), issue_prefix),
    avatar_url = COALESCE(sqlc.narg('avatar_url'), avatar_url),
    updated_at = now()
WHERE id = ?1
RETURNING *;

-- name: IncrementIssueCounter :one
UPDATE workspace SET issue_counter = issue_counter + 1
WHERE id = ?1
RETURNING issue_counter;

-- name: LockWorkspaceForDelete :one
-- Taken first by DeleteWorkspace, before it enumerates the workspace's chat
-- sessions. LockChatSessionsByWorkspace only covers sessions that exist when it
-- runs; a CreateChatSession committing during the delete window would add one
-- the lock set never saw, and a finalizer could then insert a restore for it
-- after the sweep's snapshot - orphaning the prompt (#5219).
--
-- The delete window is held closed against new sessions by an EXPLICIT protocol,
-- not the chat_session.workspace_id FK: every session creator takes
-- LockWorkspaceForChatSessionCreate (FOR KEY SHARE) on this row first, and this
-- conflicts with it. Keeping the bar in the app layer means it does
-- not silently break if that FK is ever dropped (the codebase is moving FK
-- relationships into the application layer, MUL-3515). Lock order is
-- workspace -> chat_session -> agent_task_queue; the finalizer never touches
-- workspace, so this cannot deadlock against it.
SELECT id FROM workspace WHERE id = ?1;

-- name: LockWorkspaceForChatSessionCreate :one
-- The creator half of the workspace delete/create protocol (#5219). Every
-- production path that inserts a chat_session takes this lock on the
-- parent workspace row, inside its transaction, before CreateChatSession. It
-- conflicts with DeleteWorkspace's (so a create is blocked while a
-- delete is in progress, and vice versa) but not with other creators (FOR KEY
-- SHARE locks share), so concurrent session creation stays unserialized. This
-- makes the mutual exclusion explicit rather than leaning on the workspace FK's
-- implicit, which would vanish if that FK is dropped.
SELECT id FROM workspace WHERE id = ?1;

-- name: DeleteWorkspace :exec
-- The channel_* tables (MUL-3515  4), resource-label junctions, custom issue
-- property definitions, and quick actions carry NO FK to workspace, so - unlike the CASCADE-backed
-- tables the DELETE below sweeps - they are not cleaned up implicitly. Remove
-- their workspace-owned rows here so they commit or roll back atomically with
-- the workspace row.
DELETE FROM agent_to_label WHERE agent_id IN (SELECT id FROM (SELECT id FROM agent WHERE agent.workspace_id = ?1));

-- name: DeleteWorkspace_Step2 :exec
DELETE FROM skill_to_label WHERE skill_id IN (SELECT id FROM (SELECT id FROM skill WHERE skill.workspace_id = ?1));

-- name: DeleteWorkspace_Step3 :exec
DELETE FROM channel_task_delivery WHERE installation_id IN (SELECT id FROM (SELECT id FROM channel_installation WHERE channel_installation.workspace_id = ?1));

-- name: DeleteWorkspace_Step4 :exec
DELETE FROM channel_outbound_message WHERE installation_id IN (SELECT id FROM (SELECT id FROM channel_installation WHERE channel_installation.workspace_id = ?1));

-- name: DeleteWorkspace_Step5 :exec
DELETE FROM channel_reply_delivery WHERE installation_id IN (SELECT id FROM (SELECT id FROM channel_installation WHERE channel_installation.workspace_id = ?1));

-- name: DeleteWorkspace_Step6 :exec
DELETE FROM channel_outbound_card_message
WHERE chat_session_id IN (
    SELECT chat_session_id FROM channel_chat_session_binding
    WHERE installation_id IN (SELECT id FROM channel_installation WHERE channel_installation.workspace_id = ?1)
);

-- name: DeleteWorkspace_Step7 :exec
DELETE FROM channel_chat_context_generation
WHERE chat_session_id IN (SELECT id FROM chat_session WHERE chat_session.workspace_id = ?1);

-- name: DeleteWorkspace_Step7b :exec
DELETE FROM channel_chat_session_binding
WHERE installation_id IN (SELECT id FROM channel_installation WHERE channel_installation.workspace_id = ?1)
   OR chat_session_id IN (SELECT id FROM chat_session WHERE chat_session.workspace_id = ?1);

-- name: DeleteWorkspace_Step7c :exec
DELETE FROM chat_draft_restore
WHERE chat_session_id IN (SELECT id FROM chat_session WHERE chat_session.workspace_id = ?1);

-- name: DeleteWorkspace_Step7d :exec
DELETE FROM channel_inbound_audit
WHERE installation_id IN (SELECT id FROM channel_installation WHERE channel_installation.workspace_id = ?1);

-- name: DeleteWorkspace_Step7e :exec
DELETE FROM github_pull_request_check_run
WHERE pr_id IN (SELECT id FROM github_pull_request WHERE github_pull_request.workspace_id = ?1);

-- name: DeleteWorkspace_Step7f :exec
DELETE FROM github_pull_request_check_suite
WHERE pr_id IN (SELECT id FROM github_pull_request WHERE github_pull_request.workspace_id = ?1);

-- name: DeleteWorkspace_Step8 :exec
DELETE FROM channel_inbound_message_dedup WHERE installation_id IN (SELECT id FROM (SELECT id FROM channel_installation WHERE channel_installation.workspace_id = ?1));

-- name: DeleteWorkspace_Step9 :exec
DELETE FROM dingtalk_group_presence WHERE installation_id IN (SELECT id FROM (SELECT id FROM channel_installation WHERE channel_installation.workspace_id = ?1));

-- name: DeleteWorkspace_Step10 :exec
DELETE FROM dingtalk_bot_identity WHERE installation_id IN (SELECT id FROM (SELECT id FROM channel_installation WHERE channel_installation.workspace_id = ?1));

-- name: DeleteWorkspace_Step11 :exec
DELETE FROM dingtalk_group_route WHERE workspace_id = ?1;

-- name: DeleteWorkspace_Step12 :exec
DELETE FROM channel_user_binding WHERE workspace_id = ?1;

-- name: DeleteWorkspace_Step13 :exec
DELETE FROM channel_binding_token WHERE workspace_id = ?1;

-- name: DeleteWorkspace_Step14 :exec
DELETE FROM channel_installation WHERE channel_installation.workspace_id = ?1;

-- name: DeleteWorkspace_Step15 :exec
DELETE FROM issue_property WHERE workspace_id = ?1;

-- name: DeleteWorkspace_Step16 :exec
DELETE FROM quick_action WHERE workspace_id = ?1;

-- name: DeleteWorkspace_Step17 :exec
DELETE FROM workspace_mcp_server WHERE workspace_mcp_server.workspace_id = ?1;

-- name: DeleteWorkspace_Step18 :exec
DELETE FROM github_pending_check_suite WHERE workspace_id = ?1;

-- name: DeleteWorkspace_Step19 :exec
DELETE FROM issue_vcs_pull_request
    WHERE pull_request_id IN (SELECT id FROM (SELECT id FROM vcs_pull_request WHERE vcs_pull_request.workspace_id = ?1));

-- name: DeleteWorkspace_Step20 :exec
DELETE FROM vcs_commit_status
    WHERE connection_id IN (SELECT id FROM (SELECT id FROM vcs_connection WHERE vcs_connection.workspace_id = ?1));

-- name: DeleteWorkspace_Step21 :exec
DELETE FROM vcs_pull_request WHERE vcs_pull_request.workspace_id = ?1;

-- name: DeleteWorkspace_Step22 :exec
DELETE FROM vcs_connection WHERE vcs_connection.workspace_id = ?1;

-- name: DeleteWorkspace_Step23 :exec
UPDATE client_usage_daily SET workspace_id = NULL WHERE workspace_id = ?1;

-- name: DeleteWorkspace_Step24 :exec
DELETE FROM workspace WHERE workspace.id = ?1;
