# 仓库瘦身审计（只读，未删任何文件）

统计方法：`git ls-files`，排除图片、字体、lockfile 和 `generated/`，按行数计（含测试）。整个仓库约 **1,417k 行**。
"我们的流程" = server 核心 + CLI + daemon + web + orchestrator。

## 前端 / 应用

| 模块 | 文件 | 行数 | 我们用 | 依赖关系 | 风险 | 建议 |
|---|---:|---:|---|---|---|---|
| apps/web | 192 | 31,012 | 是 | packages/views、core、ui | — | 保留 |
| packages/views | 1,377 | 353,111 | 是（web 的全部页面） | core、ui | — | 保留；内部再剪 billing/dingtalk/lark/slack 视图（约 1.8k） |
| packages/core | 525 | 91,477 | 是 | — | — | 保留（剪掉 billing、composio 等客户端后约 −3k） |
| packages/ui | 101 | 12,698 | 是 | — | — | 保留 |
| packages/plugin-sdk | 6 | 474 | 不确定 | server/pkg/plugincontract | 低 | 随插件体系决定 |
| apps/mobile | 344 | 38,183 | 否 | core 的类型；locales 与 views 平行 | 低（不在 web/server 构建链上） | **候选删除** |
| apps/desktop | 171 | 25,423 | 否（我们用 web + CLI） | core 里有 desktop 分支（freeze-watchdog、paths、cookie adapter） | 低到中（需要清掉 core 里的 desktop 分支） | **候选删除** |
| apps/docs（文档站） | 269 | 34,109 | 否（我们的文档在 docs/） | next.config 里 4747 端口的 rewrite | 低 | **候选删除**，顺带去掉 web 的 docs 代理（dev 下那条 500 就消失） |
| apps/ui-lab | 41 | 9,613 | 否 | ui | 低 | **候选删除** |

## 服务端

| 模块 | 文件 | 行数 | 我们用 | 依赖关系 | 风险 | 建议 |
|---|---:|---:|---|---|---|---|
| internal/handler | 456 | 211,693 | 是 | 全部 | — | 保留；删除下面的集成后会少一部分路由 |
| internal/daemon | 264 | 126,774 | 是 | pkg/agent | — | 保留；随不用的 provider 一起剪 probe 代码 |
| pkg/agent（provider 适配） | 221 | 94,541 | 部分 | daemon | 中 | 保留 pi、opencode、cursor、claude、codex（以及 grok）；**候选删除** hermes、kimi、openclaw、qwen、qwenpaw、antigravity、copilot、codebuddy、kiro、qoder、reasonix、zeroclaw、codearts、omp、mcode、deveco、traecli、dsh、gbot，约 **39k 行**（另有 daemon 里的 probe/测试约 3–5k） |
| internal/service | 148 | 50,689 | 是 | — | — | 保留 |
| internal/orchestrator | 26 | 3,503 | 是（新增） | handler、events、db | — | 保留 |
| integrations/ghsnapshot、vcs | 11 | 3,182 | 是（PR 快照、门禁） | handler | — | 保留 |
| integrations/wecom | 95 | 38,676 | 否 | channel；handler 中 7 处引用 | 中（路由和迁移表要一起处理） | **候选删除** |
| integrations/lark | 83 | 26,334 | 否 | channel；5 处 | 中 | **候选删除** |
| integrations/dingtalk | 76 | 16,215 | 否 | channel；6 处 | 中 | **候选删除** |
| integrations/telegram | 28 | 11,223 | 否 | channel；4 处 | 中 | **候选删除** |
| integrations/slack | 32 | 8,487 | 可选 | channel；9 处 | 中 | 先保留（以后可能通知到 Slack） |
| integrations/channel（IM 框架） | 43 | 13,464 | 只为上面这些 IM | 97 处引用 | 高 | 去掉所有 IM 后才能删；否则保留 |
| integrations/composio + pkg/composio | 18 | 4,422 | 否 | 4 处 | 低 | **候选删除** |
| entitlement、seatcapacity、billing（SaaS 计费） | 13+ | ~3,100 | 否（自托管） | 25 处 | 中（handler 里有调用点，可换成永远允许的 stub） | 候选：先换 stub，再删 |
| selfhosttelemetry、analytics | 15 | 3,300 | 否 | analytics 38 处 | 中 | 换成 no-op 后删 |
| cmd/backfill_*、*backfill 包 | ~20 | ~3,500 | 一次性 | migrate | 低，**但要等确认生产库已经跑过这些 backfill** | 生产验证后删 |
| cmd/maintenance、internal/maintenance | 7 | ~2,000 | 运维 | — | 低 | 保留 |
| e2e（playwright） | 32 | 4,546 | 是（web 测试） | — | — | 保留 |

## 合计估计

| 类别 | 估计删除行数 |
|---|---:|
| 应用：mobile + desktop + docs 站 + ui-lab | ~107k |
| IM 集成：wecom + lark + dingtalk + telegram（保留 slack 和 channel） | ~92k |
| 不用的 agent provider（含 daemon probe） | ~43k |
| composio、计费/席位、遥测、backfill（换 stub 后） | ~14k |
| 前端对应的视图和客户端（billing/IM/composio） | ~5k |
| **合计** | **~260k（约占仓库 18%）** |

## 顺序和风险控制
1. 先删构建链之外的 apps（mobile、ui-lab、docs 站、desktop）：只改 pnpm workspace 和 turbo 配置，web 和 server 不受影响。删 desktop 前要先去掉 core 里的 desktop 分支。
2. provider：一个 provider 一个提交（agent 适配、daemon probe、模型目录、前端选择项）。要先确认生产库里没有 runtime 在用这个 provider（查 `agent_runtime.provider`）。
3. IM 集成：路由、handler、service、views 一起删；对应的数据库表**不要**删迁移，另写新的 down-migration 计划，避免生产库编号冲突。
4. 计费、遥测：先换 stub 并跑完整测试，再删。
5. 每一步都要跑 `make check`（Go 测试、views 和 core 的 typecheck/vitest、e2e 冒烟）。
