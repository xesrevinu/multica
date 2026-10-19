# GitHub App：生产配置核对与本地 e2e 接入

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
