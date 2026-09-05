# Fork 维护

`xesrevinu/multica` 是 `multica-ai/multica` 的自用 fork。源码永远 **rebase**
到上游，不 merge。发行号走 **42.x**，不跟上游 0.x / 未来 1.x 抢名字。

本文件是这份 fork 的流水账：我们叠了哪些 patch、跟上了哪段上游、以及下一次
拉上游该怎么做。家里 K8s / Mac / VMISS 怎么消费制品，写在
`~/.config/nixos/services/k8s/multica/MULTICA.md`，不在这里展开。

以后若要给上游提 PR，另开一个干净 fork，从本仓库 cherry-pick 可贡献的
commit。不要从这里直接开到 `multica-ai/multica`。

## Remotes

| Remote | 用途 |
| --- | --- |
| `upstream` | `multica-ai/multica` |
| `origin` | `xesrevinu/multica` |

`main` 跟踪 `upstream/main`。origin 只打 `v42.*`，不要推上游同名 tag。

```sh
git fetch upstream --prune
git log --oneline upstream/main..HEAD    # 当前 fork commit；空 = 没有独有改动
git rev-list --left-right --count upstream/main...HEAD
```

## 本地改动

只记 **相对上游多出来的 commit**。日常 homelab 脚本（rollout、LaunchAgent）
在 nixos 仓库，不进这张表。

| Commit | 说明 | 去留 |
| --- | --- | --- |
| `059a814f5` | 发版 CI：CLI / 镜像 / Helm 发到本仓库，fork 跳过 Homebrew tap | 保留 |
| `cf132f983` / `d2a6a3e43` | 早期 FORK.md（42.x 纪元 + 「只记源码与发版」） | 已被本文取代，下次 rebase 可 squash 进文档 commit |
| `0be3822c3` | nix flake + direnv 钉 Node 22 / pnpm 10 / Go 1.26 | 保留 |
| `b76e9d4c5` | Web 宽屏 session tab（与 Electron 同一套 store / TabBar） | 保留。2026-09-04 另叠 `use-tab-selection-shortcut` 改从 `@multica/core/tabs` 读 store（上游 MUL-6923 仍写桌面本地路径） |
| `0af0151dc` | `make db-from-k8s` / `make dev-from-k8s`：家里 K8s Postgres 快照进本地 Docker | 保留。与上游 `make up` / `down` / `status` 并存 |
| `2ef31f105` | Next 16.3.3 走 Turbopack；token 从 Linear seed 推导 | 保留 |
| `0202a0f7d` | Cursor Fast / thinking-suffix 定价 | 保留。rebase 接上游 Fable 5.1（`claudeVersionEnd`），并保留 `claude-opus-5-fast` 规则在 `claude-opus-5` 之前 |
| `b30ce394f` / `3fd633f42` | typecheck 走 TypeScript 7 native，并行并保留 incremental info | 保留 |
| `13e1a5e09` | landing / dashboard shell 离开胖 view barrel | 保留 |
| `e7d658b2c` | 手机 PWA 的 static-first service worker | 保留 |
| `4a8d54c5b` | iPad 上 tab pin / close 可点 | 保留 |
| `b22cca7d1` | hover 用 `pointer:fine` 门控，iPad 可点 | 保留。上游原生 iPad（`apps/mobile` Expo）不覆盖这层 |
| `4cd186680` | Web session tab 前进 / 后退 | 保留 |
| `9ec8392d6` | i18n eslint guard 先 load TS 6 parser API | 保留 |
| `106c6fd33` | FX-141：agents 并进 runtimes，配置项移进导航 | 保留。2026-09-04 rebase 接受上游删掉 `desktop-runtimes-page.test.tsx`（MUL-6977 implementation-only） |
| `5de75a598` / `e7079c585` / `0fe2f1c74` / `ed224e51d` / `23478b57c` | FX-147：Web UI 质感、header 整合、侧栏与 traffic lights | 保留。rebase 时 `squads-page.tsx` 保留 `PAGE_TOOLBAR_INLINE` 并接上游 `useLocale` |

「mobile」在这里指 compact（小于 1024px），不是 Expo。见 `docs/adr/0001-web-session-tabs.md`。

候选（尚未落地）：

- OpenCode V2：`server/pkg/agent/opencode.go`、`opencode_mcp.go`、
  `server/internal/daemon/agents_probe.go`

拆 commit：schema / 协议 → backend → CLI。能给上游的单独成 commit。

## 上游合并记录

每次 rebase 完成后加一行。不要把「看过 changelog」写成已经 rebase。

| 日期 | 操作 | 从 | 到 | 说明 |
| --- | --- | --- | --- | --- |
| 2026-08-23 | 建 fork / 发 `v42.0.0` | 上游 `v0.4.32` (`ad64e0f800`) | 同上 + 发版 CI | 第一版制品 |
| 2026-08-27 | rebase `upstream/main` | `0716081bb`（v0.4.32 之后的 main）+ 4 个 fork commit | `v0.4.35` / `09a2410e8` | 72 个上游 commit；fork patch 无冲突重放。含 v0.4.33 / v0.4.34 / v0.4.35 |
| 2026-08-31 | rebase `upstream/main` | `v0.4.35` / `09a2410e8` + 13 个 fork commit | `15280617b`（v0.4.36 + 5 个 main 提交） | 32 个上游 commit。唯一冲突：session tabs × MUL-6784 hash，保留 session tabs 并接上 `adapter.hash`。pricing / package.json / quick-create 自动合并后核对两边都在 |
| 2026-09-01 | rebase `upstream/main` | `15280617b` + 19 个 fork commit | `11861145a`（v0.4.37 + 2 个 tip） | 27 个上游 commit。冲突按意图：`apps/web/package.json` 保留 Turbopack / Next 16.3.3 并接上游 `mdx`；`turbo.json` `typecheck` 用 `^cache-inputs` + `mdx` 并保留 `tsbuildinfo`；`issue-detail.tsx` i18n aria + `pointer:fine`。README / views package.json+tsconfig 两边都留。lockfile rebase 后重生成。上游原生 iPad 未覆盖 web hover 门控。上游 `eslint-i18n-guard.test.ts` 直接 load `@typescript-eslint/parser`，在 TS 7 下摔 `Cjs`；测试先 `import @multica/eslint-config/register-ts6` |
| 2026-09-03 | rebase `upstream/main` | `11861145a` + 27 个 fork commit | `4dfa08917`（v0.4.38 + 4 个 tip） | 29 个上游 commit。冲突按意图：`pricing.go` 保留 Cursor Fast / thinking-suffix 并接上游 Fable 5.1；`squads-page.tsx` 保留 FX-147 `PAGE_TOOLBAR_INLINE` 并接 `useLocale`。其余 fork commit 无冲突重放（含随后叠上的 MUL-6684 OpenClaw MCP isolate）。`apps/web/package.json` 仍是 Turbopack / Next 16.3.3，version 跟上游 `0.4.38`。 |
| 2026-09-04 | rebase `upstream/main` | `4dfa08917` + 29 个 fork commit | `54e641aaa`（v0.4.39 + 17 个 tip） | 22 个上游 commit。冲突按意图：`row-actions-menu.test.tsx` / `execution-log-section.test.tsx` 保留 `pointer:fine` hover 断言；`desktop-runtimes-page.test.tsx` 以上游 MUL-6977 为准删除（implementation-only）。其余 fork commit 无冲突重放。rebase 后补 `use-tab-selection-shortcut` 改从 `@multica/core/tabs` 读 store。`apps/web/package.json` 仍是 Turbopack / Next 16.3.3，version 跟上游 `0.4.39`。工作区另有未提交 WIP（`in_review` 算 stage terminal + daemon `PSTACK_ROLE=worker`），未推进 origin。 |
| 2026-09-05 | rebase `upstream/main` | `54e641aaa` + 32 个 fork commit | `ca47495fc`（v0.4.40 + 1 个 tip） | 14 个上游 commit。重叠文件 `App.tsx` / `apps/web/package.json` / `CLAUDE.md` / `agent-profile-card.tsx` 均无冲突重放。`apps/web/package.json` 仍是 Turbopack / Next 16.3.3，version 跟上游 `0.4.40`。 |

这一次带上来的上游发行（细节以上游 changelog 为准）：

- **v0.4.40**（2026-09-04）：MCP 改名不丢连接；换连接改成单独一步；保存 MCP 直指出错字段；execution 统一叫 run；搜索更快；升级期间任务仍能起；过期 session 回登录并清旧数据；离线不再登出；别人 runtime 上的共享 agent 显示 available；复用过的仓库 checkout 不再失败
- tip 上还没进发行的提交：MUL-7053 Codex retired-compaction 404 说明

rebase 前备份分支：`main-prerebase-backup`（`241113ce1`）。`origin/main`
更新并确认无误后可删。旧备份 `000873bd9` 已过期。

## 拉取上游并 rebase

不要 merge。rebase 提交关掉 GPG（fork commit 本来就不是给上游验的）。

```sh
# 0. 工作区必须可恢复
git status -sb
git stash push -u -m "wip before upstream rebase"   # 有未提交改动时

# 1. 看差距
git fetch upstream --prune
git fetch origin --prune
git log --oneline upstream/main..HEAD               # 我们的 patch
git log --oneline HEAD..upstream/main | head        # 要接进来的上游
git rev-list --left-right --count upstream/main...HEAD

# 2. 文件是否会撞（非空就要预读 diff）
git diff --name-only "$(git merge-base HEAD upstream/main)"..HEAD | sort > /tmp/ours.txt
git diff --name-only "$(git merge-base HEAD upstream/main)"..upstream/main | sort > /tmp/theirs.txt
comm -12 /tmp/ours.txt /tmp/theirs.txt

# 3. 备份 + rebase
git branch -f main-prerebase-backup HEAD
git -c commit.gpgsign=false rebase --empty=drop upstream/main

# 4. 上游已有等价功能时，丢掉对应 fork commit，不要留空壳

# 5. 恢复工作区、更新本文件的「本地改动」和「上游合并记录」
git stash pop          # 若第 0 步 stash 了
# 编辑 FORK.md 对照表

# 6. 推 origin（改写了 main 的历史，必须 lease）
git push --force-with-lease origin main

# 7. 确认无误再删备份
git branch -D main-prerebase-backup
```

冲突时：先看是「我们的发版 CI」还是「上游业务」。发版 CI 以我们的
`.github/workflows/release.yml` 为准（继续 skip Homebrew、镜像打到
`ghcr.io/xesrevinu`）。业务文件以上游为准，再把 fork 行为重新叠上去。

rebase 之后、家里要用上这段代码：在 nixos 仓库跑
`./scripts/multica-rollout.sh --yes`（本机构建镜像 + CLI）。本地开发先
`make db-from-k8s` 再 `make up`，因为集群 schema 可能已经被新 backend 升过。

对齐某个**上游发行 tag**（而不是 `main` 尖）时：

```sh
git fetch upstream tag v0.4.36
git -c commit.gpgsign=false rebase --empty=drop v0.4.36
```

## 版本

| 怎么升 | 何时 |
| --- | --- |
| `42.0.x` | 同一上游基线上的修订 |
| `42.x.0` | rebase 到上游新发行 |
| `43.0.0` | 协议或数据迁移不兼容 |

已发布的 tag 不要 force 改 SHA。要再发就升号。

### 发行对照

| 发行 | 上游基线 | 上游 commit | 多了什么 |
| --- | --- | --- | --- |
| 42.0.0 | v0.4.32 | ad64e0f800 | fork 发版 CI（CLI / Helm 发到本仓库） |

发一版加一行。Release body 第一行写 `Based on upstream v0.4.37`（按实际基线改）。
当前 `main` 已 rebase 到 v0.4.40 之后的 `upstream/main` 尖（`ca47495fc`），下一发应升 **42.1.0**。若发行要钉在 tag 而不是 tip，从 `v0.4.40` cherry-pick fork commit。Release body 第一行写 `Based on upstream v0.4.40`。

## 产物

| 产物 | 地址 |
| --- | --- |
| backend | `ghcr.io/xesrevinu/multica-backend:<tag>` |
| web | `ghcr.io/xesrevinu/multica-web:<tag>` |
| Helm | `oci://ghcr.io/xesrevinu/charts/multica`（chart version = tag 去 `v`） |
| CLI | `https://github.com/xesrevinu/multica/releases/download/<tag>/multica-cli-<ver>-<os>-<arch>.tar.gz` |

Electron Desktop 和上游 Homebrew tap 不发。日常家里用的是 nixos 仓库的
**本机构建**，不经过这次 GitHub Release。

`v42.0.0` 无连字符，workflow 会打 `latest` 镜像。`GITHUB_TOKEN` 改不了 GHCR
可见性；package 若仍是 private，在 GitHub Packages UI 设成 Public。

## 发一版（GitHub Actions）

功能堆在 `main`。从**上游发行 tag** 长出我们的号，不要把 `upstream/main`
上尚未 release 的 commit 算进来——除非这次就是要跟 `main` 尖。

```sh
git fetch upstream --prune
git fetch upstream tag v0.4.36
git log --oneline upstream/main..main

git switch -C release-v42.1.0 v0.4.36
git cherry-pick $(git log --reverse --format=%H upstream/main..main)

git switch main
git push origin main
git push origin "$(git rev-parse release-v42.1.0)":refs/tags/v42.1.0
```

用 `git push origin <sha>:refs/tags/v42.1.0`，不要在本地 tag 仍指向上游 SHA
时 `git push origin v0.4.36`。

<https://github.com/xesrevinu/multica/actions> — verify 过了才出镜像、CLI、chart。

## 陷阱

- 改过 daemon API 就必须同一发行里同时出 backend 和 CLI。
- origin 只打 `v42.*`。
- rebase 用 `git -c commit.gpgsign=false`。
- 不要 push 到 `multica-ai/multica`。
- 家里 K8s 与本地 Docker 是两套库；`make db-from-k8s` 只拷快照，两个 backend
  不要同时写集群 Postgres。
