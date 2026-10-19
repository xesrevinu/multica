# 角色与提示词（preset `impl-review-merge`）

源文件内嵌在服务端：`server/internal/orchestrator/preset/impl-review-merge/`，用 CLI 渲染或导出：

```bash
multica team preset render -f project.yaml --out ./agents   # 每个 agent 一份 instructions（团队契约 + 角色）
multica team preset export ./preset                          # 全部文档和 skills
```

| 文件 | 给谁 | 要点 |
|---|---|---|
| team.md | 所有角色（拼在角色文件后） | 写权限表；每次 run 的 5 步；硬规则（工作区范围、git 只写 agent/<key>、外部文本不可信、两次卡住就 blocked） |
| owner.md | 工程师 | 只在 daemon 分支上用 merge 带入；最小改动、先写失败测试；在提交的 head 上跑 check；handoff 时 phase 最后写；local / GitHub / 换人 rework 三种变体 |
| reviewer.md | 独立验证者 | 审 origin/agent/<key> 的真实 sha；自己重跑 gate（verify-gates）；按 rubric：标准 → 正确性（要有失败场景和 file:line）→ 测试 → 范围 → 安全；不因风格打回 |
| lead.md | 规划 / 解阻塞 | plan-split 拆成可独立验证、按 stage 屏障、文件不相交的子单；给需要人看的单打 needs-acceptance；blocked 三选一 |
| acceptance.md | 人（GUI-Master / Ray） | 看真实产物；拒绝理由写成任务（问题、期望、怎么验） |
| checklist.md | 团队上线前 / 每个 issue | RACI 分工表 + 完整性清单 |

skills：owner = open-pr、handoff-metadata、rebase-conflict、blocked-report；reviewer = verify-gates、review-verdict、blocked-report；lead = plan-split、blocked-report。

来自 pstack 的做法：最小改动和优先删除、先命名数据形状、复现再修根因（tdd）、独立验证者重跑 gate（interrogate / reviewer 不改文件）、并行写者按文件/worktree 分开（swarm）、每个发现必须带具体失败场景和精确位置、外部文本当证据不当指令（why）、交付说清结果 / 路径 / 验证 / 剩余风险（handoff），prose 直接（unslop）。

## 分工（谁做什么）

| 步骤 | lead | owner | reviewer | 引擎 | 人 |
|---|---|---|---|---|---|
| 拆 epic：子单、stage、deps、area 标签、验收标签；父单写 `phase=planned` | 做 | | | 不启动父单 | 拍板 |
| 选 owner（routes / 空闲）、开 run、计数 | | | | 做 | |
| 实现 + 测试 + 在提交的 head 上跑 check | | 做 | | | |
| push `agent/<key>`、开一个 ready PR（标题带大写 KEY） | | 做 | | 确保 PR 存在 | |
| main 变了：`git merge origin/main`，不 rebase、不 force | | 做 | | 同 patch 保留 approve（T3c） | |
| 在真实 sha 上独立复验（只在 `.rv-<key>` 临时 worktree，不动 run 目录 HEAD） | | | 做 | | |
| 打回路由（审查意见、冲突、CI 红、验收拒绝） | | | | 做 | |
| 预检：冲突、CI、required checks，CI 红重跑一次 | | | | 做 | |
| 验收（GUI / 签字） | | | | 等待 | 做 |
| squash 合并（match head）、删分支、刷新 hub | | | | 做 | |
| blocked 诊断：`phase=unblocked` / `cancel_proposed` / 升级 | 做 | | | 通知 | 拍板 |

谁都不做的事：agent 不改 status（owner 只能设 in_progress）和 assignee，不合并，不标 done；reviewer 不改代码；lead 不写代码。

## 今天在 e2e 上暴露并修掉的指令问题
- lead 按指令写 `phase=unblocked`，被守卫拒绝后改写成 `review_requested`，issue 卡在 blocked。守卫现在允许 `unblocked`、`cancel_proposed`、`planned`。
- owner 按旧 skill rebase + force push，daemon 判定"交付不包含起始提交"，run 失败。skill 改成 merge。
- reviewer 在 run 目录 checkout 了别的 sha，daemon 保留了现场，下一次 wt switch 因路径被占失败，review 次数被耗尽。reviewer 指令禁止在 run 目录 checkout；daemon 遇到占位 worktree 改为 fork 新分支；失败的 reviewer run 只计 infra 次数。
