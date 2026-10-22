-- Issue status catalog (MUL-6243). Each workspace holds the 7 built-in
-- statuses plus custom ones, all stored in four lifecycle categories.

-- name: SeedIssueStatusEntries :exec
-- Idempotent seed of the 7 built-ins. Safe to call concurrently from multiple
-- pods during a rolling deploy: the unique (workspace_id, key) index makes a
-- losing racer a no-op rather than an error. Initial positions are 0; the list's
-- built-in tiebreak preserves the seed order until an admin reorders the group.
INSERT INTO issue_status (id, workspace_id, key, name, description, category, color, is_system, position)
VALUES
    (gen_random_uuid(), sqlc.arg('workspace_id'), 'backlog', 'Backlog', 'Parked. Assigning an issue here never starts an agent run.', 'unstarted', '#6b7280', 1, 0),
    (gen_random_uuid(), sqlc.arg('workspace_id'), 'todo', 'Todo', 'Queued for work. Moving an issue here starts the assigned agent.', 'unstarted', '#6b7280', 1, 0),
    (gen_random_uuid(), sqlc.arg('workspace_id'), 'in_progress', 'In Progress', 'Actively being worked on.', 'started', '#f59e0b', 1, 0),
    (gen_random_uuid(), sqlc.arg('workspace_id'), 'in_review', 'In Review', 'Work delivered, waiting on human review. Finalizes the autopilot run.', 'started', '#22c55e', 1, 0),
    (gen_random_uuid(), sqlc.arg('workspace_id'), 'done', 'Done', 'Completed.', 'done', '#3b82f6', 1, 0),
    (gen_random_uuid(), sqlc.arg('workspace_id'), 'blocked', 'Blocked', 'Stalled on an external dependency.', 'started', '#ef4444', 1, 0),
    (gen_random_uuid(), sqlc.arg('workspace_id'), 'cancelled', 'Cancelled', 'Decided not to do.', 'closed', '#6b7280', 1, 0)
ON CONFLICT (workspace_id, key) DO NOTHING;

-- name: ListIssueStatusEntries :many
-- Position orders both built-in and custom rows inside each lifecycle group.
-- Built-in order is only a tiebreak for the original seeded positions.
SELECT * FROM issue_status
WHERE workspace_id = sqlc.arg('workspace_id')
  AND (sqlc.arg('include_archived') OR archived_at IS NULL)
ORDER BY
    CASE category WHEN 'unstarted' THEN 0 WHEN 'started' THEN 1 WHEN 'done' THEN 2 WHEN 'closed' THEN 3 ELSE 4 END,
    position,
	CASE WHEN is_system THEN 0 ELSE 1 END,
	CASE key
		WHEN 'backlog' THEN 0
		WHEN 'todo' THEN 1
		WHEN 'in_progress' THEN 2
		WHEN 'in_review' THEN 3
		WHEN 'blocked' THEN 4
		WHEN 'done' THEN 5
		WHEN 'cancelled' THEN 6
		ELSE 7
	END,
    key;

-- name: GetIssueStatusEntryByKey :one
SELECT * FROM issue_status
WHERE workspace_id = sqlc.arg('workspace_id')
  AND key = sqlc.arg('key');

-- name: GetIssueStatusEntryByID :one
SELECT * FROM issue_status
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id');

-- name: CreateIssueStatusEntry :one
-- Custom statuses only: is_system is never set here, so the canonical-key and
-- non-archivable CHECK constraints can only ever apply to seeded rows.
INSERT INTO issue_status (workspace_id, key, name, description, category, color, icon, position)
VALUES (
    sqlc.arg('workspace_id'),
    sqlc.arg('key'),
    sqlc.arg('name'),
    sqlc.arg('description'),
    sqlc.arg('category'),
    sqlc.arg('color'),
    sqlc.arg('icon'),
    COALESCE(
        (SELECT MAX(position) + 1 FROM issue_status
         WHERE workspace_id = sqlc.arg('workspace_id')
		   AND category = sqlc.arg('category')),
        0
    )
)
RETURNING *;

-- name: UpdateIssueStatusEntry :one
-- key and category are absent on purpose: both are immutable after create.
-- The is_system guard is what enforces "built-in name and color are locked" at
-- the storage layer, so a handler bug cannot rename a built-in.
UPDATE issue_status SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    color = COALESCE(sqlc.narg('color'), color),
    icon = COALESCE(sqlc.narg('icon'), icon),
    position = COALESCE(sqlc.narg('position'), position),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
  AND is_system = FALSE
  AND archived_at IS NULL
RETURNING *;

-- name: ArchiveIssueStatusEntry :one
-- Built-ins are excluded by is_system: archiving one would delete its
-- category's behavior definition.
--
-- Archiving retires a status from future use only. Issues already on it keep
-- it - Effective ignores archived_at, so their behavior is unchanged - while
-- Resolve rejects it, so nothing new can be assigned to it.
UPDATE issue_status SET
    archived_at = now(),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
  AND is_system = FALSE
  AND archived_at IS NULL
RETURNING *;

-- name: CountIssuesUsingStatusKey :one
-- Archive precondition, including terminal issues. Call under the catalog lock
-- so a concurrent status assignment cannot invalidate the empty check.
SELECT COUNT(*) FROM issue
WHERE workspace_id = sqlc.arg('workspace_id')
  AND status = sqlc.arg('key');

-- name: DeleteIssueStatusEntriesForWorkspace :exec
-- No foreign keys by project rule, so workspace teardown cleans up here.
DELETE FROM issue_status WHERE workspace_id = sqlc.arg('workspace_id');

-- name: LockIssueStatusCatalog :exec
-- EXCLUSIVE transaction lock over a workspace's status catalog, held by archive
-- so no issue can be written onto a status between the in-use census and the
SELECT 1;

-- name: LockIssueStatusCatalogShared :exec
-- SHARED counterpart, taken by a write that puts an issue ON a custom status.
-- Shared holders do not block each other, so concurrent issue writes keep their
-- current throughput; they only block against archive's exclusive lock.
--
-- Built-in statuses deliberately do NOT take this lock: a built-in can never be
-- archived (enforced by issue_status_system_not_archivable), so there is nothing
-- to race with, and the overwhelmingly common write path stays lock-free.
SELECT 1;

-- name: ListIssueStatusKeysByCategories :many
-- Expands a set of categories to the status keys that belong to them, so a
-- category filter can be applied as an INDEXED `status IN (SELECT value FROM json_each(...))` predicate
-- instead of wrapping the column in issue_effective_status(), which makes
-- (workspace_id, status) unusable and forces a full workspace scan.
--
-- ARCHIVED statuses are included on purpose: archiving retires a status from
-- future assignment but leaves existing issues on it, and those issues must
-- still appear in their category's column.
SELECT key FROM issue_status
WHERE workspace_id = sqlc.arg('workspace_id')
  AND category IN (SELECT value FROM json_each(sqlc.arg('categories')));

-- name: ListActiveCustomIssueStatusEntries :many
-- One category's ACTIVE custom statuses - the exact set a reorder must cover.
-- Read inside the reorder transaction, under the catalog lock, so a status
-- archived concurrently cannot slip in or out between validation and write.
SELECT * FROM issue_status
WHERE workspace_id = sqlc.arg('workspace_id')
  AND category = sqlc.arg('category')
  AND is_system = FALSE
  AND archived_at IS NULL
ORDER BY position, key;

-- name: ReorderIssueStatusEntries :execrows
-- Atomic intra-category reorder. One statement, so a failure leaves the whole
-- order untouched instead of the partially-applied prefix a per-row PATCH loop
-- produces.
--
-- Full-catalog callers may reorder built-ins, without changing their semantics.
-- Legacy custom-only callers preserve the positions occupied by custom rows.
-- Archived rows remain frozen.
-- JSON shape: ids and positions are JSON arrays aligned by index.
-- ids: ["status-id", ...], positions: [1, 2, ...].
UPDATE issue_status
SET position = (
        SELECT p.value
        FROM json_each(sqlc.arg('positions')) AS p
        JOIN json_each(sqlc.arg('ids')) AS v ON v.key = p.key
        WHERE v.value = issue_status.id
    ),
    updated_at = now()
WHERE issue_status.workspace_id = sqlc.arg('workspace_id')
  AND issue_status.id IN (SELECT value FROM json_each(sqlc.arg('ids')))
  AND (sqlc.arg('include_system') OR issue_status.is_system = FALSE)
  AND issue_status.archived_at IS NULL;
