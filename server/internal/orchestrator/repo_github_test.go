package orchestrator

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/github/app"
	"github.com/multica-ai/multica/server/internal/integrations/ghsnapshot"
)

// fakeRepoGitHub is an httptest GitHub for one repo (o/r) with one PR (#9 on
// branch agent/att-1). It records write calls.
type fakeRepoGitHub struct {
	mu        sync.Mutex
	writes    []string
	mergeable string // "true" | "false" | "null"
	checkConc string // conclusion of the "test" check run
	merged    bool
	noPR      bool
	mergeBody map[string]any
	// combined overrides the /status body; statuses the /statuses list
	// (default: one success posted by a user). Posted status bodies are kept.
	combined, statuses string
	posted             []map[string]any
}

func (f *fakeRepoGitHub) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Method != http.MethodGet {
			f.writes = append(f.writes, r.Method+" "+p)
		}
		switch {
		case p == "/repos/o/r/installation":
			_, _ = w.Write([]byte(`{"id":77}`))
		case p == "/app/installations/77/access_tokens":
			w.WriteHeader(http.StatusCreated)
			fmt.Fprintf(w, `{"token":"ghs_t","expires_at":%q,"permissions":{"contents":"write","pull_requests":"write","actions":"write"}}`, time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
		case p == "/app":
			_, _ = w.Write([]byte(`{"slug":"orch"}`))
		case p == "/users/orch[bot]":
			_, _ = w.Write([]byte(`{"id":5}`))
		case p == "/repos/o/r/branches/agent/att-1":
			_, _ = w.Write([]byte(`{"commit":{"sha":"h1"}}`))
		case strings.HasPrefix(p, "/repos/o/r/branches/"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Branch not found"}`))
		case p == "/repos/o/r/pulls" && r.Method == http.MethodGet:
			if r.URL.Query().Get("head") != "o:agent/att-1" || r.URL.Query().Get("state") != "open" {
				t.Errorf("pr list query %s", r.URL.RawQuery)
			}
			if f.noPR {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			_, _ = w.Write([]byte(`[{"number":9,"html_url":"https://github.com/o/r/pull/9"}]`))
		case p == "/repos/o/r/pulls" && r.Method == http.MethodPost:
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			if b["head"] != "agent/att-1" || b["base"] != "main" || b["title"] != "ATT-1: add x" {
				t.Errorf("pr create body %v", b)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"number":10,"html_url":"https://github.com/o/r/pull/10"}`))
		case p == "/repos/o/r/pulls/9":
			fmt.Fprintf(w, `{"number":9,"title":"feat: add x (ATT-1)","body":"Adds x.","state":"open","merged":%v,"merge_commit_sha":"m0","mergeable":%s,"head":{"sha":"h1"}}`, f.merged, f.mergeable)
		case p == "/repos/o/r/commits/h1/check-runs":
			st := "completed"
			if f.checkConc == "" {
				st = "in_progress"
			}
			fmt.Fprintf(w, `{"check_runs":[{"name":"test","status":%q,"conclusion":%q}]}`, st, f.checkConc)
		case p == "/repos/o/r/commits/h1/status":
			if f.combined != "" {
				_, _ = w.Write([]byte(f.combined))
				return
			}
			_, _ = w.Write([]byte(`{"statuses":[{"context":"multica/review","state":"success"}]}`))
		case p == "/repos/o/r/commits/h1/statuses":
			if f.statuses != "" {
				_, _ = w.Write([]byte(f.statuses))
				return
			}
			_, _ = w.Write([]byte(`[{"context":"review-verdict","state":"success","creator":{"login":"someone","type":"User"}}]`))
		case p == "/repos/o/r/statuses/h1":
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			if b["context"] != "review-verdict" {
				t.Errorf("status body %v", b)
			}
			f.posted = append(f.posted, b)
			w.WriteHeader(http.StatusCreated)
		case p == "/repos/o/r/actions/runs":
			_, _ = w.Write([]byte(`{"workflow_runs":[{"id":3,"status":"completed","conclusion":"failure"}]}`))
		case p == "/repos/o/r/actions/runs/3/rerun-failed-jobs":
			w.WriteHeader(http.StatusCreated)
		case p == "/repos/o/r/pulls/9/merge":
			_ = json.NewDecoder(r.Body).Decode(&f.mergeBody)
			_, _ = w.Write([]byte(`{"sha":"sq1","merged":true}`))
		case p == "/repos/o/r/git/refs/heads/agent/att-1":
			w.WriteHeader(http.StatusUnprocessableEntity) // auto-deleted already
			_, _ = w.Write([]byte(`{"message":"Reference does not exist"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		}
	}
}

func newGitHubRepoOps(t *testing.T, f *fakeRepoGitHub) (*repoOps, *Team) {
	t.Helper()
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	a, err := app.New(app.Config{AppID: "1", PrivateKey: key, APIBase: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{}
	svc.ghOnce.Do(func() { svc.gh = ghsnapshot.NewClient(a) })
	sp := &Spec{}
	sp.Repo = RepoSpec{Mode: "github", Slug: "o/r", Main: "main", BranchPrefix: "agent/", RequiredChecks: []string{"test", "multica/review"}}
	return &repoOps{svc: svc}, &Team{ProjectID: "p1", Spec: sp}
}

func ghSnap(head string) *Snapshot {
	return &Snapshot{Head: head, Issue: Issue{Key: "ATT-1", Title: "add x", Meta: map[string]string{}}}
}

func TestGitHubHeadViaApp(t *testing.T) {
	r, team := newGitHubRepoOps(t, &fakeRepoGitHub{})
	ctx := context.Background()
	if h, err := r.head(ctx, team.Spec, "ATT-1"); err != nil || h != "h1" {
		t.Fatalf("head = %q, %v", h, err)
	}
	if h, err := r.head(ctx, team.Spec, "ATT-2"); err != nil || h != "" {
		t.Fatalf("missing branch = %q, %v", h, err)
	}
}

func TestGitHubGateMergesWithSquash(t *testing.T) {
	f := &fakeRepoGitHub{mergeable: "true", checkConc: "success"}
	r, team := newGitHubRepoOps(t, f)
	res := r.gate(context.Background(), team, ghSnap("h1"), true)
	if !res.OK || res.Kind != "merged" || res.SHA != "sq1" || res.PRNum != "9" {
		t.Fatalf("gate = %+v", res)
	}
	b := f.mergeBody
	msg, _ := b["commit_message"].(string)
	if b["merge_method"] != "squash" || b["sha"] != "h1" || b["commit_title"] != "feat: add x (ATT-1)" ||
		!strings.Contains(msg, "Co-authored-by: orch[bot] <5+orch[bot]@users.noreply.github.com>") || strings.Contains(msg, "Merge pull request") {
		t.Fatalf("merge body %v", b)
	}
	want := "PUT /repos/o/r/pulls/9/merge,DELETE /repos/o/r/git/refs/heads/agent/att-1"
	if got := strings.Join(f.writes[1:], ","); got != want { // [0] is the token mint
		t.Fatalf("writes %s", got)
	}
}

func TestGitHubGateStates(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		f       *fakeRepoGitHub
		head    string
		merge   bool
		kind    string
		pending bool
	}{
		{"pending checks", &fakeRepoGitHub{mergeable: "true"}, "h1", true, "pending", true},
		{"unknown mergeability", &fakeRepoGitHub{mergeable: "null", checkConc: "success"}, "h1", true, "pending", true},
		{"conflict", &fakeRepoGitHub{mergeable: "false", checkConc: "success"}, "h1", true, "conflict", false},
		{"stale head", &fakeRepoGitHub{mergeable: "true", checkConc: "success"}, "h0", true, "stale", true},
		{"dry ok", &fakeRepoGitHub{mergeable: "true", checkConc: "success"}, "h1", false, "dry", false},
		{"already merged", &fakeRepoGitHub{mergeable: "null", merged: true}, "h1", true, "already_merged", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, team := newGitHubRepoOps(t, tc.f)
			res := r.gate(ctx, team, ghSnap(tc.head), tc.merge)
			if res.Kind != tc.kind || res.Pending != tc.pending {
				t.Fatalf("gate = %+v", res)
			}
		})
	}
}

// A red required check earns one CI re-run per head; the second red is real.
func TestGitHubGateRedRerunsOnce(t *testing.T) {
	f := &fakeRepoGitHub{mergeable: "true", checkConc: "failure"}
	r, team := newGitHubRepoOps(t, f)
	ctx := context.Background()
	if res := r.gate(ctx, team, ghSnap("h1"), true); res.Kind != "pending" || !strings.Contains(res.Detail, "re-running") {
		t.Fatalf("first red = %+v", res)
	}
	if res := r.gate(ctx, team, ghSnap("h1"), true); res.Kind != "checks_red" {
		t.Fatalf("second red = %+v", res)
	}
	if !strings.Contains(strings.Join(f.writes, ","), "POST /repos/o/r/actions/runs/3/rerun-failed-jobs") {
		t.Fatalf("writes %v", f.writes)
	}
}

func TestGitHubEnsurePRCreates(t *testing.T) {
	f := &fakeRepoGitHub{noPR: true}
	r, team := newGitHubRepoOps(t, f)
	g, err := r.ghRepo(context.Background(), team.Spec)
	if err != nil {
		t.Fatal(err)
	}
	n, url, err := r.ensurePR(context.Background(), g, team.Spec, ghSnap("h1"))
	if err != nil || n != "10" || url != "https://github.com/o/r/pull/10" {
		t.Fatalf("ensurePR = %s %s %v", n, url, err)
	}
}

func TestGitHubGateWithoutApp(t *testing.T) {
	svc := &Service{}
	svc.ghOnce.Do(func() {})
	sp := &Spec{}
	sp.Repo = RepoSpec{Mode: "github", Slug: "o/r", Main: "main"}
	res := (&repoOps{svc: svc}).gate(context.Background(), &Team{Spec: sp}, ghSnap("h1"), true)
	if res.Kind != "push_failed" || !strings.Contains(res.Detail, "GitHub App not configured") {
		t.Fatalf("gate = %+v", res)
	}
}

// With review-verdict required, the gate trusts engine metadata (approve at
// the head), ignores a status posted by a user, posts the verdict as the App
// and only then squash-merges.
func TestGitHubGatePublishesReviewVerdictAsApp(t *testing.T) {
	f := &fakeRepoGitHub{mergeable: "true", checkConc: "success"}
	r, team := newGitHubRepoOps(t, f)
	team.Spec.Repo.RequiredChecks = []string{"test", ReviewVerdictCheck}
	t.Setenv("GITHUB_APP_SLUG", "")
	sn := ghSnap("h1")
	if res := r.gate(context.Background(), team, sn, true); res.Kind != "pending" || !strings.Contains(res.Detail, "review-verdict=PENDING") {
		t.Fatalf("without approve = %+v", res)
	}
	sn.Issue.Meta = map[string]string{"verdict": "approve", "reviewed_sha": "h1"}
	res := r.gate(context.Background(), team, sn, true)
	if !res.OK || res.Kind != "merged" {
		t.Fatalf("gate = %+v", res)
	}
	got := strings.Join(f.writes, ",")
	post, merge := strings.Index(got, "POST /repos/o/r/statuses/h1"), strings.Index(got, "PUT /repos/o/r/pulls/9/merge")
	if post < 0 || merge < 0 || post > merge {
		t.Fatalf("verdict must be posted before the merge: %s", got)
	}
}

// The reviewer agent posts review-verdict itself (it holds the user's gh
// login). The gate keeps ignoring it, and the App posts the engine's state
// over it: pending while there is no approve at the head, success once there
// is. An App-created newest status is left alone.
func TestGitHubGateOverridesAgentPostedReviewVerdict(t *testing.T) {
	ctx := context.Background()
	forged := `{"statuses":[{"context":"review-verdict","state":"failure"}]}`
	byAgent := `[{"context":"review-verdict","state":"failure","creator":{"login":"ray","type":"User"}}]`
	f := &fakeRepoGitHub{mergeable: "true", checkConc: "success", combined: forged, statuses: byAgent}
	r, team := newGitHubRepoOps(t, f)
	team.Spec.Repo.RequiredChecks = []string{"test", ReviewVerdictCheck}
	t.Setenv("GITHUB_APP_SLUG", "orch")

	res := r.gate(ctx, team, ghSnap("h1"), true)
	if res.Kind != "pending" || !strings.Contains(res.Detail, "review-verdict=PENDING") {
		t.Fatalf("forged failure must not send the issue back: %+v", res)
	}
	if len(f.posted) != 1 || f.posted[0]["state"] != "pending" {
		t.Fatalf("App must post pending over the agent's failure: %v", f.posted)
	}

	// The App's own status is newest: nothing more to do.
	f.posted = nil
	f.statuses = `[{"context":"review-verdict","state":"pending","creator":{"login":"orch[bot]","type":"Bot"}},` + byAgent[1:]
	f.combined = `{"statuses":[{"context":"review-verdict","state":"failure"}]}`
	_ = r.gate(ctx, team, ghSnap("h1"), false)
	if len(f.posted) != 0 {
		t.Fatalf("App status is newest; posted %v", f.posted)
	}

	// Combined state already matches the engine: no statuses read, no post.
	f.combined = `{"statuses":[{"context":"review-verdict","state":"pending"}]}`
	f.statuses = byAgent
	_ = r.gate(ctx, team, ghSnap("h1"), false)
	if len(f.posted) != 0 {
		t.Fatalf("matching state; posted %v", f.posted)
	}
}

func TestForeignVerdict(t *testing.T) {
	list := []ghStatus{
		{Context: "ci", State: "success"},
		{Context: ReviewVerdictCheck, State: "failure", Creator: &struct {
			Login string `json:"login"`
			Type  string `json:"type"`
		}{"ray", "User"}},
	}
	if got := foreignVerdict(list, "orch[bot]"); got != "ray" {
		t.Fatalf("foreign = %q", got)
	}
	if got := foreignVerdict(list[:1], "orch[bot]"); got != "" {
		t.Fatalf("none = %q", got)
	}
	if got := engineVerdictState(&Snapshot{Head: "h", Issue: Issue{Meta: map[string]string{"verdict": "approve", "reviewed_sha": "h"}}}); got != "success" {
		t.Fatalf("engine state = %q", got)
	}
	if got := engineVerdictState(&Snapshot{Head: "h2", Issue: Issue{Meta: map[string]string{"verdict": "approve", "reviewed_sha": "h"}}}); got != "pending" {
		t.Fatalf("engine state at a new head = %q", got)
	}
}
