-- name: GetHead :one
SELECT revision, parent_id, data FROM runtime_records
WHERE tenant_id = $1 AND owner_id = $2 AND namespace = $3 AND object_id = $4;

-- name: GetVersion :one
SELECT data FROM runtime_versions
WHERE tenant_id = $1 AND owner_id = $2 AND namespace = $3 AND object_id = $4 AND revision = $5;

-- name: CreateHead :exec
INSERT INTO runtime_records (tenant_id, owner_id, namespace, object_id, revision, parent_id, data)
VALUES ($1, $2, $3, $4, 1, $5, $6);

-- name: CreateVersion :exec
INSERT INTO runtime_versions (tenant_id, owner_id, namespace, object_id, revision, data)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: PutHead :execrows
UPDATE runtime_records SET revision = revision + 1, data = $1
WHERE tenant_id = $2 AND owner_id = $3 AND namespace = $4 AND object_id = $5 AND revision = $6;

-- name: ListHeads :many
SELECT object_id, revision, parent_id, data FROM runtime_records
WHERE tenant_id = $1 AND owner_id = $2 AND namespace = $3
AND ($4::text = '' OR parent_id = $5) AND object_id > $6 ORDER BY object_id LIMIT $7;

-- name: GetHeadForUpdate :one
SELECT revision, parent_id, data FROM runtime_records
WHERE tenant_id = $1 AND owner_id = $2 AND namespace = $3 AND object_id = $4 FOR UPDATE;
