# Orchestrator 运维手册

## 看健康

```bash
multica team status          # last_beat（每次处理/对账更新，>2 分钟不动 = worker 卡住）、last_error、teams
multica flow show <KEY>      # 单条：failures、backoff_until、last_error、waiting、最近转移
```

服务端日志关键字：`orchestrator: transition`、`orchestrator: waiting`、`orchestrator: transition failed`、`orchestrator: started`。

SQL（只读）：

```sql
SELECT tid, state, error, source, created_at FROM orchestrator_transition WHERE issue_id=$1 ORDER BY id;
SELECT * FROM orchestrator_issue_state WHERE failures > 0;
SELECT count(*) FROM orchestrator_event WHERE processed_at IS NULL;   -- 堆积 = worker 不动
```

## 急停

| 范围 | 做法 | 恢复 |
|---|---|---|
| 一个 project | `multica team pause <project> --reason ...` | `multica team resume <project>`（立即对账） |
| 一个 project（无 CLI） | 给任一未完成 issue 打 `orchestrator-pause` 标签 | 去掉标签 |
| 整个 server | 环境变量 `MULTICA_ORCHESTRATOR_DISABLED=1` 后重启 | 去掉后重启 |

暂停期间写保护也关闭，人可以直接改状态；恢复后 T9 会把与 phase 不一致的状态收敛，先把 phase 改对（或 `flow unblock`）再恢复。

## 常见情况

**issue 不动。** `flow show`：
- `waiting: owner X busy / all owners busy` —— 正常排队。
- `waiting: disk guard` —— 清理磁盘（worktree-cleanup），或调 `limits.min_free_gb`。
- `waiting: re-running failed CI once` / `test=PENDING` —— 等 CI。
- `failures > 0` —— 看 `last_error`，修好后 `flow replay`。
- `next` 为空、状态 todo/in_progress、有活动 run —— agent 在干活；超过 stuck_min 会被取消重跑。

**DEAD-LETTER 通知。** 同一 issue 连续 5 次转移失败（API 拒绝、gh 失败、git 失败）。issue 已 blocked 给 lead。修原因 → `flow unblock`。

**blocked 提醒。** blocked 超过 `blocked_remind_min` 每隔同样时间提醒一次。处理：`flow unblock [--owner]` 或 `flow cancel`。

**有人用 --force 改了状态。** 下次事件/对账 T9 会按 phase 收回；想保留人工结果就先 `team pause`。

**合并失败（push_failed / gh 错误）。** 不退回 owner，下一轮重试；连续失败走 dead-letter。检查 `gh auth status`、分支保护、`--match-head-commit` 不匹配（有人在审后推了新提交 → 会触发重审）。

**server 重启。** 无需处理：启动时全量对账；进程内 PAT 重新生成；转移日志保证不重复执行。

## 改 team

```bash
multica team diff  <project> -f spec.yaml    # 只读
multica team apply <project> -f spec.yaml
```

或改 `.multica/project.yaml` 的 `orchestrator:` 段后跑 `multica-config` 的 `sync.ts`（`--check` 先看计划）。

卸载：`DELETE /api/orchestrator/teams/<project>`（admin）。issue 的 metadata 保留，状态之后由人管理。

## 故障注入（测试环境）

`MULTICA_ORCHESTRATOR_FAULT=<tid>:<n>`：对该转移的前 n 次在 metadata 已写、状态未写时注入失败（`*` 匹配全部）。用来验证 T9 收敛、backoff 和 dead-letter。
