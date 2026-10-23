package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// dashboardFixtureTZ is the zone the day-boundary fixtures in this file are
// built in, and the zone their requests pin with `?tz=`. Both sides read this
// one constant because they have to agree: every `days=N` endpoint opens its
// window at start-of-day in the VIEWER's zone (resolveViewingTZ: `?tz=`, else
// the user's stored user.timezone, else UTC), so a fixture anchored in one
// zone and a window opened in another sit an offset apart. Without the
// `?tz=` these requests would inherit whatever user.timezone holds — NULL in
// the handler fixture, hence UTC.
//
// The zone is deliberately EAST of UTC, and that is what makes the pin
// load-bearing rather than decorative. A fixture built in UTC survives losing
// its `?tz=`: the fallback is UTC too, so the window opens where the fixture
// already sits and nothing notices. Built in Asia/Tokyo the run finishes at
// 15:10 UTC the previous day, so a request that falls back to UTC opens its
// window after the run ended and the assertions go red — which is the whole
// point of pinning it. Verified by mutation: dropping the parameter, and
// pinning it to UTC, both fail.
const dashboardFixtureTZ = "Asia/Tokyo"

// dashboardFixtureTZParam is dashboardFixtureTZ as a query fragment, so a
// request cannot pin a zone the fixture was not built in.
const dashboardFixtureTZParam = "tz=" + dashboardFixtureTZ

// dashboardFixtureLoc resolves dashboardFixtureTZ.
func dashboardFixtureLoc(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(dashboardFixtureTZ)
	if err != nil {
		t.Fatalf("load fixture timezone %q: %v", dashboardFixtureTZ, err)
	}
	return loc
}

// runFinishedToday returns the started_at / completed_at of a ten-minute run
// placed in the first ten minutes of the day `now` falls in, read in loc.
//
// Every endpoint these fixtures exercise filters on `completed_at >= @since`
// — the END of the run, not its start — and for the per-agent rollups @since
// is start of today in the viewer's zone (parseExactSinceParamInTZ at
// days=1). Timestamps built as `now - 30m` therefore fall out of that window
// for the first twenty minutes after midnight: the run finished yesterday,
// the window opens today, and the assertions read the empty result as nothing
// having happened. A handler job that ran at 00:13 UTC failed on exactly
// that; the next one at 00:25 passed with nothing changed but the clock. The
// date-bucketed halves ride out those twenty minutes on the extra day
// parseSinceParamInTZ hands them, which is why only their per-agent siblings
// went red. Anchoring the run on the boundary itself takes the time of day
// out of the fixture rather than special-casing the twenty minutes it bites.
//
// Start-of-day is built the way sinceFromDays builds the cutoff, so fixture
// and window agree instant for instant. In the first ten minutes after
// midnight the run ends slightly in the future, which none of these queries
// mind: they bound completed_at from below only, and the token rollup keys
// off task_usage.created_at rather than the queue row.
func runFinishedToday(now time.Time, loc *time.Location) (started, completed time.Time) {
	local := now.In(loc)
	started = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	return started, started.Add(10 * time.Minute)
}

// pinDayWindowClock freezes the clock the request-side cutoff reads and hands
// back the same instant for the fixture, so both sides describe one moment.
//
// They read the wall clock at two different points otherwise — the fixture when
// it is built, the cutoff when the handler runs, with inserts and a rollup in
// between — and a suite that crosses midnight in that gap writes its run into
// one day and then asks for the next day's window. The gap is milliseconds, so
// it is rare and permanent: nothing about the assertions says which day they
// meant.
func pinDayWindowClock(t *testing.T, at time.Time) time.Time {
	t.Helper()
	prev := dayWindowNow
	dayWindowNow = func() time.Time { return at }
	t.Cleanup(func() { dayWindowNow = prev })
	return at
}

// TestDashboardFixtureRunLandsInsideTheWindow pins what runFinishedToday
// promises the two DB fixtures that call it: at any hour, in any zone, the
// run it places is inside the days=1 window the endpoints open. The window is
// taken from sinceFromDays — the production cutoff itself — rather than from
// a second copy of the helper's arithmetic.
//
// The midnight rows are the point of the table. A fixture built off the wall
// clock is outside that window for the first twenty minutes of the day and
// inside it for the other 23h40m, so only a synthetic clock can hold it to
// account: the suite would otherwise have to run at midnight to see the
// failure, which is how this reached CI in the first place.
func TestDashboardFixtureRunLandsInsideTheWindow(t *testing.T) {
	// Half-hour and negative offsets, so a helper that anchored on UTC
	// midnight while the request asked for another zone cannot pass.
	for _, tz := range []string{dashboardFixtureTZ, "Asia/Kolkata", "America/Los_Angeles"} {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			t.Fatalf("load %s: %v", tz, err)
		}
		for _, clock := range []string{
			"2026-03-01 00:00:00",
			"2026-03-01 00:05:00",
			"2026-03-01 00:19:59",
			"2026-03-01 12:00:00",
			"2026-03-01 23:59:59",
		} {
			t.Run(tz+" "+clock, func(t *testing.T) {
				now, err := time.ParseInLocation("2006-01-02 15:04:05", clock, loc)
				if err != nil {
					t.Fatalf("parse %s: %v", clock, err)
				}
				started, completed := runFinishedToday(now, loc)

				// parseExactSinceParamInTZ trims a day off sinceFromDays, so
				// the days=1 cutoff these endpoints use is start of today.
				since := sinceFromDays(now, 0, loc)
				tomorrow := since.AddDate(0, 0, 1)

				if got := completed.Sub(started); got != 10*time.Minute {
					t.Errorf("run lasted %s, want 10m — the >=600s the run-time assertion reads", got)
				}
				if completed.Before(since) {
					t.Errorf("completed_at %s precedes the days=1 cutoff %s: `completed_at >= @since` drops the fixture", completed, since)
				}
				if !completed.Before(tomorrow) {
					t.Errorf("completed_at %s is past the day that opened at %s", completed, since)
				}
				if started.Before(since) {
					t.Errorf("started_at %s precedes the days=1 cutoff %s", started, since)
				}
			})
		}
	}
}

// TestDashboardEndpoints covers the workspace-dashboard rollups:
//   - daily token usage with and without project filter
//   - per-agent token usage with and without project filter
//   - per-agent run time
//
// Asserts that (1) tasks belonging to a project show up under the workspace
// view, (2) the project filter excludes tasks tied to issues without a
// matching project_id, and (3) run-time aggregation accumulates the
// completed_at − started_at delta correctly.

// TestDashboardAgentRunTimeReportsMeteredTaskCoverage proves the runtime
// rollup distinguishes terminal runs from the subset that reported usage.
func TestDashboardAgentRunTimeReportsMeteredTaskCoverage(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	var agentID, runtimeID string
	dbfx.QueryRow(t, `
		SELECT id, runtime_id FROM agent
		WHERE workspace_id = $1 AND runtime_id IS NOT NULL
		LIMIT 1
	`, testWorkspaceID).Scan(&agentID, &runtimeID)

	readCoverage := func() (int32, int32) {
		rows, err := testHandler.Queries.ListDashboardAgentRunTime(context.Background(), db.ListDashboardAgentRunTimeParams{
			WorkspaceID: parseUUID(testWorkspaceID),
			Since:       nullString(time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339Nano)),
		})
		if err != nil {
			t.Fatalf("list dashboard agent runtime: %v", err)
		}
		for _, row := range rows {
			if uuidToString(row.AgentID) == agentID {
				return int32(row.TaskCount), int32(row.MeteredTaskCount)
			}
		}
		return 0, 0
	}
	baseTasks, baseMetered := readCoverage()

	issueID := dbfx.Issue(t, "dashboard usage coverage")
	finished := testutil.Cols{
		"issue_id":     issueID,
		"runtime_id":   runtimeID,
		"started_at":   testutil.Raw("now() - interval '2 minutes'"),
		"completed_at": testutil.Raw("now() - interval '1 minute'"),
	}
	meteredTaskID := dbfx.Task(t, agentID, finished, testutil.Cols{"status": "completed"})
	dbfx.Task(t, agentID, finished, testutil.Cols{"status": "failed"})
	dbfx.Insert(t, "task_usage", testutil.Cols{
		"task_id":       meteredTaskID,
		"provider":      "qoderclicn",
		"model":         "bailian/tp/qwen3.8-max",
		"input_tokens":  120,
		"output_tokens": 30,
	})

	tasks, metered := readCoverage()
	if tasks-baseTasks != 2 || metered-baseMetered != 1 {
		t.Fatalf("coverage delta = terminal:%d metered:%d, want 2/1", tasks-baseTasks, metered-baseMetered)
	}
}

// TestDashboardUsageDailyBucketsByViewerTimezone proves the `?tz=` query
// param drives the calendar-day boundary: the same UTC instant lands under
// a different `date` for a UTC viewer vs an America/Los_Angeles viewer.
// This is the core promise of the timezone-architecture RFC.

// TestDashboardRunTimeDailyBucketsByViewerTimezone proves the `?tz=` query
// param drives the calendar-day boundary of the Time / Tasks dashboard tab:
// GetDashboardRunTimeDaily applies `@tz` to `completed_at AT TIME ZONE @tz`
// on agent_task_queue. A task completed at 04:00 UTC is still the previous
// evening in America/Los_Angeles (UTC-7/-8), so the LA viewer must see the
// row under the prior calendar date relative to a UTC viewer.

// TestDashboardRunTimeCountsCancelledRuns pins the fix for the run-time
// rollups dropping every run the user stopped mid-flight. CancelAgentTask
// accepts a 'running' task, so a cancelled row can carry both started_at and
// completed_at — real agent occupancy, and real tokens the cost rollup
// charges for regardless of status. The old `status IN ('completed','failed')`
// filter zeroed that time, so Time/Tasks and Cost/Tokens summed different
// task populations on the same dashboard.
//
// Also asserts the other half of the contract: a run cancelled while still
// queued (started_at NULL) must stay out, since it never occupied an agent.

// readAgentRunTime returns (total_seconds, task_count, cancelled_count) for
// one agent from GetDashboardAgentRunTime. Zeroes when the agent has no row.
func readAgentRunTime(t *testing.T, agentID string) (int64, int32, int32) {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.GetDashboardAgentRunTime(w, newRequest("GET", "/api/dashboard/agent-runtime?days=10&tz=UTC", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var rows []struct {
		AgentID        string `json:"agent_id"`
		TotalSeconds   int64  `json:"total_seconds"`
		TaskCount      int32  `json:"task_count"`
		CancelledCount int32  `json:"cancelled_count"`
	}
	if err := json.NewDecoder(w.Body).Decode(&rows); err != nil {
		t.Fatalf("decode agent run time: %v", err)
	}
	for _, r := range rows {
		if r.AgentID == agentID {
			return r.TotalSeconds, r.TaskCount, r.CancelledCount
		}
	}
	return 0, 0, 0
}

// TestRollupTaskUsageHourlyIdempotentAndWatermark covers two pipeline
// invariants the deleted runtime_rollup_test.go used to guard for the
// legacy daily rollup: (1) re-running the window function over the same
// range produces identical totals, and (2) the cron entry point advances
// the rollup-state watermark and clears last_error.

// TestRollupTaskUsageHourlyReassignBetweenRuntimes ports the invalidation
// coverage the deleted runtime_rollup_test.go held for the legacy daily
// rollup. Reassigning a task between runtimes (the runtime-merge path) must
// move its usage: the `trg_atq_dirty_hourly` trigger enqueues both the old
// and new runtime buckets, and the next window run drains the queue,
// empties the old bucket, and fills the new one. Without this the rollup
// keeps attributing usage to the merged-away runtime.

// TestRollupTaskUsageHourlyWorkspaceMismatch constructs an atq row whose
// agent.workspace_id differs from issue.workspace_id and verifies the
// hourly rollup resolves workspace_id consistently from `agent` across the
// trigger, dirty_from_updates, and the recompute join. If any path leaked
// back to issue.workspace_id the dirty key would miss the recompute join
// and the bucket would be dropped or mis-attributed across tenants. The
// schema does not enforce the two workspace_ids match, so this canary
// keeps the alignment honest.

// TestDashboardRollupReattributesOnProjectChange verifies the trigger that
// fires on `UPDATE issue SET project_id` enqueues both old + new project
// buckets so the next rollup tick re-attributes the affected tokens.
// Uses the rollup window function directly to drain the dirty queue,
// then asserts the rollup table reflects the new project_id.

// TestDashboardRollupClearsOnIssueDelete verifies that deleting an issue
// (which cascades to its tasks and task_usage rows) also clears the
// dashboard rollup row attributed to that issue's project. The
// `issue BEFORE DELETE` trigger has to fire ahead of the cascade so the
// dirty queue captures the original project_id while the issue row is
// still readable.

// TestDashboardRollupReattributesOnLinkTaskToIssue verifies that
// `LinkTaskToIssue` (which UPDATEs `agent_task_queue.issue_id` from NULL
// to a real issue id) re-attributes existing rollup rows from the
// no-project bucket to the linked issue's project bucket. Mirrors the
// quick-create flow in `service.task.LinkTaskToIssue`.

// TestPruneTaskUsageHourlyDirty covers the dirty-queue TTL. Both the RFC
// (§7.1) and the rollup-pipeline migration call this THE most-easily-missed correctness
// requirement of the hourly pipeline: without the prune, a row that escapes
// the per-tick drain (crash mid-tick, worker paused during an incident)
// pins its bucket's recompute forever and the queue grows unbounded.

// TestRollupTaskUsageHourlyCapsWindowAtOneDay covers the catch-up cap
// in rollup_task_usage_hourly(): when the watermark has
// fallen far behind (worker paused for an incident or a migration freeze),
// a single tick must advance it by at most one day, so a multi-week backlog
// drains in bounded steps instead of one giant statement holding advisory
// lock 4246. The existing watermark test only parks the watermark one hour
// back, so the cap itself is never exercised there.

// TestDashboardUsageDailyCrossMidnightFullPipeline runs the WHOLE timezone
// pipeline end to end: insert a raw `task_usage` row near UTC midnight →
// run `rollup_task_usage_hourly_window` to bucket it → call
// GetDashboardUsageDaily with a non-UTC viewer tz. It asserts the tokens
// land on the viewer's correct calendar day and NOT on the UTC day.
//
// This is the #2822 bug class the RFC exists to prevent. The existing
// TestDashboardUsageDailyBucketsByViewerTimezone seeds a pre-built
// task_usage_hourly row and only exercises the SQL read path; here the
// row travels from raw task_usage through the rollup, so a regression in
// task_usage_hour_bucket or the recompute join is also caught.

// TestRollupTaskUsageHourlyConvergesOnTaskUsageDelete covers the
// `trg_tu_dirty_hourly` trigger — a BEFORE DELETE trigger on task_usage.
// Migration 102 notes it has no production callers today and exists purely
// as defensive convergence guard, so a single minimal test is enough:
// seed a task_usage row, roll it up, DELETE the task_usage row directly,
// roll up again, and assert the hourly bucket is recomputed down to zero.
// Without the trigger the deleted row's bucket would never be re-enqueued.

// TestDashboardFailuresCountNeverStartedTasks pins the reason the failure
// rollups exist as their own queries rather than reusing the run-time ones:
// ListDashboardRunTimeDaily / ListDashboardAgentRunTime require
// `started_at IS NOT NULL`, so a task that expired in the queue — the exact
// signature of a runtime outage — contributes nothing to their failed_count.
// The failure endpoints must count it, and must report the succeeded tasks in
// the same payload so the client's error rate has a matching denominator.

// TestDashboardFailuresByAgentUsesExactWindow pins the cutoff difference
// between the two failure endpoints.
//
// parseSinceParamInTZ deliberately returns N+1 calendar days of headroom, and
// the workspace dashboard trims the surplus client-side with `-(days-1)`. The
// by-agent rollup carries no date column, so it cannot be trimmed that way —
// it must close its own window server-side. Before that fix, `days=1` served
// the Errors card yesterday's failures while the chart beside it, correctly
// trimmed, showed none.

// TestDashboardPerAgentRollupsUseExactWindow is MUL-5551: the Usage page's
// leaderboard reported MORE tokens for a single agent than the Tokens KPI
// reported for the entire workspace.
//
// Both halves read the same underlying rows; they disagreed only on the
// window. usage/daily and runtime/daily are date-bucketed, so the client
// trims `parseSinceParamInTZ`'s extra calendar day back to `-(days-1)` before
// computing the KPIs and the chart. usage/by-agent and agent-runtime carry no
// date, so nothing trimmed them and they kept the whole N+1 span — at days=1
// that is today PLUS yesterday. One busy agent's two-day total then trivially
// exceeded the workspace's one-day total.
//
// Sibling of TestDashboardFailuresByAgentUsesExactWindow, which pinned the
// same cutoff for the third date-less rollup. All three now close their own
// window server-side.

// TestDashboardFailureWireContractKeepsEmptyReason pins the success bucket's
// wire form. The client's zod schema defaults a missing `failure_reason` to
// "" — the succeeded bucket — which is only safe while the server always
// emits the field. Adding `omitempty` to the struct tag would strip it from
// exactly the success rows and silently turn every window into a 100% error
// rate, so that regression is caught here rather than in a dashboard.
func TestDashboardFailureWireContractKeepsEmptyReason(t *testing.T) {
	// Each case decodes into its OWN map. json.Unmarshal merges into a
	// non-nil map rather than resetting it, so sharing one across cases would
	// leave the first payload's failure_reason in place and let a later
	// omitempty regression pass unnoticed — the exact failure this test
	// exists to catch.
	for _, tc := range []struct {
		name string
		row  any
	}{
		{"daily", DashboardFailureDailyResponse{Date: "2026-05-19", TaskCount: 3}},
		{"by-agent", DashboardFailureByAgentResponse{AgentID: "a", TaskCount: 3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(tc.row)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			decoded := map[string]any{}
			if err := json.Unmarshal(body, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if _, present := decoded["failure_reason"]; !present {
				t.Errorf("succeeded rows must serialize an explicit empty failure_reason, got %s", body)
			}
		})
	}
}

// dashboardRunTimeSeconds inserts one finished run for the fixture agent and
// returns what agent-runtime reports for it under the given query string. The
// caller owns the clock: whatever pinDayWindowClock was given is what both the
// fixture and the handler's cutoff read.
func dashboardRunTimeSeconds(t *testing.T, at time.Time, loc *time.Location, query string) int64 {
	t.Helper()
	ctx := context.Background()

	var runtimeID, agentID string
	dbfx.QueryRow(t, `SELECT id FROM agent_runtime WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&runtimeID)
	dbfx.QueryRow(t, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID)
	var issueID string
	dbfx.QueryRow(t, `
		INSERT INTO issue (workspace_id, title, creator_id, creator_type, number)
		VALUES ($1, 'clock fixture', $2, 'member',
			(SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID)
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	started, completed := runFinishedToday(at, loc)
	dbfx.Task(t, agentID, testutil.Cols{
		"issue_id":     issueID,
		"runtime_id":   runtimeID,
		"status":       "completed",
		"started_at":   started,
		"completed_at": completed,
		"created_at":   started,
	})

	w := httptest.NewRecorder()
	testHandler.GetDashboardAgentRunTime(w, newRequest("GET", "/api/dashboard/agent-runtime?"+query, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("agent-runtime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var rows []struct {
		AgentID      string `json:"agent_id"`
		TotalSeconds int64  `json:"total_seconds"`
	}
	_ = json.NewDecoder(w.Body).Decode(&rows)
	var seconds int64
	for _, r := range rows {
		if r.AgentID == agentID {
			seconds += r.TotalSeconds
		}
	}
	return seconds
}

// A fixture built a hair before midnight is still counted by a request handled
// a hair after it.
//
// The two sides used to read the wall clock at different moments — the fixture
// when it was built, the cutoff when the handler ran, with inserts and a rollup
// in between. Crossing midnight in that gap wrote the run into one day and then
// asked for the next day's window, and the row vanished. Pinning one instant is
// what removes the question; this asserts the removal at the instant where it
// used to bite, rather than trusting that the suite never runs at 23:59.

// The timezone pin is load-bearing, at an instant that proves it.
//
// A request that loses its `?tz=` falls back to the fixture user's stored zone
// — UTC — while the fixture stays in dashboardFixtureTZ, and the two windows
// then sit an offset apart. Whether that offset actually hides the run depends
// on the time of day, so asserting it against the wall clock proves nothing for
// most of the day. Pinned at 02:00 UTC on a fixed date, Tokyo's day began at
// 15:00 UTC the day before and UTC's began two hours ago: the run is behind the
// UTC cutoff and must not be counted.
