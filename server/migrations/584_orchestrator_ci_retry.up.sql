-- babysit rule "one CI re-run per red head", persisted so a server restart
-- does not grant a second re-run of the same head.
CREATE TABLE orchestrator_ci_retry (
    head_sha   TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
