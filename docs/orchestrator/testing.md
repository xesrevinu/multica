# 编排器测试分层

| 层 | 覆盖 | 命令 | 耗时 |
|---|---|---|---|
| 1 单元 | 引擎各分支、spec 校验、守卫、preset 渲染、patch-id | `go test ./internal/orchestrator` | ~1s |
| 2 回放（golden） | `testdata/scenarios/*.yaml`：每个场景是一串"外部世界变化 → 引擎必须做的决定"，模拟器按动作器的方式把决定应用回状态。带 `recorded:` 的场景，其转换序列必须等于 `testdata/recorded.txt` 里该 issue 的真实 e2e 序列（从 e2e 库 `orchestrator_transition` 导出） | 同上（`TestReplayScenarios`） | <0.1s |
| 3 集成（假 agent / 假 GitHub / 真 DB） | 真实 runtime：snapshot → Decide → 动作器（元数据事务、幂等键）→ 转换日志、时间线、inbox；假 API 路由做 PUT/rerun/cancel（指派 agent 即排队 run），假 agent 按脚本写元数据，假 GitHub 给 head / 预检 / 合并结果 | `eval "$(server/scripts/orch-testdb.sh \| tail -1)"; go test ./internal/orchestrator -run Integration` | ~1s（建库 + 迁移首次 ~5s） |
| 4 真 agent e2e | daemon、wt worktree、hooks、真实 LLM agent 读指令和 skills、真实 GitHub | `.orch-dev` 下的 mk.sh / ev.sh | 每个场景 3–10 分钟 |

## 已录入的场景
回放（21 个）：S1、PR-S1、S2/PR-S2、GA（拒绝+卡死取消+再审+接受）、OOB、F1（T1 元数据提交后崩溃）、S5 磁盘守卫、S6/PR-S6 stage 屏障和 deps、S3 预检冲突、PR-S3 合并冲突、PR-S4 idle 取消三次→blocked→unblock、PR-S5 CI 抖动等待、T3c 同 patch 保留 verdict 及反例、ATT-65/67 环境故障、ATT-64 回归（失败的 reviewer run 计 infra）、lead 的 blocked 答复、planned epic、routes/slots/验收默认标签、路由池忙时等待。
集成（7 个）：happy path（3 条时间线、1 次合并、2 次 run）、验收门拒绝→重做→接受（inbox 有 orchestrator_alert）、OOB 收敛、F2 死信转 blocked、shadow 不改任何东西且不拦写、4 个并发 reconcile 只应用一次、cargo slot 跨 issue。

集成层第一次跑就抓到一个真 bug：同一轮 reconcile 里两个 `needs:cargo` 的 issue 都被启动了（兄弟列表是本轮开头的快照）。已修：backlog 的快照重新读取兄弟 issue。

## 什么时候仍然要跑真 agent e2e
- 改了 preset 的指令或 skills（agent 是否真的照做，只有真模型能验证：ATT-64 的 rebase、lead 写错 phase 都是这样暴露的）。
- 改了 daemon / execenv / wt 集成（worktree 路径、hooks、交付校验、分支名）。
- 改了 GitHub 交互（PR 创建、check 名、合并、webhook / ghsnapshot）。
- 升级 agent provider 或模型。
引擎、动作器、spec 字段、UI 文案的改动，层 1–3 足够。

## 最小真 agent 冒烟集（约 15 分钟，可并行）
| 场景 | 验证 |
|---|---|
| PR-S1 | 指派 → wt worktree + hook → owner 开 PR（标题带 KEY、分支 `agent/<key>`）→ reviewer 结论 → 合并 → hub 前进 |
| PR-S3（两个改同一处的 issue） | 冲突 → owner `git merge origin/main`（不 rebase）→ 再审 → 合并 |
| GA | `needs-GUI-Master` → 验收门 → `flow reject --reason` → 返工 → `flow accept` |
| blocked | 给一个缺信息的 issue → owner `phase=blocked` → lead 写 `phase=unblocked` 或 `cancel_proposed` 被接受 |
