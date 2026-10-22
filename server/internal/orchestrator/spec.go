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
	PhPlanned          = "planned"         // lead-written on an epic it split; the engine never starts it
	PhMerged           = "merged"
	PhCancelled        = "cancelled"
	// PhResolvingConflict: the reviewer agent is merging main into the
	// issue branch with the resolve-conflict skill (T11).
	PhResolvingConflict = "resolving_conflict"
)

// Spec is the declarative team definition of one project (stored as JSONB in
// orchestrator_team.spec; authored as the `orchestrator:` YAML section).
type Spec struct {
	Preset string `json:"preset,omitempty" yaml:"preset,omitempty"`
	// Shadow: decide and log every transition (state "shadow") but change
	// nothing: no status/assignee/metadata writes, no runs, no PRs, merges or
	// CI reruns, no write guard. For trialling a spec on live issues.
	Shadow      bool         `json:"shadow,omitempty" yaml:"shadow,omitempty"`
	Roles       Roles        `json:"roles" yaml:"roles"`
	Experts     []Expert     `json:"experts,omitempty" yaml:"experts,omitempty"`
	Repo        RepoSpec     `json:"repo" yaml:"repo"`
	Limits      Limits       `json:"limits" yaml:"limits"`
	Acceptance  Acceptance   `json:"acceptance" yaml:"acceptance"`
	Merge       MergeSpec    `json:"merge" yaml:"merge"`
	Transitions []Transition `json:"transitions,omitempty" yaml:"transitions,omitempty"`
	// AutoStart: off (default) | dry_run | on. See decidePromote.
	AutoStart string `json:"auto_start,omitempty" yaml:"auto_start,omitempty"`
	// authoredOwners is roles.owners before route owners are merged. Expert
	// agents are never merged. Not serialized; Defaults fills it.
	authoredOwners []string
}

type Roles struct {
	Lead     string   `json:"lead" yaml:"lead"`         // agent name
	Reviewer string   `json:"reviewer" yaml:"reviewer"` // agent name
	Owners   []string `json:"owners" yaml:"owners"`     // agent names
	// Human is the member (email, name or user id) who receives the acceptance
	// gate and notifications. Empty = the team's applier.
	Human string `json:"human,omitempty" yaml:"human,omitempty"`
	// Routes pick the owner pool by issue label (first match wins); an issue
	// matching no route uses Owners. Route owners are added to Owners.
	Routes []Route `json:"routes,omitempty" yaml:"routes,omitempty"`
}

// Route maps issue labels (any match, case-insensitive) to an owner pool.
type Route struct {
	Labels []string `json:"labels" yaml:"labels"`
	Owners []string `json:"owners" yaml:"owners"`
}

// Expert is a label-routed pool that is not part of the default owners.
// Checked before routes; an unlabelled issue never matches one unless its
// when.metadata opt-in matches.
type Expert struct {
	Name         string       `json:"name" yaml:"name"`
	Agents       []string     `json:"agents" yaml:"agents"`
	When         ExpertWhen   `json:"when" yaml:"when"`
	Mode         string       `json:"mode,omitempty" yaml:"mode,omitempty"` // implement (default) | consult
	Skills       []string     `json:"skills,omitempty" yaml:"skills,omitempty"`
	Instructions string       `json:"instructions,omitempty" yaml:"instructions,omitempty"`
	Limits       ExpertLimits `json:"limits,omitempty" yaml:"limits,omitempty"`
}

// ExpertWhen is AND across the groups that are set. Labels are any-of,
// case-insensitive, same as routes. not_labels reject a match. metadata
// values must equal the issue metadata (explicit opt-in).
type ExpertWhen struct {
	Labels    []string          `json:"labels,omitempty" yaml:"labels,omitempty"`
	NotLabels []string          `json:"not_labels,omitempty" yaml:"not_labels,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// ExpertLimits caps how many project issues this expert may hold at once.
type ExpertLimits struct {
	Concurrent int `json:"concurrent,omitempty" yaml:"concurrent,omitempty"`
}

type RepoSpec struct {
	Mode string `json:"mode" yaml:"mode"` // "github" or "local"
	Slug string `json:"slug,omitempty" yaml:"slug,omitempty"`
	URL  string `json:"url,omitempty" yaml:"url,omitempty"`
	Bare string `json:"bare,omitempty" yaml:"bare,omitempty"` // local mode: bare remote path
	// Hub is the local main checkout the project's local_directory resource
	// points at (both modes). The daemon cuts every task worktree from it
	// (with `wt switch` when it has .config/wt.toml, so hooks run); the engine
	// keeps it fresh: fetch + ff-only main before a run starts and after a
	// merge, and uses `git worktree add` from it instead of cloning for its own
	// integration checkout. Pushes and PRs still go to origin.
	Hub            string   `json:"hub,omitempty" yaml:"hub,omitempty"`
	Main           string   `json:"main" yaml:"main"`
	BranchPrefix   string   `json:"branch_prefix" yaml:"branch_prefix"`
	CheckCmd       string   `json:"check_cmd,omitempty" yaml:"check_cmd,omitempty"`
	RequiredChecks []string `json:"required_checks,omitempty" yaml:"required_checks,omitempty"`
	// DiskPath overrides where the free-disk check looks; empty = the wt
	// (worktrunk) worktree-path root, see wt.go.
	DiskPath string `json:"disk_path,omitempty" yaml:"disk_path,omitempty"`
}

type Limits struct {
	Impl   int `json:"impl" yaml:"impl"`
	Review int `json:"review" yaml:"review"`
	Infra  int `json:"infra" yaml:"infra"`
	// Conflict: resolve-conflict runs per conflict episode before the
	// owner gets it back (T11b). 0 = preset default 2; negative disables
	// conflict resolution (conflicts go straight back to the owner).
	Conflict int `json:"conflict,omitempty" yaml:"conflict,omitempty"`
	StuckMin int `json:"stuck_min" yaml:"stuck_min"`
	// IdleMin, MinFreeGB, QueuedMin and Unblocks are pointers so an unset
	// value (nil → preset default) differs from an explicit 0 or negative.
	// IdleMin: a running run with no task_message for this long is cancelled;
	// default 8, 0 or negative disables.
	IdleMin *int `json:"idle_min,omitempty" yaml:"idle_min,omitempty"`
	// IdleMinByBackend overrides idle_min per agent backend (the runtime's
	// provider, e.g. gbot, claude, codex); 0 or negative disables idle
	// detection for that backend. Backends that report no output while they
	// work (gbot buffers its whole run) would otherwise be killed as idle.
	// The preset adds gbot: -1 unless the spec names gbot itself.
	IdleMinByBackend map[string]int `json:"idle_min_by_backend,omitempty" yaml:"idle_min_by_backend,omitempty"`
	// MinFreeGB: T1 waits while the worktree disk has less free space;
	// default 20, 0 or negative disables the disk guard.
	MinFreeGB *int `json:"min_free_gb,omitempty" yaml:"min_free_gb,omitempty"`
	// QueuedMin: a run still queued/dispatched (never started) after this
	// long is cancelled as stuck; default 15, 0 or negative disables.
	QueuedMin *int `json:"queued_min,omitempty" yaml:"queued_min,omitempty"`
	// Unblocks: how many times the lead's phase=unblocked may reset the
	// counters; past it the issue stays blocked and escalates to the human
	// (only `multica flow unblock` resets). Default 2; 0 = the lead never may.
	Unblocks         *int `json:"unblocks,omitempty" yaml:"unblocks,omitempty"`
	BlockedRemindMin int  `json:"blocked_remind_min" yaml:"blocked_remind_min"`
	Failures         int  `json:"dead_letter_after" yaml:"dead_letter_after"`
	// Slots caps concurrently implementing issues per resource: an issue
	// labelled "needs:<name>" starts only while fewer than Slots[name]
	// project issues with that label are todo/in_progress.
	Slots map[string]int `json:"slots,omitempty" yaml:"slots,omitempty"`
}

// Acceptance flags issues that need a human acceptance gate before merge
// (macOS GUI / simulator evidence, sign-off).
type Acceptance struct {
	Labels []string `json:"labels" yaml:"labels"`
	// DefaultForLabels gates issues carrying any of these labels too (e.g.
	// area:studio-app = every screen change waits for the human).
	DefaultForLabels []string `json:"default_for_labels,omitempty" yaml:"default_for_labels,omitempty"`
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
		{ID: "T0_dispatch", Actor: "engine", From: []string{StBacklog}, To: []string{StInProgress}, Label: "auto_start=on only: backlog, deps done, stage open, not an epic, owner free → owner"},
		{ID: "T_epic_progress", Actor: "engine", From: all, To: []string{StInProgress}, Label: "epic: a sub-issue started → in_progress (no agent)"},
		{ID: "T_epic_acceptance", Actor: "engine", From: all, To: []string{StInReview}, Label: "epic: every sub-issue done, acceptance needed → @human"},
		{ID: "T_epic_done", Actor: "engine", From: append(all, StInReview), To: []string{StDone}, Label: "epic: every sub-issue done (and accepted when needed) → done"},
		{ID: "T1_start", Actor: "engine", From: []string{StTodo}, To: []string{StInProgress}, Label: "todo (never backlog), deps done, stage open, disk ok, owner free → owner"},
		{ID: "A1_in_progress", Actor: "agent", From: []string{StTodo}, To: []string{StInProgress}, Label: "owner starts"},
		{ID: "T2_review", Actor: "engine", From: []string{StTodo, StInProgress}, To: []string{StInReview}, Label: "phase=review_requested, branch pushed, no active run → reviewer"},
		{ID: "T3_rework", Actor: "engine", From: []string{StInReview, StTodo, StInProgress}, To: []string{StTodo}, Label: "changes | checks red | conflict | acceptance rejected → owner"},
		{ID: "T3c_carry_verdict", Actor: "engine", From: []string{StInReview}, To: []string{StInReview}, Label: "approved patch unchanged by rebase → keep verdict"},
		{ID: "T3b_rereview", Actor: "engine", From: []string{StInReview}, To: []string{StInReview}, Label: "no/stale verdict → reviewer again"},
		{ID: "T4_done", Actor: "engine", From: []string{StInReview}, To: []string{StDone}, Label: "approve@head, checks green, mergeable, accepted → merge"},
		{ID: "T5_rerun", Actor: "engine", From: []string{StTodo, StInProgress}, To: []string{StTodo, StInProgress}, Label: "run ended without hand-over → rerun (attempt++)"},
		{ID: "T5b_cancel_stuck", Actor: "engine", From: []string{StTodo, StInProgress, StInReview, StBlocked}, To: []string{StTodo, StInProgress, StInReview, StBlocked}, Label: "run past stuck_min / idle_min, or queued past queued_min → cancel"},
		{ID: "T5q_provider_limit", Actor: "engine", From: []string{StTodo, StInProgress, StInReview}, To: []string{StTodo, StInProgress, StInReview}, Label: "provider quota/rate limit → rerun after backoff, no attempt counted"},
		{ID: "T6_block", Actor: "engine", From: all, To: []string{StBlocked}, Label: "attempts exhausted | phase=blocked | dead-letter → lead"},
		{ID: "T7_unblock", Actor: "engine", From: []string{StBlocked}, To: []string{StTodo}, Label: "phase=unblocked (lead within limits.unblocks, or a member) → owner, counters reset"},
		{ID: "T7b_unblock_escalate", Actor: "engine", From: []string{StBlocked}, To: []string{StBlocked}, Label: "lead unblock past limits.unblocks → human"},
		{ID: "T8a_cancel_proposed", Actor: "engine", From: []string{StBlocked}, To: []string{StBlocked}, Label: "lead phase=cancel_proposed → human confirms"},
		{ID: "T8_cancel", Actor: "engine", From: []string{StBlocked}, To: []string{StCancelled}, Label: "cancel confirmed by a member (flow cancel)"},
		{ID: "T9_reconcile", Actor: "engine", From: all, To: []string{StTodo, StInReview, StDone, StCancelled, StBlocked}, Label: "status inconsistent with engine phase (crash / out-of-band) → converge"},
		{ID: "T11_resolve_conflict", Actor: "engine", From: []string{StTodo, StInProgress, StInReview}, To: []string{StInReview}, Label: "branch conflicts with main → reviewer merges main in (resolve-conflict)"},
		{ID: "T11a_conflict_resolved", Actor: "engine", From: []string{StInReview}, To: []string{StInReview}, Label: "conflict merged & clean → approval carried (same patch) or re-review"},
		{ID: "T11b_conflict_handback", Actor: "engine", From: []string{StInReview}, To: []string{StTodo}, Label: "resolver could not merge → owner reworks with conflict context"},
		{ID: "T10_await_acceptance", Actor: "engine", From: []string{StInReview}, To: []string{StInReview}, Label: "gated issue approved & green → human acceptance"},
	}
}

func intOr(p *int, v int) int {
	if p == nil {
		return v
	}
	return *p
}

// Idle is the idle_min limit in minutes (<= 0 = disabled).
func (l Limits) Idle() int { return intOr(l.IdleMin, 8) }

// IdleFor is the idle limit for a run of the given backend (<= 0 = disabled).
func (l Limits) IdleFor(backend string) int {
	if v, ok := l.IdleMinByBackend[strings.ToLower(strings.TrimSpace(backend))]; ok && backend != "" {
		return v
	}
	return l.Idle()
}

// FreeGB is the min_free_gb disk guard (<= 0 = disabled).
func (l Limits) FreeGB() int { return intOr(l.MinFreeGB, 20) }

// Queued is the queued_min limit in minutes (<= 0 = disabled).
func (l Limits) Queued() int { return intOr(l.QueuedMin, 15) }

// MaxUnblocks is how many lead unblocks may reset the counters.
func (l Limits) MaxUnblocks() int { return max(0, intOr(l.Unblocks, 2)) }

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
	// only a missing value gets the default; 0 and negatives are kept.
	dp := func(p **int, v int) {
		if *p == nil {
			*p = &v
		}
	}
	dp(&s.Limits.IdleMin, 8)
	dp(&s.Limits.MinFreeGB, 20)
	dp(&s.Limits.QueuedMin, 15)
	dp(&s.Limits.Unblocks, 2)
	// gbot emits no task messages until its run ends; idle detection would
	// cancel every gbot run longer than idle_min.
	if s.Limits.IdleMinByBackend == nil {
		s.Limits.IdleMinByBackend = map[string]int{}
	}
	norm := map[string]int{}
	for k, v := range s.Limits.IdleMinByBackend {
		norm[strings.ToLower(strings.TrimSpace(k))] = v
	}
	if _, ok := norm["gbot"]; !ok {
		norm["gbot"] = -1
	}
	s.Limits.IdleMinByBackend = norm
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
	s.normalizeExperts()
	for _, r := range s.Roles.Routes {
		for _, o := range r.Owners {
			if !contains(s.Roles.Owners, o) {
				s.Roles.Owners = append(s.Roles.Owners, o)
			}
		}
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
	for _, r := range s.Roles.Routes {
		if len(r.Labels) == 0 || len(r.Owners) == 0 {
			return fmt.Errorf("roles.routes entries need labels and owners")
		}
	}
	for k, v := range s.Limits.Slots {
		if k == "" || v <= 0 {
			return fmt.Errorf("limits.slots.%s must be a positive count", k)
		}
	}
	for _, o := range s.Roles.Owners {
		if o == s.Roles.Reviewer {
			return fmt.Errorf("owner %q cannot also be the reviewer (independent verifier)", o)
		}
	}
	if err := s.validateExperts(); err != nil {
		return err
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

// normalizeExperts defaults mode and remembers the owner pool before route
// owners are merged into it. Expert agents are not added to that pool.
func (s *Spec) normalizeExperts() {
	if s.authoredOwners == nil {
		s.authoredOwners = append([]string{}, s.Roles.Owners...)
	}
	for i := range s.Experts {
		e := &s.Experts[i]
		e.Name = strings.TrimSpace(e.Name)
		e.Mode = strings.ToLower(strings.TrimSpace(e.Mode))
		if e.Mode == "" {
			e.Mode = "implement"
		}
		var agents []string
		for _, a := range e.Agents {
			a = strings.TrimSpace(a)
			if a != "" && !contains(agents, a) {
				agents = append(agents, a)
			}
		}
		e.Agents = agents
	}
}

func (s *Spec) validateExperts() error {
	authored := s.authoredOwners
	if authored == nil {
		authored = s.Roles.Owners
	}
	seen := map[string]bool{}
	expertAgent := map[string]bool{}
	for _, e := range s.Experts {
		if e.Name == "" || seen[e.Name] {
			return fmt.Errorf("expert name %q empty or duplicated", e.Name)
		}
		seen[e.Name] = true
		if len(e.Agents) == 0 {
			return fmt.Errorf("expert %q needs at least one agent", e.Name)
		}
		if len(e.When.Labels) == 0 && len(e.When.NotLabels) == 0 && len(e.When.Metadata) == 0 {
			return fmt.Errorf("expert %q when needs labels, not_labels, or metadata", e.Name)
		}
		if e.Mode != "implement" && e.Mode != "consult" {
			return fmt.Errorf("expert %q mode must be implement or consult, not %q", e.Name, e.Mode)
		}
		if e.Limits.Concurrent < 0 {
			return fmt.Errorf("expert %q limits.concurrent must be >= 0", e.Name)
		}
		for _, a := range e.Agents {
			if a == s.Roles.Reviewer {
				return fmt.Errorf("expert %q agent %q cannot also be the reviewer (independent verifier)", e.Name, a)
			}
			expertAgent[a] = true
		}
	}
	for _, r := range s.Roles.Routes {
		for _, o := range r.Owners {
			if expertAgent[o] && !contains(authored, o) {
				return fmt.Errorf("route owner %q is an expert agent and not a default owner; experts stay out of roles.routes (they would be merged into the default pool)", o)
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
				if f == to && t.ID != "T1_start" && t.ID != "T3b_rereview" && t.ID != "T10_await_acceptance" && t.ID != "T11_resolve_conflict" && t.ID != "T11a_conflict_resolved" && t.ID != "T7b_unblock_escalate" && t.ID != "T8a_cancel_proposed" {
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

// Auto-start modes. Backlog = planned, waiting on its dependencies: when every
// 依赖: dependency is done, the stage barrier is clear and it is not an epic,
// "on" starts it straight into in_progress (T0_dispatch) as owners free up;
// "dry_run" only records the would-start verdict as the issue's wait reason.
// todo stays the manual "start now" lane; backlog never moves to todo.
const (
	AutoStartOff    = "off"
	AutoStartDryRun = "dry_run"
	AutoStartOn     = "on"
)

func (sp *Spec) autoStartMode() string {
	if sp == nil {
		return AutoStartOff
	}
	switch sp.AutoStart {
	case AutoStartDryRun, AutoStartOn:
		return sp.AutoStart
	}
	return AutoStartOff
}

// conflictLimit: resolve-conflict attempts per episode (default 2; <0 = off).
func (l Limits) conflictLimit() int {
	if l.Conflict == 0 {
		return 2
	}
	return l.Conflict
}
