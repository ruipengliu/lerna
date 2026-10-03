-- name: GetHead :one
SELECT revision, parent_id, data FROM runtime_records
WHERE tenant_id = ? AND owner_id = ? AND namespace = ? AND object_id = ?;

-- name: GetVersion :one
SELECT data FROM runtime_versions
WHERE tenant_id = ? AND owner_id = ? AND namespace = ? AND object_id = ? AND revision = ?;

-- name: CreateHead :exec
INSERT INTO runtime_records (tenant_id, owner_id, namespace, object_id, revision, parent_id, data)
VALUES (?, ?, ?, ?, 1, ?, ?);

-- name: CreateVersion :exec
INSERT INTO runtime_versions (tenant_id, owner_id, namespace, object_id, revision, data)
VALUES (?, ?, ?, ?, ?, ?);

-- name: PutHead :execrows
UPDATE runtime_records SET revision = revision + 1, data = ?
WHERE tenant_id = ? AND owner_id = ? AND namespace = ? AND object_id = ? AND revision = ?;

-- name: ListHeads :many
SELECT object_id, revision, parent_id, data FROM runtime_records
WHERE tenant_id = ? AND owner_id = ? AND namespace = ?
AND (? = '' OR parent_id = ?) AND object_id > ? ORDER BY object_id LIMIT ?;
