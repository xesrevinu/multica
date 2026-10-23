package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

)

// Regression tests for MUL-5492: the per-issue timeline cap used to be applied
// with ORDER BY created_at ASC, so once an issue accumulated more than
// timelineHardCap rows the cap discarded the NEWEST ones and the timeline
// silently appeared to stop at some point in the past.
//
// These tests pin four properties:
//  1. the cap keeps the newest window, not the oldest;
//  2. each list is capped independently — activity truncation must never delete
//     comments, so the range may legitimately hold comments older than the
//     oldest activity;
//  3. every retained reply arrives with a parent chain that reaches a root in
//     the same response, because an orphaned reply is invisible in the UI;
//  4. truncation is reported instead of being silent, naming which kinds were
//     affected.

// fetchTimelineRecorder issues GET /timeline and returns the raw recorder so
// tests can assert on response headers as well as the body.
func fetchTimelineRecorder(t *testing.T, issueID, rawQuery string) *httptest.ResponseRecorder {
	t.Helper()
	target := "/api/issues/" + issueID + "/timeline"
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	w := httptest.NewRecorder()
	req := newRequest("GET", target, nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListTimeline(w, req)
	return w
}

func decodeTimelineEntries(t *testing.T, w *httptest.ResponseRecorder) []TimelineEntry {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var entries []TimelineEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode timeline: %v", err)
	}
	return entries
}

// bulkSeedActivities inserts n activities one second apart starting at start.
// One statement, because cap scenarios need more rows than a window holds.
func bulkSeedActivities(t *testing.T, issueID string, start time.Time, n int) {
	t.Helper()
	_, err := testPool.Exec(context.Background(), `
		INSERT INTO activity_log (workspace_id, issue_id, actor_type, actor_id, action, details, created_at)
		SELECT $1, $2, 'member', $3, 'status_changed', '{"from":"todo","to":"in_progress"}'::jsonb,
		       $4::timestamptz + (g * interval '1 second')
		FROM generate_series(0, $5::int - 1) AS g
	`, testWorkspaceID, issueID, testUserID, start, n)
	if err != nil {
		t.Fatalf("bulk seed %d activities: %v", n, err)
	}
}

// bulkSeedComments inserts n comments one second apart starting at start.
func bulkSeedComments(t *testing.T, issueID string, start time.Time, n int) {
	t.Helper()
	_, err := testPool.Exec(context.Background(), `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at, updated_at)
		SELECT $1, $2, 'member', $3, 'bulk comment ' || g, 'comment',
		       $4::timestamptz + (g * interval '1 second'),
		       $4::timestamptz + (g * interval '1 second')
		FROM generate_series(0, $5::int - 1) AS g
	`, issueID, testWorkspaceID, testUserID, start, n)
	if err != nil {
		t.Fatalf("bulk seed %d comments: %v", n, err)
	}
}

// bulkSeedReplies inserts n direct replies to parentID one second apart.
func bulkSeedReplies(t *testing.T, issueID, parentID string, start time.Time, n int) {
	t.Helper()
	_, err := testPool.Exec(context.Background(), `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type,
		                     created_at, updated_at, parent_id)
		SELECT $1, $2, 'member', $3, 'bulk reply ' || g, 'comment',
		       $4::timestamptz + (g * interval '1 second'),
		       $4::timestamptz + (g * interval '1 second'),
		       $5
		FROM generate_series(0, $6::int - 1) AS g
	`, issueID, testWorkspaceID, testUserID, start, parentID, n)
	if err != nil {
		t.Fatalf("bulk seed %d replies: %v", n, err)
	}
}

// shrinkListCapsForTest runs one cap scenario at a small size. The production
// values (2000-row windows, a 2000-row completion budget, 64 levels) only size
// the safety net: windowing, the probe row and thread completion behave the
// same at any size, while seeding and deleting thousands of rows per scenario
// made the cap tests the slowest in the package. Window-sized counts in these
// tests are derived from the variables; the few fixed ones (a handful of old
// replies, a +50 overflow) stay well inside the shrunk budgets. The timeline and
// comment caps stay equal, as they are in production.
func shrinkListCapsForTest(t *testing.T) {
	t.Helper()
	comments, timeline, budget, depth := commentHardCap, timelineHardCap, commentThreadContextBudget, commentThreadMaxDepth
	commentHardCap, timelineHardCap, commentThreadContextBudget, commentThreadMaxDepth = 20, 20, 20, 8
	t.Cleanup(func() {
		commentHardCap, timelineHardCap, commentThreadContextBudget, commentThreadMaxDepth = comments, timeline, budget, depth
	})
}

// seedComment inserts a single comment at an exact timestamp, optionally as a
// reply, and returns its id.
func seedComment(t *testing.T, issueID string, at time.Time, content string, parentID *string) string {
	t.Helper()
	var id string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at, updated_at, parent_id)
		VALUES ($1, $2, 'member', $3, $4, 'comment', $5, $5, $6)
		RETURNING id
	`, issueID, testWorkspaceID, testUserID, content, at, parentID).Scan(&id)
	if err != nil {
		t.Fatalf("seed comment %q: %v", content, err)
	}
	return id
}

func countByType(entries []TimelineEntry) (comments, activities int) {
	for _, e := range entries {
		switch e.Type {
		case "comment":
			comments++
		case "activity":
			activities++
		}
	}
	return
}

// mustParseTS parses a timeline timestamp into an instant. Tests compare
// instants, never strings: the API renders timestamps in the DB session's
// offset, so two equal instants can have different textual forms and a string
// comparison would silently pass for the wrong reason.
func mustParseTS(t *testing.T, label, raw string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatalf("parse %s timestamp %q: %v", label, raw, err)
	}
	return parsed
}

// TestListTimeline_HardCapKeepsNewestActivities is the direct regression: with
// more activities than the cap, the response must be the newest window. Before
// the fix the LAST seeded activity was missing and the FIRST one was present.

// TestListTimeline_ActivityTruncationDoesNotDropComments replaces an earlier
// test that asserted the opposite. That test pinned a cross-kind clamp: both
// lists trimmed to a shared floor so the window was provably contiguous. The
// clamp was wrong — comments essentially never reach the cap while activity does,
// so the shared floor was almost always the activity floor deleting comments that
// had been fetched successfully. Deleting content to buy a cosmetic property is
// the worse trade, and it is what created orphaned replies.
//
// The window is now allowed to hold comments older than the oldest activity. That
// costs activity density in the older range, which is metadata, and it is
// reported rather than hidden.

// TestListTimeline_NoOrphanedReplies is the regression for the review finding.
// Scenario: a thread root older than the comment window, a fresh reply to it
// inside the window. The reply must not come back without its parent — an orphan
// is invisible in the UI, not merely mis-nested (MUL-1847 / #2263).

// TestListTimeline_RootResolvedThreadIsComplete is the root-resolution variant
// of the partial-thread regression. The timeline UI folds immediately when the
// root has resolved_at, so returning only a backfilled root plus the newest
// reply would make its collapsed count and author list look complete while five
// older replies were missing.

// TestListTimeline_ReplyResolutionOutsideWindowIsRestored covers the opposite
// stale-state failure. When the resolution reply falls outside the newest
// window but a later reply remains, deriveThreadResolution would otherwise see
// no resolution and render an already-resolved thread as unresolved.

// TestListTimeline_OversizedAffectedThreadIsDroppedWhole pins the bounded
// fallback. If one cut thread cannot fit in the shared context budget, returning
// its root plus an arbitrary suffix would let old clients fold partial data.
// The whole affected thread must disappear while truncation remains explicit.

// assertNoOrphanedReplies pins the invariant issue-detail.tsx relies on and
// foldResolvedThreads requires: every returned reply's parent is in the same set.
// More valuable than any single scenario — it holds for every shape of response.
func assertNoOrphanedReplies(t *testing.T, entries []TimelineEntry) {
	t.Helper()
	present := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		present[e.ID] = struct{}{}
	}
	for _, e := range entries {
		if e.Type != "comment" || e.ParentID == nil || *e.ParentID == "" {
			continue
		}
		if _, ok := present[*e.ParentID]; !ok {
			t.Errorf("comment %s references parent %s which is absent from the response", e.ID, *e.ParentID)
		}
	}
}

// TestListTimeline_BackfilledRootKeepsQuickActionID guards the projection of
// newly-added comment columns through parent-chain completion. An earlier
// revision hand-copied SQL rows into db.Comment field by field, which silently
// dropped quick_action_id when main added it — a backfilled quick-action root
// would have rendered as a raw prompt instead of a quick-action card. The query
// now returns db.Comment directly so new columns cannot be lost this way.

// TestListTimeline_ExactlyAtCapIsNotTruncated pins the cap+1 probe read. An
// issue that happens to hold exactly timelineHardCap rows is complete, so
// reporting it as truncated would be a lie and would trigger a needless
// ancestor-backfill query.

// TestListTimeline_WrappedShapeReportsHasMoreBefore checks the legacy wrapped
// response also stops claiming the timeline is complete. has_more_before was
// hardcoded false.

// TestListCommentsForIssue_KeepsNewestWindow covers the same query from the
// comment-list endpoint's side, since ListCommentsForIssue is shared. The
// ascending contract must be preserved while the cap now bites at the old end.
