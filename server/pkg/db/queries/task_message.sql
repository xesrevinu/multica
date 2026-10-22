-- name: CreateTaskMessage :one
INSERT INTO task_message (id, task_id, seq, type, tool, content, input, output, output_truncated, call_id)
VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10)
RETURNING *;

-- name: CreateTaskMessages :many
-- Batch variant of CreateTaskMessage: persists a whole daemon-reported batch in
-- ONE statement - therefore one round trip and, more importantly, one commit
-- instead of one per message. Commit acknowledgement (IO:XactSync) is ~94% of
-- this SQL's load in production, so the commit count is the thing being
-- optimized; the round trip is a bonus.
--
-- The rows arrive as parallel arrays rather than as one jsonb document, even
-- though a jsonb document is the tidier Go side. content and output routinely
-- carry tens of KB and occasionally megabytes, and wrapping them in JSON makes
-- the server escape every byte a second time and Postgres parse the whole
-- envelope back out - measured at 1.2x the old per-row insert at 128KB and
-- ~1.8x at 1MB, i.e. a regression on exactly the most expensive requests, which
-- are also the ones least likely to be batched. Native text[] elements are
-- length-prefixed by the wire protocol, so they cost neither pass.
--
-- NULLIF is what makes per-row NULL expressible through a non-nullable []string
-- (the Go type sqlc gives a text[] parameter): it reproduces, exactly, the
-- `pgtype.Text{Valid: x != ""}` mapping the single-row query carries - empty
-- string means SQL NULL. input is passed as text and cast here for the same
-- reason; it is the one column that genuinely has to be parsed as JSON, because
-- it is a jsonb column. created_at also rides this transport so an older daemon
-- can omit its event time and keep the database-time fallback. output_truncated
-- uses the same trick because it is a genuinely tri-state boolean (NULL = the
-- reporting daemon did not measure it), and a bool[] parameter becomes a Go
-- []bool, which has no way to spell the third state.
--
-- Callers MUST still run the Postgres text sanitizer first. A NUL anywhere in
-- the batch fails the whole statement (GH #7098) - that is inherent to batching
-- into one statement, not to the parameter shape.
--
-- Atomicity is a deliberate side effect, not just a speedup: the per-message
-- loop this replaces could persist part of a batch and then fail, leaving the
-- transcript with a prefix of the batch and no way to complete it - the daemon
-- does not retry this endpoint. One statement makes the batch all-or-nothing,
-- which buys consistency; a batch that fails is still lost whole, so closing
-- the gap for real needs a retry plus a (task_id, seq) uniqueness rule.
--
-- The ORDER BY is a contract, not decoration. A bare `INSERT ... RETURNING`
-- has no defined row order, and the caller republishes these rows as realtime
-- events in the order they arrive - the per-row loop this replaces implicitly
-- published in request order, so the ordering has to be restored explicitly or
-- subscribers can see a batch out of order. seq is assigned by the daemon and
-- increases within a batch, so it is the request order.
-- JSON shape: sqlc.arg('messages') is one JSON array, one object per row:
-- [{"id":"...","seq":1,"type":"...","tool":"","call_id":"","content":"",
--   "input":"","output":"","created_at":"","output_truncated":""}]
-- Empty string means NULL, matching the single-row insert.
INSERT INTO task_message (id, task_id, seq, type, tool, content, input, output, created_at, output_truncated, call_id)
SELECT
    json_extract(m.value, '$.id') AS id,
    sqlc.arg('task_id') AS task_id,
    json_extract(m.value, '$.seq') AS seq,
    json_extract(m.value, '$.type') AS type,
    NULLIF(json_extract(m.value, '$.tool'), '') AS tool,
    NULLIF(json_extract(m.value, '$.content'), '') AS content,
    NULLIF(json_extract(m.value, '$.input'), '') AS input,
    NULLIF(json_extract(m.value, '$.output'), '') AS output,
    COALESCE(NULLIF(json_extract(m.value, '$.created_at'), ''), now()) AS created_at,
    NULLIF(json_extract(m.value, '$.output_truncated'), '') AS output_truncated,
    NULLIF(json_extract(m.value, '$.call_id'), '') AS call_id
FROM json_each(sqlc.arg('messages')) AS m
ORDER BY json_extract(m.value, '$.seq') ASC
RETURNING *;

-- name: ListTaskMessages :many
SELECT * FROM task_message
WHERE task_id = ?1
ORDER BY seq ASC;

-- name: ListTaskMessagesSince :many
SELECT * FROM task_message
WHERE task_id = ?1 AND seq > ?2
ORDER BY seq ASC;

-- name: DeleteTaskMessages :exec
DELETE FROM task_message
WHERE task_id = ?1;
