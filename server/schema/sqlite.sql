-- Multica SQLite schema (single writer).
-- Converted from the PostgreSQL schema at migration 585.
-- Foreign keys are intentionally omitted; the application maintains relationships.
-- gen_random_uuid() defaults are omitted; the application supplies ids.
-- Arrays are TEXT and store a JSON array. JSON values are TEXT.

CREATE TABLE activity_log (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    issue_id TEXT,
    actor_type TEXT,
    actor_id TEXT,
    "action" TEXT NOT NULL,
    details TEXT DEFAULT '{}' NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT activity_log_actor_type_check CHECK ((actor_type IN ('member', 'agent', 'system'))),
    CONSTRAINT activity_log_pkey PRIMARY KEY (id)
);

CREATE TABLE agent (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    name TEXT NOT NULL,
    avatar_url TEXT,
    runtime_mode TEXT NOT NULL,
    runtime_config TEXT DEFAULT '{}' NOT NULL,
    visibility TEXT DEFAULT 'private' NOT NULL,
    status TEXT DEFAULT 'offline' NOT NULL,
    max_concurrent_tasks INTEGER DEFAULT 6 NOT NULL,
    owner_id TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    description TEXT DEFAULT '' NOT NULL,
    runtime_id TEXT,
    instructions TEXT DEFAULT '' NOT NULL,
    archived_at TEXT,
    archived_by TEXT,
    custom_env TEXT DEFAULT '{}' NOT NULL,
    custom_args TEXT DEFAULT '[]' NOT NULL,
    mcp_config TEXT,
    model TEXT,
    thinking_level TEXT,
    composio_toolkit_allowlist TEXT,
    permission_mode TEXT DEFAULT 'private' NOT NULL,
    kind TEXT DEFAULT 'user' NOT NULL,
    system_key TEXT,
    disabled_runtime_skills TEXT DEFAULT '[]' NOT NULL,
    service_tier TEXT,
    conversation_starters TEXT DEFAULT '[]' NOT NULL,
CONSTRAINT agent_conversation_starters_check CHECK (((json_type(conversation_starters) = 'array') AND (json_array_length(conversation_starters) <= 3))),
CONSTRAINT agent_description_length CHECK ((length(description) <= 255)),
CONSTRAINT agent_kind_check CHECK ((kind IN ('user', 'system'))),
CONSTRAINT agent_permission_mode_check CHECK ((permission_mode IN ('private', 'public_to'))),
CONSTRAINT agent_runtime_mode_check CHECK ((runtime_mode IN ('local', 'cloud'))),
CONSTRAINT agent_status_check CHECK ((status IN ('idle', 'working', 'blocked', 'error', 'offline'))),
CONSTRAINT agent_visibility_check CHECK ((visibility IN ('workspace', 'private'))),
    CONSTRAINT agent_pkey PRIMARY KEY (id),
    CONSTRAINT agent_workspace_name_unique UNIQUE (workspace_id, name)
);

CREATE TABLE agent_builder_draft (
    chat_session_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    draft TEXT DEFAULT '{}' NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    CONSTRAINT agent_builder_draft_pkey PRIMARY KEY (chat_session_id)
);

CREATE TABLE agent_invocation_target (
    id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id TEXT NOT NULL,
    created_by TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT agent_invocation_target_target_type_check CHECK ((target_type IN ('workspace', 'member', 'team'))),
    CONSTRAINT agent_invocation_target_pkey PRIMARY KEY (id),
    CONSTRAINT agent_invocation_target_agent_id_target_type_target_id_key UNIQUE (agent_id, target_type, target_id)
);

CREATE TABLE agent_mcp_server (
    agent_id TEXT NOT NULL,
    server_id TEXT NOT NULL,
    enabled INTEGER DEFAULT 1 NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    CONSTRAINT agent_mcp_server_pkey PRIMARY KEY (agent_id, server_id)
);

CREATE TABLE agent_runtime (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    daemon_id TEXT,
    name TEXT NOT NULL,
    runtime_mode TEXT NOT NULL,
    provider TEXT NOT NULL,
    status TEXT DEFAULT 'offline' NOT NULL,
    device_info TEXT DEFAULT '' NOT NULL,
    metadata TEXT DEFAULT '{}' NOT NULL,
    last_seen_at TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    owner_id TEXT,
    legacy_daemon_id TEXT,
    visibility TEXT DEFAULT 'private' NOT NULL,
    profile_id TEXT,
    custom_name TEXT,
CONSTRAINT agent_runtime_runtime_mode_check CHECK ((runtime_mode IN ('local', 'cloud'))),
CONSTRAINT agent_runtime_status_check CHECK ((status IN ('online', 'offline'))),
CONSTRAINT agent_runtime_visibility_check CHECK ((visibility IN ('private', 'public'))),
    CONSTRAINT agent_runtime_pkey PRIMARY KEY (id)
);

CREATE TABLE agent_skill (
    agent_id TEXT NOT NULL,
    skill_id TEXT NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    enabled INTEGER DEFAULT 1 NOT NULL,
    CONSTRAINT agent_skill_pkey PRIMARY KEY (agent_id, skill_id)
);

CREATE TABLE agent_task_queue (
    id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    issue_id TEXT,
    status TEXT DEFAULT 'queued' NOT NULL,
    priority INTEGER DEFAULT 0 NOT NULL,
    dispatched_at TEXT,
    started_at TEXT,
    completed_at TEXT,
    result TEXT,
    error TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    context TEXT,
    runtime_id TEXT,
    session_id TEXT,
    work_dir TEXT,
    trigger_comment_id TEXT,
    chat_session_id TEXT,
    autopilot_run_id TEXT,
    attempt INTEGER DEFAULT 1 NOT NULL,
    max_attempts INTEGER DEFAULT 2 NOT NULL,
    parent_task_id TEXT,
    failure_reason TEXT,
    trigger_summary TEXT,
    force_fresh_session INTEGER DEFAULT 0 NOT NULL,
    is_leader_task INTEGER DEFAULT 0 NOT NULL,
    wait_reason TEXT,
    initiator_user_id TEXT,
    handoff_note TEXT,
    prepare_lease_expires_at TEXT,
    squad_id TEXT,
    runtime_mcp_overlay TEXT,
    escalation_for_task_id TEXT,
    fire_at TEXT,
    originator_user_id TEXT,
    runtime_connected_apps TEXT,
    coalesced_comment_ids TEXT DEFAULT '[]' NOT NULL,
    delivered_comment_ids TEXT DEFAULT '[]' NOT NULL,
    chat_input_task_id TEXT,
    chat_finalize_deferred_at TEXT,
    originator_source TEXT,
    delegated_from_task_id TEXT,
    retry_of_task_id TEXT,
    rerun_of_task_id TEXT,
    rule_version_id TEXT,
    trigger_evidence_kind TEXT,
    trigger_evidence_ref_id TEXT,
    accountable_user_id TEXT,
    session_rollout_missing INTEGER DEFAULT 0 NOT NULL,
    retired_session_id TEXT,
    quick_actions_disabled INTEGER DEFAULT 0 NOT NULL,
    regenerate_quick_actions_for TEXT,
    branch_name TEXT,
    durable_work_dir TEXT,
    channel_context_revision INTEGER,
    comment_thread_id TEXT,
    cancelled_by_type TEXT,
    cancelled_by_id TEXT,
    cancelled_by_name TEXT,
    issue_snapshot TEXT,
    workflow_step TEXT,
CONSTRAINT agent_task_queue_accountable_matches_originator CHECK (((originator_user_id IS NULL) OR ((accountable_user_id IS NOT NULL) AND (accountable_user_id = originator_user_id)))),
CONSTRAINT agent_task_queue_status_check CHECK ((status IN ('queued', 'dispatched', 'running', 'completed', 'failed', 'cancelled', 'waiting_local_directory', 'deferred'))),
    CONSTRAINT agent_task_queue_active_requires_runtime CHECK (((runtime_id IS NOT NULL) OR (completed_at IS NOT NULL))),
    CONSTRAINT agent_task_queue_pkey PRIMARY KEY (id)
);

CREATE TABLE agent_to_label (
    agent_id TEXT NOT NULL,
    label_id TEXT NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    CONSTRAINT agent_to_label_pkey PRIMARY KEY (agent_id, label_id)
);

CREATE TABLE attachment (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    issue_id TEXT,
    comment_id TEXT,
    uploader_type TEXT NOT NULL,
    uploader_id TEXT NOT NULL,
    filename TEXT NOT NULL,
    url TEXT NOT NULL,
    content_type TEXT NOT NULL,
    size_bytes INTEGER NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    chat_session_id TEXT,
    chat_message_id TEXT,
    task_id TEXT,
    source_context_id TEXT,
CONSTRAINT attachment_uploader_type_check CHECK ((uploader_type IN ('member', 'agent'))),
    CONSTRAINT attachment_pkey PRIMARY KEY (id)
);

CREATE TABLE autopilot (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT,
    assignee_id TEXT NOT NULL,
    status TEXT DEFAULT 'active' NOT NULL,
    execution_mode TEXT DEFAULT 'create_issue' NOT NULL,
    issue_title_template TEXT,
    created_by_type TEXT NOT NULL,
    created_by_id TEXT NOT NULL,
    last_run_at TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    assignee_type TEXT DEFAULT 'agent' NOT NULL,
    project_id TEXT,
    pause_reason TEXT,
CONSTRAINT autopilot_assignee_type_check CHECK ((assignee_type IN ('agent', 'squad'))),
CONSTRAINT autopilot_created_by_type_check CHECK ((created_by_type IN ('member', 'agent'))),
CONSTRAINT autopilot_execution_mode_check CHECK ((execution_mode IN ('create_issue', 'run_only'))),
CONSTRAINT autopilot_status_check CHECK ((status IN ('active', 'paused', 'archived'))),
    CONSTRAINT autopilot_pkey PRIMARY KEY (id)
);

CREATE TABLE autopilot_collaborator (
    autopilot_id TEXT NOT NULL,
    user_type TEXT NOT NULL,
    user_id TEXT NOT NULL,
    granted_by TEXT NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT autopilot_collaborator_user_type_check CHECK ((user_type = 'member')),
    CONSTRAINT autopilot_collaborator_pkey PRIMARY KEY (autopilot_id, user_type, user_id)
);

CREATE TABLE autopilot_quota_period (
    workspace_id TEXT NOT NULL,
    period_start TEXT NOT NULL,
    period_end TEXT NOT NULL,
    used_count INTEGER DEFAULT 0 NOT NULL,
    reserved_count INTEGER DEFAULT 0 NOT NULL,
    blocked_counts TEXT DEFAULT '{}' NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    rejection_notified_at TEXT,
CONSTRAINT autopilot_quota_period_check CHECK ((period_start < period_end)),
CONSTRAINT autopilot_quota_period_reserved_count_check CHECK ((reserved_count >= 0)),
CONSTRAINT autopilot_quota_period_used_count_check CHECK ((used_count >= 0)),
    CONSTRAINT autopilot_quota_period_pkey PRIMARY KEY (workspace_id, period_start, period_end)
);

CREATE TABLE autopilot_quota_reservation (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    period_start TEXT NOT NULL,
    period_end TEXT NOT NULL,
    policy_revision INTEGER NOT NULL,
    subscription_version INTEGER NOT NULL,
    source TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    state TEXT DEFAULT 'reserved' NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    finalized_at TEXT,
CONSTRAINT autopilot_quota_reservation_check CHECK ((period_start < period_end)),
CONSTRAINT autopilot_quota_reservation_state_check CHECK ((state IN ('reserved', 'consumed', 'released'))),
    CONSTRAINT autopilot_quota_reservation_pkey PRIMARY KEY (id)
);

CREATE TABLE autopilot_rule_version (
    id TEXT NOT NULL,
    autopilot_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    published_by_type TEXT NOT NULL,
    published_by_id TEXT,
    config_summary TEXT DEFAULT '{}' NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    CONSTRAINT autopilot_rule_version_pkey PRIMARY KEY (id)
);

CREATE TABLE autopilot_run (
    id TEXT NOT NULL,
    autopilot_id TEXT NOT NULL,
    trigger_id TEXT,
    source TEXT NOT NULL,
    status TEXT DEFAULT 'pending' NOT NULL,
    issue_id TEXT,
    task_id TEXT,
    triggered_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    completed_at TEXT,
    failure_reason TEXT,
    trigger_payload TEXT,
    result TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    squad_id TEXT,
    planned_at TEXT,
    webhook_delivery_id TEXT,
    quota_reservation_id TEXT,
    reason_code TEXT,
CONSTRAINT autopilot_run_source_check CHECK ((source IN ('schedule', 'manual', 'webhook', 'api'))),
CONSTRAINT autopilot_run_status_check CHECK ((status IN ('issue_created', 'running', 'completed', 'failed', 'skipped'))),
    CONSTRAINT autopilot_run_pkey PRIMARY KEY (id)
);

CREATE TABLE autopilot_subscriber (
    autopilot_id TEXT NOT NULL,
    user_type TEXT NOT NULL,
    user_id TEXT NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT autopilot_subscriber_user_type_check CHECK ((user_type = 'member')),
    CONSTRAINT autopilot_subscriber_pkey PRIMARY KEY (autopilot_id, user_type, user_id)
);

CREATE TABLE autopilot_trigger (
    id TEXT NOT NULL,
    autopilot_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    enabled INTEGER DEFAULT 1 NOT NULL,
    cron_expression TEXT,
    timezone TEXT DEFAULT 'UTC',
    next_run_at TEXT,
    webhook_token TEXT,
    label TEXT,
    last_fired_at TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    provider TEXT DEFAULT 'generic' NOT NULL,
    signing_secret TEXT,
    event_filters TEXT,
    published_by_type TEXT,
    published_by_id TEXT,
    created_by_type TEXT,
    created_by_id TEXT,
CONSTRAINT autopilot_trigger_kind_check CHECK ((kind IN ('schedule', 'webhook', 'api'))),
CONSTRAINT autopilot_trigger_provider_check CHECK ((provider IN ('generic', 'github'))),
    CONSTRAINT autopilot_trigger_pkey PRIMARY KEY (id)
);

CREATE TABLE channel_binding_token (
    token_hash TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    installation_id TEXT NOT NULL,
    channel_type TEXT NOT NULL,
    channel_user_id TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    consumed_at TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT channel_binding_token_ttl_cap CHECK ((expires_at <= (strftime('%Y-%m-%dT%H:%M:%fZ', created_at, '+15 minutes')))),
    CONSTRAINT channel_binding_token_pkey PRIMARY KEY (token_hash)
);

CREATE TABLE channel_chat_context_generation (
    chat_session_id TEXT NOT NULL,
    revision INTEGER NOT NULL,
    history_start_message_id TEXT,
    history_end_message_id TEXT,
    history_boundary_pending INTEGER DEFAULT 0 NOT NULL,
    pending_fresh INTEGER DEFAULT 0 NOT NULL,
    initiator_user_id TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    last_message_id TEXT,
    last_thread_id TEXT,
    last_sender_id TEXT
);

CREATE TABLE channel_chat_session_binding (
    id TEXT NOT NULL,
    chat_session_id TEXT NOT NULL,
    installation_id TEXT NOT NULL,
    channel_type TEXT NOT NULL,
    channel_chat_id TEXT NOT NULL,
    chat_type TEXT NOT NULL,
    last_message_id TEXT,
    last_thread_id TEXT,
    config TEXT DEFAULT '{}' NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    pending_fresh INTEGER DEFAULT 0 NOT NULL,
    context_revision INTEGER DEFAULT 1 NOT NULL,
    route_revision INTEGER DEFAULT 1 NOT NULL,
    retired_at TEXT,
    history_start_message_id TEXT,
    history_end_message_id TEXT,
    history_boundary_pending INTEGER DEFAULT 0 NOT NULL,
CONSTRAINT channel_chat_session_binding_chat_type_check CHECK ((chat_type IN ('p2p', 'group'))),
    CONSTRAINT channel_chat_session_binding_pkey PRIMARY KEY (id),
    CONSTRAINT channel_chat_session_binding_chat_session_id_key UNIQUE (chat_session_id)
);

CREATE TABLE channel_inbound_audit (
    id TEXT NOT NULL,
    installation_id TEXT,
    channel_type TEXT NOT NULL,
    channel_chat_id TEXT,
    event_type TEXT NOT NULL,
    channel_event_id TEXT,
    channel_message_id TEXT,
    drop_reason TEXT NOT NULL,
    received_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    CONSTRAINT channel_inbound_audit_pkey PRIMARY KEY (id)
);

CREATE TABLE channel_inbound_message_dedup (
    installation_id TEXT NOT NULL,
    message_id TEXT NOT NULL,
    received_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    processed_at TEXT,
    claim_token TEXT NOT NULL,
    CONSTRAINT channel_inbound_message_dedup_pkey PRIMARY KEY (installation_id, message_id)
);

CREATE TABLE channel_installation (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    channel_type TEXT NOT NULL,
    config TEXT DEFAULT '{}' NOT NULL,
    status TEXT DEFAULT 'active' NOT NULL,
    ws_lease_token TEXT,
    ws_lease_expires_at TEXT,
    installer_user_id TEXT NOT NULL,
    installed_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT channel_installation_status_check CHECK ((status IN ('active', 'revoked'))),
    CONSTRAINT channel_installation_pkey PRIMARY KEY (id),
    CONSTRAINT channel_installation_workspace_id_agent_id_channel_type_key UNIQUE (workspace_id, agent_id, channel_type)
);

CREATE TABLE channel_media_pending_object (
    storage_key TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    chat_message_id TEXT NOT NULL,
    storage_url TEXT NOT NULL,
    installation_id TEXT,
    state TEXT DEFAULT 'pending' NOT NULL,
    lease_token TEXT,
    lease_expires_at TEXT,
    attempt INTEGER DEFAULT 0 NOT NULL,
    next_attempt_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    last_error TEXT,
    tombstone_pass INTEGER DEFAULT 0 NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT channel_media_pending_object_state_check CHECK ((state IN ('pending', 'deleting', 'tombstoned'))),
    CONSTRAINT channel_media_pending_object_pkey PRIMARY KEY (storage_key)
);

CREATE TABLE channel_outbound_card_message (
    id TEXT NOT NULL,
    chat_session_id TEXT NOT NULL,
    task_id TEXT,
    channel_type TEXT NOT NULL,
    channel_chat_id TEXT NOT NULL,
    channel_card_message_id TEXT NOT NULL,
    status TEXT DEFAULT 'pending' NOT NULL,
    last_patched_at TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT channel_outbound_card_message_status_check CHECK ((status IN ('pending', 'streaming', 'final', 'error'))),
    CONSTRAINT channel_outbound_card_message_pkey PRIMARY KEY (id)
);

CREATE TABLE channel_outbound_message (
    installation_id TEXT NOT NULL,
    channel_type TEXT NOT NULL,
    channel_message_id TEXT NOT NULL,
    binding_id TEXT NOT NULL,
    route_revision INTEGER NOT NULL,
    task_id TEXT,
    outbound_kind TEXT NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL
);

CREATE TABLE channel_reply_delivery (
    turn_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    binding_id TEXT NOT NULL,
    installation_id TEXT NOT NULL,
    channel_type TEXT NOT NULL,
    chat_id TEXT NOT NULL,
    phase TEXT NOT NULL,
    send_state TEXT NOT NULL,
    message_id TEXT DEFAULT '' NOT NULL,
    chunks_sent INTEGER DEFAULT 0 NOT NULL,
    owner_token TEXT,
    owner_expires_at TEXT,
    settled_reason TEXT DEFAULT '' NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    attempt_depth INTEGER DEFAULT 0 NOT NULL,
CONSTRAINT channel_reply_delivery_attempt_depth_check CHECK ((attempt_depth >= 0)),
CONSTRAINT channel_reply_delivery_chunks_sent_check CHECK ((chunks_sent >= 0)),
CONSTRAINT channel_reply_delivery_phase_check CHECK ((phase IN ('streaming', 'terminal', 'settled'))),
CONSTRAINT channel_reply_delivery_send_state_check CHECK ((send_state IN ('none', 'in_flight', 'known', 'unknown')))
);

CREATE TABLE channel_task_delivery (
    task_id TEXT NOT NULL,
    binding_id TEXT NOT NULL,
    installation_id TEXT NOT NULL,
    channel_type TEXT NOT NULL,
    channel_chat_id TEXT NOT NULL,
    chat_type TEXT NOT NULL,
    channel_message_id TEXT,
    channel_thread_id TEXT,
    route_revision INTEGER NOT NULL,
    config TEXT DEFAULT '{}' NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    channel_sender_id TEXT,
    CONSTRAINT channel_task_delivery_pkey PRIMARY KEY (task_id)
);

CREATE TABLE channel_typing_reaction (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    chat_session_id TEXT NOT NULL,
    chat_message_id TEXT NOT NULL,
    installation_id TEXT NOT NULL,
    channel_message_id TEXT NOT NULL,
    installation_snapshot TEXT NOT NULL,
    reaction_id TEXT DEFAULT '' NOT NULL,
    add_finished INTEGER DEFAULT 0 NOT NULL,
    cleanup_required INTEGER DEFAULT 0 NOT NULL,
    cleaned_at TEXT,
    retry_after TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    attempts INTEGER DEFAULT 0 NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    abandoned_at TEXT,
    quota_slot INTEGER
);

CREATE TABLE channel_user_binding (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    multica_user_id TEXT NOT NULL,
    installation_id TEXT NOT NULL,
    channel_type TEXT NOT NULL,
    channel_user_id TEXT NOT NULL,
    config TEXT DEFAULT '{}' NOT NULL,
    bound_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    CONSTRAINT channel_user_binding_pkey PRIMARY KEY (id),
    CONSTRAINT channel_user_binding_installation_id_channel_user_id_key UNIQUE (installation_id, channel_user_id)
);

CREATE TABLE chat_draft_restore (
    id TEXT NOT NULL,
    chat_session_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    content TEXT NOT NULL,
    attachment_ids TEXT DEFAULT '[]' NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    CONSTRAINT chat_draft_restore_pkey PRIMARY KEY (id)
);

CREATE TABLE chat_message (
    id TEXT NOT NULL,
    chat_session_id TEXT NOT NULL,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    task_id TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    failure_reason TEXT,
    elapsed_ms INTEGER,
    message_kind TEXT DEFAULT 'message' NOT NULL,
    channel_media_pending_until TEXT,
    channel_ingested INTEGER DEFAULT 0 NOT NULL,
    quick_actions TEXT DEFAULT '[]' NOT NULL,
    channel_context_revision INTEGER,
    channel_outbound_type TEXT,
    channel_outbound_installation_id TEXT,
    channel_outbound_chat_id TEXT,
    channel_outbound_message_ids TEXT,
    channel_typing_settled INTEGER DEFAULT 0 NOT NULL,
CONSTRAINT chat_message_role_check CHECK ((role IN ('user', 'assistant'))),
    CONSTRAINT chat_message_pkey PRIMARY KEY (id)
);

CREATE TABLE chat_pinned_agent (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    "position" REAL DEFAULT 0 NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    CONSTRAINT chat_pinned_agent_pkey PRIMARY KEY (id),
    CONSTRAINT chat_pinned_agent_workspace_id_user_id_agent_id_key UNIQUE (workspace_id, user_id, agent_id)
);

CREATE TABLE chat_session (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    creator_id TEXT NOT NULL,
    title TEXT DEFAULT '' NOT NULL,
    session_id TEXT,
    work_dir TEXT,
    status TEXT DEFAULT 'active' NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    unread_since TEXT,
    runtime_id TEXT,
    last_read_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    is_agent_intro INTEGER DEFAULT 0 NOT NULL,
    pinned_at TEXT,
    project_id TEXT,
    explicitly_created_at TEXT,
CONSTRAINT chat_session_status_check CHECK ((status IN ('active', 'archived'))),
    CONSTRAINT chat_session_pkey PRIMARY KEY (id)
);

CREATE TABLE client_usage_daily (
    user_id TEXT NOT NULL,
    client_type TEXT NOT NULL,
    install_id TEXT NOT NULL,
    activity_date TEXT NOT NULL,
    workspace_id TEXT,
    client_version TEXT NOT NULL,
    os TEXT NOT NULL,
    first_active_at TEXT NOT NULL,
    last_active_at TEXT NOT NULL,
    runtime_probed_at TEXT,
    probe_result TEXT,
    runtime_count INTEGER,
    provider_summary TEXT,
    online_count INTEGER,
    offline_count INTEGER,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT client_usage_daily_check CHECK ((first_active_at <= last_active_at)),
CONSTRAINT client_usage_daily_check1 CHECK ((((runtime_probed_at IS NULL) AND (probe_result IS NULL) AND (runtime_count IS NULL) AND (provider_summary IS NULL) AND (online_count IS NULL) AND (offline_count IS NULL)) OR ((runtime_probed_at IS NOT NULL) AND (probe_result = 'error') AND (runtime_count IS NULL) AND (provider_summary IS NULL) AND (online_count IS NULL) AND (offline_count IS NULL)) OR ((runtime_probed_at IS NOT NULL) AND (probe_result = 'success') AND (runtime_count IS NOT NULL) AND (provider_summary IS NOT NULL) AND (online_count IS NOT NULL) AND (offline_count IS NOT NULL) AND ((online_count + offline_count) = runtime_count) AND (json_type(provider_summary) = 'object')))),
CONSTRAINT client_usage_daily_client_type_check CHECK ((client_type IN ('web', 'desktop'))),
CONSTRAINT client_usage_daily_offline_count_check CHECK ((offline_count >= 0)),
CONSTRAINT client_usage_daily_online_count_check CHECK ((online_count >= 0)),
CONSTRAINT client_usage_daily_probe_result_check CHECK ((probe_result IN ('success', 'error'))),
CONSTRAINT client_usage_daily_runtime_count_check CHECK ((runtime_count >= 0)),
    CONSTRAINT client_usage_daily_pkey PRIMARY KEY (user_id, client_type, install_id, activity_date)
);

CREATE TABLE comment (
    id TEXT NOT NULL,
    issue_id TEXT NOT NULL,
    author_type TEXT NOT NULL,
    author_id TEXT NOT NULL,
    content TEXT NOT NULL,
    type TEXT DEFAULT 'comment' NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    parent_id TEXT,
    workspace_id TEXT NOT NULL,
    resolved_at TEXT,
    resolved_by_type TEXT,
    resolved_by_id TEXT,
    source_task_id TEXT,
    quick_action_id TEXT,
    via_plugin_id TEXT,
    revision INTEGER DEFAULT 1 NOT NULL,
    recovery_settled_at TEXT,
    deleted_at TEXT,
    suppressed_agent_ids TEXT,
CONSTRAINT comment_author_type_check CHECK ((author_type IN ('member', 'agent', 'system', 'plugin'))),
CONSTRAINT comment_resolved_consistency CHECK ((((resolved_at IS NULL) AND (resolved_by_type IS NULL) AND (resolved_by_id IS NULL)) OR ((resolved_at IS NOT NULL) AND (resolved_by_type IS NOT NULL) AND (resolved_by_id IS NOT NULL)))),
CONSTRAINT comment_type_check CHECK ((type IN ('comment', 'status_change', 'progress_update', 'system'))),
    CONSTRAINT comment_pkey PRIMARY KEY (id)
);

CREATE TABLE comment_reaction (
    id TEXT NOT NULL,
    comment_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    actor_type TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    emoji TEXT NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT comment_reaction_actor_type_check CHECK ((actor_type IN ('member', 'agent'))),
    CONSTRAINT comment_reaction_pkey PRIMARY KEY (id),
    CONSTRAINT comment_reaction_comment_id_actor_type_actor_id_emoji_key UNIQUE (comment_id, actor_type, actor_id, emoji)
);

CREATE TABLE contact_sales_inquiry (
    id TEXT NOT NULL,
    first_name TEXT NOT NULL,
    last_name TEXT NOT NULL,
    business_email TEXT NOT NULL,
    company_name TEXT NOT NULL,
    company_size TEXT NOT NULL,
    country_region TEXT NOT NULL,
    use_case TEXT NOT NULL,
    goals TEXT DEFAULT '' NOT NULL,
    consent_outreach INTEGER DEFAULT 0 NOT NULL,
    consent_updates INTEGER DEFAULT 0 NOT NULL,
    submitter_ip TEXT,
    user_agent TEXT DEFAULT '' NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    CONSTRAINT contact_sales_inquiry_pkey PRIMARY KEY (id)
);

CREATE TABLE daemon_connection (
    id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    daemon_id TEXT NOT NULL,
    status TEXT DEFAULT 'disconnected' NOT NULL,
    last_heartbeat_at TEXT,
    runtime_info TEXT DEFAULT '{}' NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT daemon_connection_status_check CHECK ((status IN ('connected', 'disconnected'))),
    CONSTRAINT daemon_connection_pkey PRIMARY KEY (id),
    CONSTRAINT uq_daemon_agent UNIQUE (agent_id, daemon_id)
);

CREATE TABLE daemon_token (
    id TEXT NOT NULL,
    token_hash TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    daemon_id TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    CONSTRAINT daemon_token_pkey PRIMARY KEY (id)
);

CREATE TABLE dingtalk_bot_identity (
    workspace_id TEXT NOT NULL,
    installation_id TEXT NOT NULL,
    bot_name TEXT DEFAULT '' NOT NULL,
    bot_identity_issue TEXT DEFAULT '' NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL
);

CREATE TABLE dingtalk_group_presence (
    workspace_id TEXT NOT NULL,
    installation_id TEXT NOT NULL,
    conversation_id TEXT NOT NULL,
    conversation_title TEXT DEFAULT '' NOT NULL,
    bot_name TEXT DEFAULT '' NOT NULL,
    bot_identity_issue TEXT DEFAULT '' NOT NULL,
    first_seen_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    last_active_at TEXT,
    mention_count INTEGER DEFAULT 0 NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT dingtalk_group_presence_mention_count_check CHECK ((mention_count >= 0))
);

CREATE TABLE dingtalk_group_route (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    installation_id TEXT NOT NULL,
    conversation_id TEXT NOT NULL,
    conversation_title TEXT DEFAULT '' NOT NULL,
    agent_id TEXT NOT NULL,
    revision INTEGER DEFAULT 1 NOT NULL,
    discovered_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL
);

CREATE TABLE feedback (
    id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    workspace_id TEXT,
    message TEXT NOT NULL,
    metadata TEXT DEFAULT '{}' NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    CONSTRAINT feedback_pkey PRIMARY KEY (id)
);

CREATE TABLE github_installation (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    installation_id INTEGER NOT NULL,
    account_login TEXT NOT NULL,
    account_type TEXT DEFAULT 'User' NOT NULL,
    account_avatar_url TEXT,
    connected_by_id TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT github_installation_account_type_check CHECK ((account_type IN ('User', 'Organization'))),
    CONSTRAINT github_installation_pkey PRIMARY KEY (id),
    CONSTRAINT github_installation_workspace_id_installation_id_key UNIQUE (workspace_id, installation_id)
);

CREATE TABLE github_pending_check_suite (
    workspace_id TEXT NOT NULL,
    installation_id INTEGER NOT NULL,
    repo_owner TEXT NOT NULL,
    repo_name TEXT NOT NULL,
    pr_number INTEGER NOT NULL,
    suite_id INTEGER NOT NULL,
    head_sha TEXT NOT NULL,
    app_id INTEGER NOT NULL,
    conclusion TEXT,
    status TEXT NOT NULL,
    suite_updated_at TEXT NOT NULL,
    received_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    CONSTRAINT github_pending_check_suite_pkey PRIMARY KEY (workspace_id, repo_owner, repo_name, pr_number, suite_id)
);

CREATE TABLE github_pending_installation (
    installation_id INTEGER NOT NULL,
    account_login TEXT NOT NULL,
    account_type TEXT DEFAULT 'User' NOT NULL,
    account_avatar_url TEXT,
    received_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT github_pending_installation_account_login_check CHECK ((account_login <> '')),
CONSTRAINT github_pending_installation_account_type_check CHECK ((account_type IN ('User', 'Organization'))),
    CONSTRAINT github_pending_installation_pkey PRIMARY KEY (installation_id)
);

CREATE TABLE github_pull_request (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    installation_id INTEGER NOT NULL,
    repo_owner TEXT NOT NULL,
    repo_name TEXT NOT NULL,
    pr_number INTEGER NOT NULL,
    title TEXT NOT NULL,
    state TEXT NOT NULL,
    html_url TEXT NOT NULL,
    branch TEXT,
    author_login TEXT,
    author_avatar_url TEXT,
    merged_at TEXT,
    closed_at TEXT,
    pr_created_at TEXT NOT NULL,
    pr_updated_at TEXT NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    head_sha TEXT DEFAULT '' NOT NULL,
    mergeable_state TEXT,
    additions INTEGER DEFAULT 0 NOT NULL,
    deletions INTEGER DEFAULT 0 NOT NULL,
    changed_files INTEGER DEFAULT 0 NOT NULL,
    api_mergeable TEXT,
    api_merge_state_status TEXT,
    checks_rollup_state TEXT,
    snapshot_head_sha TEXT DEFAULT '' NOT NULL,
    snapshot_fetched_at TEXT,
CONSTRAINT github_pull_request_state_check CHECK ((state IN ('open', 'closed', 'merged', 'draft'))),
    CONSTRAINT github_pull_request_pkey PRIMARY KEY (id),
    CONSTRAINT github_pull_request_workspace_id_repo_owner_repo_name_pr_nu_key UNIQUE (workspace_id, repo_owner, repo_name, pr_number)
);

CREATE TABLE github_pull_request_check_run (
    pr_id TEXT NOT NULL,
    head_sha TEXT NOT NULL,
    ordinal INTEGER NOT NULL,
    name TEXT NOT NULL,
    status TEXT NOT NULL,
    conclusion TEXT,
    details_url TEXT,
    is_status_context INTEGER DEFAULT 0 NOT NULL
);

CREATE TABLE github_pull_request_check_suite (
    pr_id TEXT NOT NULL,
    suite_id INTEGER NOT NULL,
    head_sha TEXT NOT NULL,
    app_id INTEGER NOT NULL,
    conclusion TEXT,
    status TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CONSTRAINT github_pull_request_check_suite_pkey PRIMARY KEY (pr_id, suite_id)
);

CREATE TABLE inbox_item (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    recipient_type TEXT NOT NULL,
    recipient_id TEXT NOT NULL,
    type TEXT NOT NULL,
    severity TEXT DEFAULT 'info' NOT NULL,
    issue_id TEXT,
    title TEXT NOT NULL,
    body TEXT,
    read INTEGER DEFAULT 0 NOT NULL,
    archived INTEGER DEFAULT 0 NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    actor_type TEXT,
    actor_id TEXT,
    details TEXT DEFAULT '{}',
CONSTRAINT inbox_item_recipient_type_check CHECK ((recipient_type IN ('member', 'agent'))),
CONSTRAINT inbox_item_severity_check CHECK ((severity IN ('action_required', 'attention', 'info'))),
    CONSTRAINT inbox_item_pkey PRIMARY KEY (id)
);

CREATE TABLE instance_telemetry_state (
    singleton INTEGER DEFAULT 1 NOT NULL,
    instance_id TEXT NOT NULL,
    last_successful_day TEXT,
    pending_day TEXT,
    pending_body BLOB,
    next_attempt_at TEXT,
    attempt_count INTEGER DEFAULT 0 NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT instance_telemetry_state_attempt_count_check CHECK ((attempt_count >= 0)),
CONSTRAINT instance_telemetry_state_check CHECK ((((pending_day IS NULL) AND (pending_body IS NULL) AND (next_attempt_at IS NULL) AND (attempt_count = 0)) OR ((pending_day IS NOT NULL) AND (pending_body IS NOT NULL) AND (next_attempt_at IS NOT NULL)))),
CONSTRAINT instance_telemetry_state_singleton_check CHECK (singleton),
    CONSTRAINT instance_telemetry_state_pkey PRIMARY KEY (singleton)
);

CREATE TABLE issue (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT,
    status TEXT DEFAULT 'backlog' NOT NULL,
    priority TEXT DEFAULT 'none' NOT NULL,
    assignee_type TEXT,
    assignee_id TEXT,
    creator_type TEXT NOT NULL,
    creator_id TEXT NOT NULL,
    parent_issue_id TEXT,
    acceptance_criteria TEXT DEFAULT '[]' NOT NULL,
    context_refs TEXT DEFAULT '[]' NOT NULL,
    "position" REAL DEFAULT 0 NOT NULL,
    due_date TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    number INTEGER DEFAULT 0 NOT NULL,
    project_id TEXT,
    origin_type TEXT,
    origin_id TEXT,
    first_executed_at TEXT,
    start_date TEXT,
    metadata TEXT DEFAULT '{}' NOT NULL,
    stage INTEGER,
    properties TEXT DEFAULT '{}' NOT NULL,
    revision INTEGER DEFAULT 1 NOT NULL,
    last_activity_at TEXT,
    triage_state TEXT,
    duplicate_of_issue_id TEXT,
CONSTRAINT issue_assignee_type_check CHECK ((assignee_type IN ('member', 'agent', 'squad'))),
CONSTRAINT issue_creator_type_check CHECK ((creator_type IN ('member', 'agent'))),
CONSTRAINT issue_metadata_is_object CHECK ((json_type(metadata) = 'object')),
CONSTRAINT issue_metadata_size_limit CHECK ((length(CAST(metadata AS BLOB)) <= 8192)),
CONSTRAINT issue_origin_type_check CHECK ((origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'dingtalk_chat', 'wecom_chat', 'telegram_chat'))),
CONSTRAINT issue_priority_check CHECK ((priority IN ('urgent', 'high', 'medium', 'low', 'none'))),
CONSTRAINT issue_properties_is_object CHECK ((json_type(properties) = 'object')),
CONSTRAINT issue_properties_size_limit CHECK ((length(CAST(properties AS BLOB)) <= 16384)),
CONSTRAINT issue_stage_check CHECK (((stage IS NULL) OR (stage >= 1))),
CONSTRAINT issue_status_format_check CHECK (((length(status) BETWEEN 1 AND 32 AND substr(status, 1, 1) GLOB '[a-z0-9]' AND status NOT GLOB '*[^a-z0-9_]*'))),
CONSTRAINT issue_triage_state_known CHECK (((triage_state IS NULL) OR (triage_state = 'pending'))),
    CONSTRAINT issue_pkey PRIMARY KEY (id),
    CONSTRAINT uq_issue_workspace_number UNIQUE (workspace_id, number)
);

CREATE TABLE issue_child_event (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    parent_id TEXT NOT NULL,
    child_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    source_task_id TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    claimed_at TEXT,
    processed_at TEXT,
CONSTRAINT issue_child_event_kind_check CHECK ((kind IN ('closed', 'reopened', 'attached', 'detached', 'restaged')))
);

CREATE TABLE issue_dependency (
    id TEXT NOT NULL,
    issue_id TEXT NOT NULL,
    depends_on_issue_id TEXT NOT NULL,
    type TEXT NOT NULL,
CONSTRAINT issue_dependency_type_check CHECK ((type IN ('blocks', 'blocked_by', 'related'))),
    CONSTRAINT issue_dependency_pkey PRIMARY KEY (id)
);

CREATE TABLE issue_label (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    name TEXT NOT NULL,
    color TEXT NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    resource_type TEXT DEFAULT 'issue' NOT NULL,
    description TEXT DEFAULT '' NOT NULL,
CONSTRAINT issue_label_resource_type_check CHECK ((resource_type IN ('issue', 'agent', 'skill'))),
    CONSTRAINT issue_label_pkey PRIMARY KEY (id)
);

CREATE TABLE issue_pr_automation (
    issue_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    auto_complete_disabled INTEGER DEFAULT 0 NOT NULL,
    updated_by_type TEXT,
    updated_by_id TEXT,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    CONSTRAINT issue_pr_automation_pkey PRIMARY KEY (issue_id)
);

CREATE TABLE issue_property (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    description TEXT DEFAULT '' NOT NULL,
    config TEXT DEFAULT '{}' NOT NULL,
    "position" REAL DEFAULT 0 NOT NULL,
    archived_at TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    icon TEXT DEFAULT '' NOT NULL,
CONSTRAINT issue_property_config_check CHECK ((json_type(config) = 'object')),
CONSTRAINT issue_property_type_check CHECK ((type IN ('text', 'number', 'select', 'multi_select', 'date', 'checkbox', 'url', 'actor', 'multi_actor', 'multi_text', 'multi_url'))),
    CONSTRAINT issue_property_pkey PRIMARY KEY (id)
);

CREATE TABLE issue_pull_request (
    issue_id TEXT NOT NULL,
    pull_request_id TEXT NOT NULL,
    linked_by_type TEXT,
    linked_by_id TEXT,
    linked_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    close_intent INTEGER DEFAULT 0 NOT NULL,
    CONSTRAINT issue_pull_request_pkey PRIMARY KEY (issue_id, pull_request_id)
);

CREATE TABLE issue_pull_request_exclusion (
    issue_id TEXT NOT NULL,
    pull_request_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    excluded_by_type TEXT,
    excluded_by_id TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    CONSTRAINT issue_pull_request_exclusion_pkey PRIMARY KEY (issue_id, pull_request_id)
);

CREATE TABLE issue_reaction (
    id TEXT NOT NULL,
    issue_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    actor_type TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    emoji TEXT NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT issue_reaction_actor_type_check CHECK ((actor_type IN ('member', 'agent'))),
    CONSTRAINT issue_reaction_pkey PRIMARY KEY (id),
    CONSTRAINT issue_reaction_issue_id_actor_type_actor_id_emoji_key UNIQUE (issue_id, actor_type, actor_id, emoji)
);

CREATE TABLE issue_source_context (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    issue_id TEXT,
    origin_task_id TEXT,
    source_issue_id TEXT NOT NULL,
    anchor_comment_id TEXT NOT NULL,
    captured_by_user_id TEXT NOT NULL,
    snapshot_version INTEGER NOT NULL,
    snapshot TEXT NOT NULL,
    capture_digest TEXT NOT NULL,
    state TEXT NOT NULL,
    captured_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    attached_at TEXT,
CONSTRAINT issue_source_context_check CHECK ((((state = 'pending') AND (issue_id IS NULL) AND (origin_task_id IS NOT NULL) AND (attached_at IS NULL)) OR ((state = 'attached') AND (issue_id IS NOT NULL) AND (attached_at IS NOT NULL)) OR ((state = 'abandoned') AND (issue_id IS NULL) AND (attached_at IS NULL)))),
CONSTRAINT issue_source_context_state_check CHECK ((state IN ('pending', 'attached', 'abandoned')))
);

CREATE TABLE issue_source_context_object_intent (
    storage_key TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    source_context_id TEXT NOT NULL,
    attachment_id TEXT NOT NULL,
    object_url TEXT NOT NULL,
    state TEXT DEFAULT 'pending' NOT NULL,
    lease_token TEXT,
    lease_expires_at TEXT,
    next_attempt_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    last_error TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT issue_source_context_object_intent_state_check CHECK ((state IN ('pending', 'deleting')))
);

CREATE TABLE issue_status (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    "key" TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT DEFAULT '' NOT NULL,
    category TEXT NOT NULL,
    color TEXT NOT NULL,
    is_system INTEGER DEFAULT 0 NOT NULL,
    "position" REAL DEFAULT 0 NOT NULL,
    archived_at TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    icon TEXT DEFAULT '' NOT NULL,
CONSTRAINT issue_status_category_check CHECK ((category IN ('unstarted', 'started', 'done', 'closed'))),
CONSTRAINT issue_status_color_check CHECK (((color GLOB '#[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]'))),
CONSTRAINT issue_status_description_check CHECK ((length(description) <= 256)),
CONSTRAINT issue_status_key_check CHECK (((length("key") BETWEEN 1 AND 32 AND substr("key", 1, 1) GLOB '[a-z0-9]' AND "key" NOT GLOB '*[^a-z0-9_]*'))),
CONSTRAINT issue_status_name_check CHECK (((length(name) >= 1) AND (length(name) <= 64))),
CONSTRAINT issue_status_system_is_canonical CHECK (((NOT is_system) OR (((((((("key" = 'backlog') AND (category = 'unstarted')) OR (("key" = 'todo') AND (category = 'unstarted'))) OR (("key" = 'in_progress') AND (category = 'started'))) OR (("key" = 'in_review') AND (category = 'started'))) OR (("key" = 'blocked') AND (category = 'started'))) OR (("key" = 'done') AND (category = 'done'))) OR (("key" = 'cancelled') AND (category = 'closed'))))),
CONSTRAINT issue_status_system_not_archivable CHECK (((NOT is_system) OR (archived_at IS NULL))),
    CONSTRAINT issue_status_pkey PRIMARY KEY (id)
);

CREATE TABLE issue_subscriber (
    issue_id TEXT NOT NULL,
    user_type TEXT NOT NULL,
    user_id TEXT NOT NULL,
    reason TEXT NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    unsubscribed_at TEXT,
    opt_out_scope TEXT,
CONSTRAINT issue_subscriber_opt_out_scope_check CHECK ((opt_out_scope IN ('issue', 'subtree'))),
CONSTRAINT issue_subscriber_reason_check CHECK ((reason IN ('creator', 'assignee', 'commenter', 'mentioned', 'manual', 'autopilot', 'delegated'))),
CONSTRAINT issue_subscriber_user_type_check CHECK ((user_type IN ('member', 'agent'))),
    CONSTRAINT issue_subscriber_pkey PRIMARY KEY (issue_id, user_type, user_id)
);

CREATE TABLE issue_to_label (
    issue_id TEXT NOT NULL,
    label_id TEXT NOT NULL,
    CONSTRAINT issue_to_label_pkey PRIMARY KEY (issue_id, label_id)
);

CREATE TABLE issue_vcs_pull_request (
    issue_id TEXT NOT NULL,
    pull_request_id TEXT NOT NULL,
    close_intent INTEGER DEFAULT 0 NOT NULL,
    linked_by_type TEXT,
    linked_by_id TEXT,
    linked_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    CONSTRAINT issue_vcs_pull_request_pkey PRIMARY KEY (issue_id, pull_request_id)
);

CREATE TABLE issue_view (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    owner_id TEXT NOT NULL,
    name TEXT NOT NULL,
    scope_type TEXT NOT NULL,
    scope_id TEXT,
    scope_variant TEXT,
    visibility TEXT DEFAULT 'private' NOT NULL,
    definition_version INTEGER DEFAULT 1 NOT NULL,
    "query" TEXT NOT NULL,
    display TEXT DEFAULT '{}' NOT NULL,
    revision INTEGER DEFAULT 1 NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT issue_view_check CHECK ((((scope_type = 'project') AND (scope_id IS NOT NULL)) OR ((scope_type IN ('workspace', 'my')) AND (scope_id IS NULL)))),
CONSTRAINT issue_view_check2 CHECK (((scope_type <> 'my') OR (visibility = 'private'))),
CONSTRAINT issue_view_display_check CHECK ((json_type(display) = 'object')),
CONSTRAINT issue_view_name_check CHECK (((length(name) >= 1) AND (length(name) <= 80))),
CONSTRAINT issue_view_query_check CHECK ((json_type("query") = 'object')),
CONSTRAINT issue_view_scope_type_check CHECK ((scope_type IN ('workspace', 'my', 'project'))),
CONSTRAINT issue_view_scope_variant_check CHECK ((scope_variant IN ('assigned', 'created', 'involved', 'any', 'members', 'agents'))),
CONSTRAINT issue_view_scope_variant_pairing CHECK ((((scope_type = 'my') AND (scope_variant IN ('assigned', 'created', 'involved', 'any'))) OR ((scope_type = 'workspace') AND ((scope_variant IS NULL) OR (scope_variant IN ('members', 'agents')))) OR ((scope_type = 'project') AND ((scope_variant IS NULL) OR (scope_variant IN ('members', 'agents')))))),
CONSTRAINT issue_view_visibility_check CHECK ((visibility IN ('private', 'workspace'))),
    CONSTRAINT issue_view_pkey PRIMARY KEY (id)
);

CREATE TABLE issue_view_preference (
    workspace_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    scope_type TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    prefs TEXT DEFAULT '{}' NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT issue_view_preference_prefs_check CHECK ((json_type(prefs) = 'object')),
CONSTRAINT issue_view_preference_scope_type_check CHECK ((scope_type IN ('workspace', 'my', 'project'))),
    CONSTRAINT issue_view_preference_pkey PRIMARY KEY (workspace_id, user_id, scope_type, scope_id)
);

CREATE TABLE issue_wakeup (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    issue_id TEXT NOT NULL,
    agent_id TEXT,
    created_by TEXT,
    source_task_id TEXT,
    parent_comment_id TEXT,
    instruction TEXT NOT NULL,
    kind TEXT NOT NULL,
    mode TEXT NOT NULL,
    event_types TEXT DEFAULT '[]' NOT NULL,
    filter_agent_id TEXT,
    filter_task_id TEXT,
    interval_seconds INTEGER,
    cron_expression TEXT,
    timezone TEXT DEFAULT 'UTC' NOT NULL,
    next_fire_at TEXT,
    enabled INTEGER DEFAULT 1 NOT NULL,
    disabled_at TEXT,
    revision INTEGER DEFAULT 1 NOT NULL,
    last_task_id TEXT,
    last_error TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    filter_actor_type TEXT,
    filter_actor_id TEXT,
    expires_at TEXT,
    expiry_seconds INTEGER,
    on_timeout TEXT,
    timed_out_at TEXT,
    system_rule TEXT,
    customized_at TEXT,
    condition TEXT,
    condition_state TEXT DEFAULT '' NOT NULL,
    max_fires INTEGER,
    fire_count INTEGER DEFAULT 0 NOT NULL,
    paused_reason TEXT,
CONSTRAINT issue_wakeup_kind_check CHECK ((kind IN ('event', 'at', 'every', 'cron'))),
CONSTRAINT issue_wakeup_max_fires_check CHECK (((max_fires >= 1) AND (max_fires <= 1000))),
CONSTRAINT issue_wakeup_mode_check CHECK ((mode IN ('once', 'continuous'))),
CONSTRAINT issue_wakeup_on_timeout_check CHECK ((on_timeout IN ('wake', 'end'))),
CONSTRAINT issue_wakeup_paused_reason_check CHECK ((paused_reason IN ('max_fires', 'loop', 'rate'))),
CONSTRAINT issue_wakeup_system_rule_check CHECK ((system_rule = 'child_done')),
    CONSTRAINT issue_wakeup_actor_filter CHECK ((((filter_actor_type IS NULL) AND (filter_actor_id IS NULL)) OR ((filter_actor_type IS NOT NULL) AND (filter_actor_type IN ('member', 'agent')) AND (filter_actor_id IS NOT NULL) AND (kind = 'event'))))
);

CREATE TABLE issue_wakeup_receipt (
    id TEXT NOT NULL,
    wakeup_id TEXT NOT NULL,
    revision INTEGER NOT NULL,
    event_key TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload TEXT NOT NULL,
    task_id TEXT,
    processed_at TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    coalesce_key TEXT
);

CREATE TABLE issue_workflow (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT DEFAULT '' NOT NULL,
    initial_status_key TEXT NOT NULL,
    steps TEXT DEFAULT '[]' NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    is_default INTEGER DEFAULT 0 NOT NULL,
CONSTRAINT issue_workflow_description_check CHECK ((length(description) <= 256)),
CONSTRAINT issue_workflow_name_check CHECK (((length(name) >= 1) AND (length(name) <= 64))),
CONSTRAINT issue_workflow_steps_check CHECK ((json_type(steps) = 'array')),
    CONSTRAINT issue_workflow_pkey PRIMARY KEY (id)
);

CREATE TABLE lark_binding_token (
    token_hash TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    installation_id TEXT NOT NULL,
    lark_open_id TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    consumed_at TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT lark_binding_token_ttl_cap CHECK ((expires_at <= (strftime('%Y-%m-%dT%H:%M:%fZ', created_at, '+15 minutes')))),
    CONSTRAINT lark_binding_token_pkey PRIMARY KEY (token_hash)
);

CREATE TABLE lark_chat_session_binding (
    id TEXT NOT NULL,
    chat_session_id TEXT NOT NULL,
    installation_id TEXT NOT NULL,
    lark_chat_id TEXT NOT NULL,
    lark_chat_type TEXT NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    last_lark_message_id TEXT,
    last_lark_thread_id TEXT,
CONSTRAINT lark_chat_session_binding_lark_chat_type_check CHECK ((lark_chat_type IN ('p2p', 'group'))),
    CONSTRAINT lark_chat_session_binding_pkey PRIMARY KEY (id),
    CONSTRAINT lark_chat_session_binding_chat_session_id_key UNIQUE (chat_session_id),
    CONSTRAINT lark_chat_session_binding_installation_id_lark_chat_id_key UNIQUE (installation_id, lark_chat_id)
);

CREATE TABLE lark_inbound_audit (
    id TEXT NOT NULL,
    installation_id TEXT,
    lark_chat_id TEXT,
    event_type TEXT NOT NULL,
    lark_event_id TEXT,
    lark_message_id TEXT,
    drop_reason TEXT NOT NULL,
    received_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    CONSTRAINT lark_inbound_audit_pkey PRIMARY KEY (id)
);

CREATE TABLE lark_inbound_message_dedup (
    installation_id TEXT NOT NULL,
    message_id TEXT NOT NULL,
    received_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    processed_at TEXT,
    claim_token TEXT NOT NULL,
    CONSTRAINT lark_inbound_message_dedup_pkey PRIMARY KEY (installation_id, message_id)
);

CREATE TABLE lark_installation (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    app_id TEXT NOT NULL,
    app_secret_encrypted BLOB NOT NULL,
    tenant_key TEXT,
    bot_open_id TEXT NOT NULL,
    installer_user_id TEXT NOT NULL,
    status TEXT DEFAULT 'active' NOT NULL,
    ws_lease_token TEXT,
    ws_lease_expires_at TEXT,
    installed_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    bot_union_id TEXT,
    region TEXT DEFAULT 'feishu' NOT NULL,
CONSTRAINT lark_installation_region_check CHECK ((region IN ('feishu', 'lark'))),
CONSTRAINT lark_installation_status_check CHECK ((status IN ('active', 'revoked'))),
    CONSTRAINT lark_installation_pkey PRIMARY KEY (id),
    CONSTRAINT lark_installation_app_id_key UNIQUE (app_id),
    CONSTRAINT lark_installation_id_workspace_id_key UNIQUE (id, workspace_id),
    CONSTRAINT lark_installation_workspace_id_agent_id_key UNIQUE (workspace_id, agent_id)
);

CREATE TABLE lark_outbound_card_message (
    id TEXT NOT NULL,
    chat_session_id TEXT NOT NULL,
    task_id TEXT,
    lark_chat_id TEXT NOT NULL,
    lark_card_message_id TEXT NOT NULL,
    status TEXT DEFAULT 'pending' NOT NULL,
    last_patched_at TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT lark_outbound_card_message_status_check CHECK ((status IN ('pending', 'streaming', 'final', 'error'))),
    CONSTRAINT lark_outbound_card_message_pkey PRIMARY KEY (id)
);

CREATE TABLE lark_user_binding (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    multica_user_id TEXT NOT NULL,
    installation_id TEXT NOT NULL,
    lark_open_id TEXT NOT NULL,
    union_id TEXT,
    bound_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    CONSTRAINT lark_user_binding_pkey PRIMARY KEY (id),
    CONSTRAINT lark_user_binding_installation_id_lark_open_id_key UNIQUE (installation_id, lark_open_id)
);

CREATE TABLE maintenance_job (
    id TEXT NOT NULL,
    job_type TEXT NOT NULL,
    job_version INTEGER NOT NULL,
    scope_key TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    status TEXT NOT NULL,
    revision INTEGER DEFAULT 0 NOT NULL,
    dry_run INTEGER DEFAULT 1 NOT NULL,
    options TEXT NOT NULL,
    parameters TEXT DEFAULT '{}' NOT NULL,
    checkpoint TEXT DEFAULT '{}' NOT NULL,
    progress TEXT DEFAULT '{}' NOT NULL,
    result TEXT DEFAULT '{}' NOT NULL,
    last_error TEXT DEFAULT '' NOT NULL,
    next_allowed_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    completed_at TEXT,
CONSTRAINT maintenance_job_check CHECK (((json_type(options) = 'object') AND (json_type(parameters) = 'object') AND (json_type(checkpoint) = 'object') AND (json_type(progress) = 'object') AND (json_type(result) = 'object'))),
CONSTRAINT maintenance_job_job_version_check CHECK ((job_version > 0)),
CONSTRAINT maintenance_job_status_check CHECK ((status IN ('ready', 'paused', 'completed', 'cancelled')))
);

CREATE TABLE member (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    role TEXT NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT member_role_check CHECK ((role IN ('owner', 'admin', 'member'))),
    CONSTRAINT member_pkey PRIMARY KEY (id),
    CONSTRAINT member_workspace_id_user_id_key UNIQUE (workspace_id, user_id)
);

CREATE TABLE notification_preference (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    preferences TEXT DEFAULT '{}' NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    CONSTRAINT notification_preference_pkey PRIMARY KEY (id),
    CONSTRAINT notification_preference_workspace_id_user_id_key UNIQUE (workspace_id, user_id)
);

CREATE TABLE orchestrator_ci_retry (
    head_sha TEXT NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    CONSTRAINT orchestrator_ci_retry_pkey PRIMARY KEY (head_sha)
);

CREATE TABLE orchestrator_event (
    id INTEGER NOT NULL,
    issue_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    processed_at TEXT,
    CONSTRAINT orchestrator_event_pkey PRIMARY KEY (id)
);

CREATE TABLE orchestrator_issue_state (
    issue_id TEXT NOT NULL,
    failures INTEGER DEFAULT 0 NOT NULL,
    next_at TEXT,
    last_error TEXT DEFAULT '' NOT NULL,
    reminded_at TEXT,
    last_wait TEXT DEFAULT '' NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    CONSTRAINT orchestrator_issue_state_pkey PRIMARY KEY (issue_id)
);

CREATE TABLE orchestrator_team (
    project_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    spec TEXT NOT NULL,
    spec_yaml TEXT DEFAULT '' NOT NULL,
    paused INTEGER DEFAULT 0 NOT NULL,
    paused_reason TEXT DEFAULT '' NOT NULL,
    human_user_id TEXT,
    applied_by TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    CONSTRAINT orchestrator_team_pkey PRIMARY KEY (project_id)
);

CREATE TABLE orchestrator_transition (
    id INTEGER NOT NULL,
    workspace_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    issue_id TEXT NOT NULL,
    tid TEXT NOT NULL,
    idem_key TEXT NOT NULL,
    decision TEXT NOT NULL,
    state TEXT NOT NULL,
    error TEXT DEFAULT '' NOT NULL,
    source TEXT DEFAULT '' NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    finished_at TEXT,
CONSTRAINT orchestrator_transition_state_check CHECK ((state IN ('started', 'done', 'failed', 'shadow'))),
    CONSTRAINT orchestrator_transition_pkey PRIMARY KEY (id),
    CONSTRAINT orchestrator_transition_idem_key_key UNIQUE (idem_key)
);

CREATE TABLE personal_access_token (
    id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    name TEXT NOT NULL,
    token_hash TEXT NOT NULL,
    token_prefix TEXT NOT NULL,
    expires_at TEXT,
    last_used_at TEXT,
    revoked INTEGER DEFAULT 0 NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    CONSTRAINT personal_access_token_pkey PRIMARY KEY (id)
);

CREATE TABLE pinned_item (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    item_type TEXT NOT NULL,
    item_id TEXT NOT NULL,
    "position" REAL DEFAULT 0 NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT pinned_item_item_type_check CHECK ((item_type IN ('issue', 'project', 'view'))),
    CONSTRAINT pinned_item_pkey PRIMARY KEY (id),
    CONSTRAINT pinned_item_workspace_id_user_id_item_type_item_id_key UNIQUE (workspace_id, user_id, item_type, item_id)
);

CREATE TABLE plugin_hook_schedule (
    id TEXT NOT NULL,
    installation_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    hook_key TEXT NOT NULL,
    cron_expression TEXT NOT NULL,
    timezone TEXT NOT NULL,
    generation TEXT NOT NULL,
    activated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    next_run_at TEXT,
    enabled INTEGER DEFAULT 1 NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT plugin_hook_schedule_cron_expression_check CHECK (((length(cron_expression) >= 1) AND (length(cron_expression) <= 255))),
CONSTRAINT plugin_hook_schedule_hook_key_check CHECK (((length(hook_key) >= 1) AND (length(hook_key) <= 128))),
CONSTRAINT plugin_hook_schedule_timezone_check CHECK (((length(timezone) >= 1) AND (length(timezone) <= 255))),
    CONSTRAINT plugin_hook_schedule_pkey PRIMARY KEY (id)
);

CREATE TABLE plugin_installation (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    plugin_key TEXT NOT NULL,
    version TEXT NOT NULL,
    manifest TEXT NOT NULL,
    granted_scopes TEXT DEFAULT '[]' NOT NULL,
    config TEXT DEFAULT '{}' NOT NULL,
    enabled INTEGER DEFAULT 1 NOT NULL,
    installed_by TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    token_hash TEXT,
    token_rotated_at TEXT,
    mcp_approvals TEXT DEFAULT '{}' NOT NULL,
    package_version_id TEXT NOT NULL,
CONSTRAINT plugin_installation_config_check CHECK ((json_type(config) = 'object')),
CONSTRAINT plugin_installation_granted_scopes_check CHECK ((json_type(granted_scopes) = 'array')),
CONSTRAINT plugin_installation_manifest_check CHECK ((json_type(manifest) = 'object')),
CONSTRAINT plugin_installation_mcp_approvals_check CHECK ((json_type(mcp_approvals) = 'object')),
CONSTRAINT plugin_installation_plugin_key_check CHECK (((length(plugin_key) >= 3) AND (length(plugin_key) <= 255))),
CONSTRAINT plugin_installation_version_check CHECK (((length(version) >= 1) AND (length(version) <= 64))),
    CONSTRAINT plugin_installation_pkey PRIMARY KEY (id)
);

CREATE TABLE plugin_invocation (
    id TEXT NOT NULL,
    installation_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    hook_key TEXT NOT NULL,
    "trigger" TEXT NOT NULL,
    status TEXT NOT NULL,
    event_type TEXT,
    attempt INTEGER DEFAULT 1 NOT NULL,
    latency_ms INTEGER DEFAULT 0 NOT NULL,
    error TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    delivery_id TEXT,
    planned_at TEXT,
CONSTRAINT plugin_invocation_attempt_check CHECK (((attempt >= 1) AND (attempt <= 10))),
CONSTRAINT plugin_invocation_delivery_id_check CHECK (((delivery_id IS NULL) OR ((length(delivery_id) >= 1) AND (length(delivery_id) <= 128)))),
CONSTRAINT plugin_invocation_error_check CHECK (((error IS NULL) OR (length(error) <= 500))),
CONSTRAINT plugin_invocation_hook_key_check CHECK (((length(hook_key) >= 1) AND (length(hook_key) <= 128))),
CONSTRAINT plugin_invocation_latency_ms_check CHECK ((latency_ms >= 0)),
CONSTRAINT plugin_invocation_status_check CHECK ((status IN ('ok', 'failed', 'timeout', 'refused'))),
CONSTRAINT plugin_invocation_trigger_check CHECK (("trigger" IN ('ui', 'manual', 'event', 'agent', 'schedule'))),
    CONSTRAINT plugin_invocation_pkey PRIMARY KEY (id)
);

CREATE TABLE plugin_package (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    plugin_key TEXT NOT NULL,
    name TEXT NOT NULL,
    created_by TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT plugin_package_name_check CHECK (((length(name) >= 1) AND (length(name) <= 160))),
CONSTRAINT plugin_package_plugin_key_check CHECK (((length(plugin_key) >= 3) AND (length(plugin_key) <= 255))),
    CONSTRAINT plugin_package_pkey PRIMARY KEY (id)
);

CREATE TABLE plugin_package_file (
    id TEXT NOT NULL,
    version_id TEXT NOT NULL,
    path TEXT NOT NULL,
    content BLOB NOT NULL,
    size_bytes INTEGER NOT NULL,
    sha256 TEXT NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT plugin_package_file_path_check CHECK (((length(path) >= 1) AND (length(path) <= 1024))),
CONSTRAINT plugin_package_file_sha256_check CHECK ((length(sha256) = 64)),
CONSTRAINT plugin_package_file_size_bytes_check CHECK ((size_bytes >= 0)),
    CONSTRAINT plugin_package_file_pkey PRIMARY KEY (id)
);

CREATE TABLE plugin_package_version (
    id TEXT NOT NULL,
    package_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    version TEXT NOT NULL,
    manifest TEXT NOT NULL,
    digest TEXT NOT NULL,
    size_bytes INTEGER NOT NULL,
    published_by TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT plugin_package_version_digest_check CHECK ((length(digest) = 64)),
CONSTRAINT plugin_package_version_manifest_check CHECK ((json_type(manifest) = 'object')),
CONSTRAINT plugin_package_version_size_bytes_check CHECK ((size_bytes >= 0)),
CONSTRAINT plugin_package_version_version_check CHECK (((length(version) >= 1) AND (length(version) <= 64))),
    CONSTRAINT plugin_package_version_pkey PRIMARY KEY (id)
);

CREATE TABLE plugin_secret (
    id TEXT NOT NULL,
    installation_id TEXT NOT NULL,
    "key" TEXT NOT NULL,
    ciphertext BLOB NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT plugin_secret_ciphertext_check CHECK ((length(CAST(ciphertext AS BLOB)) > 0)),
CONSTRAINT plugin_secret_key_check CHECK (((length("key") >= 1) AND (length("key") <= 128))),
    CONSTRAINT plugin_secret_pkey PRIMARY KEY (id)
);

CREATE TABLE plugin_storage (
    id TEXT NOT NULL,
    installation_id TEXT NOT NULL,
    scope_type TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    "key" TEXT NOT NULL,
    value TEXT NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT plugin_storage_key_check CHECK (((length(CAST("key" AS BLOB)) >= 1) AND (length(CAST("key" AS BLOB)) <= 1024))),
CONSTRAINT plugin_storage_scope_type_check CHECK ((scope_type IN ('workspace', 'user'))),
CONSTRAINT plugin_storage_value_check CHECK ((length(CAST(value AS BLOB)) <= 102400)),
    CONSTRAINT plugin_storage_pkey PRIMARY KEY (id)
);

CREATE TABLE project (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT,
    icon TEXT,
    status TEXT DEFAULT 'planned' NOT NULL,
    lead_type TEXT,
    lead_id TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    priority TEXT DEFAULT 'none' NOT NULL,
    start_date TEXT,
    due_date TEXT,
    workflow_id TEXT,
CONSTRAINT project_lead_type_check CHECK ((lead_type IN ('member', 'agent'))),
CONSTRAINT project_priority_check CHECK ((priority IN ('urgent', 'high', 'medium', 'low', 'none'))),
CONSTRAINT project_status_check CHECK ((status IN ('planned', 'in_progress', 'paused', 'completed', 'cancelled'))),
    CONSTRAINT project_pkey PRIMARY KEY (id)
);

CREATE TABLE project_resource (
    id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_ref TEXT NOT NULL,
    label TEXT,
    "position" INTEGER DEFAULT 0 NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    created_by TEXT,
    CONSTRAINT project_resource_pkey PRIMARY KEY (id),
    CONSTRAINT project_resource_project_id_resource_type_resource_ref_key UNIQUE (project_id, resource_type, resource_ref)
);

CREATE TABLE quick_action (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT DEFAULT '' NOT NULL,
    assignee_type TEXT NOT NULL,
    assignee_id TEXT NOT NULL,
    prompt TEXT NOT NULL,
    visibility TEXT DEFAULT 'public' NOT NULL,
    status TEXT DEFAULT 'active' NOT NULL,
    last_used_at TEXT,
    use_count INTEGER DEFAULT 0 NOT NULL,
    created_by_type TEXT NOT NULL,
    created_by_id TEXT NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT quick_action_assignee_type_check CHECK ((assignee_type IN ('agent', 'squad'))),
CONSTRAINT quick_action_created_by_type_check CHECK ((created_by_type IN ('member', 'agent'))),
CONSTRAINT quick_action_status_check CHECK ((status IN ('active', 'archived'))),
CONSTRAINT quick_action_visibility_check CHECK ((visibility IN ('private', 'public'))),
    CONSTRAINT quick_action_pkey PRIMARY KEY (id)
);

CREATE TABLE runtime_profile (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    display_name TEXT NOT NULL,
    protocol_family TEXT NOT NULL,
    command_name TEXT NOT NULL,
    description TEXT,
    fixed_args TEXT DEFAULT '[]' NOT NULL,
    visibility TEXT DEFAULT 'workspace' NOT NULL,
    created_by TEXT,
    enabled INTEGER DEFAULT 1 NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    runtime_type TEXT DEFAULT '' NOT NULL,
CONSTRAINT runtime_profile_visibility_check CHECK ((visibility IN ('workspace', 'private'))),
    CONSTRAINT runtime_profile_protocol_family_check CHECK ((protocol_family IN ('claude', 'codebuddy', 'codex', 'copilot', 'opencode', 'codearts', 'openclaw', 'hermes', 'pi', 'cursor', 'kimi', 'reasonix', 'dsh', 'kiro', 'antigravity', 'qoder', 'qoderclicn', 'traecli', 'deveco', 'gbot', 'grok', 'qwen', 'qwenpaw', 'mcode', 'dim', 'zeroclaw', 'yielded'))),
    CONSTRAINT runtime_profile_pkey PRIMARY KEY (id),
    CONSTRAINT runtime_profile_workspace_id_display_name_key UNIQUE (workspace_id, display_name)
);

CREATE TABLE schema_migrations (
    version TEXT NOT NULL,
    applied_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    CONSTRAINT schema_migrations_pkey PRIMARY KEY (version)
);

CREATE TABLE search_index_change (
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    change_seq INTEGER NOT NULL,
    changed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CONSTRAINT search_index_change_entity_type_check CHECK (entity_type IN ('issue', 'comment', 'project')),
    CONSTRAINT search_index_change_pkey PRIMARY KEY (entity_type, entity_id)
);

CREATE TABLE search_index_prune_mark (
    singleton INTEGER NOT NULL,
    pruned_through_seq INTEGER NOT NULL,
    CONSTRAINT search_index_prune_mark_singleton_check CHECK (singleton = 1),
    CONSTRAINT search_index_prune_mark_pkey PRIMARY KEY (singleton)
);

CREATE TABLE seat_capacity_outbox (
    workspace_id TEXT NOT NULL,
    operation_token TEXT NOT NULL,
    "action" TEXT NOT NULL,
    subject_id TEXT,
    member_id TEXT,
    invitation_id TEXT,
    share_link_id TEXT,
    user_id TEXT,
    expires_at TEXT,
    delivered_at TEXT,
    attempt_count INTEGER DEFAULT 0 NOT NULL,
    next_attempt_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    lease_token TEXT,
    last_error TEXT,
    dead_lettered_at TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT seat_capacity_outbox_action_check CHECK (("action" IN ('reserve_invitation', 'consume_invitation', 'claim_share_join', 'confirm', 'release', 'release_member'))),
CONSTRAINT seat_capacity_outbox_attempt_count_check CHECK ((attempt_count >= 0)),
CONSTRAINT seat_capacity_outbox_check CHECK ((("action" <> 'reserve_invitation') OR ((invitation_id IS NOT NULL) AND (expires_at IS NOT NULL)))),
CONSTRAINT seat_capacity_outbox_check1 CHECK ((("action" <> 'consume_invitation') OR ((invitation_id IS NOT NULL) AND (user_id IS NOT NULL)))),
CONSTRAINT seat_capacity_outbox_check2 CHECK ((("action" <> 'claim_share_join') OR ((share_link_id IS NOT NULL) AND (user_id IS NOT NULL)))),
CONSTRAINT seat_capacity_outbox_check3 CHECK ((("action" <> 'confirm') OR (member_id IS NOT NULL))),
CONSTRAINT seat_capacity_outbox_check4 CHECK ((("action" <> 'release_member') OR (member_id IS NOT NULL))),
    CONSTRAINT seat_capacity_outbox_pkey PRIMARY KEY (operation_token)
);

CREATE TABLE skill (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT DEFAULT '' NOT NULL,
    content TEXT DEFAULT '' NOT NULL,
    config TEXT DEFAULT '{}' NOT NULL,
    created_by TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    plugin_installation_id TEXT,
    CONSTRAINT skill_pkey PRIMARY KEY (id),
    CONSTRAINT skill_workspace_id_name_key UNIQUE (workspace_id, name)
);

CREATE TABLE skill_file (
    id TEXT NOT NULL,
    skill_id TEXT NOT NULL,
    path TEXT NOT NULL,
    content TEXT NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    CONSTRAINT skill_file_pkey PRIMARY KEY (id),
    CONSTRAINT skill_file_skill_id_path_key UNIQUE (skill_id, path)
);

CREATE TABLE skill_to_label (
    skill_id TEXT NOT NULL,
    label_id TEXT NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    CONSTRAINT skill_to_label_pkey PRIMARY KEY (skill_id, label_id)
);

CREATE TABLE squad (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT DEFAULT '' NOT NULL,
    leader_id TEXT NOT NULL,
    creator_id TEXT NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    archived_at TEXT,
    archived_by TEXT,
    avatar_url TEXT,
    instructions TEXT DEFAULT '' NOT NULL,
    CONSTRAINT squad_pkey PRIMARY KEY (id)
);

CREATE TABLE squad_member (
    id TEXT NOT NULL,
    squad_id TEXT NOT NULL,
    member_type TEXT NOT NULL,
    member_id TEXT NOT NULL,
    role TEXT DEFAULT '' NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT squad_member_member_type_check CHECK ((member_type IN ('agent', 'member'))),
    CONSTRAINT squad_member_pkey PRIMARY KEY (id),
    CONSTRAINT squad_member_squad_id_member_type_member_id_key UNIQUE (squad_id, member_type, member_id)
);

CREATE TABLE sys_cron_executions (
    id TEXT NOT NULL,
    job_name TEXT NOT NULL,
    scope_kind TEXT DEFAULT 'global' NOT NULL,
    scope_id TEXT DEFAULT 'global' NOT NULL,
    plan_time TEXT NOT NULL,
    status TEXT NOT NULL,
    attempt INTEGER DEFAULT 1 NOT NULL,
    max_attempts INTEGER DEFAULT 3 NOT NULL,
    next_retry_at TEXT,
    runner_id TEXT,
    lease_token TEXT NOT NULL,
    heartbeat_at TEXT,
    stale_after TEXT,
    started_at TEXT,
    finished_at TEXT,
    duration_ms INTEGER,
    rows_affected INTEGER,
    result TEXT DEFAULT '{}' NOT NULL,
    error_code TEXT,
    error_msg TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT chk_sys_cron_attempt CHECK (((attempt >= 1) AND (max_attempts >= attempt))),
CONSTRAINT chk_sys_cron_duration CHECK (((duration_ms IS NULL) OR (duration_ms >= 0))),
CONSTRAINT chk_sys_cron_status CHECK ((status IN ('RUNNING', 'SUCCESS', 'FAILED'))),
    CONSTRAINT sys_cron_executions_pkey PRIMARY KEY (id),
    CONSTRAINT uq_sys_cron_execution UNIQUE (job_name, scope_kind, scope_id, plan_time)
);

CREATE TABLE task_message (
    id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    seq INTEGER NOT NULL,
    type TEXT NOT NULL,
    tool TEXT,
    content TEXT,
    input TEXT,
    output TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    output_truncated INTEGER,
    call_id TEXT,
    CONSTRAINT task_message_pkey PRIMARY KEY (id)
);

CREATE TABLE task_supplement (
    task_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    issue_id TEXT NOT NULL,
    comment_id TEXT NOT NULL,
    author_id TEXT NOT NULL,
    client_request_id TEXT NOT NULL,
    status TEXT NOT NULL,
    failure_reason TEXT,
    attempt_count INTEGER DEFAULT 0 NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    delivered_at TEXT,
CONSTRAINT task_supplement_status_check CHECK ((status IN ('pending', 'delivering', 'delivered', 'failed'))),
    CONSTRAINT task_supplement_pkey PRIMARY KEY (comment_id, task_id)
);

CREATE TABLE task_supplement_capability (
    task_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    issue_id TEXT NOT NULL,
    capability TEXT NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL
);

CREATE TABLE task_token (
    id TEXT NOT NULL,
    token_hash TEXT NOT NULL,
    task_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    CONSTRAINT task_token_pkey PRIMARY KEY (id)
);

CREATE TABLE task_usage (
    id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    provider TEXT DEFAULT '' NOT NULL,
    model TEXT NOT NULL,
    input_tokens INTEGER DEFAULT 0 NOT NULL,
    output_tokens INTEGER DEFAULT 0 NOT NULL,
    cache_read_tokens INTEGER DEFAULT 0 NOT NULL,
    cache_write_tokens INTEGER DEFAULT 0 NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    cost_usd_ticks INTEGER,
    CONSTRAINT task_usage_pkey PRIMARY KEY (id),
    CONSTRAINT task_usage_task_id_provider_model_key UNIQUE (task_id, provider, model)
);

CREATE TABLE task_usage_hourly (
    bucket_hour TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    runtime_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    project_id TEXT,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    input_tokens INTEGER DEFAULT 0 NOT NULL,
    output_tokens INTEGER DEFAULT 0 NOT NULL,
    cache_read_tokens INTEGER DEFAULT 0 NOT NULL,
    cache_write_tokens INTEGER DEFAULT 0 NOT NULL,
    task_count INTEGER DEFAULT 0 NOT NULL,
    event_count INTEGER DEFAULT 0 NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    cost_usd_ticks INTEGER DEFAULT 0 NOT NULL,
    uncosted_input_tokens INTEGER,
    uncosted_output_tokens INTEGER,
    uncosted_cache_read_tokens INTEGER,
    uncosted_cache_write_tokens INTEGER
);

CREATE TABLE task_usage_hourly_dirty (
    bucket_hour TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    runtime_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    project_id TEXT,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    enqueued_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL
);

CREATE TABLE task_usage_hourly_rollup_state (
    id INTEGER DEFAULT 1 NOT NULL,
    watermark_at TEXT DEFAULT '1970-01-01 00:00:00+00' NOT NULL,
    last_run_started_at TEXT,
    last_run_finished_at TEXT,
    last_run_rows INTEGER DEFAULT 0 NOT NULL,
    last_error TEXT,
CONSTRAINT task_usage_hourly_rollup_state_id_check CHECK ((id = 1)),
    CONSTRAINT task_usage_hourly_rollup_state_pkey PRIMARY KEY (id)
);

CREATE TABLE "user" (
    id TEXT NOT NULL,
    name TEXT NOT NULL,
    email TEXT NOT NULL,
    avatar_url TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    onboarded_at TEXT,
    onboarding_questionnaire TEXT DEFAULT '{}' NOT NULL,
    cloud_waitlist_email TEXT,
    cloud_waitlist_reason TEXT,
    starter_content_state TEXT,
    language TEXT DEFAULT NULL,
    profile_description TEXT DEFAULT '' NOT NULL,
    timezone TEXT,
    CONSTRAINT user_pkey PRIMARY KEY (id),
    CONSTRAINT user_email_key UNIQUE (email)
);

CREATE TABLE user_composio_connection (
    id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    toolkit_slug TEXT NOT NULL,
    auth_config_id TEXT NOT NULL,
    connected_account_id TEXT NOT NULL,
    composio_user_id TEXT NOT NULL,
    status TEXT DEFAULT 'active' NOT NULL,
    connected_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    last_used_at TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    CONSTRAINT user_composio_connection_pkey PRIMARY KEY (id),
    CONSTRAINT user_composio_connection_user_id_connected_account_id_key UNIQUE (user_id, connected_account_id)
);

CREATE TABLE vcs_commit_status (
    connection_id TEXT NOT NULL,
    sha TEXT NOT NULL,
    context TEXT NOT NULL,
    state TEXT NOT NULL,
    target_url TEXT,
    description TEXT,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    CONSTRAINT vcs_commit_status_pkey PRIMARY KEY (connection_id, sha, context)
);

CREATE TABLE vcs_connection (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    provider TEXT DEFAULT 'forgejo' NOT NULL,
    instance_url TEXT NOT NULL,
    account_login TEXT NOT NULL,
    access_token_encrypted TEXT NOT NULL,
    webhook_secret_encrypted TEXT NOT NULL,
    connected_by_id TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT vcs_connection_provider_check CHECK ((provider IN ('forgejo', 'gitea', 'gitlab'))),
    CONSTRAINT vcs_connection_pkey PRIMARY KEY (id),
    CONSTRAINT vcs_connection_workspace_id_instance_url_key UNIQUE (workspace_id, instance_url)
);

CREATE TABLE vcs_pull_request (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    connection_id TEXT NOT NULL,
    provider TEXT DEFAULT 'forgejo' NOT NULL,
    repo_owner TEXT NOT NULL,
    repo_name TEXT NOT NULL,
    pr_number INTEGER NOT NULL,
    title TEXT NOT NULL,
    state TEXT NOT NULL,
    html_url TEXT NOT NULL,
    branch TEXT,
    head_sha TEXT DEFAULT '' NOT NULL,
    author_login TEXT,
    author_avatar_url TEXT,
    merged_at TEXT,
    closed_at TEXT,
    pr_created_at TEXT NOT NULL,
    pr_updated_at TEXT NOT NULL,
    additions INTEGER DEFAULT 0 NOT NULL,
    deletions INTEGER DEFAULT 0 NOT NULL,
    changed_files INTEGER DEFAULT 0 NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT vcs_pull_request_provider_check CHECK ((provider IN ('forgejo', 'gitea', 'gitlab'))),
CONSTRAINT vcs_pull_request_state_check CHECK ((state IN ('open', 'closed', 'merged', 'draft'))),
    CONSTRAINT vcs_pull_request_pkey PRIMARY KEY (id),
    CONSTRAINT vcs_pull_request_connection_id_repo_owner_repo_name_pr_numb_key UNIQUE (connection_id, repo_owner, repo_name, pr_number)
);

CREATE TABLE verification_code (
    id TEXT NOT NULL,
    email TEXT NOT NULL,
    code TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    used INTEGER DEFAULT 0 NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    attempts INTEGER DEFAULT 0 NOT NULL,
    CONSTRAINT verification_code_pkey PRIMARY KEY (id)
);

CREATE TABLE webhook_delivery (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    autopilot_id TEXT NOT NULL,
    trigger_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    event TEXT DEFAULT 'webhook.received' NOT NULL,
    dedupe_key TEXT,
    dedupe_source TEXT,
    signature_status TEXT DEFAULT 'not_required' NOT NULL,
    status TEXT DEFAULT 'queued' NOT NULL,
    attempt_count INTEGER DEFAULT 1 NOT NULL,
    selected_headers TEXT DEFAULT '{}' NOT NULL,
    content_type TEXT,
    raw_body BLOB,
    response_status INTEGER,
    response_body TEXT,
    autopilot_run_id TEXT,
    replayed_from_delivery_id TEXT,
    error TEXT,
    received_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    last_attempt_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    available_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    lease_token TEXT,
    lease_expires_at TEXT,
    dispatch_attempts INTEGER DEFAULT 0 NOT NULL,
    reason_code TEXT,
    replay_idempotency_key TEXT,
CONSTRAINT webhook_delivery_provider_check CHECK ((provider IN ('generic', 'github'))),
CONSTRAINT webhook_delivery_signature_status_check CHECK ((signature_status IN ('not_required', 'valid', 'invalid', 'missing'))),
CONSTRAINT webhook_delivery_status_check CHECK ((status IN ('queued', 'dispatched', 'rejected', 'ignored', 'failed'))),
    CONSTRAINT webhook_delivery_pkey PRIMARY KEY (id)
);

CREATE TABLE workspace (
    id TEXT NOT NULL,
    name TEXT NOT NULL,
    slug TEXT NOT NULL,
    description TEXT,
    settings TEXT DEFAULT '{}' NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    context TEXT,
    repos TEXT DEFAULT '[]' NOT NULL,
    issue_prefix TEXT DEFAULT '' NOT NULL,
    issue_counter INTEGER DEFAULT 0 NOT NULL,
    avatar_url TEXT,
    attribution_fail_closed INTEGER DEFAULT 0 NOT NULL,
    CONSTRAINT workspace_pkey PRIMARY KEY (id),
    CONSTRAINT workspace_slug_key UNIQUE (slug)
);

CREATE TABLE workspace_invitation (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    inviter_id TEXT NOT NULL,
    invitee_email TEXT NOT NULL,
    invitee_user_id TEXT,
    role TEXT NOT NULL,
    status TEXT DEFAULT 'pending' NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    expires_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '+7 days')) NOT NULL,
CONSTRAINT workspace_invitation_role_check CHECK ((role IN ('admin', 'member'))),
CONSTRAINT workspace_invitation_status_check CHECK ((status IN ('pending', 'accepted', 'declined', 'expired'))),
    CONSTRAINT workspace_invitation_pkey PRIMARY KEY (id)
);

CREATE TABLE workspace_mcp_server (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    name TEXT NOT NULL,
    config TEXT NOT NULL,
    created_by TEXT,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
    CONSTRAINT workspace_mcp_server_pkey PRIMARY KEY (id)
);

CREATE TABLE workspace_share_link (
    id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    code TEXT NOT NULL,
    created_by TEXT NOT NULL,
    role TEXT DEFAULT 'member' NOT NULL,
    expires_at TEXT,
    max_uses INTEGER,
    use_count INTEGER DEFAULT 0 NOT NULL,
    is_active INTEGER DEFAULT 1 NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
CONSTRAINT workspace_share_link_role_check CHECK ((role IN ('admin', 'member'))),
    CONSTRAINT workspace_share_link_pkey PRIMARY KEY (id)
);

CREATE TABLE search_change_counter (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    n INTEGER NOT NULL
);

INSERT INTO search_change_counter (singleton, n) VALUES (1, 0);

CREATE INDEX agent_invocation_target_agent_id_idx ON agent_invocation_target (agent_id);
CREATE INDEX agent_invocation_target_target_idx ON agent_invocation_target (target_type, target_id);
CREATE UNIQUE INDEX agent_runtime_workspace_daemon_profile_key ON agent_runtime (workspace_id, daemon_id, profile_id) WHERE (profile_id IS NOT NULL);
CREATE UNIQUE INDEX agent_runtime_workspace_daemon_provider_key ON agent_runtime (workspace_id, daemon_id, provider) WHERE (profile_id IS NULL);
CREATE UNIQUE INDEX agent_system_identity_unique ON agent (workspace_id, owner_id, runtime_id, system_key) WHERE (system_key IS NOT NULL);
CREATE INDEX agent_task_queue_originator_user_id_idx ON agent_task_queue (originator_user_id) WHERE (originator_user_id IS NOT NULL);
CREATE INDEX agent_task_queue_squad_id_idx ON agent_task_queue (squad_id) WHERE (squad_id IS NOT NULL);
CREATE INDEX agent_task_wakeup_active_idx ON agent_task_queue (((json_extract(context, '$.wakeup_id'))), ((status IN ('running', 'waiting_local_directory', 'dispatched'))) DESC, created_at DESC, id DESC) WHERE (((json_extract(context, '$.wakeup_id')) IS NOT NULL) AND (status IN ('queued', 'deferred', 'dispatched', 'running', 'waiting_local_directory')));
CREATE INDEX agent_task_wakeup_lookup_idx ON agent_task_queue (((json_extract(context, '$.wakeup_id'))), created_at DESC) WHERE ((json_extract(context, '$.wakeup_id')) IS NOT NULL);
CREATE INDEX agent_task_wakeup_terminal_idx ON agent_task_queue (((json_extract(context, '$.wakeup_id'))), created_at DESC, id DESC) WHERE (((json_extract(context, '$.wakeup_id')) IS NOT NULL) AND (status NOT IN ('queued', 'deferred', 'dispatched', 'running', 'waiting_local_directory')));
CREATE INDEX agent_to_label_label_idx ON agent_to_label (label_id);
CREATE UNIQUE INDEX channel_chat_context_generation_session_revision_idx ON channel_chat_context_generation (chat_session_id, revision);
CREATE INDEX channel_typing_reaction_abandoned_idx ON channel_typing_reaction (abandoned_at) WHERE (abandoned_at IS NOT NULL);
CREATE INDEX channel_typing_reaction_expiry_idx ON channel_typing_reaction (created_at, id) WHERE ((cleaned_at IS NULL) AND (abandoned_at IS NULL));
CREATE INDEX channel_typing_reaction_gc_idx ON channel_typing_reaction (cleaned_at) WHERE (cleaned_at IS NOT NULL);
CREATE UNIQUE INDEX channel_typing_reaction_id_idx ON channel_typing_reaction (id);
CREATE UNIQUE INDEX channel_typing_reaction_quota_idx ON channel_typing_reaction (workspace_id, quota_slot) WHERE ((cleaned_at IS NULL) AND (abandoned_at IS NULL));
CREATE INDEX channel_typing_reaction_retry_idx ON channel_typing_reaction (retry_after, chat_session_id) WHERE (cleaned_at IS NULL);
CREATE INDEX client_usage_daily_activity_client_user_idx ON client_usage_daily (activity_date, client_type, user_id);
CREATE INDEX client_usage_daily_workspace_idx ON client_usage_daily (workspace_id) WHERE (workspace_id IS NOT NULL);
CREATE INDEX comment_issue_resolved_at_idx ON comment (issue_id, resolved_at);
CREATE UNIQUE INDEX github_pull_request_check_run_pr_ordinal_idx ON github_pull_request_check_run (pr_id, ordinal);
CREATE INDEX idx_activity_log_issue_keyset ON activity_log (issue_id, created_at DESC, id DESC);
CREATE INDEX idx_activity_log_member_assignee_frequency ON activity_log (workspace_id, actor_id, ((json_extract(details, '$.to_type'))), ((json_extract(details, '$.to_id')))) WHERE ((actor_type = 'member') AND ("action" = 'assignee_changed') AND ((json_extract(details, '$.to_type')) IS NOT NULL) AND ((json_extract(details, '$.to_id')) IS NOT NULL));
CREATE INDEX idx_activity_log_squad_no_action_task ON activity_log (issue_id, actor_id, ((json_extract(details, '$.task_id')))) WHERE ((actor_type = 'agent') AND ("action" = 'squad_leader_evaluated') AND ((json_extract(details, '$.outcome')) = 'no_action'));
CREATE INDEX idx_agent_mcp_server_server ON agent_mcp_server (server_id);
CREATE INDEX idx_agent_runtime_id ON agent (runtime_id);
CREATE INDEX idx_agent_runtime_offline_last_seen ON agent_runtime (last_seen_at, id) WHERE (status = 'offline');
CREATE INDEX idx_agent_runtime_online_last_seen ON agent_runtime (last_seen_at) WHERE (status = 'online');
CREATE INDEX idx_agent_runtime_status ON agent_runtime (workspace_id, status);
CREATE INDEX idx_agent_runtime_workspace ON agent_runtime (workspace_id);
CREATE INDEX idx_agent_runtime_workspace_id_keyset ON agent_runtime (workspace_id, id);
CREATE INDEX idx_agent_skill_agent ON agent_skill (agent_id);
CREATE INDEX idx_agent_skill_skill ON agent_skill (skill_id);
CREATE INDEX idx_agent_task_queue_agent ON agent_task_queue (agent_id, status);
CREATE INDEX idx_agent_task_queue_agent_id_keyset ON agent_task_queue (agent_id, id);
CREATE INDEX idx_agent_task_queue_agent_terminal_latest ON agent_task_queue (agent_id, completed_at DESC, created_at DESC, id DESC) WHERE (status IN ('completed', 'failed'));
CREATE INDEX idx_agent_task_queue_autopilot_run_created_at ON agent_task_queue (autopilot_run_id, created_at) WHERE (autopilot_run_id IS NOT NULL);
CREATE INDEX idx_agent_task_queue_chat_pending_v3 ON agent_task_queue (chat_session_id, created_at DESC) WHERE ((chat_session_id IS NOT NULL) AND (status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')));
CREATE INDEX idx_agent_task_queue_chat_retired_session ON agent_task_queue (chat_session_id, retired_session_id) WHERE ((chat_session_id IS NOT NULL) AND (retired_session_id IS NOT NULL));
CREATE INDEX idx_agent_task_queue_chat_session ON agent_task_queue (chat_session_id, created_at DESC) WHERE (chat_session_id IS NOT NULL);
CREATE INDEX idx_agent_task_queue_chat_terminal_resume ON agent_task_queue (chat_session_id, session_id, completed_at DESC) WHERE ((chat_session_id IS NOT NULL) AND (status IN ('completed', 'failed', 'cancelled')));
CREATE INDEX idx_agent_task_queue_claim_candidates ON agent_task_queue (runtime_id, priority DESC, created_at) WHERE (status = 'queued');
CREATE INDEX idx_agent_task_queue_deferred_fire ON agent_task_queue (runtime_id, fire_at) WHERE (status = 'deferred');
CREATE INDEX idx_agent_task_queue_delegated_failure_evidence ON agent_task_queue (trigger_evidence_ref_id, created_at DESC, id DESC) WHERE (trigger_evidence_kind = 'delegated_failure');
CREATE INDEX idx_agent_task_queue_dispatched_reclaim_v2 ON agent_task_queue (runtime_id, dispatched_at, priority DESC) WHERE ((status = 'dispatched') AND (started_at IS NULL));
CREATE INDEX idx_agent_task_queue_escalation_for ON agent_task_queue (escalation_for_task_id) WHERE (escalation_for_task_id IS NOT NULL);
CREATE INDEX idx_agent_task_queue_history_page ON agent_task_queue (agent_id, created_at DESC, id DESC) WHERE (NOT ((escalation_for_task_id IS NOT NULL) AND (started_at IS NULL) AND (status IN ('deferred', 'cancelled'))));
CREATE INDEX idx_agent_task_queue_issue_id ON agent_task_queue (issue_id);
CREATE INDEX idx_agent_task_queue_issue_id_keyset ON agent_task_queue (issue_id, id);
CREATE INDEX idx_agent_task_queue_parent ON agent_task_queue (parent_task_id);
CREATE INDEX idx_agent_task_queue_pending ON agent_task_queue (agent_id, priority DESC, created_at) WHERE (status IN ('queued', 'dispatched'));
CREATE INDEX idx_agent_task_queue_queued_created_at ON agent_task_queue (created_at) WHERE (status = 'queued');
CREATE INDEX idx_agent_task_queue_running_started_at ON agent_task_queue (started_at) WHERE (status = 'running');
CREATE INDEX idx_agent_task_queue_runtime_id ON agent_task_queue (runtime_id, id);
CREATE INDEX idx_agent_task_queue_runtime_pending ON agent_task_queue (runtime_id, priority DESC, created_at) WHERE (status IN ('queued', 'dispatched'));
CREATE INDEX idx_agent_task_queue_telemetry_started ON agent_task_queue (started_at) WHERE (started_at IS NOT NULL);
CREATE INDEX idx_agent_task_queue_terminal_completed_at_v2 ON agent_task_queue (completed_at) WHERE (status IN ('completed', 'failed', 'cancelled'));
CREATE INDEX idx_agent_task_queue_trigger_comment_id ON agent_task_queue (trigger_comment_id) WHERE (trigger_comment_id IS NOT NULL);
CREATE INDEX idx_agent_workspace ON agent (workspace_id);
CREATE INDEX idx_agent_workspace_id_keyset ON agent (workspace_id, id);
CREATE INDEX idx_attachment_chat_message ON attachment (chat_message_id) WHERE (chat_message_id IS NOT NULL);
CREATE INDEX idx_attachment_chat_session ON attachment (chat_session_id) WHERE (chat_session_id IS NOT NULL);
CREATE INDEX idx_attachment_comment ON attachment (comment_id) WHERE (comment_id IS NOT NULL);
CREATE INDEX idx_attachment_issue ON attachment (issue_id) WHERE (issue_id IS NOT NULL);
CREATE INDEX idx_attachment_source_context ON attachment (source_context_id) WHERE (source_context_id IS NOT NULL);
CREATE INDEX idx_attachment_task ON attachment (task_id) WHERE (task_id IS NOT NULL);
CREATE INDEX idx_attachment_workspace ON attachment (workspace_id);
CREATE INDEX idx_autopilot_assignee ON autopilot (assignee_id);
CREATE INDEX idx_autopilot_assignee_type_id ON autopilot (assignee_type, assignee_id);
CREATE INDEX idx_autopilot_collaborator_user ON autopilot_collaborator (user_type, user_id);
CREATE INDEX idx_autopilot_project ON autopilot (project_id);
CREATE INDEX idx_autopilot_quota_reservation_state ON autopilot_quota_reservation (state, created_at) WHERE (state = 'reserved');
CREATE INDEX idx_autopilot_rule_version_active ON autopilot_rule_version (workspace_id, autopilot_id, created_at DESC);
CREATE INDEX idx_autopilot_run_autopilot ON autopilot_run (autopilot_id, created_at DESC);
CREATE INDEX idx_autopilot_run_issue ON autopilot_run (issue_id) WHERE (issue_id IS NOT NULL);
CREATE INDEX idx_autopilot_run_squad_id ON autopilot_run (squad_id) WHERE (squad_id IS NOT NULL);
CREATE INDEX idx_autopilot_run_status ON autopilot_run (autopilot_id, status) WHERE (status IN ('issue_created', 'running'));
CREATE INDEX idx_autopilot_run_task_id ON autopilot_run (task_id);
CREATE INDEX idx_autopilot_subscriber_user ON autopilot_subscriber (user_type, user_id);
CREATE INDEX idx_autopilot_trigger_autopilot ON autopilot_trigger (autopilot_id);
CREATE INDEX idx_autopilot_trigger_next_run ON autopilot_trigger (next_run_at) WHERE ((enabled = 1) AND (kind = 'schedule'));
CREATE UNIQUE INDEX idx_autopilot_trigger_webhook_token ON autopilot_trigger (webhook_token) WHERE ((kind = 'webhook') AND (webhook_token IS NOT NULL));
CREATE INDEX idx_autopilot_workspace ON autopilot (workspace_id);
CREATE INDEX idx_channel_binding_token_installation ON channel_binding_token (installation_id, expires_at);
CREATE UNIQUE INDEX idx_channel_chat_session_binding_active_route ON channel_chat_session_binding (installation_id, channel_chat_id) WHERE (retired_at IS NULL);
CREATE INDEX idx_channel_inbound_audit_installation ON channel_inbound_audit (installation_id, received_at DESC);
CREATE INDEX idx_channel_inbound_audit_reason ON channel_inbound_audit (drop_reason, received_at DESC);
CREATE INDEX idx_channel_inbound_dedup_received ON channel_inbound_message_dedup (received_at);
CREATE INDEX idx_channel_installation_agent ON channel_installation (agent_id);
CREATE INDEX idx_channel_installation_lease ON channel_installation (ws_lease_expires_at) WHERE (status = 'active');
CREATE UNIQUE INDEX idx_channel_installation_type_appid ON channel_installation (channel_type, ((json_extract(config, '$.app_id'))));
CREATE INDEX idx_channel_installation_workspace ON channel_installation (workspace_id);
CREATE INDEX idx_channel_media_pending_object_claim ON channel_media_pending_object (state, next_attempt_at);
CREATE INDEX idx_channel_media_pending_object_due ON channel_media_pending_object (next_attempt_at);
CREATE INDEX idx_channel_outbound_card_session ON channel_outbound_card_message (chat_session_id, created_at DESC);
CREATE UNIQUE INDEX idx_channel_outbound_card_task ON channel_outbound_card_message (task_id) WHERE (task_id IS NOT NULL);
CREATE INDEX idx_channel_outbound_message_binding_route ON channel_outbound_message (binding_id, route_revision);
CREATE UNIQUE INDEX idx_channel_outbound_message_id ON channel_outbound_message (installation_id, channel_message_id);
CREATE INDEX idx_channel_reply_delivery_binding ON channel_reply_delivery (binding_id);
CREATE INDEX idx_channel_reply_delivery_installation ON channel_reply_delivery (installation_id);
CREATE UNIQUE INDEX idx_channel_reply_delivery_turn ON channel_reply_delivery (turn_id);
CREATE INDEX idx_channel_task_delivery_binding ON channel_task_delivery (binding_id);
CREATE INDEX idx_channel_task_delivery_installation ON channel_task_delivery (installation_id);
CREATE INDEX idx_channel_user_binding_user ON channel_user_binding (multica_user_id, workspace_id);
CREATE INDEX idx_channel_user_binding_workspace_user ON channel_user_binding (workspace_id, channel_user_id);
CREATE INDEX idx_chat_draft_restore_session ON chat_draft_restore (chat_session_id);
CREATE INDEX idx_chat_draft_restore_task_id ON chat_draft_restore (task_id);
CREATE INDEX idx_chat_message_assistant_task ON chat_message (task_id, created_at DESC) WHERE (role = 'assistant');
CREATE INDEX idx_chat_message_input_owner ON chat_message (task_id, created_at) WHERE (role = 'user');
CREATE INDEX idx_chat_message_session ON chat_message (chat_session_id, created_at);
CREATE INDEX idx_chat_pinned_agent_user_ws ON chat_pinned_agent (workspace_id, user_id, "position");
CREATE INDEX idx_chat_session_agent_id ON chat_session (agent_id);
CREATE INDEX idx_chat_session_creator ON chat_session (creator_id, workspace_id);
CREATE INDEX idx_chat_session_pinned ON chat_session (creator_id, workspace_id, pinned_at DESC) WHERE (pinned_at IS NOT NULL);
CREATE INDEX idx_chat_session_project ON chat_session (project_id) WHERE (project_id IS NOT NULL);
CREATE INDEX idx_chat_session_runtime_id ON chat_session (runtime_id) WHERE (runtime_id IS NOT NULL);
CREATE INDEX idx_chat_session_workspace ON chat_session (workspace_id);
CREATE INDEX idx_comment_delegated_failure_unsettled ON comment (created_at, id) WHERE ((author_type = 'system') AND (type = 'progress_update') AND (source_task_id IS NOT NULL) AND (recovery_settled_at IS NULL));
CREATE INDEX idx_comment_issue_keyset ON comment (issue_id, created_at DESC, id DESC);
CREATE INDEX idx_comment_parent_id ON comment (parent_id) WHERE (parent_id IS NOT NULL);
CREATE INDEX idx_comment_reaction_comment_id ON comment_reaction (comment_id);
CREATE INDEX idx_comment_workspace ON comment (workspace_id);
CREATE INDEX idx_comment_workspace_issue_parent ON comment (workspace_id, issue_id, parent_id) WHERE (parent_id IS NOT NULL);
CREATE INDEX idx_contact_sales_inquiry_created ON contact_sales_inquiry (created_at DESC);
CREATE INDEX idx_contact_sales_inquiry_email_created ON contact_sales_inquiry (business_email, created_at DESC);
CREATE UNIQUE INDEX idx_daemon_token_hash ON daemon_token (token_hash);
CREATE INDEX idx_daemon_token_workspace_daemon ON daemon_token (workspace_id, daemon_id);
CREATE UNIQUE INDEX idx_dingtalk_bot_identity_installation ON dingtalk_bot_identity (installation_id);
CREATE INDEX idx_dingtalk_bot_identity_workspace ON dingtalk_bot_identity (workspace_id);
CREATE UNIQUE INDEX idx_dingtalk_group_presence_installation_conversation ON dingtalk_group_presence (installation_id, conversation_id);
CREATE INDEX idx_dingtalk_group_presence_workspace_activity ON dingtalk_group_presence (workspace_id, last_active_at DESC, installation_id, conversation_id);
CREATE UNIQUE INDEX idx_dingtalk_group_route_id_unique ON dingtalk_group_route (id);
CREATE UNIQUE INDEX idx_dingtalk_group_route_installation_conversation ON dingtalk_group_route (installation_id, conversation_id);
CREATE INDEX idx_dingtalk_group_route_workspace ON dingtalk_group_route (workspace_id);
CREATE INDEX idx_feedback_user_created ON feedback (user_id, created_at DESC);
CREATE INDEX idx_github_installation_installation_id ON github_installation (installation_id);
CREATE INDEX idx_github_installation_workspace ON github_installation (workspace_id);
CREATE INDEX idx_github_pending_check_suite_received_at ON github_pending_check_suite (received_at);
CREATE INDEX idx_github_pr_check_suite_aggregate ON github_pull_request_check_suite (pr_id, head_sha, app_id, updated_at DESC);
CREATE INDEX idx_github_pull_request_head_sha ON github_pull_request (head_sha);
CREATE INDEX idx_github_pull_request_pr_owner_repo ON github_pull_request (pr_number, repo_owner, repo_name);
CREATE INDEX idx_github_pull_request_workspace ON github_pull_request (workspace_id);
CREATE INDEX idx_inbox_active_by_issue ON inbox_item (workspace_id, recipient_type, recipient_id, issue_id) WHERE (archived = 0);
CREATE INDEX idx_inbox_item_issue_id ON inbox_item (issue_id);
CREATE INDEX idx_inbox_recipient ON inbox_item (recipient_type, recipient_id, read);
CREATE INDEX idx_inbox_recipient_archived_created ON inbox_item (workspace_id, recipient_type, recipient_id, archived, created_at DESC);
CREATE INDEX idx_invitation_invitee_email ON workspace_invitation (invitee_email) WHERE (status = 'pending');
CREATE INDEX idx_invitation_invitee_user ON workspace_invitation (invitee_user_id) WHERE (status = 'pending');
CREATE UNIQUE INDEX idx_invitation_unique_pending ON workspace_invitation (workspace_id, invitee_email) WHERE (status = 'pending');
CREATE INDEX idx_issue_assignee ON issue (assignee_type, assignee_id);
CREATE INDEX idx_issue_dependency_depends_on_issue_id ON issue_dependency (depends_on_issue_id);
CREATE INDEX idx_issue_dependency_issue_id ON issue_dependency (issue_id);
CREATE INDEX idx_issue_duplicate_of ON issue (duplicate_of_issue_id) WHERE (duplicate_of_issue_id IS NOT NULL);
CREATE INDEX idx_issue_first_executed_at ON issue (workspace_id, first_executed_at) WHERE (first_executed_at IS NOT NULL);
CREATE INDEX idx_issue_origin ON issue (origin_type, origin_id) WHERE (origin_type IS NOT NULL);
CREATE INDEX idx_issue_parent ON issue (parent_issue_id);
CREATE INDEX idx_issue_pr_automation_workspace ON issue_pr_automation (workspace_id);
CREATE INDEX idx_issue_project ON issue (project_id);
CREATE INDEX idx_issue_project_status ON issue (project_id, workspace_id, status);
CREATE INDEX idx_issue_property_workspace ON issue_property (workspace_id);
CREATE UNIQUE INDEX idx_issue_property_ws_name ON issue_property (workspace_id, lower(name));
CREATE INDEX idx_issue_pull_request_exclusion_workspace ON issue_pull_request_exclusion (workspace_id);
CREATE INDEX idx_issue_pull_request_pr ON issue_pull_request (pull_request_id);
CREATE INDEX idx_issue_reaction_issue_id ON issue_reaction (issue_id);
CREATE UNIQUE INDEX idx_issue_source_context_id ON issue_source_context (id);
CREATE UNIQUE INDEX idx_issue_source_context_issue ON issue_source_context (issue_id) WHERE ((state = 'attached') AND (issue_id IS NOT NULL));
CREATE INDEX idx_issue_source_context_object_intent_context ON issue_source_context_object_intent (source_context_id);
CREATE INDEX idx_issue_source_context_object_intent_due ON issue_source_context_object_intent (next_attempt_at) WHERE (state IN ('pending', 'deleting'));
CREATE UNIQUE INDEX idx_issue_source_context_object_intent_key ON issue_source_context_object_intent (storage_key);
CREATE UNIQUE INDEX idx_issue_source_context_origin_task ON issue_source_context (origin_task_id) WHERE ((state = 'pending') AND (origin_task_id IS NOT NULL));
CREATE INDEX idx_issue_status ON issue (workspace_id, status);
CREATE UNIQUE INDEX idx_issue_status_workspace_key ON issue_status (workspace_id, "key");
CREATE UNIQUE INDEX idx_issue_status_workspace_name_active ON issue_status (workspace_id, lower(name)) WHERE (archived_at IS NULL);
CREATE INDEX idx_issue_subscriber_user ON issue_subscriber (user_type, user_id);
CREATE INDEX idx_issue_triage_state ON issue (workspace_id, created_at) WHERE (triage_state IS NOT NULL);
CREATE INDEX idx_issue_vcs_pull_request_pr ON issue_vcs_pull_request (pull_request_id);
CREATE INDEX idx_issue_view_owner ON issue_view (workspace_id, scope_type, scope_id, owner_id);
CREATE INDEX idx_issue_view_shared ON issue_view (workspace_id, scope_type, scope_id) WHERE (visibility = 'workspace');
CREATE UNIQUE INDEX idx_issue_workflow_workspace_default ON issue_workflow (workspace_id) WHERE is_default;
CREATE UNIQUE INDEX idx_issue_workflow_workspace_name ON issue_workflow (workspace_id, lower(name));
CREATE INDEX idx_issue_workspace ON issue (workspace_id);
CREATE INDEX idx_issue_workspace_assignee ON issue (workspace_id, assignee_type, assignee_id);
CREATE INDEX idx_issue_workspace_id_keyset ON issue (workspace_id, id);
CREATE INDEX idx_issue_workspace_parent ON issue (workspace_id, parent_issue_id);
CREATE INDEX idx_issue_workspace_position ON issue (workspace_id, "position", created_at DESC, id DESC);
CREATE INDEX idx_issue_workspace_status_position ON issue (workspace_id, status, "position");
CREATE INDEX idx_lark_binding_token_installation ON lark_binding_token (installation_id, expires_at);
CREATE INDEX idx_lark_inbound_audit_installation ON lark_inbound_audit (installation_id, received_at DESC);
CREATE INDEX idx_lark_inbound_audit_reason ON lark_inbound_audit (drop_reason, received_at DESC);
CREATE INDEX idx_lark_inbound_dedup_received ON lark_inbound_message_dedup (received_at);
CREATE INDEX idx_lark_installation_agent ON lark_installation (agent_id);
CREATE INDEX idx_lark_installation_lease ON lark_installation (ws_lease_expires_at) WHERE (status = 'active');
CREATE INDEX idx_lark_installation_workspace ON lark_installation (workspace_id);
CREATE INDEX idx_lark_outbound_card_session ON lark_outbound_card_message (chat_session_id, created_at DESC);
CREATE UNIQUE INDEX idx_lark_outbound_card_task ON lark_outbound_card_message (task_id) WHERE (task_id IS NOT NULL);
CREATE INDEX idx_lark_user_binding_user ON lark_user_binding (multica_user_id, workspace_id);
CREATE INDEX idx_lark_user_binding_workspace_open ON lark_user_binding (workspace_id, lark_open_id);
CREATE UNIQUE INDEX idx_maintenance_job_active ON maintenance_job (job_type, scope_key) WHERE (status IN ('ready', 'paused'));
CREATE UNIQUE INDEX idx_maintenance_job_id ON maintenance_job (id);
CREATE UNIQUE INDEX idx_maintenance_job_idempotency ON maintenance_job (idempotency_key);
CREATE INDEX idx_member_user_workspace ON member (user_id, workspace_id);
CREATE INDEX idx_member_workspace ON member (workspace_id);
CREATE UNIQUE INDEX idx_one_pending_task_per_issue_agent_thread ON agent_task_queue (issue_id, agent_id, COALESCE(comment_thread_id, '00000000-0000-0000-0000-000000000000')) WHERE ((status IN ('queued', 'dispatched')) OR ((status = 'deferred') AND ((json_extract(context, '$.channel_issue_media_pending')) = 'true')));
CREATE UNIQUE INDEX idx_pat_token_hash ON personal_access_token (token_hash);
CREATE INDEX idx_pat_user ON personal_access_token (user_id, revoked);
CREATE INDEX idx_pinned_item_user_ws ON pinned_item (workspace_id, user_id, "position");
CREATE INDEX idx_plugin_hook_schedule_enabled ON plugin_hook_schedule (id) WHERE enabled;
CREATE UNIQUE INDEX idx_plugin_hook_schedule_installation_key ON plugin_hook_schedule (installation_id, hook_key);
CREATE INDEX idx_plugin_installation_package_version ON plugin_installation (package_version_id);
CREATE UNIQUE INDEX idx_plugin_installation_workspace_key ON plugin_installation (workspace_id, plugin_key);
CREATE INDEX idx_plugin_invocation_created_at ON plugin_invocation (created_at);
CREATE INDEX idx_plugin_invocation_installation_created ON plugin_invocation (installation_id, created_at DESC);
CREATE UNIQUE INDEX idx_plugin_package_file_path ON plugin_package_file (version_id, path);
CREATE INDEX idx_plugin_package_version_package ON plugin_package_version (package_id, created_at DESC);
CREATE UNIQUE INDEX idx_plugin_package_version_unique ON plugin_package_version (package_id, version);
CREATE UNIQUE INDEX idx_plugin_package_workspace_key ON plugin_package (workspace_id, plugin_key);
CREATE UNIQUE INDEX idx_plugin_secret_installation_key ON plugin_secret (installation_id, "key");
CREATE UNIQUE INDEX idx_plugin_storage_scope_key ON plugin_storage (installation_id, scope_type, scope_id, "key");
CREATE INDEX idx_project_resource_project ON project_resource (project_id, "position");
CREATE INDEX idx_project_resource_workspace ON project_resource (workspace_id);
CREATE INDEX idx_project_workspace ON project (workspace_id);
CREATE INDEX idx_quick_action_workspace_status_usage ON quick_action (workspace_id, status, use_count DESC);
CREATE INDEX idx_runtime_profile_workspace ON runtime_profile (workspace_id);
CREATE INDEX idx_search_index_change_changed_at ON search_index_change (changed_at);
CREATE INDEX idx_search_index_change_workspace_seq ON search_index_change (workspace_id, change_seq, entity_type, entity_id);
CREATE INDEX idx_seat_capacity_outbox_due ON seat_capacity_outbox (next_attempt_at, created_at) WHERE (dead_lettered_at IS NULL);
CREATE UNIQUE INDEX idx_seat_capacity_outbox_share_join ON seat_capacity_outbox (workspace_id, share_link_id, user_id) WHERE ((share_link_id IS NOT NULL) AND (user_id IS NOT NULL));
CREATE INDEX idx_skill_file_skill ON skill_file (skill_id);
CREATE INDEX idx_skill_workspace ON skill (workspace_id);
CREATE INDEX idx_squad_member_entity ON squad_member (member_type, member_id);
CREATE INDEX idx_squad_member_squad ON squad_member (squad_id);
CREATE INDEX idx_squad_workspace ON squad (workspace_id);
CREATE INDEX idx_sys_cron_exec_failed_recent ON sys_cron_executions (job_name, plan_time DESC) WHERE (status = 'FAILED');
CREATE INDEX idx_sys_cron_exec_finished ON sys_cron_executions (finished_at) WHERE (status IN ('SUCCESS', 'FAILED'));
CREATE INDEX idx_sys_cron_exec_running_stale ON sys_cron_executions (stale_after) WHERE (status = 'RUNNING');
CREATE INDEX idx_task_chat_finalize_deferred ON agent_task_queue (chat_finalize_deferred_at) WHERE (chat_finalize_deferred_at IS NOT NULL);
CREATE INDEX idx_task_message_task_id_seq ON task_message (task_id, seq);
CREATE INDEX idx_task_token_agent_id ON task_token (agent_id);
CREATE UNIQUE INDEX idx_task_token_hash ON task_token (token_hash);
CREATE INDEX idx_task_token_task ON task_token (task_id);
CREATE INDEX idx_task_token_workspace_id ON task_token (workspace_id);
CREATE INDEX idx_task_usage_created_at ON task_usage (created_at);
CREATE INDEX idx_task_usage_created_at_legacy ON task_usage (created_at) WHERE (updated_at IS NULL);
CREATE INDEX idx_task_usage_hourly_dirty_enqueued_at ON task_usage_hourly_dirty (enqueued_at);
CREATE INDEX idx_task_usage_hourly_runtime_time ON task_usage_hourly (runtime_id, bucket_hour DESC);
CREATE INDEX idx_task_usage_hourly_workspace_agent_time ON task_usage_hourly (workspace_id, agent_id, bucket_hour DESC);
CREATE INDEX idx_task_usage_hourly_workspace_project_time ON task_usage_hourly (workspace_id, project_id, bucket_hour DESC) WHERE (project_id IS NOT NULL);
CREATE INDEX idx_task_usage_hourly_workspace_time ON task_usage_hourly (workspace_id, bucket_hour DESC);
CREATE INDEX idx_task_usage_task_id ON task_usage (task_id);
CREATE INDEX idx_task_usage_updated_at ON task_usage (updated_at);
CREATE INDEX idx_user_created_at ON "user" (created_at);
CREATE INDEX idx_vcs_commit_status_lookup ON vcs_commit_status (connection_id, sha);
CREATE INDEX idx_vcs_connection_workspace ON vcs_connection (workspace_id);
CREATE INDEX idx_vcs_pull_request_connection ON vcs_pull_request (connection_id);
CREATE INDEX idx_vcs_pull_request_workspace ON vcs_pull_request (workspace_id);
CREATE INDEX idx_verification_code_email ON verification_code (email, used, expires_at);
CREATE INDEX idx_wakeup_event_issue ON issue_wakeup (issue_id) WHERE (enabled AND (kind = 'event'));
CREATE INDEX idx_wakeup_workspace_enabled ON issue_wakeup (workspace_id, issue_id) WHERE enabled;
CREATE INDEX idx_webhook_delivery_autopilot ON webhook_delivery (autopilot_id, created_at DESC);
CREATE UNIQUE INDEX idx_webhook_delivery_dedupe ON webhook_delivery (trigger_id, dedupe_key) WHERE ((dedupe_key IS NOT NULL) AND (status NOT IN ('rejected', 'failed')));
CREATE INDEX idx_webhook_delivery_queue ON webhook_delivery (available_at, created_at) WHERE (status = 'queued');
CREATE INDEX idx_webhook_delivery_run ON webhook_delivery (autopilot_run_id) WHERE (autopilot_run_id IS NOT NULL);
CREATE UNIQUE INDEX idx_workspace_mcp_server_workspace_name ON workspace_mcp_server (workspace_id, name);
CREATE UNIQUE INDEX issue_child_event_id_idx ON issue_child_event (id);
CREATE INDEX issue_child_event_pending_idx ON issue_child_event (parent_id, created_at) WHERE (processed_at IS NULL);
CREATE INDEX issue_label_workspace_type_idx ON issue_label (workspace_id, resource_type);
CREATE UNIQUE INDEX issue_label_workspace_type_name_lower_idx ON issue_label (workspace_id, resource_type, lower(name));
CREATE INDEX issue_to_label_label_idx ON issue_to_label (label_id);
CREATE INDEX issue_wakeup_due_idx ON issue_wakeup (next_fire_at) WHERE enabled;
CREATE INDEX issue_wakeup_expiry_idx ON issue_wakeup (expires_at) WHERE (enabled AND (expires_at IS NOT NULL));
CREATE UNIQUE INDEX issue_wakeup_id_idx ON issue_wakeup (id);
CREATE INDEX issue_wakeup_issue_idx ON issue_wakeup (issue_id, workspace_id);
CREATE UNIQUE INDEX issue_wakeup_pending_event_idx ON issue_wakeup_receipt (wakeup_id, revision, coalesce_key) WHERE ((processed_at IS NULL) AND (coalesce_key IS NOT NULL));
CREATE INDEX issue_wakeup_receipt_expiry_idx ON issue_wakeup_receipt (processed_at, id) WHERE (processed_at IS NOT NULL);
CREATE UNIQUE INDEX issue_wakeup_receipt_id_idx ON issue_wakeup_receipt (id);
CREATE UNIQUE INDEX issue_wakeup_receipt_key_idx ON issue_wakeup_receipt (wakeup_id, revision, event_key);
CREATE INDEX issue_wakeup_receipt_pending_idx ON issue_wakeup_receipt (wakeup_id, created_at) WHERE (processed_at IS NULL);
CREATE UNIQUE INDEX issue_wakeup_system_rule_idx ON issue_wakeup (issue_id, system_rule) WHERE (system_rule IS NOT NULL);
CREATE INDEX issue_wakeup_workspace_history_idx ON issue_wakeup (workspace_id, created_at DESC, id DESC);
CREATE INDEX orchestrator_event_pending_idx ON orchestrator_event (id) WHERE (processed_at IS NULL);
CREATE INDEX orchestrator_team_workspace_idx ON orchestrator_team (workspace_id);
CREATE INDEX orchestrator_transition_issue_idx ON orchestrator_transition (issue_id, id DESC);
CREATE INDEX skill_to_label_label_idx ON skill_to_label (label_id);
CREATE UNIQUE INDEX task_supplement_capability_task_uidx ON task_supplement_capability (task_id);
CREATE UNIQUE INDEX task_supplement_task_request_uidx ON task_supplement (task_id, client_request_id);
CREATE UNIQUE INDEX uq_autopilot_quota_reservation_key ON autopilot_quota_reservation (workspace_id, period_start, period_end, idempotency_key) WHERE (state <> 'released');
CREATE UNIQUE INDEX uq_autopilot_run_quota_reservation ON autopilot_run (quota_reservation_id) WHERE (quota_reservation_id IS NOT NULL);
CREATE UNIQUE INDEX uq_autopilot_run_trigger_planned ON autopilot_run (trigger_id, planned_at) WHERE ((trigger_id IS NOT NULL) AND (planned_at IS NOT NULL));
CREATE UNIQUE INDEX uq_autopilot_run_webhook_delivery ON autopilot_run (webhook_delivery_id) WHERE (webhook_delivery_id IS NOT NULL);
CREATE UNIQUE INDEX uq_webhook_delivery_replay_idempotency ON webhook_delivery (replayed_from_delivery_id, replay_idempotency_key) WHERE ((replayed_from_delivery_id IS NOT NULL) AND (replay_idempotency_key IS NOT NULL));
CREATE INDEX user_composio_connection_account_idx ON user_composio_connection (connected_account_id);
CREATE INDEX user_composio_connection_user_status_idx ON user_composio_connection (user_id, status);
CREATE UNIQUE INDEX workspace_share_link_active_ws_uidx ON workspace_share_link (workspace_id) WHERE (is_active = 1);
CREATE UNIQUE INDEX workspace_share_link_code_uidx ON workspace_share_link (code);

CREATE VIRTUAL TABLE issue_fts USING fts5(
    id UNINDEXED,
    workspace_id UNINDEXED,
    title,
    description,
    tokenize = 'trigram'
);

CREATE VIRTUAL TABLE comment_fts USING fts5(
    id UNINDEXED,
    workspace_id UNINDEXED,
    content,
    tokenize = 'trigram'
);

CREATE VIRTUAL TABLE project_fts USING fts5(
    id UNINDEXED,
    workspace_id UNINDEXED,
    title,
    description,
    tokenize = 'trigram'
);

CREATE TRIGGER search_index_issue_ai
AFTER INSERT ON issue
BEGIN
    UPDATE search_change_counter SET n = n + 1 WHERE singleton = 1;
    INSERT INTO search_index_change (entity_type, entity_id, workspace_id, change_seq, changed_at)
    VALUES (
        'issue',
        NEW.id,
        NEW.workspace_id,
        (SELECT n FROM search_change_counter WHERE singleton = 1),
        strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    )
    ON CONFLICT (entity_type, entity_id) DO UPDATE SET
        workspace_id = excluded.workspace_id,
        change_seq = excluded.change_seq,
        changed_at = excluded.changed_at;
END;

CREATE TRIGGER search_index_issue_au
AFTER UPDATE ON issue
BEGIN
    UPDATE search_change_counter SET n = n + 1 WHERE singleton = 1;
    INSERT INTO search_index_change (entity_type, entity_id, workspace_id, change_seq, changed_at)
    VALUES (
        'issue',
        NEW.id,
        NEW.workspace_id,
        (SELECT n FROM search_change_counter WHERE singleton = 1),
        strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    )
    ON CONFLICT (entity_type, entity_id) DO UPDATE SET
        workspace_id = excluded.workspace_id,
        change_seq = excluded.change_seq,
        changed_at = excluded.changed_at;
END;

CREATE TRIGGER search_index_issue_ad
AFTER DELETE ON issue
BEGIN
    UPDATE search_change_counter SET n = n + 1 WHERE singleton = 1;
    INSERT INTO search_index_change (entity_type, entity_id, workspace_id, change_seq, changed_at)
    VALUES (
        'issue',
        OLD.id,
        OLD.workspace_id,
        (SELECT n FROM search_change_counter WHERE singleton = 1),
        strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    )
    ON CONFLICT (entity_type, entity_id) DO UPDATE SET
        workspace_id = excluded.workspace_id,
        change_seq = excluded.change_seq,
        changed_at = excluded.changed_at;
END;

CREATE TRIGGER search_index_comment_ai
AFTER INSERT ON comment
BEGIN
    UPDATE search_change_counter SET n = n + 1 WHERE singleton = 1;
    INSERT INTO search_index_change (entity_type, entity_id, workspace_id, change_seq, changed_at)
    VALUES (
        'comment',
        NEW.id,
        NEW.workspace_id,
        (SELECT n FROM search_change_counter WHERE singleton = 1),
        strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    )
    ON CONFLICT (entity_type, entity_id) DO UPDATE SET
        workspace_id = excluded.workspace_id,
        change_seq = excluded.change_seq,
        changed_at = excluded.changed_at;
END;

CREATE TRIGGER search_index_comment_au
AFTER UPDATE ON comment
BEGIN
    UPDATE search_change_counter SET n = n + 1 WHERE singleton = 1;
    INSERT INTO search_index_change (entity_type, entity_id, workspace_id, change_seq, changed_at)
    VALUES (
        'comment',
        NEW.id,
        NEW.workspace_id,
        (SELECT n FROM search_change_counter WHERE singleton = 1),
        strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    )
    ON CONFLICT (entity_type, entity_id) DO UPDATE SET
        workspace_id = excluded.workspace_id,
        change_seq = excluded.change_seq,
        changed_at = excluded.changed_at;
END;

CREATE TRIGGER search_index_comment_ad
AFTER DELETE ON comment
BEGIN
    UPDATE search_change_counter SET n = n + 1 WHERE singleton = 1;
    INSERT INTO search_index_change (entity_type, entity_id, workspace_id, change_seq, changed_at)
    VALUES (
        'comment',
        OLD.id,
        OLD.workspace_id,
        (SELECT n FROM search_change_counter WHERE singleton = 1),
        strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    )
    ON CONFLICT (entity_type, entity_id) DO UPDATE SET
        workspace_id = excluded.workspace_id,
        change_seq = excluded.change_seq,
        changed_at = excluded.changed_at;
END;

CREATE TRIGGER search_index_project_ai
AFTER INSERT ON project
BEGIN
    UPDATE search_change_counter SET n = n + 1 WHERE singleton = 1;
    INSERT INTO search_index_change (entity_type, entity_id, workspace_id, change_seq, changed_at)
    VALUES (
        'project',
        NEW.id,
        NEW.workspace_id,
        (SELECT n FROM search_change_counter WHERE singleton = 1),
        strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    )
    ON CONFLICT (entity_type, entity_id) DO UPDATE SET
        workspace_id = excluded.workspace_id,
        change_seq = excluded.change_seq,
        changed_at = excluded.changed_at;
END;

CREATE TRIGGER search_index_project_au
AFTER UPDATE ON project
BEGIN
    UPDATE search_change_counter SET n = n + 1 WHERE singleton = 1;
    INSERT INTO search_index_change (entity_type, entity_id, workspace_id, change_seq, changed_at)
    VALUES (
        'project',
        NEW.id,
        NEW.workspace_id,
        (SELECT n FROM search_change_counter WHERE singleton = 1),
        strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    )
    ON CONFLICT (entity_type, entity_id) DO UPDATE SET
        workspace_id = excluded.workspace_id,
        change_seq = excluded.change_seq,
        changed_at = excluded.changed_at;
END;

CREATE TRIGGER search_index_project_ad
AFTER DELETE ON project
BEGIN
    UPDATE search_change_counter SET n = n + 1 WHERE singleton = 1;
    INSERT INTO search_index_change (entity_type, entity_id, workspace_id, change_seq, changed_at)
    VALUES (
        'project',
        OLD.id,
        OLD.workspace_id,
        (SELECT n FROM search_change_counter WHERE singleton = 1),
        strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    )
    ON CONFLICT (entity_type, entity_id) DO UPDATE SET
        workspace_id = excluded.workspace_id,
        change_seq = excluded.change_seq,
        changed_at = excluded.changed_at;
END;

CREATE TRIGGER issue_fts_ai
AFTER INSERT ON issue
BEGIN
    INSERT INTO issue_fts (id, workspace_id, title, description) VALUES (NEW.id, NEW.workspace_id, NEW.title, NEW.description);
END;

CREATE TRIGGER issue_fts_au
AFTER UPDATE ON issue
BEGIN
    DELETE FROM issue_fts WHERE id = OLD.id;
    INSERT INTO issue_fts (id, workspace_id, title, description) VALUES (NEW.id, NEW.workspace_id, NEW.title, NEW.description);
END;

CREATE TRIGGER issue_fts_ad
AFTER DELETE ON issue
BEGIN
    DELETE FROM issue_fts WHERE id = OLD.id;
END;

CREATE TRIGGER comment_fts_ai
AFTER INSERT ON comment
BEGIN
    INSERT INTO comment_fts (id, workspace_id, content) VALUES (NEW.id, NEW.workspace_id, NEW.content);
END;

CREATE TRIGGER comment_fts_au
AFTER UPDATE ON comment
BEGIN
    DELETE FROM comment_fts WHERE id = OLD.id;
    INSERT INTO comment_fts (id, workspace_id, content) VALUES (NEW.id, NEW.workspace_id, NEW.content);
END;

CREATE TRIGGER comment_fts_ad
AFTER DELETE ON comment
BEGIN
    DELETE FROM comment_fts WHERE id = OLD.id;
END;

CREATE TRIGGER project_fts_ai
AFTER INSERT ON project
BEGIN
    INSERT INTO project_fts (id, workspace_id, title, description) VALUES (NEW.id, NEW.workspace_id, NEW.title, NEW.description);
END;

CREATE TRIGGER project_fts_au
AFTER UPDATE ON project
BEGIN
    DELETE FROM project_fts WHERE id = OLD.id;
    INSERT INTO project_fts (id, workspace_id, title, description) VALUES (NEW.id, NEW.workspace_id, NEW.title, NEW.description);
END;

CREATE TRIGGER project_fts_ad
AFTER DELETE ON project
BEGIN
    DELETE FROM project_fts WHERE id = OLD.id;
END;
