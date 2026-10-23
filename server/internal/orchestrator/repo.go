package orchestrator

import (
	"bytes"
	"context"
	"fmt"
	"github.com/multica-ai/multica/server/internal/util"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// repoOps is the git / GitHub side of the engine. Local mode squash-merges
// into a bare remote; GitHub mode gates on the PR (webhook-fed App snapshot
// when fresh, a live REST read otherwise) and squash-merges through the
// GitHub App REST API guarded by the head sha. No `gh` CLI on the server.
type repoOps struct {
	svc     *Service
	retried sync.Map // head sha → CI already re-run once
	fetched sync.Map // hub path → last fetch time
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

// branchOf is the issue branch: the same name the daemon's worktree uses
// (prefix + lowercase slug of the key), pushed as-is. The uppercase key stays
// in the PR title for linking.
func branchOf(sp *Spec, key string) string { return sp.Repo.BranchPrefix + util.BranchSlug(key) }

// ExclusiveBranch is the branch the daemon must use for an issue of an
// orchestrated project (or fail with the occupant, never fork): "" when the
// project is not orchestrated right now (no team, paused, shadow).
func (s *Service) ExclusiveBranch(projectID, issueKey string) string {
	if !s.Owns(projectID) || issueKey == "" {
		return ""
	}
	t := s.Team(projectID)
	if t == nil || t.Spec == nil {
		return ""
	}
	return branchOf(t.Spec, issueKey)
}

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

// head returns the current sha of the issue branch, "" when not pushed.
func (r *repoOps) head(ctx context.Context, sp *Spec, key string) (string, error) {
	br := branchOf(sp, key)
	if sp.Repo.Mode == "local" {
		o := run(ctx, sp.Repo.Bare, "git", "rev-parse", "--verify", "-q", "refs/heads/"+br)
		return strings.TrimSpace(o.out), nil
	}
	g, err := r.ghRepo(ctx, sp)
	if err != nil {
		return "", err
	}
	sha, err := g.branchHead(ctx, br)
	if err != nil {
		return "", fmt.Errorf("github branch lookup: %w", err)
	}
	return sha, nil
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
		if sp.Repo.Hub != "" {
			// Reuse the local checkout's object store: a detached worktree,
			// no network clone. Shares origin with the hub.
			run(ctx, sp.Repo.Hub, "git", "worktree", "prune")
			if o := run(ctx, sp.Repo.Hub, "git", "worktree", "add", "-q", "--detach", w); o.code != 0 {
				return ProbeResult{Kind: "push_failed", Detail: "worktree add: " + o.err}
			}
		} else if o := run(ctx, "", "git", "clone", "-q", src, w); o.code != 0 {
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
	r.refreshHub(ctx, sp, true)
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

// ensurePR finds the open PR of the branch or opens one.
func (r *repoOps) ensurePR(ctx context.Context, g *ghRepo, sp *Spec, s *Snapshot) (num, url string, err error) {
	if n := s.Issue.Meta["pr_number"]; n != "" {
		return n, s.Issue.Meta["pr"], nil
	}
	br := branchOf(sp, s.Issue.Key)
	pr, err := g.openPRFor(ctx, br)
	if err != nil {
		return "", "", fmt.Errorf("github pr list: %w", err)
	}
	if pr == nil {
		body := fmt.Sprintf("Issue %s.\n\nOpened by the Multica orchestrator because the owner handed over without a PR. The owner's result comment on the issue carries the summary and test evidence.", s.Issue.Key)
		if pr, err = g.createPR(ctx, br, sp.Repo.Main, s.Issue.Key+": "+s.Issue.Title, body); err != nil {
			return "", "", fmt.Errorf("github pr create: %w", err)
		}
	}
	return fmt.Sprint(pr.Number), pr.HTMLURL, nil
}

// gate evaluates (and with merge=true performs) the GitHub merge.
func (r *repoOps) gate(ctx context.Context, t *Team, s *Snapshot, merge bool) ProbeResult {
	sp := t.Spec
	g, err := r.ghRepo(ctx, sp)
	if err != nil {
		if res, ok := rateLimited(err, ProbeResult{}); ok {
			return res
		}
		return ProbeResult{Kind: "push_failed", Detail: err.Error()}
	}
	n, url, err := r.ensurePR(ctx, g, sp, s)
	if err != nil {
		if res, ok := rateLimited(err, ProbeResult{}); ok {
			return res
		}
		return ProbeResult{Kind: "push_failed", Detail: err.Error()}
	}
	res := ProbeResult{PR: url, PRNum: n}
	if !merge {
		// Prefer the webhook-fed App snapshot; accept an older one when the
		// installation's REST budget runs low. A red rollup is not taken
		// from the snapshot: the live read below applies the review-verdict
		// rule (engine metadata, never a posted status).
		age := 2 * time.Minute
		if g.lowBudget() {
			age = 15 * time.Minute
		}
		if snap := r.svc.prSnapshot(ctx, sp, n, age); snap != nil && snap.conclusive() {
			return snap.apply(res, s.Head, sp.Repo.RequiredChecks)
		}
	}
	v, err := g.pr(ctx, n)
	if err != nil {
		if res, ok := rateLimited(err, res); ok {
			return res
		}
		res.Kind, res.Detail = "push_failed", "github pr view: "+err.Error()
		return res
	}
	if v.Merged {
		res.OK, res.Kind, res.SHA = true, "already_merged", v.MergeCommitSHA
		return res
	}
	if s.Head != "" && v.Head.SHA != s.Head {
		res.Kind, res.Detail, res.Pending = "stale", fmt.Sprintf("PR head %s != %s", short(v.Head.SHA, 12), short(s.Head, 12)), true
		return res
	}
	var checks map[string]string
	if len(sp.Repo.RequiredChecks) > 0 {
		if checks, err = g.checkStates(ctx, v.Head.SHA); err != nil {
			if res, ok := rateLimited(err, res); ok {
				return res
			}
			res.Kind, res.Detail = "push_failed", "github checks: "+err.Error()
			return res
		}
	}
	if seen, ok := checks[ReviewVerdictCheck]; ok && contains(sp.Repo.RequiredChecks, ReviewVerdictCheck) {
		// Somebody (in practice the reviewer agent, with the user's gh login)
		// posted review-verdict. Ignored by requiredChecks; the App puts the
		// engine's state back on the PR so humans see the real verdict.
		if who, err := r.svc.reassertReviewVerdict(ctx, sp, n, s, seen); err != nil {
			slog.Warn("orchestrator: re-assert review-verdict", "issue", s.Issue.Key, "pr", n, "err", err)
		} else if who != "" {
			slog.Warn("orchestrator: overrode a review-verdict status not posted by the App", "issue", s.Issue.Key, "pr", n, "creator", who, "state", seen)
		}
	}
	mergeable := v.mergeable()
	red, all, pending := requiredChecks(checks, sp.Repo.RequiredChecks, s)
	switch {
	case len(red) > 0:
		// babysit rule: a red CI head earns exactly one fresh re-run of its
		// failed workflow runs before it goes back to the owner; an identical
		// second failure is real.
		if r.retryCI(ctx, g, s.Head) {
			res.Kind, res.Pending, res.Detail = "pending", true, "re-running failed CI once: "+strings.Join(red, ",")
			return res
		}
		res.Kind, res.Detail = "checks_red", strings.Join(red, ",")
		return res
	case mergeable == "CONFLICTING":
		res.Kind, res.Detail = "conflict", "GitHub mergeable=CONFLICTING"
		return res
	case !merge:
		res.OK, res.Kind = true, "dry"
		return res
	case pending:
		res.Kind, res.Pending, res.Detail = "pending", true, strings.Join(all, ",")
		return res
	case mergeable != "MERGEABLE":
		res.Kind, res.Pending, res.Detail = "pending", true, "mergeable="+mergeable
		return res
	}
	defer r.lock(t.ProjectID)()
	if contains(sp.Repo.RequiredChecks, ReviewVerdictCheck) {
		// the engine, not the reviewer, publishes review-verdict - with the
		// GitHub App installation token, so its creator is the App - and only
		// for the head its own metadata says was approved (requiredChecks).
		if err := r.svc.publishReviewVerdict(ctx, sp, n, s); err != nil {
			res.Kind, res.Pending, res.Detail = "push_failed", true, "publish review-verdict status: "+short(err.Error(), 300)
			return res
		}
	}
	num := v.Number
	sha, err := g.c.SquashMerge(ctx, g.inst, g.owner, g.name, num, s.Head)
	if err != nil {
		if res, ok := rateLimited(err, res); ok {
			return res
		}
		res.Kind, res.Detail = "push_failed", "github merge: "+err.Error()
		return res
	}
	// Same as `gh pr merge --delete-branch`; a repo that auto-deletes head
	// branches answers 404/422 here, which is fine.
	if err := g.deleteBranch(ctx, branchOf(sp, s.Issue.Key)); err != nil {
		slog.Warn("orchestrator: delete merged branch", "pr", num, "err", err)
	}
	res.OK, res.Kind, res.SHA = true, "merged", sha
	r.refreshHub(ctx, sp, true)
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

// conclusive: a red rollup cannot be used as is. It aggregates every status
// context, including a review-verdict anyone with repo access can post (a
// forged failure used to send the issue back to the owner), so a red
// snapshot falls back to the per-check evaluation (requiredChecks), which
// reads only required checks and takes review-verdict from the App alone.
func (p *prSnap) conclusive() bool {
	return !strings.EqualFold(p.rollup, "failure") && !strings.EqualFold(p.rollup, "error")
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
// fresh (webhook-driven ghsnapshot, fetched within maxAge); nil means read
// the PR live.
func (s *Service) prSnapshot(ctx context.Context, sp *Spec, num string, maxAge time.Duration) *prSnap {
	parts := strings.SplitN(sp.Repo.Slug, "/", 2)
	if len(parts) != 2 || s.opt.Pool == nil {
		return nil
	}
	p := &prSnap{}
	cutoff := time.Now().UTC().Add(-maxAge).Format(time.RFC3339Nano)
	err := s.opt.Pool.QueryRowContext(ctx, `SELECT COALESCE(snapshot_head_sha, head_sha, ''), COALESCE(checks_rollup_state,''),
		COALESCE(CASE WHEN api_mergeable = 1 THEN 'true' WHEN api_mergeable = 0 THEN 'false' ELSE NULL END, mergeable_state, ''), snapshot_fetched_at
		FROM github_pull_request WHERE lower(repo_owner)=lower($1) AND lower(repo_name)=lower($2) AND CAST(pr_number AS TEXT)=$3 AND snapshot_fetched_at > $4`,
		parts[0], parts[1], num, cutoff).Scan(&p.headSHA, &p.rollup, &p.mergeable, sqlTime{&p.fetched})
	if err != nil {
		return nil
	}
	return p
}

func (r *repoOps) retryCI(ctx context.Context, g *ghRepo, head string) bool {
	if head == "" {
		return false
	}
	if _, done := r.retried.LoadOrStore(head, true); done {
		return false
	}
	// Durable claim: only the first INSERT for a head wins, across restarts.
	if r.svc.opt.Pool == nil {
		// no database (unit tests): the in-memory claim above is all there is
	} else if tag, err := r.svc.opt.Pool.ExecContext(ctx, `INSERT INTO orchestrator_ci_retry (head_sha) VALUES ($1) ON CONFLICT DO NOTHING`, head); err != nil || rowsAffected(tag) == 0 {
		return false
	}
	n, err := g.c.RerunFailedChecks(ctx, g.inst, g.owner, g.name, head)
	if err != nil {
		slog.Warn("orchestrator: CI re-run", "head", head, "err", err)
	}
	if n > 0 {
		slog.Info("orchestrator: CI re-run (one per red head)", "head", head, "runs", n)
	}
	return n > 0
}

var hexSHA = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// samePatch compares the stable patch-id of a and b, each diffed against its
// merge-base with main. Any failure (unknown object, force-pushed away,
// empty diff) answers false so the engine asks for a fresh review.
func (r *repoOps) samePatch(ctx context.Context, t *Team, a, b string) bool {
	if !hexSHA.MatchString(a) || !hexSHA.MatchString(b) {
		return false
	}
	sp := t.Spec
	dir, main := sp.Repo.Bare, "refs/heads/"+sp.Repo.Main
	if sp.Repo.Mode == "github" {
		dir, main = filepath.Join(r.work, "integ-"+t.ProjectID), "origin/"+sp.Repo.Main
		if _, err := os.Stat(dir); err != nil {
			return false
		}
		run(ctx, dir, "git", "fetch", "-q", "origin", sp.Repo.Main, a, b)
	}
	pid := func(x string) string {
		o := run(ctx, dir, "sh", "-c", fmt.Sprintf(`git diff "$(git merge-base %s %s)" %s | git patch-id --stable`, main, x, x))
		f := strings.Fields(o.out)
		if o.code != 0 || len(f) == 0 {
			return ""
		}
		return f[0]
	}
	pa := pid(a)
	return pa != "" && pa == pid(b)
}

// refreshHub keeps the local main checkout current so the daemon cuts task
// worktrees from today's main: fetch origin (throttled to once per 10s unless
// forced) and fast-forward main when the checkout sits on main with a clean
// tree. A dirty or diverged hub is left alone (it is the user's checkout);
// the agent still merges origin/main itself per its instructions.
func (r *repoOps) refreshHub(ctx context.Context, sp *Spec, force bool) {
	hub := sp.Repo.Hub
	if hub == "" {
		return
	}
	now := time.Now()
	if last, ok := r.fetched.Load(hub); ok && !force && now.Sub(last.(time.Time)) < 10*time.Second {
		return
	}
	r.fetched.Store(hub, now)
	if o := run(ctx, hub, "git", "fetch", "-q", "origin", "--prune"); o.code != 0 {
		slog.Warn("orchestrator: hub fetch", "hub", hub, "err", short(o.err, 200))
		return
	}
	cur := strings.TrimSpace(run(ctx, hub, "git", "symbolic-ref", "-q", "--short", "HEAD").out)
	dirty := strings.TrimSpace(run(ctx, hub, "git", "status", "--porcelain", "--untracked-files=no").out)
	if cur != sp.Repo.Main || dirty != "" {
		return
	}
	if o := run(ctx, hub, "git", "merge", "-q", "--ff-only", "origin/"+sp.Repo.Main); o.code != 0 {
		slog.Warn("orchestrator: hub fast-forward", "hub", hub, "err", short(o.err, 200))
	}
}

// ReviewVerdictCheck is the required check that carries the review verdict.
const ReviewVerdictCheck = "review-verdict"

// requiredChecks evaluates the required checks from the head's check states
// (check runs + commit statuses, see ghRepo.checkStates). review-verdict is
// never read from GitHub: it carries no creator, and any agent holding the
// user's gh login can post a commit status. It is derived from engine state
// (verdict=approve and reviewed_sha equal to the head); the engine publishes
// it with the GitHub App right before merging (publishReviewVerdict), and only
// an App-created review-verdict status is ever trusted (appVerdict).
func requiredChecks(checks map[string]string, required []string, s *Snapshot) (red, all []string, pending bool) {
	state := func(name string) string {
		if name == ReviewVerdictCheck {
			return strings.ToUpper(engineVerdictState(s))
		}
		if st, ok := checks[name]; ok {
			return st
		}
		return "MISSING"
	}
	for _, rc := range required {
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
	return red, all, pending
}
