# PocketJS 接入编排器（提案，未应用到 pocketjs 仓库）

| 文件 | 内容 |
|---|---|
| `multica-orchestrator.patch` | 改 pocketjs 的 `.multica/`：project.yaml 两个项目（Pocket / Studio Parts）加 `orchestrator:` 段并去掉 `github_repo` 资源；lead 指令改成只做规划和 blocked 处理；workflow.md 重写为引擎流程 + 区域/owner/检查表；新 skill `pocket-checks` |
| （同一 patch）`.multica/REVIEW.md` | PocketJS 的评审规则：各区域"失败长什么样"、`risk` 分级（high 进 Ray 验收门）、finding 格式与跳过路径 |
| `envrc-sccache.patch` | `.envrc`：有 sccache 时设置 `RUSTC_WRAPPER`、共享 `SCCACHE_DIR`、40G 上限 |

基线：pocketjs `0344e315` 的真实 `.multica/project.yaml`（271 行），两个 patch 均 `git apply --check` 通过（2026-10-19），两段 `orchestrator:` 已单独用 `multica team preset render -f` 校验通过。

## 本轮更新（pstack + 评审实践）
- 各 agent 的 `skills:` 按角色加上 preset 技能和 pstack 技能：lead 10 个（plan-split、blocked-report、figure-it-out、sharpen、recall、reflect…），owner（gbot / heavy / frontend / control-plane）加 open-pr、handoff-metadata、rebase-conflict（内容是 merge）、tdd、prove-it-works、blast-radius、unslop 等，frontend 另加 create/maintain-verification-skill，reviewer 加 verify-gates、review-verdict、interrogate、swarm 等。researcher / yielded-engineer / code-auditor 不在编排器角色里，没改。
- yielded-reviewer 指令加一段：读 REVIEW.md、先找再核实、important/nit/pre-existing、数量开头、复审只看增量 diff、写 `risk`。
- workflow.md 加"评审规则"和"pstack 技能"两节（workspace 范围、metadata 交接、不 rebase/force-push、三轮不收敛交给 lead）。
- 删掉了已实现功能上过时的"proposed"注释。
- 前提：pstack 技能要先进 workspace（`multica team preset export <dir>` 后 `multica skill create`），否则 project.yaml 里引用的名字不存在。

应用：在 pocketjs 根目录 `git apply docs/.../multica-orchestrator.patch`（已 `git apply --check` 通过）。两段 `orchestrator:` 已用 `multica team preset render -f` 解析和校验通过（route owner 自动并入 owners、reviewer 不在 owner 池）。

## 映射

| area 标签 | owner 池（`roles.routes`） | 检查 | 资源 |
|---|---|---|---|
| area:hard, area:cross-cutting | gbot | 涉及区域全部 | 视改动 |
| area:host / compiler / framework / cli / apps | heavy-engineer, gbot | `pocket check`、`bun run test --project <area>`、`bun check` | host 带 `needs:cargo` |
| area:studio-app | frontend-engineer | `pocket dev studio` + 截图到 `.build/validation/` | 默认验收门 |
| area:control-plane | control-plane-engineer | 包内测试 | — |
| 无匹配标签 | `roles.owners`（OX: heavy-engineer, gbot；Studio: frontend, control-plane） | | |

- reviewer：yielded-reviewer（两个项目共用，独立于所有 owner）。lead：yielded-lead。
- 验收门：`needs-GUI-Master` / `needs-acceptance` / `needs-Ray-signoff`，Studio 另有 `acceptance.default_for_labels: [area:studio-app]`。Ray 在 Flow 卡片或 `multica flow accept|reject` 决定，引擎在同一 head 上合并。
- CI：required_checks 用 ci.yml 的 job 名 test / rust / bundle / lint + review-verdict。matrix job 在 GitHub 上的 check 名可能带 os 后缀，第一次 PR 时要核对，再改 spec。

## 构建资源
- `limits.slots.cargo: 2`：带 `needs:cargo` 的 issue 在本项目里同时最多 2 条处于 todo/in_progress，其余在 T1 等待（`waiting: cargo slots 2/2`）。计数范围是同一个项目；OX 和 Studio 若同时大量跑 Rust，总数最多 4，必要时把 Studio 设为 1。
- 每个 worktree 自己的 `target/`（AGENTS.md 规则不变），编译缓存靠 sccache 共享。flake.nix devShell 需加 `sccache`（未写，flake 结构由 Ray 定）。
- `limits.min_free_gb: 40`（键名与现有 spec 一致）。stuck/idle 放宽到 60/15 分钟（OX）和 45/12（Studio）。

## 引擎支持（已实现，feat/orchestrator）
| 键 | 行为 | 测试 |
|---|---|---|
| `roles.routes` | 第一个标签匹配的 route 决定 owner 池（大小写不敏感）；池全忙就等待，不回落到别的池；T7 unblock 也用该池 | `TestRoutesPickOwnerByLabel` |
| `limits.slots.<name>` | 带 `needs:<name>` 的 issue 在 slot 满时等待；不带标签的不受影响 | `TestCargoSlots` |
| `acceptance.default_for_labels` | 带这些标签也走 T10 验收门 | `TestDefaultAcceptanceLabels` |
| 校验 | route 必须有 labels 和 owners；slot 必须为正；route owner 不能是 reviewer | `TestValidateSettings` |

## 待 Ray 定
- 核对 required_checks 实际名称。
- gbot 既是 owner 又拆分：拆分后父单写 `phase=planned`（引擎不再启动它），子单建成 todo 由引擎派（backlog = 未计划，引擎不碰）。
- 两个项目共用一个 hub 和仓库，worktree 都在 `~/.cursor/worktrees/pocketjs/agent-<key>`。
- 分支命名：`agent/<key>`（worktree `agent-<key>`）只给编排器用；herdr worker 用 `herdr/<task>`，不要用 `agent/<task>`，否则会占住编排分支，issue 被 `[branch_occupied]` 阻塞。
