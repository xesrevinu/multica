# Orchestrator 设计

代码：`server/internal/orchestrator`。迁移：`583_orchestrator`。参考实现：外部 `teamctl`（e2e worktree，保留作对照）。

## 目标与边界

- 一个 project 一份 team spec；挂上之后 status/assignee 只有一个写者。
- 决策是纯函数：同一个快照 + 同一个探针结果，得到同一个决定。
- 每次交接恰好启动一次 run（status+assignee 一次写入，走普通 issue API）。
- 不做：多 preset、自定义边的执行（spec 里的 `transitions` 现在只用于校验/图/断言）、按评论文字判断事件。

## 组件

| 文件 | 职责 |
|---|---|
| `spec.go` | Spec（YAML `orchestrator:` 段）、preset 默认值、校验不变量、mermaid 图 |
| `engine.go` | `Spec.Decide(snapshot, prober) *Decision`，纯函数，单测覆盖所有转移 |
| `store.go` | SQL：快照、转移日志、事件队列、backoff/提醒状态 |
| `repo.go` | git/GitHub：分支 head、本地 squash 合并、PR 闸门（App 快照新鲜时用快照，否则 App REST 实时读；`repo_github.go`）、CI 重跑一次、合并后删分支 |
| `runtime.go` | Service：总线订阅、去抖 worker、reconciler、actuator、通知、服务成员 |
| `guard.go` | 写保护：`orchestrator_owned` |
| `api.go` | `/api/orchestrator/*`（team、flow） |

## 数据

- `orchestrator_team(project_id, spec, paused, human_user_id, …)`：有行 = 被编排。
- `orchestrator_transition`：只追加。`idem_key = issue|tid|revision` 唯一；同一修订上的同一转移只执行一次。`state` started/done/failed。
- `orchestrator_event`：总线事件落库（只收被编排 project 的 issue），worker 认领后标记。
- `orchestrator_issue_state`：连续失败次数、`next_at`（指数退避，5s 起，封顶 5 分钟）、最近等待原因、提醒时间。
- issue 本身：机器状态在 `issue.metadata`（phase、owner、head_sha、reviewed_sha、verdict、attempt_*、blocked_reason、pr、merged_sha、acceptance）。

## 执行路径

```
bus 事件 ──→ orchestrator_event ──→ worker（300ms 去抖，单 goroutine = 每 issue 串行）
reconciler 每 60s 全量 ─────────────────┘
        │
        ▼
snapshot（issue、兄弟 issue、runs、busy、磁盘、分支 head）→ Decide → Decision
        │
        ▼
actuator: ① 事务：插入 transition(started) + 合并 metadata
          ② 进程内调用 API（服务成员 PAT）：cancel run / PUT status+assignee / rerun
          ③ inbox 通知 ④ transition=done
```

①和②之间崩溃：phase 已写、status 没写，下次评估 T9 按 phase 收敛。②失败：transition=failed，
backoff 重试；同一 revision 的 idem_key 会复用这行（failed 不算 done）。连续失败达到
`dead_letter_after`（默认 5）：直接 T6 block + `DEAD-LETTER` 通知。

服务成员是全局用户 `orchestrator@system.multica.local`，apply 时加入 workspace（admin）。PAT 每次启动重新生成，只在内存里。
走 API 而不是直接写表，是为了让一次转移和人改一次 issue 完全相同：同一个 run 派发、同一套事件和 WS 广播。

## 状态机（impl-review-merge）

| id | 从 | 到 | 条件 |
|---|---|---|---|
| T1_start | todo（phase 为空） | todo→owner | deps 完成、stage 屏障放行、磁盘 ≥ min_free_gb（0/负数关闭）、owner 空闲（跨 project 统计）、slot 有空（只算已启动的兄弟）。成员或非 owner agent 持有的 todo 不动；已指派给 owner 时重跑或接管已在跑的 run。**backlog = 未计划，引擎从不碰** |
| A1 | todo | in_progress | agent 自己写（唯一允许 agent 写的状态） |
| T2_review | todo/in_progress | in_review→reviewer | phase=review_requested、分支已推、无活动 run、预检通过（本地合并+check_cmd；GitHub：PR 存在/自动开、无冲突、CI 不红） |
| T3_rework | in_review/todo/in_progress | todo→owner | verdict=changes（大小写不敏感）、CI 红（已重跑过一次）、冲突、验收拒绝；attempt_impl+1 |
| T3b_rereview | in_review | in_review→reviewer | 没有 verdict（rerun）、reviewed_sha≠head（等待验收期间 head 真变了）、或 verdict 不是 approve/changes；旧 reviewed_sha 记到 `last_reviewed_sha`，reviewer 只看此后的 diff |
| T3c_carry_verdict | in_review | 同 | approve 后 head 变了但 patch-id 相同（纯 rebase/合 main）：verdict 挪到新 head |
| T4_done | in_review | done | approve@head、required checks 绿、mergeable、（需要时）验收通过；GitHub App REST squash merge（`sha` 防护，标题 = PR 标题，合并后删分支）或本地 squash+push |
| T5_rerun（_norun/_nobranch） | todo/in_progress | 同 | run 结束但没交接；失败 run 记 infra，否则记 impl |
| T5q_provider_limit | todo/in_progress/in_review | 同 | 上一个 run 的 `failure_reason` 是 provider 配额/限流：按 1m,2m,4m…≤30m 退避后重跑，**不计次数**（`attempt_quota` 只管退避） |
| T5b_cancel_stuck | 活动状态和 blocked | 同 | 运行中的 run 超过 stuck_min，或 idle_min 内没有 task_message，或排队/已派发但没开始超过 queued_min（默认 15）。blocked 上 lead 的 run 同样检测 |
| T6_block | 任意活动 | blocked→lead | 次数用尽、agent phase=blocked、dead-letter、provider 认证/权限失败（`failure_reason`）、`agent/<key>` 被别的 worktree 占用（run error 带 `[branch_occupied]`，`blocked_reason` 写占用路径，不计 infra 次数）、review 期间分支被动过（reviewer/lead 只读）；通知 |
| T7_unblock | blocked | todo→owner | 成员 `flow unblock`（`unblock_confirmed`，计数和 `unblock_count` 清零）；或 lead 写 phase=unblocked 且 `unblock_count` < `limits.unblocks`（默认 2，`unblock_count`+1） |
| T7b_unblock_escalate | blocked | blocked→human | lead 解除次数用尽：交给人，不再清计数，只认成员 `flow unblock` |
| T8a_cancel_proposed | blocked | blocked→human | lead 写 phase=cancel_proposed：通知人确认，不直接取消 |
| T8_cancel | blocked | cancelled | 成员确认取消（`flow cancel` 或 UI「确认取消」，都走 `/flow/{key}/cancel`，写 `cancel_confirmed`） |
| T8b_member_cancel | 任意非终态 | cancelled | 成员对非 blocked issue `flow cancel`（`cancel_requested`） |
| T9_reconcile | todo/in_progress/in_review | phase 对应状态 | status 与引擎写的 phase 不一致（崩溃或越权修改）；T1 写完 metadata 后崩溃（todo、无人指派）。blocked 优先于 T9（blocked 只能经 T7/T8 或成员动作离开）；backlog 不收敛 |
| T10_await_acceptance | in_review | in_review→human | 带验收标签，approve+绿；通知 |

终态（done/cancelled）从不被 T9 拉回。

`limits` 的数值：没写 = 默认值；写 0 或负数 = 关闭（`idle_min`、`min_free_gb`、`queued_min`），`unblocks: 0` = lead 不能自行解除。`idle_min_by_backend` 按 run 所在 runtime 的 provider 覆盖 idle_min，预设 `gbot: -1`（gbot 在 run 结束前没有输出；grok-bot-cli 应该在 `--json` 模式下输出进度事件，之后再去掉这个覆盖）。

## 不变量（吸收自 issueworkflow/FLOW.md 草案）

- 未知状态/角色、重复 id 在 apply 时拒绝。
- agent 永远不能把 issue 送进 done/cancelled；只有引擎和成员可以。
- 状态修改不对应一条边就拒绝——这里落实为：被编排 project 上，非服务成员的 status/assignee 写入一律拒绝（成员可 `--force`）。
- reviewer 不能同时是 owner（独立验证者，来自 pstack shipping）。
- 评论文字不是事件。事件是结构化的 metadata 写入。

## 写保护

`UpdateIssue`、`BatchUpdateIssues`、`SetIssueMetadataKey`/`DeleteIssueMetadataKey`、`AttachLabel`/`DetachLabel` 调 `Service.CheckWrite` / `CheckAgentMetaWrite` / `CheckAgentLabel`：

- 服务成员：放行。
- agent：只能 todo→in_progress；不能改 assignee（`--no-start` 也不行）。引擎自有 key（`owner`、`attempt_*`、`merged_sha`、`acceptance*`、`review_started_sha`、`paused`、`cancel_*`、`unblock_*`、`last_reviewed_sha` 等）不能写也不能删；phase/verdict/reviewed_sha/head_sha/risk/blocked_reason/deps 能写不能删；phase 值和当前状态要匹配（例如 unblocked/cancel_proposed 只在 blocked 时）；verdict 只能 approve|changes，verdict/reviewed_sha 只能在 in_review 时写；risk 只能升不能降。标签：不能加/删 `orchestrator-pause`，不能删验收标签（可以加）。
- 成员：拒绝，除非 `X-Orchestrator-Override: 1`（CLI `--force`）。
- team paused 时不保护（人接管）。

拒绝返回 403 `{"code":"orchestrator_owned"}`，消息写明拒绝了什么（status a → b / assignee change）；CLI 原样显示原因（退出码 3），`issue assign` 额外提示 `--no-start` 不解除限制、成员可 `--force`。

PR 自动完成（`pr_merge_status`）对被编排 project 视为 `none`：合并后移动 issue 是引擎的事，webhook 不能抢。

## GitHub

- App 已配置且 `github_pull_request` 快照 2 分钟内更新过：预检直接读快照（head、checks rollup、mergeable），不调 GitHub API。
- 否则：用 App REST 实时读 PR、check runs 和 commit statuses（reconciler 每 60s，加事件触发；GET 走 ETag，304 不耗额度）。合并走 `PUT /pulls/{n}/merge`，带 `sha`，不会合进没审过的 head。
- CI 红：对这个 head 的失败 workflow run 调一次 `POST /actions/runs/{id}/rerun-failed-jobs`；同一个 head 第二次红才退回 owner（pstack babysit 规则）。
- stale（PR/快照的 head 落后于分支）：T2、T10、合并都等待，不审也不退回。
- `review-verdict`：只信 GitHub App 自己建的。所有 agent 共用用户的 gh 登录，用户身份发的 status 证明不了是谁写的，statusCheckRollup 也不带创建者。引擎按自己的 metadata（verdict=approve 且 reviewed_sha==head）判定；合并前用 App 安装令牌发 `review-verdict=success`（创建者是 `<GITHUB_APP_SLUG>[bot]`），head 上已有 App 发的 success 就不重发；其他创建者的 review-verdict（含伪造的 failure）一律忽略。App 快照 rollup 为 failure/error 时不直接判红，改走逐项检查（只看 required checks，review-verdict 只看 App），所以伪造的失败状态不会再造成返工。其他 CI 检查照常信任。需要：`GITHUB_APP_ID`、`GITHUB_APP_PRIVATE_KEY`（`GITHUB_APP_SLUG` 可选；不设时由共享 App 客户端 `GET /app` 解析 bot 登录名并缓存），App 权限加 commit statuses: write；缺任何一项合并会等待并写明原因。建议 branch protection 把 `review-verdict` 的 expected source 设为这个 App。reviewer 不再发。
- review 期间（phase=reviewing）分支只能停在 `review_started_sha`（纯 rebase 例外），否则 T6 block。daemon 层拦 push 没做：daemon 不知道任务的编排角色，git 层的 pushurl/hook 在 run 里都能绕过。

## 来自 pstack 的做法

| pstack | 这里 |
|---|---|
| 独立验证者，只认 ready SHA 上的结论 | reviewer≠owner；verdict 绑定 reviewed_sha；head 变了就重审 |
| babysit：CI 只重跑一次，第二次同样失败就是真失败 | `retryCI` |
| 合并前确认验的就是要合的 | `--match-head-commit`，本地模式在当前 main 上重新合并+测试 |
| 一个分支一个写者，只 `--force-with-lease` 自己的分支 | 写进 role skill / orchestrated.md |
| PR 描述：Why / What changed / Scope / Blast Radius / Verification | open-pr skill |
| worktree-cleanup | 合并后删远端分支；daemon 复用空闲 worktree |
| 卡住按副作用判断 | stuck_min 只算运行中的 run |

## 已知限制

- 单 worker：一个慢合并（本地 check_cmd）会拖住其他 issue 的评估。规模大时改成每 project 一个 worker。
- 多副本部署：worker 没有选主；同时跑两个 server 时两个都会评估。idem_key 防止重复执行同一转移，但 squash merge 这类外部动作可能被尝试两次（GitHub 侧幂等）。需要上线前加 advisory lock 选主。
- spec 的 `transitions` 只做校验和展示，引擎只实现 preset。

## Verdict carry-over (T3c)
When the reviewer approved `reviewed_sha` and the branch head moved afterwards (the owner merged/rebased main, or a rebase-conflict fix that only replays the same change), the engine compares `git patch-id --stable` of each commit's diff against its merge-base with main. If they are equal, T3c_carry_verdict moves the verdict to the new head (`verdict_carried_from` records the old sha) and the flow goes on to the gate/merge without another review round. Any doubt (force-pushed-away object, fetch failure, different patch) falls back to T3b re-review. The CI gate still runs on the new head.

## 分支独占（`agent/<key>`）

编排中的项目（有 team、未暂停、非 shadow），server 在 daemon 领任务时下发 `exclusive_branch = agent/<key>`。daemon 准备 worktree 时，若计划落在这个分支而它已被别的 worktree 检出（含 wt 把 `agent/att-62` 净化成 `agent-att-62` 后路径撞上的情况），**不再回退到 `agent/<key>-<task>`**（那个分支 engine 永远不看），而是失败：`[branch_occupied] orchestrator branch agent/<key> is held by the worktree at <path>; ...`，路径取自 `git worktree list --porcelain`。engine 在 `agent_task_queue.error` 里认出这个标记，T6 交 lead，`blocked_reason` 写明路径；腾出 worktree 后 `multica flow unblock KEY`。非编排项目与 reviewer run（计划不落在该分支）仍按原样 fork（MUL-6881）。约定：别的工具（herdr 等）不要用 `agent/` 前缀或 `agent-<key>` 路径，改用 `herdr/<task>`。
