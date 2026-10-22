-- name: ListConversationRootOwners :many
-- Preserve the newest non-null squad per agent, including terminal tasks.
-- Routing does not need execution context, results, or other issue threads.
SELECT agent_id, squad_id
FROM (
    SELECT agent_id, squad_id,
           row_number() OVER (
               PARTITION BY agent_id
               ORDER BY (squad_id IS NOT NULL) DESC, created_at DESC, id DESC
           ) AS rn
    FROM agent_task_queue
    WHERE issue_id = ?1 AND trigger_comment_id = ?2 AND agent_id IS NOT NULL
)
WHERE rn = 1;
