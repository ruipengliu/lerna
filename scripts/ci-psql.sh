#!/usr/bin/env bash
# CI-only restore client: use the same immutable PostgreSQL image as the service.
set -euo pipefail
: "${TMPDIR:?CI must supply an owned temporary directory mounted at the same path}"
[[ "$TMPDIR" = /* && -d "$TMPDIR" ]] || { echo 'CI psql requires an existing absolute TMPDIR' >&2; exit 1; }
exec docker run --rm --network host \
  --mount "type=bind,src=$TMPDIR,dst=$TMPDIR,readonly" \
  -e PGHOST -e PGPORT -e PGUSER -e PGPASSWORD -e PGDATABASE \
  -e PGCONNECT_TIMEOUT -e PGSSLMODE -e PGSSLROOTCERT -e PGSSLCERT -e PGSSLKEY \
  --entrypoint psql \
  postgres@sha256:3725f4e2499eef5134592b3b4ab79a543ed7f8e533b05b5b637af926630f6650 "$@"
