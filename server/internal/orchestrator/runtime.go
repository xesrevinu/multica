package orchestrator

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/inbox"
	"github.com/multica-ai/multica/server/internal/integrations/ghsnapshot"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ServiceUserEmail identifies the orchestrator's own member. Its writes are
// the only status/assignee writes the guard admits on orchestrated projects.
const ServiceUserEmail = "orchestrator@system.multica.local"

// InboxType is the native inbox item type for orchestrator notifications.
const InboxType = "orchestrator_alert"

type Options struct {
	Pool      *pgxpool.Pool
	Bus       *events.Bus
	Queries   *db.Queries
	WorkDir   string
	Reconcile time.Duration
	Debounce  time.Duration
	// BackoffBase / BackoffCap for per-issue failure backoff.
	BackoffBase, BackoffCap time.Duration
}

// repoBackend is everything the runtime needs from git/GitHub. repoOps is the
// real one; integration tests plug in a fake GitHub.
type repoBackend interface {
	head(ctx context.Context, sp *Spec, key string) (string, error)
	samePatch(ctx context.Context, t *Team, a, b string) bool
	prober(ctx context.Context, t *Team) Prober
	refreshHub(ctx context.Context, sp *Spec, force bool)
}

// Service runs the engine in-process.
type Service struct {
	opt    Options
	st     *store
	repo   repoBackend
	router http.Handler

	mu        sync.RWMutex
	teams     map[string]*Team // by project id
	svcUserID string
	token     string
	wake      chan struct{}
	locks     sync.Map
	lastBeat  time.Time
	lastErr   string
	disabled  bool

	// gh is the GitHub App client for PR card ops (nil = App not configured).
	ghOnce sync.Once
	gh     *ghsnapshot.Client
}

func New(opt Options) *Service {
	if opt.Reconcile <= 0 {
		opt.Reconcile = 60 * time.Second
	}
	if opt.Debounce <= 0 {
		opt.Debounce = 300 * time.Millisecond
	}
	if opt.BackoffBase <= 0 {
		opt.BackoffBase = 5 * time.Second
	}
	if opt.BackoffCap <= 0 {
		opt.BackoffCap = 5 * time.Minute
	}
	if opt.WorkDir == "" {
		d, _ := os.UserCacheDir()
		opt.WorkDir = filepath.Join(d, "multica-orchestrator")
	}
	s := &Service{opt: opt, st: &store{opt.Pool}, teams: map[string]*Team{}, wake: make(chan struct{}, 1),
		disabled: os.Getenv("MULTICA_ORCHESTRATOR_DISABLED") == "1"}
	s.repo = &repoOps{svc: s, work: opt.WorkDir}
	return s
}

// --- queries used by the write guard (cheap, cached) --------------------------

// Owns reports whether the project is orchestrated and not paused.
func (s *Service) Owns(projectID string) bool {
	if s == nil || projectID == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	t := s.teams[projectID]
	return t != nil && !t.Paused && !s.disabled && !t.Spec.Shadow
}

// Team returns the cached team of a project.
func (s *Service) Team(projectID string) *Team {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.teams[projectID]
}

// IsServiceUser reports whether userID is the orchestrator's member.
func (s *Service) IsServiceUser(userID string) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return userID != "" && userID == s.svcUserID
}

func (s *Service) reload(ctx context.Context) error {
	ts, err := s.st.teams(ctx)
	if err != nil {
		return err
	}
	m := map[string]*Team{}
	for _, t := range ts {
		m[t.ProjectID] = t
	}
	s.mu.Lock()
	s.teams = m
	s.mu.Unlock()
	return nil
}

// --- lifecycle --------------------------------------------------------------

// Start provisions the service member, subscribes to the bus and runs the
// worker. router is the API router: decisions are applied through it so a
// transition behaves exactly like a member's update (one run per hand-off).
func (s *Service) Start(ctx context.Context, router http.Handler) {
	s.router = router
	if err := s.ensureServiceUser(ctx); err != nil {
		slog.Error("orchestrator: service user", "err", err)
		return
	}
	if err := s.reload(ctx); err != nil {
		slog.Error("orchestrator: load teams", "err", err)
	}
	for _, t := range s.snapshotTeams() {
		_ = s.ensureMember(ctx, t.WorkspaceID)
	}
	s.opt.Bus.SubscribeAll(func(e events.Event) {
		id := issueIDOf(e)
		if id == "" {
			return
		}
		if e.Type == protocol.EventIssueUpdated || e.Type == protocol.EventIssueCreated || strings.HasPrefix(e.Type, "issue") || strings.HasPrefix(e.Type, "task:") {
			go func() {
				c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := s.st.enqueue(c, id, e.Type); err == nil {
					s.Kick()
				}
			}()
		}
	})
	go s.loop(ctx)
	slog.Info("orchestrator: started", "teams", len(s.snapshotTeams()), "disabled", s.disabled)
}

func (s *Service) Kick() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Service) snapshotTeams() []*Team {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Team, 0, len(s.teams))
	for _, t := range s.teams {
		out = append(out, t)
	}
	return out
}

func (s *Service) loop(ctx context.Context) {
	rec := time.NewTicker(s.opt.Reconcile)
	defer rec.Stop()
	s.reconcile(ctx) // startup: catch up on whatever happened while down
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
			time.Sleep(s.opt.Debounce) // coalesce bursts (one update emits several events)
			ids, err := s.st.claim(ctx)
			if err != nil {
				s.fail("claim", err)
				continue
			}
			for _, id := range ids {
				go s.processID(ctx, id, "event")
			}
		case <-rec.C:
			s.reconcile(ctx)
		}
		s.mu.Lock()
		s.lastBeat = time.Now()
		s.mu.Unlock()
	}
}

func (s *Service) fail(what string, err error) {
	slog.Warn("orchestrator: "+what, "err", err)
	s.mu.Lock()
	s.lastErr = what + ": " + err.Error()
	s.mu.Unlock()
}

// Status is the health view (heartbeat for the watchdog / `multica team status`).
func (s *Service) Status() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]any{"last_beat": s.lastBeat, "last_error": s.lastErr, "teams": len(s.teams), "disabled": s.disabled, "service_user_id": s.svcUserID}
}

func (s *Service) reconcile(ctx context.Context) {
	if err := s.reload(ctx); err != nil {
		s.fail("reload", err)
		return
	}
	var wg sync.WaitGroup
	for _, t := range s.snapshotTeams() {
		wg.Add(1)
		go func(t *Team) { // projects in parallel, issues of one project serially
			defer wg.Done()
			unlock := s.lockKey("p:" + t.ProjectID)
			defer unlock()
			rows, err := s.st.projectIssues(ctx, t.ProjectID)
			if err != nil {
				s.fail("reconcile list", err)
				return
			}
			for i := range rows {
				if rows[i].Status == StDone || rows[i].Status == StCancelled {
					continue
				}
				s.process(ctx, t, rows, &rows[i], "reconcile")
			}
			s.remindBlocked(ctx, t, rows)
		}(t)
	}
	wg.Wait()
	s.mu.Lock()
	s.lastBeat = time.Now()
	s.mu.Unlock()
}

func (s *Service) processID(ctx context.Context, issueID, source string) {
	row, err := s.st.issue(ctx, issueID)
	if err != nil || row == nil {
		return
	}
	t := s.Team(row.ProjectID)
	if t == nil {
		return
	}
	unlock := s.lockKey("p:" + t.ProjectID)
	defer unlock()
	if row, err = s.st.issue(ctx, issueID); err != nil || row == nil {
		return // re-read under the lock: the queued event may be stale
	}
	rows, err := s.st.projectIssues(ctx, t.ProjectID)
	if err != nil {
		s.fail("list", err)
		return
	}
	s.process(ctx, t, rows, row, source)
}

// pausedWhy is the kill switch: global env, team pause, or a pause label on any
// open issue of the project.
func (s *Service) pausedWhy(t *Team, rows []issueRow) string {
	if s.disabled {
		return "MULTICA_ORCHESTRATOR_DISABLED=1"
	}
	if t.Paused {
		return "team paused: " + t.PausedReason
	}
	for _, r := range rows {
		if r.Status != StDone && r.Status != StCancelled {
			for _, l := range r.Labels {
				if l == "orchestrator-pause" {
					return "label orchestrator-pause on " + r.Key
				}
			}
		}
	}
	return ""
}

// Snapshot builds the engine input for one issue (exported for `flow show`).
func (s *Service) snapshot(ctx context.Context, t *Team, rows []issueRow, row *issueRow) (*Snapshot, error) {
	sn := &Snapshot{Now: time.Now(), Issue: row.Issue, FreeGB: -1}
	for _, r := range rows {
		sn.Siblings = append(sn.Siblings, r.Issue)
	}
	var err error
	if sn.ActiveRun, sn.LastRun, err = s.st.runs(ctx, row.ID); err != nil {
		return nil, err
	}
	if StartsHere(row.Status, row.Meta) {
		if sn.Busy, err = s.st.busyOwners(ctx, t.WorkspaceID); err != nil {
			return nil, err
		}
		// T1 decisions in one pass must see the issues started earlier in the
		// same pass (resource slots), so re-read the siblings here.
		fresh, ferr := s.st.projectIssues(ctx, t.ProjectID)
		if ferr != nil {
			return nil, ferr
		}
		sn.Siblings = sn.Siblings[:0]
		for _, r := range fresh {
			sn.Siblings = append(sn.Siblings, r.Issue)
		}
		sn.FreeGB = freeGB(diskPath(t.Spec))
	}
	switch row.Status {
	case StTodo, StInProgress, StInReview:
		if sn.ActiveRun == nil {
			if sn.Head, err = s.repo.head(ctx, t.Spec, row.Key); err != nil {
				return nil, err
			}
			sn.SamePatch = func(a, b string) bool { return s.repo.samePatch(ctx, t, a, b) }
		}
	}
	return sn, nil
}

func (s *Service) process(ctx context.Context, t *Team, rows []issueRow, row *issueRow, source string) {
	if why := s.pausedWhy(t, rows); why != "" {
		return
	}
	now := time.Now()
	if !s.st.backoffOK(ctx, row.ID, now) {
		return
	}
	if StartsHere(row.Status, row.Meta) {
		// owner selection reads workspace-wide busy state: serialize T1 per workspace
		defer s.lockKey("w:" + t.WorkspaceID)()
	}
	d, err := s.decide(ctx, t, rows, row)
	if err == nil && d != nil {
		if d.Tid == "" {
			if d.Wait != "" && s.st.setWait(ctx, row.ID, d.Wait) {
				slog.Info("orchestrator: waiting", "issue", row.Key, "why", d.Wait)
			}
			return
		}
		if t.Spec.Shadow {
			if s.st.shadow(ctx, t, row, d, source) {
				slog.Info("orchestrator: shadow", "issue", row.Key, "tid", d.Tid, "status", d.Status, "assignee", d.Assignee, "why", d.Why)
			}
			return
		}
		err = s.apply(ctx, t, row, d, source)
	}
	if err == nil {
		s.st.clearFailure(ctx, row.ID)
		return
	}
	n, wait := s.st.recordFailure(ctx, row.ID, err, s.opt.BackoffBase, s.opt.BackoffCap)
	slog.Warn("orchestrator: transition failed", "issue", row.Key, "n", n, "backoff", wait, "err", err)
	if n >= t.Spec.Limits.Failures {
		why := fmt.Sprintf("dead-letter: %d consecutive orchestrator failures (%s)", n, short(err.Error(), 120))
		sn := &Snapshot{Issue: row.Issue}
		bd := t.Spec.block(sn, why, nil)
		bd.Notify = "DEAD-LETTER " + bd.Notify
		bd.Tid = "T6_block"
		bd.Handoff = t.Spec.BuildHandoff(sn, bd)
		if e2 := s.apply(ctx, t, row, bd, "dead-letter"); e2 == nil {
			s.st.clearFailure(ctx, row.ID)
		} else {
			s.fail("dead-letter block", e2)
		}
	}
}

func (s *Service) decide(ctx context.Context, t *Team, rows []issueRow, row *issueRow) (d *Decision, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("engine panic: %v", r)
		}
	}()
	sn, err := s.snapshot(ctx, t, rows, row)
	if err != nil {
		return nil, err
	}
	probe := s.repo.prober(ctx, t)
	if t.Spec.Shadow {
		probe = shadowProbe
	}
	d = t.Spec.Decide(sn, probe)
	if d != nil && d.Tid != "" {
		d.Handoff = t.Spec.BuildHandoff(sn, d)
	}
	return d, nil
}

// faultHook lets tests / F-scenarios inject a failure mid-transition
// (MULTICA_ORCHESTRATOR_FAULT=<tid>:<count>).
var faultMu sync.Mutex
var faultLeft = map[string]int{}

func injected(tid string) error {
	faultMu.Lock()
	defer faultMu.Unlock()
	for _, spec := range strings.Split(os.Getenv("MULTICA_ORCHESTRATOR_FAULT"), ",") {
		parts := strings.SplitN(strings.TrimSpace(spec), ":", 2)
		if parts[0] == "" || (parts[0] != tid && parts[0] != "*") {
			continue
		}
		if _, ok := faultLeft[spec]; !ok {
			n := 1
			if len(parts) == 2 {
				fmt.Sscan(parts[1], &n)
			}
			faultLeft[spec] = n
		}
		if faultLeft[spec] <= 0 {
			continue
		}
		faultLeft[spec]--
		return fmt.Errorf("injected fault in %s", tid)
	}
	return nil
}

// apply is the actuator: transition log + metadata in one transaction, then
// the status/assignee write through the issue API (dispatches the run), the
// cancel/rerun calls, and the notification. A crash between the two halves
// leaves phase ahead of status, which T9 converges on the next pass.
func (s *Service) apply(ctx context.Context, t *Team, row *issueRow, d *Decision, source string) (err error) {
	if d.Handoff != nil && !t.Spec.Shadow {
		s.enrichHandoff(ctx, t, row, d.Handoff)
	}
	logID, fresh, err := s.st.begin(ctx, t, row, d, source)
	if err != nil {
		return err
	}
	if !fresh {
		return nil
	}
	defer func() { s.st.finish(ctx, logID, err) }()
	slog.Info("orchestrator: transition", "issue", row.Key, "tid", d.Tid, "status", d.Status, "assignee", d.Assignee, "why", d.Why, "source", source)
	if err = injected(d.Tid); err != nil {
		return err
	}
	if d.StartsRun() {
		// The daemon cuts the task worktree from the hub: make it current first.
		s.repo.refreshHub(ctx, t.Spec, false)
	}
	if d.CancelRun != "" {
		if err = s.call(ctx, t.WorkspaceID, "POST", fmt.Sprintf("/api/issues/%s/tasks/%s/cancel", row.ID, d.CancelRun), nil); err != nil {
			return err
		}
	}
	body := map[string]any{}
	if d.Status != "" && d.Status != row.Status {
		body["status"] = d.Status
	}
	if d.AssigneeKind != "" {
		typ, id := d.AssigneeKind, ""
		if d.Assignee == "@human" {
			id = t.HumanUserID
		} else if id, err = s.st.agentID(ctx, t.WorkspaceID, d.Assignee); err != nil {
			return err
		}
		if id != "" && !(row.AssigneeType == typ && (row.Assignee == d.Assignee || row.Assignee == id)) {
			body["assignee_type"], body["assignee_id"] = typ, id
		}
	}
	if len(body) > 0 {
		if err = s.call(ctx, t.WorkspaceID, "PUT", "/api/issues/"+row.ID, body); err != nil {
			return err
		}
	} else if len(d.Meta) > 0 {
		s.publishMeta(ctx, row)
	}
	if d.Rerun {
		if err = s.call(ctx, t.WorkspaceID, "POST", "/api/issues/"+row.ID+"/rerun", map[string]any{}); err != nil {
			return err
		}
	}
	s.activity(ctx, row, d, source)
	if d.Notify != "" {
		s.notify(ctx, t, row, d)
	}
	return nil
}

// activity interleaves the transition into the issue timeline (activity_log,
// action orchestrator_transition) so people see why the issue moved where it
// happened, not only in `multica flow log`.
func (s *Service) activity(ctx context.Context, row *issueRow, d *Decision, source string) {
	dm := map[string]any{"tid": d.Tid, "why": short(d.Why, 300), "status": d.Status, "assignee": d.Assignee, "source": source}
	if d.Handoff != nil {
		dm["handoff"] = d.Handoff // the issue chat renders it as a card
	}
	det, _ := json.Marshal(dm)
	var actor any
	if s.svcUserID != "" {
		actor = s.svcUserID
	}
	if _, err := s.opt.Pool.Exec(ctx, `INSERT INTO activity_log (workspace_id, issue_id, actor_type, actor_id, action, details) VALUES ($1, $2, 'member', $3, 'orchestrator_transition', $4)`, row.Workspace, row.ID, actor, det); err != nil {
		slog.Warn("orchestrator: activity log", "issue", row.Key, "err", err)
	}
}

func (s *Service) publishMeta(ctx context.Context, row *issueRow) {
	var raw []byte
	var rev int64
	if err := s.opt.Pool.QueryRow(ctx, `SELECT metadata, revision FROM issue WHERE id=$1`, row.ID).Scan(&raw, &rev); err != nil {
		return
	}
	m := map[string]any{}
	_ = json.Unmarshal(raw, &m)
	s.opt.Bus.Publish(events.Event{Type: protocol.EventIssueMetadataChanged, WorkspaceID: row.Workspace, ActorType: "member", ActorID: s.svcUserID,
		Payload: map[string]any{"issue_id": row.ID, "metadata": m, "issue_revision": rev}})
}

// call performs an API request in-process as the service member.
func (s *Service) call(ctx context.Context, workspaceID, method, path string, body any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	}
	req := httptest.NewRequest(method, path, rd).WithContext(ctx)
	req.RemoteAddr = "127.0.0.1:1"
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("X-Workspace-ID", workspaceID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Multica-Client", "orchestrator")
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	if rec.Code >= 300 {
		return fmt.Errorf("%s %s: %d %s", method, path, rec.Code, short(rec.Body.String(), 300))
	}
	return nil
}

// notify writes a native inbox item (type orchestrator_alert) to the team's human.
func (s *Service) notify(ctx context.Context, t *Team, row *issueRow, d *Decision) {
	if t.HumanUserID == "" || s.opt.Queries == nil {
		return
	}
	sev := "attention"
	if d.Tid == "T10_await_acceptance" || strings.HasPrefix(d.Notify, "DEAD-LETTER") {
		sev = "action_required"
	}
	details, _ := json.Marshal(map[string]any{"tid": d.Tid, "why": d.Why, "issue_key": row.Key})
	item, ok, err := inbox.CreateIfEligible(ctx, s.opt.Queries, db.CreateInboxItemParams{
		ID: dbid.NewV7(), WorkspaceID: util.MustParseUUID(t.WorkspaceID), RecipientType: "member", RecipientID: util.MustParseUUID(t.HumanUserID),
		Type: InboxType, Severity: sev, IssueID: util.MustParseUUID(row.ID), Title: row.Key + ": " + row.Title,
		Body:      pgtype.Text{String: d.Notify, Valid: true},
		ActorType: pgtype.Text{String: "system", Valid: true}, Details: details,
	})
	if err != nil || !ok {
		if err != nil {
			s.fail("notify", err)
		}
		return
	}
	s.opt.Bus.Publish(events.Event{Type: protocol.EventInboxNew, WorkspaceID: t.WorkspaceID, ActorType: "system",
		Payload: map[string]any{"item": map[string]any{
			"id": util.UUIDToString(item.ID), "workspace_id": t.WorkspaceID, "recipient_type": item.RecipientType,
			"recipient_id": util.UUIDToString(item.RecipientID), "type": item.Type, "severity": item.Severity,
			"issue_id": row.ID, "title": item.Title, "body": d.Notify, "read": false, "archived": false,
			"created_at": util.TimestampToString(item.CreatedAt), "actor_type": "system", "details": json.RawMessage(details),
		}}})
}

func (s *Service) remindBlocked(ctx context.Context, t *Team, rows []issueRow) {
	if t.Spec.Shadow {
		return
	}
	every := time.Duration(t.Spec.Limits.BlockedRemindMin) * time.Minute
	for i := range rows {
		r := &rows[i]
		if r.Status != StBlocked || time.Since(r.UpdatedAt) < every {
			continue
		}
		if s.st.remindDue(ctx, r.ID, every) {
			s.notify(ctx, t, r, &Decision{Tid: "R1_blocked_reminder", Notify: fmt.Sprintf("REMINDER %s blocked for %d min: %s — `multica flow unblock %s` or cancel it", r.Key, int(time.Since(r.UpdatedAt).Minutes()), r.Meta["blocked_reason"], r.Key)})
		}
	}
}

// --- service member -----------------------------------------------------------

func (s *Service) ensureServiceUser(ctx context.Context) error {
	var id string
	err := s.opt.Pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Orchestrator', $1)
		ON CONFLICT (email) DO UPDATE SET name=EXCLUDED.name RETURNING id::text`, ServiceUserEmail).Scan(&id)
	if err != nil {
		return err
	}
	raw := make([]byte, 20)
	_, _ = rand.Read(raw)
	tok := "mul_" + hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(tok))
	// One live token: revoke older ones, mint a fresh in-memory one per boot.
	_, _ = s.opt.Pool.Exec(ctx, `DELETE FROM personal_access_token WHERE user_id=$1 AND name='orchestrator (in-process)'`, id)
	if _, err := s.opt.Pool.Exec(ctx, `INSERT INTO personal_access_token (user_id, name, token_hash, token_prefix, expires_at)
		VALUES ($1, 'orchestrator (in-process)', $2, $3, now() + interval '3650 days')`, id, hex.EncodeToString(sum[:]), tok[:8]); err != nil {
		return err
	}
	s.mu.Lock()
	s.svcUserID, s.token = id, tok
	s.mu.Unlock()
	return nil
}

func (s *Service) ensureMember(ctx context.Context, workspaceID string) error {
	_, err := s.opt.Pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'admin') ON CONFLICT (workspace_id, user_id) DO NOTHING`, workspaceID, s.svcUserID)
	return err
}

// issueIDOf extracts the issue id from a bus event payload.
func issueIDOf(e events.Event) string {
	b, err := json.Marshal(e.Payload)
	if err != nil {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return ""
	}
	if v, ok := m["issue_id"].(string); ok && v != "" {
		return v
	}
	for _, k := range []string{"issue", "task"} {
		if x, ok := m[k].(map[string]any); ok {
			if k == "issue" {
				if v, ok := x["id"].(string); ok {
					return v
				}
			}
			if v, ok := x["issue_id"].(string); ok && v != "" {
				return v
			}
		}
	}
	if strings.HasPrefix(e.Type, "issue:") {
		if v, ok := m["id"].(string); ok {
			return v
		}
	}
	return ""
}

var errNotFound = errors.New("not found")

func contextBG() context.Context { return context.Background() }

// lockKey is a named mutex (per project / per workspace).
func (s *Service) lockKey(k string) func() {
	m, _ := s.locks.LoadOrStore(k, &sync.Mutex{})
	mx := m.(*sync.Mutex)
	mx.Lock()
	return mx.Unlock
}
