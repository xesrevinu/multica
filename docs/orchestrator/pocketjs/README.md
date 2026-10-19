# PocketJS 接入编排器（提案，未应用到 pocketjs 仓库）

| 文件 | 内容 |
|---|---|
| `multica-orchestrator.patch` | 改 pocketjs 的 `.multica/`：project.yaml 两个项目（Pocket / Studio Parts）加 `orchestrator:` 段并去掉 `github_repo` 资源；lead 指令改成只做规划和 blocked 处理；workflow.md 重写为引擎流程 + 区域/owner/检查表；新 skill `pocket-checks` |
| `envrc-sccache.patch` | `.envrc`：有 sccache 时设置 `RUSTC_WRAPPER`、共享 `SCCACHE_DIR`、40G 上限 |

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
- gbot 既是 owner 又拆分：拆分后父单写 `phase=planned`（引擎不再启动它），子单回 backlog 由引擎派。
- 两个项目共用一个 hub 和仓库，worktree 都在 `~/.cursor/worktrees/pocketjs/agent-<key>`。
