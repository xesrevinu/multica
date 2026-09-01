-- name: GetTerminalWorkspaceState :one
SELECT workspace_id, user_id, state, updated_at
FROM terminal_workspace_state
WHERE workspace_id = $1 AND user_id = $2;

-- name: UpsertTerminalWorkspaceState :one
INSERT INTO terminal_workspace_state (workspace_id, user_id, state)
VALUES ($1, $2, $3)
ON CONFLICT (workspace_id, user_id)
DO UPDATE SET state = EXCLUDED.state, updated_at = now()
RETURNING workspace_id, user_id, state, updated_at;

-- name: DeleteTerminalWorkspaceStateByUser :exec
DELETE FROM terminal_workspace_state
WHERE workspace_id = $1 AND user_id = $2;
