-- name: ListRuntimeUsage :many
-- Reads from the UTC-bucketed `task_usage_hourly` rollup table,
-- aggregated to per-(date, provider, model) under the
-- caller-supplied @tz. Powers the trend chart on the runtime detail
-- page and the per-row cost cell on the runtimes list.
--
-- @tz is required, even if the caller intends "UTC", so the bucket
-- cast is unambiguous - `bucket_hour` is UTC and the caller picks the
-- calendar boundary per request.
--
-- provider is LOWER()-normalized so mixed-case historical rows merge
-- (same reason as ListRuntimeUsageByAgent below).
SELECT
    CASE WHEN sqlc.arg('tz') IS NOT NULL THEN date(bucket_hour) END AS date,
    LOWER(provider) AS provider,
    model,
    SUM(input_tokens)        AS input_tokens,
    SUM(output_tokens)       AS output_tokens,
    SUM(cache_read_tokens)   AS cache_read_tokens,
    SUM(cache_write_tokens)  AS cache_write_tokens,
    SUM(cost_usd_ticks)                                          AS cost_usd_ticks,
    SUM(COALESCE(uncosted_input_tokens, input_tokens))           AS uncosted_input_tokens,
    SUM(COALESCE(uncosted_output_tokens, output_tokens))         AS uncosted_output_tokens,
    SUM(COALESCE(uncosted_cache_read_tokens, cache_read_tokens)) AS uncosted_cache_read_tokens,
    SUM(COALESCE(uncosted_cache_write_tokens, cache_write_tokens)) AS uncosted_cache_write_tokens
FROM task_usage_hourly
WHERE runtime_id = ?1
  AND bucket_hour >= sqlc.arg('since')
GROUP BY CASE WHEN sqlc.arg('tz') IS NOT NULL THEN date(bucket_hour) END, LOWER(provider), model
ORDER BY CASE WHEN sqlc.arg('tz') IS NOT NULL THEN date(bucket_hour) END DESC, LOWER(provider), model;

-- name: GetRuntimeTaskHourlyActivity :many
-- Hour-of-day distribution for queue starts. Bucketed in the viewer's
-- tz so "this runtime is busy in the afternoon" actually means
-- the operator's afternoon, not UTC's.
SELECT CAST(strftime('%H', started_at) AS INTEGER) AS hour,
       COUNT(*) AS count
FROM agent_task_queue
WHERE runtime_id = ?1 AND started_at IS NOT NULL
GROUP BY hour
ORDER BY hour;

-- name: ListRuntimeUsageByAgent :many
-- Per-(agent, provider, model) token aggregates for a runtime since a cutoff. Powers
-- the runtime-detail "Cost by agent" tab. task_usage only carries task_id,
-- so we join the queue to expose agent_id. The model dimension is kept on
-- purpose: cost is computed client-side from a per-model pricing table, so
-- collapsing models server-side would erase the information needed to do
-- that arithmetic. The client groups by agent_id and sums cost per agent.
--
-- This view doesn't bucket by date, so it doesn't need @tz; only the
-- @since cutoff is provided in runtime-local terms (computed in Go).
-- provider is LOWER()-normalized so mixed-case historical rows merge with
-- new rows (see ListDashboardUsageDaily in task_usage.sql).
SELECT
    atq.agent_id,
    LOWER(tu.provider) AS provider,
    tu.model,
    SUM(tu.input_tokens) AS input_tokens,
    SUM(tu.output_tokens) AS output_tokens,
    SUM(tu.cache_read_tokens) AS cache_read_tokens,
    SUM(tu.cache_write_tokens) AS cache_write_tokens,
    COALESCE(SUM(tu.cost_usd_ticks), 0) AS cost_usd_ticks,
    COALESCE(SUM(tu.input_tokens)       FILTER (WHERE tu.cost_usd_ticks IS NULL), 0) AS uncosted_input_tokens,
    COALESCE(SUM(tu.output_tokens)      FILTER (WHERE tu.cost_usd_ticks IS NULL), 0) AS uncosted_output_tokens,
    COALESCE(SUM(tu.cache_read_tokens)  FILTER (WHERE tu.cost_usd_ticks IS NULL), 0) AS uncosted_cache_read_tokens,
    COALESCE(SUM(tu.cache_write_tokens) FILTER (WHERE tu.cost_usd_ticks IS NULL), 0) AS uncosted_cache_write_tokens,
    COUNT(DISTINCT tu.task_id) AS task_count
FROM task_usage tu
JOIN agent_task_queue atq ON atq.id = tu.task_id
WHERE atq.runtime_id = ?1
  AND tu.created_at >= @since
GROUP BY atq.agent_id, LOWER(tu.provider), tu.model
ORDER BY atq.agent_id, LOWER(tu.provider), tu.model;

-- name: GetRuntimeUsageByHour :many
-- Per-(hour, model) token aggregates (hour   0..23) for a runtime since a
-- cutoff. Powers the "By hour" tab - shows when in the day this runtime is
-- doing real work, with model preserved for client-side cost calculation
-- (same reason as ListRuntimeUsageByAgent above). Hours with zero activity
-- are omitted; the client fills the 24-bucket axis.
--
-- Hours are extracted in the viewer's tz via @tz so afternoon
-- work bucketed at UTC 06:00 lands in 14:00 for a UTC+8 viewer.
SELECT
    CAST(strftime('%H', tu.created_at) AS INTEGER) AS hour,
    tu.model,
    SUM(tu.input_tokens) AS input_tokens,
    SUM(tu.output_tokens) AS output_tokens,
    SUM(tu.cache_read_tokens) AS cache_read_tokens,
    SUM(tu.cache_write_tokens) AS cache_write_tokens,
    COALESCE(SUM(tu.cost_usd_ticks), 0) AS cost_usd_ticks,
    COALESCE(SUM(tu.input_tokens)       FILTER (WHERE tu.cost_usd_ticks IS NULL), 0) AS uncosted_input_tokens,
    COALESCE(SUM(tu.output_tokens)      FILTER (WHERE tu.cost_usd_ticks IS NULL), 0) AS uncosted_output_tokens,
    COALESCE(SUM(tu.cache_read_tokens)  FILTER (WHERE tu.cost_usd_ticks IS NULL), 0) AS uncosted_cache_read_tokens,
    COALESCE(SUM(tu.cache_write_tokens) FILTER (WHERE tu.cost_usd_ticks IS NULL), 0) AS uncosted_cache_write_tokens,
    COUNT(DISTINCT tu.task_id) AS task_count
FROM task_usage tu
JOIN agent_task_queue atq ON atq.id = tu.task_id
WHERE atq.runtime_id = ?1
  AND tu.created_at >= @since
GROUP BY CAST(strftime('%H', tu.created_at) AS INTEGER), tu.model
ORDER BY hour, tu.model;
