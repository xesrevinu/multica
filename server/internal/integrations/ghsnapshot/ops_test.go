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
				"additions":3,"deletions":1,"changed_files":2,"head":{"ref":"agent/att-1","sha":"abc"},"base":{"ref":"main"},"user":{"login":"app[bot]"},"labels":[{"name":"risk:low"}]}`))
		case p == "/repos/o/r/pulls/7/commits":
			_, _ = w.Write([]byte(`[{"sha":"abc","commit":{"message":"feat: x\n\nbody","author":{"name":"bot","date":"2026-10-19T01:00:00Z"}},"author":{"login":"app[bot]"},"parents":[{"sha":"p1"},{"sha":"p2"}]}]`))
		case p == "/repos/o/r/commits/abc/check-runs":
			_, _ = w.Write([]byte(`{"check_runs":[{"name":"test","status":"completed","conclusion":"failure","html_url":"u","app":{"slug":"github-actions"}}]}`))
		case p == "/repos/o/r/compare/main...abc":
			_, _ = w.Write([]byte(`{"ahead_by":1,"behind_by":4,"merge_base_commit":{"sha":"mb"}}`))
		case p == "/repos/o/r/compare/mb...main":
			_, _ = w.Write([]byte(`{"files":[{"filename":"src/a.js"},{"filename":"README.md"}]}`))
		case p == "/repos/o/r/pulls/7/files":
			_, _ = w.Write([]byte(`[{"filename":"src/a.js","status":"modified","additions":3,"deletions":1,"patch":"@@"},{"filename":"test/a.test.js","status":"added","additions":2}]`))
		case p == "/repos/o/r/pulls/7/reviews":
			_, _ = w.Write([]byte(`[{"user":{"login":"rev"},"state":"CHANGES_REQUESTED","body":"fix it","submitted_at":"2026-10-19T02:00:00Z"},{"user":{"login":"x"},"state":"PENDING"}]`))
		case p == "/repos/o/r/pulls/7/comments":
			_, _ = w.Write([]byte(`[{"id":5,"user":{"login":"rev"},"path":"src/a.js","line":3,"body":"nit","created_at":"2026-10-19T02:01:00Z"}]`))
		case p == "/repos/o/r/issues/7/labels":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
		case p == "/repos/o/r/git/refs/heads/agent/att-1":
			w.WriteHeader(http.StatusNoContent)
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
		case p == "/app":
			_, _ = w.Write([]byte(`{"slug":"orch-app"}`))
		case p == "/users/orch-app[bot]":
			_, _ = w.Write([]byte(`{"id":99}`))
		case p == "/repos/o/r/pulls/7/merge":
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			msg, _ := b["commit_message"].(string)
			if b["merge_method"] != "squash" || b["commit_title"] != "t" || b["sha"] != "abc" ||
				!strings.Contains(msg, "PR #7") || !strings.Contains(msg, "Co-authored-by: orch-app[bot] <99+orch-app[bot]@users.noreply.github.com>") ||
				strings.Contains(msg, "Merge pull request") {
				t.Errorf("merge body %v", b)
			}
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
	if len(d.Commits[0].Parents) != 2 || d.Author != "app[bot]" || len(d.Labels) != 1 || len(d.Conflicts) != 1 || d.Conflicts[0] != "src/a.js" {
		t.Fatalf("parents/labels/conflicts %+v", d)
	}
	if len(d.Reviews) != 1 || d.Reviews[0].State != "CHANGES_REQUESTED" || len(d.ReviewComments) != 1 || d.ReviewComments[0].Line != 3 {
		t.Fatalf("reviews %+v %+v", d.Reviews, d.ReviewComments)
	}
	if len(d.Checks) != 1 || d.Checks[0].Conclusion != "failure" || d.Checks[0].App != "github-actions" {
		t.Fatalf("checks %+v", d.Checks)
	}

	perms, err := c.Permissions(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	ab := AbilitiesFrom(perms)
	if !ab.UpdateBranch || !ab.Merge || ab.Rerun || !ab.ReadChecks || !ab.Labels || !ab.DeleteBranch {
		t.Fatalf("abilities %+v", ab)
	}

	if h, err := c.FetchPRHead(ctx, 1, "o", "r", 7); err != nil || h != "abc" {
		t.Fatalf("FetchPRHead = %q, %v", h, err)
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
	files, err := c.PRFiles(ctx, 1, "o", "r", 7)
	if err != nil || len(files) != 2 || files[1].Status != "added" {
		t.Fatalf("files %v %v", files, err)
	}
	if err := c.AddLabels(ctx, 1, "o", "r", 7, []string{"risk:low"}); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteBranch(ctx, 1, "o", "r", "agent/att-1"); err != nil {
		t.Fatal(err)
	}
	want := "POST /app/installations/1/access_tokens,PUT /repos/o/r/pulls/7/update-branch,POST /repos/o/r/actions/runs/11/rerun-failed-jobs,PUT /repos/o/r/pulls/7/merge,POST /repos/o/r/issues/7/labels,DELETE /repos/o/r/git/refs/heads/agent/att-1"
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

func TestSquashCommitMessage(t *testing.T) {
	body := "<!-- template -->\r\nAdds ten() to src/ops.js with a test.\r\n\r\n## Checklist\r\n- [x] tests"
	title, msg := SquashCommitMessage(" feat(ops): add ten() ", body, 25, "Co-authored-by: b[bot] <1+b[bot]@users.noreply.github.com>")
	if title != "feat(ops): add ten()" {
		t.Fatalf("title %q", title)
	}
	want := "Adds ten() to src/ops.js with a test.\n\nPR #25\n\nCo-authored-by: b[bot] <1+b[bot]@users.noreply.github.com>"
	if msg != want {
		t.Fatalf("msg\n%q\nwant\n%q", msg, want)
	}
	if _, msg := SquashCommitMessage("x", "## Why\n\nCallers need eleven().\n\n## Test\nnpm test", 26, ""); msg != "Callers need eleven().\n\nPR #26" {
		t.Fatalf("heading body msg %q", msg)
	}
	if _, msg := SquashCommitMessage("x", "## Summary\nAdds eleven().", 7, ""); msg != "Adds eleven().\n\nPR #7" {
		t.Fatalf("heading+text paragraph msg %q", msg)
	}
	if _, msg := SquashCommitMessage("x", "", 3, ""); msg != "PR #3" {
		t.Fatalf("empty body msg %q", msg)
	}
}
