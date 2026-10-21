# GitHub App：生产配置核对与本地 e2e 接入

## 服务端 App 客户端：`server/internal/github/app`（2026-10-19）
服务端所有以 App 身份访问 GitHub 的代码共用这一个客户端。引擎（`orchestrator/repo.go`）、PR 卡片操作（`ghops.go`）和 PR 快照管线（`ghsnapshot`）都走它，**服务端不再调用 `gh` CLI**。agent 在自己 worktree 里用 git/gh 不受影响。

```go
import "github.com/multica-ai/multica/server/internal/github/app"

c, err := app.Shared()        // 进程内单例，读 GITHUB_APP_ID / GITHUB_APP_PRIVATE_KEY（可选 GITHUB_APP_SLUG、GITHUB_API_URL）
                              // 两者都没配 → (nil, nil)；nil *Client 的 Enabled()==false，所有调用返回 app.ErrDisabled
c.Enabled() bool
inst, err := c.InstallationFor(ctx, owner, repo)              // GET /repos/{o}/{r}/installation，缓存 1h
err = c.Do(ctx, inst, method, "/repos/o/r/...", body, &out)   // REST；GET 自动带 If-None-Match
raw, err := c.GraphQL(ctx, inst, query, vars)
tok, err := c.Token(ctx, inst)                                // 需要自己发请求时用
perms, err := c.Permissions(ctx, inst)                        // {"contents":"write",...}
bot, err := c.Bot(ctx, inst)                                  // bot.Login "<slug>[bot]"、bot.Email()、bot.CoAuthoredBy()
rs := c.Rate(inst)                                            // 最近一次 X-RateLimit-*；rs.Low(now) = 剩余 <10%
// 错误类型：*app.APIError{Status, Message}（app.IsStatus(err, 404)）、*app.RateLimitError{RetryAfter}
```

例如以 App 身份发一条 review verdict 的 commit status（fix/orch-guards 用）：
```go
inst, _ := c.InstallationFor(ctx, owner, repo)
err := c.Do(ctx, inst, http.MethodPost, "/repos/"+owner+"/"+repo+"/statuses/"+sha,
    map[string]any{"state": "success", "context": "multica/review", "description": "approved"}, nil)
```
（需要 App 有 `statuses: write`；ghsnapshot 的 `*ghsnapshot.Client` 可以用 `.App()` 拿到同一个客户端。）

**令牌**：App JWT（RS256，iat 往前 60s，exp 9 分钟）换 installation token。按 installation 缓存，过期前 5 分钟续期；同一 installation 的并发请求合并成一次 mint（singleflight）。遇到 401 会丢弃缓存、重新 mint 并重试一次。私钥和令牌不进日志，也不进错误信息。

**限流**：
- 每个响应都记录 `X-RateLimit-Limit/Remaining/Reset`。已知额度用完且未到 reset 时，直接返回 `RateLimitError`，不再请求 GitHub。
- 403 带 `Remaining: 0` 或 `Retry-After`、"secondary rate limit"，以及 429，都算限流。等待时间按 Retry-After，否则按 Reset，限制在 1s–5m。
- GET 走 ETag 条件请求（LRU 1024 条）。304 不消耗额度，直接回放缓存的 body。
- 引擎遇到限流时 gate 返回 pending，下一轮再试，不算 infra 失败。
- 平时以 webhook 驱动的 ghsnapshot 快照为准：pre-check 优先读 2 分钟内的快照，额度低于 10% 时放宽到 15 分钟。实时读只作兜底，reconciler 的轮询间隔是 60s。

**提交身份**：
- 引擎的 squash merge 走 `PUT /pulls/{n}/merge`，`merge_method=squash`。`commit_title` 就是 PR 标题，不带 "Merge pull request #n"。`commit_message` 由 PR 描述第一段（最多 400 字）、`PR #n` 和 `Co-authored-by: <slug>[bot] <id+<slug>[bot]@users.noreply.github.com>` 组成。合并请求带 head sha 防护，合并后删除分支（404/422 视为已删）。
- agent 提交：daemon 已有 prepare-commit-msg 钩子，会加 `Co-authored-by` trailer。把 daemon 环境的 `MULTICA_CO_AUTHOR_NAME=<slug>[bot]`、`MULTICA_CO_AUTHOR_EMAIL=<id>+<slug>[bot]@users.noreply.github.com` 设成 App bot 身份即可（id 可以用 `c.Bot()` 或 `GET /users/<slug>[bot]` 查到）。

## 生产（K8s namespace `multica`，只读核对）
- `deployment/multica-backend` 通过 `envFrom` 引用 `configmap/multica-config` 和 `secret/multica-secrets`。
- secret 里与 App 相关的键：`GITHUB_APP_ID`、`GITHUB_APP_PRIVATE_KEY`、`GITHUB_APP_SLUG`、`GITHUB_WEBHOOK_SECRET`（值未记录）。configmap 里 `MULTICA_APP_URL=https://multica.home.internal`。
- App 当前只有一个安装：账号 `xesrevinu`，repository_selection = all（覆盖 autoteam-sandbox）。
- 服务端读取方式：`GITHUB_APP_ID` + `GITHUB_APP_PRIVATE_KEY`（PEM 内容）→ ghsnapshot 客户端（PR 快照、checks rollup、mergeable）；`GITHUB_WEBHOOK_SECRET` 校验 `/api/webhooks/github`；`GITHUB_APP_SLUG` 用于安装链接。

## 本地 e2e 接入（只改环境）
1. 从 secret 取出上面 4 个值写入 `.orch-dev/github-app.env`（权限 600，git 忽略），`run-api.sh` 启动时加载。
2. 本地库里把安装绑定到 e2e workspace（等同 `/api/github/setup` 回调写入的那一行）：`github_installation(workspace_id, installation_id=<App 安装 id>, account_login=xesrevinu)`。回调本身需要 GitHub 跳回本地，所以直接写行。
3. webhook：App 的 webhook URL 指向生产，GitHub 推不到本地。用 `server/scripts/ghrelay`：
   - `ghrelay installations` 列安装；
   - `ghrelay relay -repo xesrevinu/autoteam-sandbox -since 90m -every 15s`：用 App JWT 轮询 `GET /app/hook/deliveries`，取每条 delivery 的 payload，只挑 sandbox 仓库（和 installation 事件），用 webhook secret 重新签名后 POST 到本地 `/api/webhooks/github`。
   - 只读 GitHub；不改 App 配置；不对外暴露端口；生产照常收到每一条。

验证（2026-10-19 17:49）：本地 `github_pull_request` 有 #18–#23，`snapshot_fetched_at` 已填（ghsnapshot 通过 App API 拉取），checks rollup = SUCCESS；`issue_pull_request` 把 #21/#22/#23（分支 `agent/att-66/67/65`，小写）和 #18/#20（大写分支）都关联到了对应 issue，linked_by=system。

## 其他接收 webhook 的方式（都需要 Ray 同意，未做）
| 方式 | 做法 | 代价 |
|---|---|---|
| 隧道（cloudflared / ngrok / tailscale funnel / smee.io） | 本地 18899 暴露为公网 URL | App 只有一个 webhook URL：要么改掉生产的（不可接受），要么为本地另建一个 dev App；本地端口暴露到公网 |
| 第二个 dev App | 新建 App 指向隧道 URL，安装到 sandbox | 多一套密钥；生产和本地完全隔离（最干净的长期方案） |
| 轮询 relay（已采用） | 见上 | 延迟 = 轮询间隔（15s）；delivery 列表只保留近 3 天，长时间停机要用 `-since` 补 |

## 事故
写 `github-app.env` 时，我用 `cut -d= -f1` 只想列键名，但私钥 PEM 跨多行，没有 `=` 的行被原样打印，**App 私钥出现在了这次会话的工具输出里**。私钥没有写进仓库或日志文件，但会话记录里有。建议 Ray 在 GitHub App 设置里生成新私钥、更新 K8s secret 和本地 `github-app.env`，再删除旧私钥。
