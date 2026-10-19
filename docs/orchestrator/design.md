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
| `repo.go` | git/GitHub：分支 head、本地 squash 合并、PR 闸门（App 快照新鲜时用快照，否则 `gh`）、CI 重跑一次、合并后删分支 |
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
| T1_start | backlog | todo→owner | deps 完成、stage 屏障放行、磁盘 ≥ min_free_gb、owner 空闲（跨 project 统计） |
| A1 | todo | in_progress | agent 自己写（唯一允许 agent 写的状态） |
| T2_review | todo/in_progress | in_review→reviewer | phase=review_requested、分支已推、无活动 run、预检通过（本地合并+check_cmd；GitHub：PR 存在/自动开、无冲突、CI 不红） |
| T3_rework | in_review/todo/in_progress | todo→owner | verdict=changes、CI 红（已重跑过一次）、冲突、验收拒绝；attempt_impl+1 |
| T3b_rereview | in_review | in_review→reviewer | 没有 verdict（rerun）或 reviewed_sha≠head |
| T4_done | in_review | done | approve@head、required checks 绿、mergeable、（需要时）验收通过；GitHub App REST squash merge（`sha` 防护，标题 = PR 标题，合并后删分支）或本地 squash+push |
| T5_rerun | todo/in_progress | 同 | run 结束但没交接；失败 run 记 infra，否则记 impl |
| T5b_cancel_stuck | 活动状态 | 同 | 运行中的 run 超过 stuck_min（排队中不算） |
| T6_block | 任意活动 | blocked→lead | 次数用尽、agent phase=blocked、dead-letter；通知 |
| T7_unblock | blocked | todo→owner | phase=unblocked（`flow unblock`），计数清零 |
| T8_cancel | blocked | cancelled | phase=cancel_proposed（`flow cancel`） |
| T9_reconcile | 任意活动 | phase 对应状态 | status 与引擎写的 phase 不一致（崩溃或越权修改） |
| T10_await_acceptance | in_review | in_review→human | 带验收标签，approve+绿；通知 |

终态（done/cancelled）从不被 T9 拉回。

## 不变量（吸收自 issueworkflow/FLOW.md 草案）

- 未知状态/角色、重复 id 在 apply 时拒绝。
- agent 永远不能把 issue 送进 done/cancelled；只有引擎和成员可以。
- 状态修改不对应一条边就拒绝——这里落实为：被编排 project 上，非服务成员的 status/assignee 写入一律拒绝（成员可 `--force`）。
- reviewer 不能同时是 owner（独立验证者，来自 pstack shipping）。
- 评论文字不是事件。事件是结构化的 metadata 写入。

## 写保护

`UpdateIssue`、`BatchUpdateIssues`、`SetIssueMetadataKey` 调 `Service.CheckWrite/CheckAgentMeta`：

- 服务成员：放行。
- agent：只能 todo→in_progress；不能改 assignee；不能写 `owner`、`attempt_*`、`merged_sha`、`acceptance*`、`review_started_sha`；phase 只能写 review_requested/blocked/implementing。
- 成员：拒绝，除非 `X-Orchestrator-Override: 1`（CLI `--force`）。
- team paused 时不保护（人接管）。

拒绝返回 403 `{"code":"orchestrator_owned"}`；CLI 原样显示原因（退出码 3）。

PR 自动完成（`pr_merge_status`）对被编排 project 视为 `none`：合并后移动 issue 是引擎的事，webhook 不能抢。

## GitHub

- App 已配置且 `github_pull_request` 快照 2 分钟内更新过：预检直接读快照（head、checks rollup、mergeable），不调 GitHub API。
- 否则：用 App REST 实时读 PR、check runs 和 commit statuses（reconciler 每 60s，加事件触发；GET 走 ETag，304 不耗额度）。合并走 `PUT /pulls/{n}/merge`，带 `sha`，不会合进没审过的 head。
- CI 红：对这个 head 的失败 workflow run 调一次 `POST /actions/runs/{id}/rerun-failed-jobs`；同一个 head 第二次红才退回 owner（pstack babysit 规则）。

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
- patch-id 复用 verdict（rebase 后 diff 不变免重审）未实现。
- spec 的 `transitions` 只做校验和展示，引擎只实现 preset。

## Verdict carry-over (T3c)
When the reviewer approved `reviewed_sha` and the branch head moved afterwards (the owner merged/rebased main, or a rebase-conflict fix that only replays the same change), the engine compares `git patch-id --stable` of each commit's diff against its merge-base with main. If they are equal, T3c_carry_verdict moves the verdict to the new head (`verdict_carried_from` records the old sha) and the flow goes on to the gate/merge without another review round. Any doubt (force-pushed-away object, fetch failure, different patch) falls back to T3b re-review. The CI gate still runs on the new head.
