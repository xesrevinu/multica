package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/issuestatus"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Issue status catalog tests (MUL-6243).

// seedTestCatalog makes the shared test workspace's catalog present. The
// fixture creates its workspace with raw SQL, so it has no catalog rows —
// which is itself the unseeded case covered by TestUnseededWorkspaceStillAccepts.
func seedTestCatalog(t *testing.T) {
	t.Helper()
	if err := issuestatus.Ensure(context.Background(), testHandler.Queries, parseUUID(testWorkspaceID)); err != nil {
		t.Fatalf("seed catalog: %v", err)
	}
}

// createTestCustomStatus inserts a custom status directly and removes it after
// the test, so catalog state cannot leak between tests in the shared workspace.
func createTestCustomStatus(t *testing.T, key, category string) db.IssueStatus {
	t.Helper()
	if normalized, ok := issuestatus.ParseCategory(category); ok {
		category = normalized
	}
	seedTestCatalog(t)
	entry, err := testHandler.Queries.CreateIssueStatusEntry(context.Background(), db.CreateIssueStatusEntryParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		Key:         key,
		Name:        key,
		Description: "",
		Category:    category,
		Color:       "#123456",
	})
	if err != nil {
		t.Fatalf("create custom status %q: %v", key, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_status WHERE id = $1`, entry.ID)
	})
	return entry
}

// TestEnsureIsIdempotent covers the rolling-deploy case: two pods can seed the
// same workspace concurrently and the second must be a no-op, not an error.

// TestCatalogOrderMatchesLifecycleGroups pins the concrete status order within
// the four lifecycle groups.


// TestUnseededWorkspaceStillAcceptsBuiltInStatuses is the regression guard for
// the failure mode found while wiring this up: requiring a catalog row to
// validate a status made every issue write fail in a workspace whose seed had
// not landed. The built-ins must resolve with or without a row.

// TestCustomStatusInheritsItsCategoryBehavior is the core promise of the
// model: a custom status behaves as the canonical status it names.

// TestIssueWriteAcceptsCustomStatusAndRejectsUnknown exercises the real HTTP
// write path, which is where the dropped enum CHECK has to be replaced.

// TestBuiltInStatusesAreImmutable pins the product decision that built-in
// statuses cannot be renamed, recolored, or archived, so the default workspace
// is identical for every user who never opens the settings page.

// In-use statuses cannot be archived, including successful and canceled work.

// Historical archived entries remain resolvable even under the new empty-only rule.

// TestCreateIssueStatusValidation covers the reserved-key and category rules.

// TestCreateIssueStatusIsNotGatedOnRollout pins the removal of the rollout gate
// (MUL-6643). Creation used to 403 unless a feature flag was flipped on, because
// the first custom status minted a value older pods could not interpret. Every
// pod resolves categories now, so an unset flag must not be able to take the
// feature away again — creation answers to workspace role alone.

// TestIssueWriteStoresCanonicalStatusKey guards the 500 found in review:
// resolution is case- and whitespace-insensitive, so writing the caller's raw
// string back would store a value the column's format constraint rejects.

// TestCustomTerminalStatusCountsAsTerminalInSQL covers the SQL-side consumers
// the Go resolver cannot reach. Terminal categories are expanded once into
// concrete keys so custom done statuses preserve their behavior without a
// per-row issue_effective_status call.

// archiveStatusVia runs the real archive endpoint and returns its status code.
func archiveStatusVia(t *testing.T, entry db.IssueStatus) int {
	t.Helper()
	rec := httptest.NewRecorder()
	testHandler.ArchiveIssueStatus(rec, withURLParam(
		newRequest(http.MethodDelete, "/api/issue-statuses/"+uuidToString(entry.ID), nil),
		"id", uuidToString(entry.ID)))
	return rec.Code
}

// holdExclusiveCatalogLock takes the archive side of the catalog lock and holds
// it until the returned release func runs, so a test can pin one ordering.
func holdExclusiveCatalogLock(t *testing.T) (holderPID int32, release func()) {
	t.Helper()
	ctx := context.Background()
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lock holder: %v", err)
	}
	if _, err := tx.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1::uuid::text || ':issue_status', 0))`,
		parseUUID(testWorkspaceID)); err != nil {
		t.Fatalf("take exclusive lock: %v", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&holderPID); err != nil {
		t.Fatalf("read lock-holder pid: %v", err)
	}
	released := false
	return holderPID, func() {
		if released {
			return
		}
		released = true
		tx.Rollback()
	}
}

// waitForCatalogLockWaiter observes a contender waiting on the exact advisory
// lock held by holderPID. It replaces fixed sleeps in race tests with the state
// transition those sleeps were trying to approximate.
func waitForCatalogLockWaiter(t *testing.T, ctx context.Context, holderPID int32) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		if err := testPool.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM pg_locks AS held
    JOIN pg_locks AS waiter
      ON waiter.locktype = held.locktype
     AND waiter.database IS NOT DISTINCT FROM held.database
     AND waiter.classid IS NOT DISTINCT FROM held.classid
     AND waiter.objid IS NOT DISTINCT FROM held.objid
     AND waiter.objsubid IS NOT DISTINCT FROM held.objsubid
    WHERE held.pid = $1
      AND held.locktype = 'advisory'
      AND held.granted
      AND NOT waiter.granted
)
`, holderPID).Scan(&waiting); err != nil {
			t.Fatalf("observe issue-status catalog-lock waiter: %v", err)
		}
		if waiting {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("writer never reached the held issue-status catalog lock")
}

// TestWritesRejectAnArchivedStatus covers the SEQUENTIAL case: the status is
// already archived before the request arrives, so the pre-flight validation
// alone is enough to refuse it.
//
// This is deliberately NOT the race test — no archive is interleaved with a
// write here, and these cases would pass even without the in-transaction
// re-resolve. The interleaved counterexample is
// TestArchiveCommitsInsideTheWriteRaceWindow below.

// A committed writer must be counted by the archive's locked precondition.

// TestArchiveCommitsInsideTheWriteRaceWindow is the sharp counterexample, run
// against EVERY write endpoint that can land on a custom status.
//
// It interleaves the two operations so the writer's PRE-FLIGHT validation
// observes an active status and the archive commits before the write lands.
// That is the exact hole the first implementation had: the pre-flight Resolve
// ran outside the transaction and was never rechecked, so the writer sailed
// past the released lock and stored an archived key. Verified to fail without
// the in-transaction re-resolve (the issue is left stranded) and pass with it.
//
// Seed issues for the update/batch cases are created BEFORE the exclusive lock
// is taken, so the only thing racing the archive is the status write itself.

// mustCreateIssue creates an issue through the real endpoint and returns its id.
func mustCreateIssue(t *testing.T, title, status string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	testHandler.CreateIssue(rec, newRequest(http.MethodPost, "/api/issues", map[string]any{
		"title": title, "status": status,
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed issue %q: %d %s", title, rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &created)
	id := parseUUID(created.ID)
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, id) })
	return id
}

// TestBatchCustomTerminalStatusEntersStageBarrier is the regression guard for
// the batch stage barrier with a custom closed status (MUL-6243): moving the
// last child onto a CUSTOM done status must close the parent's barrier like
// the built-in done status.

// TestChildrenResponseCarriesStatusCategory pins the field the CLI's
// `issue children` stage progress reads. Without it the CLI counts only literal
// done/cancelled and reports wrong progress to an agent.

// TestCategoryFilterExpandsToIndexedStatusKeys is the regression guard for the
// performance fix: filtering by category must expand to concrete status keys so
// `status = ANY(...)` can use the (workspace_id, status) index. Wrapping the
// column in issue_effective_status() made that index unusable and turned a
// two-page index read into a full workspace scan.

// TestListFilterByCategoryReturnsCustomStatusIssues exercises the real endpoint.

// TestCreateEventCarriesCustomStatusCategory is the regression guard for the
// websocket path: filling the category only on the HTTP response is too late
// for other tabs, which receive the broadcast payload. Without it they cannot
// bucket the new issue and must fall back to a full refetch.

// TestListEndpointsCarryStatusCategory covers the remaining payload exits: every
// row a list or get returns must carry an authoritative category, custom
// statuses included. It deliberately does NOT assert the per-request catalog
// read count — see the note on the resolver subtest below for why.

// TestBackgroundEventCarriesCustomStatusCategory pins the background-event
// payload. IssueToMap fills a category only for built-ins; the publishers that
// can emit a CUSTOM status go through IssueToMapResolved so clients receive
// an authoritative one instead of a blank they would have to refetch to resolve.

// TestCatalogWritesAnnounceThemselves pins the realtime contract for the status
// catalog (MUL-6458): every write that changes it publishes exactly one
// workspace-scoped event, and a write that changes nothing publishes none.
//
// One event type covers all four writes because every client answers them the
// same way — re-read the catalog. What the assertions below are really
// protecting is the routing: an event with an empty WorkspaceID is dropped by
// the SubscribeAll forwarder without a word, so the catalog would look
// synchronized in tests and silently never reach another tab.

// TestCreateIssueStatusAcceptsANonLatinName is the regression for MUL-6749 /
// GitHub #7627. A display name written entirely in a non-Latin script has no
// characters in the key alphabet, so key derivation used to fail the create
// outright — and the settings form has no field for an explicit key, which left
// no way to create the status at all.

// TestCreateIssueStatusDisambiguatesCollidingSlugs covers the sharper half of
// #7627. The bug report's suggested workaround was to mix ASCII into the
// display name, but the non-ASCII part is dropped, so two DIFFERENT names
// collapse onto one key — and the second create used to fail with a conflict
// that blamed a display name nobody had taken.

// TestDerivedKeyAvoidsAnArchivedKey pins the storage constraint that makes the
// "taken" set wider than the visible catalog: idx_issue_status_workspace_key is
// NOT partial, so an archived status still owns its key and handing it out
// again would fail on insert.

// TestUnknownStatusErrorNamesCustomStatuses covers the other half of what makes
// a derived key usable: on its own `in_review_2` says nothing, so the error a
// caller gets after writing a bad status has to carry the display name or there
// is no way to find the status they were told to use. (MUL-6749)

// TestIssueResponseCarriesCustomStatusName pins the field an agent reads an
// issue through. `status` alone is a bare handle, and a derived key carries no
// meaning, so the display name travels beside it. (MUL-6749)

// TestExplicitKeyCreateAlsoTakesTheCatalogLock closes the half-open race found
// in review of MUL-6749. Derivation reads the catalog and then inserts; if a
// create that supplies its OWN key were allowed to skip the lock, it could land
// on the key the derive just chose in exactly that gap. The derive would then
// fail on the unique index and return 409 to a settings form that has no key
// field — the same dead end this issue exists to remove.
//
// The assertion is that an explicit-key create PARKS while the lock is held.
// Before the fix it completed immediately.

// TestConcurrentDerivedCreatesNeverConflict is the behavioral half of the same
// fix: whatever else is writing the catalog, a create that supplied no key must
// never come back 409. A caller who typed a key CAN legitimately be told it is
// taken; a caller who typed only a display name has nothing to correct.
//
// Every result carries WHICH kind of writer produced it. Counting successes
// without that — the first version of this test did — lets an explicit writer's
// 201 stand in for a derived writer's 409 and passes on exactly the regression
// it exists to catch.
//
// This is a probabilistic net, not a proof: the window it hunts for is the few
// microseconds between a derive's catalog read and its insert, and in-process
// it does not reliably open. TestExplicitKeyCreateAlsoTakesTheCatalogLock is
// the deterministic guard on the lock itself — this one guards the OUTCOME, so
// that any future path which lets a keyless create conflict shows up here even
// if the lock is still nominally taken. Do not delete one for the other.

// TestCustomStatusPayloadsAgreeAcrossRenderings pins the contract
// TestIssueToMap_KeysMatchIssueResponse cannot reach with a built-in fixture:
// an issue on a CUSTOM status must describe itself identically whether it
// arrives over HTTP or on a background event. A field present in one and blank
// in the other reads back undefined depending on which entry point produced the
// issue. (MUL-6749)
