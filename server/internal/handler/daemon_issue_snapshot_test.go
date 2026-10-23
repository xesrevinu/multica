package handler

import (
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// seedPriorRunWithSnapshot creates the terminal prior task this agent's next
// claim will RESUME, carrying the snapshot that run was handed. Passing an
// empty snapshot models a run recorded before the column existed.
//
// The session_id and matching runtime_id are load-bearing, not decoration: a
// claim reports a delta only when it actually hands a resumed session back, and
// that session has to come from THIS run for the delta to describe the context
// the agent will continue from (MUL-7344). A prior run with no resumable
// session is a cold start, and a cold start has no delta to report.
func seedPriorRunWithSnapshot(t *testing.T, agentID, runtimeID, issueID, snapshot string) {
	t.Helper()
	cols := testutil.Cols{
		"runtime_id":   runtimeID,
		"issue_id":     issueID,
		"status":       "completed",
		"session_id":   "prior-run-session-" + issueID,
		"work_dir":     "/tmp/prior-run-workdir",
		"started_at":   testutil.Raw("now() - interval '1 hour'"),
		"completed_at": testutil.Raw("now() - interval '50 minutes'"),
	}
	if snapshot != "" {
		cols["issue_snapshot"] = snapshot
	}
	dbfx.Task(t, agentID, cols)
}

// issueSnapshotJSON renders a stored snapshot for the issue fixture, letting a
// caller override individual fields to model what the previous run saw.
// createClaimReclaimAgentAndIssue always creates an unassigned issue in
// status in_progress with priority none, and titles it "<name> issue".
func issueSnapshotJSON(t *testing.T, version int, title, description string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"v":                  version,
		"title_sha256":       sha256Hex(title),
		"description_sha256": sha256Hex(description),
	})
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	return string(raw)
}

// TestClaimTaskByRuntime_IssueUnchangedReportsEmptyDelta: the previous run saw
// the issue exactly as it is now, so the claim reports "compared, nothing
// changed" — the one answer a daemon may use to skip workflow step 1's read.

// TestClaimTaskByRuntime_IssueChangedNamesFields: the comparison reports which
// of the compared fields moved, in the fixed order, so the daemon can name them
// instead of telling the agent to diff the whole record.

// TestClaimTaskByRuntime_NoPriorSnapshotIsNotUnchanged pins the ambiguity this
// flag exists to remove. A first run on an issue, and a prior run recorded
// before the column existed, both produce an empty changed-field list — the
// same bytes as a genuine "nothing changed". Only the flag separates them, and
// getting it wrong means an agent continues from stale context.
//
// The current status and assignee ship regardless: a run that must read the
// issue anyway loses nothing, and workflow step 3 needs them either way.

// TestClaimTaskByRuntime_ForeignSnapshotVersionIsUnknown: a snapshot written by
// a different shape version compared a different set of fields, so neither
// "unchanged" nor a changed-field list would be a true statement about it.

// TestClaimTaskByRuntime_RecordsItsOwnIssueSnapshot closes the loop: the
// comparison above is only ever true if each claim persists what it saw. This
// is the write half, and it must happen for an ASSIGNMENT claim too — a run
// that records nothing leaves the next one with no baseline.

// TestClaimTaskByRuntime_BothDeltasShareOneAnchor pins that the comment delta
// and the issue-state delta are resolved from the SAME row — the resumed run's
// — so "since your last run" is one fact on a claim rather than two
// independently-resolved ones that could disagree. Both now ride the row
// GetLastTaskSession already returns, so a claim reads no extra anchor at all.

// TestClaimTaskByRuntime_DeltasDateFromTheResumedRun is the regression Niko's
// review found, and it is the invariant the whole mechanism rests on: a claim
// may only report a delta measured from the run whose session it hands back.
//
// "Newest run" and "resumed run" are different rows more often than they look.
// GetLastTaskSession skips poisoned sessions, so a newer run that died on an
// unprocessable conversation is passed over for an older healthy one; a manual
// rerun resumes whatever source the operator clicked. In both cases the newest
// run's snapshot describes an issue the resumed session never saw — so
// comparing against it can report "unchanged" to an agent whose memory predates
// the edit, which is the exact failure this mechanism exists to prevent, and
// the only one with no symptom at runtime.
//
// Both sub-cases put the CURRENT issue in the state the newer run recorded, so
// a claim that anchors on the newest run finds nothing changed and says so.

// TestClaimTaskByRuntime_NeverStartedAnchorRowReportsNoIssueDelta closes the
// gap between "the row whose session we resume" and "the run whose memory we
// resume". GetLastTaskSession returns the session's latest TERMINAL row, and a
// retry child that inherited the session can be claimed — writing its snapshot
// — and then fail before the agent ever ran (runtime offline during prepare).
// Its snapshot describes the issue as of ITS claim, which the session's memory
// never saw. Anchoring on it would report "unchanged" across an edit made after
// the last run that actually executed: the silent failure this mechanism must
// never produce. A never-started anchor row dates neither delta.

// TestClaimTaskByRuntime_NoAdoptedSessionHasNoDeltas pins the other side of the
// resumed-run rule: a delta exists only when this claim actually hands a
// resumed session back. Each mode below leaves the agent with no continued
// context, so there is nothing for a delta to be measured against, and the
// daemon must perform the reads it always has.
//
// Each fixture seeds a prior run WITH a usable snapshot and an unread comment,
// so a claim that reported a delta anyway would have something to report — the
// assertion fails on a real regression, not on an empty fixture.

// TestClaimTaskByRuntime_NonBodyFieldsAreOutOfScope pins the compared set.
// The comparison answers "must the agent re-read the issue BODY?", so status,
// assignee and priority are all outside it: an issue whose ONLY change is one
// of them is reported unchanged, and the prompt names the two fields it did
// check so that report cannot be read as "nothing about this issue moved".
//
// Status matters most here. The workflow has the agent set in_progress and
// in_review on its own runs, so comparing status made the agent's own
// bookkeeping demand a body re-read that nothing in the body justified.
//
// Their CURRENT values still ship on every claim, which is why comparing them
// buys nothing: the agent reads status and assignee straight off the message.

// TestClaimTaskByRuntime_StartedRetryWithoutProviderMustNotWaiveReads closes the
// last window in "which run may date a delta", and it is a step later than the
// never-started case above.
//
// A snapshot is written at CLAIM time. The daemon then writes started_at
// (TaskService.StartTask) and only afterwards launches the provider, so a run
// can carry both a snapshot and a started_at while the prompt never reached the
// session: the daemon exits during prepare and orphan recovery marks the row
// failed. Anchoring there reports "unchanged" across a real edit AND hides
// every comment older than that row's start.
//
// Nothing on the row separates "failed after the provider ran" from "failed
// before it ran" — session_id is inherited by CreateRetryTask at insert, and
// started_at precedes the launch — so only a COMPLETED run may date a delta.
//
// The fixture drives the real path: CreateRetryTask → claim → StartTask →
// RecoverOrphanedTasksForRuntime → follow-up claim (from Niko's review repro).

// TestClaimTaskByRuntime_LegacySnapshotWithStatusStillCompares pins the reason
// dropping status did NOT bump issueSnapshotVersion.
//
// Rows written before the narrowing carry an extra "status" key. Removing a
// field from the compared set is backward compatible: the fields that remain
// are still present in those rows and still mean the same thing, so the
// comparison stays correct and the stale key is simply ignored. Only ADDING a
// field, or changing how a value is derived, leaves an old row unable to answer
// the current question — and only that earns a bump.
//
// Without this test the compatibility claim is an assertion in a comment, and
// the cost of it being wrong is a silent false "unchanged" on every dev row.
