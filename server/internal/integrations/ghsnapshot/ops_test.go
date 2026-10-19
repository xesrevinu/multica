package ghsnapshot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeGitHub serves the endpoints the PR card uses and records write calls.
func fakeGitHub(t *testing.T) (*httptest.Server, *[]string) {
	var mu sync.Mutex
	calls := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Errorf("missing auth on %s", r.URL.Path)
		}
		mu.Lock()
		if r.Method != http.MethodGet {
			calls = append(calls, r.Method+" "+r.URL.Path)
		}
		mu.Unlock()
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/access_tokens"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"ghs_x","expires_at":"` + time.Now().Add(time.Hour).UTC().Format(time.RFC3339) +
				`","permissions":{"contents":"write","pull_requests":"write","checks":"read","actions":"read"}}`))
		case p == "/repos/o/r/pulls/7":
			_, _ = w.Write([]byte(`{"number":7,"title":"t","state":"open","html_url":"https://x/7","mergeable":false,"mergeable_state":"dirty",
				"additions":3,"deletions":1,"changed_files":2,"head":{"ref":"agent/att-1","sha":"abc"},"base":{"ref":"main"}}`))
		case p == "/repos/o/r/pulls/7/commits":
			_, _ = w.Write([]byte(`[{"sha":"abc","commit":{"message":"feat: x\n\nbody","author":{"name":"bot","date":"2026-10-19T01:00:00Z"}},"author":{"login":"app[bot]"}}]`))
		case p == "/repos/o/r/commits/abc/check-runs":
			_, _ = w.Write([]byte(`{"check_runs":[{"name":"test","status":"completed","conclusion":"failure","html_url":"u","app":{"slug":"github-actions"}}]}`))
		case p == "/repos/o/r/compare/main...abc":
			_, _ = w.Write([]byte(`{"ahead_by":1,"behind_by":4}`))
		case p == "/repos/o/r/pulls/7/update-branch":
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			if b["expected_head_sha"] != "abc" {
				t.Errorf("update-branch body %v", b)
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"message":"Updating pull request branch."}`))
		case p == "/repos/o/r/actions/runs":
			_, _ = w.Write([]byte(`{"workflow_runs":[{"id":11,"status":"completed","conclusion":"failure"},{"id":12,"status":"completed","conclusion":"success"},{"id":13,"status":"in_progress"}]}`))
		case p == "/repos/o/r/actions/runs/11/rerun-failed-jobs":
			w.WriteHeader(http.StatusCreated)
		case p == "/repos/o/r/pulls/7/merge":
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = w.Write([]byte(`{"message":"Pull Request is not mergeable"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		}
	}))
	return srv, &calls
}

func TestPRDetailAndOps(t *testing.T) {
	srv, calls := fakeGitHub(t)
	defer srv.Close()
	c := newTestClient(t, srv.URL)
	ctx := context.Background()

	d, err := c.FetchPRDetail(ctx, 1, "o", "r", 7)
	if err != nil {
		t.Fatal(err)
	}
	if d.MergeableState != "dirty" || d.BehindBy != 4 || len(d.Commits) != 1 || d.Commits[0].Message != "feat: x" || d.Commits[0].Author != "app[bot]" {
		t.Fatalf("detail %+v", d)
	}
	if len(d.Checks) != 1 || d.Checks[0].Conclusion != "failure" || d.Checks[0].App != "github-actions" {
		t.Fatalf("checks %+v", d.Checks)
	}

	perms, err := c.Permissions(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	ab := AbilitiesFrom(perms)
	if !ab.UpdateBranch || !ab.Merge || ab.Rerun || !ab.ReadChecks {
		t.Fatalf("abilities %+v", ab)
	}

	if err := c.UpdateBranch(ctx, 1, "o", "r", 7, "abc"); err != nil {
		t.Fatal(err)
	}
	if n, err := c.RerunFailedChecks(ctx, 1, "o", "r", "abc"); err != nil || n != 1 {
		t.Fatalf("rerun n=%d err=%v", n, err)
	}
	_, err = c.SquashMerge(ctx, 1, "o", "r", 7, "abc")
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 405 || ae.Message != "Pull Request is not mergeable" {
		t.Fatalf("merge err %v", err)
	}
	if strings.Contains(err.Error(), "ghs_x") {
		t.Fatal("token leaked into error")
	}
	want := "POST /app/installations/1/access_tokens,PUT /repos/o/r/pulls/7/update-branch,POST /repos/o/r/actions/runs/11/rerun-failed-jobs,PUT /repos/o/r/pulls/7/merge"
	if got := strings.Join(*calls, ","); got != want {
		t.Fatalf("calls\n got  %s\n want %s", got, want)
	}
}

func TestOpsDisabledClient(t *testing.T) {
	var c *Client
	if _, err := c.FetchPRDetail(context.Background(), 1, "o", "r", 1); err == nil {
		t.Fatal("nil client must error")
	}
	if ab := AbilitiesFrom(nil); ab.Merge || ab.Rerun || ab.UpdateBranch || ab.ReadChecks {
		t.Fatalf("nil perms %+v", ab)
	}
}
