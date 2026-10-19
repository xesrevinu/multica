# feat/orchestrator — PROGRESS

In-app orchestrator (design doc B, teamctl/docs/B-orchestrator-in-multica.md in the e2e worktree).
Reference implementation: /Users/kee/Workspace/ai-stack/multica-e2e/teamctl (kept as-is).

- [x] M0 engine: server/internal/orchestrator/{spec,engine}.go + tests (port of teamctl tick; adds acceptance gate T10, FLOW.md invariants from backup/20261019/workflow-transitions-wip)
- [x] M1 migration 583_orchestrator (team, transition log w/ idem_key, event queue, issue_state for backoff/reminders)
- [x] M1 store/actuator/runtime: bus SubscribeAll → durable event queue → debounced worker; 60s reconciler; backoff + dead-letter; kill switch (team pause, label orchestrator-pause, MULTICA_ORCHESTRATOR_DISABLED=1); in-process API calls as service member "Orchestrator" (orchestrator@system.multica.local); native inbox type orchestrator_alert
- [x] M1 guard: UpdateIssue/BatchUpdate/SetIssueMetadataKey refuse with code orchestrator_owned; PR auto-complete disabled for orchestrated projects (pr_merge_status=none semantics)
- [x] M1 API /api/orchestrator (teams apply/diff/pause/resume/detach, flow show/log/unblock/cancel/accept/reject/replay)

## Scenarios run on the in-app engine, teamctl serve stopped (2026-10-19)
- S1 ATT-37 pass; PR-S1 ATT-38 pass (PR #9); S2 ATT-39 pass; PR-S2 ATT-41 pass (changes→rework→merged, PR #10)
- GA ATT-40 pass: T10→reject(reason)→T3 rework→T5b stuck cancel→re-review→T10→`flow accept`→done
- OOB ATT-43 pass: T9 reverted in ~430ms; F1 ATT-45 pass; F2 ATT-46 pass (dead-letter); S5 ATT-47 pass (disk wait, then done)
- S6+S3 ATT-48..51 pass: stage-2 child started only after both stage-1 children were done; ATT-50 conflict precheck → T3 rework → merged
- S4 pass (on its own): ATT-40 run >20m → T5b_cancel_stuck → T5 rerun
- Incident: the ATT-40 owner ran `rg` over /Users/kee looking for the rejection reason, which was only in metadata.acceptance. Killed only that rg (a daemon→pi child), paused the team, cancelled the run. Fix: reject also writes blocked_reason; WORKSPACE SCOPE rule added to instructions, skills, the platform reference and the runbook. The rerun was clean.
- Not done yet: PR-S3..S6, M4 UI, patch-id verdict carry-over, leader election, rebuilding the e2e daemon/CLI from orch

## Step: idle detection, M4 UI, e2e daemon rebuild, PR-S3/S4/S6 (2026-10-19 16:00–16:30)
- 54ea8f512 no-progress detection: `limits.idle_min` (default 8, negative disables). A running run with no task_message for that long gets T5b (counts as an infra attempt).
- 0336312ae web: issue Flow card (state rail with current node, phase, attempt counters, failures, why blocked, paused, waiting, next decision, last 10 transitions, Accept button) and project team section (spec summary plus pause/resume). views typecheck clean; vitest projects+issues 1443/1443 pass.
- e2e daemon rebuilt from feat/orchestrator: the old binary is kept as multica-e2e/server/bin/multica.bak-pre-orch, and the daemon was restarted with the same env/profile (pid 57230).
- PR-S3 ATT-52/53: pass. PR #11 merged first; #12 GitHub mergeable=CONFLICTING → T3 rework → re-review → merged (de563cc).
- PR-S6 ATT-54..57: pass. Stage-1 PRs merged in parallel, stage 2 started (16:10:48) only after both were done, then merged.
- PR-S4 ATT-58: pass, on the new daemon. With idle_min=1 the agent's `sleep 150` was cancelled 3 times ("no tool output for 1m"), then T6 block to lead after infra 2/2, orchestrator_alert inbox. idle_min restored to 8, `flow unblock` → the 150s sleep was tolerated → PR merged.
- PR-S5 ATT-59: in progress (intentional first-attempt CI flake → expect exactly one re-run).
- PR-S5 ATT-59: pass. The first CI attempt was red (intentional flake); the engine re-ran it once (run 39223273946 attempt 2 = success; the agents' task_messages have no rerun) → merged PR #17. The re-run is now logged (commit "log CI re-runs"). Note: the "one re-run per head" memory is in-process, so a server restart could allow one more.
- Scenario suite on the in-app engine: S1–S6, PR-S1–S6, GA, OOB, F1, F2 all pass.

## Remaining
- Multi-replica: no leader election yet (single-replica assumption, documented in design.md).
- patch-id verdict carry-over (rebase-only reworks still re-review).
- UI: transitions are listed in the Flow card, not interleaved in the comment timeline; reject from the UI is not implemented (CLI needs --reason); the frontend has no special rendering for the orchestrator_alert inbox type.
- The CI re-run memory should be persisted (orchestrator_issue_state).
- PocketJS .multica changes deferred, as instructed.

## Step: remaining items (2026-10-19 16:34–16:55)
- CI re-run record persisted: migration 584 `orchestrator_ci_retry(head_sha PK)`. The first INSERT wins across restarts; the in-memory map stays only as a fast path.
- Transitions go into the issue timeline (activity_log action=orchestrator_transition, actor = Orchestrator service user, never coalesced). The Flow card has Accept plus Reject with a reason (button disabled while the reason is empty). The orchestrator_alert inbox type has a label in web+mobile and shows the full alert body.
- Web dev now runs from the orch worktree on :13819 (`.orch-dev/web.sh`, pid in web.pid), pointed at the e2e API :18899. The old e2e web dev was stopped.
- Headless screenshots: system Chrome via playwright channel "chrome", headless, no focus stealing (`.orch-dev/shots.mjs`, `reject.mjs`). The playwright chromium download hung, so it was not used. Files in .orch-dev/shots/: flow-blocked(.png,-card), flow-acceptance(.png,-card), flow-after-reject(.png,-card), reject-filled, team(.png,-card), inbox, inbox-alert.
- The Reject UI was verified end-to-end: ATT-60 rejected from the browser → T3_rework with the typed reason; the timeline shows the Orchestrator rows.
- T3c verdict carry-over (patch-id): unit test on a temp repo (rebase same / changed differs). Live: ATT-60 awaiting acceptance, I merged main into agent/ATT-60 (fast-forward push, no force) → T3c_carry_verdict "same patch-id as reviewed 2694c93172b6", no extra review run.
- Known dev-console noise, not from this work: the docs proxy :4747 is refused (500 in the Next overlay "1 Issue"), and the nativeButton prop warning.

## Step: one branch name, PocketJS engine keys, e2e fixes, shadow mode, docs (2026-10-19 17:03–17:35)
- One branch name everywhere: `agent/<lowercase slug of key>` (util.BranchSlug, used by both the daemon's sanitizeName and the engine's branchOf). The preset says push the branch you are on; the PR title carries the uppercase KEY. Linking is case-insensitive over title/body/branch (handler test added). The e2e stack has no GitHub App webhook, so live link rows could not be observed (github_pull_request is empty).
- Engine keys for the PocketJS patch (tests in settings_test.go):
  - roles.routes: label → owner pool; a busy pool waits; T7 uses the pool.
  - limits.slots.<name>: counts needs:<name> siblings in todo/in_progress.
  - acceptance.default_for_labels.
  - min_free_gb: the name was already aligned.
- e2e found real bugs (ATT-64), all fixed:
  - The guard refused the lead's phase=unblocked/cancel_proposed/planned, so the lead wrote review_requested and the issue was stuck in blocked. All three phases are now allowed; a planned epic never starts; blocked shows a wait reason.
  - The reviewer moved its run HEAD, so the daemon preserved a detached worktree and the next `wt switch` failed on the occupied path. The daemon now forks a task branch in that case (branchUnavailable). Reviewer instructions forbid checkout in the run dir. A failed or cancelled reviewer run now counts as infra, not review.
  - Owner rebase + force-push violated the daemon delivery check ("no longer contains the start commit"). The rebase-conflict skill now merges origin/main and forbids force-push.
- Shadow mode: spec `shadow: true`, plus migration 585 adding 'shadow' to the transition state check. Verified on ATT-68: transition logged, the issue was untouched, and the guard was off.
- Item 1 verification (evidence in .orch-dev/runs/PR-S1n-S3n.ATT-65-67.txt):
  - ATT-65 (S1), and ATT-66/67 (S3 conflict: precheck conflict → owner merged main → review → merged) all merged.
  - PR heads were agent/att-65..67; the remote branches were deleted after merge.
  - The hub was on main, clean, with main == origin/main = fd456f8 after the merges.
  - Worktrees follow `~/.cursor/worktrees/autoteam-sandbox/agent-att-6x` = the wt template `{{ repo }}/{{ branch | sanitize }}`. The pre-start hook ran for every one (/tmp/wt-hook-autoteam-sandbox.log).
  - No per-task clone: there is no "git clone" in the daemon or api logs, and only the integ worktree exists in the cache.
- Incident: I restarted the daemon with `env -i`, so it lost the provider keys. Every run failed with "Connection error.", and two issues were blocked on infra attempts. I restarted it with the login env and unblocked both with `flow unblock`. Runbook updated.
- Docs:
  - docs/orchestrator/pocketjs/ (patches + mapping)
  - production-readiness.md
  - roles.md: division of responsibilities
  - README: new keys, branch rule
  - runbook: daemon restart
- Remaining:
  - B1 leader election.
  - B2 GitHub REST instead of the server-side hub/gh.
  - B3 GitHub App.
  - B4 migration renumbering to a reserved range before the first prod deploy.
  - B7 metrics.
  - The ATT-64 leftovers (closed PR #19, remote branch agent/ATT-64, three e2e worktrees) were kept as evidence.

## Step: fast feedback harness + GitHub App on local e2e (2026-10-19 17:41–17:52)
- Replay harness: `testdata/scenarios/*.yaml` has 21 scenarios. 15 of them are tied to recorded e2e sequences in testdata/recorded.txt (exported from the e2e DB). They run in under 0.1s.
- Integration layer: real runtime + real Postgres (`multica_orch_test`, created by scripts/orch-testdb.sh) with a fake API router, fake agents and a fake GitHub. 7 tests, ~1s. A `repoBackend` interface is the seam.
- Bug found by the integration layer: two needs:cargo issues started in one pass, because the siblings snapshot was stale. Fixed: the backlog snapshot now re-reads siblings.
- docs/orchestrator/testing.md covers the layers, when real-agent e2e is still needed, and a 4-scenario smoke set.
- GitHub App:
  - K8s (read-only): the secret holds GITHUB_APP_ID/PRIVATE_KEY/SLUG/WEBHOOK_SECRET. There is one installation (xesrevinu, all repos).
  - Local wiring: github-app.env (600) is loaded by run-api.sh, the installation row is bound to the e2e workspace, and the ghrelay poller replays App deliveries for the sandbox repo, signed.
  - Verified: local github_pull_request snapshots and issue_pull_request links for #18–#23, including the lowercase branches.
- INCIDENT: the App private key was printed in the session output by a `cut` over a multi-line PEM. Recommend rotating it.

## Step: pstack import (2026-10-19)
- All 55 skills, 2 agents and LICENSE are vendored verbatim under preset/impl-review-merge/pstack/.
- manifest.yaml: 34 keep, 15 adapt, 3 operator (bro, teach, poteto-help), 3 drop (setup-pstack, automate-me, make-bot-ui).
- CONTRACT.md is appended to every exported SKILL.md.
- Role sizes: lead 23, owner 33, reviewer 20, acceptance 4. RenderPreset attaches them; export applies the adaptations.
- A test enforces full coverage and a reason for every non-keep entry.
- Mapping table: docs/orchestrator/pstack.md.

## Step: code-review practices (2026-10-19)
- Source: the claude.dev article. Mapping in docs/orchestrator/code-review-practices.md.
- Reviewer flow: find, then verify; severity tags important / nit / pre-existing; count-first summary; diff-only re-review; reads REVIEW.md.
- Owner answers every important finding.
- Engine: the reviewer sets metadata risk=high, which forces human acceptance (T10). The guard validates risk values. The round-cap block reason now tells the lead to reconsider the approach.
- New scenarios: REVIEW-risk-high-gates and REVIEW-risk-high-acceptance.

## Step: PocketJS patch refresh (2026-10-19)
- Rebuilt multica-orchestrator.patch against the real pocketjs .multica/project.yaml at 0344e315.
- Per-role pstack and preset skills added to the agents. yielded-reviewer now follows the review practices.
- New .multica/REVIEW.md: failure modes and risk tiers. workflow.md gains review and pstack sections.
- `git apply --check` is OK for both patches. Both orchestrator specs validate with `preset render -f`. Nothing was applied to pocketjs.

## Step: Web UI redesign (2026-10-19, phases 1-5 done)
Plan: docs/orchestrator/ui-redesign.md. Prototype: .orch-dev/proto/index.html. Resume from the first unchecked phase.
- [x] Design doc committed.
- [x] Phase 1 backend:
  - Engine: member `paused` (wait only) and `cancel_requested` (T8b_member_cancel, stops the live run); both engine-owned keys. Scenarios MEMBER-pause-resume and MEMBER-cancel-running.
  - API: `GET /api/orchestrator/issues` (attention buckets needs_you > running > queued > settled; nullable project/assignee/PR/worktree), `GET /issues/{key}/timeline` (runs, transitions, flow state), `POST /flow/{key}/pause|resume`, `cancel` now works from any open state.
  - GitHub App: ghsnapshot REST helper + token permissions; `GET /issues/{key}/pr` (live commits, checks, behind_by, `can` from permissions; `mode:local` hides it), `POST /issues/{key}/pr/update-branch|rerun-checks|merge` (merge refused for orchestrated issues: engine_owned).
  - wt: repo.disk_path optional; default = static root of wt `worktree-path` (repo .config/wt.toml, then user config).
  - Tests: TestAttentionAndSort, TestWtRootAndDiskPath, TestPRDetailAndOps (httptest fake GitHub), TestIntegrationIssueViewsAndMemberActions. Verified live on e2e (18899) incl. PR #22 via the App.
  - Priority/risk use existing APIs (PUT /api/issues/{id}, PUT /api/issues/{id}/metadata/risk).
- [x] Phase 2 sidebar + DMs: agent DMs on top (latest session, unread, busy dot), pinned kept, attention-grouped issue list (needs you / in progress / up next / settled collapsed; sub-issues expand; filter + project), secondary surfaces as a footer icon strip. New `orch` i18n namespace (5 locales). core `@multica/core/orchestrator` (schemas, queries, mutations, grouping, realtime).
- [x] Phase 3 issue chat view (`/issues/:id`; `?view=classic` or a `#comment-` deep link keeps the document page): oldest→newest timeline with thread replies, agent runs folded ("worked for"), orchestrator transitions as dividers, activities folded; acceptance / blocked / cancel-proposed card pinned above the composer; live PR card (checks, commits, behind/conflict, update-branch/rerun/merge gated by App permissions; engine merges orchestrated issues); status strip (phase, attempts, waiting, risk, worktree, priority, pause/resume/cancel); title opens the details drawer (read-only, sheet on desktop, bottom sheet on mobile).
- [x] Phase 4 tabs + landing: issues/sub-issues open as session tabs (sidebar hover button, drawer); fixed web never mounting SessionTabsProvider (session tabs were off on web); workspace root / post-auth / switcher land on /chat; board/list/table etc. stay in the footer strip.
- [x] Phase 5 compact: chat list doubles as phone home with the attention list; issue view back link; drawer as bottom sheet; manifest unchanged (start_url /inbox, since "chat" is not a legacy/reserved segment).
Screenshots: .orch-dev/shots/ui-p2-*, ui-p3-*, ui-p4-*, ui-p5-m-* (box /workspace/ui/).
Known: React "nativeButton" unknown-prop warning on every page (pre-existing, not from orch code); drawer is read-only (edit via classic page); sidebar sort is attention-only; core vitest has localStorage failures under Node 26 (env, untouched files).
Helpers (git-excluded): `.orch-dev/api.mjs` (authenticated API calls; needs DEV_CODE from api.env's MULTICA_DEV_VERIFICATION_CODE, never print), `.orch-dev/shots.mjs`.

## Step: UI round 2 (2026-10-19, in progress)
Ray: "functionally OK but styling only ~50% there". Same constraints. Screenshots → box /workspace/ui2/.
- [ ] Step 0 merge chore/slim — NOT merged yet: chore/slim stops at DingTalk removal (providers, Composio, Dockerfile.web/stale refs still open per its SLIM-PROGRESS.md). Re-check before the end; tag backup/20261019/pre-slim-merge then merge.
- [x] nativeButton warning fixed (ui tooltip/hover-card wrappers dropped the prop base-ui 1.3 triggers lack).
- [x] Backend: PR list per issue, PR files, reviews + review comments + conflict files + labels in PR detail, sync-labels / delete-branch ops (App-permission gated), worktree list/remove (server-side git on team hubs), `settled_total`.
- [x] PWA start_url /chat ("chat" reserved slug + legacy segment).
- [x] Sidebar: t3 rows (project badge, Working spinner, PR/diff chips, branch), view options (sort/assignee/priority/project/orchestrated/sub-issues, localStorage), hover pin/unpin, "Settled (N)" footer + load more.
- [x] Chat: plain agent messages, member bubbles, "Worked for …" fold, hairline dividers, rounded composer with agent/priority/risk chips, context bar (mode · diff · branch · phase · actions).
- [x] Codex-style PR card: commit lanes, branch chips, sha·author·time, conflicts, checks/reviews summary, pending "Merging main into the branch… Resolving" row, linked-PR switcher, sync labels / delete branch.
- [ ] Drawer in-place editing; worktree panel; files tab; review comments in timeline; cmd-k / shortcuts / unread; e2e run.
