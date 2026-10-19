package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// repoOps is the git / GitHub side of the engine. Local mode squash-merges
// into a bare remote; GitHub mode gates on the PR (App snapshot when fresh,
// `gh` otherwise) and merges with `gh pr merge --squash --match-head-commit`.
type repoOps struct {
	svc     *Service
	retried sync.Map // head sha → CI already re-run once
	mu      sync.Map // per-project integration lock (serial merge)
	work    string
}

type shOut struct {
	code     int
	out, err string
}

func run(ctx context.Context, dir string, name string, args ...string) shOut {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = dir
	var o, e bytes.Buffer
	c.Stdout, c.Stderr = &o, &e
	err := c.Run()
	code := 0
	if err != nil {
		code = 1
		if x, ok := err.(*exec.ExitError); ok {
			code = x.ExitCode()
		}
		if e.Len() == 0 {
			e.WriteString(err.Error())
		}
	}
	return shOut{code, o.String(), e.String()}
}

func (r *repoOps) lock(project string) func() {
	m, _ := r.mu.LoadOrStore(project, &sync.Mutex{})
	mx := m.(*sync.Mutex)
	mx.Lock()
	return mx.Unlock
}

func branchOf(sp *Spec, key string) string { return sp.Repo.BranchPrefix + key }

func freeGB(path string) int {
	if path == "" {
		path = "/"
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return -1
	}
	return int(st.Bavail * uint64(st.Bsize) / (1 << 30))
}

func (r *repoOps) gh(ctx context.Context, sp *Spec, args ...string) (string, error) {
	o := run(ctx, "", "gh", append(args, "--repo", sp.Repo.Slug)...)
	if o.code != 0 {
		return "", fmt.Errorf("gh %s: %s", args[0]+" "+args[1], short(o.err, 300))
	}
	return o.out, nil
}

// head returns the current sha of the issue branch, "" when not pushed.
func (r *repoOps) head(ctx context.Context, sp *Spec, key string) (string, error) {
	br := branchOf(sp, key)
	if sp.Repo.Mode == "local" {
		o := run(ctx, sp.Repo.Bare, "git", "rev-parse", "--verify", "-q", "refs/heads/"+br)
		return strings.TrimSpace(o.out), nil
	}
	o := run(ctx, "", "gh", "api", fmt.Sprintf("repos/%s/branches/%s", sp.Repo.Slug, br), "--jq", ".commit.sha")
	if o.code != 0 {
		if strings.Contains(o.err+o.out, "404") || strings.Contains(o.err+o.out, "Branch not found") {
			return "", nil
		}
		return "", fmt.Errorf("gh branch lookup: %s", short(o.err, 200))
	}
	return strings.TrimSpace(o.out), nil
}

// integrate squash-merges the branch onto main in a scratch clone, runs the
// check command and, when push is set, pushes main (local mode merge path and
// the pre-check in both modes when check_cmd is set).
func (r *repoOps) integrate(ctx context.Context, t *Team, s *Snapshot, push bool) ProbeResult {
	sp := t.Spec
	src := sp.Repo.Bare
	if sp.Repo.Mode == "github" {
		src = sp.Repo.URL
		if src == "" {
			src = "https://github.com/" + sp.Repo.Slug + ".git"
		}
	}
	w := filepath.Join(r.work, "integ-"+t.ProjectID)
	if _, err := os.Stat(w); err != nil {
		_ = os.MkdirAll(filepath.Dir(w), 0o755)
		if o := run(ctx, "", "git", "clone", "-q", src, w); o.code != 0 {
			return ProbeResult{Kind: "push_failed", Detail: "clone: " + o.err}
		}
		run(ctx, w, "git", "config", "user.email", "orchestrator@multica.local")
		run(ctx, w, "git", "config", "user.name", "Multica Orchestrator")
	}
	M := sp.Repo.Main
	run(ctx, w, "git", "fetch", "-q", "origin", "--prune")
	run(ctx, w, "git", "reset", "-q", "--hard")
	run(ctx, w, "git", "clean", "-fdq")
	run(ctx, w, "git", "checkout", "-q", "--detach", "origin/"+M)
	if mg := run(ctx, w, "git", "merge", "--squash", "origin/"+branchOf(sp, s.Issue.Key)); mg.code != 0 {
		run(ctx, w, "git", "reset", "-q", "--hard")
		return ProbeResult{Kind: "conflict", Detail: tail(mg.out+mg.err, 300)}
	}
	if sp.Repo.CheckCmd != "" {
		if ck := run(ctx, w, "sh", "-c", sp.Repo.CheckCmd); ck.code != 0 {
			run(ctx, w, "git", "reset", "-q", "--hard")
			return ProbeResult{Kind: "checks_red", Detail: tail(ck.out+ck.err, 400)}
		}
	}
	if !push {
		run(ctx, w, "git", "reset", "-q", "--hard")
		return ProbeResult{OK: true, Kind: "dry"}
	}
	run(ctx, w, "git", "commit", "-q", "-m", fmt.Sprintf("%s: %s (squash of %s)", s.Issue.Key, s.Issue.Title, branchOf(sp, s.Issue.Key)))
	if ps := run(ctx, w, "git", "push", "-q", "origin", "HEAD:"+M); ps.code != 0 {
		return ProbeResult{Kind: "push_failed", Detail: tail(ps.err, 200)}
	}
	if sp.Repo.Hub != "" {
		run(ctx, sp.Repo.Hub, "git", "pull", "-q", "--ff-only")
	}
	// cleanup: the merged branch is done; drop it from the bare remote.
	run(ctx, sp.Repo.Bare, "git", "update-ref", "-d", "refs/heads/"+branchOf(sp, s.Issue.Key))
	return ProbeResult{OK: true, Kind: "merged", SHA: strings.TrimSpace(run(ctx, w, "git", "rev-parse", "HEAD").out)}
}

func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

type prView struct {
	Number      int    `json:"number"`
	URL         string `json:"url"`
	HeadRefOid  string `json:"headRefOid"`
	Mergeable   string `json:"mergeable"`
	State       string `json:"state"`
	MergeCommit *struct {
		Oid string `json:"oid"`
	} `json:"mergeCommit"`
	StatusCheckRollup []struct {
		Typename   string `json:"__typename"`
		Name       string `json:"name"`
		Context    string `json:"context"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		State      string `json:"state"`
	} `json:"statusCheckRollup"`
}

// ensurePR finds the open PR of the branch or opens one.
func (r *repoOps) ensurePR(ctx context.Context, sp *Spec, s *Snapshot) (num, url string, err error) {
	if n := s.Issue.Meta["pr_number"]; n != "" {
		return n, s.Issue.Meta["pr"], nil
	}
	out, err := r.gh(ctx, sp, "pr", "list", "--head", branchOf(sp, s.Issue.Key), "--state", "open", "--json", "number,url")
	if err != nil {
		return "", "", err
	}
	var l []prView
	_ = json.Unmarshal([]byte(out), &l)
	if len(l) > 0 {
		return fmt.Sprint(l[0].Number), l[0].URL, nil
	}
	body := fmt.Sprintf("Issue %s.\n\nOpened by the Multica orchestrator because the owner handed over without a PR. The owner's result comment on the issue carries the summary and test evidence.", s.Issue.Key)
	out, err = r.gh(ctx, sp, "pr", "create", "--head", branchOf(sp, s.Issue.Key), "--base", sp.Repo.Main, "--title", s.Issue.Key+": "+s.Issue.Title, "--body", body)
	if err != nil {
		return "", "", err
	}
	url = strings.TrimSpace(out)
	return url[strings.LastIndex(url, "/")+1:], url, nil
}

// gate evaluates (and with merge=true performs) the GitHub merge.
func (r *repoOps) gate(ctx context.Context, t *Team, s *Snapshot, merge bool) ProbeResult {
	sp := t.Spec
	n, url, err := r.ensurePR(ctx, sp, s)
	if err != nil {
		return ProbeResult{Kind: "push_failed", Detail: err.Error()}
	}
	res := ProbeResult{PR: url, PRNum: n}
	if snap := r.svc.prSnapshot(ctx, sp, n); snap != nil && !merge {
		// App snapshot is fresh: no GitHub API call needed for the pre-check.
		return snap.apply(res, s.Head, sp.Repo.RequiredChecks)
	}
	out, err := r.gh(ctx, sp, "pr", "view", n, "--json", "number,url,headRefOid,mergeable,statusCheckRollup,state,mergeCommit")
	if err != nil {
		res.Kind, res.Detail = "push_failed", err.Error()
		return res
	}
	var v prView
	_ = json.Unmarshal([]byte(out), &v)
	if v.State == "MERGED" {
		res.OK, res.Kind = true, "already_merged"
		if v.MergeCommit != nil {
			res.SHA = v.MergeCommit.Oid
		}
		return res
	}
	if s.Head != "" && v.HeadRefOid != s.Head {
		res.Kind, res.Detail, res.Pending = "stale", fmt.Sprintf("PR head %s != %s", short(v.HeadRefOid, 12), short(s.Head, 12)), true
		return res
	}
	state := func(name string) string {
		for _, c := range v.StatusCheckRollup {
			if c.Name == name || c.Context == name {
				if c.Typename == "StatusContext" {
					return c.State
				}
				if c.Status != "COMPLETED" {
					return "PENDING"
				}
				return c.Conclusion
			}
		}
		return "MISSING"
	}
	var red, all []string
	pending := false
	for _, rc := range sp.Repo.RequiredChecks {
		x := state(rc)
		all = append(all, rc+"="+x)
		switch x {
		case "FAILURE", "ERROR", "CANCELLED", "TIMED_OUT", "ACTION_REQUIRED":
			red = append(red, rc+"="+x)
		case "SUCCESS":
		default:
			pending = true
		}
	}
	switch {
	case len(red) > 0:
		// babysit rule: a red CI head earns exactly one fresh re-run of its
		// failed workflow runs before it goes back to the owner; an identical
		// second failure is real.
		if r.retryCI(ctx, sp, s.Head) {
			res.Kind, res.Pending, res.Detail = "pending", true, "re-running failed CI once: "+strings.Join(red, ",")
			return res
		}
		res.Kind, res.Detail = "checks_red", strings.Join(red, ",")
		return res
	case v.Mergeable == "CONFLICTING":
		res.Kind, res.Detail = "conflict", "GitHub mergeable=CONFLICTING"
		return res
	case !merge:
		res.OK, res.Kind = true, "dry"
		return res
	case pending:
		res.Kind, res.Pending, res.Detail = "pending", true, strings.Join(all, ",")
		return res
	case v.Mergeable != "MERGEABLE":
		res.Kind, res.Pending, res.Detail = "pending", true, "mergeable="+v.Mergeable
		return res
	}
	defer r.lock(t.ProjectID)()
	if _, err := r.gh(ctx, sp, "pr", "merge", n, "--squash", "--delete-branch", "--match-head-commit", s.Head); err != nil {
		res.Kind, res.Detail = "push_failed", err.Error()
		return res
	}
	out, _ = r.gh(ctx, sp, "pr", "view", n, "--json", "mergeCommit,state")
	_ = json.Unmarshal([]byte(out), &v)
	res.OK, res.Kind = v.State == "MERGED", "merged"
	if v.MergeCommit != nil {
		res.SHA = v.MergeCommit.Oid
	}
	if !res.OK {
		res.Kind, res.Detail = "push_failed", "state after merge: "+v.State
	}
	return res
}

func (r *repoOps) prober(ctx context.Context, t *Team) Prober {
	return func(s *Snapshot, merge bool) ProbeResult {
		if t.Spec.Repo.Mode == "local" {
			if merge {
				defer r.lock(t.ProjectID)()
			}
			return r.integrate(ctx, t, s, merge)
		}
		if !merge && t.Spec.Repo.CheckCmd != "" {
			if pr := r.integrate(ctx, t, s, false); !pr.OK && pr.Kind != "push_failed" {
				return pr
			}
		}
		return r.gate(ctx, t, s, merge)
	}
}

// prSnap is the GitHub App snapshot row of a PR (ghsnapshot tables).
type prSnap struct {
	headSHA, rollup, mergeable string
	fetched                    time.Time
}

func (p *prSnap) apply(res ProbeResult, head string, _ []string) ProbeResult {
	switch {
	case head != "" && p.headSHA != head:
		res.Kind, res.Pending, res.Detail = "stale", true, "snapshot head "+short(p.headSHA, 12)
	case strings.EqualFold(p.rollup, "failure") || strings.EqualFold(p.rollup, "error"):
		res.Kind, res.Detail = "checks_red", "checks rollup="+p.rollup
	case strings.EqualFold(p.mergeable, "conflicting") || strings.EqualFold(p.mergeable, "false"):
		res.Kind, res.Detail = "conflict", "mergeable="+p.mergeable
	default:
		res.OK, res.Kind = true, "dry"
	}
	return res
}

// prSnapshot returns the GitHub App snapshot of the PR when the App keeps it
// fresh (webhook-driven ghsnapshot); nil means fall back to `gh`.
func (s *Service) prSnapshot(ctx context.Context, sp *Spec, num string) *prSnap {
	parts := strings.SplitN(sp.Repo.Slug, "/", 2)
	if len(parts) != 2 {
		return nil
	}
	p := &prSnap{}
	err := s.opt.Pool.QueryRow(ctx, `SELECT COALESCE(snapshot_head_sha, head_sha, ''), COALESCE(checks_rollup_state,''), COALESCE(api_mergeable::text, mergeable_state, ''), snapshot_fetched_at
		FROM github_pull_request WHERE lower(repo_owner)=lower($1) AND lower(repo_name)=lower($2) AND pr_number::text=$3 AND snapshot_fetched_at > now() - interval '2 minutes'`,
		parts[0], parts[1], num).Scan(&p.headSHA, &p.rollup, &p.mergeable, &p.fetched)
	if err != nil {
		return nil
	}
	return p
}

func (r *repoOps) retryCI(ctx context.Context, sp *Spec, head string) bool {
	if head == "" {
		return false
	}
	if _, done := r.retried.LoadOrStore(head, true); done {
		return false
	}
	out, err := r.gh(ctx, sp, "run", "list", "--commit", head, "--json", "databaseId,conclusion")
	if err != nil {
		return false
	}
	var runs []struct {
		ID         int64  `json:"databaseId"`
		Conclusion string `json:"conclusion"`
	}
	_ = json.Unmarshal([]byte(out), &runs)
	n := 0
	for _, x := range runs {
		if x.Conclusion == "failure" || x.Conclusion == "timed_out" || x.Conclusion == "cancelled" {
			if _, err := r.gh(ctx, sp, "run", "rerun", fmt.Sprint(x.ID), "--failed"); err == nil {
				n++
			}
		}
	}
	return n > 0
}
