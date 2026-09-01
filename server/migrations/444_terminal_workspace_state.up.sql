-- Per-user terminal session list + split layout for one workspace.
-- Opaque JSON owned by the client: { sessions, activeSessionId, sidebarCollapsed }.
-- No FKs by repository policy; member-revoke and workspace-delete prune explicitly.
CREATE TABLE terminal_workspace_state (
    workspace_id UUID NOT NULL,
    user_id UUID NOT NULL,
    state JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(state) = 'object'),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, user_id)
);
