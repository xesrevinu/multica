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
	if in.CallerType == "agent" {
		if !in.AssigneeChange && in.ToStatus == StInProgress && (in.FromStatus == StTodo || in.FromStatus == StInProgress) {
			return ""
		}
		return "this project is run by the orchestrator: agents may only set status in_progress and never change the assignee. " +
			"Hand over with issue metadata instead: phase=review_requested (+ head_sha, pr), verdict=approve|changes + reviewed_sha, or phase=blocked + blocked_reason."
	}
	if in.Override {
		return ""
	}
	return fmt.Sprintf("this project is run by the orchestrator, which owns status and assignee. Use `multica flow unblock|accept|reject|replay`, " +
		"pause the team (`multica team pause`), or retry with --force (X-Orchestrator-Override: 1) to override")
}

// EngineOwnedMetaKeys may only be written by the orchestrator (or a member
// via `multica flow`); agents writing them would forge counters or gates.
var EngineOwnedMetaKeys = map[string]bool{
	"owner": true, "attempt_impl": true, "attempt_review": true, "attempt_infra": true,
	"merged_sha": true, "pr_number": true, "pr": true, "acceptance": true, "acceptance_required": true, "review_started_sha": true,
	"paused": true, "cancel_requested": true,
}

// AgentPhaseValues are the phase values an agent may write. unblocked and
// cancel_proposed are the lead's answers to a blocked issue; the engine acts
// on them only while the issue is blocked (T7/T8).
var AgentPhaseValues = map[string]bool{PhReviewRequested: true, PhBlocked: true, PhImplementing: true, PhUnblocked: true, PhCancelProposed: true, PhPlanned: true}

// CheckAgentMeta refuses agent metadata writes that would forge engine state.
func (s *Service) CheckAgentMeta(projectID, key, value string) string {
	if !s.Owns(projectID) {
		return ""
	}
	if EngineOwnedMetaKeys[key] {
		return fmt.Sprintf("metadata key %q is owned by the orchestrator", key)
	}
	if key == "phase" && !AgentPhaseValues[trimJSON(value)] {
		return fmt.Sprintf("agents may set phase to review_requested, blocked, implementing, unblocked, cancel_proposed or planned, not %s", value)
	}
	if key == "risk" && !RiskValues[strings.ToLower(trimJSON(value))] {
		return fmt.Sprintf("risk must be low, medium or high, not %s", value)
	}
	return ""
}

// RiskValues: the reviewer's risk tier; high forces human acceptance.
var RiskValues = map[string]bool{"low": true, "medium": true, "high": true}

func trimJSON(v string) string {
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		return v[1 : len(v)-1]
	}
	return v
}
