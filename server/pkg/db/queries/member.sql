-- name: ListMembers :many
SELECT * FROM member
WHERE workspace_id = ?1
ORDER BY created_at ASC;

-- name: ListWorkspaceManagerUserIDs :many
SELECT user_id FROM member
WHERE workspace_id = ?1 AND role IN ('owner', 'admin')
ORDER BY created_at ASC;

-- name: GetMember :one
SELECT * FROM member
WHERE id = ?1;

-- name: GetMemberByUserAndWorkspace :one
SELECT * FROM member
WHERE user_id = ?1 AND workspace_id = ?2;

-- name: GetRealtimeMemberships :many
SELECT m.user_id, m.workspace_id
FROM member m
JOIN (
    -- JSON shape: user_ids and workspace_ids are JSON string arrays zipped by index.
    SELECT json_extract(u.value, '$') AS user_id, json_extract(w.value, '$') AS workspace_id
    FROM json_each(sqlc.arg(user_ids)) AS u
    JOIN json_each(sqlc.arg(workspace_ids)) AS w ON u.key = w.key
) AS requested
    ON m.user_id = requested.user_id AND m.workspace_id = requested.workspace_id;

-- name: CreateMember :one
INSERT INTO member (id, workspace_id, user_id, role)
VALUES (gen_random_uuid(), ?1, ?2, ?3)
RETURNING *;

-- name: UpdateMemberRole :one
UPDATE member SET role = ?2
WHERE id = ?1
RETURNING *;

-- name: DeleteMember :exec
DELETE FROM member WHERE id = ?1;

-- name: ListMembersWithUser :many
SELECT m.id, m.workspace_id, m.user_id, m.role, m.created_at,
       u.name as user_name, u.email as user_email, u.avatar_url as user_avatar_url
FROM member m
JOIN "user" u ON u.id = m.user_id
WHERE m.workspace_id = ?1
ORDER BY m.created_at ASC;
