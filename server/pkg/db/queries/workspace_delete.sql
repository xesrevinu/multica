-- Workspace deletion is application-owned. These statements form a fixed,
-- bottom-up deletion plan; the legacy foreign keys remain only as a
-- compatibility safety net during the expand phase.

-- name: SetWorkspaceTeardownMode :exec
-- The setting is transaction-local. Migration 242 makes only the three DELETE
-- dirty triggers skip their per-row work while this flag is on.
SELECT 1;

-- name: LockTaskUsageRollupForWorkspaceDelete :exec
-- Advisory lock 4246 is the hourly rollup family's key: the rollup function
-- takes it per transaction (migration 272) and the backfill commands take it
-- per session. An in-flight rollup finishes first, then no new rollup can
-- write the workspace's aggregates until this delete commits. The caller
-- bounds this wait with SET LOCAL lock_timeout - batch jobs hold 4246 for
-- minutes, and an unbounded wait here hangs the delete request (MUL-5983).
SELECT 1;

-- name: LockWorkspaceTaskOwnerAgents :exec
-- The three LockWorkspaceTaskOwner* statements are a write fence, not a lookup -
-- they deliberately return nothing, so a workspace with a huge owner set costs
-- the API process no memory at all.
--
-- agent_task_queue has no workspace_id, so a task becomes this workspace's
-- problem through agent_id, issue_id or runtime_id - and inserting such a task
-- requires on the referenced owner row, which conflicts with
--. Holding these locks for the rest of the teardown transaction
-- therefore blocks any enqueue (or reassignment) that would add a task to this
-- workspace after we have swept it, which row locks on the tasks alone cannot
-- do. Same technique the handler already uses on the workspace row to keep
-- CreateChatSession out of the delete window.
--
-- This fence is defence in depth, not the only guarantee: deleteWorkspaceTasks
-- re-verifies every owner after sweeping it and fails closed if tasks keep
-- appearing, so teardown does not silently leak rows even if the fence is ever
-- weakened (for example by removing the legacy FKs this one leans on).
SELECT 1 FROM agent WHERE agent.workspace_id = ?1;

-- name: LockWorkspaceTaskOwnerIssues :exec
SELECT 1 FROM issue WHERE issue.workspace_id = ?1;

-- name: LockWorkspaceTaskOwnerRuntimes :exec
SELECT 1 FROM agent_runtime WHERE agent_runtime.workspace_id = ?1;

-- name: ListWorkspaceAgentIDFirstPage :many
-- First page of the same walk. Split from the keyset query rather than seeded
-- with the all-zero uuid, because that value is itself a valid uuid: a row whose
-- id happened to be all zeros would be skipped forever by `id > arg2`.
SELECT id FROM agent
WHERE agent.workspace_id = ?1
ORDER BY id
LIMIT ?2;

-- name: ListWorkspaceAgentIDPage :many
-- Owner enumeration in bounded, keyset-paged chunks. The rows are already locked
-- by LockWorkspaceTaskOwnerAgents above; this only walks them.
--
-- Keyset on id, not on a natural key: the cursor column must be immutable, and
-- renaming an agent mid-teardown would otherwise let it sort behind the cursor
-- and escape the sweep. Uses idx_agent_workspace_id_keyset (migration 281), which
-- exists so this page does not sort the whole owner set.
SELECT id FROM agent
WHERE agent.workspace_id = ?1 AND id > ?2
ORDER BY id
LIMIT ?3;

-- name: ListWorkspaceIssueIDFirstPage :many
-- First page of the same walk. Split from the keyset query rather than seeded
-- with the all-zero uuid, because that value is itself a valid uuid: a row whose
-- id happened to be all zeros would be skipped forever by `id > arg2`.
SELECT id FROM issue
WHERE issue.workspace_id = ?1
ORDER BY id
LIMIT ?2;

-- name: ListWorkspaceIssueIDPage :many
-- Uses idx_issue_workspace_id_keyset (migration 282).
SELECT id FROM issue
WHERE issue.workspace_id = ?1 AND id > ?2
ORDER BY id
LIMIT ?3;

-- name: ListWorkspaceRuntimeIDFirstPage :many
-- First page of the same walk. Split from the keyset query rather than seeded
-- with the all-zero uuid, because that value is itself a valid uuid: a row whose
-- id happened to be all zeros would be skipped forever by `id > arg2`.
SELECT id FROM agent_runtime
WHERE agent_runtime.workspace_id = ?1
ORDER BY id
LIMIT ?2;

-- name: ListWorkspaceRuntimeIDPage :many
-- Uses idx_agent_runtime_workspace_id_keyset (migration 283).
SELECT id FROM agent_runtime
WHERE agent_runtime.workspace_id = ?1 AND id > ?2
ORDER BY id
LIMIT ?3;

-- name: ListTaskIDsByAgentFirstPage :many
-- First page of the same walk; see ListWorkspaceAgentIDFirstPage for why this is
-- a separate query rather than a zero-uuid cursor, and ListTaskIDsByAgentPage for
-- the single-key / keyset / rationale.
SELECT id FROM agent_task_queue
WHERE agent_id = ?1
ORDER BY id
LIMIT ?2;

-- name: ListTaskIDsByAgentPage :many
-- One owner key per call, one keyset-paged chunk per call.
--
-- Single-key equality is deliberate. All three ownership paths must stay (nothing
-- enforces that a task's agent/runtime/issue share a workspace), but expressing
-- them as `OR agent_id IN (subquery) OR ...` - or as a UNION of joins, or as
-- `IN (SELECT value FROM json_each(arg1))` over a whole workspace's agents - makes the planner
-- estimate a proportional share of the global table and fall back to a Seq Scan
-- on agent_task_queue. A single-key equality is the only form whose row estimate
-- stays small enough to be index-driven regardless of how the workspace's tasks
-- are distributed (MUL-5999).
--
-- `id > arg2 ORDER BY id LIMIT arg3` against idx_agent_task_queue_agent_id_keyset
-- (migration 278) bounds the SCAN, not just the result: each page is an index
-- range scan that starts where the previous one stopped. Bounding only the result
-- - `ORDER BY id LIMIT n` against the (agent_id, status) index - puts a Sort over
-- the agent's entire task set in front of every page, so a busy agent costs
-- roughly O(N  / page) for the sweep.
--
-- is load-bearing for correctness, not throughput. It locks the rows
-- this page is about to delete, and under READ COMMITTED PostgreSQL re-checks the
-- WHERE clause after acquiring each lock: a task a concurrent committed UPDATE
-- has already moved to another owner no longer matches and is skipped, and one we
-- did lock cannot be moved away before we delete it.
SELECT id FROM agent_task_queue
WHERE agent_id = ?1 AND id > ?2
ORDER BY id
LIMIT ?3;

-- name: ListTaskIDsByIssueFirstPage :many
-- First page of the same walk; see ListWorkspaceAgentIDFirstPage for why this is
-- a separate query rather than a zero-uuid cursor, and ListTaskIDsByAgentPage for
-- the single-key / keyset / rationale.
SELECT id FROM agent_task_queue
WHERE issue_id = ?1
ORDER BY id
LIMIT ?2;

-- name: ListTaskIDsByIssuePage :many
-- Uses idx_agent_task_queue_issue_id_keyset (migration 279). See
-- ListTaskIDsByAgentPage for the single-key / keyset / rationale.
SELECT id FROM agent_task_queue
WHERE issue_id = ?1 AND id > ?2
ORDER BY id
LIMIT ?3;

-- name: ListTaskIDsByRuntimeFirstPage :many
-- First page of the same walk; see ListWorkspaceAgentIDFirstPage for why this is
-- a separate query rather than a zero-uuid cursor, and ListTaskIDsByAgentPage for
-- the single-key / keyset / rationale.
SELECT id FROM agent_task_queue
WHERE runtime_id = ?1
ORDER BY id
LIMIT ?2;

-- name: ListTaskIDsByRuntimePage :many
-- Uses idx_agent_task_queue_runtime_id (migration 273, (runtime_id, id)); before
-- it every runtime_id index was partial, so this path had none at all. See
-- ListTaskIDsByAgentPage for the single-key / keyset / rationale.
SELECT id FROM agent_task_queue
WHERE runtime_id = ?1 AND id > ?2
ORDER BY id
LIMIT ?3;

-- name: DetachTaskBatchReferences :exec
-- Break inbound references to a task batch before deleting it, in the
-- application layer rather than through the FKs' ON DELETE SET NULL. Both
-- referencing columns are indexed (idx_agent_task_queue_parent from migration
-- 055, idx_autopilot_run_task_id from migration 277) so this stays a per-batch
-- index scan instead of the per-row full scan the FK action would do.
--
-- Kept separate from DeleteTaskBatch: a task can be both a member of the batch
-- and the parent of another member, and updating plus deleting the same row
-- inside one statement is not well defined.
UPDATE autopilot_run
    SET task_id = NULL
    WHERE task_id IN (SELECT value FROM json_each(@task_ids));

-- name: DetachTaskBatchReferences_Step2 :exec
UPDATE agent_task_queue
SET parent_task_id = NULL
WHERE parent_task_id IN (SELECT value FROM json_each(@task_ids));
-- name: DeleteTaskBatch :exec
-- Deletes one bounded batch of tasks together with everything that hangs off
-- them, every arm keyed by task_id against an existing index, then the task rows
-- by primary key. The legacy FK cascades stay a safety net only: this statement
-- is what actually removes the rows, so teardown keeps working when they go.
DELETE FROM task_usage WHERE task_id IN (SELECT id FROM (SELECT value AS id FROM json_each(@task_ids)));

-- name: DeleteTaskBatch_Step2 :exec
DELETE FROM task_message WHERE task_id IN (SELECT id FROM (SELECT value AS id FROM json_each(@task_ids)));

-- name: DeleteTaskBatch_Step3 :exec
DELETE FROM task_token WHERE task_id IN (SELECT id FROM (SELECT value AS id FROM json_each(@task_ids)));

-- name: DeleteTaskBatch_Step4 :exec
DELETE FROM task_supplement WHERE task_id IN (SELECT id FROM (SELECT value AS id FROM json_each(@task_ids)));

-- name: DeleteTaskBatch_Step5 :exec
DELETE FROM task_supplement_capability WHERE task_id IN (SELECT id FROM (SELECT value AS id FROM json_each(@task_ids)));

-- name: DeleteTaskBatch_Step6 :exec
DELETE FROM channel_outbound_card_message WHERE task_id IN (SELECT id FROM (SELECT value AS id FROM json_each(@task_ids)));

-- name: DeleteTaskBatch_Step7 :exec
DELETE FROM lark_outbound_card_message WHERE task_id IN (SELECT id FROM (SELECT value AS id FROM json_each(@task_ids)));

-- name: DeleteTaskBatch_Step8 :exec
DELETE FROM channel_task_delivery WHERE task_id IN (SELECT id FROM (SELECT value AS id FROM json_each(@task_ids)));

-- name: DeleteTaskBatch_Step9 :exec
DELETE FROM chat_draft_restore WHERE task_id IN (SELECT id FROM (SELECT value AS id FROM json_each(@task_ids)));

-- name: DeleteTaskBatch_Step10 :exec
DELETE FROM agent_task_queue WHERE id IN (SELECT id FROM (SELECT value AS id FROM json_each(@task_ids)));
-- name: DeleteTaskTokensByAgent :exec
-- The third explicit task_token path. workspace_id (in DeleteWorkspaceLeafData)
-- and task_id (in DeleteTaskBatch) do not cover a token whose own workspace_id
-- points at a neighbour while its agent lives here, and teardown must not fall
-- back on the agent_id cascade for it. Uses idx_task_token_agent_id
-- (migration 275); one agent key per call for the same planner reason as
-- ListTaskIDsByAgentBatch.
DELETE FROM task_token WHERE agent_id = ?1;

-- name: PrepareWorkspaceDeletionLinks :exec
-- Break self-references and the task/run cycle explicitly before deleting
-- either side. This keeps their ordering in the application-owned graph
-- instead of relying on ON DELETE SET NULL actions.
--
-- The task half of this step now lives in DetachTaskBatchReferences, which runs
-- once per bounded batch instead of once for a whole workspace's task set.
UPDATE comment
    SET parent_id = NULL
    WHERE comment.workspace_id = ?1
      AND parent_id IS NOT NULL;

-- name: PrepareWorkspaceDeletionLinks_Step2 :exec
UPDATE issue
    SET parent_issue_id = NULL
    WHERE issue.workspace_id = ?1
      AND parent_issue_id IS NOT NULL;

-- name: PrepareWorkspaceDeletionLinks_Step3 :exec
UPDATE webhook_delivery
SET replayed_from_delivery_id = NULL
WHERE webhook_delivery.workspace_id = ?1
  AND replayed_from_delivery_id IS NOT NULL;
-- name: DeleteWorkspaceLeafData :exec
-- Everything task-keyed moved to DeleteTaskBatch, which runs in bounded batches
-- before this step; what is left is keyed by the workspace or by one of the
-- workspace-scoped id sets below.
DELETE FROM task_token
    WHERE workspace_id = ?1;

-- name: DeleteWorkspaceLeafData_Step2 :exec
DELETE FROM task_supplement WHERE workspace_id = ?1;

-- name: DeleteWorkspaceLeafData_Step3 :exec
DELETE FROM task_supplement_capability WHERE workspace_id = ?1;

-- name: DeleteWorkspaceLeafData_Step4 :exec
DELETE FROM task_usage_hourly_dirty WHERE workspace_id = ?1;

-- name: DeleteWorkspaceLeafData_Step5 :exec
DELETE FROM task_usage_hourly WHERE workspace_id = ?1;

-- name: DeleteWorkspaceLeafData_Step6 :exec
DELETE FROM attachment WHERE workspace_id = ?1;

-- name: DeleteWorkspaceLeafData_Step7 :exec
DELETE FROM channel_outbound_card_message
    WHERE chat_session_id IN (SELECT id FROM (SELECT id FROM chat_session WHERE chat_session.workspace_id = ?1));

-- name: DeleteWorkspaceLeafData_Step8 :exec
DELETE FROM lark_outbound_card_message
    WHERE chat_session_id IN (SELECT id FROM (SELECT id FROM chat_session WHERE chat_session.workspace_id = ?1));

-- name: DeleteWorkspaceLeafData_Step9 :exec
DELETE FROM chat_draft_restore
    WHERE chat_session_id IN (SELECT id FROM (SELECT id FROM chat_session WHERE chat_session.workspace_id = ?1));

-- name: DeleteWorkspaceLeafData_Step10 :exec
DELETE FROM agent_builder_draft WHERE workspace_id = ?1;

-- name: DeleteWorkspaceLeafData_Step11 :exec
DELETE FROM comment_reaction WHERE workspace_id = ?1;

-- name: DeleteWorkspaceLeafData_Step12 :exec
DELETE FROM issue_reaction WHERE workspace_id = ?1;

-- name: DeleteWorkspaceLeafData_Step13 :exec
DELETE FROM activity_log WHERE workspace_id = ?1;

-- name: DeleteWorkspaceLeafData_Step14 :exec
DELETE FROM inbox_item WHERE workspace_id = ?1;

-- name: DeleteWorkspaceLeafData_Step15 :exec
DELETE FROM issue_dependency
    WHERE issue_id IN (SELECT id FROM (SELECT id FROM issue WHERE issue.workspace_id = ?1))
       OR depends_on_issue_id IN (SELECT id FROM (SELECT id FROM issue WHERE issue.workspace_id = ?1));

-- name: DeleteWorkspaceLeafData_Step16 :exec
DELETE FROM issue_subscriber
    WHERE issue_id IN (SELECT id FROM (SELECT id FROM issue WHERE issue.workspace_id = ?1));

-- name: DeleteWorkspaceLeafData_Step17 :exec
DELETE FROM issue_to_label
    WHERE issue_id IN (SELECT id FROM (SELECT id FROM issue WHERE issue.workspace_id = ?1))
       OR label_id IN (SELECT id FROM (SELECT id FROM issue_label WHERE issue_label.workspace_id = ?1));

-- name: DeleteWorkspaceLeafData_Step18 :exec
DELETE FROM agent_to_label
    WHERE agent_id IN (SELECT id FROM (SELECT id FROM agent WHERE agent.workspace_id = ?1))
       OR label_id IN (SELECT id FROM (SELECT id FROM issue_label WHERE issue_label.workspace_id = ?1));

-- name: DeleteWorkspaceLeafData_Step19 :exec
DELETE FROM skill_to_label
    WHERE skill_id IN (SELECT id FROM (SELECT id FROM skill WHERE skill.workspace_id = ?1))
       OR label_id IN (SELECT id FROM (SELECT id FROM issue_label WHERE issue_label.workspace_id = ?1));

-- name: DeleteWorkspaceLeafData_Step20 :exec
DELETE FROM issue_pull_request
    WHERE issue_id IN (SELECT id FROM (SELECT id FROM issue WHERE issue.workspace_id = ?1))
       OR pull_request_id IN (SELECT id FROM (SELECT id FROM github_pull_request WHERE github_pull_request.workspace_id = ?1));

-- name: DeleteWorkspaceLeafData_Step21 :exec
DELETE FROM issue_vcs_pull_request
    WHERE issue_id IN (SELECT id FROM (SELECT id FROM issue WHERE issue.workspace_id = ?1))
       OR pull_request_id IN (SELECT id FROM (SELECT id FROM vcs_pull_request WHERE vcs_pull_request.workspace_id = ?1));

-- name: DeleteWorkspaceLeafData_Step22 :exec
DELETE FROM issue_pr_automation WHERE workspace_id = ?1;

-- name: DeleteWorkspaceLeafData_Step23 :exec
DELETE FROM issue_pull_request_exclusion WHERE workspace_id = ?1;

-- name: DeleteWorkspaceLeafData_Step24 :exec
DELETE FROM agent_invocation_target
    WHERE agent_id IN (SELECT id FROM (SELECT id FROM agent WHERE agent.workspace_id = ?1));

-- name: DeleteWorkspaceLeafData_Step25 :exec
DELETE FROM agent_skill
    WHERE agent_id IN (SELECT id FROM (SELECT id FROM agent WHERE agent.workspace_id = ?1))
       OR skill_id IN (SELECT id FROM (SELECT id FROM skill WHERE skill.workspace_id = ?1));

-- name: DeleteWorkspaceLeafData_Step26 :exec
DELETE FROM skill_file
    WHERE skill_id IN (SELECT id FROM (SELECT id FROM skill WHERE skill.workspace_id = ?1));

-- name: DeleteWorkspaceLeafData_Step27 :exec
DELETE FROM daemon_connection
    WHERE agent_id IN (SELECT id FROM (SELECT id FROM agent WHERE agent.workspace_id = ?1));

-- name: DeleteWorkspaceLeafData_Step28 :exec
DELETE FROM squad_member
    WHERE squad_id IN (SELECT id FROM (SELECT id FROM squad WHERE squad.workspace_id = ?1));

-- name: DeleteWorkspaceLeafData_Step29 :exec
DELETE FROM project_resource WHERE workspace_id = ?1;

-- name: DeleteWorkspaceLeafData_Step30 :exec
DELETE FROM autopilot_collaborator
    WHERE autopilot_id IN (SELECT id FROM (SELECT id FROM autopilot WHERE autopilot.workspace_id = ?1));

-- name: DeleteWorkspaceLeafData_Step31 :exec
DELETE FROM autopilot_subscriber
    WHERE autopilot_id IN (SELECT id FROM (SELECT id FROM autopilot WHERE autopilot.workspace_id = ?1));

-- name: DeleteWorkspaceLeafData_Step32 :exec
DELETE FROM webhook_delivery WHERE workspace_id = ?1;

-- name: DeleteWorkspaceLeafData_Step33 :exec
DELETE FROM github_pull_request_check_run
    WHERE pr_id IN (SELECT id FROM (SELECT id FROM github_pull_request WHERE github_pull_request.workspace_id = ?1));

-- name: DeleteWorkspaceLeafData_Step34 :exec
DELETE FROM github_pull_request_check_suite
    WHERE pr_id IN (SELECT id FROM (SELECT id FROM github_pull_request WHERE github_pull_request.workspace_id = ?1));

-- name: DeleteWorkspaceLeafData_Step35 :exec
DELETE FROM github_pending_check_suite WHERE workspace_id = ?1;

-- name: DeleteWorkspaceLeafData_Step36 :exec
DELETE FROM channel_task_delivery
    WHERE installation_id IN (SELECT id FROM (SELECT id FROM channel_installation WHERE channel_installation.workspace_id = ?1));

-- name: DeleteWorkspaceLeafData_Step37 :exec
DELETE FROM channel_outbound_message
    WHERE installation_id IN (SELECT id FROM (SELECT id FROM channel_installation WHERE channel_installation.workspace_id = ?1));

-- name: DeleteWorkspaceLeafData_Step38 :exec
DELETE FROM channel_reply_delivery
    WHERE installation_id IN (SELECT id FROM (SELECT id FROM channel_installation WHERE channel_installation.workspace_id = ?1));

-- name: DeleteWorkspaceLeafData_Step39 :exec
DELETE FROM channel_chat_context_generation
    WHERE chat_session_id IN (SELECT id FROM (SELECT id FROM chat_session WHERE chat_session.workspace_id = ?1));

-- name: DeleteWorkspaceLeafData_Step40 :exec
DELETE FROM vcs_commit_status
    WHERE connection_id IN (SELECT id FROM (SELECT id FROM vcs_connection WHERE vcs_connection.workspace_id = ?1));

-- name: DeleteWorkspaceLeafData_Step41 :exec
DELETE FROM channel_chat_session_binding
    WHERE installation_id IN (SELECT id FROM (SELECT id FROM channel_installation WHERE channel_installation.workspace_id = ?1))
       OR chat_session_id IN (SELECT id FROM (SELECT id FROM chat_session WHERE chat_session.workspace_id = ?1));

-- name: DeleteWorkspaceLeafData_Step42 :exec
DELETE FROM dingtalk_group_route WHERE workspace_id = ?1;

-- name: DeleteWorkspaceLeafData_Step43 :exec
DELETE FROM channel_inbound_message_dedup
    WHERE installation_id IN (SELECT id FROM (SELECT id FROM channel_installation WHERE channel_installation.workspace_id = ?1));

-- name: DeleteWorkspaceLeafData_Step44 :exec
DELETE FROM channel_inbound_audit
    WHERE installation_id IN (SELECT id FROM (SELECT id FROM channel_installation WHERE channel_installation.workspace_id = ?1));

-- name: DeleteWorkspaceLeafData_Step45 :exec
DELETE FROM channel_user_binding WHERE workspace_id = ?1;

-- name: DeleteWorkspaceLeafData_Step46 :exec
DELETE FROM channel_binding_token WHERE workspace_id = ?1;

-- name: DeleteWorkspaceLeafData_Step47 :exec
DELETE FROM lark_chat_session_binding
    WHERE installation_id IN (SELECT id FROM (SELECT id FROM lark_installation WHERE lark_installation.workspace_id = ?1))
       OR chat_session_id IN (SELECT id FROM (SELECT id FROM chat_session WHERE chat_session.workspace_id = ?1));

-- name: DeleteWorkspaceLeafData_Step48 :exec
DELETE FROM lark_inbound_message_dedup
    WHERE installation_id IN (SELECT id FROM (SELECT id FROM lark_installation WHERE lark_installation.workspace_id = ?1));

-- name: DeleteWorkspaceLeafData_Step49 :exec
DELETE FROM lark_inbound_audit
    WHERE installation_id IN (SELECT id FROM (SELECT id FROM lark_installation WHERE lark_installation.workspace_id = ?1));

-- name: DeleteWorkspaceLeafData_Step50 :exec
DELETE FROM lark_user_binding WHERE workspace_id = ?1;

-- name: DeleteWorkspaceLeafData_Step51 :exec
DELETE FROM lark_binding_token WHERE workspace_id = ?1;

-- name: DeleteWorkspaceLeafData_Step52 :exec
-- Keep the two-system cleanup ledger until object storage has been settled.
-- Moving every row out of pending also prevents a concurrent media bind from
-- attaching an object after the workspace teardown commits. The reconciler
-- performs the idempotent object delete and clears the row afterwards.
UPDATE channel_media_pending_object
SET state = CASE
        WHEN state = 'tombstoned' THEN 'tombstoned'
        ELSE 'deleting'
    END,
    lease_token = NULL,
    lease_expires_at = NULL,
    next_attempt_at = now(),
    last_error = NULL
WHERE channel_media_pending_object.workspace_id = ?1;
-- name: DeleteWorkspaceChatMessages :exec
DELETE FROM chat_message
WHERE chat_session_id IN (
    SELECT id FROM chat_session WHERE chat_session.workspace_id = ?1
);

-- name: DeleteWorkspaceCommunicationRoots :exec
DELETE FROM chat_session WHERE chat_session.workspace_id = ?1;

-- name: DeleteWorkspaceCommunicationRoots_Step2 :exec
DELETE FROM dingtalk_group_presence WHERE workspace_id = ?1;

-- name: DeleteWorkspaceCommunicationRoots_Step3 :exec
DELETE FROM dingtalk_bot_identity WHERE workspace_id = ?1;

-- name: DeleteWorkspaceCommunicationRoots_Step4 :exec
DELETE FROM channel_installation
    WHERE channel_installation.workspace_id = ?1;

-- name: DeleteWorkspaceCommunicationRoots_Step5 :exec
DELETE FROM lark_installation WHERE lark_installation.workspace_id = ?1;
-- name: DeleteWorkspaceComments :exec
DELETE FROM comment WHERE comment.workspace_id = ?1;

-- name: DeleteWorkspaceIssueRoots :exec
DELETE FROM issue_wakeup_receipt WHERE wakeup_id IN (SELECT id FROM issue_wakeup WHERE workspace_id=?1);

-- name: DeleteWorkspaceIssueRoots_Step2 :exec
DELETE FROM issue_wakeup WHERE workspace_id=?1;

-- name: DeleteWorkspaceIssueRoots_Step3 :exec
DELETE FROM issue_child_event WHERE workspace_id=?1;

-- name: DeleteWorkspaceIssueRoots_Step4 :exec
DELETE FROM issue WHERE issue.workspace_id = ?1;

-- name: DeleteWorkspaceIssueRoots_Step5 :exec
DELETE FROM issue_label WHERE issue_label.workspace_id = ?1;

-- name: DeleteWorkspaceIssueRoots_Step6 :exec
DELETE FROM issue_property WHERE issue_property.workspace_id = ?1;

-- name: DeleteWorkspaceIssueRoots_Step7 :exec
DELETE FROM issue_view WHERE issue_view.workspace_id = ?1;

-- name: DeleteWorkspaceIssueRoots_Step8 :exec
DELETE FROM issue_view_preference
    WHERE issue_view_preference.workspace_id = ?1;

-- name: DeleteWorkspaceIssueRoots_Step9 :exec
DELETE FROM quick_action WHERE quick_action.workspace_id = ?1;
-- name: DeleteWorkspaceAutopilotRuns :exec
DELETE FROM autopilot_run
WHERE autopilot_id IN (
    SELECT id FROM autopilot WHERE autopilot.workspace_id = ?1
);

-- name: DeleteWorkspaceAutopilotQuotaReservations :exec
DELETE FROM autopilot_quota_reservation
WHERE autopilot_quota_reservation.workspace_id = ?1;

-- name: DeleteWorkspaceAutopilotQuotaPeriods :exec
DELETE FROM autopilot_quota_period
WHERE autopilot_quota_period.workspace_id = ?1;

-- name: DeleteWorkspaceAutopilotChildren :exec
DELETE FROM autopilot_trigger
    WHERE autopilot_id IN (
        SELECT id FROM autopilot WHERE autopilot.workspace_id = ?1
    );

-- name: DeleteWorkspaceAutopilotChildren_Step2 :exec
DELETE FROM autopilot_rule_version
WHERE autopilot_rule_version.workspace_id = ?1;
-- name: DeleteWorkspaceAutopilots :exec
DELETE FROM autopilot WHERE autopilot.workspace_id = ?1;

-- name: DeleteWorkspacePullRequests :exec
DELETE FROM github_pull_request_check_run
WHERE pr_id IN (SELECT id FROM github_pull_request WHERE github_pull_request.workspace_id = ?1);

-- name: DeleteWorkspacePullRequests_Step2 :exec
DELETE FROM github_pull_request_check_suite
WHERE pr_id IN (SELECT id FROM github_pull_request WHERE github_pull_request.workspace_id = ?1);

-- name: DeleteWorkspacePullRequests_Step3 :exec
DELETE FROM github_pull_request
    WHERE github_pull_request.workspace_id = ?1;

-- name: DeleteWorkspacePullRequests_Step4 :exec
DELETE FROM vcs_pull_request WHERE vcs_pull_request.workspace_id = ?1;
-- name: DeleteWorkspaceConnections :exec
DELETE FROM github_installation
    WHERE github_installation.workspace_id = ?1;

-- name: DeleteWorkspaceConnections_Step2 :exec
DELETE FROM vcs_connection WHERE vcs_connection.workspace_id = ?1;
-- name: DeleteWorkspaceSquadsAndSkills :exec
DELETE FROM squad WHERE squad.workspace_id = ?1;

-- name: DeleteWorkspaceSquadsAndSkills_Step2 :exec
DELETE FROM skill WHERE skill.workspace_id = ?1;
-- name: DeleteWorkspacePluginData :exec
-- Plugin relationships have no foreign keys or cascades. Storage and secrets
-- hang off the installation, so both leaf tables are cleared through the
-- workspace's installation ids before the installations themselves.
DELETE FROM plugin_storage
    WHERE installation_id IN (SELECT id FROM (SELECT plugin_installation.id
    FROM plugin_installation
    WHERE plugin_installation.workspace_id = ?1));

-- name: DeleteWorkspacePluginData_Step2 :exec
DELETE FROM plugin_secret
    WHERE installation_id IN (SELECT id FROM (SELECT plugin_installation.id
    FROM plugin_installation
    WHERE plugin_installation.workspace_id = ?1));

-- name: DeleteWorkspacePluginData_Step3 :exec
DELETE FROM plugin_hook_schedule
    WHERE installation_id IN (SELECT id FROM (SELECT plugin_installation.id
    FROM plugin_installation
    WHERE plugin_installation.workspace_id = ?1));

-- name: DeleteWorkspacePluginData_Step4 :exec
DELETE FROM plugin_invocation
    WHERE workspace_id = ?1;

-- name: DeleteWorkspacePluginData_Step5 :exec
DELETE FROM plugin_package_file
    WHERE version_id IN (SELECT id FROM (SELECT plugin_package_version.id
    FROM plugin_package_version
    WHERE plugin_package_version.workspace_id = ?1));

-- name: DeleteWorkspacePluginData_Step6 :exec
DELETE FROM plugin_package_version
    WHERE workspace_id = ?1;

-- name: DeleteWorkspacePluginData_Step7 :exec
DELETE FROM plugin_package
    WHERE workspace_id = ?1;

-- name: DeleteWorkspacePluginData_Step8 :exec
DELETE FROM plugin_installation WHERE id IN (SELECT id FROM (SELECT plugin_installation.id
    FROM plugin_installation
    WHERE plugin_installation.workspace_id = ?1));
-- name: DeleteWorkspaceAgents :exec
DELETE FROM agent_mcp_server
WHERE agent_id IN (SELECT id FROM agent WHERE agent.workspace_id = ?1)
   OR server_id IN (SELECT id FROM workspace_mcp_server WHERE workspace_mcp_server.workspace_id = ?1);

-- name: DeleteWorkspaceAgents_Step2 :exec
DELETE FROM agent WHERE agent.workspace_id = ?1;

-- name: DeleteWorkspaceRuntimesAndProjects :exec
DELETE FROM agent_runtime WHERE agent_runtime.workspace_id = ?1;

-- name: DeleteWorkspaceRuntimesAndProjects_Step2 :exec
DELETE FROM runtime_profile WHERE runtime_profile.workspace_id = ?1;

-- name: DeleteWorkspaceRuntimesAndProjects_Step3 :exec
DELETE FROM project WHERE project.workspace_id = ?1;
-- name: DeleteWorkspaceAdministration :exec
DELETE FROM member WHERE member.workspace_id = ?1;

-- name: DeleteWorkspaceAdministration_Step2 :exec
DELETE FROM notification_preference
    WHERE notification_preference.workspace_id = ?1;

-- name: DeleteWorkspaceAdministration_Step3 :exec
DELETE FROM pinned_item WHERE pinned_item.workspace_id = ?1;

-- name: DeleteWorkspaceAdministration_Step4 :exec
DELETE FROM daemon_token WHERE daemon_token.workspace_id = ?1;

-- name: DeleteWorkspaceAdministration_Step5 :exec
UPDATE feedback
    SET workspace_id = NULL
    WHERE feedback.workspace_id = ?1;

-- name: DeleteWorkspaceAdministration_Step6 :exec
UPDATE client_usage_daily
    SET workspace_id = NULL
    WHERE client_usage_daily.workspace_id = ?1;

-- name: DeleteWorkspaceAdministration_Step7 :exec
DELETE FROM workspace_share_link
    WHERE workspace_share_link.workspace_id = ?1;

-- name: DeleteWorkspaceAdministration_Step8 :exec
DELETE FROM workspace_invitation
WHERE workspace_invitation.workspace_id = ?1;
