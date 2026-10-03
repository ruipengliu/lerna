-- name: ExpireQueryBinding :exec
DELETE FROM runtime_query_bindings WHERE tenant_id = sqlc.arg(tenant_id) AND owner_id = sqlc.arg(owner_id) AND query_id = sqlc.arg(query_id) AND expires_at <= clock_timestamp();

-- name: InsertQueryBinding :exec
INSERT INTO runtime_query_bindings (tenant_id,owner_id,query_id,binding_id,principal_id,credential_generation,roles_digest,query_digest,expires_at)
VALUES (sqlc.arg(tenant_id),sqlc.arg(owner_id),sqlc.arg(query_id),sqlc.arg(binding_id),sqlc.arg(principal_id),sqlc.arg(credential_generation),sqlc.arg(roles_digest),sqlc.arg(query_digest),clock_timestamp() + (sqlc.arg(ttl_milliseconds)::BIGINT * interval '1 millisecond'))
ON CONFLICT (tenant_id,owner_id,query_id) DO NOTHING;

-- name: GetQueryBindingForUpdate :one
SELECT query_id,binding_id,principal_id,credential_generation,roles_digest,query_digest,COALESCE(result_digest,'')::text AS result_digest,expires_at
FROM runtime_query_bindings WHERE tenant_id = sqlc.arg(tenant_id) AND owner_id = sqlc.arg(owner_id) AND query_id = sqlc.arg(query_id) FOR UPDATE;

-- name: SealQueryBinding :execrows
UPDATE runtime_query_bindings SET result_digest = sqlc.arg(result_digest)
WHERE tenant_id = sqlc.arg(tenant_id) AND owner_id = sqlc.arg(owner_id) AND query_id = sqlc.arg(query_id) AND binding_id = sqlc.arg(binding_id) AND result_digest IS NULL AND expires_at > clock_timestamp();

-- name: PruneQueryBindings :many
WITH expired AS (
  SELECT source.tenant_id,source.owner_id,source.query_id FROM runtime_query_bindings AS source
  WHERE source.tenant_id = sqlc.arg(tenant_id) AND source.owner_id = sqlc.arg(owner_id) AND source.expires_at <= clock_timestamp()
  ORDER BY source.expires_at,source.query_id LIMIT sqlc.arg(batch_limit) FOR UPDATE SKIP LOCKED
)
DELETE FROM runtime_query_bindings AS q USING expired
WHERE q.tenant_id = expired.tenant_id AND q.owner_id = expired.owner_id AND q.query_id = expired.query_id
RETURNING q.query_id;
