# holt 接入编排器（提案，未应用；本地 checkout ~/Workspace/boom/holt）

| 文件 | 内容 |
|---|---|
| `multica-orchestrator.patch` | 改 holt 的 `.multica/`：Pocket / Studio Parts 两个项目加 `orchestrator:` 段（保留已绑定的 `github_repo` 资源）；删掉 `squads:`（squads 概念已移除）；lead 只做规划和 blocked 处理；gbot 改为专家，人设改为 `gbot_instructions`（创建时写入 bot，sync 用 `gbot bots update --instructions-file` 管理）；**删除 workflow.md**；删除 `avatars/*.manifest.json` 和 `avatars/prompts/`（已移到 `~/.cursor/artifacts/holt/avatars/`）；agent 加 `avatar:`；AGENTS.md / studio/AGENTS.md 去掉 squad 和 workflow.md 引用；新 skill `pocket-checks`；新 `REVIEW.md` |
| `project.yaml.proposed` | patch 应用后的完整 `.multica/project.yaml`，便于直接阅读 |
| `envrc-sccache.patch` | `.envrc`：有 sccache 时设置 `RUSTC_WRAPPER`、共享 `SCCACHE_DIR=$HOME/.cache/sccache-holt`、40G 上限 |

基线：holt main `6c2b188d`（提案分支 `herdr/multica-proposal` @ `35463802`）。两个 patch 都 `git apply --check` 通过（2026-10-21）；两段 `orchestrator:` 用 `multica team preset render -f` 校验通过。

## 决定（Ray，2026-10-21）
- GitHub CI 保持关闭：`required_checks: [review-verdict]`，靠本地 pocket-checks / verify-gates。
- cargo 槽位：Pocket 2，Studio 1（合计最多 3）。
- 不加显式截图技能：frontend-engineer 不挂 create/maintain-verification-skill、principle-experience-first。
- 命名统一为 holt；品牌项（pocketjs.dev、pocket CLI、pocket-* crate、POCKETJS_* 环境变量）不变。
- squads 移除：project.yaml 不再有 `squads:`，派活全由编排器按 area 标签路由。
- gbot 不是 owner，是外部专家：`area:hard` / `area:cross-cutting` / `area:research` 路由给它。它可以自己改代码，或开 herdr pane 输入 `grok-pi-worker` 拆分（每个 pane 独立 worktree、分支 `herdr/<task>`），自己验收每一块，整合成一个分支 / PR 交回编排器，并关掉自己开的 pane。

## 角色与技能
| agent | 角色 | pstack 技能 |
|---|---|---|
| yielded-lead | lead | in-your-own-words, figure-it-out, attack-the-premise, sequence-verifiable-units, outcome-oriented-execution, sharpen, guard-the-context-window |
| heavy-engineer / frontend-engineer / control-plane-engineer | owner | tdd, prove-it-works, fix-root-causes, subtract-before-you-add, test-behavior-not-implementation, blast-radius, boundary-discipline, typescript-best-practices（heavy 另保留 rust-best-practices） |
| yielded-reviewer | reviewer | interrogate, blast-radius, prove-it-works, test-behavior-not-implementation, no-comments（+ handoff-metadata） |
| gbot | 专家 | 研究：in-your-own-words, figure-it-out, attack-the-premise, sequence-verifiable-units, guard-the-context-window；验证/整合：prove-it-works, blast-radius, swarm（另有 pocket-checks、open-pr、handoff-metadata、rebase-conflict 等仓库技能） |

## 映射
| area 标签 | 池（`roles.routes`） | 检查 | 资源 |
|---|---|---|---|
| area:hard, area:cross-cutting, area:research | gbot（专家） | 涉及区域全部 | 视改动 |
| area:host / compiler / framework / cli / apps | heavy-engineer | `pocket check`、`bun run test --project <area>`、`bun check` | host 带 `needs:cargo` |
| area:studio-app | frontend-engineer | `pocket dev studio` 验证点名界面 | 默认验收门 |
| area:control-plane | control-plane-engineer | 包内测试 | — |
| 无匹配标签 | `roles.owners`（Pocket: heavy-engineer；Studio: frontend, control-plane） | | |

## 引擎差距（未改代码，需 Ray 定）
- 引擎没有"专家"角色。`Spec.Normalize` 把 route owner 并入 `roles.owners`，所以 gbot 实际仍按 owner 渲染（`preset render` 输出 `gbot (owner)`，挂 owner 技能和 owner.md），而且未打标签的 issue 也可能派给它。
- 要真正做成专家，需要：`roles.experts`（route 可引用，但不并入默认 owner 池）、preset 里的 `expert.md` 角色文本、pstack manifest 的 `expert` 角色（上面的技能表），以及 `TestRoleContextBudget` / 有效角色列表加上 expert。
- 在那之前，lead 应给每个 issue 打 area 标签，避免落到默认池。

## 引擎支持（已实现，feat/orchestrator）
| 键 | 行为 | 测试 |
|---|---|---|
| `roles.routes` | 第一个标签匹配的 route 决定 owner 池（大小写不敏感）；池全忙就等待，不回落到别的池；T7 unblock 也用该池 | `TestRoutesPickOwnerByLabel` |
| `limits.slots.<name>` | 带 `needs:<name>` 的 issue 在 slot 满时等待；不带标签的不受影响 | `TestCargoSlots` |
| `acceptance.default_for_labels` | 带这些标签也走 T10 验收门 | `TestDefaultAcceptanceLabels` |
| 校验 | route 必须有 labels 和 owners；slot 必须为正；route owner 不能是 reviewer | `TestValidateSettings` |

## 待 Ray 定
- CI 恢复后可在 `required_checks` 里加回真实 check 名。
- 两个项目共用一个 hub 和仓库，worktree 都在 `~/.cursor/worktrees/holt/agent-<key>`。
- 分支命名：`agent/<key>`（worktree `agent-<key>`）只给编排器用；herdr worker 用 `herdr/<task>`，不要用 `agent/<task>`，否则会占住编排分支，issue 被 `[branch_occupied]` 阻塞。

## 头像映射（待 Ray 确认）
| agent | avatar |
|---|---|
| yielded-lead | team-lead.png |
| yielded-researcher | researcher.png |
| yielded-engineer | codex-engineer.png |
| heavy-engineer | claude-engineer.png |
| yielded-reviewer | reviewer.png |
| code-auditor | code-auditor.png |
| frontend-engineer | frontend-engineer.png |

gbot、control-plane-engineer 没有头像。

## 已知问题
- 提案里 lead 的 `plan-split` 等 pstack 技能不在 sync 的技能目录里，`sync.ts` 会报 `references unknown skill: plan-split`。需要先把这些技能加进 global.yaml / workspaces 目录，或者从 agent `skills:` 里拿掉（引擎的 pstack 另行注入）。
