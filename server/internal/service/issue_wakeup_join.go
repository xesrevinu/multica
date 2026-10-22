package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/attribution"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Runs are serialized per issue and agent, so a wakeup never runs alongside
// another run of its agent; it runs after it. Two cases would make that later
// run a repeat, and a firing that hits either one starts no run of its own:
//
//   - acknowledged: every input came from the agent itself, so it already
//     knows. An issue, comment, reaction or attachment event the agent
//     produced never wakes it. A satisfied condition its own unfinished run on
//     the issue brought about does not wake it either, when the rule is the
//     platform's or the agent set it up itself; a person's condition rule
//     carries an instruction the running agent does not have, so it still
//     runs afterwards.
//   - merged: a run of the agent that runs as the same person the rule's own
//     run would is waiting to start on the issue. The rule keeps its inputs,
//     and when a daemon that renders them claims that run it takes them along
//     (JoinWaitingWakeups): the instruction and facts ride in its prompt. The
//     inputs stay the rule's until that run starts; then the firing counts
//     like one that started a run (takenReceipts). If the run ends without
//     starting, or goes to an older daemon, the rule has its inputs back and
//     starts its own run.
const (
	wakeupOutcomeMerged       = "merged"
	wakeupOutcomeAcknowledged = "acknowledged"
	maxJoinedWakeupNote       = 12000
)

// receiptActor reads who produced an event receipt. Coalesced receipts keep
// only the latest actor, so they never count as the agent's own.
func receiptActor(r db.IssueWakeupReceipt) (actorType, actorID string, single bool) {
	var p struct {
		ActorType string `json:"actor_type"`
		ActorID   string `json:"actor_id"`
		AgentID   string `json:"agent_id"`
		Count     int64  `json:"coalesced_count"`
	}
	_ = json.Unmarshal([]byte(r.Payload), &p)
	if p.ActorType == "" && p.AgentID != "" {
		p.ActorType, p.ActorID = "agent", p.AgentID
	}
	return p.ActorType, p.ActorID, p.Count <= 1
}

// acknowledgedBySelf reports whether every input of this firing came from the
// target agent itself (see the rules above).
func acknowledgedBySelf(ctx context.Context, q *db.Queries, w db.IssueWakeup, agentID, issueID string, receipts []db.IssueWakeupReceipt) (bool, error) {
	if len(receipts) == 0 {
		return false, nil
	}
	agent := util.UUIDToString(agentID)
	ownRule := w.SystemRule.Valid
	if !ownRule && w.SourceTaskID.Valid {
		source, err := q.GetAgentTask(ctx, w.SourceTaskID.String)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
		ownRule = err == nil && source.AgentID == agentID
	}
	seen := map[string]bool{}
	var causes []string
	for _, r := range receipts {
		switch {
		case r.EventType == wakeupConditionEventType:
			if !ownRule {
				return false, nil
			}
			var p struct {
				SourceTaskIDs []string `json:"source_task_ids"`
			}
			_ = json.Unmarshal([]byte(r.Payload), &p)
			if len(p.SourceTaskIDs) == 0 {
				return false, nil
			}
			for _, raw := range p.SourceTaskIDs {
				id, err := util.ParseUUID(raw)
				if err != nil {
					return false, nil
				}
				if !seen[id] {
					seen[id] = true
					causes = append(causes, id)
				}
			}
		case strings.HasPrefix(r.EventType, "task."), r.EventType == "time.due", r.EventType == wakeupManualEventType, r.EventType == wakeupTimeoutEventType:
			// A run ending, a time, a person's "wake now" and a deadline are
			// never the agent's own action.
			return false, nil
		default:
			// A rule that names the agent itself as the actor asked for this.
			if w.FilterActorType.String == "agent" && w.FilterActorID.String == agentID {
				return false, nil
			}
			actorType, actorID, single := receiptActor(r)
			if !single || actorType != "agent" || actorID != agent {
				return false, nil
			}
		}
	}
	if len(causes) == 0 {
		return true, nil
	}
	active, err := q.CountActiveIssueRunsOfAgent(ctx, db.CountActiveIssueRunsOfAgentParams{Ids: causes, AgentID: agentID, IssueID: nullString(issueID)})
	if err != nil {
		return false, err
	}
	return int(active) == len(causes), nil
}

// hasWaitingRun reports whether a run of the agent that runs as this person
// is waiting to start on the issue. A firing then keeps its inputs for it.
func hasWaitingRun(ctx context.Context, q *db.Queries, issueID, agentID, runAs string) (bool, error) {
	if runAs == "" {
		return false, nil
	}
	_, err := q.FindWaitingIssueRun(ctx, db.FindWaitingIssueRunParams{IssueID: nullString(issueID), AgentID: agentID, OriginatorUserID: nullString(runAs)})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// childDoneRunAs is who a child_done run for this agent runs as: whoever
// caused the parent to exist, as a run the parent's own assignment would
// start.
func (s *IssueWakeupService) childDoneRunAs(ctx context.Context, issue db.Issue, agent db.Agent) (attribution.Result, error) {
	attr := s.Tasks.attributionForIssueTask(ctx, issue, "", attribution.SourceDelegation, "")
	return s.Tasks.applyAttributionFallback(ctx, attr, agent)
}

// joinedWakeupNote is what the run is told about a rule that joined it.
func joinedWakeupNote(w db.IssueWakeup, instruction string, receipts []db.IssueWakeupReceipt) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Wakeup %s fired while this run was waiting to start, so it did not start a run of its own; handle it in this run. Instruction:\n%s\nTrigger facts (read current state before deciding what to do):\n", util.UUIDToString(w.ID), instruction)
	for _, r := range receipts {
		line := r.EventType + " " + string(canonicalWakeupPayload([]byte(fmt.Sprint(r.Payload)))) + "\n"
		if b.Len()+len(line) > maxJoinedWakeupNote {
			b.WriteString(wakeupOmittedEvidence)
			break
		}
		b.WriteString(line)
	}
	return b.String()
}

type joinedWakeup struct {
	WakeupID string `json:"wakeup_id"`
	Revision int64  `json:"wakeup_revision"`
	Note     string `json:"note"`
}

// JoinWaitingWakeups hands a run being claimed the inputs of the wakeup rules
// that waited for it and returns the run's context as the claim should render
// it. Call it only for a daemon that renders wakeup_joined. The inputs are
// reserved for the run, not consumed (see takenReceipts). What an earlier
// claim of the same run reserved is checked again, so a rule turned off,
// changed or no longer allowed since then drops out and gets its inputs back.
// A rule another writer holds keeps what it had.
func (s *IssueWakeupService) JoinWaitingWakeups(ctx context.Context, task db.AgentTaskQueue) ([]byte, error) {
	if !task.IssueID.Valid || !task.OriginatorUserID.Valid || task.Status != "dispatched" {
		return []byte(task.Context.String), nil
	}
	var stored struct {
		Joined []joinedWakeup `json:"wakeup_joined"`
		Chain  []string       `json:"wakeup_chain"`
	}
	_ = json.Unmarshal([]byte(task.Context.String), &stored)
	candidates, err := s.Tasks.Queries.ListWaitingWakeups(ctx, db.ListWaitingWakeupsParams{IssueID: task.IssueID.String, AgentID: nullString(task.AgentID)})
	if err != nil || (len(candidates) == 0 && len(stored.Joined) == 0) {
		return []byte(task.Context.String), err
	}
	previous := map[string]joinedWakeup{}
	var rules []string
	for _, entry := range stored.Joined {
		if id, err := util.ParseUUID(entry.WakeupID); err == nil {
			previous[id] = entry
			rules = append(rules, id)
		}
	}
	for _, id := range candidates {
		if _, ok := previous[id]; !ok {
			rules = append(rules, id)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, err := s.Tasks.TxStarter.BeginTx(ctx, nil)
	if err != nil {
		return []byte(task.Context.String), err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SET LOCAL lock_timeout = '50ms'"); err != nil {
		return []byte(task.Context.String), err
	}
	q := s.Tasks.Queries.WithTx(tx)
	issue, err := q.GetIssue(ctx, task.IssueID.String)
	if err != nil {
		return []byte(task.Context.String), err
	}
	active, err := wakeupIssueActive(ctx, q, issue)
	if err != nil {
		return []byte(task.Context.String), err
	}
	changed := len(previous) != len(stored.Joined)
	joined := make([]joinedWakeup, 0, len(rules))
	chain := stored.Chain
	for _, id := range rules {
		prev, had := previous[id]
		w, err := q.TryLockIssueWakeup(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			// Busy: keep what an earlier claim reserved. Deleted: nothing left.
			if _, err = q.LocklessWakeup(ctx, id); err != nil && !errors.Is(err, sql.ErrNoRows) {
				return []byte(task.Context.String), err
			}
			if had && err == nil {
				joined = append(joined, prev)
			} else if had {
				changed = true
			}
			continue
		}
		if err != nil {
			return []byte(task.Context.String), err
		}
		entry, ruleChain, err := s.reserveForRun(ctx, q, issue, active, task, w)
		if err != nil {
			return []byte(task.Context.String), err
		}
		if entry == nil {
			changed = changed || had
			continue
		}
		joined = append(joined, *entry)
		if !had || prev != *entry {
			changed = true
			chain = append(chain, ruleChain...)
		}
	}
	if !changed {
		return []byte(task.Context.String), nil
	}
	if len(chain) > wakeupChainLimit {
		chain = chain[len(chain)-wakeupChainLimit:]
	}
	values := map[string]json.RawMessage{}
	_ = json.Unmarshal([]byte(task.Context.String), &values)
	values["wakeup_joined"], _ = json.Marshal(joined)
	if len(chain) > 0 {
		values["wakeup_chain"], _ = json.Marshal(chain)
	}
	next, _ := json.Marshal(values)
	updated, err := q.SetClaimedTaskContext(ctx, db.SetClaimedTaskContextParams{Context: nullString(string(next)), ID: task.ID, DispatchedAt: task.DispatchedAt})
	if errors.Is(err, sql.ErrNoRows) {
		// A newer claim of this run answers instead.
		return []byte(task.Context.String), nil
	}
	if err != nil {
		return []byte(task.Context.String), err
	}
	if err = tx.Commit(); err != nil {
		return []byte(task.Context.String), err
	}
	return []byte(updated.Context.String), nil
}

// reserveForRun reserves one locked rule's pending inputs for the run and
// returns its entry, or releases what the run held and returns nil when the
// rule must not reach this run (or, for its own dispatch to decide, the inputs
// are the agent's own or close a loop).
func (s *IssueWakeupService) reserveForRun(ctx context.Context, q *db.Queries, issue db.Issue, active bool, task db.AgentTaskQueue, w db.IssueWakeup) (*joinedWakeup, []string, error) {
	pending, err := q.ListPendingWakeupReceipts(ctx, db.ListPendingWakeupReceiptsParams{WakeupID: w.ID, Revision: w.Revision})
	if err != nil {
		return nil, nil, err
	}
	var receipts []db.IssueWakeupReceipt
	var held, free []string
	for _, r := range pending {
		if len(w.Condition.String) > 0 && r.EventType != wakeupConditionEventType && r.EventType != wakeupTimeoutEventType && r.EventType != wakeupManualEventType {
			continue
		}
		switch {
		case r.TaskID.Valid && r.TaskID.String == task.ID:
			held = append(held, r.ID)
		case !r.TaskID.Valid || r.TaskID.String == "":
			free = append(free, r.ID)
		default:
			continue
		}
		receipts = append(receipts, r)
	}
	release := func() (*joinedWakeup, []string, error) {
		if len(held) == 0 {
			return nil, nil, nil
		}
		return nil, nil, q.ReleaseWakeupReceipts(ctx, held)
	}
	if len(receipts) == 0 || !active || w.DisabledAt.Valid || w.IssueID != task.IssueID.String {
		return release()
	}
	if _, err := q.FindPendingWakeupTask(ctx, nullString(util.UUIDToString(w.ID))); err == nil || !errors.Is(err, sql.ErrNoRows) {
		// The rule's own run takes its inputs.
		if err != nil {
			return nil, nil, err
		}
		return release()
	}
	instruction, ok, err := s.mayJoin(ctx, q, issue, task, w)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return release()
	}
	if self, err := acknowledgedBySelf(ctx, q, w, task.AgentID, issue.ID, receipts); err != nil || self {
		if err != nil {
			return nil, nil, err
		}
		return release()
	}
	chain, loop, err := wakeupChain(ctx, q, w, receipts)
	if err != nil {
		return nil, nil, err
	}
	if loop {
		return release()
	}
	if len(free) > 0 {
		if err = q.ReserveWakeupReceipts(ctx, db.ReserveWakeupReceiptsParams{TaskID: nullString(task.ID), Ids: free}); err != nil {
			return nil, nil, err
		}
	}
	return &joinedWakeup{WakeupID: util.UUIDToString(w.ID), Revision: w.Revision, Note: joinedWakeupNote(w, instruction, receipts)}, chain, nil
}

// mayJoin reports whether the run is one the rule's own run would be: the
// same agent, run as the same person, who may still use the agent. It returns
// the instruction the run gets.
func (s *IssueWakeupService) mayJoin(ctx context.Context, q *db.Queries, issue db.Issue, task db.AgentTaskQueue, w db.IssueWakeup) (string, bool, error) {
	agent, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: task.AgentID, WorkspaceID: w.WorkspaceID})
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if !w.SystemRule.Valid {
		if w.AgentID.String != task.AgentID || w.CreatedBy != task.OriginatorUserID || (w.Mode == "once" && w.LastTaskID.Valid) {
			return "", false, nil
		}
		if err := s.authorize(ctx, q, w.WorkspaceID, w.CreatedBy.String, agent); err != nil {
			if errors.Is(err, ErrWakeupForbidden) {
				err = nil
			}
			return "", false, err
		}
		return w.Instruction, true, nil
	}
	if (w.Enabled == 0) || issuestatus.Effective(ctx, q, issue.WorkspaceID, issue.Status) == "backlog" {
		return "", false, nil
	}
	target, err := resolveWakeTarget(ctx, q, issue)
	if err != nil || target.Agent.ID != task.AgentID {
		return "", false, err
	}
	runAs, err := s.childDoneRunAs(ctx, issue, agent)
	if err != nil || runAs.UserID != task.OriginatorUserID {
		return "", false, err
	}
	var settings []byte
	if ws, err := q.GetWorkspace(ctx, issue.WorkspaceID); err == nil {
		settings = []byte(ws.Settings)
	}
	return ChildDoneInstruction(w.Instruction, settings), true, nil
}

// takenRun is a run that took some of a rule's inputs along and has started.
type takenRun struct {
	task     db.AgentTaskQueue
	receipts []db.IssueWakeupReceipt
}

// takenReceipts sorts a rule's pending inputs by the run a claim reserved them
// for. It returns the free ones (including those of a run that ended without
// starting, which go back to the rule), whether a run that has not started
// yet still holds some, and the runs that started with some, whose inputs
// the caller consumes as a merged firing.
func takenReceipts(ctx context.Context, q *db.Queries, receipts []db.IssueWakeupReceipt) ([]db.IssueWakeupReceipt, bool, []takenRun, error) {
	byRun := map[string][]db.IssueWakeupReceipt{}
	var runs []string
	for _, r := range receipts {
		if r.TaskID.Valid {
			if _, ok := byRun[r.TaskID.String]; !ok {
				runs = append(runs, r.TaskID.String)
			}
			byRun[r.TaskID.String] = append(byRun[r.TaskID.String], r)
		}
	}
	if len(runs) == 0 {
		return receipts, false, nil, nil
	}
	released := map[string]bool{}
	holding := false
	var taken []takenRun
	for _, id := range runs {
		task, err := q.GetAgentTask(ctx, id)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil, err
		}
		switch {
		case err == nil && (task.StartedAt.Valid || task.Status == "running"):
			taken = append(taken, takenRun{task: task, receipts: byRun[id]})
		case err == nil && task.Status != "completed" && task.Status != "failed" && task.Status != "cancelled":
			holding = true
		default:
			ids := make([]string, 0, len(byRun[id]))
			for _, r := range byRun[id] {
				ids = append(ids, r.ID)
				released[r.ID] = true
			}
			if err = q.ReleaseWakeupReceipts(ctx, ids); err != nil {
				return nil, false, nil, err
			}
		}
	}
	free := make([]db.IssueWakeupReceipt, 0, len(receipts))
	for _, r := range receipts {
		if released[r.ID] {
			r.TaskID = nullString("")
		}
		if !r.TaskID.Valid {
			free = append(free, r)
		}
	}
	return free, holding, taken, nil
}

func receiptIDs(receipts []db.IssueWakeupReceipt) []string {
	ids := make([]string, 0, len(receipts))
	for _, r := range receipts {
		ids = append(ids, r.ID)
	}
	return ids
}

// JoinedWakeupNotes renders the wakeups that joined a run, for its prompt.
func JoinedWakeupNotes(context []byte) string {
	var stored struct {
		Joined []joinedWakeup `json:"wakeup_joined"`
	}
	if json.Unmarshal(context, &stored) != nil {
		return ""
	}
	notes := make([]string, 0, len(stored.Joined))
	for _, j := range stored.Joined {
		if strings.TrimSpace(j.Note) != "" {
			notes = append(notes, strings.TrimSpace(j.Note))
		}
	}
	return strings.Join(notes, "\n\n")
}
