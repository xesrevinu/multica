package scheduler

import "testing"

func TestRollupTaskUsageHourlyDoesNotLeakAdvisoryLockOnCancel(t *testing.T) {
	t.Skip("rollup_task_usage_hourly PL/pgSQL function and pg_locks are Postgres-only")
}
