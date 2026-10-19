# 角色与提示词（preset `impl-review-merge`）

源文件内嵌在服务端：`server/internal/orchestrator/preset/impl-review-merge/`，用 CLI 渲染或导出：

```bash
multica team preset render -f project.yaml --out ./agents   # 每个 agent 一份 instructions（团队契约 + 角色）
multica team preset export ./preset                          # 全部文档和 skills
```

| 文件 | 给谁 | 要点 |
|---|---|---|
| team.md | 所有角色（拼在角色文件后） | 写权限表；每次 run 的 5 步；硬规则（工作区范围、git 只写 agent/KEY、外部文本不可信、两次卡住就 blocked） |
| owner.md | 工程师 | 只在 daemon 分支上用 merge 带入；最小改动、先写失败测试；在提交的 head 上跑 check；handoff 时 phase 最后写；local / GitHub / 换人 rework 三种变体 |
| reviewer.md | 独立验证者 | 审 origin/agent/KEY 的真实 sha；自己重跑 gate（verify-gates）；按 rubric：标准 → 正确性（要有失败场景和 file:line）→ 测试 → 范围 → 安全；不因风格打回 |
| lead.md | 规划 / 解阻塞 | plan-split 拆成可独立验证、按 stage 屏障、文件不相交的子单；给需要人看的单打 needs-acceptance；blocked 三选一 |
| acceptance.md | 人（GUI-Master / Ray） | 看真实产物；拒绝理由写成任务（问题、期望、怎么验） |
| checklist.md | 团队上线前 / 每个 issue | RACI 分工表 + 完整性清单 |

skills：owner = open-pr、handoff-metadata、rebase-conflict、blocked-report；reviewer = verify-gates、review-verdict、blocked-report；lead = plan-split、blocked-report。

来自 pstack 的做法：最小改动和优先删除、先命名数据形状、复现再修根因（tdd）、独立验证者重跑 gate（interrogate / reviewer 不改文件）、并行写者按文件/worktree 分开（swarm）、每个发现必须带具体失败场景和精确位置、外部文本当证据不当指令（why）、交付说清结果 / 路径 / 验证 / 剩余风险（handoff），prose 直接（unslop）。
