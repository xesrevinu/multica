-- name: ListIssues :many
-- involves_user_id widens the assignee filter to surface issues where the user
-- is *indirectly* the assignee - via an owned agent or a squad they belong to /
-- lead / have an agent inside. The semantics intentionally exclude direct
-- member assignment (`assignee_type='member' AND assignee_id=involves_user_id`)
-- because that is already the meaning of the `assignee_id` filter (tab 1
-- "Assigned to me"), and the two filters must produce disjoint result sets.
SELECT i.id, i.workspace_id, i.title, i.description, i.status, i.priority,
       i.assignee_type, i.assignee_id, i.creator_type, i.creator_id,
       i.parent_issue_id, i.position, i.start_date, i.due_date, i.created_at, i.updated_at, i.last_activity_at, i.number, i.project_id, i.metadata, i.stage, i.properties,
       i.revision, i.duplicate_of_issue_id
FROM issue i
WHERE i.workspace_id = ?1
  AND (sqlc.narg('status') IS NULL OR i.status = sqlc.narg('status'))
  AND (sqlc.narg('priority') IS NULL OR i.priority = sqlc.narg('priority'))
  AND (sqlc.narg('assignee_id') IS NULL OR i.assignee_id = sqlc.narg('assignee_id'))
  AND (sqlc.narg('assignee_ids') IS NULL OR i.assignee_id IN (SELECT value FROM json_each(sqlc.narg('assignee_ids'))))
  AND (sqlc.narg('creator_id') IS NULL OR i.creator_id = sqlc.narg('creator_id'))
  AND (sqlc.narg('project_id') IS NULL OR i.project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('scheduled') IS NULL OR (i.start_date IS NOT NULL OR i.due_date IS NOT NULL))
  AND (sqlc.narg('metadata_filter') IS NULL OR NOT EXISTS (
      SELECT 1 FROM json_each(sqlc.narg('metadata_filter')) AS mf
      WHERE json_extract(i.metadata, '$.' || mf.key) IS NOT mf.value
  ))
  AND (
    sqlc.narg('involves_user_id') IS NULL
    -- (1) assignee is an agent owned by the user
    OR (i.assignee_type = 'agent' AND i.assignee_id IN (
          SELECT a.id FROM agent a
           WHERE a.workspace_id = ?1
             AND a.owner_id     = sqlc.narg('involves_user_id')
    ))
    -- (2)(3)(4) assignee is a squad related to the user - three relations
    OR (i.assignee_type = 'squad' AND i.assignee_id IN (
          -- (2) the user is a human member of the squad
          SELECT sm.squad_id
            FROM squad_member sm
            JOIN squad s ON s.id = sm.squad_id
           WHERE s.workspace_id = ?1
             AND sm.member_type = 'member'
             AND sm.member_id   = sqlc.narg('involves_user_id')
          UNION
          -- (3) the squad's canonical leader is an agent owned by the user.
          -- We read squad.leader_id directly rather than relying on a
          -- squad_member row, because the leader copy in squad_member is
          -- best-effort (see squad.go AddSquadMember error handling).
          SELECT s.id
            FROM squad s
            JOIN agent a ON a.id = s.leader_id
           WHERE s.workspace_id = ?1
             AND a.workspace_id = ?1
             AND a.owner_id     = sqlc.narg('involves_user_id')
          UNION
          -- (4) the squad has an agent member owned by the user
          SELECT sm.squad_id
            FROM squad_member sm
            JOIN squad s ON s.id = sm.squad_id
            JOIN agent a ON a.id = sm.member_id
           WHERE s.workspace_id = ?1
             AND sm.member_type = 'agent'
             AND a.workspace_id = ?1
             AND a.owner_id     = sqlc.narg('involves_user_id')
    ))
  )
ORDER BY i.position ASC, i.created_at DESC
LIMIT ?2 OFFSET ?3;

-- name: GetIssue :one
SELECT * FROM issue
WHERE id = ?1;

-- name: CountIssuesInTriage :one
-- How many of these issues are in Triage. The batch parent-write guard only
-- needs "any", and a count keeps the check one round trip regardless of size.
SELECT count(*) FROM issue
WHERE workspace_id = sqlc.arg('workspace_id')
  AND id IN (SELECT value FROM json_each(sqlc.arg('issue_ids')))
  AND triage_state IS NOT NULL;

-- name: GetIssueTriageState :one
-- Answers "is this issue in Triage" for the queue door, which runs immediately
-- before an INSERT and must see the status its own transaction wrote. NULL is
-- an ordinary issue.
SELECT triage_state FROM issue WHERE id = ?1;

-- name: GetIssueGCStatus :one
SELECT workspace_id, status, updated_at
FROM issue
WHERE id = ?1;

-- name: ListIssueGCStatuses :many
SELECT id, status, updated_at
FROM issue
WHERE workspace_id = sqlc.arg('workspace_id')
  AND id IN (SELECT value FROM json_each(sqlc.arg('issue_ids')));

-- name: GetIssueInWorkspace :one
SELECT * FROM issue
WHERE id = ?1 AND workspace_id = ?2;

-- name: GetIssueMetadataInWorkspace :one
-- Reloads the committed metadata snapshot after a conditional mutation
-- returns no rows, without fetching the rest of the issue payload.
SELECT metadata, revision FROM issue
WHERE id = ?1 AND workspace_id = ?2;

-- name: LockIssueForChannelMediaBind :one
-- Channel media resolves after /issue creation. Hold a key-share lock while
-- the attachment row is written so a concurrent issue delete cannot land
-- between the workspace-scoped validation and the attachment insert.
SELECT id FROM issue
WHERE id = ?1 AND workspace_id = ?2;

-- name: LockIssueForAttachmentWrite :one
-- Owner-first guard for a write to one of an issue's attachments: take the
-- issue before the attachment row, so a writer that reaches the same row
-- through the issue - teardown's issue_id cascade, or the revision bump this
-- write itself performs - either waits for this transaction or is waited on,
-- never both.
--
--, the mode of that revision bump, is the weakest mode that
-- actually serializes issue writers. is NOT enough: it is
-- compatible with (see LockLiveComment), so a concurrent
-- CreateComment would take the issue anyway, wait on the attachment this
-- transaction holds, and deadlock with its bump.
SELECT id FROM issue
WHERE id = ?1 AND workspace_id = ?2;

-- name: LockIssueForDescriptionUpdate :one
-- Serialize field-baseline checks and combined attachment binding on the
-- owner row. The handler merges channel media that landed after the editor's
-- submitted base, updates the issue, and binds this request's attachments in
-- the same transaction while holding this lock.
SELECT * FROM issue
WHERE id = ?1 AND workspace_id = ?2;

-- name: MaterializeIssueChannelMediaMarkdown :one
-- Detached channel media resolves after /issue creation. When the description
-- still equals the exact creation-time base, replace its inline placeholders
-- with the fully composed Markdown so rich-text ordering survives. If a user
-- edited concurrently (or the adapter has no inline layout), append instead;
-- preserving user-authored bytes takes precedence over layout fidelity.
-- This is asynchronous system materialization, not a new user action, so it
-- intentionally preserves last_activity_at while still advancing revision.
UPDATE issue
SET description = CASE
        WHEN sqlc.narg('base_description') IS NOT NULL
             AND COALESCE(description, '') = sqlc.narg('base_description')
            THEN sqlc.arg('description')
        WHEN description IS NULL OR description = '' THEN sqlc.arg('markdown')
        ELSE description || char(10) || char(10) || sqlc.arg('markdown')
    END,
    revision = revision + 1,
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND workspace_id = sqlc.arg(workspace_id)
RETURNING *;

-- name: LockIssueForDelete :one
-- Issue deletion must collect every attachment URL after it has won the same
-- row-lock race used by channel media binding. conflicts with the
-- binder's: either bind commits first and its URL is collected,
-- or delete commits first and the binder leaves its durable intent for cleanup.
SELECT id FROM issue
WHERE id = ?1 AND workspace_id = ?2;

-- name: DetachDirectChildIssues :many
UPDATE issue
SET parent_issue_id = NULL,
    stage = NULL,
    revision = revision + 1,
    updated_at = now(),
    last_activity_at = GREATEST(COALESCE(last_activity_at, updated_at), now())
WHERE workspace_id = sqlc.arg(workspace_id)
  AND parent_issue_id = sqlc.arg(parent_issue_id)
  AND NOT COALESCE(id IN (SELECT value FROM json_each(sqlc.arg(excluded_issue_ids))), false)
RETURNING *;

-- name: CreateIssue :one
INSERT INTO issue (
    workspace_id, title, description, status, priority,
    assignee_type, assignee_id, creator_type, creator_id,
    parent_issue_id, position, start_date, due_date, number, project_id,
    stage, properties, last_activity_at, id
) VALUES (
    ?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13, ?14, ?15,
    sqlc.narg('stage'), COALESCE(sqlc.narg('properties'), '{}'), now(), COALESCE(sqlc.narg('id'), gen_random_uuid())
) RETURNING *;

-- name: GetIssueByNumber :one
SELECT * FROM issue
WHERE workspace_id = ?1 AND number = ?2;

-- name: UpdateIssue :one
-- source_task_id is referenced so the parameter stays on the query. SQLite has
-- no session GUC, so the Postgres set_config side effect is a no-op compare.
UPDATE issue SET
    title = COALESCE(sqlc.narg('title'), issue.title),
    description = COALESCE(sqlc.narg('description'), issue.description),
    status = COALESCE(sqlc.narg('status'), issue.status),
    priority = COALESCE(sqlc.narg('priority'), issue.priority),
    assignee_type = sqlc.narg('assignee_type'),
    assignee_id = sqlc.narg('assignee_id'),
    position = CASE
        WHEN sqlc.narg('position') IS NOT NULL THEN sqlc.narg('position')
        WHEN issue.status IS NOT COALESCE(sqlc.narg('status'), issue.status) THEN (
            SELECT COALESCE(MIN(target.position), 0) - 1
            FROM issue AS target
            WHERE target.workspace_id = issue.workspace_id
              AND target.status = COALESCE(sqlc.narg('status'), issue.status)
        )
        ELSE issue.position
    END,
    start_date = sqlc.narg('start_date'),
    due_date = sqlc.narg('due_date'),
    parent_issue_id = sqlc.narg('parent_issue_id'),
    project_id = sqlc.narg('project_id'),
    stage = sqlc.narg('stage'),
    duplicate_of_issue_id = CASE
        WHEN sqlc.narg('duplicate_of_issue_id') IS NOT NULL THEN sqlc.narg('duplicate_of_issue_id')
        WHEN issue.status = 'cancelled' AND COALESCE(sqlc.narg('status'), issue.status) = 'cancelled' THEN issue.duplicate_of_issue_id
        ELSE NULL
    END,
    revision = issue.revision + 1,
    last_activity_at = GREATEST(COALESCE(issue.last_activity_at, issue.updated_at), now()),
    updated_at = now()
WHERE issue.id = sqlc.arg('id')
  AND (sqlc.narg('expected_revision') IS NULL OR issue.revision = sqlc.narg('expected_revision'))
  AND COALESCE(sqlc.narg('source_task_id'), '') = COALESCE(sqlc.narg('source_task_id'), '')
RETURNING *;

-- name: UpdateIssueStatus :one
-- Workspace_id in the WHERE clause is a SQL-layer tenant guard.
-- Reposition to the top of the destination status column when the status changes.
-- source_task_id is retained as a parameter; SQLite has no set_config GUC.
UPDATE issue SET
    status = ?2,
    duplicate_of_issue_id = CASE
        WHEN ?2 = 'cancelled' AND issue.status = 'cancelled' THEN issue.duplicate_of_issue_id
        ELSE NULL
    END,
    position = CASE WHEN issue.status IS NOT ?2 THEN (
        SELECT COALESCE(MIN(target.position), 0) - 1
        FROM issue AS target
        WHERE target.workspace_id = issue.workspace_id
          AND target.status = ?2
    ) ELSE issue.position END,
    revision = issue.revision + CASE WHEN issue.status IS NOT ?2 THEN 1 ELSE 0 END,
    last_activity_at = CASE WHEN issue.status IS NOT ?2
        THEN GREATEST(COALESCE(issue.last_activity_at, issue.updated_at), now())
        ELSE issue.last_activity_at
    END,
    updated_at = now()
WHERE issue.id = ?1 AND issue.workspace_id = ?3
  AND COALESCE(sqlc.narg('source_task_id'), '') = COALESCE(sqlc.narg('source_task_id'), '')
RETURNING *;

-- name: LockIssuesForDuplicateMark :many
-- Locks the issue being marked and its target, in id order, before the mark is
-- validated. Two marks that share an issue (A -> B racing B -> A, or C -> A
-- racing A -> B) then run one after the other, so the second one validates
-- against what the first wrote. A target outside the workspace is not returned.
-- is_duplicate applies the same validity rule as the reads below.
SELECT i.id,
       (i.status = 'cancelled' AND EXISTS (
           SELECT 1 FROM issue AS original
           WHERE original.id = i.duplicate_of_issue_id
             AND original.workspace_id = i.workspace_id
       )) AS is_duplicate
FROM issue AS i
WHERE i.workspace_id = sqlc.arg('workspace_id')
  AND i.id IN (SELECT value FROM json_each(sqlc.arg('issue_ids')))
ORDER BY i.id;

-- A mark only counts while the duplicate is cancelled and its original still
-- exists. Writes keep that true, but a server predating this feature (after a
-- rollback that kept the column) can reopen a duplicate or delete an original
-- without touching the pointer, so every read applies the rule itself.

-- name: IssueHasDuplicates :one
SELECT EXISTS (
    SELECT 1 FROM issue
    WHERE workspace_id = sqlc.arg('workspace_id')
      AND duplicate_of_issue_id = sqlc.arg('issue_id')
      AND status = 'cancelled'
) AS has_duplicates;

-- name: ListIssueDuplicates :many
SELECT * FROM issue
WHERE workspace_id = sqlc.arg('workspace_id')
  AND duplicate_of_issue_id = sqlc.arg('issue_id')
  AND status = 'cancelled'
ORDER BY created_at ASC, id ASC;

-- name: GetIssueRefInWorkspace :one
-- The summary a duplicate's response carries for its original (MUL-7349).
SELECT id, number, title, status FROM issue
WHERE id = ?1 AND workspace_id = ?2;

-- name: ListIssueRefsInWorkspace :many
-- GetIssueRefInWorkspace for a page: every original the page's duplicates
-- point at, in one read.
SELECT id, number, title, status FROM issue
WHERE workspace_id = sqlc.arg('workspace_id')
  AND id IN (SELECT value FROM json_each(sqlc.arg('ids')));

-- name: ClearIssueDuplicatesOf :many
-- Deleting an issue clears the pointers of its duplicates, the way deleting a
-- parent detaches its children. They stay cancelled. Issues deleted in the
-- same batch are skipped.
UPDATE issue
SET duplicate_of_issue_id = NULL,
    revision = revision + 1,
    updated_at = now(),
    last_activity_at = GREATEST(COALESCE(last_activity_at, updated_at), now())
WHERE workspace_id = sqlc.arg(workspace_id)
  AND duplicate_of_issue_id = sqlc.arg(issue_id)
  AND NOT COALESCE(id IN (SELECT value FROM json_each(sqlc.arg(excluded_issue_ids))), false)
RETURNING *;

-- name: CreateIssueWithOrigin :one
INSERT INTO issue (
    workspace_id, title, description, status, priority,
    assignee_type, assignee_id, creator_type, creator_id,
    parent_issue_id, position, start_date, due_date, number, project_id,
    origin_type, origin_id, stage, properties, last_activity_at, id
) VALUES (
    ?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13, ?14, ?15,
    sqlc.narg('origin_type'), sqlc.narg('origin_id'), sqlc.narg('stage'), COALESCE(sqlc.narg('properties'), '{}'), now(), COALESCE(sqlc.narg('id'), gen_random_uuid())
) RETURNING *;

-- name: LockIssueDuplicateKey :exec
SELECT 1;

-- name: FindActiveDuplicateIssue :one
SELECT * FROM issue
WHERE workspace_id = ?1
  -- Negate only known terminal keys so an unknown legacy key remains active.
  AND NOT (status IN (SELECT value FROM json_each(sqlc.arg('terminal_status_keys'))))
  -- An entry waiting in Triage has not been taken on, so it never blocks
  -- someone filing the same work; a duplicate there is resolved by merging
  -- it out of Triage (MUL-7189  2.6).
  AND triage_state IS NULL
  AND project_id IS sqlc.arg('project_id')
  AND parent_issue_id IS sqlc.arg('parent_issue_id')
  AND lower(trim(replace(replace(replace(replace(replace(replace(replace(replace(title, char(9), ' '), char(10), ' '), char(13), ' '), char(12), ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '))) = sqlc.arg('normalized_title')
ORDER BY created_at ASC
LIMIT 1;

-- name: FindRecentAutopilotDuplicateIssue :one
SELECT i.* FROM issue i
WHERE i.workspace_id = ?1
  -- Negate only known terminal keys so an unknown legacy key remains active.
  AND NOT (i.status IN (SELECT value FROM json_each(sqlc.arg('terminal_status_keys'))))
  -- An entry waiting in Triage has not been taken on, so it never blocks
  -- someone filing the same work; a duplicate there is resolved by merging
  -- it out of Triage (MUL-7189  2.6).
  AND i.triage_state IS NULL
  AND i.origin_type = 'autopilot'
  AND i.origin_id = ?2
  AND i.project_id IS sqlc.arg('project_id')
  AND lower(trim(replace(replace(replace(replace(replace(replace(replace(replace(i.title, char(9), ' '), char(10), ' '), char(13), ' '), char(12), ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '))) = sqlc.arg('normalized_title')
  AND i.created_at >= sqlc.arg('created_after')
  AND EXISTS (
    SELECT 1
    FROM autopilot_run r
    WHERE r.issue_id = i.id
      AND r.autopilot_id = i.origin_id
      AND r.status IN ('issue_created', 'running', 'completed')
  )
ORDER BY i.created_at ASC
LIMIT 1;

-- name: DeleteIssue :exec
-- Child rows are removed by the following steps. There is no FK cascade.
DELETE FROM issue_wakeup_receipt
WHERE wakeup_id IN (
    SELECT id FROM issue_wakeup
    WHERE issue_id = ?1 AND workspace_id = ?2
);

-- name: DeleteIssue_Step2 :exec
DELETE FROM issue_wakeup
WHERE issue_id = ?1 AND workspace_id = ?2;

-- name: DeleteIssue_Step3 :exec
DELETE FROM issue_child_event
WHERE workspace_id = ?2 AND (parent_id = ?1 OR child_id = ?1);

-- name: DeleteIssue_Step4 :exec
DELETE FROM task_supplement
WHERE issue_id = ?1 AND workspace_id = ?2;

-- name: DeleteIssue_Step5 :exec
DELETE FROM task_supplement_capability
WHERE issue_id = ?1 AND workspace_id = ?2;

-- name: DeleteIssue_Step6 :exec
DELETE FROM issue_vcs_pull_request
WHERE issue_id = ?1
  AND EXISTS (SELECT 1 FROM issue WHERE issue.id = ?1 AND issue.workspace_id = ?2);

-- name: DeleteIssue_Step7 :exec
DELETE FROM issue_pr_automation
WHERE issue_id = ?1 AND workspace_id = ?2;

-- name: DeleteIssue_Step8 :exec
DELETE FROM issue_pull_request_exclusion
WHERE issue_id = ?1 AND workspace_id = ?2;

-- name: DeleteIssue_Step9 :exec
DELETE FROM issue
WHERE id = ?1 AND workspace_id = ?2;

-- name: ListOpenIssues :many
-- See ListIssues for the semantics of involves_user_id (mirrors the 4-branch
-- filter; member-direct assignment is intentionally excluded).
SELECT i.id, i.workspace_id, i.title, i.description, i.status, i.priority,
       i.assignee_type, i.assignee_id, i.creator_type, i.creator_id,
       i.parent_issue_id, i.position, i.start_date, i.due_date, i.created_at, i.updated_at, i.last_activity_at, i.number, i.project_id, i.metadata, i.stage, i.properties,
       i.revision, i.duplicate_of_issue_id
FROM issue i
WHERE i.workspace_id = ?1
  -- Negate only known terminal keys so an unknown legacy key remains visible.
  AND NOT (i.status IN (SELECT value FROM json_each(sqlc.arg('terminal_status_keys'))))
  AND (sqlc.narg('priority') IS NULL OR i.priority = sqlc.narg('priority'))
  AND (sqlc.narg('assignee_id') IS NULL OR i.assignee_id = sqlc.narg('assignee_id'))
  AND (sqlc.narg('assignee_ids') IS NULL OR i.assignee_id IN (SELECT value FROM json_each(sqlc.narg('assignee_ids'))))
  AND (sqlc.narg('creator_id') IS NULL OR i.creator_id = sqlc.narg('creator_id'))
  AND (sqlc.narg('project_id') IS NULL OR i.project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('metadata_filter') IS NULL OR NOT EXISTS (
      SELECT 1 FROM json_each(sqlc.narg('metadata_filter')) AS mf
      WHERE json_extract(i.metadata, '$.' || mf.key) IS NOT mf.value
  ))
  -- properties_filter is a jsonb array of groups, each group an array of
  -- patterns (built by parsePropertiesFilterParam): the issue must match at
  -- least one pattern from EVERY group (AND of ORs). Three pattern shapes:
  --   {"__none__": "<definitionId>"}                         - "no value" marker;
  --   {"<definitionId>": <value>, ...}                        - containment;
  --   {"__op__": "<op>", "def": "<id>", "value": "<v>"}       - scalar operator
  -- (contains / gt / gte / lt / lte / before / after), where the contains
  -- value arrives already LIKE-escaped and the op was validated in Go, so
  -- the per-op branches below only ever see legal values. A contains pattern
  -- additionally carries "prefilter" when its needle survives jsonb text
  -- serialization intact; the clause it enables is redundant by construction
  -- (prefilterableContainsNeedle in property.go) and stays here so both renderings
  -- of the filter keep matching row for row. The correlated form skips the GIN
  -- index, which is fine here: open_only is an unpaginated workspace scan. The
  -- terminal-status predicate intentionally remains a filter rather than a
  -- positive index narrowing so unknown legacy status keys stay visible.
  AND (
    sqlc.narg('properties_filter') IS NULL
    OR NOT EXISTS (
      SELECT 1
      FROM json_each(sqlc.narg('properties_filter')) AS pf
      WHERE NOT EXISTS (
        SELECT 1
        FROM json_each(pf.value) AS alt
        WHERE (json_type(alt.value, '$.__none__') IS NOT NULL AND json_type(i.properties, '$.' || json_extract(alt.value, '$.__none__')) IS NULL)
           OR (json_type(alt.value, '$.__none__') IS NULL AND json_type(alt.value, '$.__op__') IS NULL
               AND NOT EXISTS (
                    SELECT 1 FROM json_each(alt.value) AS kv
                    WHERE json_extract(i.properties, '$.' || kv.key) IS NOT kv.value
               ))
           OR (json_type(alt.value, '$.__op__') IS NOT NULL
               AND json_extract(i.properties, '$.' || json_extract(alt.value, '$.def')) IS NOT NULL
               AND (
                 (json_extract(alt.value, '$.__op__') = 'contains'
                  AND (json_type(alt.value, '$.prefilter') IS NULL
                       OR lower(i.properties) LIKE lower('%' || json_extract(alt.value, '$.prefilter') || '%'))
                  AND json_type(i.properties, '$.' || json_extract(alt.value, '$.def')) = 'text'
                  AND lower(json_extract(i.properties, '$.' || json_extract(alt.value, '$.def'))) LIKE lower('%' || json_extract(alt.value, '$.value') || '%'))
                 OR (json_extract(alt.value, '$.__op__') = 'gt'
                  AND json_type(i.properties, '$.' || json_extract(alt.value, '$.def')) IN ('integer', 'real')
                  AND json_extract(i.properties, '$.' || json_extract(alt.value, '$.def')) > json_extract(alt.value, '$.value'))
                 OR (json_extract(alt.value, '$.__op__') = 'gte'
                  AND json_type(i.properties, '$.' || json_extract(alt.value, '$.def')) IN ('integer', 'real')
                  AND json_extract(i.properties, '$.' || json_extract(alt.value, '$.def')) >= json_extract(alt.value, '$.value'))
                 OR (json_extract(alt.value, '$.__op__') = 'lt'
                  AND json_type(i.properties, '$.' || json_extract(alt.value, '$.def')) IN ('integer', 'real')
                  AND json_extract(i.properties, '$.' || json_extract(alt.value, '$.def')) < json_extract(alt.value, '$.value'))
                 OR (json_extract(alt.value, '$.__op__') = 'lte'
                  AND json_type(i.properties, '$.' || json_extract(alt.value, '$.def')) IN ('integer', 'real')
                  AND json_extract(i.properties, '$.' || json_extract(alt.value, '$.def')) <= json_extract(alt.value, '$.value'))
                 OR (json_extract(alt.value, '$.__op__') = 'before'
                  AND json_type(i.properties, '$.' || json_extract(alt.value, '$.def')) = 'text'
                  AND json_extract(i.properties, '$.' || json_extract(alt.value, '$.def')) < json_extract(alt.value, '$.value'))
                 OR (json_extract(alt.value, '$.__op__') = 'after'
                  AND json_type(i.properties, '$.' || json_extract(alt.value, '$.def')) = 'text'
                  AND json_extract(i.properties, '$.' || json_extract(alt.value, '$.def')) > json_extract(alt.value, '$.value'))
               ))
      )
    )
  )
  AND (
    sqlc.narg('involves_user_id') IS NULL
    OR (i.assignee_type = 'agent' AND i.assignee_id IN (
          SELECT a.id FROM agent a
           WHERE a.workspace_id = ?1
             AND a.owner_id     = sqlc.narg('involves_user_id')
    ))
    OR (i.assignee_type = 'squad' AND i.assignee_id IN (
          SELECT sm.squad_id
            FROM squad_member sm
            JOIN squad s ON s.id = sm.squad_id
           WHERE s.workspace_id = ?1
             AND sm.member_type = 'member'
             AND sm.member_id   = sqlc.narg('involves_user_id')
          UNION
          SELECT s.id
            FROM squad s
            JOIN agent a ON a.id = s.leader_id
           WHERE s.workspace_id = ?1
             AND a.workspace_id = ?1
             AND a.owner_id     = sqlc.narg('involves_user_id')
          UNION
          SELECT sm.squad_id
            FROM squad_member sm
            JOIN squad s ON s.id = sm.squad_id
            JOIN agent a ON a.id = sm.member_id
           WHERE s.workspace_id = ?1
             AND sm.member_type = 'agent'
             AND a.workspace_id = ?1
             AND a.owner_id     = sqlc.narg('involves_user_id')
    ))
  )
ORDER BY i.position ASC, i.created_at DESC;

-- name: CountIssues :one
-- See ListIssues for the semantics of involves_user_id.
SELECT count(*) FROM issue i
WHERE i.workspace_id = ?1
  AND (sqlc.narg('status') IS NULL OR i.status = sqlc.narg('status'))
  AND (sqlc.narg('priority') IS NULL OR i.priority = sqlc.narg('priority'))
  AND (sqlc.narg('assignee_id') IS NULL OR i.assignee_id = sqlc.narg('assignee_id'))
  AND (sqlc.narg('assignee_ids') IS NULL OR i.assignee_id IN (SELECT value FROM json_each(sqlc.narg('assignee_ids'))))
  AND (sqlc.narg('creator_id') IS NULL OR i.creator_id = sqlc.narg('creator_id'))
  AND (sqlc.narg('project_id') IS NULL OR i.project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('scheduled') IS NULL OR (i.start_date IS NOT NULL OR i.due_date IS NOT NULL))
  AND (sqlc.narg('metadata_filter') IS NULL OR NOT EXISTS (
      SELECT 1 FROM json_each(sqlc.narg('metadata_filter')) AS mf
      WHERE json_extract(i.metadata, '$.' || mf.key) IS NOT mf.value
  ))
  AND (
    sqlc.narg('involves_user_id') IS NULL
    OR (i.assignee_type = 'agent' AND i.assignee_id IN (
          SELECT a.id FROM agent a
           WHERE a.workspace_id = ?1
             AND a.owner_id     = sqlc.narg('involves_user_id')
    ))
    OR (i.assignee_type = 'squad' AND i.assignee_id IN (
          SELECT sm.squad_id
            FROM squad_member sm
            JOIN squad s ON s.id = sm.squad_id
           WHERE s.workspace_id = ?1
             AND sm.member_type = 'member'
             AND sm.member_id   = sqlc.narg('involves_user_id')
          UNION
          SELECT s.id
            FROM squad s
            JOIN agent a ON a.id = s.leader_id
           WHERE s.workspace_id = ?1
             AND a.workspace_id = ?1
             AND a.owner_id     = sqlc.narg('involves_user_id')
          UNION
          SELECT sm.squad_id
            FROM squad_member sm
            JOIN squad s ON s.id = sm.squad_id
            JOIN agent a ON a.id = sm.member_id
           WHERE s.workspace_id = ?1
             AND sm.member_type = 'agent'
             AND a.workspace_id = ?1
             AND a.owner_id     = sqlc.narg('involves_user_id')
    ))
  );

-- name: ListChildIssues :many
-- Order by number ASC so sub-issues display in stable creation order
-- (oldest first), matching how a parent's plan reads top-to-bottom. The
-- position column is computed per-(workspace, status) by NextTopPosition,
-- not relative to siblings, so ordering by it interleaves children
-- unpredictably across batches and statuses; number is a per-workspace
-- monotonic counter and is sibling-stable.
SELECT * FROM issue
WHERE parent_issue_id = ?1
ORDER BY number ASC;

-- name: ListChildrenByParents :many
-- Batched variant of ListChildIssues: returns all children for the given
-- parent set in one round trip. Used by Swimlane to avoid an N+1 fan-out
-- (one request per visible parent lane). Result is grouped client-side by
-- parent_issue_id; the workspace filter is also enforced so callers can't
-- enumerate children of parents in workspaces they don't belong to.
-- Within each parent, order by number ASC for the same sibling-stable
-- creation order as ListChildIssues.
SELECT * FROM issue
WHERE workspace_id = sqlc.arg('workspace_id')
  AND parent_issue_id IN (SELECT value FROM json_each(sqlc.arg('parent_ids')))
ORDER BY parent_issue_id, number ASC;

-- name: GetIssueByOrigin :one
-- Finds the issue stamped with a specific (origin_type, origin_id) pair.
-- Used by quick-create completion to deterministically locate the issue
-- produced by a given agent_task_queue.id - robust against concurrent
-- issue creates by the same agent (assignment task + quick-create both
-- running with max_concurrent_tasks > 1).
SELECT * FROM issue
WHERE workspace_id = ?1
  AND origin_type = ?2
  AND origin_id = ?3
LIMIT 1;

-- name: CountCreatedIssueAssignees :many
-- Count assignees on issues created by a specific user.
SELECT
  assignee_type,
  assignee_id,
  COUNT(*) as frequency
FROM issue
WHERE workspace_id = ?1
  AND creator_id = ?2
  AND creator_type = 'member'
  AND assignee_type IS NOT NULL
  AND assignee_id IS NOT NULL
GROUP BY assignee_type, assignee_id;

-- name: ChildIssueProgress :many
SELECT parent_issue_id,
       COUNT(*) AS total,
       COUNT(CASE WHEN status IN (SELECT value FROM json_each(sqlc.arg('terminal_status_keys'))) THEN 1 END) AS done
FROM issue
WHERE workspace_id = ?1
  AND parent_issue_id IS NOT NULL
GROUP BY parent_issue_id;

-- SearchIssues: moved to handler (dynamic SQL for multi-word search support).

-- name: SetIssueMetadataKey :one
-- Atomically sets a single key in the issue's metadata JSONB. The
-- workspace_id filter is the authorization gate - handler resolves the
-- issue first so this is also the tenant check. A no-op, a missing issue, or
-- a workspace mismatch returns no rows; callers that must distinguish those
-- cases need a separate workspace-scoped read.
UPDATE issue SET
    metadata = json_set(COALESCE(metadata, '{}'), '$.' || sqlc.arg('key'), json(sqlc.arg('value'))),
    revision = revision + 1,
    last_activity_at = GREATEST(COALESCE(last_activity_at, updated_at), now()),
    updated_at = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
  AND json_extract(metadata, '$.' || sqlc.arg('key')) IS NOT json_extract(sqlc.arg('value'), '$')
RETURNING id, workspace_id, metadata, revision;

-- name: DeleteIssueMetadataKey :one
-- Atomically removes a single key from the issue's metadata JSONB.
-- Deleting a missing key is a no-op (returns no rows).
UPDATE issue SET
    metadata = json_remove(metadata, '$.' || sqlc.arg('key')),
    revision = revision + 1,
    last_activity_at = GREATEST(COALESCE(last_activity_at, updated_at), now()),
    updated_at = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
  AND json_type(metadata, '$.' || sqlc.arg('key')) IS NOT NULL
RETURNING id, workspace_id, metadata, revision;

-- name: MarkIssueFirstExecuted :one
-- Flips first_executed_at from NULL to now() atomically. Returns the row if
-- this was the first time the issue was executed; no rows otherwise. The
-- analytics issue_executed event fires exactly when this returns a row -
-- retries and re-assignments hit the WHERE clause and no-op.
UPDATE issue
SET first_executed_at = now()
WHERE id = ?1 AND first_executed_at IS NULL
RETURNING id, workspace_id, creator_type, creator_id, first_executed_at;

-- name: CountIssuesUpTo :one
-- Bounded count for issue-limit admission and display. Callers pass only the
-- threshold needed for their decision, avoiding a full scan in an oversized
-- workspace.
SELECT COUNT(*)
FROM (
    SELECT 1
    FROM issue
    WHERE workspace_id = ?1
    LIMIT sqlc.arg('limit')
) bounded_issues;
