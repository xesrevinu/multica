# Orchestrator 使用指南

Orchestrator 是 server 内置的确定性状态机。给一个 project 挂上 team 之后，这个 project 里 issue 的
**status 和 assignee 只由 orchestrator 写**。Agent 只干活、写 metadata、写一条结果评论。

三份文档：本指南（怎么用）、[design.md](design.md)（为什么这样设计）、[runbook.md](runbook.md)（出事了怎么办）。

## 1. 挂上 team

在 `.multica/project.yaml`（或单独文件）里写 `orchestrator:` 段，见 [examples/team.yaml](examples/team.yaml)：

```yaml
orchestrator:
  preset: impl-review-merge          # 目前唯一的 preset
  roles:
    lead: at-lead                    # blocked 的 issue 交给它
    reviewer: at-reviewer            # 独立验证者，不能同时是 owner
    owners: [at-dev-a, at-dev-b]     # 实现者，按空闲分配
    human: ray@example.com           # 验收 + 通知对象，默认是 apply 的人
  repo:
    mode: github                     # github | local
    slug: owner/repo
    main: main
    check_cmd: npm test && npm run lint   # 送审前的本地预检（可选）
    required_checks: [test, lint]
  limits: { impl: 3, review: 3, infra: 2, stuck_min: 20, min_free_gb: 20 }
  acceptance:
    labels: [needs-acceptance, needs-GUI-Master, needs-Ray-signoff]
```

```bash
multica team diff  <project> -f .multica/project.yaml   # 校验 + 预览改动
multica team apply <project> -f .multica/project.yaml   # 生效（owner/admin）
multica team status                                     # 心跳、teams
```

`multica-config` 的 `sync.ts` 会把 project.yaml 的 `orchestrator:` 段同步成 `team apply`。

## 2. 一条 issue 怎么走

```
backlog ─T1→ todo(owner) ─agent→ in_progress ─T2→ in_review(reviewer) ─T4→ done（合并）
                ↑                                      │
                └──────────── T3 rework ───────────────┘  verdict=changes / CI 红 / 冲突 / 验收拒绝
任何状态 ─T6→ blocked(lead) ─T7→ todo   或 ─T8→ cancelled
```

| 谁 | 写什么 | 结果 |
|---|---|---|
| 人 | 建 issue，status=backlog | 依赖完成、stage 放行、磁盘够、有空闲 owner 时 T1 分配 |
| owner | push `agent/<KEY>`，`phase=review_requested`、`head_sha`、`pr` | 预检（合并+check_cmd/PR 状态），通过则 T2 给 reviewer；失败 T3 退回 |
| reviewer | `verdict=approve|changes`，`reviewed_sha=<跑过的 sha>` | approve 且 sha==head 且 CI 绿 → T4 合并；否则退回或重审 |
| 任何 agent | `phase=blocked`、`blocked_reason` | T6 给 lead，通知你 |

带验收标签（或 metadata `acceptance_required=true`）的 issue 在 approve + CI 绿之后进入
`phase=awaiting_acceptance`，assignee 变成你，inbox 收到 `orchestrator_alert`。

## 3. 人用的命令

```bash
multica flow show <KEY>            # 状态、metadata、下一步（dry-run）、最近转移和原因
multica flow show <KEY> --diagram  # mermaid 状态图，高亮当前状态
multica flow log <KEY>             # 完整转移日志
multica flow accept <KEY>          # 验收通过 → 合并 → done
multica flow reject <KEY> --reason "按钮错位"   # 退回 owner，原因写进 blocked_reason
multica flow unblock <KEY> [--owner at-dev-b] [--reason ...]
multica flow cancel <KEY>          # blocked → cancelled
multica flow replay <KEY>          # 清 backoff，立即重新评估
multica team pause <project> --reason ...   # 急停
multica team resume <project>
```

直接改状态会被拒绝：

```
$ multica issue update ATT-37 --status done
refused by the orchestrator (code orchestrator_owned): ...
```

确实要手工干预时用 `--force`（仅成员），reconciler 之后可能把它收敛回 phase 对应的状态（T9）。
更好的方式是先 `team pause`。

## 4. 通知

都进原生 inbox，类型 `orchestrator_alert`：blocked、dead-letter（连续失败 5 次）、待验收、
blocked 超过 `blocked_remind_min`（默认 60 分钟）的提醒。

- **工作区范围（硬规则）**：所有搜索/读写只在本次 run 的工作目录（git worktree）里。用 `rg <pattern> .` 或 `git grep`，禁止对 `$HOME`、`/`、`/Users` 或父目录做递归 grep/find/rg。issue 需要的信息只从 `multica issue get`、评论、metadata（`blocked_reason`、`acceptance`）和 `multica flow show` 获取；找不到就 blocked，不要在磁盘上翻找。
