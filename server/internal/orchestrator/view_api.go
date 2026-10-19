package orchestrator

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

const activeTaskStatuses = `('queued','dispatched','running','waiting_local_directory','deferred')`

// viewSQL selects one IssueView per issue. $1 = workspace id; callers append
// their own WHERE clause on alias i.
const viewSQL = `SELECT i.id::text, w.issue_prefix || '-' || i.number, i.number, i.title, i.status, i.priority,
	COALESCE(p.id::text,''), COALESCE(p.title,''), COALESCE(i.parent_issue_id::text,''),
	COALESCE(i.assignee_type,''), COALESCE(i.assignee_id::text,''), COALESCE(ag.name, u.name, ''),
	COALESCE((SELECT array_agg(l.name ORDER BY l.name) FROM issue_to_label il JOIN issue_label l ON l.id=il.label_id WHERE il.issue_id=i.id), '{}'),
	i.metadata, i.created_at, i.updated_at,
	COALESCE(ch.total,0), COALESCE(ch.done,0),
	COALESCE(ar.id::text,''), COALESCE(ar.agent_id::text,''), COALESCE(ar.agent,''), COALESCE(ar.status,''), ar.created_at, ar.started_at,
	COALESCE(lt.dir,''), COALESCE(lt.branch,''),
	COALESCE(pr.pr_number,0), COALESCE(pr.html_url,''), COALESCE(pr.title,''), COALESCE(pr.state,''), COALESCE(pr.branch,''), COALESCE(pr.head_sha,''),
	COALESCE(pr.additions,0), COALESCE(pr.deletions,0), COALESCE(pr.changed_files,0), COALESCE(pr.checks_rollup_state,''),
	COALESCE(pr.api_mergeable, pr.mergeable_state, ''), COALESCE(pr.api_merge_state_status,''), COALESCE(pr.repo_owner || '/' || pr.repo_name, '')
FROM issue i
JOIN workspace w ON w.id=i.workspace_id
LEFT JOIN project p ON p.id=i.project_id
LEFT JOIN agent ag ON ag.id=i.assignee_id AND i.assignee_type='agent'
LEFT JOIN "user" u ON u.id=i.assignee_id AND i.assignee_type='member'
LEFT JOIN LATERAL (SELECT count(*) AS total, count(*) FILTER (WHERE c.status IN ('done','cancelled')) AS done FROM issue c WHERE c.parent_issue_id=i.id) ch ON true
LEFT JOIN LATERAL (SELECT t.id, t.agent_id, a.name AS agent, t.status, t.created_at, t.started_at FROM agent_task_queue t JOIN agent a ON a.id=t.agent_id
	WHERE t.issue_id=i.id AND t.status IN ` + activeTaskStatuses + ` ORDER BY t.created_at DESC LIMIT 1) ar ON true
LEFT JOIN LATERAL (SELECT COALESCE(NULLIF(t.durable_work_dir,''), t.work_dir) AS dir, t.branch_name AS branch FROM agent_task_queue t
	WHERE t.issue_id=i.id AND COALESCE(NULLIF(t.durable_work_dir,''), t.work_dir, '') <> '' ORDER BY t.created_at DESC LIMIT 1) lt ON true
LEFT JOIN LATERAL (SELECT g.* FROM issue_pull_request ip JOIN github_pull_request g ON g.id=ip.pull_request_id
	WHERE ip.issue_id=i.id ORDER BY (g.state='open') DESC, g.pr_updated_at DESC LIMIT 1) pr ON true
WHERE i.workspace_id=$1 `

type rowScanner interface{ Scan(...any) error }

func (s *Service) scanView(sc rowScanner, userID string) (*IssueView, error) {
	v := &IssueView{}
	var projID, projTitle, aType, aID, aName string
	var raw []byte
	var runID, runAgentID, runAgent, runStatus, wtDir, wtBranch string
	var runCreated, runStarted *time.Time
	pr := &viewPR{}
	if err := sc.Scan(&v.ID, &v.Key, &v.Number, &v.Title, &v.Status, &v.Priority,
		&projID, &projTitle, &v.ParentID, &aType, &aID, &aName, &v.Labels, &raw, &v.CreatedAt, &v.UpdatedAt,
		&v.Children.Total, &v.Children.Done,
		&runID, &runAgentID, &runAgent, &runStatus, &runCreated, &runStarted, &wtDir, &wtBranch,
		&pr.Number, &pr.URL, &pr.Title, &pr.State, &pr.Branch, &pr.HeadSHA, &pr.Additions, &pr.Deletions, &pr.Files,
		&pr.Checks, &pr.Mergeable, &pr.MergeStat, &pr.Repo); err != nil {
		return nil, err
	}
	m := metaStrings(raw)
	v.meta, v.assigneeID = m, aID
	if v.Labels == nil {
		v.Labels = []string{}
	}
	if projID != "" {
		v.Project = &viewRef{ID: projID, Title: projTitle}
	}
	if aType != "" && aID != "" {
		v.Assignee = &viewAssignee{Type: aType, ID: aID, Name: aName}
	}
	if runID != "" {
		r := &viewRun{TaskID: runID, AgentID: runAgentID, Agent: runAgent, Status: runStatus, StartedAt: runStarted}
		if runCreated != nil {
			r.CreatedAt = *runCreated
		}
		v.Running = r
	}
	if pr.Number > 0 {
		v.PR = pr
	}
	v.Phase, v.Risk = m["phase"], m["risk"]
	v.Paused = m["paused"] == "true"
	v.Attempts = attemptsOf(m)
	if projID != "" {
		if t := s.Team(projID); t != nil {
			v.Orchestrated, v.Mode = true, t.Spec.Repo.Mode
			if wtBranch == "" {
				wtBranch = branchOf(t.Spec, v.Key)
			}
		}
	}
	if wtBranch == "" && v.PR != nil {
		wtBranch = v.PR.Branch
	}
	if wtDir != "" || (wtBranch != "" && (v.Orchestrated || v.PR != nil)) {
		v.Worktree = &viewWorktree{Path: wtDir, Branch: wtBranch}
	}
	v.Attention = attentionOf(v, userID)
	return v, nil
}

func (s *Service) listViews(ctx context.Context, wsID, userID, where string, args ...any) ([]*IssueView, error) {
	rows, err := s.opt.Pool.Query(ctx, viewSQL+where, append([]any{wsID}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*IssueView{}
	for rows.Next() {
		v, err := s.scanView(rows, userID)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// hIssues: GET /issues — every open issue of the workspace plus the most
// recently settled ones (?settled_limit, default 30), attention-sorted.
// Optional ?project=<id> narrows to one project.
func (s *Service) hIssues(w http.ResponseWriter, r *http.Request) {
	limit := 30
	if n, err := strconv.Atoi(r.URL.Query().Get("settled_limit")); err == nil && n >= 0 && n <= 500 {
		limit = n
	}
	where := `AND (i.status NOT IN ('done','cancelled') OR i.id IN (SELECT s.id FROM issue s WHERE s.workspace_id=$1 AND s.status IN ('done','cancelled') ORDER BY s.updated_at DESC LIMIT $2))`
	args := []any{limit}
	if pid := r.URL.Query().Get("project"); pid != "" {
		where += ` AND i.project_id::text=$3`
		args = append(args, pid)
	}
	vs, err := s.listViews(r.Context(), wsID(r), r.Header.Get("X-User-ID"), where, args...)
	if err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	sortIssueViews(vs)
	writeJSON(w, 200, map[string]any{"issues": vs})
}

func (s *Service) viewByKey(r *http.Request, key string) (*IssueView, error) {
	vs, err := s.listViews(r.Context(), wsID(r), r.Header.Get("X-User-ID"),
		`AND (i.id::text=$2 OR upper(w.issue_prefix || '-' || i.number)=upper($2))`, key)
	if err != nil || len(vs) == 0 {
		return nil, err
	}
	return vs[0], nil
}

type timelineTransition struct {
	ID        int64     `json:"id"`
	Tid       string    `json:"tid"`
	State     string    `json:"state"`
	Why       string    `json:"why,omitempty"`
	Status    string    `json:"status,omitempty"`
	Phase     string    `json:"phase,omitempty"`
	Assignee  string    `json:"assignee,omitempty"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// hIssueTimeline: GET /issues/{key}/timeline — the orchestrator side of the
// issue conversation (state header, runs, transitions). Comments stay on
// /api/issues/{id}/timeline so realtime patching and threads keep working.
func (s *Service) hIssueTimeline(w http.ResponseWriter, r *http.Request) {
	v, err := s.viewByKey(r, chi.URLParam(r, "key"))
	if err != nil || v == nil {
		writeErr(w, 404, "not_found", "issue not found")
		return
	}
	runs := []viewRun{}
	rows, err := s.opt.Pool.Query(r.Context(), `SELECT t.id::text, t.agent_id::text, COALESCE(a.name,''), t.status, t.created_at, t.started_at, t.completed_at, COALESCE(t.error,'')
		FROM agent_task_queue t LEFT JOIN agent a ON a.id=t.agent_id WHERE t.issue_id=$1 ORDER BY t.created_at LIMIT 300`, v.ID)
	if err == nil {
		for rows.Next() {
			var x viewRun
			if rows.Scan(&x.TaskID, &x.AgentID, &x.Agent, &x.Status, &x.CreatedAt, &x.StartedAt, &x.EndedAt, &x.Error) == nil {
				x.Error = short(x.Error, 400)
				runs = append(runs, x)
			}
		}
		rows.Close()
	}
	trs := []timelineTransition{}
	rows, err = s.opt.Pool.Query(r.Context(), `SELECT id, tid, state, COALESCE(decision->>'why',''), COALESCE(decision->>'status',''), COALESCE(decision->'meta'->>'phase',''),
		COALESCE(decision->>'assignee',''), error, created_at FROM orchestrator_transition WHERE issue_id=$1 ORDER BY id LIMIT 500`, v.ID)
	if err == nil {
		for rows.Next() {
			var x timelineTransition
			if rows.Scan(&x.ID, &x.Tid, &x.State, &x.Why, &x.Status, &x.Phase, &x.Assignee, &x.Error, &x.CreatedAt) == nil {
				trs = append(trs, x)
			}
		}
		rows.Close()
	}
	flow := map[string]string{}
	for _, k := range []string{"verdict", "reviewed_sha", "head_sha", "acceptance", "blocked_reason", "unblock_note", "owner", "merged_sha"} {
		if v.meta[k] != "" {
			flow[k] = v.meta[k]
		}
	}
	var waiting, lastErr string
	var backoff *time.Time
	_ = s.opt.Pool.QueryRow(r.Context(), `SELECT last_wait, last_error, next_at FROM orchestrator_issue_state WHERE issue_id=$1`, v.ID).Scan(&waiting, &lastErr, &backoff)
	writeJSON(w, 200, map[string]any{"issue": v, "flow": flow, "runs": runs, "transitions": trs,
		"waiting": waiting, "last_error": short(lastErr, 400), "backoff_until": backoff})
}
