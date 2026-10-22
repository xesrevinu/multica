# SQLite schema

`sqlite.sql` 由 PostgreSQL `pg_dump --schema-only`（迁移 585，131 张表）收成。单进程、单写者。不重放历史迁移。

普通表 **132** 张（原 131 张，加上 `search_change_counter`）。另有 FTS5 虚表 **`issue_fts`、`comment_fts`、`project_fts`**（`tokenize = 'trigram'`，不使用 `content=`）。

## 类型

| PostgreSQL | SQLite |
| --- | --- |
| uuid、text、jsonb、date、timestamptz、inet | TEXT |
| bool | INTEGER（1/0） |
| bytea | BLOB |
| int、bigint、smallint | INTEGER |
| numeric、double precision | REAL |
| uuid[]、text[] 等数组 | TEXT，存 JSON 数组（`'{}'` 默认写成 `'[]'`） |

`now()` / `clock_timestamp()` 默认值是 `(strftime('%Y-%m-%dT%H:%M:%fZ','now'))`。`gen_random_uuid()` 默认值去掉，由应用插入。表名 `"user"` 加引号。

CHECK 保留，并改写成 SQLite 可执行的形式：`= ANY` / `<> ALL` → `IN` / `NOT IN`；`jsonb_typeof` / `jsonb_array_length` → `json_type` / `json_array_length`；`char_length` → `length`；`octet_length` / `pg_column_size` → `length(CAST(x AS BLOB))`；`~` 正则 → `GLOB`；`timestamp + interval` → `strftime` 修饰符。

## 搜索

- `search_index_change`：`change_xid` 改为 `change_seq INTEGER`。主键仍是 `(entity_type, entity_id)`。索引 `idx_search_index_change_workspace_xid` 改名为 `idx_search_index_change_workspace_seq`。
- `search_change_counter(singleton, n)` 插入 `(1, 0)`。
- `search_index_prune_mark.pruned_through_xid` 改为 `pruned_through_seq INTEGER`。不用 xid / pg_snapshot。
- `issue`、`comment`、`project` 的 AFTER INSERT/UPDATE/DELETE 触发器先 `UPDATE search_change_counter SET n = n + 1`，再把新的 `n` upsert 进 `search_index_change`。
- 同三张表的触发器同步对应 FTS 表（`id`、`workspace_id` UNINDEXED，外加 title/description 或 content）。

## 故意不搬

- **扩展**：`pg_trgm`、`pgcrypto`。Dump 里没有 `pg_bigm` 索引。
- **序列**：`orchestrator_event_id_seq`、`orchestrator_transition_id_seq`（这两列不再 `nextval`）。
- **函数 28 个**（无 PROCEDURE）：唤醒捕获与容量（`capture_*_wakeup`、`capture_issue_wakeup`、`guard_issue_wakeup_capacity`）；task usage 小时汇总（`enqueue_task_usage_hourly_dirty_*`、`prune_task_usage_hourly_dirty`、`rollup_task_usage_hourly*`、`task_usage_hour_bucket`、`task_usage_hourly_rollup_lag_seconds`）；`record_search_index_change`；issue 子事件/状态（`record_issue_child_event`、`issue_effective_status`、`issue_status_category`）；任务与通道（`set_agent_task_comment_thread`、`clear_runtime_mcp_overlay_on_terminal_state`、`comment_thread_root_id`、`lock_task_owner_rows`、`enforce_channel_message_task_context_revision`）；钉钉镜像（`mirror_dingtalk_group_presence_bot_identity`、`mirror_legacy_dingtalk_group_route_presence`）。
- **PG 触发器 23 个**，全部不搬，类别与上面的函数相同（wakeup、usage rollup、search、issue child event、channel context、dingtalk mirror、runtime mcp overlay）。搜索与 FTS 由上面的 SQLite 触发器替代。
- **外键 113 条**（含 `ON DELETE CASCADE`）。关系由应用维护。
- **GIN 索引 5 条**：`idx_issue_metadata_gin`、`idx_issue_properties_gin`、`idx_issue_title_trgm`、`idx_project_title_trgm`、`idx_project_description_trgm`。btree / UNIQUE / 部分索引 / 表达式索引保留（295 条）；`CONCURRENTLY`、`NULLS FIRST/LAST` 去掉。
- Dump 中的 OWNER、`SET`、`\restrict` 不进入脚本。没有 `GRANT` / `COMMENT ON` / `VIEW` / `POLICY`。
