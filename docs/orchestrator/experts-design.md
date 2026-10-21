# Experts — design (not implemented)

Status: proposal, 2026-10-21. Base: feat/orchestrator `e62e4de3e`.

## 1. Today: are routing labels hardcoded?

No. Label routing is generic string matching against whatever the spec declares; nothing in the engine knows `area:*`.

| What | Where | Behaviour |
|---|---|---|
| Route schema | `server/internal/orchestrator/spec.go:69-78` | `roles.routes: [{labels: [...], owners: [...]}]`, free-form strings |
| Matching | `engine.go:119-128` `hasLabel` | any-of, `strings.EqualFold` (case-insensitive), exact otherwise (no prefix/glob) |
| Pool choice | `engine.go:130-139` `OwnerPool` | first route whose labels match, else `roles.owners` |
| T1 start | `engine.go:691-697` | first non-busy agent in that pool; pool full = wait, no fallback |
| T7 unblock | `engine.go:606-608` | previous `owner` meta, else `OwnerPool(i)[0]` |
| Route owners merged into default pool | `spec.go:265-271` (Normalize) | **why gbot becomes a default owner today** |
| Validation | `spec.go:284-300` | lead/reviewer/≥1 owner required; routes need labels+owners; owner ≠ reviewer |
| Acceptance gate | `engine.go:116`, `guard.go:196`, `spec.go:138-141` | `acceptance.labels` + `default_for_labels`, any strings; only the defaults at `spec.go:262-263` are built in |
| Slots | `engine.go:141-160` | the one *convention*: label prefix `needs:<slot>` must match a key in `limits.slots` |
| Rendering | `preset.go:30-80` | roles are fixed: lead, owner (each of `roles.owners`), reviewer; files `<role>.md` |
| Agent check on apply | `api.go:123` | lead, reviewer, owners must exist in the workspace |

The only `area:` text in the engine is a comment (`spec.go:140`). The preset lead text (`preset/impl-review-merge/lead.md:5-15`) does not mention routing labels at all; the `area:*` vocabulary lives only in the holt `project.yaml` (yielded-lead instructions). So any label works as long as the lead applies it and a route lists it.

## 2. Goals

- Experts declared only in `project.yaml` (`orchestrator.experts`), any number, no code change to add one.
- An expert is never in the default owner pool and never picked for an unlabelled issue.
- Each expert has its own trigger, skills and instructions; the role text renders as `expert`, not `owner`.
- Experts reuse the impl → review → merge flow (the reviewer still verifies), so no new state machine.
- The lead learns the trigger labels from the spec, not from hand-written instructions.

## 3. Schema

```yaml
orchestrator:
  roles:
    lead: yielded-lead
    reviewer: yielded-reviewer
    owners: [heavy-engineer]            # default pool; experts never added here
    routes:                             # unchanged; owners only (validation rejects experts here)
      - { labels: [area:host, area:compiler], owners: [heavy-engineer] }
  experts:
    - name: deep-work                   # id used in metadata (`expert=deep-work`) and logs
      agents: [gbot]                    # pool, first non-busy wins (same as owners)
      when:
        labels: [area:hard, area:cross-cutting]   # any-of, case-insensitive (hasLabel)
        # optional, all AND-ed with labels:
        # not_labels: [needs-GUI-Master]
        # metadata: { expert: deep-work }          # explicit opt-in by the lead or a person
      mode: implement                   # implement | consult
      skills: [figure-it-out, principle-attack-the-premise, principle-prove-it-works, blast-radius, swarm]
      instructions: |
        Do it yourself or fan out with herdr panes (grok-pi-worker, own worktree,
        branch herdr/<task>); accept every piece, integrate into this issue's
        branch, close your panes, hand back.
      limits: { concurrent: 2 }         # optional cap across this project's issues
    - name: research
      agents: [gbot]
      when: { labels: [area:research] }
      mode: consult
      skills: [in-your-own-words, figure-it-out, principle-explain-the-number]
```

Rules:
- `when` matches like routes. Experts are checked **before** routes; the first matching expert wins. No match → routes → `roles.owners`.
- An agent may appear in several experts and also be an owner elsewhere, but not the reviewer (independence check extended).
- `skills` are pstack/preset/workspace skill names, appended to the `expert` role baseline (section 5). `instructions` are appended after the role text.
- `mode`:
  - `implement` — expert takes the owner slot for that issue: branch `agent/<key>`, handoff, reviewer verdict, merge. Same transitions as an owner.
  - `consult` — expert writes one finding comment and no branch; on completion the engine returns the issue to `todo` with `expert_done=<name>` and routes it normally (routes/owners), so the owner starts with the finding in context. Lets research run before implementation without a separate issue.

## 4. Engine changes (estimate ~250 lines + tests)

1. `spec.go`
   - Add `Experts []Expert` (`name, agents, when{labels,not_labels,metadata}, mode, skills, instructions, limits.concurrent`).
   - Normalize: keep merging **route** owners into `roles.owners` (existing behaviour), never expert agents. Default `mode: implement`.
   - Validate: unique names; `agents` non-empty; `when` has at least one condition; mode ∈ {implement, consult}; no expert agent equals the reviewer; route owners must not be expert-only agents (catches the old gbot-in-routes pattern with a clear error).
2. `engine.go`
   - `func (sp *Spec) MatchExpert(i *Issue) *Expert` — first expert whose `when` matches (labels via `hasLabel`, `not_labels`, metadata equality). Skip an expert already recorded in `expert_done`.
   - `OwnerPool(i)` → `Assign(i) (pool []string, expert string)`: expert pool if matched, else today's logic. Callers: T1 (`engine.go:691`), T7 (`engine.go:606-608`).
   - T1 writes meta `expert=<name>` alongside `owner`. Everything that reads `m["owner"]` (rework T3 `engine.go:195`, reconcile T9 `engine.go:254-263`) keeps working unchanged.
   - `isOwner` (`engine.go:166`; callers `engine.go:315`, `engine.go:670`, `api.go:355`) → also true for expert agents of this spec, so a held expert assignee is adopted like an owner and `flow` owner overrides accept them.
   - Consult completion: on the expert's handoff with `mode: consult`, transition `T_expert_consulted`: status `todo`, clear `owner`, set `expert_done`, re-run assignment.
   - Optional `limits.concurrent` per expert: count siblings with `expert=<name>` in todo/in_progress, same shape as `slotWait` (`engine.go:141-160`).
3. `runtime.go:333` `busyOwners` — include expert agents in the busy set.
4. `api.go:123` — validate expert agents exist in the workspace.
5. `preset.go` `RenderPreset` — emit one `expert` role per (expert, agent) with skills = preset `expert` baseline + pstack `roles.expert` + `expert.skills`, instructions = `expert.md` + team + `expert.instructions`. `PresetRole` gets `Expert string`.
6. Flow UI / `multica flow log`: show `expert` meta next to owner (view.go, read-only).
7. Tests: spec validation table; `MatchExpert` precedence (expert > route > default); unlabelled issue never goes to an expert; consult round-trip; busy expert waits (no fallback to owners); render output; scenarios under `testdata/scenarios/EXPERTS-*.yaml`.

## 5. pstack `expert` role

- New `preset/impl-review-merge/expert.md` (owner.md minus "you are one of the owners", plus: you are routed here by label; scope to the issue; in consult mode change no files and post one sourced finding; in implement mode the same handoff contract as owners).
- `pstack/manifest.yaml`:
  ```yaml
  roles:
    expert:
      budget_bytes: 40000
      max_skills: 8
      skills: [in-your-own-words, figure-it-out, principle-attack-the-premise,
               principle-sequence-verifiable-units, principle-guard-the-context-window,
               principle-prove-it-works, blast-radius, swarm]
  ```
- `presetSkills["expert"]` = owner's preset skills (open-pr, handoff-metadata, rebase-conflict, blocked-report) so implement mode can hand off.
- `pstack_test.go`: add `expert` to the valid-role set and to `TestRoleContextBudget`. Per-expert `skills` from project.yaml are counted against the budget at render time (warn, not fail).

## 6. Lead instructions

- `lead.md` step A2 gains: "Label each sub-issue with the label that routes it (see *Routing* below). Unlabelled issues go to the default owners."
- `RenderPreset` appends a generated **Routing** section to the lead role from the spec, so the vocabulary never drifts from project.yaml:
  ```
  Routing (first match wins):
  - experts: deep-work ← area:hard, area:cross-cutting (gbot, implement); research ← area:research (gbot, consult)
  - routes: area:host, area:compiler → heavy-engineer
  - otherwise: heavy-engineer
  Slots: needs:cargo (2). Acceptance: needs-acceptance, needs-GUI-Master, needs-Ray-signoff; default for area:apps.
  ```
- Project-specific label meaning (what counts as `area:hard`) stays in the lead agent's own instructions in project.yaml.

## 7. holt project.yaml after this lands

- Remove gbot from `routes`; add `experts: [deep-work, research]` as above in Pocket and Studio Parts.
- `owners: [heavy-engineer]` (Pocket), `[frontend-engineer, control-plane-engineer]` (Studio).
- gbot agent `skills`/`instructions` in project.yaml can shrink to repo skills; the expert block carries the role-specific ones.

## 8. Compatibility

- Specs without `experts` behave exactly as today.
- Existing in-flight issues keep their `owner` meta; no migration.
- Out of scope: per-expert review policy (always the spec reviewer), expert-to-expert handoff chains, a separate merge path.
