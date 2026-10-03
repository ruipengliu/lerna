-- name: ExpireQueryBinding :exec
DELETE FROM runtime_query_bindings WHERE tenant_id = sqlc.arg(tenant_id) AND owner_id = sqlc.arg(owner_id) AND query_id = sqlc.arg(query_id) AND expires_at <= sqlc.arg(now);

-- name: InsertQueryBinding :exec
INSERT INTO runtime_query_bindings (tenant_id,owner_id,query_id,binding_id,principal_id,credential_generation,roles_digest,query_digest,expires_at)
VALUES (sqlc.arg(tenant_id),sqlc.arg(owner_id),sqlc.arg(query_id),sqlc.arg(binding_id),sqlc.arg(principal_id),sqlc.arg(credential_generation),sqlc.arg(roles_digest),sqlc.arg(query_digest),sqlc.arg(expires_at))
ON CONFLICT (tenant_id,owner_id,query_id) DO NOTHING;

-- name: GetQueryBinding :one
SELECT query_id,binding_id,principal_id,credential_generation,roles_digest,query_digest,COALESCE(result_digest,'') AS result_digest,expires_at
FROM runtime_query_bindings WHERE tenant_id = sqlc.arg(tenant_id) AND owner_id = sqlc.arg(owner_id) AND query_id = sqlc.arg(query_id);

-- name: SealQueryBinding :execrows
UPDATE runtime_query_bindings SET result_digest = sqlc.arg(result_digest)
WHERE tenant_id = sqlc.arg(tenant_id) AND owner_id = sqlc.arg(owner_id) AND query_id = sqlc.arg(query_id) AND binding_id = sqlc.arg(binding_id) AND result_digest IS NULL AND expires_at > sqlc.arg(now);

-- name: PruneQueryBindings :many
DELETE FROM runtime_query_bindings AS q
WHERE q.tenant_id = sqlc.arg(tenant_id) AND q.owner_id = sqlc.arg(owner_id)
AND q.query_id IN (SELECT expired.query_id FROM runtime_query_bindings AS expired WHERE expired.tenant_id = sqlc.arg(tenant_id) AND expired.owner_id = sqlc.arg(owner_id) AND expired.expires_at <= sqlc.arg(now) ORDER BY expired.expires_at,expired.query_id LIMIT sqlc.arg(batch_limit))
RETURNING query_id;
