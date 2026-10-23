-- Local search index sync (MUL-7754). Web and Desktop copy the issue, comment,
-- and project text of a workspace, then catch up through search_index_change.
-- The cursor is search_change_counter.n (singleton = 1), not a snapshot.

-- name: GetSearchIndexManifest :one
-- Taken before any snapshot page, so every write the pages might miss is newer
-- than this snapshot and reaches the client through the change log. The byte
-- totals let the client reject a workspace too large for its memory budget
-- before downloading it; length reads the stored length, so TOASTed
-- bodies are not decompressed.
SELECT
    (SELECT n FROM search_change_counter WHERE singleton = 1) AS change_seq,
    (SELECT count(*) FROM issue i WHERE i.workspace_id = @workspace_id) AS issue_count,
    (SELECT COALESCE(sum(length(i.title) + COALESCE(length(i.description), 0)), 0)
        FROM issue i WHERE i.workspace_id = @workspace_id) AS issue_bytes,
    (SELECT count(*) FROM comment c
        WHERE c.workspace_id = @workspace_id AND c.deleted_at IS NULL) AS comment_count,
    (SELECT COALESCE(sum(length(c.content)), 0) FROM comment c
        WHERE c.workspace_id = @workspace_id AND c.deleted_at IS NULL) AS comment_bytes,
    (SELECT count(*) FROM project p WHERE p.workspace_id = @workspace_id) AS project_count,
    (SELECT COALESCE(sum(length(p.title) + COALESCE(length(p.description), 0)), 0)
        FROM project p WHERE p.workspace_id = @workspace_id) AS project_bytes;

-- name: GetSearchIndexCurrentSnapshot :one
SELECT n AS change_seq FROM search_change_counter WHERE singleton = 1;

-- name: ListSearchIndexIssuesPage :many
-- Pages by the unique (workspace_id, number) index.
SELECT * FROM issue
WHERE workspace_id = @workspace_id AND number > @after_number
ORDER BY number
LIMIT @page_limit;

-- name: ListSearchIndexIssuesByIDs :many
SELECT * FROM issue
WHERE workspace_id = @workspace_id AND id IN (SELECT value FROM json_each(@ids));

-- name: ListSearchIndexCommentsByIssues :many
SELECT id, issue_id, content, created_at
FROM comment
WHERE workspace_id = @workspace_id
  AND issue_id IN (SELECT value FROM json_each(@issue_ids))
  AND deleted_at IS NULL;

-- name: ListSearchIndexCommentsByIDs :many
-- Tombstoned comments are returned so the caller can report them as deleted.
SELECT id, issue_id, content, created_at, deleted_at
FROM comment
WHERE workspace_id = @workspace_id AND id IN (SELECT value FROM json_each(@ids));

-- name: ListSearchIndexProjects :many
SELECT * FROM project
WHERE workspace_id = @workspace_id
ORDER BY id;

-- name: ListSearchIndexProjectsByIDs :many
SELECT * FROM project
WHERE workspace_id = @workspace_id AND id IN (SELECT value FROM json_each(@ids));

-- name: SearchIndexSinceIsPruned :one
-- True when retention pruning may have removed a change the client has not
-- seen yet: pruned_through_seq >= since.
SELECT EXISTS (
    SELECT 1 FROM search_index_prune_mark
    WHERE singleton = 1
      AND pruned_through_seq >= @since
) AS pruned;

-- name: ListSearchIndexChanges :many
-- Changes with change_seq > since AND change_seq <= target, in keyset order.
SELECT entity_type, entity_id, change_seq
FROM search_index_change
WHERE workspace_id = @workspace_id
  AND change_seq > @since
  AND change_seq <= @target
  AND (change_seq, entity_type, entity_id)
      > (@after_seq, CAST(@after_type AS TEXT), CAST(@after_id AS TEXT))
ORDER BY change_seq, entity_type, entity_id
LIMIT @page_limit;

-- name: PruneSearchIndexChanges :one
-- Deletes one bounded batch of rows older than the cutoff.
-- Order: PruneSearchIndexChanges_Step2, then PruneSearchIndexChanges.
-- Step2 raises the prune mark to the highest change_seq in the batch while
-- those rows still exist. deleted_count is the batch size Step2 observed.
WITH doomed AS (
    SELECT sic.entity_type AS entity_type, sic.entity_id AS entity_id, sic.change_seq AS change_seq
    FROM search_index_change sic
    WHERE sic.changed_at < @cutoff
    ORDER BY sic.changed_at
    LIMIT @batch_size
),
payload AS (
    SELECT json_group_array(entity_type || char(31) || entity_id) AS keys,
           count(*) AS n
    FROM doomed
)
DELETE FROM search_index_change
WHERE entity_type || char(31) || entity_id IN (
    SELECT value FROM json_each((SELECT keys FROM payload))
)
RETURNING (SELECT n FROM payload) AS deleted_count;

-- name: PruneSearchIndexChanges_Step2 :exec
INSERT INTO search_index_prune_mark (singleton, pruned_through_seq)
SELECT 1 AS singleton, batch.mx AS pruned_through_seq
FROM (
    SELECT max(change_seq) AS mx
    FROM (
        SELECT change_seq
        FROM search_index_change
        WHERE changed_at < @cutoff
        ORDER BY changed_at
        LIMIT @batch_size
    ) doomed
) batch
WHERE batch.mx IS NOT NULL
ON CONFLICT (singleton) DO UPDATE
SET pruned_through_seq = max(search_index_prune_mark.pruned_through_seq, excluded.pruned_through_seq);

-- name: DeleteWorkspaceSearchIndexChanges :exec
DELETE FROM search_index_change WHERE workspace_id = ?1;
