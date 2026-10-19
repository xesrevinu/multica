# Web UI 重设计：以 issue 会话为中心

原型：`.orch-dev/proto/index.html`（静态，Ray 已认可）。视觉参考 t3code（`apps/web/src/components`：
`Sidebar.tsx`、`chat/MessagesTimeline.tsx`、`chat/WorkLog.tsx`、`chat/TimelineSystemDivider.tsx`、
`chat/ThreadDetailsPrRow.tsx`、`chat/DiffStatLabel.tsx`、`chat/ComposerSurface.tsx`、`RightPanelTabs.tsx`）。

## 决定（Ray，2026-10-19）
- 数据按 workspace 隔离，不跨 workspace。project 只是 issue 的可选属性；issue 可以没有 project、assignee、metadata。
  UI 和 API 对所有可选字段都要稳健（缺省 = 不显示，而不是报错）。
- 左栏顶部是与 agent 的私聊（复用现有 chat/DM 机制，`/chat?session=` / `?agent=`）；没有选中 issue 时主区就是私聊。
- 左栏 issue 列表按"需要我处理"排序：Needs you → In progress → Up next → Settled（默认折叠）。
  子 issue 在父 issue 下展开，也能作为 tab 打开。原有 board/list/table 视图、projects、settings 等降为次级导航（左栏底部图标条）。
  现有的筛选/排序/置顶继续在左栏可用。
- 右侧保留多 tab 机制（session tabs），以后可以加 Files / Terminal tab。
- Issue 视图是会话：最新在底部，评论线程可回复（现有 comment thread），底部固定输入框（@ 提及、/ 动作）和状态条。
  点标题打开详情抽屉。旧的右侧属性栏移除（信息放到头部和底部）。
- 这个视图里不能随意改派或拖状态。固定动作：Accept、Reject（必须写原因）、Unblock、Cancel、Priority/Risk、Pause，
  全部走 orchestrator 路径。
- GitHub：用 GitHub App（密钥在 multica-secrets / `.orch-dev/github-app.env`，永不打印）。PR 卡显示 checks、commits、
  冲突、merge main into branch、rerun checks、squash merge，按 App 实际权限开关；local 模式隐藏。
- Worktree：wt（worktrunk）优先，路径来自 wt 配置；spec 里重复的路径配置（`disk_path`）变成可选覆盖；UI 显示 worktree。
- 迭代循环内建（teamctl 已弃用）。
- 移动端 PWA：manifest、可安装，布局同原型。

## 后端 API（`/api/orchestrator`，workspace member + human actor）

全部按 `X-Workspace-ID` 过滤。非 orchestrated 的 issue 也能用只读接口（返回 `orchestrated:false`）。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/issues` | 左栏列表：每个 issue 一行摘要，服务端按 attention 排序。字段见下。`?settled_limit=` 默认 30 |
| GET | `/issues/{key}/timeline` | 会话聚合：orchestrator 转移、agent run（开始/结束/时长/消息数）、PR 摘要、验收/verdict/blocked 状态、worktree。评论仍走现有 `/api/issues/{id}/timeline`（保留实时补丁和线程回复），前端按时间合并 |
| GET | `/issues/{key}/pr` | PR 详情（GitHub App 实时拉取）：commits、checks、mergeable、behind_by、`can.{update_branch,rerun,merge}`（来自安装 token 的权限）。local 模式返回 `mode:"local"`，无 PR 操作 |
| POST | `/issues/{key}/pr/update-branch` | 把 main 合进分支（GitHub `PUT /pulls/{n}/update-branch`） |
| POST | `/issues/{key}/pr/rerun-checks` | 重跑失败的 workflow run（`POST /actions/runs/{id}/rerun-failed-jobs`） |
| POST | `/issues/{key}/pr/merge` | squash merge；orchestrated issue 拒绝（合并由引擎在 Accept 后做），返回 409 `engine_owned` |
| POST | `/issues/{key}/props` | `{priority?, risk?}`：priority 写 issue 列，risk 写 metadata（low/medium/high） |
| POST | `/flow/{key}/accept` `/reject` `/unblock` | 现有 |
| POST | `/flow/{key}/cancel` | blocked → `phase=cancel_proposed`（T8）；其他未结束状态 → `cancel_requested=true`（新 T8b：取消当前 run，status=cancelled） |
| POST | `/flow/{key}/pause` `/resume` | 单 issue 暂停：`metadata.paused=true`，引擎对它只等待（当前 run 跑完，不再转移）。team 级暂停照旧 |

`paused`、`cancel_requested` 加入引擎专属 metadata 键（agent 不能写）。

### `/issues` 行
`id, key, title, status, priority, project{id,title}?, parent_id?, assignee{type,id,name}?, labels[], phase?, risk?,
orchestrated, paused, attention{bucket,reason,rank}, attempts{impl,review,infra}?, running{agent,task_id,started_at}?,
pr{number,url,state,additions,deletions,checks,mergeable}?, worktree{path,branch}?, children{total,done}, updated_at`。

attention 规则（纯函数 `attentionOf`，有单测）：
- `needs_you`：phase=awaiting_acceptance；status=blocked；phase=cancel_proposed；非 orchestrated 且 status=in_review 并指派给我。
- `running`：有活跃 run，或 status in_progress / in_review。
- `queued`：todo / backlog。
- `settled`：done / cancelled。
桶内排序：needs_you 按等待时间（最久在前），其余按 priority、updated_at。

## Worktree
- 路径：该 issue 最新 task 的 `durable_work_dir`，否则 `work_dir`（daemon 用 `wt switch` 实际创建的目录）。
- 分支：orchestrated = `branch_prefix + BranchSlug(key)`；否则取关联 PR 的 head 分支。
- 磁盘检查目录：`repo.disk_path` 变成可选；为空时取 wt 配置 `worktree-path` 模板的静态前缀
  （先读 `<hub>/.config/wt.toml`，再读 `~/.config/worktrunk/config.toml`），都没有就用 hub。

## 前端结构
- `packages/core/orchestrator/`：zod schema（`parseWithFallback`）、api、query options、mutations。
- `packages/views/orch/`：
  - `sidebar/`：`AgentDmSection`、`IssueAttentionList`（分组、展开子 issue、PR chip、worktree、耗时）、`SecondaryNav`（底部图标）。
  - `issue-chat/`：`IssueChatView`（头部、时间线、卡片、状态条、输入框）、`TransitionDivider`、`WorkedFor`、`PrCard`、
    `VerdictCard`、`AcceptanceCard`、`FlowActions`、`DetailsDrawer`。
  - 复用：`useIssueTimeline`、`buildCommentRunView`、`CommentCard`（线程回复）、`AgentRunComment`、`CommentInput`、
    现有 pickers、`IssueDetail` 的属性逻辑放进抽屉。
- 路由：`/{ws}/issues/{id}` 渲染 `IssueChatView`；`/{ws}/issues` 保留 board/list/table；工作区默认落地页改为 `/{ws}/chat`。
- i18n：新命名空间 `orch`（en、zh-Hans、fr、ja、ko，parity 测试覆盖）。
- 移动端（compact）：首页 = issue 列表；issue 会话全屏；详情用底部 sheet；底部输入框。manifest 已有，补 `display`、图标、start_url。

## 阶段
1. 后端：`/issues`、`/timeline`、动作（pause/resume/cancel/props）、GitHub App PR 操作、wt 磁盘路径。replay 场景 + 单测。
2. 左栏 + DM。
3. 会话时间线 + 卡片 + 输入框 + 动作。
4. 详情抽屉、tabs、次级导航。
5. 移动端 PWA。

每阶段：`go test ./internal/orchestrator/...`、`pnpm typecheck`、相关 vitest；对本地 e2e（web 13819 / API 18899，
workspace autoteam-test）用 headless Playwright 截图到 `.orch-dev/shots/ui-*.png`。进度记在 PROGRESS.md。

## 不做 / 边界
- 不碰 chore/slim 正在删的区域（desktop、mobile app、额外 IM、plugin-sdk、composio、额外 agent provider）。
- 不 push，不碰生产（K8s ns multica、profile home、`~/.multica/config.json`、生产 daemon、`~/.local/bin/multica`）。
