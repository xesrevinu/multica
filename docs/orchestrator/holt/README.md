# holt 接入编排器（提案，未应用；本地 checkout ~/Workspace/boom/holt）

| 文件 | 内容 |
|---|---|
| `multica-orchestrator.patch` | 基于 holt main `245fa28a`（Ray 已把上一版提案合入 main）的增量：gbot 从 `roles.routes` 移到 `orchestrator.experts`（Pocket 和 Studio Parts 各两个专家：`deep-work` implement ← area:hard / area:cross-cutting，`research` consult ← area:research），需要 feat/orchestrator `4a46d48b0` 及以后的引擎 |
| `project.yaml.proposed` | patch 应用后的完整 `.multica/project.yaml`，便于直接阅读 |
| `envrc-sccache.patch` | `.envrc`：有 sccache 时设置 `RUSTC_WRAPPER`、共享 `SCCACHE_DIR=$HOME/.cache/sccache-holt`、40G 上限 |

基线：holt main `245fa28a`。patch 和 envrc patch 都 `git apply --check` 通过（2026-10-21）；两段 `orchestrator:` 用 feat/orchestrator `4a46d48b0` 的 `multica team preset render -f` 校验通过（无预算警告，lead 带生成的 Routing 段）。

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
| area:hard, area:cross-cutting | 专家 `deep-work` = gbot（implement，`limits.concurrent: 2`，先于 routes 匹配，不进默认 owner 池） | 涉及区域全部 | 视改动 |
| area:research | 专家 `research` = gbot（consult：只发一条带来源的结论，然后由 routes/owners 接手实现） | — | — |
| area:host / compiler / framework / cli / apps | heavy-engineer | `pocket check`、`bun run test --project <area>`、`bun check` | host 带 `needs:cargo` |
| area:studio-app | frontend-engineer | `pocket dev studio` 验证点名界面 | 默认验收门 |
| area:control-plane | control-plane-engineer | 包内测试 | — |
| 无匹配标签 | `roles.owners`（Pocket: heavy-engineer；Studio: frontend, control-plane） | | |

## 引擎
- 专家机制已实现（feat/orchestrator `4a46d48b0`，设计见 `../experts-design.md`）。research 专家只用角色基线技能：再加 principle-explain-the-number 会超出 45000 字节预算。

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
