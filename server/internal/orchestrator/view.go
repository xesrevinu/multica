package orchestrator

import (
	"sort"
	"strconv"
	"time"
)

// View models for the issue-centric web UI. Every optional part is a pointer
// or omitempty so issues without project, assignee, metadata, PR or runs
// serialize cleanly.

type viewRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type viewAssignee struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

type viewRun struct {
	TaskID    string     `json:"task_id"`
	AgentID   string     `json:"agent_id"`
	Agent     string     `json:"agent"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Error     string     `json:"error,omitempty"`
}

type viewPR struct {
	Number    int    `json:"number"`
	URL       string `json:"url"`
	Title     string `json:"title"`
	State     string `json:"state"`
	Branch    string `json:"branch,omitempty"`
	HeadSHA   string `json:"head_sha,omitempty"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Files     int    `json:"changed_files"`
	Checks    string `json:"checks,omitempty"`
	Mergeable string `json:"mergeable,omitempty"`
	MergeStat string `json:"merge_state,omitempty"`
	Repo      string `json:"repo"`
}

type viewWorktree struct {
	Path   string `json:"path,omitempty"`
	Branch string `json:"branch,omitempty"`
}

type viewAttention struct {
	Bucket string    `json:"bucket"` // needs_you | running | queued | settled
	Reason string    `json:"reason,omitempty"`
	Since  time.Time `json:"since"`
}

type viewAttempts struct {
	Impl   int `json:"impl"`
	Review int `json:"review"`
	Infra  int `json:"infra"`
}

type viewChildren struct {
	Total int `json:"total"`
	Done  int `json:"done"`
}

// IssueView is one row of GET /api/orchestrator/issues.
type IssueView struct {
	ID           string        `json:"id"`
	Key          string        `json:"key"`
	Number       int           `json:"number"`
	Title        string        `json:"title"`
	Status       string        `json:"status"`
	Priority     string        `json:"priority"`
	Project      *viewRef      `json:"project,omitempty"`
	ParentID     string        `json:"parent_id,omitempty"`
	Assignee     *viewAssignee `json:"assignee,omitempty"`
	Labels       []string      `json:"labels"`
	Orchestrated bool          `json:"orchestrated"`
	Mode         string        `json:"mode,omitempty"` // github | local (orchestrated only)
	Phase        string        `json:"phase,omitempty"`
	Risk         string        `json:"risk,omitempty"`
	Expert       string        `json:"expert,omitempty"` // metadata expert=<name>, read-only
	Paused       bool          `json:"paused"`
	Attempts     *viewAttempts `json:"attempts,omitempty"`
	Attention    viewAttention `json:"attention"`
	Running      *viewRun      `json:"running,omitempty"`
	PR           *viewPR       `json:"pr,omitempty"`
	Worktree     *viewWorktree `json:"worktree,omitempty"`
	Children     viewChildren  `json:"children"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
	meta         map[string]string
	assigneeID   string
}

var bucketRank = map[string]int{"needs_you": 0, "running": 1, "queued": 2, "settled": 3}

var priorityRank = map[string]int{"urgent": 0, "high": 1, "medium": 2, "low": 3, "none": 4, "": 4}

// attentionOf buckets an issue by what it needs from a human. Pure; unit tested.
func attentionOf(v *IssueView, userID string) viewAttention {
	a := viewAttention{Since: v.UpdatedAt}
	switch {
	case v.Status == StDone || v.Status == StCancelled:
		a.Bucket = "settled"
	case v.Phase == PhAwaitAcceptance:
		a.Bucket, a.Reason = "needs_you", "acceptance"
	case v.Phase == PhCancelProposed:
		a.Bucket, a.Reason = "needs_you", "cancel_proposed"
	case v.Status == StBlocked:
		a.Bucket, a.Reason = "needs_you", "blocked"
	case !v.Orchestrated && v.Status == StInReview && userID != "" && v.Assignee != nil && v.Assignee.Type == "member" && v.assigneeID == userID:
		a.Bucket, a.Reason = "needs_you", "review"
	case v.Paused:
		a.Bucket, a.Reason = "queued", "paused"
	case v.Running != nil || v.Status == StInProgress || v.Status == StInReview:
		a.Bucket = "running"
	default:
		a.Bucket = "queued"
	}
	return a
}

// sortIssueViews orders by bucket; needs_you oldest-waiting first, the rest
// by priority then most recently updated.
func sortIssueViews(vs []*IssueView) {
	sort.SliceStable(vs, func(i, j int) bool {
		a, b := vs[i], vs[j]
		if ra, rb := bucketRank[a.Attention.Bucket], bucketRank[b.Attention.Bucket]; ra != rb {
			return ra < rb
		}
		if a.Attention.Bucket == "needs_you" {
			return a.Attention.Since.Before(b.Attention.Since)
		}
		if a.Attention.Bucket != "settled" {
			if pa, pb := priorityRank[a.Priority], priorityRank[b.Priority]; pa != pb {
				return pa < pb
			}
		}
		return a.UpdatedAt.After(b.UpdatedAt)
	})
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

func attemptsOf(m map[string]string) *viewAttempts {
	a := &viewAttempts{Impl: atoi(m["attempt_impl"]), Review: atoi(m["attempt_review"]), Infra: atoi(m["attempt_infra"])}
	if a.Impl == 0 && a.Review == 0 && a.Infra == 0 {
		return nil
	}
	return a
}
