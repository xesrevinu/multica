package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/eventcontract"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

var ErrWakeupInput = errors.New("invalid wakeup")
var ErrWakeupConflict = errors.New("wakeup changed; refresh and retry")
var ErrWakeupForbidden = errors.New("wakeup permission denied")

// WakeupEventTypes is independent of plugin availability. These facts have
// transactional capture into matching receipts, including non-HTTP writers.
var WakeupEventTypes = eventcontract.WakeupTypes

type WakeupInput struct {
	AgentID         string     `json:"agent_id"`
	Instruction     string     `json:"instruction"`
	Kind            string     `json:"kind"`
	Mode            string     `json:"mode"`
	EventTypes      []string   `json:"event_types"`
	FilterAgentID   string     `json:"filter_agent_id"`
	FilterActorType string     `json:"filter_actor_type"`
	FilterActorID   string     `json:"filter_actor_id"`
	FilterTaskID    string     `json:"filter_task_id"`
	ParentCommentID string     `json:"parent_comment_id"`
	AfterSeconds    int64      `json:"after_seconds"`
	At              *time.Time `json:"at"`
	IntervalSeconds int64      `json:"interval_seconds"`
	CronExpression  string     `json:"cron_expression"`
	Timezone        string     `json:"timezone"`
	// A wakeup ends at an absolute deadline (expires_at) or after a relative
	// wait (expires_in_seconds) that restarts whenever the rule is re-enabled.
	// on_timeout is "end" (default) or, for event rules, "wake": run the target
	// once more to handle the missed deadline.
	ExpiresAt        *time.Time `json:"expires_at"`
	ExpiresInSeconds int64      `json:"expires_in_seconds"`
	OnTimeout        string     `json:"on_timeout"`
	// Condition is a platform-evaluated predicate (see WakeupCondition). The
	// rule stays kind=event; its hint events are derived from the condition.
	Condition json.RawMessage `json:"condition"`
	// MaxFires caps how many runs a repeating rule may start. Repeating event
	// rules default to wakeupDefaultMaxFires.
	MaxFires int32 `json:"max_fires"`
}

func hasWakeupCondition(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null"
}

const maxWakeupSeconds = 31536000

// Expiry validates and resolves the rule's end. A single-time wakeup ends
// when it fires, so it takes no deadline. Relative waits are stored as a
// duration so enabling the rule again restarts the wait from "now".
func (s *IssueWakeupService) Expiry(in *WakeupInput, now time.Time) (sql.NullString, sql.NullInt64, error) {
	bad := func(msg string) (sql.NullString, sql.NullInt64, error) {
		return sql.NullString{}, sql.NullInt64{}, fmt.Errorf("%w: %s", ErrWakeupInput, msg)
	}
	hasExpiry := in.ExpiresAt != nil || in.ExpiresInSeconds != 0
	switch {
	case in.OnTimeout != "" && in.OnTimeout != "end" && in.OnTimeout != "wake":
		return bad("on_timeout must be end or wake")
	case in.Kind == "at" && (hasExpiry || in.OnTimeout != ""):
		return bad("a single-time wakeup ends when it fires; it takes no deadline")
	case in.ExpiresAt != nil && in.ExpiresInSeconds != 0:
		return bad("provide expires_at or expires_in_seconds, not both")
	case !hasExpiry && in.OnTimeout != "":
		return bad("on_timeout requires a deadline")
	case in.OnTimeout == "wake" && in.Kind != "event":
		return bad("only event wakeups can wake the target on timeout")
	case !hasExpiry:
		return sql.NullString{}, sql.NullInt64{}, nil
	}
	if in.OnTimeout == "" {
		in.OnTimeout = "end"
	}
	if in.ExpiresInSeconds != 0 {
		if in.ExpiresInSeconds < 60 || in.ExpiresInSeconds > maxWakeupSeconds {
			return bad("expires_in_seconds must be between 60 and 31536000")
		}
		return nullTime(now.Add(time.Duration(in.ExpiresInSeconds)*time.Second), true), sql.NullInt64{Int64: in.ExpiresInSeconds, Valid: true}, nil
	}
	at := in.ExpiresAt.UTC()
	if !at.After(now) || at.Sub(now) > maxWakeupSeconds*time.Second {
		return bad("expires_at must be in the future and within one year")
	}
	return nullTime(at, true), sql.NullInt64{}, nil
}

type IssueWakeupService struct{ Tasks *TaskService }

func (s *IssueWakeupService) Validate(in *WakeupInput, now time.Time) (sql.NullString, error) {
	bad := func(msg string) (sql.NullString, error) {
		return sql.NullString{}, fmt.Errorf("%w: %s", ErrWakeupInput, msg)
	}
	in.Instruction = strings.TrimSpace(in.Instruction)
	if len(in.Instruction) == 0 || len(in.Instruction) > 12000 {
		return bad("instruction must contain 1–12000 bytes")
	}
	if in.Timezone == "" {
		in.Timezone = "UTC"
	}
	if _, err := time.LoadLocation(in.Timezone); err != nil {
		return bad("invalid timezone")
	}
	if in.Mode == "" {
		if in.Kind == "every" || in.Kind == "cron" {
			in.Mode = "continuous"
		} else {
			in.Mode = "once"
		}
	}
	if in.Mode != "once" && in.Mode != "continuous" {
		return bad("mode must be once or continuous")
	}
	if in.FilterActorType != "" || in.FilterActorID != "" {
		if in.Kind != "event" || (in.FilterActorType != "member" && in.FilterActorType != "agent") || in.FilterActorID == "" {
			return bad("actor filter requires an event, member or agent type, and actor ID")
		}
		if in.FilterAgentID != "" || in.FilterTaskID != "" {
			return bad("choose an actor filter or agent/run filters")
		}
		for _, event := range in.EventTypes {
			if strings.HasPrefix(event, "task.") {
				return bad("actor filters apply to issue, comment, reaction and attachment changes; use agent/run filters for task events")
			}
		}
	}
	if in.MaxFires != 0 && (in.MaxFires < 1 || in.MaxFires > 1000 || in.Mode != "continuous") {
		return bad("max_fires must be 1–1000 on a repeating rule")
	}
	if hasWakeupCondition(in.Condition) && in.Kind != "event" {
		return bad("conditions use kind event")
	}
	switch in.Kind {
	case "event":
		if hasWakeupCondition(in.Condition) {
			if len(in.EventTypes) > 0 || in.FilterAgentID != "" || in.FilterTaskID != "" || in.FilterActorType != "" || in.FilterActorID != "" {
				return bad("a condition cannot be combined with events or filters")
			}
			if in.AfterSeconds != 0 || in.At != nil || in.IntervalSeconds != 0 || in.CronExpression != "" {
				return bad("event wakeups cannot contain a schedule")
			}
			return sql.NullString{}, nil
		}
		if len(in.EventTypes) == 0 || len(in.EventTypes) > len(WakeupEventTypes) {
			return bad("select at least one supported event")
		}
		for _, e := range in.EventTypes {
			if slices.Contains(eventcontract.LifecycleTypes, e) {
				return bad(e + " cannot wake its own issue; subscriptions require an existing, open issue")
			}
			if !slices.Contains(WakeupEventTypes, e) {
				return bad("unsupported event: " + e)
			}
		}
		if in.FilterTaskID != "" {
			for _, e := range in.EventTypes {
				if !strings.HasPrefix(e, "task.") {
					return bad("task filter requires task events")
				}
			}
		}
		if in.AfterSeconds != 0 || in.At != nil || in.IntervalSeconds != 0 || in.CronExpression != "" {
			return bad("event wakeups cannot contain a schedule")
		}
		// Legacy mutation-only agent filters are an alias of actor=agent. Keep
		// mixed task/mutation subscriptions unchanged for existing API clients.
		if in.FilterAgentID != "" && in.FilterTaskID == "" {
			mutationOnly := true
			for _, event := range in.EventTypes {
				if strings.HasPrefix(event, "task.") {
					mutationOnly = false
				}
			}
			if mutationOnly {
				in.FilterActorType, in.FilterActorID = "agent", in.FilterAgentID
				in.FilterAgentID = ""
			}
		}
		return sql.NullString{}, nil
	case "at":
		if in.Mode != "once" {
			return bad("single time requires once mode")
		}
		if (in.At == nil) == (in.AfterSeconds == 0) || in.AfterSeconds < 0 || in.AfterSeconds > 31536000 {
			return bad("provide at or after_seconds (1–31536000)")
		}
		if in.IntervalSeconds != 0 || in.CronExpression != "" {
			return bad("incompatible schedule fields")
		}
		if in.At != nil {
			return nullTime(in.At.UTC(), true), nil
		}
		return nullTime(now.Add(time.Duration(in.AfterSeconds)*time.Second), true), nil
	case "every":
		if in.Mode != "continuous" || in.IntervalSeconds < 60 || in.IntervalSeconds > 31536000 || in.At != nil || in.AfterSeconds != 0 || in.CronExpression != "" {
			return bad("every requires continuous mode and interval_seconds between 60 and 31536000")
		}
		return nullTime(now.Add(time.Duration(in.IntervalSeconds)*time.Second), true), nil
	case "cron":
		if in.Mode != "continuous" || in.At != nil || in.AfterSeconds != 0 || in.IntervalSeconds != 0 {
			return bad("cron requires continuous mode without other schedule fields")
		}
		t, err := NextOccurrenceAfterUTC(in.CronExpression, in.Timezone, now)
		if err != nil || t.IsZero() {
			return bad("cron must have a future occurrence")
		}
		return nullTime(t, true), nil
	default:
		return bad("kind must be event, at, every or cron")
	}
}

func wakeupUUID(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	id, err := util.ParseUUID(s)
	if err != nil {
		return id, fmt.Errorf("%w: invalid UUID", ErrWakeupInput)
	}
	return id, nil
}

func wakeupIssueActive(ctx context.Context, q *db.Queries, i db.Issue) (bool, error) {
	category, err := issuestatus.CategoryWithError(ctx, q, i.WorkspaceID, i.Status)
	if err != nil {
		return false, err
	}
	return category != "done" && category != "closed", nil
}

// StopClosedIssueWakeups applies the close rule after a status write: once an
// issue is done or closed, every wakeup on it is disabled and wakeup runs that
// have not started are cancelled. Running work keeps its ordinary Stop
// control. Call it in the transaction that wrote the status, after that write
// locked the issue row, so the close and this cleanup commit together. The
// returned runs are for the caller's post-commit broadcast.
func StopClosedIssueWakeups(ctx context.Context, q *db.Queries, issue db.Issue) ([]db.AgentTaskQueue, error) {
	active, err := wakeupIssueActive(ctx, q, issue)
	if err != nil || active {
		return nil, err
	}
	if err = q.DisableIssueWakeups(ctx, issue.ID); err != nil {
		return nil, err
	}
	return q.CancelUnstartedIssueWakeupTasks(ctx, nullString(issue.ID))
}

func (s *IssueWakeupService) Create(ctx context.Context, issueID, member, source string, in WakeupInput) (db.IssueWakeup, error) {
	return s.Save(ctx, issueID, member, source, "", in)
}

type WakeupEnableInput struct {
	Revision int64      `json:"revision"`
	At       *time.Time `json:"at,omitempty"`
	Rearm    bool       `json:"rearm,omitempty"`
}

type WakeupInstructionInput struct {
	Instruction         string `json:"instruction"`
	ExpectedInstruction string `json:"expected_instruction"`
	Revision            int64  `json:"revision"`
}

// EditInstruction preserves the subscription revision and queued work. Compare
// both the revision and the prior text so concurrent edits cannot overwrite one
// another, without invalidating captured events or rearming a consumed rule.
func (s *IssueWakeupService) EditInstruction(ctx context.Context, issueID, id, member string, in WakeupInstructionInput) error {
	in.Instruction = strings.TrimSpace(in.Instruction)
	if len(in.Instruction) == 0 || len(in.Instruction) > 12000 || in.Revision < 1 {
		return fmt.Errorf("%w: instruction must be 1–12000 bytes and revision is required", ErrWakeupInput)
	}
	tx, err := s.Tasks.TxStarter.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := s.Tasks.Queries.WithTx(tx)
	var workspace string
	if err = tx.QueryRowContext(ctx, "SELECT w.id FROM workspace w JOIN issue i ON i.workspace_id=w.id WHERE i.id=$1", issueID).Scan(&workspace); err != nil {
		return err
	}
	issue, err := q.LockWakeupIssue(ctx, issueID)
	if err != nil {
		return err
	}
	membership, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: member, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		return ErrWakeupForbidden
	}
	w, err := q.LockIssueWakeup(ctx, id)
	if err != nil {
		return err
	}
	if w.IssueID != issue.ID || w.WorkspaceID != issue.WorkspaceID {
		return sql.ErrNoRows
	}
	if w.SystemRule.Valid || (w.CreatedBy.String != member && membership.Role != "owner" && membership.Role != "admin") {
		return ErrWakeupForbidden
	}
	agent, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: w.AgentID.String, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		return ErrWakeupForbidden
	}
	if err = s.authorize(ctx, q, issue.WorkspaceID, member, agent); err != nil {
		return err
	}
	if w.Revision != in.Revision || w.Instruction != in.ExpectedInstruction {
		return ErrWakeupConflict
	}
	_, err = tx.ExecContext(ctx, "UPDATE issue_wakeup SET instruction=$2,updated_at=now() WHERE id=$1 AND workspace_id=$3", id, in.Instruction, workspace)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// Enable reads the configuration under Save's locks; clients never round-trip
// instructions or filters. The revision fences stale toggles and duplicate rearm.
func (s *IssueWakeupService) Enable(ctx context.Context, issueID, member, source, id string, in WakeupEnableInput) (db.IssueWakeup, error) {
	return s.save(ctx, issueID, member, source, id, WakeupInput{}, &in)
}

// Save replaces a subscription explicitly; revisions fence obsolete queued work.
func (s *IssueWakeupService) Save(ctx context.Context, issueID, member, source, existingID string, in WakeupInput) (db.IssueWakeup, error) {
	return s.save(ctx, issueID, member, source, existingID, in, nil)
}

func (s *IssueWakeupService) save(ctx context.Context, issueID, member, source, existingID string, in WakeupInput, enable *WakeupEnableInput) (db.IssueWakeup, error) {
	var out db.IssueWakeup
	tx, err := s.Tasks.TxStarter.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	q := s.Tasks.Queries.WithTx(tx)
	var workspace string
	if err = tx.QueryRowContext(ctx, "SELECT w.id FROM workspace w JOIN issue i ON i.workspace_id=w.id WHERE i.id=$1", issueID).Scan(&workspace); err != nil {
		return out, err
	}
	issue, err := q.LockWakeupIssue(ctx, issueID)
	if err != nil {
		return out, err
	}
	active, err := wakeupIssueActive(ctx, q, issue)
	if err != nil {
		return out, err
	}
	if !active {
		return out, fmt.Errorf("%w: issue is closed", ErrWakeupInput)
	}
	now, err := txNow(ctx, tx)
	if err != nil {
		return out, err
	}

	if enable != nil {
		old, e := q.LockIssueWakeup(ctx, existingID)
		if e != nil {
			return out, e
		}
		if old.IssueID != issueID || old.WorkspaceID != issue.WorkspaceID {
			return out, sql.ErrNoRows
		}
		if old.SystemRule.Valid {
			return out, ErrWakeupForbidden
		}
		if enable.Revision < 1 {
			return out, fmt.Errorf("%w: revision is required", ErrWakeupInput)
		}
		if old.Revision != enable.Revision {
			return out, ErrWakeupConflict
		}
		optionalID := func(id string) string {
			if id != "" {
				return util.UUIDToString(id)
			}
			return ""
		}
		in = WakeupInput{AgentID: optionalID(old.AgentID.String), Instruction: old.Instruction, Kind: old.Kind, Mode: old.Mode, EventTypes: parseStringList(old.EventTypes), FilterAgentID: optionalID(old.FilterAgentID.String), FilterTaskID: optionalID(old.FilterTaskID.String), FilterActorType: old.FilterActorType.String, FilterActorID: optionalID(old.FilterActorID.String), ParentCommentID: optionalID(old.ParentCommentID.String), IntervalSeconds: old.IntervalSeconds.Int64, CronExpression: old.CronExpression.String, Timezone: old.Timezone, OnTimeout: old.OnTimeout.String}
		// A relative wait restarts from now; an absolute deadline is kept and
		// must still be in the future.
		if len(old.Condition.String) > 0 {
			in.Condition, in.EventTypes = json.RawMessage(old.Condition.String), nil
		}
		in.MaxFires = int32(old.MaxFires.Int64)
		if old.ExpirySeconds.Valid {
			in.ExpiresInSeconds = old.ExpirySeconds.Int64
		} else if old.ExpiresAt.Valid {
			deadline := asTime(old.ExpiresAt)
			in.ExpiresAt = &deadline
		}
		if enable.At != nil && old.Kind != "at" {
			return out, fmt.Errorf("%w: only single-time wakeups accept a new time", ErrWakeupInput)
		}
		if (old.Enabled == 0) && old.Mode == "once" && (!old.DisabledAt.Valid || old.LastTaskID.Valid) {
			if !enable.Rearm {
				return out, fmt.Errorf("%w: consumed one-shot requires explicit rearm", ErrWakeupInput)
			}
			var activeRun bool
			if e = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM agent_task_queue WHERE issue_id=$1 AND context->>'wakeup_id'=$2 AND status IN ('queued','deferred','dispatched','running','waiting_local_directory'))", issueID, optionalID(old.ID)).Scan(&activeRun); e != nil {
				return out, e
			}
			if activeRun {
				return out, fmt.Errorf("%w: previous run is still active", ErrWakeupConflict)
			}
		}
		if old.Kind == "at" {
			if old.NextFireAt.Valid {
				at := asTime(old.NextFireAt)
				in.At = &at
			}
			if enable.At != nil {
				in.At = enable.At
			}
			if (old.Enabled == 0) && (in.At == nil || !in.At.After(now)) {
				return out, fmt.Errorf("%w: choose a future time", ErrWakeupInput)
			}
		}
	}
	next, err := s.Validate(&in, now)
	if err != nil {
		return out, err
	}
	expiresAt, expirySeconds, err := s.Expiry(&in, now)
	if err != nil {
		return out, err
	}
	onTimeout := sql.NullString{String: in.OnTimeout, Valid: in.OnTimeout != ""}
	var condition []byte
	if hasWakeupCondition(in.Condition) {
		normalized, hints, e := validateCondition(ctx, tx, issue, in.Condition)
		if e != nil {
			return out, e
		}
		condition, in.EventTypes = normalized, hints
		// The scheduler evaluates the condition on its first tick.
		next = nullTime(now, true)
	}
	maxFires := nullInt64(int64(in.MaxFires), in.MaxFires > 0)
	if !maxFires.Valid && in.Kind == "event" && in.Mode == "continuous" {
		maxFires = nullInt64(int64(wakeupDefaultMaxFires), true)
	}
	agentID, err := wakeupUUID(in.AgentID)
	if err != nil || (agentID == "") {
		return out, fmt.Errorf("%w: agent_id is required", ErrWakeupInput)
	}
	agent, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: agentID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		return out, ErrWakeupForbidden
	}
	if err = s.authorize(ctx, q, issue.WorkspaceID, member, agent); err != nil {
		return out, err
	}
	filterAgent, err := wakeupUUID(in.FilterAgentID)
	if err != nil {
		return out, err
	}
	filterTask, err := wakeupUUID(in.FilterTaskID)
	if err != nil {
		return out, err
	}
	filterActor, err := wakeupUUID(in.FilterActorID)
	if err != nil {
		return out, err
	}
	if filterActor != "" {
		if in.FilterActorType == "member" {
			_, err = q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: filterActor, WorkspaceID: issue.WorkspaceID})
		} else {
			_, err = q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: filterActor, WorkspaceID: issue.WorkspaceID})
		}
		if errors.Is(err, sql.ErrNoRows) {
			return out, ErrWakeupForbidden
		}
		if err != nil {
			return out, err
		}
	}
	parent, err := wakeupUUID(in.ParentCommentID)
	if err != nil {
		return out, err
	}
	if filterAgent != "" {
		if _, err = q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: filterAgent, WorkspaceID: issue.WorkspaceID}); err != nil {
			return out, ErrWakeupForbidden
		}
	}
	if in.Kind != "event" && (len(in.EventTypes) > 0 || (filterAgent != "") || (filterTask != "")) {
		return out, fmt.Errorf("%w: time wakeup cannot contain event filters", ErrWakeupInput)
	}
	if parent != "" {
		var exists bool
		err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM comment WHERE id=$1 AND issue_id=$2 AND deleted_at IS NULL)", parent, issueID).Scan(&exists)
		if err != nil {
			return out, err
		}
		if !exists {
			return out, fmt.Errorf("%w: comment not found", ErrWakeupInput)
		}
	}
	if existingID != "" {
		old, e := q.LockIssueWakeup(ctx, existingID)
		if e != nil {
			return out, e
		}
		if old.IssueID != issueID || old.WorkspaceID != issue.WorkspaceID {
			return out, sql.ErrNoRows
		}
		if old.SystemRule.Valid {
			return out, ErrWakeupForbidden
		}
		membership, e := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: member, WorkspaceID: issue.WorkspaceID})
		if e != nil {
			return out, ErrWakeupForbidden
		}
		if old.CreatedBy.String != member && membership.Role != "owner" && membership.Role != "admin" {
			return out, ErrWakeupForbidden
		}
		if enable != nil && old.Enabled != 0 {
			if enable.Rearm || enable.At != nil {
				return out, ErrWakeupConflict
			}
			return old, tx.Commit()
		}
		if err = q.DiscardWakeupReceipts(ctx, old.ID); err != nil {
			return out, err
		}
		if _, err = q.CancelUnstartedWakeupTasks(ctx, nullString(util.UUIDToString(old.ID))); err != nil {
			return out, err
		}
	}
	var target db.AgentTaskQueue
	if filterTask != "" {
		target, err = q.LockWakeupSourceTask(ctx, db.LockWakeupSourceTaskParams{ID: filterTask, IssueID: nullString(issueID)})
		if errors.Is(err, sql.ErrNoRows) {
			return out, fmt.Errorf("%w: run does not belong to this issue", ErrWakeupInput)
		}
		if err != nil {
			return out, err
		}
		if (filterAgent != "") && filterAgent != target.AgentID {
			return out, fmt.Errorf("%w: run and agent filters disagree", ErrWakeupInput)
		}
	}
	params := db.CreateIssueWakeupParams{ID: newV7(), WorkspaceID: issue.WorkspaceID, IssueID: issue.ID, AgentID: nullString(agent.ID), CreatedBy: nullString(member), SourceTaskID: nullString(source), ParentCommentID: nullString(parent), Instruction: in.Instruction, Kind: in.Kind, Mode: in.Mode, EventTypes: stringListJSON(in.EventTypes), FilterAgentID: nullString(filterAgent), FilterTaskID: nullString(filterTask), FilterActorType: sql.NullString{String: in.FilterActorType, Valid: in.FilterActorType != ""}, FilterActorID: nullString(filterActor), IntervalSeconds: sql.NullInt64{Int64: in.IntervalSeconds, Valid: in.IntervalSeconds > 0}, CronExpression: sql.NullString{String: in.CronExpression, Valid: in.CronExpression != ""}, Timezone: in.Timezone, NextFireAt: next, ExpiresAt: expiresAt, ExpirySeconds: expirySeconds, OnTimeout: onTimeout, Condition: nullString(string(condition)), MaxFires: maxFires}
	if existingID != "" {
		_, err = tx.ExecContext(ctx, `UPDATE issue_wakeup SET agent_id=$2,created_by=$3,source_task_id=$4,parent_comment_id=$5,instruction=$6,kind=$7,mode=$8,event_types=$9,filter_agent_id=$10,filter_task_id=$11,interval_seconds=$12,cron_expression=$13,timezone=$14,next_fire_at=$15,filter_actor_type=$16,filter_actor_id=$17,expires_at=$18,expiry_seconds=$19,on_timeout=$20,timed_out_at=NULL,condition=$21,max_fires=$22,condition_state='',fire_count=0,paused_reason=NULL,enabled=true,disabled_at=NULL,revision=revision+1,last_task_id=NULL,last_error=NULL,updated_at=now() WHERE id=$1`, existingID, params.AgentID, member, source, parent, in.Instruction, in.Kind, in.Mode, params.EventTypes, filterAgent, filterTask, params.IntervalSeconds, params.CronExpression, in.Timezone, next, params.FilterActorType, filterActor, expiresAt, expirySeconds, onTimeout, condition, maxFires)
		if err == nil {
			out, err = q.LockIssueWakeup(ctx, existingID)
		}
	} else {
		out, err = q.CreateIssueWakeup(ctx, params)
	}
	if err != nil {
		return out, err
	}
	if len(out.Condition.String) > 0 && conditionFiresOnChange([]byte(out.Condition.String)) {
		if err = s.baselineCondition(ctx, tx, q, out, now); err != nil {
			return out, err
		}
		out, err = q.LockIssueWakeup(ctx, out.ID)
		if err != nil {
			return out, err
		}
	}
	var activities []wakeupActivity
	if existingID == "" {
		actorType, actorID := "member", member
		if source != "" {
			var agentID string
			if e := tx.QueryRowContext(ctx, "SELECT agent_id FROM agent_task_queue WHERE id=$1", source).Scan(&agentID); e == nil && (agentID != "") {
				actorType, actorID = "agent", agentID
			}
		}
		created, e := recordWakeupActivity(ctx, q, out, wakeupActivityCreated, actorType, actorID, nil)
		if e != nil {
			return out, e
		}
		activities = append(activities, created)
	}
	// Subscribe and inspect the concrete source under the same row lock. This
	// closes the already-terminal race even if the caller exits immediately.
	if (filterTask != "") && slices.Contains([]string{"completed", "failed", "cancelled"}, target.Status) && slices.Contains(in.EventTypes, eventcontract.TaskEvent(target.Status)) {
		payload, _ := json.Marshal(map[string]any{
			"event_id": util.UUIDToString(target.ID) + ":" + target.Status, "event_type": eventcontract.TaskEvent(target.Status), "version": 1,
			"occurred_at": target.CompletedAt, "observed_at": now, "registration_snapshot": true,
			"workspace_id": util.UUIDToString(issue.WorkspaceID), "issue_id": util.UUIDToString(issue.ID),
			"task_id": util.UUIDToString(target.ID), "source_task_id": util.UUIDToString(target.ID), "status": target.Status,
			"agent_id": util.UUIDToString(target.AgentID), "actor_type": "agent", "actor_id": util.UUIDToString(target.AgentID),
			"retry_of_task_id": target.RetryOfTaskID, "rerun_of_task_id": target.RerunOfTaskID,
		})
		_, err = q.RecordWakeupReceipt(ctx, db.RecordWakeupReceiptParams{ID: newV7(), WakeupID: out.ID, Revision: out.Revision, EventKey: util.UUIDToString(target.ID) + ":" + target.Status, EventType: "task." + target.Status, Payload: string(payload)})
		if err != nil {
			return out, err
		}
		if out.Mode == "once" {
			out.Enabled = 0
			err = q.AdvanceIssueWakeup(ctx, db.AdvanceIssueWakeupParams{ID: out.ID, Enabled: 0})
			if err != nil {
				return out, err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	s.publishWakeupActivities(activities...)
	return out, nil
}

func (s *IssueWakeupService) authorize(ctx context.Context, q *db.Queries, ws, member string, a db.Agent) error {
	if (member == "") || a.ArchivedAt.Valid || !a.RuntimeID.Valid {
		return ErrWakeupForbidden
	}
	if _, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: member, WorkspaceID: ws}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrWakeupForbidden
		}
		return err
	}
	if !CanMemberInvokeAgent(ctx, q, a, member, ws) {
		return ErrWakeupForbidden
	}
	return nil
}

// Tick uses the existing scheduler lease. Each config has a row lock as well,
// so a manual retry or a second server cannot dispatch the same receipt twice.
func (s *IssueWakeupService) Tick(ctx context.Context) error {
	return s.tick(ctx, nil)
}

// TickWorkspaces is one Tick pass over the given workspaces' ready wakeups.
// Tests use it: packages run concurrently against one database, and an
// unscoped pass would dispatch — and consume the pending receipts of — rules
// another package's test is still asserting on.
func (s *IssueWakeupService) TickWorkspaces(ctx context.Context, workspaceIDs ...string) error {
	if len(workspaceIDs) == 0 {
		return nil
	}
	return s.tick(ctx, workspaceIDs)
}

// Scheduler pass limits. A pass used to visit a single batch of 100 rules, so
// with more ready rules than that a status change waited several passes
// (minutes) behind condition checks that had nothing new (MUL-7933, #9139).
// A pass now keeps taking batches until both lanes are drained or a limit is
// hit. The scheduler runs its jobs one after another, so the pass stops well
// inside the job's 45s run timeout and leaves the rest of the 30s scheduler
// tick to the other jobs; one dispatch is itself capped at 2s.
const (
	wakeupTickBatch = 100
	// At most this many rules of a batch come from the input lane; the due
	// lane fetches the rest.
	wakeupTickInputShare = 50
	wakeupTickBudget     = 15 * time.Second
	// While the due lane still has work, the input lane may spend at most
	// this much of the pass, so slow inputs cannot starve schedules,
	// deadlines and condition checks. Either lane uses the whole budget once
	// the other is drained.
	wakeupTickInputBudget = wakeupTickBudget / 2
	wakeupTickMaxRules    = 1500
	// A pending input the scheduler could act on that is older than this is
	// logged as a warning. Inputs a run holds are reported separately.
	wakeupPendingWarnAge = 2 * time.Minute
)

// tickLimits are a pass's limits; zero fields take the defaults above. Tests
// shrink them, and beforeDispatch lets a test make a dispatch slow.
type tickLimits struct {
	budget, inputBudget time.Duration
	beforeDispatch      func(db.IssueWakeup)
}

// tick dispatches ready wakeups; nil workspaceIDs means every workspace.
//
// Two lanes feed the pass: rules with unprocessed inputs (an event, a
// condition hint, "wake now") and rules whose schedule, deadline or condition
// check is due. Dispatches alternate between the lanes, and the input lane's
// time is capped while due work remains. Each rule is visited at most once.
func (s *IssueWakeupService) tick(ctx context.Context, workspaceIDs []string) error {
	return s.tickWith(ctx, workspaceIDs, tickLimits{})
}

func (s *IssueWakeupService) tickWith(ctx context.Context, workspaceIDs []string, lim tickLimits) error {
	if lim.budget <= 0 {
		lim.budget = wakeupTickBudget
	}
	if lim.inputBudget <= 0 {
		lim.inputBudget = wakeupTickInputBudget
	}
	start := time.Now()
	// Receipts are operational evidence, not the run history. Match the existing
	// event telemetry's seven-day retention; never expire unprocessed inputs.
	cleanupCtx, cleanupCancel := context.WithTimeout(ctx, 2*time.Second)
	_, cleanupErr := s.Tasks.Queries.DeleteExpiredWakeupReceipts(cleanupCtx, nullTime(time.Now().Add(-7*24*time.Hour), true))
	cleanupCancel()
	var errs []error
	if cleanupErr != nil {
		errs = append(errs, fmt.Errorf("expire wakeup receipts: %w", cleanupErr))
	}
	// claimed holds every rule visited or queued in this pass. Never nil:
	// "id = ANY(NULL)" is NULL and would exclude every rule.
	claimed := []string{}
	var inputQ, dueQ []db.IssueWakeup
	inputDrained, dueDrained := false, false
	var inputSpent time.Duration
	inputs, due := 0, 0
	stop := "drained"
	inputTurn := true
	for {
		if ctx.Err() != nil {
			return errors.Join(append(errs, ctx.Err())...)
		}
		if time.Since(start) >= lim.budget {
			stop = "time_budget"
			break
		}
		if inputs+due >= wakeupTickMaxRules {
			stop = "rule_cap"
			break
		}
		room := wakeupTickMaxRules - len(claimed)
		if len(inputQ) == 0 && !inputDrained && room > 0 {
			limit := min(wakeupTickInputShare, room)
			rows, err := s.Tasks.Queries.ListInputWakeups(ctx, db.ListInputWakeupsParams{WorkspaceIds: workspaceIDs, SkipIds: claimed, RowLimit: int64(int32(limit))})
			if err != nil {
				return errors.Join(append(errs, err)...)
			}
			inputDrained = len(rows) < limit
			inputQ = rows
			for _, w := range rows {
				claimed = append(claimed, w.ID)
			}
			room -= len(rows)
		}
		if len(dueQ) == 0 && !dueDrained && room > 0 {
			limit := min(wakeupTickBatch-len(inputQ), room)
			rows, err := s.Tasks.Queries.ListDueWakeups(ctx, db.ListDueWakeupsParams{WorkspaceIds: workspaceIDs, SkipIds: claimed, RowLimit: int64(int32(limit))})
			if err != nil {
				return errors.Join(append(errs, err)...)
			}
			dueDrained = len(rows) < limit
			dueQ = rows
			for _, w := range rows {
				claimed = append(claimed, w.ID)
			}
		}
		// Due work is left while the lane has queued rules or can still fetch.
		dueLeft := len(dueQ) > 0 || (!dueDrained && wakeupTickMaxRules > len(claimed))
		inputAllowed := len(inputQ) > 0 && (inputSpent < lim.inputBudget || !dueLeft)
		var w db.IssueWakeup
		fromInput := false
		switch {
		case inputAllowed && (inputTurn || len(dueQ) == 0):
			w, inputQ, fromInput = inputQ[0], inputQ[1:], true
		case len(dueQ) > 0:
			w, dueQ = dueQ[0], dueQ[1:]
		default:
			// Both lanes are empty: drained, or out of room under the cap.
			if !inputDrained || !dueDrained {
				stop = "rule_cap"
			}
			return s.finishTick(ctx, workspaceIDs, start, inputs, due, stop, errs)
		}
		inputTurn = !fromInput
		began := time.Now()
		if lim.beforeDispatch != nil {
			lim.beforeDispatch(w)
		}
		errs = append(errs, s.dispatchFromTick(ctx, w)...)
		if fromInput {
			inputs++
			inputSpent += time.Since(began)
		} else {
			due++
		}
	}
	return s.finishTick(ctx, workspaceIDs, start, inputs, due, stop, errs)
}

// finishTick logs a global pass and returns its errors.
func (s *IssueWakeupService) finishTick(ctx context.Context, workspaceIDs []string, start time.Time, inputs, due int, stop string, errs []error) error {
	if workspaceIDs == nil {
		s.logTick(ctx, start, inputs, due, stop, len(errs))
	}
	return errors.Join(errs...)
}

// dispatchFromTick dispatches one rule for a scheduler pass and records the
// outcome on the rule.
func (s *IssueWakeupService) dispatchFromTick(ctx context.Context, w db.IssueWakeup) []error {
	var errs []error
	// Outcome writes must not turn a busy rule row into another batch-wide
	// wait. Use the batch context, not dispatch's expired per-rule context.
	dispatch := s.dispatch
	if w.SystemRule.Valid {
		dispatch = s.dispatchSystem
	}
	if err := dispatch(ctx, w); err != nil {
		errs = append(errs, fmt.Errorf("wakeup %s: %w", util.UUIDToString(w.ID), err))
		outcomeCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		_ = s.Tasks.Queries.NoteWakeupFailure(outcomeCtx, db.NoteWakeupFailureParams{ID: w.ID, LastError: sql.NullString{String: truncateForSummary(err.Error(), 500), Valid: true}})
		cancel()
	}
	// Move the rule to the back of its lane so one noisy issue cannot
	// monopolize the batches. Only scan order depends on this write: dispatch
	// already committed the rule's outcome, and an unfinished rule stays a
	// candidate either way. A pass now visits many more rules than one batch,
	// so under load this bounded write can time out without anything being
	// wrong; that is logged, not reported as a failed pass.
	outcomeCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	if touchErr := s.Tasks.Queries.TouchWakeupDispatch(outcomeCtx, w.ID); touchErr != nil && ctx.Err() == nil {
		slog.DebugContext(ctx, "issue wakeup: scan-order touch skipped", "wakeup_id", util.UUIDToString(w.ID), "error", touchErr)
	}
	cancel()
	return errs
}

// logTick reports one global pass: how many rules each lane visited, how long
// the pass took and why it stopped, plus the age of the oldest input the
// scheduler could act on and how many inputs a run is holding. It logs at
// warn level when a limit cut the pass short, a dispatch failed, an
// actionable input has waited longer than wakeupPendingWarnAge, or the
// pending scan could not finish (pending_scan=incomplete: the age may be
// missing or too young, so silence must not be read as "no backlog").
// Held inputs never warn on their own.
func (s *IssueWakeupService) logTick(ctx context.Context, start time.Time, inputs, due int, stop string, failures int) {
	attrs := []any{"input_rules", inputs, "due_rules", due, "duration_ms", time.Since(start).Milliseconds(), "stopped", stop, "failures", failures}
	level := slog.LevelInfo
	if stop != "drained" || failures > 0 {
		level = slog.LevelWarn
	}
	pendingCtx, cancel := context.WithTimeout(ctx, wakeupPendingScanBudget)
	pending, err := s.pendingInputs(pendingCtx, nil, wakeupPendingScanPage)
	cancel()
	attrs = append(attrs, "held_inputs", pending.Held)
	if err != nil {
		attrs = append(attrs, "pending_scan", "incomplete", "pending_scan_error", err.Error())
		level = slog.LevelWarn
	} else {
		attrs = append(attrs, "pending_scan", "complete")
	}
	if age, ok := pending.age(time.Now()); ok {
		attrs = append(attrs, "oldest_ready_input_age_s", int64(age.Seconds()))
		if age > wakeupPendingWarnAge {
			level = slog.LevelWarn
		}
	}
	slog.Log(ctx, level, "issue wakeup dispatch pass", attrs...)
}

// The pending scan reads every group of unprocessed inputs, a page at a time,
// within this budget. A scan that runs out of it reports itself incomplete.
const (
	wakeupPendingScanBudget = 3 * time.Second
	wakeupPendingScanPage   = 1000
)

// pendingInputSummary splits unprocessed inputs into those a run is holding
// on purpose and the backlog the scheduler could act on.
type pendingInputSummary struct {
	OldestReady sql.NullString
	Held        int
}

// age is how long the oldest actionable input has waited, if there is one.
func (p pendingInputSummary) age(now time.Time) (time.Duration, bool) {
	if !p.OldestReady.Valid {
		return 0, false
	}
	return now.Sub(asTime(p.OldestReady)), true
}

// pendingInputs classifies every unprocessed input the way dispatch treats
// it (see ListPendingWakeupInputGroups), paging until the groups run out. For
// a platform rule an input is held only while a run of the issue's current
// target agent, run as the person the platform rule's run would run as, waits
// to start: the same check dispatchSystem makes before keeping its inputs.
//
// On an error, including ctx running out, it returns what it read so far
// with the error: the summary is then a lower bound, not the answer.
func (s *IssueWakeupService) pendingInputs(ctx context.Context, workspaceIDs []string, pageSize int) (pendingInputSummary, error) {
	var out pendingInputSummary
	q := s.Tasks.Queries
	systemWaiting := map[string]bool{}
	params := db.ListPendingWakeupInputGroupsParams{WorkspaceIds: workspaceIDs, PageSize: int64(int32(pageSize))}
	for {
		groups, err := q.ListPendingWakeupInputGroups(ctx, params)
		if err != nil {
			return out, fmt.Errorf("list pending wakeup inputs: %w", err)
		}
		for _, g := range groups {
			held := truthy(g.Reserved) || truthy(g.OwnClaimed) || truthy(g.UserWaiting)
			if !held && truthy(g.SystemRule) {
				waiting, known := systemWaiting[g.IssueID]
				if !known {
					if waiting, err = s.systemRuleRunWaiting(ctx, q, g.IssueID); err != nil {
						return out, fmt.Errorf("resolve platform rule target: %w", err)
					}
					systemWaiting[g.IssueID] = waiting
				}
				held = waiting
			}
			switch {
			case held:
				out.Held += int(g.Inputs)
			case !out.OldestReady.Valid || asTime(g.Oldest).Before(asTime(out.OldestReady)):
				out.OldestReady = asNullString(g.Oldest)
			}
		}
		if len(groups) < pageSize {
			return out, nil
		}
		last := groups[len(groups)-1]
		params.AfterOldest, params.AfterWakeupID, params.AfterReserved = last.Oldest, last.WakeupID, last.Reserved
	}
}

// systemRuleRunWaiting reports whether a platform rule on the issue would
// keep its inputs for a run waiting to start (dispatchSystem's hasWaitingRun).
func (s *IssueWakeupService) systemRuleRunWaiting(ctx context.Context, q *db.Queries, issueID string) (bool, error) {
	issue, err := q.GetIssue(ctx, issueID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	target, err := resolveWakeTarget(ctx, q, issue)
	if err != nil || (target.Agent.ID == "") {
		return false, err
	}
	runAs, err := s.childDoneRunAs(ctx, issue, target.Agent)
	if err != nil {
		return false, err
	}
	return hasWaitingRun(ctx, q, issue.ID, target.Agent.ID, runAs.UserID.String)
}

func (s *IssueWakeupService) dispatch(ctx context.Context, prev db.IssueWakeup) error {
	// A rule gets at most two seconds including credentials and SQL. Lock waits
	// below are shorter: even 100 contended rules consume only ~5s of the 45s
	// batch budget, leaving time to dispatch unrelated issues.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	// Resolve optional connected-app credentials before taking database locks.
	candidate, readErr := s.Tasks.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: prev.AgentID.String, WorkspaceID: prev.WorkspaceID})
	if readErr != nil && !errors.Is(readErr, sql.ErrNoRows) {
		return readErr
	}
	var overlay runtimeMCPOverlayData
	if (candidate.ID != "") && s.authorize(ctx, s.Tasks.Queries, prev.WorkspaceID, prev.CreatedBy.String, candidate) == nil {
		overlay = s.Tasks.buildRuntimeMCPOverlay(ctx, prev.CreatedBy.String, candidate)
	}
	tx, err := s.Tasks.TxStarter.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := s.Tasks.Queries.WithTx(tx)
	var fenced bool
	if candidate.ID != "" {
		if fenced, err = lockTaskOwnerRows(ctx, tx, candidate.ID, prev.IssueID, candidate.RuntimeID); err != nil {
			return err
		}
		if !fenced {
			return sql.ErrNoRows
		}
	} else {
		if _, err = q.LockWorkspaceForChatSessionCreate(ctx, prev.WorkspaceID); err != nil {
			return err
		}
	}
	issue, err := q.LockWakeupIssue(ctx, prev.IssueID)
	if errors.Is(err, sql.ErrNoRows) {
		return qCleanupMissingWakeup(ctx, tx, prev.ID)
	}
	if err != nil {
		return err
	}
	w, err := q.LockIssueWakeup(ctx, prev.ID)
	if err != nil {
		return err
	}
	if w.Revision != prev.Revision {
		return tx.Commit()
	}
	active, err := wakeupIssueActive(ctx, q, issue)
	if err != nil {
		return err
	}
	agent, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: w.AgentID.String, WorkspaceID: w.WorkspaceID})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if (agent.ID != "") && agent.RuntimeID != candidate.RuntimeID {
		return tx.Commit()
	}
	authErr := s.authorize(ctx, q, w.WorkspaceID, w.CreatedBy.String, agent)
	if w.DisabledAt.Valid || !active || authErr != nil {
		if authErr != nil && !errors.Is(authErr, ErrWakeupForbidden) {
			return authErr
		}
		_, err = tx.ExecContext(ctx, "UPDATE issue_wakeup SET enabled=false,disabled_at=COALESCE(disabled_at,now()),last_error=$2 WHERE id=$1", w.ID, "Wakeup disabled: issue closed or permission unavailable")
		if err != nil {
			return err
		}
		if err = q.DiscardWakeupReceipts(ctx, w.ID); err != nil {
			return err
		}
		if _, err = q.CancelUnstartedWakeupTasks(ctx, nullString(util.UUIDToString(w.ID))); err != nil {
			return err
		}
		return tx.Commit()
	}
	if _, err = tx.ExecContext(ctx, "UPDATE issue_wakeup_receipt SET processed_at=now() WHERE wakeup_id=$1 AND revision<>$2 AND processed_at IS NULL", w.ID, w.Revision); err != nil {
		return err
	}
	now, err := txNow(ctx, tx)
	if err != nil {
		return err
	}
	next := w.NextFireAt
	enabled := w.Enabled
	if w.Mode == "once" && w.LastTaskID.Valid {
		if err := q.DiscardWakeupReceipts(ctx, w.ID); err != nil {
			return err
		}
		return tx.Commit()
	}
	// Timeline entries are written with the rule's changes and published only
	// once they commit.
	var activities []wakeupActivity
	commit := func() error {
		if err := tx.Commit(); err != nil {
			return err
		}
		s.publishWakeupActivities(activities...)
		return nil
	}
	note := func(action string, details map[string]any) error {
		a, err := recordWakeupActivity(ctx, q, w, action, "system", "", details)
		if err == nil {
			activities = append(activities, a)
		}
		return err
	}
	markTimedOut := func() error {
		if err := q.MarkWakeupTimedOut(ctx, w.ID); err != nil {
			return err
		}
		return note(wakeupActivityTimedOut, map[string]any{"woke": w.Kind == "event" && w.OnTimeout.String == "wake"})
	}
	// Reaching the deadline ends the rule. Inputs captured before it still
	// dispatch; an event rule may also wake the target once to handle the
	// missed deadline.
	timedOut := (w.Enabled != 0) && w.ExpiresAt.Valid && !asTime(w.ExpiresAt).After(now)
	if timedOut {
		enabled = 0
		next = sql.NullString{}
		if w.Kind == "event" && w.OnTimeout.String == "wake" {
			deadline := asTime(w.ExpiresAt).UTC().Format(time.RFC3339)
			payload, _ := json.Marshal(map[string]any{"expires_at": deadline, "event_types": w.EventTypes,
				"note": "The deadline passed before a subscribed event arrived. Read current state, then escalate, extend or stop."})
			if _, err = q.RecordWakeupReceipt(ctx, db.RecordWakeupReceiptParams{ID: newV7(), WakeupID: w.ID, Revision: w.Revision, EventKey: "timeout:" + deadline, EventType: wakeupTimeoutEventType, Payload: string(payload)}); err != nil {
				return err
			}
		}
	}
	// A condition rule turns its hint events and scheduled checks into at most
	// one condition.met input; a rule that ended only drops the hints.
	if w.Condition.Valid && w.Condition.String != "" {
		if w.Enabled != 0 && !timedOut {
			if next, err = s.pollCondition(ctx, tx, q, w, now); err != nil {
				return err
			}
		} else if _, _, err = consumeConditionHints(ctx, tx, w.ID); err != nil {
			return err
		}
	}
	if w.Enabled != 0 && !timedOut && w.Kind != "event" && next.Valid && !asTime(next).After(now) {
		planned := asTime(next)
		switch w.Kind {
		case "at":
			enabled = 0
			next = sql.NullString{}
		case "every":
			step := time.Duration(w.IntervalSeconds.Int64) * time.Second
			planned = planned.Add(now.Sub(planned) / step * step)
			next = nullTime(planned.Add(step), true)
		case "cron":
			var nextAt time.Time
			nextAt, err = NextOccurrenceAfterUTC(w.CronExpression.String, w.Timezone, now)
			if err != nil {
				return err
			}
			next = nullTime(nextAt, true)
			if nextAt.IsZero() {
				enabled = 0
				next = sql.NullString{}
			}
		}
		payload, _ := json.Marshal(map[string]any{"planned_at": planned.UTC().Format(time.RFC3339), "kind": w.Kind})
		var latest db.IssueWakeupReceipt
		latest, err = q.RecordWakeupReceipt(ctx, db.RecordWakeupReceiptParams{ID: newV7(), WakeupID: w.ID, Revision: w.Revision, EventKey: planned.UTC().Format(time.RFC3339Nano), EventType: "time.due", Payload: string(payload)})
		if err == nil {
			_, err = tx.ExecContext(ctx, "DELETE FROM issue_wakeup_receipt WHERE wakeup_id=$1 AND event_type='time.due' AND processed_at IS NULL AND id<>$2", w.ID, latest.ID)
		}
		if err != nil {
			return err
		}
	}
	task, err := q.FindPendingWakeupTask(ctx, nullString(util.UUIDToString(w.ID)))
	if err == nil && task.Status == "dispatched" {
		// A claimed prompt is immutable. Recovery belongs to the ordinary
		// claim/prepare lease, not a second wakeup-specific task timeout.
		var waiting sql.NullString
		if task.DispatchedAt.Valid && now.Sub(asTime(task.DispatchedAt)) >= claimResponseRecoveryWindow &&
			(!task.PrepareLeaseExpiresAt.Valid || !asTime(task.PrepareLeaseExpiresAt).After(now)) {
			waiting = sql.NullString{String: "Waiting for claimed run " + util.UUIDToString(task.ID) + " to start or recover; new trigger inputs are retained.", Valid: true}
		}
		// Persist timer progress even while waiting; do not regenerate each
		// elapsed tick. Keep once enabled until its pending input is assigned.
		if err := q.AdvanceIssueWakeup(ctx, db.AdvanceIssueWakeupParams{ID: w.ID, Enabled: w.Enabled, NextFireAt: next, LastError: waiting}); err != nil {
			return err
		}
		if timedOut {
			if err := markTimedOut(); err != nil {
				return err
			}
		}
		return commit()
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	taskExists := err == nil
	receipts, err := q.ListPendingWakeupReceipts(ctx, db.ListPendingWakeupReceiptsParams{WakeupID: w.ID, Revision: w.Revision})
	if err != nil {
		return err
	}
	// Inputs a claimed run took along count once that run starts; until then
	// they wait with it, and they come back if it ends without starting.
	receipts, _, taken, err := takenReceipts(ctx, q, receipts)
	if err != nil {
		return err
	}
	for _, run := range taken {
		if err = q.ConsumeWakeupReceipts(ctx, db.ConsumeWakeupReceiptsParams{Ids: receiptIDs(run.receipts), TaskID: nullString(run.task.ID)}); err != nil {
			return err
		}
		if err = q.CountWakeupFires(ctx, w.ID); err != nil {
			return err
		}
		w.FireCount++
		w.LastTaskID = nullString(run.task.ID)
		details := wakeupTriggerDetails(run.task, run.receipts)
		details["outcome"] = wakeupOutcomeMerged
		if err = note(wakeupActivityTriggered, details); err != nil {
			return err
		}
	}
	if len(taken) > 0 {
		// A once rule has fired, and one at its cap stops after this firing,
		// as after a run of its own; later inputs are dropped with it.
		switch {
		case w.Mode == "once":
			if err = q.DiscardWakeupReceipts(ctx, w.ID); err != nil {
				return err
			}
			if err = q.AdvanceIssueWakeup(ctx, db.AdvanceIssueWakeupParams{ID: w.ID, Enabled: 0, LastTaskID: w.LastTaskID}); err != nil {
				return err
			}
			if timedOut {
				if err = markTimedOut(); err != nil {
					return err
				}
			}
			return commit()
		case (w.Enabled != 0) && w.MaxFires.Valid && w.FireCount >= w.MaxFires.Int64:
			if err = q.DiscardWakeupReceipts(ctx, w.ID); err != nil {
				return err
			}
			if err = q.AdvanceIssueWakeup(ctx, db.AdvanceIssueWakeupParams{ID: w.ID, Enabled: w.Enabled, NextFireAt: next, LastTaskID: w.LastTaskID}); err != nil {
				return err
			}
			if err = q.PauseIssueWakeup(ctx, db.PauseIssueWakeupParams{ID: w.ID, PausedReason: sql.NullString{String: wakeupPausedMaxFires, Valid: true}, BlockRuns: nullString(fmt.Sprint(false))}); err != nil {
				return err
			}
			if err = note(wakeupActivityPaused, map[string]any{"reason": wakeupPausedMaxFires, "limit": w.MaxFires.Int64}); err != nil {
				return err
			}
			return commit()
		}
		if err = q.AdvanceIssueWakeup(ctx, db.AdvanceIssueWakeupParams{ID: w.ID, Enabled: w.Enabled, NextFireAt: next, LastTaskID: w.LastTaskID}); err != nil {
			return err
		}
	}
	if len(receipts) == 0 {
		if timedOut {
			if err = markTimedOut(); err != nil {
				return err
			}
		}
		return commit()
	}
	ids := make([]string, 0, len(receipts))
	manual := true
	for _, r := range receipts {
		ids = append(ids, r.ID)
		manual = manual && r.EventType == wakeupManualEventType
	}
	if w.Mode == "once" {
		enabled = 0
		next = sql.NullString{}
	}
	// A firing the agent already knows about starts no run. One that a run of
	// the agent waiting to start can take along keeps its inputs for that run
	// (JoinWaitingWakeups).
	if !taskExists {
		self, err := acknowledgedBySelf(ctx, q, w, w.AgentID.String, issue.ID, receipts)
		if err != nil {
			return err
		}
		if self {
			if err = q.ConsumeWakeupReceipts(ctx, db.ConsumeWakeupReceiptsParams{Ids: ids}); err != nil {
				return err
			}
			if err = q.AdvanceIssueWakeup(ctx, db.AdvanceIssueWakeupParams{ID: w.ID, Enabled: enabled, NextFireAt: next}); err != nil {
				return err
			}
			if timedOut {
				if err = markTimedOut(); err != nil {
					return err
				}
			}
			details := wakeupTriggerDetails(db.AgentTaskQueue{}, receipts)
			details["outcome"] = wakeupOutcomeAcknowledged
			delete(details, "task_id")
			if err = note(wakeupActivityTriggered, details); err != nil {
				return err
			}
			return commit()
		}
		waiting, err := hasWaitingRun(ctx, q, issue.ID, w.AgentID.String, w.CreatedBy.String)
		if err != nil {
			return err
		}
		if waiting {
			// As while its own run is claimed: timers advance, inputs stay,
			// and a once rule stays on until its input is handed over.
			if err = q.AdvanceIssueWakeup(ctx, db.AdvanceIssueWakeupParams{ID: w.ID, Enabled: w.Enabled, NextFireAt: next}); err != nil {
				return err
			}
			if timedOut {
				if err = markTimedOut(); err != nil {
					return err
				}
			}
			return commit()
		}
	}
	var chain []string
	if !taskExists {
		var loop bool
		if chain, loop, err = wakeupChain(ctx, q, w, receipts); err != nil {
			return err
		}
		// Runaway protection covers event-driven rules; schedules are bounded
		// by their own interval and deadline. A person's "wake now" is exempt.
		if !manual && w.Kind == "event" {
			reason := ""
			if loop {
				reason = wakeupPausedLoop
			} else {
				recent, e := q.CountRecentWakeupTasks(ctx, db.CountRecentWakeupTasksParams{WakeupID: nullString(util.UUIDToString(w.ID)), IssueID: nullString(w.IssueID), Since: nullTime(now.Add(-time.Hour), true).String})
				if e != nil {
					return e
				}
				if recent >= wakeupHourlyRunLimit {
					reason = wakeupPausedRate
				}
			}
			if reason != "" {
				if err = q.PauseIssueWakeup(ctx, db.PauseIssueWakeupParams{ID: w.ID, PausedReason: sql.NullString{String: reason, Valid: true}, BlockRuns: nullString(fmt.Sprint(true))}); err != nil {
					return err
				}
				if err = q.DiscardWakeupReceipts(ctx, w.ID); err != nil {
					return err
				}
				if err = note(wakeupActivityPaused, map[string]any{"reason": reason, "limit": wakeupHourlyRunLimit}); err != nil {
					return err
				}
				return commit()
			}
		}
	}
	noteText, evidence := mergeWakeupEvidence(w, task, receipts)
	if taskExists {
		task, err = q.ReplaceWakeupEvidence(ctx, db.ReplaceWakeupEvidenceParams{ID: task.ID, HandoffNote: sql.NullString{String: noteText, Valid: true}, WakeupEvidence: evidence})
	} else {
		if err = guardIssueNotInTriage(ctx, q, issue.ID, OriginNamed); err != nil {
			return err
		}
		contextJSON, _ := json.Marshal(map[string]any{"wakeup_id": util.UUIDToString(w.ID), "wakeup_revision": w.Revision, "wakeup_evidence": evidence, "wakeup_chain": chain})
		task, err = q.CreateWakeupTask(ctx, db.CreateWakeupTaskParams{ID: nullString(newV7()), AgentID: w.AgentID.String, RuntimeID: agent.RuntimeID, IssueID: nullString(w.IssueID), Priority: int64(priorityToInt(issue.Priority)), TriggerCommentID: w.ParentCommentID, TriggerSummary: sql.NullString{String: "Wakeup: " + truncateForSummary(w.Instruction, 160), Valid: true}, HandoffNote: sql.NullString{String: noteText, Valid: true}, OriginatorUserID: w.CreatedBy, AccountableUserID: w.CreatedBy, OriginatorSource: sql.NullString{String: "trigger_owner", Valid: true}, TriggerEvidenceKind: sql.NullString{String: "issue_wakeup", Valid: true}, TriggerEvidenceRefID: nullString(w.ID), DelegatedFromTaskID: w.SourceTaskID, WakeupContext: contextJSON, RuntimeMcpOverlay: nullString(string(overlay.Overlay)), RuntimeConnectedApps: nullString(string(overlay.ConnectedApps))})
	}
	if err != nil {
		return err
	}
	if err = q.ConsumeWakeupReceipts(ctx, db.ConsumeWakeupReceiptsParams{Ids: ids, TaskID: nullString(task.ID)}); err != nil {
		return err
	}
	if err = q.AdvanceIssueWakeup(ctx, db.AdvanceIssueWakeupParams{ID: w.ID, Enabled: enabled, NextFireAt: next, LastTaskID: nullString(task.ID)}); err != nil {
		return err
	}
	if timedOut {
		if err = markTimedOut(); err != nil {
			return err
		}
	}
	if !taskExists {
		if err = q.CountWakeupFires(ctx, w.ID); err != nil {
			return err
		}
		// Schedules speak for themselves in the run list; triggers from events,
		// conditions, a single time or a person get a timeline entry.
		if w.Kind != "every" && w.Kind != "cron" {
			if err = note(wakeupActivityTriggered, wakeupTriggerDetails(task, receipts)); err != nil {
				return err
			}
		}
		// The run that reaches the cap is legitimate; the rule stops after it.
		if (enabled != 0) && w.MaxFires.Valid && w.FireCount+1 >= w.MaxFires.Int64 {
			if err = q.PauseIssueWakeup(ctx, db.PauseIssueWakeupParams{ID: w.ID, PausedReason: sql.NullString{String: wakeupPausedMaxFires, Valid: true}, BlockRuns: nullString(fmt.Sprint(false))}); err != nil {
				return err
			}
			if err = note(wakeupActivityPaused, map[string]any{"reason": wakeupPausedMaxFires, "limit": w.MaxFires.Int64}); err != nil {
				return err
			}
		}
	}
	if err = commit(); err != nil {
		return err
	}
	s.Tasks.broadcastTaskEvent(ctx, protocol.EventTaskQueued, task)
	s.Tasks.NotifyTaskEnqueued(ctx, task)
	return nil
}

func qCleanupMissingWakeup(ctx context.Context, tx *sql.Tx, id string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM issue_wakeup_receipt WHERE wakeup_id=$1", id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM issue_wakeup WHERE id=$1", id); err != nil {
		return err
	}
	return tx.Commit()
}

// Disable withdraws only work not yet claimed. A running task keeps the
// ordinary Stop control; closing a subscription never kills unrelated work.
func (s *IssueWakeupService) Disable(ctx context.Context, issueID, id, member string) (db.IssueWakeup, error) {
	var out db.IssueWakeup
	tx, err := s.Tasks.TxStarter.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	q := s.Tasks.Queries.WithTx(tx)
	var workspace string
	if err = tx.QueryRowContext(ctx, "SELECT w.id FROM workspace w JOIN issue i ON i.workspace_id=w.id WHERE i.id=$1", issueID).Scan(&workspace); err != nil {
		return out, err
	}
	issue, err := q.LockWakeupIssue(ctx, issueID)
	if err != nil {
		return out, err
	}
	membership, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: member, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		return out, ErrWakeupForbidden
	}
	out, err = q.LockIssueWakeup(ctx, id)
	if err != nil {
		return out, err
	}
	if out.IssueID != issue.ID || out.WorkspaceID != issue.WorkspaceID {
		return db.IssueWakeup{}, sql.ErrNoRows
	}
	if out.SystemRule.Valid || (out.CreatedBy.String != member && membership.Role != "owner" && membership.Role != "admin") {
		return db.IssueWakeup{}, ErrWakeupForbidden
	}
	_, err = tx.ExecContext(ctx, "UPDATE issue_wakeup SET enabled=false,disabled_at=COALESCE(disabled_at,now()),updated_at=now() WHERE id=$1", id)
	if err != nil {
		return out, err
	}
	if err = q.DiscardWakeupReceipts(ctx, id); err != nil {
		return out, err
	}
	tasks, err := q.CancelUnstartedWakeupTasks(ctx, nullString(util.UUIDToString(id)))
	if err != nil {
		return out, err
	}
	out, err = q.LockIssueWakeup(ctx, id)
	if err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	for _, task := range tasks {
		s.Tasks.broadcastTaskEvent(ctx, protocol.EventTaskCancelled, task)
	}
	return out, nil
}

// CheckClaim revalidates stored human authority after an offline wait. The
// enqueue-time MCP overlay does not grant permission to execute forever.
func (s *IssueWakeupService) CheckClaim(ctx context.Context, task db.AgentTaskQueue) error {
	var source struct {
		ID       string `json:"wakeup_id"`
		Revision int64  `json:"wakeup_revision"`
	}
	if err := json.Unmarshal([]byte(task.Context.String), &source); err != nil || source.ID == "" {
		return nil
	}
	id, err := wakeupUUID(source.ID)
	if err != nil {
		return ErrWakeupForbidden
	}
	w, err := s.Tasks.Queries.LocklessWakeup(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrWakeupForbidden
	}
	if err != nil {
		return err
	}
	if w.SystemRule.Valid {
		// A system rule's run targets the assignee resolved when it fired; it
		// is claimable while the rule is on and that agent can still run.
		if (w.Enabled == 0) || w.DisabledAt.Valid || w.Revision != source.Revision || w.IssueID != task.IssueID.String {
			return ErrWakeupForbidden
		}
		agent, err := s.Tasks.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: task.AgentID, WorkspaceID: w.WorkspaceID})
		if errors.Is(err, sql.ErrNoRows) || (err == nil && (agent.ArchivedAt.Valid || !agent.RuntimeID.Valid)) {
			return ErrWakeupForbidden
		}
		return err
	}
	if w.DisabledAt.Valid || w.Revision != source.Revision || w.IssueID != task.IssueID.String || w.AgentID.String != task.AgentID || w.CreatedBy.String != task.OriginatorUserID.String {
		return ErrWakeupForbidden
	}
	agent, err := s.Tasks.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: w.AgentID.String, WorkspaceID: w.WorkspaceID})
	if errors.Is(err, sql.ErrNoRows) {
		return ErrWakeupForbidden
	}
	if err != nil {
		return err
	}
	return s.authorize(ctx, s.Tasks.Queries, w.WorkspaceID, w.CreatedBy.String, agent)
}

func txNow(ctx context.Context, tx *sql.Tx) (time.Time, error) {
	var s string
	if err := tx.QueryRowContext(ctx, "SELECT now()").Scan(&s); err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339Nano, s)
}

func lockTaskOwnerRows(ctx context.Context, tx *sql.Tx, agentID, issueID, runtimeID any) (bool, error) {
	var ok bool
	err := tx.QueryRowContext(ctx, `
		SELECT
			($1 IS NULL OR $1 = '' OR EXISTS (SELECT 1 FROM agent WHERE id = $1 AND EXISTS (SELECT 1 FROM workspace WHERE id = agent.workspace_id)))
			AND ($2 IS NULL OR $2 = '' OR EXISTS (SELECT 1 FROM issue WHERE id = $2 AND EXISTS (SELECT 1 FROM workspace WHERE id = issue.workspace_id)))
			AND ($3 IS NULL OR $3 = '' OR EXISTS (SELECT 1 FROM agent_runtime WHERE id = $3 AND EXISTS (SELECT 1 FROM workspace WHERE id = agent_runtime.workspace_id)))
	`, agentID, issueID, runtimeID).Scan(&ok)
	return ok, err
}
