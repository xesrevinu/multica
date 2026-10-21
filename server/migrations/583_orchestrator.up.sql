-- In-app orchestrator (internal/orchestrator). A project with a row in
-- orchestrator_team is "orchestrated": the engine is the sole writer of its
-- issues' status and assignee.
CREATE TABLE orchestrator_team (
    project_id      UUID PRIMARY KEY REFERENCES project(id) ON DELETE CASCADE,
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    spec            JSONB NOT NULL,
    spec_yaml       TEXT NOT NULL DEFAULT '',
    paused          BOOLEAN NOT NULL DEFAULT false,
    paused_reason   TEXT NOT NULL DEFAULT '',
    human_user_id   UUID REFERENCES "user"(id) ON DELETE SET NULL,
    applied_by      UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX orchestrator_team_workspace_idx ON orchestrator_team (workspace_id);

-- Append-only transition log. idem_key = issue|tid|issue revision: a
-- transition decided twice on the same revision is applied once.
CREATE TABLE orchestrator_transition (
    id            BIGSERIAL PRIMARY KEY,
    workspace_id  UUID NOT NULL,
    project_id    UUID NOT NULL,
    issue_id      UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    tid           TEXT NOT NULL,
    idem_key      TEXT NOT NULL UNIQUE,
    decision      JSONB NOT NULL,
    state         TEXT NOT NULL CHECK (state IN ('started', 'done', 'failed')),
    error         TEXT NOT NULL DEFAULT '',
    source        TEXT NOT NULL DEFAULT '', -- event | reconcile | replay | api
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at   TIMESTAMPTZ
);
CREATE INDEX orchestrator_transition_issue_idx ON orchestrator_transition (issue_id, id DESC);

-- Durable wake-up queue fed from the event bus; the reconciler covers misses.
CREATE TABLE orchestrator_event (
    id            BIGSERIAL PRIMARY KEY,
    issue_id      UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    kind          TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at  TIMESTAMPTZ
);
CREATE INDEX orchestrator_event_pending_idx ON orchestrator_event (id) WHERE processed_at IS NULL;

-- Per-issue failure / backoff / reminder bookkeeping (dead-letter after N).
CREATE TABLE orchestrator_issue_state (
    issue_id      UUID PRIMARY KEY REFERENCES issue(id) ON DELETE CASCADE,
    failures      INT NOT NULL DEFAULT 0,
    next_at       TIMESTAMPTZ,
    last_error    TEXT NOT NULL DEFAULT '',
    reminded_at   TIMESTAMPTZ,
    last_wait     TEXT NOT NULL DEFAULT '',
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
