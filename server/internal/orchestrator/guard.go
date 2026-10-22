package orchestrator

import (
	"fmt"
	"strings"
)

// ErrCodeOwned is the machine-readable API error code of a refused write.
const ErrCodeOwned = "orchestrator_owned"

// GuardInput describes an attempted issue write.
type GuardInput struct {
	ProjectID      string
	CallerType     string // agent | member
	UserID         string
	FromStatus     string
	ToStatus       string // "" = unchanged
	AssigneeChange bool
	Override       bool // member asked to bypass (X-Orchestrator-Override: 1)
}

// CheckWrite returns a non-nil refusal message when the write must be refused.
//
//   - the orchestrator's own member: always allowed
//   - agents: may only move todo → in_progress; never the assignee
//   - members: refused unless they explicitly override (the reconciler will
//     then either accept the edit or converge it; see T9) — or use `multica flow`
func (s *Service) CheckWrite(in GuardInput) string {
	if !s.Owns(in.ProjectID) || s.IsServiceUser(in.UserID) {
		return ""
	}
	if in.ToStatus == "" && !in.AssigneeChange {
		return ""
	}
	var what []string
	if in.ToStatus != "" {
		what = append(what, fmt.Sprintf("status %s → %s", in.FromStatus, in.ToStatus))
	}
	if in.AssigneeChange {
		what = append(what, "assignee change")
	}
	refused := strings.Join(what, " and ")
	if in.CallerType == "agent" {
		if !in.AssigneeChange && in.ToStatus == StInProgress && (in.FromStatus == StTodo || in.FromStatus == StInProgress) {
			return ""
		}
		return "this project is run by the orchestrator (refused: " + refused + "): agents may only set status in_progress and never change the assignee, with or without --no-start. " +
			"Hand over with issue metadata instead: phase=review_requested (+ head_sha, pr), verdict=approve|changes + reviewed_sha, or phase=blocked + blocked_reason."
	}
	if in.Override {
		return ""
	}
	return "this project is run by the orchestrator, which owns status and assignee (refused: " + refused + "). " +
		"--no-start does not lift this: it only skips the agent run, the field still belongs to the orchestrator. " +
		"Use `multica flow unblock|accept|reject|cancel|replay`, pause the team (`multica team pause`), " +
		"or override once with --force (X-Orchestrator-Override: 1); the orchestrator may still converge the issue afterwards"
}

// EngineOwnedMetaKeys may only be written by the orchestrator (or a member
// via `multica flow`); agents writing them would forge counters or gates.
var EngineOwnedMetaKeys = map[string]bool{
	"owner": true, "expert_done": true, "attempt_impl": true, "attempt_review": true, "attempt_infra": true, "attempt_quota": true,
	"merged_sha": true, "pr_number": true, "pr": true, "acceptance": true, "acceptance_required": true, "review_started_sha": true,
	"paused": true, "cancel_requested": true, "verdict_carried_from": true, "last_reviewed_sha": true,
	"attempt_conflict": true, "conflict_from_phase": true, "conflict_head": true, "conflict_handback": true, "conflict_requested": true,
	// member confirmations and engine bookkeeping for blocked issues (T7/T7b/T8a/T8)
	"cancel_confirmed": true, "unblock_confirmed": true, "unblock_count": true, "unblock_escalated": true, "cancel_notified": true,
}

// AgentPhaseValues are the phase values an agent may write. unblocked and
// cancel_proposed are the lead's answers to a blocked issue; the engine acts
// on them only while the issue is blocked (T7/T8).
var AgentPhaseValues = map[string]bool{PhReviewRequested: true, PhBlocked: true, PhImplementing: true, PhUnblocked: true, PhCancelProposed: true, PhPlanned: true}

// ProtectedMetaKeys are the agent-writable hand-over keys the engine reads.
// Agents may set them (within CheckAgentMetaWrite's rules) but never delete
// them: deleting phase/verdict/risk would silently rewind or ungate a flow.
var ProtectedMetaKeys = map[string]bool{
	"phase": true, "verdict": true, "reviewed_sha": true, "head_sha": true, "risk": true, "blocked_reason": true, "deps": true,
}

// VerdictValues are the only verdicts the engine understands.
var VerdictValues = map[string]bool{"approve": true, "changes": true}

// MetaWrite is one attempted agent metadata write or delete.
type MetaWrite struct {
	ProjectID string
	Status    string // issue status before the write ("" = unknown)
	Key       string
	Value     string            // raw JSON value (ignored for Delete)
	Current   map[string]string // current metadata (for raise-only keys)
	Delete    bool
}

// phaseStatuses: the statuses in which an agent may write each phase value.
var phaseStatuses = map[string][]string{
	PhReviewRequested: {StTodo, StInProgress},
	PhImplementing:    {StTodo, StInProgress},
	PhBlocked:         {StTodo, StInProgress, StInReview},
	PhUnblocked:       {StBlocked},
	PhCancelProposed:  {StBlocked},
	PhPlanned:         {StBacklog, StTodo, StInProgress},
}

// CheckAgentMeta is CheckAgentMetaWrite without status/current context.
func (s *Service) CheckAgentMeta(projectID, key, value string) string {
	return s.CheckAgentMetaWrite(MetaWrite{ProjectID: projectID, Key: key, Value: value})
}

// CheckAgentMetaWrite refuses agent metadata writes and deletes that would
// forge or rewind engine state. Members and the engine are never checked.
func (s *Service) CheckAgentMetaWrite(w MetaWrite) string {
	if !s.Owns(w.ProjectID) {
		return ""
	}
	key := w.Key
	if EngineOwnedMetaKeys[key] {
		return fmt.Sprintf("metadata key %q is owned by the orchestrator: only the engine or a member (multica flow) may change it", key)
	}
	if w.Delete {
		if ProtectedMetaKeys[key] {
			return fmt.Sprintf("metadata key %q drives the orchestrator: agents may set it but not delete it", key)
		}
		return ""
	}
	v := trimJSON(w.Value)
	switch key {
	case "phase":
		ok, known := phaseStatuses[v]
		if !known || !AgentPhaseValues[v] {
			return fmt.Sprintf("agents may set phase to review_requested, blocked, implementing, unblocked, cancel_proposed or planned, not %s", w.Value)
		}
		if w.Status != "" && !contains(ok, w.Status) {
			return fmt.Sprintf("phase=%s is not an agent hand-over while the issue is %s (allowed in: %s)", v, w.Status, strings.Join(ok, ", "))
		}
	case "verdict":
		if !VerdictValues[strings.ToLower(v)] {
			return fmt.Sprintf("verdict must be approve or changes, not %s", w.Value)
		}
		fallthrough
	case "reviewed_sha":
		// only the reviewer runs while the issue is in_review; an owner (todo /
		// in_progress) must not approve its own change.
		if w.Status != "" && w.Status != StInReview {
			return fmt.Sprintf("%s is the reviewer's hand-over and may only be written while the issue is in_review (it is %s)", key, w.Status)
		}
	case "conflict_result":
		if v != "resolved" && v != "cannot_resolve" {
			return fmt.Sprintf("conflict_result must be resolved or cannot_resolve, not %s", w.Value)
		}
		if w.Status != "" && w.Status != StInReview {
			return fmt.Sprintf("conflict_result is the resolver's hand-over and may only be written while the issue is in_review (it is %s)", w.Status)
		}
	case "risk":
		if !RiskValues[strings.ToLower(v)] {
			return fmt.Sprintf("risk must be low, medium or high, not %s", w.Value)
		}
		if cur := strings.ToLower(w.Current["risk"]); riskRank[cur] > riskRank[strings.ToLower(v)] {
			return fmt.Sprintf("risk may be raised but not lowered by an agent (%s → %s); a member decides", cur, v)
		}
	}
	return ""
}

// PauseLabel on any open issue pauses the whole project (runtime.pausedWhy).
const PauseLabel = "orchestrator-pause"

// LabelWrite describes an attempted label attach/detach (or a label supplied
// when the issue is created, which is an attach).
type LabelWrite struct {
	ProjectID  string
	CallerType string // agent | member
	UserID     string
	Label      string
	Attach     bool
}

// CheckLabel refuses label changes that would halt or ungate an orchestrated
// issue:
//   - orchestrator-pause pauses the whole project. Only a human member may add
//     it (never an agent, never the engine's own member); removing it is for a
//     human or the engine, never an agent. This holds while the team is paused
//     too, because the label is the kill switch.
//   - acceptance labels (labels and default_for_labels): agents may add them
//     (that only raises the gate, like risk) but not remove them.
func (s *Service) CheckLabel(w LabelWrite) string {
	t := s.Team(w.ProjectID)
	if t == nil {
		return ""
	}
	l := strings.ToLower(strings.TrimSpace(w.Label))
	if l == PauseLabel {
		switch {
		case w.CallerType == "agent":
			return "label orchestrator-pause pauses the whole project: only a human may add it and only a human or the orchestrator may remove it (or use `multica team pause`)"
		case w.Attach && s.IsServiceUser(w.UserID):
			return "label orchestrator-pause may only be added by a human"
		}
		return ""
	}
	if w.CallerType != "agent" || t.Paused {
		return ""
	}
	gate := hasLabel([]string{l}, t.Spec.Acceptance.Labels) || hasLabel([]string{l}, t.Spec.Acceptance.DefaultForLabels)
	if gate && !w.Attach {
		return fmt.Sprintf("label %q is an acceptance gate of this orchestrated project: agents may add it but only a member may remove it", w.Label)
	}
	return ""
}

// CheckAgentLabel is CheckLabel for an agent caller.
func (s *Service) CheckAgentLabel(projectID, label string, attach bool) string {
	return s.CheckLabel(LabelWrite{ProjectID: projectID, CallerType: "agent", Label: label, Attach: attach})
}

var riskRank = map[string]int{"low": 1, "medium": 2, "high": 3}

// RiskValues: the reviewer's risk tier; high forces human acceptance.
var RiskValues = map[string]bool{"low": true, "medium": true, "high": true}

func trimJSON(v string) string {
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		return v[1 : len(v)-1]
	}
	return v
}
