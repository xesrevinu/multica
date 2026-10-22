package orchestrator

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

// Routes is mounted at /api/orchestrator behind RequireWorkspaceMember and
// RequireHumanActor: agents never drive the flow through this API.
func (s *Service) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/status", s.hStatus)
	r.Get("/teams", s.hTeams)
	r.Post("/teams/diff", s.hDiff)
	r.Put("/teams", s.hApply)
	r.Post("/teams/{project}/pause", s.hPause(true))
	r.Post("/teams/{project}/resume", s.hPause(false))
	r.Delete("/teams/{project}", s.hDetach)
	r.Get("/flow/{key}", s.hShow)
	r.Get("/flow/{key}/log", s.hLog)
	r.Post("/flow/{key}/unblock", s.hMeta(StBlocked, "", map[string]string{"phase": PhUnblocked, "unblock_confirmed": "true"}))
	r.Post("/flow/{key}/cancel", s.hCancel)
	r.Post("/flow/{key}/pause", s.hFlowPause(true))
	r.Post("/flow/{key}/resume", s.hFlowPause(false))
	r.Post("/flow/{key}/accept", s.hMeta("", PhAwaitAcceptance, map[string]string{"acceptance": "ok"}))
	r.Post("/flow/{key}/reject", s.hMeta("", PhAwaitAcceptance, map[string]string{"acceptance": "reject"}))
	r.Post("/flow/{key}/replay", s.hReplay)
	r.Post("/flow/{key}/resolve-conflict", s.hResolveConflict)
	r.Get("/issues", s.hIssues)
	r.Get("/issues/{key}/timeline", s.hIssueTimeline)
	r.Get("/issues/{key}/pr", s.hPR)
	r.Post("/issues/{key}/pr/update-branch", s.hPROp("update-branch"))
	r.Post("/issues/{key}/pr/rerun-checks", s.hPROp("rerun-checks"))
	r.Post("/issues/{key}/pr/merge", s.hPROp("merge"))
	r.Get("/issues/{key}/prs", s.hPRList)
	r.Get("/issues/{key}/pr/files", s.hPRFiles)
	r.Post("/issues/{key}/pr/sync-labels", s.hPRManage("sync-labels"))
	r.Post("/issues/{key}/pr/delete-branch", s.hPRManage("delete-branch"))
	r.Get("/worktrees", s.hWorktrees)
	r.Post("/worktrees/remove", s.hWorktreeRemove)
	return r
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, errCode, msg string) {
	writeJSON(w, code, map[string]any{"error": msg, "code": errCode})
}

func wsID(r *http.Request) string { return middleware.WorkspaceIDFromContext(r.Context()) }

func isAdmin(r *http.Request) bool {
	m, ok := middleware.MemberFromContext(r.Context())
	return ok && (m.Role == "owner" || m.Role == "admin")
}

func (s *Service) resolveProject(r *http.Request, ref string) (string, error) {
	var id string
	err := s.opt.Pool.QueryRow(r.Context(), `SELECT id::text FROM project WHERE workspace_id=$1 AND (id::text=$2 OR lower(title)=lower($2)) LIMIT 1`, wsID(r), ref).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("project %q not found", ref)
	}
	return id, nil
}

func (s *Service) hStatus(w http.ResponseWriter, r *http.Request) {
	st := s.Status()
	var mine []map[string]any
	for _, t := range s.snapshotTeams() {
		if t.WorkspaceID == wsID(r) {
			mine = append(mine, map[string]any{"project_id": t.ProjectID, "paused": t.Paused, "paused_reason": t.PausedReason})
		}
	}
	st["workspace_teams"] = mine
	writeJSON(w, 200, st)
}

func (s *Service) hTeams(w http.ResponseWriter, r *http.Request) {
	out := []map[string]any{}
	for _, t := range s.snapshotTeams() {
		if t.WorkspaceID == wsID(r) {
			out = append(out, map[string]any{"project_id": t.ProjectID, "paused": t.Paused, "paused_reason": t.PausedReason,
				"human_user_id": t.HumanUserID, "spec": t.Spec, "updated_at": t.UpdatedAt})
		}
	}
	writeJSON(w, 200, map[string]any{"teams": out})
}

type applyReq struct {
	Project  string `json:"project"`
	SpecYAML string `json:"spec_yaml"`
	Human    string `json:"human,omitempty"`
}

func (s *Service) parseApply(w http.ResponseWriter, r *http.Request) (*applyReq, string, *Spec, bool) {
	var req applyReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "bad_request", "invalid JSON")
		return nil, "", nil, false
	}
	pid, err := s.resolveProject(r, req.Project)
	if err != nil {
		writeErr(w, 404, "not_found", err.Error())
		return nil, "", nil, false
	}
	sp, err := ParseSpec([]byte(req.SpecYAML))
	if err != nil {
		writeErr(w, 422, "invalid_spec", err.Error())
		return nil, "", nil, false
	}
	seen := map[string]bool{}
	var names []string
	addAgent := func(a string) {
		if a == "" || seen[a] {
			return
		}
		seen[a] = true
		names = append(names, a)
	}
	addAgent(sp.Roles.Lead)
	addAgent(sp.Roles.Reviewer)
	for _, a := range sp.Roles.Owners {
		addAgent(a)
	}
	for _, e := range sp.Experts {
		for _, a := range e.Agents {
			addAgent(a)
		}
	}
	for _, a := range names {
		if _, err := s.st.agentID(r.Context(), wsID(r), a); err != nil {
			writeErr(w, 422, "invalid_spec", err.Error())
			return nil, "", nil, false
		}
	}
	return &req, pid, sp, true
}

// diffSpecs lists top-level differences as "path: old → new".
func diffSpecs(a, b *Spec) []string {
	if a == nil {
		return []string{"(new team)"}
	}
	var out []string
	am, bm := map[string]any{}, map[string]any{}
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	_ = json.Unmarshal(ja, &am)
	_ = json.Unmarshal(jb, &bm)
	for _, k := range []string{"preset", "roles", "experts", "repo", "limits", "acceptance", "merge", "transitions"} {
		if !reflect.DeepEqual(am[k], bm[k]) {
			x, _ := json.Marshal(am[k])
			y, _ := json.Marshal(bm[k])
			out = append(out, fmt.Sprintf("%s: %s → %s", k, short(string(x), 400), short(string(y), 400)))
		}
	}
	return out
}

func (s *Service) hDiff(w http.ResponseWriter, r *http.Request) {
	_, pid, sp, ok := s.parseApply(w, r)
	if !ok {
		return
	}
	var cur *Spec
	if t := s.Team(pid); t != nil {
		cur = t.Spec
	}
	writeJSON(w, 200, map[string]any{"project_id": pid, "changes": diffSpecs(cur, sp), "spec": sp})
}

func (s *Service) hApply(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeErr(w, 403, "forbidden", "only workspace owners/admins can apply a team")
		return
	}
	req, pid, sp, ok := s.parseApply(w, r)
	if !ok {
		return
	}
	human := r.Header.Get("X-User-ID")
	if req.Human != "" || sp.Roles.Human != "" {
		ref := req.Human
		if ref == "" {
			ref = sp.Roles.Human
		}
		if err := s.opt.Pool.QueryRow(r.Context(), `SELECT u.id::text FROM "user" u JOIN member m ON m.user_id=u.id AND m.workspace_id=$1 WHERE u.id::text=$2 OR lower(u.email)=lower($2) OR lower(u.name)=lower($2) LIMIT 1`, wsID(r), ref).Scan(&human); err != nil {
			writeErr(w, 422, "invalid_spec", fmt.Sprintf("human %q is not a member of this workspace", ref))
			return
		}
	}
	var cur *Spec
	if t := s.Team(pid); t != nil {
		cur = t.Spec
	}
	_, err := s.opt.Pool.Exec(r.Context(), `INSERT INTO orchestrator_team (project_id, workspace_id, spec, spec_yaml, human_user_id, applied_by)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (project_id) DO UPDATE SET spec=$3, spec_yaml=$4, human_user_id=$5, applied_by=$6, updated_at=now()`,
		pid, wsID(r), sp.JSON(), req.SpecYAML, human, r.Header.Get("X-User-ID"))
	if err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	// pr_merge_status=none semantics: the orchestrator, not the PR webhook,
	// moves an orchestrated issue to done (see handler/pr_auto_complete.go).
	_ = s.ensureMember(r.Context(), wsID(r))
	_ = s.reload(r.Context())
	go s.reconcile(contextBG()) // a changed limit (disk, owners) can unblock waiting issues now
	writeJSON(w, 200, map[string]any{"project_id": pid, "changes": diffSpecs(cur, sp), "human_user_id": human})
}

func (s *Service) hPause(pause bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pid, err := s.resolveProject(r, chi.URLParam(r, "project"))
		if err != nil {
			writeErr(w, 404, "not_found", err.Error())
			return
		}
		var body struct {
			Reason string `json:"reason"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Reason == "" && pause {
			body.Reason = "paused via API"
		}
		tag, err := s.opt.Pool.Exec(r.Context(), `UPDATE orchestrator_team SET paused=$2, paused_reason=$3, updated_at=now() WHERE project_id=$1 AND workspace_id=$4`, pid, pause, body.Reason, wsID(r))
		if err != nil || tag.RowsAffected() == 0 {
			writeErr(w, 404, "not_found", "project has no team")
			return
		}
		_ = s.reload(r.Context())
		if !pause {
			s.Kick()
			go s.reconcile(contextBG())
		}
		writeJSON(w, 200, map[string]any{"project_id": pid, "paused": pause})
	}
}

func (s *Service) hDetach(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeErr(w, 403, "forbidden", "only workspace owners/admins can detach a team")
		return
	}
	pid, err := s.resolveProject(r, chi.URLParam(r, "project"))
	if err != nil {
		writeErr(w, 404, "not_found", err.Error())
		return
	}
	_, _ = s.opt.Pool.Exec(r.Context(), `DELETE FROM orchestrator_team WHERE project_id=$1 AND workspace_id=$2`, pid, wsID(r))
	_ = s.reload(r.Context())
	writeJSON(w, 200, map[string]any{"project_id": pid, "detached": true})
}

func (s *Service) issueFor(w http.ResponseWriter, r *http.Request) (*issueRow, *Team, bool) {
	row, err := s.st.issueByKey(r.Context(), wsID(r), chi.URLParam(r, "key"))
	if err != nil || row == nil {
		writeErr(w, 404, "not_found", "issue not found")
		return nil, nil, false
	}
	t := s.Team(row.ProjectID)
	if t == nil {
		writeErr(w, 409, "not_orchestrated", row.Key+" is not in an orchestrated project")
		return nil, nil, false
	}
	return row, t, true
}

type logEntry struct {
	ID        int64           `json:"id"`
	Tid       string          `json:"tid"`
	State     string          `json:"state"`
	Error     string          `json:"error,omitempty"`
	Source    string          `json:"source"`
	Decision  json.RawMessage `json:"decision"`
	CreatedAt time.Time       `json:"created_at"`
}

func (s *Service) transitions(r *http.Request, issueID string, limit int) []logEntry {
	rows, err := s.opt.Pool.Query(r.Context(), `SELECT id, tid, state, error, source, decision, created_at FROM orchestrator_transition WHERE issue_id=$1 ORDER BY id DESC LIMIT $2`, issueID, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []logEntry
	for rows.Next() {
		var e logEntry
		if rows.Scan(&e.ID, &e.Tid, &e.State, &e.Error, &e.Source, &e.Decision, &e.CreatedAt) == nil {
			out = append(out, e)
		}
	}
	return out
}

func (s *Service) hShow(w http.ResponseWriter, r *http.Request) {
	row, t, ok := s.issueFor(w, r)
	if !ok {
		return
	}
	rows, _ := s.st.projectIssues(r.Context(), t.ProjectID)
	var next *Decision
	if sn, err := s.snapshot(r.Context(), t, rows, row); err == nil {
		next = t.Spec.Decide(sn, nil) // dry: no probes (no PR creation, no merge)
	}
	var fail struct {
		N    int
		Next *time.Time
		Err  string
		Wait string
	}
	_ = s.opt.Pool.QueryRow(r.Context(), `SELECT failures, next_at, last_error, last_wait FROM orchestrator_issue_state WHERE issue_id=$1`, row.ID).Scan(&fail.N, &fail.Next, &fail.Err, &fail.Wait)
	writeJSON(w, 200, map[string]any{
		"key": row.Key, "title": row.Title, "status": row.Status, "assignee_type": row.AssigneeType, "assignee": row.Assignee,
		"metadata": row.Meta, "labels": row.Labels, "needs_acceptance": t.Spec.NeedsAcceptance(&row.Issue),
		"paused": s.pausedWhy(t, rows), "next": next, "failures": fail.N, "backoff_until": fail.Next, "last_error": fail.Err, "waiting": fail.Wait,
		"recent": s.transitions(r, row.ID, 10), "diagram": t.Spec.Mermaid(row.Status),
	})
}

func (s *Service) hLog(w http.ResponseWriter, r *http.Request) {
	row, _, ok := s.issueFor(w, r)
	if !ok {
		return
	}
	writeJSON(w, 200, map[string]any{"key": row.Key, "transitions": s.transitions(r, row.ID, 200)})
}

// hMeta writes member-only flow controls as metadata; the engine acts on them.
func (s *Service) hMeta(needStatus, needPhase string, meta map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		row, t, ok := s.issueFor(w, r)
		if !ok {
			return
		}
		var body struct {
			Reason string `json:"reason"`
			Owner  string `json:"owner"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if needStatus != "" && row.Status != needStatus {
			writeErr(w, 409, "wrong_state", fmt.Sprintf("%s is %s, expected %s", row.Key, row.Status, needStatus))
			return
		}
		if needPhase != "" && row.Meta["phase"] != needPhase {
			writeErr(w, 409, "wrong_state", fmt.Sprintf("%s phase is %q, expected %s", row.Key, row.Meta["phase"], needPhase))
			return
		}
		m := map[string]string{}
		for k, v := range meta {
			m[k] = v
		}
		if m["acceptance"] == "reject" {
			if strings.TrimSpace(body.Reason) == "" {
				writeErr(w, 400, "bad_request", "reject needs --reason (the owner reads it)")
				return
			}
			m["acceptance"] = "reject: " + body.Reason
			// The owner reads blocked_reason on rework; without it agents went hunting for the text.
			m["blocked_reason"] = "acceptance rejected: " + body.Reason
		}
		if body.Owner != "" {
			if !t.Spec.isOwner(body.Owner) {
				writeErr(w, 400, "bad_request", body.Owner+" is not an owner of this team")
				return
			}
			m["owner"] = body.Owner
		}
		if body.Reason != "" && m["phase"] == PhUnblocked {
			m["unblock_note"] = body.Reason
		}
		s.writeFlowMeta(w, r, row, m)
	}
}

// writeFlowMeta merges member-written flow metadata, announces it and
// re-evaluates the issue.
func (s *Service) writeFlowMeta(w http.ResponseWriter, r *http.Request, row *issueRow, m map[string]string) {
	sj, _ := json.Marshal(m)
	if _, err := s.opt.Pool.Exec(r.Context(), `UPDATE issue SET metadata = metadata || $2::jsonb, revision=revision+1, updated_at=now() WHERE id=$1`, row.ID, sj); err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	s.publishMeta(r.Context(), row)
	_ = s.st.enqueue(r.Context(), row.ID, "flow:"+chi.URLParam(r, "key"))
	s.Kick()
	writeJSON(w, 200, map[string]any{"key": row.Key, "metadata": m, "accepted_by": r.Header.Get("X-User-ID")})
}

func settled(status string) bool { return status == StDone || status == StCancelled }

// hCancel: on a blocked issue it confirms the lead's cancel proposal (T8,
// the "确认取消" card action) or proposes-and-confirms in one step;
// any other open issue is cancelled at once (T8b), stopping its live run.
func (s *Service) hCancel(w http.ResponseWriter, r *http.Request) {
	row, _, ok := s.issueFor(w, r)
	if !ok {
		return
	}
	if settled(row.Status) {
		writeErr(w, 409, "wrong_state", fmt.Sprintf("%s is already %s", row.Key, row.Status))
		return
	}
	if row.Status == StBlocked {
		// a member's cancel confirms (or directly makes) the proposal: T8 runs.
		s.writeFlowMeta(w, r, row, map[string]string{"phase": PhCancelProposed, "cancel_confirmed": "true"})
		return
	}
	s.writeFlowMeta(w, r, row, map[string]string{"cancel_requested": "true"})
}

// hFlowPause pauses one issue: the engine only waits on it (a live run
// finishes, nothing new starts) until resumed.
func (s *Service) hFlowPause(pause bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		row, _, ok := s.issueFor(w, r)
		if !ok {
			return
		}
		if settled(row.Status) {
			writeErr(w, 409, "wrong_state", fmt.Sprintf("%s is already %s", row.Key, row.Status))
			return
		}
		v := ""
		if pause {
			v = "true"
		}
		s.writeFlowMeta(w, r, row, map[string]string{"paused": v})
	}
}

// hReplay clears backoff and re-evaluates the issue now (synchronously).
func (s *Service) hReplay(w http.ResponseWriter, r *http.Request) {
	row, t, ok := s.issueFor(w, r)
	if !ok {
		return
	}
	_, _ = s.opt.Pool.Exec(r.Context(), `UPDATE orchestrator_issue_state SET failures=0, next_at=NULL WHERE issue_id=$1`, row.ID)
	rows, _ := s.st.projectIssues(r.Context(), t.ProjectID)
	s.process(r.Context(), t, rows, row, "replay")
	after, _ := s.st.issue(r.Context(), row.ID)
	writeJSON(w, 200, map[string]any{"key": row.Key, "status": after.Status, "metadata": after.Meta, "recent": s.transitions(r, row.ID, 3), "by": util.UUIDToString(util.MustParseUUID(s.svcUserID))})
}

// hResolveConflict: the PR card's "Resolve conflicts" on an orchestrated
// issue. Asks the engine to run T11 now (reviewer + resolve-conflict skill);
// a member's request starts a fresh episode (attempts and hand-back reset).
func (s *Service) hResolveConflict(w http.ResponseWriter, r *http.Request) {
	row, t, ok := s.issueFor(w, r)
	if !ok {
		return
	}
	if t.Spec.Roles.Reviewer == "" || t.Spec.Limits.conflictLimit() < 0 {
		writeErr(w, 409, "conflict_resolution_off", "conflict resolution is off for this team (limits.conflict < 0 or no reviewer)")
		return
	}
	if settled(row.Status) || row.Status == StBlocked || row.Status == StBacklog {
		writeErr(w, 409, "wrong_state", fmt.Sprintf("%s is %s", row.Key, row.Status))
		return
	}
	if row.Meta["phase"] == PhResolvingConflict {
		writeErr(w, 409, "wrong_state", row.Key+" is already resolving conflicts")
		return
	}
	s.writeFlowMeta(w, r, row, map[string]string{"conflict_requested": "true", "attempt_conflict": "0", "conflict_handback": ""})
}
