# 生产就绪评估与上线计划（2026-10-19）

结论：**不能直接上生产**。引擎、动作器、UI 和 e2e 场景（PR-S1..S6、验收门、T3c、hub 模式）在本地栈上已跑通；但在生产的形态（K8s、多副本、服务端没有本地 hub、GitHub 凭据）下还有 5 个硬阻塞。按下面顺序消掉，每一步都可单独回滚。

## 阻塞项

| # | 阻塞 | 现状 | 要做的 | 估计 |
|---|---|---|---|---|
| B1 | 多副本 | 每个 API 副本都跑 orchestrator 循环。转换有 DB 幂等键（issue+tid+revision）兜底，不会重复写状态；但 merge、gh 调用、CI 重跑、hub 刷新这类外部副作用每个副本各做一次 | Postgres advisory lock 选主（`pg_try_advisory_lock(hash('orchestrator'))`，持有连接即持有锁，断线自动释放）；非主副本只服务 API；`/api/orchestrator/status` 显示 leader | 0.5 天 + 测试 |
| B2 | 服务端仓库操作 | 引擎的 hub 刷新、集成 worktree、`gh pr merge` 在 API 进程所在机器执行。生产 API 在 K8s pod 里，没有 Ray 的本地 hub，也没有 gh 登录 | 生产走 GitHub 模式且不需要 hub：合并、PR 查询、状态、CI 重跑全部改用 GitHub REST（App 安装令牌），patch-id 比较改成比较 compare API 的 diff 哈希；hub 只留给 daemon（worktree 本来就在 daemon 侧用 wt 建） | 1.5–2 天。**进展（10-19）**：合并、PR 查询/创建、状态、CI 重跑已改用 App REST（`internal/github/app`），服务端不再需要 gh；hub 刷新、check_cmd 集成 worktree、patch-id 比较仍在 API 机器上跑 git |
| B3 | GitHub App | App 已建并配置在 K8s（见 github-app.md），安装覆盖 xesrevinu；编排器的合并/门禁仍走 gh 个人登录 | 建 GitHub App（contents:write、pull_requests:write、checks/statuses:read、actions:write 仅重跑），安装到目标仓库；服务端用 App 私钥换安装令牌；agent push 仍用各自 daemon 的凭据；branch protection 要求 `review-verdict` 状态 | 0.5 天 + Ray 建 App |
| B4 | 迁移编号 | 生产库停在 573/574；本分支 583/584/585。migrate 按文件名记录（`skip 583_orchestrator (already applied)`），跳号本身没问题；上线时会把 575–585 一次性带上，其中 575–582 来自上游主线 | 上线前在生产快照上演练 `migrate up` 和 `down` 到 574；fork 脱离上游后，把编排器迁移改成保留段（如 `9000_orchestrator`…），避免以后自己的编号和上游重名。生产从未执行过 583+，此时改名零成本 | 0.5 天 |
| B5 | 影子运行 | **已实现** `shadow: true`：照常决策，转换记为 `state=shadow`，不写状态/指派/元数据，不开 run、不建 PR、不合并、不重跑 CI，写保护也不生效 | e2e 已验证（ATT-68：记录 `T1_start shadow`，issue 保持 backlog、无 run、成员改状态不被拦）。下一步在 OX 真实 issue 上开 shadow 一周，每天对比"引擎会做什么"和"人实际做了什么" | 已完成，待在 OX 运行 |
| B6 | 部署/回滚演练 | 没做过 | 见下文 rollout 阶段 1 | 0.5 天 |
| B7 | 监控 | 只有日志和 `team status` 心跳 | 指标：每分钟转换数（按 tid、state）、失败/死信数、等待原因分布、run 时长、阻塞时长、合并耗时；告警：心跳超过 3 个周期没更新、死信 >0、同一 issue 15 分钟内 >10 次转换（抖动）、磁盘守卫触发；dashboard 一张 | 1 天 |

不阻塞但上线前应修：

- **agent 凭据和环境**：daemon 用 `env -i` 重启会丢 provider key，所有 run 秒失败（今天在 e2e 上复现过：6 次 "Connection error"）。runbook 要写清楚 daemon 的环境来源，并给 daemon 加启动自检：缺 key 时拒绝认领任务。
- **owner rebase 与 daemon 交付校验冲突**：已改为 merge origin/main、禁止 force push（skill rebase-conflict）。
- **lead 写 `phase=unblocked/cancel_proposed/planned` 被守卫拒绝**：已修。
- **reviewer 动了 run 目录的 HEAD，导致 daemon 保留现场、下一次 wt switch 失败**：daemon 遇到占位 worktree 时改为 fork 新分支；reviewer 指令禁止在 run 目录 checkout。
- **失败的 reviewer run 计入 review 次数**：已改为计入 infra 次数。

## 上线计划

| 阶段 | 内容 | 进入条件 | 回滚 |
|---|---|---|---|
| 0 预备 | 完成 B1、B2、B3、B4、B7；镜像带上 git（gh 不再需要） | 单测 + e2e 全绿；在生产快照上 `migrate up/down` 演练通过 | 不涉及生产 |
| 1 部署演练 | 在 staging（生产库快照 + 1 副本）部署本分支：迁移、启动、`team apply` 一个 shadow 团队、回滚到旧镜像 + `migrate down` 到 574、再前滚 | 两次往返都无数据差异（对比 issue/metadata 行数和校验和） | 旧镜像 + down |
| 2 生产 shadow | 生产部署，先带 `MULTICA_ORCHESTRATOR_DISABLED=1` 部署（已有开关），确认无副作用后去掉；只给 OX 的 Pocket 项目 apply `shadow: true` | 部署后 API 指标无变化 | 删 team（`orchestrator_team` 一行）或关开关 |
| 3 shadow 评估 | 一周。每天导出 shadow 转换和真实 activity 对比：漏判（人做了、引擎没想做）、误判（引擎想做、人没做）、时机差 | 误判 0、漏判都有解释 | 同上 |
| 4 单项目接管 | Pocket 去掉 shadow；先只放低风险 area（compiler/framework），`needs:cargo` slot 1，所有单走验收门 | 3 天内无死信、无人工纠正状态 | `multica team pause`（立即停止所有转换）；必要时 `shadow: true` |
| 5 扩大 | 放开验收门默认值、slot 到 2；接入 Studio Parts；多副本开启选主 | 阶段 4 指标稳定 | 同上，按项目 |

开关和止损（已存在）：`multica team pause|resume`（单项目急停）、`flow unblock|cancel|replay`、成员用 `--force` 覆盖写保护、死信自动转 blocked 给 lead。全局急停：`MULTICA_ORCHESTRATOR_DISABLED=1`（已存在）。新增建议：`/api/orchestrator/status` 增加 leader 字段。
