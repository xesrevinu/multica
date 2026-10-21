package orchestrator

import (
	"encoding/json"
	"strings"
	"testing"
)

// A review-verdict status posted by anyone (an agent with the user's gh login
// included) is ignored; the check follows engine metadata only.
func TestReviewVerdictIgnoresForgedStatus(t *testing.T) {
	v := map[string]string{"review-verdict": "SUCCESS", "test": "SUCCESS"}
	req := []string{"test", ReviewVerdictCheck}
	snap := func(meta map[string]string) *Snapshot { return &Snapshot{Head: "h2", Issue: Issue{Meta: meta}} }

	red, all, pending := requiredChecks(v, req, snap(map[string]string{}))
	if len(red) != 0 || !pending || !strings.Contains(strings.Join(all, ","), "review-verdict=PENDING") {
		t.Fatalf("forged status must not satisfy review-verdict: red=%v all=%v pending=%v", red, all, pending)
	}
	_, all, pending = requiredChecks(v, req, snap(map[string]string{"verdict": "approve", "reviewed_sha": "h1"}))
	if !pending {
		t.Fatalf("approve on an older sha must stay pending: %v", all)
	}
	red, all, pending = requiredChecks(v, req, snap(map[string]string{"verdict": "approve", "reviewed_sha": "h2"}))
	if len(red) != 0 || pending {
		t.Fatalf("approve on the head must pass: red=%v all=%v", red, all)
	}
}

func TestVerdictOnlyWhileInReview(t *testing.T) {
	s := ownsP()
	for _, c := range []struct {
		w    MetaWrite
		want bool
	}{
		{MetaWrite{Key: "verdict", Value: `"approve"`, Status: StInProgress}, true},
		{MetaWrite{Key: "verdict", Value: `"approve"`, Status: StTodo}, true},
		{MetaWrite{Key: "reviewed_sha", Value: `"abc"`, Status: StInProgress}, true},
		{MetaWrite{Key: "verdict", Value: `"approve"`, Status: StInReview}, false},
		{MetaWrite{Key: "reviewed_sha", Value: `"abc"`, Status: StInReview}, false},
	} {
		c.w.ProjectID = "p"
		if got := s.CheckAgentMetaWrite(c.w) != ""; got != c.want {
			t.Errorf("%+v: refused=%v want %v", c.w, got, c.want)
		}
	}
}

// Only a review-verdict status created by the GitHub App counts; a status by
// the shared gh user (any agent) or another bot is ignored.
func TestAppVerdictTrustsOnlyTheApp(t *testing.T) {
	var list []ghStatus
	if err := json.Unmarshal([]byte(`[
		{"context":"review-verdict","state":"failure","creator":{"login":"kee","type":"User"}},
		{"context":"review-verdict","state":"success","creator":{"login":"other-app[bot]","type":"Bot"}},
		{"context":"test","state":"success","creator":{"login":"multica-orch[bot]","type":"Bot"}},
		{"context":"review-verdict","state":"success","creator":{"login":"multica-orch[bot]","type":"Bot"}},
		{"context":"review-verdict","state":"failure","creator":{"login":"multica-orch[bot]","type":"Bot"}}]`), &list); err != nil {
		t.Fatal(err)
	}
	if got := appVerdict(list, "multica-orch[bot]"); got != "success" {
		t.Fatalf("newest App review-verdict must win, got %q", got)
	}
	if got := appVerdict(list[:3], "multica-orch[bot]"); got != "" {
		t.Fatalf("forged / foreign statuses must be ignored, got %q", got)
	}
	if got := appVerdict(list, ""); got != "" {
		t.Fatalf("no App slug, nothing is trusted, got %q", got)
	}
	t.Setenv("GITHUB_APP_SLUG", "multica-orch")
	if appBotLogin() != "multica-orch[bot]" {
		t.Fatal(appBotLogin())
	}
}

// A red App snapshot rollup is not used as is (it may be red only because of
// a forged review-verdict failure); the per-check path decides.
func TestRedSnapshotIsNotConclusive(t *testing.T) {
	for rollup, want := range map[string]bool{"success": true, "pending": true, "": true, "failure": false, "ERROR": false} {
		if got := (&prSnap{rollup: rollup}).conclusive(); got != want {
			t.Errorf("rollup %q: conclusive=%v want %v", rollup, got, want)
		}
	}
	v := map[string]string{"review-verdict": "FAILURE", "test": "SUCCESS"}
	red, _, pending := requiredChecks(v, []string{"test", ReviewVerdictCheck}, &Snapshot{Head: "h", Issue: Issue{Meta: map[string]string{"verdict": "approve", "reviewed_sha": "h"}}})
	if len(red) != 0 || pending {
		t.Fatalf("a forged review-verdict failure must not turn the gate red: red=%v pending=%v", red, pending)
	}
}
