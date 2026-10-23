package scheduler

import "testing"

func TestPgCronConcurrentNoDoubleWrite(t *testing.T) {
	t.Skip("rollup_task_usage_hourly PL/pgSQL function and pg_cron advisory locks are Postgres-only")
}
