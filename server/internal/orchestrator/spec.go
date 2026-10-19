// Package orchestrator is the in-app, deterministic state machine that moves
// issues of an opted-in project through implement → review → (accept) → merge.
//
// Agents only work: they write issue metadata (phase, head_sha, verdict…), one
// result comment, and may set in_progress. Every other status or assignee
// change belongs to the orchestrator (see guard.go). The decision function in
// engine.go is pure; runtime.go feeds it snapshots from events and a periodic
// reconciler and applies its decisions through the ordinary issue API, so a
// transition starts exactly one run, the same way a person's update does.
package orchestrator

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Statuses are the seven built-in status keys the engine uses.
const (
	StBacklog    = "backlog"
	StTodo       = "todo"
	StInProgress = "in_progress"
	StInReview   = "in_review"
	StDone       = "done"
	StBlocked    = "blocked"
	StCancelled  = "cancelled"
)

// Phases live in issue metadata key "phase".
const (
	PhImplementing     = "implementing"
	PhReviewRequested  = "review_requested" // agent-written hand-over signal
	PhReviewing        = "reviewing"
	PhChangesRequested = "changes_requested"
	PhAwaitAcceptance  = "awaiting_acceptance"
	PhBlocked          = "blocked"         // agent- or engine-written
	PhUnblocked        = "unblocked"       // lead/member-written
	PhCancelProposed   = "cancel_proposed" // lead/member-written
	PhMerged           = "merged"
	PhCancelled        = "cancelled"
)

// Spec is the declarative team definition of one project (stored as JSONB in
// orchestrator_team.spec; authored as the `orchestrator:` YAML section).
type Spec struct {
	Preset      string       `json:"preset,omitempty" yaml:"preset,omitempty"`
	Roles       Roles        `json:"roles" yaml:"roles"`
	Repo        RepoSpec     `json:"repo" yaml:"repo"`
	Limits      Limits       `json:"limits" yaml:"limits"`
	Acceptance  Acceptance   `json:"acceptance" yaml:"acceptance"`
	Merge       MergeSpec    `json:"merge" yaml:"merge"`
	Transitions []Transition `json:"transitions,omitempty" yaml:"transitions,omitempty"`
}

type Roles struct {
	Lead     string   `json:"lead" yaml:"lead"`         // agent name
	Reviewer string   `json:"reviewer" yaml:"reviewer"` // agent name
	Owners   []string `json:"owners" yaml:"owners"`     // agent names
	// Human is the member (email, name or user id) who receives the acceptance
	// gate and notifications. Empty = the team's applier.
	Human string `json:"human,omitempty" yaml:"human,omitempty"`
}

type RepoSpec struct {
	Mode           string   `json:"mode" yaml:"mode"` // "github" or "local"
	Slug           string   `json:"slug,omitempty" yaml:"slug,omitempty"`
	URL            string   `json:"url,omitempty" yaml:"url,omitempty"`
	Bare           string   `json:"bare,omitempty" yaml:"bare,omitempty"` // local mode: bare remote path
	Hub            string   `json:"hub,omitempty" yaml:"hub,omitempty"`   // local mode: worktree source clone (ff-pulled after merge)
	Main           string   `json:"main" yaml:"main"`
	BranchPrefix   string   `json:"branch_prefix" yaml:"branch_prefix"`
	CheckCmd       string   `json:"check_cmd,omitempty" yaml:"check_cmd,omitempty"`
	RequiredChecks []string `json:"required_checks,omitempty" yaml:"required_checks,omitempty"`
	DiskPath       string   `json:"disk_path,omitempty" yaml:"disk_path,omitempty"`
}

type Limits struct {
	Impl             int `json:"impl" yaml:"impl"`
	Review           int `json:"review" yaml:"review"`
	Infra            int `json:"infra" yaml:"infra"`
	StuckMin         int `json:"stuck_min" yaml:"stuck_min"`
	IdleMin          int `json:"idle_min" yaml:"idle_min"` // no task_message for this long → cancel; default 8, negative disables
	MinFreeGB        int `json:"min_free_gb" yaml:"min_free_gb"`
	BlockedRemindMin int `json:"blocked_remind_min" yaml:"blocked_remind_min"`
	Failures         int `json:"dead_letter_after" yaml:"dead_letter_after"`
}

// Acceptance flags issues that need a human acceptance gate before merge
// (macOS GUI / simulator evidence, sign-off).
type Acceptance struct {
	Labels []string `json:"labels" yaml:"labels"`
}

type MergeSpec struct {
	Method string `json:"method" yaml:"method"` // squash
}

// Transition is one declared edge, used for validation, diagrams and assertions.
type Transition struct {
	ID    string   `json:"id" yaml:"id"`
	Actor string   `json:"actor" yaml:"actor"` // engine | agent | member
	From  []string `json:"from" yaml:"from"`
	To    []string `json:"to" yaml:"to"`
	Label string   `json:"label,omitempty" yaml:"label,omitempty"`
}

var builtinStatuses = map[string]bool{StBacklog: true, StTodo: true, StInProgress: true, StInReview: true, StDone: true, StBlocked: true, StCancelled: true}

// PresetTransitions is the edge list of the impl-review-merge preset; it is the
// machine the engine implements.
func PresetTransitions() []Transition {
	all := []string{StBacklog, StTodo, StInProgress, StInReview, StBlocked}
	return []Transition{
		{ID: "T1_start", Actor: "engine", From: []string{StBacklog}, To: []string{StTodo}, Label: "deps done, stage open, disk ok, owner free → owner"},
		{ID: "A1_in_progress", Actor: "agent", From: []string{StTodo}, To: []string{StInProgress}, Label: "owner starts"},
		{ID: "T2_review", Actor: "engine", From: []string{StTodo, StInProgress}, To: []string{StInReview}, Label: "phase=review_requested, branch pushed, no active run → reviewer"},
		{ID: "T3_rework", Actor: "engine", From: []string{StInReview, StTodo, StInProgress}, To: []string{StTodo}, Label: "changes | checks red | conflict | acceptance rejected → owner"},
		{ID: "T3c_carry_verdict", Actor: "engine", From: []string{StInReview}, To: []string{StInReview}, Label: "approved patch unchanged by rebase → keep verdict"},
		{ID: "T3b_rereview", Actor: "engine", From: []string{StInReview}, To: []string{StInReview}, Label: "no/stale verdict → reviewer again"},
		{ID: "T4_done", Actor: "engine", From: []string{StInReview}, To: []string{StDone}, Label: "approve@head, checks green, mergeable, accepted → merge"},
		{ID: "T5_rerun", Actor: "engine", From: []string{StTodo, StInProgress}, To: []string{StTodo, StInProgress}, Label: "run ended without hand-over → rerun (attempt++)"},
		{ID: "T5b_cancel_stuck", Actor: "engine", From: []string{StTodo, StInProgress, StInReview}, To: []string{StTodo, StInProgress, StInReview}, Label: "run older than stuck_min → cancel"},
		{ID: "T6_block", Actor: "engine", From: all, To: []string{StBlocked}, Label: "attempts exhausted | phase=blocked | dead-letter → lead"},
		{ID: "T7_unblock", Actor: "engine", From: []string{StBlocked}, To: []string{StTodo}, Label: "phase=unblocked → owner, counters reset"},
		{ID: "T8_cancel", Actor: "engine", From: []string{StBlocked}, To: []string{StCancelled}, Label: "phase=cancel_proposed"},
		{ID: "T9_reconcile", Actor: "engine", From: all, To: []string{StTodo, StInReview, StDone, StCancelled, StBlocked}, Label: "status inconsistent with engine phase (crash / out-of-band) → converge"},
		{ID: "T10_await_acceptance", Actor: "engine", From: []string{StInReview}, To: []string{StInReview}, Label: "gated issue approved & green → human acceptance"},
	}
}

// Defaults fills unset fields from the preset.
func (s *Spec) Defaults() {
	if s.Preset == "" {
		s.Preset = "impl-review-merge"
	}
	d := func(p *int, v int) {
		if *p <= 0 {
			*p = v
		}
	}
	d(&s.Limits.Impl, 3)
	d(&s.Limits.Review, 3)
	d(&s.Limits.Infra, 2)
	d(&s.Limits.StuckMin, 20)
	d(&s.Limits.IdleMin, 8)
	d(&s.Limits.MinFreeGB, 20)
	d(&s.Limits.BlockedRemindMin, 60)
	d(&s.Limits.Failures, 5)
	if s.Repo.Mode == "" {
		s.Repo.Mode = "github"
	}
	if s.Repo.Main == "" {
		s.Repo.Main = "main"
	}
	if s.Repo.BranchPrefix == "" {
		s.Repo.BranchPrefix = "agent/"
	}
	if s.Merge.Method == "" {
		s.Merge.Method = "squash"
	}
	if s.Acceptance.Labels == nil {
		s.Acceptance.Labels = []string{"needs-acceptance", "needs-GUI-Master", "needs-Ray-signoff"}
	}
	if len(s.Transitions) == 0 {
		s.Transitions = PresetTransitions()
	}
}

// Validate enforces the invariants no team may relax (absorbed from the
// issueworkflow transition-table draft): known vocabulary, unique ids, and
// terminal statuses reachable only by the engine or a member.
func (s *Spec) Validate() error {
	if s.Preset != "impl-review-merge" {
		return fmt.Errorf("unknown preset %q (supported: impl-review-merge)", s.Preset)
	}
	if s.Roles.Reviewer == "" || s.Roles.Lead == "" || len(s.Roles.Owners) == 0 {
		return fmt.Errorf("roles.lead, roles.reviewer and at least one roles.owners entry are required")
	}
	for _, o := range s.Roles.Owners {
		if o == s.Roles.Reviewer {
			return fmt.Errorf("owner %q cannot also be the reviewer (independent verifier)", o)
		}
	}
	switch s.Repo.Mode {
	case "github":
		if s.Repo.Slug == "" {
			return fmt.Errorf("repo.slug (owner/name) is required in github mode")
		}
	case "local":
		if s.Repo.Bare == "" {
			return fmt.Errorf("repo.bare is required in local mode")
		}
	default:
		return fmt.Errorf("repo.mode must be github or local")
	}
	seen := map[string]bool{}
	for _, t := range s.Transitions {
		if t.ID == "" || seen[t.ID] {
			return fmt.Errorf("transition id %q empty or duplicated", t.ID)
		}
		seen[t.ID] = true
		if t.Actor != "engine" && t.Actor != "agent" && t.Actor != "member" {
			return fmt.Errorf("transition %s: unknown actor %q", t.ID, t.Actor)
		}
		for _, st := range append(append([]string{}, t.From...), t.To...) {
			if !builtinStatuses[st] {
				return fmt.Errorf("transition %s: unknown status %q", t.ID, st)
			}
		}
		for _, st := range t.To {
			if (st == StDone || st == StCancelled) && t.Actor == "agent" {
				return fmt.Errorf("transition %s: an agent can never move an issue to %s", t.ID, st)
			}
		}
	}
	return nil
}

// ParseSpec accepts either a bare spec or a document with an `orchestrator:`
// key (project.yaml style), YAML or JSON.
func ParseSpec(b []byte) (*Spec, error) {
	var wrap struct {
		Orchestrator *Spec `yaml:"orchestrator" json:"orchestrator"`
	}
	var s Spec
	if err := yaml.Unmarshal(b, &wrap); err == nil && wrap.Orchestrator != nil {
		s = *wrap.Orchestrator
	} else if err := yaml.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("parse spec: %w", err)
	}
	s.Defaults()
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return &s, nil
}

func (s *Spec) JSON() []byte { b, _ := json.Marshal(s); return b }

// Mermaid renders the declared machine as a stateDiagram; current (optional)
// highlights the issue's status.
func (s *Spec) Mermaid(current string) string {
	var b strings.Builder
	b.WriteString("stateDiagram-v2\n  [*] --> backlog\n")
	for _, t := range s.Transitions {
		for _, f := range t.From {
			for _, to := range t.To {
				if f == to && t.ID != "T3b_rereview" && t.ID != "T10_await_acceptance" {
					continue
				}
				suffix := ""
				if t.Actor != "engine" {
					suffix = " (" + t.Actor + ")"
				}
				fmt.Fprintf(&b, "  %s --> %s: %s%s\n", f, to, t.ID, suffix)
			}
		}
	}
	b.WriteString("  done --> [*]\n  cancelled --> [*]\n")
	if current != "" {
		fmt.Fprintf(&b, "  classDef current fill:#f96,stroke:#333,stroke-width:3px\n  class %s current\n", current)
	}
	return b.String()
}
