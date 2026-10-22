-- name: EnsureAutopilotQuotaPeriod :one
-- The no-op update is intentional: ON CONFLICT UPDATE locks the period row for
-- the caller's transaction, serialising admission for one workspace/period.
INSERT INTO autopilot_quota_period (workspace_id, period_start, period_end)
VALUES (?1, ?2, ?3)
ON CONFLICT (workspace_id, period_start, period_end) DO UPDATE
SET updated_at = autopilot_quota_period.updated_at
RETURNING *;

-- name: GetAutopilotQuotaPeriod :one
SELECT * FROM autopilot_quota_period
WHERE workspace_id = ?1 AND period_start = ?2 AND period_end = ?3;

-- name: GetAutopilotQuotaReservationByKey :one
SELECT * FROM autopilot_quota_reservation
WHERE workspace_id = ?1
  AND period_start = ?2
  AND period_end = ?3
  AND idempotency_key = ?4
  AND state <> 'released';

-- name: CreateAutopilotQuotaReservation :one
INSERT INTO autopilot_quota_reservation (
    workspace_id, period_start, period_end, policy_revision,
    subscription_version, source, idempotency_key
) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7)
RETURNING *;

-- name: IncrementAutopilotQuotaReserved :one
UPDATE autopilot_quota_period
SET reserved_count = reserved_count + 1,
    updated_at = now()
WHERE workspace_id = ?1 AND period_start = ?2 AND period_end = ?3
RETURNING *;

-- name: IncrementAutopilotQuotaBlocked :one
UPDATE autopilot_quota_period
SET blocked_counts = json_set(
        blocked_counts,
        '$.' || @source,
        COALESCE(json_extract(blocked_counts, '$.' || @source), 0) + 1
    ),
    updated_at = now()
WHERE workspace_id = @workspace_id
  AND period_start = @period_start
  AND period_end = @period_end
RETURNING *;

-- name: MarkAutopilotQuotaRejectionNotified :one
UPDATE autopilot_quota_period
SET rejection_notified_at = COALESCE(rejection_notified_at, now()),
	updated_at = now()
WHERE workspace_id = @workspace_id
	AND period_start = @period_start
	AND period_end = @period_end
RETURNING *;

-- name: ConsumeAutopilotQuotaReservation :one
-- used_count is monotonic within a period: consuming a reserved slot is the
-- only write that changes it, and no release path decrements it.
-- SQLite cannot update the reservation and the period in one statement. This
-- statement moves the counters (the returned row). MarkConsumedAutopilotQuotaReservation
-- must run in the same transaction, before or after, while state is still reserved
-- is checked here so a missing period does not change the counters.
UPDATE autopilot_quota_period
SET reserved_count = reserved_count - 1,
    used_count = used_count + 1,
    updated_at = now()
WHERE workspace_id = (
    SELECT qr.workspace_id
    FROM autopilot_quota_reservation qr
    WHERE qr.id = @reservation_id
      AND qr.state = 'reserved'
      AND qr.workspace_id = autopilot_quota_period.workspace_id
      AND qr.period_start = autopilot_quota_period.period_start
      AND qr.period_end = autopilot_quota_period.period_end
)
  AND period_start = (
    SELECT qr.period_start FROM autopilot_quota_reservation qr
    WHERE qr.id = @reservation_id AND qr.state = 'reserved'
)
  AND period_end = (
    SELECT qr.period_end FROM autopilot_quota_reservation qr
    WHERE qr.id = @reservation_id AND qr.state = 'reserved'
)
RETURNING *;

-- name: MarkConsumedAutopilotQuotaReservation :exec
-- Second half of ConsumeAutopilotQuotaReservation. Marks the slot consumed when
-- its period row exists, so a replay does not move the counters again.
UPDATE autopilot_quota_reservation
SET state = 'consumed',
    finalized_at = now()
WHERE id = @reservation_id
  AND state = 'reserved'
  AND EXISTS (
      SELECT 1 FROM autopilot_quota_period p
      WHERE p.workspace_id = autopilot_quota_reservation.workspace_id
        AND p.period_start = autopilot_quota_reservation.period_start
        AND p.period_end = autopilot_quota_reservation.period_end
  );

-- name: ReleaseAutopilotQuotaReservation :one
-- Release is intentionally limited to still-reserved work. A consumed
-- create_issue slot remains counted after cancellation, blocking, or deletion.
-- The reservation state flip is MarkReleasedAutopilotQuotaReservation; SQLite
-- cannot apply that write in this statement.
UPDATE autopilot_quota_period
SET reserved_count = reserved_count - 1,
    updated_at = now()
WHERE workspace_id = (
    SELECT qr.workspace_id
    FROM autopilot_quota_reservation qr
    WHERE qr.id = @reservation_id
      AND qr.state = 'reserved'
      AND qr.workspace_id = autopilot_quota_period.workspace_id
      AND qr.period_start = autopilot_quota_period.period_start
      AND qr.period_end = autopilot_quota_period.period_end
)
  AND period_start = (
    SELECT qr.period_start FROM autopilot_quota_reservation qr
    WHERE qr.id = @reservation_id AND qr.state = 'reserved'
)
  AND period_end = (
    SELECT qr.period_end FROM autopilot_quota_reservation qr
    WHERE qr.id = @reservation_id AND qr.state = 'reserved'
)
RETURNING *;

-- name: MarkReleasedAutopilotQuotaReservation :exec
UPDATE autopilot_quota_reservation
SET state = 'released',
    finalized_at = now()
WHERE id = @reservation_id
  AND state = 'reserved'
  AND EXISTS (
      SELECT 1 FROM autopilot_quota_period p
      WHERE p.workspace_id = autopilot_quota_reservation.workspace_id
        AND p.period_start = autopilot_quota_reservation.period_start
        AND p.period_end = autopilot_quota_reservation.period_end
  );

-- name: ListRecoverableAutopilotQuotaReservations :many
SELECT r.*
FROM autopilot_quota_reservation r
LEFT JOIN autopilot_run ar ON ar.quota_reservation_id = r.id
WHERE r.state = 'reserved'
  AND (
      (
          r.created_at < @terminal_created_before
          AND (ar.id IS NULL OR ar.status IN ('completed', 'failed', 'skipped'))
      )
      OR (
          -- Manual/API requests have no durable retry owner. Reclaim only an
          -- hours-old partial run with neither a linked side effect nor even
          -- an unlinked task row; schedule and webhook retries repair their
          -- own partial state and are deliberately excluded.
          r.created_at < @partial_created_before
          AND ar.source IN ('manual', 'api')
          AND (
              ar.status = 'pending'
              OR (ar.status = 'issue_created' AND ar.issue_id IS NULL)
              OR (ar.status = 'running' AND ar.task_id IS NULL)
          )
          AND NOT EXISTS (
              SELECT 1
              FROM agent_task_queue task
              WHERE task.autopilot_run_id = ar.id
          )
      )
  )
ORDER BY r.created_at
LIMIT @row_limit;
