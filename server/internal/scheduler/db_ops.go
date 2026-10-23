package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// ErrLeaseLost is returned by heartbeat / terminal-update primitives
// when the row is no longer owned by the calling runner. The handler
// should stop, and the scheduler will not write a terminal status for
// the lost lease.
var ErrLeaseLost = errors.New("scheduler: lease lost")

// errTerminalIgnored is the sentinel finishSuccess / finishFailure use
// to convey a successful UPDATE that affected zero rows. Internal to
// this package.
var errTerminalIgnored = ErrLeaseLost

func formatDBTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseDBTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func parseDBID(s string) (uuid.UUID, error) {
	if s == "" {
		return uuid.Nil, fmt.Errorf("empty id")
	}
	return uuid.Parse(s)
}

// dbNow returns the database clock. now() is registered by sqliteutil and
// returns UTC RFC3339Nano text.
func dbNow(ctx context.Context, pool *sql.DB) (time.Time, error) {
	var raw string
	if err := pool.QueryRowContext(ctx, "SELECT now()").Scan(&raw); err != nil {
		return time.Time{}, fmt.Errorf("scheduler: read db now: %w", err)
	}
	t := parseDBTime(raw)
	if t.IsZero() {
		return time.Time{}, fmt.Errorf("scheduler: read db now: unparseable %q", raw)
	}
	return t, nil
}

// claim is the result of trying to acquire a plan. Only one of the
// boolean outcomes is true at a time.
type claim struct {
	ID         uuid.UUID
	LeaseToken uuid.UUID
	Attempt    int
	Won        bool // fresh insert
	Stole      bool // stale-steal or FAILED retry
	Conflicted bool // another runner already owns this plan, or attempts exhausted
}

// tryClaim attempts to acquire the lease for (job, scope, plan_time).
func tryClaim(
	ctx context.Context,
	pool *sql.DB,
	job *JobSpec,
	scope Scope,
	planTime time.Time,
	dbTime time.Time,
	runnerID string,
) (claim, error) {
	insertSQL := `
		INSERT INTO sys_cron_executions (
			job_name, scope_kind, scope_id, plan_time,
			status, attempt, max_attempts,
			runner_id, lease_token,
			heartbeat_at, stale_after,
			started_at, updated_at,
			id
		) VALUES (
			?, ?, ?, ?,
			'RUNNING', 1, ?,
			?, gen_random_uuid(),
			?, strftime('%Y-%m-%dT%H:%M:%fZ', ?, '+' || ? || ' seconds'),
			?, ?,
			COALESCE(NULLIF(?, ''), gen_random_uuid())
		)
		ON CONFLICT (job_name, scope_kind, scope_id, plan_time) DO NOTHING
		RETURNING id, lease_token, attempt
	`
	staleSecs := int64(job.StaleTimeout / time.Second)
	if staleSecs <= 0 {
		staleSecs = 1
	}
	dbText := formatDBTime(dbTime)

	var c claim
	var idText, leaseText string
	err := pool.QueryRowContext(ctx, insertSQL,
		job.Name, scope.Kind, scope.ID, formatDBTime(planTime),
		job.MaxAttempts,
		runnerID,
		dbText, dbText, staleSecs,
		dbText, dbText,
		dbid.NewV7(),
	).Scan(&idText, &leaseText, &c.Attempt)
	if err == nil {
		c.ID, err = parseDBID(idText)
		if err != nil {
			return claim{}, fmt.Errorf("scheduler: claim insert id: %w", err)
		}
		c.LeaseToken, err = parseDBID(leaseText)
		if err != nil {
			return claim{}, fmt.Errorf("scheduler: claim insert lease: %w", err)
		}
		c.Won = true
		return c, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return claim{}, fmt.Errorf("scheduler: claim insert: %w", err)
	}

	stealSQL := `
		UPDATE sys_cron_executions
		   SET status        = 'RUNNING',
		       attempt       = attempt + 1,
		       runner_id     = ?,
		       lease_token   = gen_random_uuid(),
		       heartbeat_at  = ?,
		       stale_after   = strftime('%Y-%m-%dT%H:%M:%fZ', ?, '+' || ? || ' seconds'),
		       started_at    = ?,
		       finished_at   = NULL,
		       duration_ms   = NULL,
		       next_retry_at = NULL,
		       error_code    = NULL,
		       error_msg     = NULL,
		       updated_at    = ?
		 WHERE job_name   = ?
		   AND scope_kind = ?
		   AND scope_id   = ?
		   AND plan_time  = ?
		   AND attempt < max_attempts
		   AND (
		        (status = 'FAILED' AND unixepoch(COALESCE(next_retry_at, ?), 'subsec') <= unixepoch(?, 'subsec'))
		        OR
		        (status = 'RUNNING' AND unixepoch(stale_after, 'subsec') < unixepoch(?, 'subsec') AND ?)
		   )
		RETURNING id, lease_token, attempt
	`
	staleFlag := 0
	if job.AllowStaleReentry {
		staleFlag = 1
	}
	err = pool.QueryRowContext(ctx, stealSQL,
		runnerID,
		dbText, dbText, staleSecs, dbText, dbText,
		job.Name, scope.Kind, scope.ID, formatDBTime(planTime),
		dbText, dbText, dbText, staleFlag,
	).Scan(&idText, &leaseText, &c.Attempt)
	if err == nil {
		c.ID, err = parseDBID(idText)
		if err != nil {
			return claim{}, fmt.Errorf("scheduler: claim steal id: %w", err)
		}
		c.LeaseToken, err = parseDBID(leaseText)
		if err != nil {
			return claim{}, fmt.Errorf("scheduler: claim steal lease: %w", err)
		}
		c.Stole = true
		return c, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return claim{}, fmt.Errorf("scheduler: claim steal: %w", err)
	}

	c.Conflicted = true
	return c, nil
}

func markStaleAsFailed(
	ctx context.Context,
	pool *sql.DB,
	jobName string,
	dbTime time.Time,
) (int64, error) {
	tag, err := pool.ExecContext(ctx, `
		UPDATE sys_cron_executions
		   SET status      = 'FAILED',
		       finished_at = ?,
		       error_code  = 'stale_timeout',
		       error_msg   = 'lease expired without heartbeat',
		       updated_at  = ?
		 WHERE job_name    = ?
		   AND status      = 'RUNNING'
		   AND unixepoch(stale_after, 'subsec') < unixepoch(?, 'subsec')
	`, formatDBTime(dbTime), formatDBTime(dbTime), jobName, formatDBTime(dbTime))
	if err != nil {
		return 0, fmt.Errorf("scheduler: mark stale failed: %w", err)
	}
	return tag.RowsAffected()
}

func heartbeat(
	ctx context.Context,
	pool *sql.DB,
	id, leaseToken uuid.UUID,
	staleTimeout time.Duration,
) error {
	staleSecs := int64(staleTimeout / time.Second)
	if staleSecs <= 0 {
		staleSecs = 1
	}
	tag, err := pool.ExecContext(ctx, `
		UPDATE sys_cron_executions
		   SET heartbeat_at = now(),
		       stale_after  = strftime('%Y-%m-%dT%H:%M:%fZ', now(), '+' || ? || ' seconds'),
		       updated_at   = now()
		 WHERE id          = ?
		   AND lease_token = ?
		   AND status      = 'RUNNING'
	`, staleSecs, id.String(), leaseToken.String())
	if err != nil {
		return fmt.Errorf("scheduler: heartbeat: %w", err)
	}
	n, err := tag.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrLeaseLost
	}
	return nil
}

func finishSuccess(
	ctx context.Context,
	pool *sql.DB,
	id, leaseToken uuid.UUID,
	dbTime time.Time,
	durationMs int64,
	res HandlerResult,
) error {
	resultJSON, err := encodeResult(res.Result)
	if err != nil {
		return err
	}
	when := formatDBTime(dbTime)
	tag, err := pool.ExecContext(ctx, `
		UPDATE sys_cron_executions
		   SET status        = 'SUCCESS',
		       finished_at   = ?,
		       duration_ms   = ?,
		       rows_affected = ?,
		       result        = ?,
		       error_code    = NULL,
		       error_msg     = NULL,
		       updated_at    = ?
		 WHERE id            = ?
		   AND lease_token   = ?
		   AND status        = 'RUNNING'
	`, when, durationMs, res.RowsAffected, resultJSON, when, id.String(), leaseToken.String())
	if err != nil {
		return fmt.Errorf("scheduler: finish success: %w", err)
	}
	n, err := tag.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return errTerminalIgnored
	}
	return nil
}

func finishFailure(
	ctx context.Context,
	pool *sql.DB,
	id, leaseToken uuid.UUID,
	dbTime time.Time,
	durationMs int64,
	errorCode, errorMsg string,
	nextRetryAt time.Time,
) error {
	var nextRetry sql.NullString
	if !nextRetryAt.IsZero() {
		nextRetry = sql.NullString{String: formatDBTime(nextRetryAt), Valid: true}
	}
	if errorCode == "" {
		errorCode = "handler_error"
	}
	if len(errorMsg) > 4000 {
		errorMsg = errorMsg[:4000]
	}
	when := formatDBTime(dbTime)
	tag, err := pool.ExecContext(ctx, `
		UPDATE sys_cron_executions
		   SET status        = 'FAILED',
		       finished_at   = ?,
		       duration_ms   = ?,
		       next_retry_at = ?,
		       error_code    = ?,
		       error_msg     = ?,
		       updated_at    = ?
		 WHERE id            = ?
		   AND lease_token   = ?
		   AND status        = 'RUNNING'
	`, when, durationMs, nextRetry, errorCode, errorMsg, when, id.String(), leaseToken.String())
	if err != nil {
		return fmt.Errorf("scheduler: finish failure: %w", err)
	}
	n, err := tag.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return errTerminalIgnored
	}
	return nil
}

func encodeResult(in map[string]any) (string, error) {
	if len(in) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(in)
	if err != nil {
		return "", fmt.Errorf("scheduler: marshal result: %w", err)
	}
	if len(b) > 16*1024 {
		return "", fmt.Errorf("scheduler: result payload too large (%d bytes); keep it small or use logs", len(b))
	}
	return string(b), nil
}

type LatestPlanInfo struct {
	Found       bool
	PlanTime    time.Time
	Status      string
	Attempt     int
	MaxAttempts int
	NextRetryAt time.Time
}

func (i LatestPlanInfo) RetryEligible(now time.Time) bool {
	if !i.Found {
		return false
	}
	if i.Status != "FAILED" {
		return false
	}
	if i.Attempt >= i.MaxAttempts {
		return false
	}
	if i.NextRetryAt.IsZero() {
		return true
	}
	return !i.NextRetryAt.After(now)
}

func latestPlan(
	ctx context.Context,
	pool *sql.DB,
	jobName string,
	scope Scope,
) (LatestPlanInfo, error) {
	var info LatestPlanInfo
	var planText string
	var nextRetry sql.NullString
	err := pool.QueryRowContext(ctx, `
		SELECT plan_time, status, attempt, max_attempts, next_retry_at
		  FROM sys_cron_executions
		 WHERE job_name   = ?
		   AND scope_kind = ?
		   AND scope_id   = ?
		 ORDER BY plan_time DESC
		 LIMIT 1
	`, jobName, scope.Kind, scope.ID).Scan(
		&planText, &info.Status,
		&info.Attempt, &info.MaxAttempts,
		&nextRetry,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return info, nil
		}
		return info, fmt.Errorf("scheduler: read latest plan: %w", err)
	}
	info.Found = true
	info.PlanTime = parseDBTime(planText)
	if nextRetry.Valid {
		info.NextRetryAt = parseDBTime(nextRetry.String)
	}
	return info, nil
}
