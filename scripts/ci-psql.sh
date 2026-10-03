#!/usr/bin/env bash
# CI-only restore client: use the same immutable PostgreSQL image as the service.
set -euo pipefail
: "${TMPDIR:?CI must supply an owned temporary directory mounted at the same path}"
[[ "$TMPDIR" = /* && -d "$TMPDIR" ]] || { echo 'CI psql requires an existing absolute TMPDIR' >&2; exit 1; }
private_scope=''
if [[ -z "${LERNA_PSQL_CIDFILE:-}" ]]; then
  private_scope=$(mktemp -d "$TMPDIR/lerna-psql.XXXXXXXX")
  LERNA_PSQL_CIDFILE="$private_scope/container.cid"
fi
[[ "$LERNA_PSQL_CIDFILE" = /* && ! -e "$LERNA_PSQL_CIDFILE" ]] || { echo 'CI psql requires a new owned absolute cidfile' >&2; exit 1; }
cleanup() {
  local result=$? container_id='' proved_absent=0
  if [[ -f "$LERNA_PSQL_CIDFILE" ]]; then
    container_id=$(<"$LERNA_PSQL_CIDFILE")
    if [[ "$container_id" =~ ^[0-9a-f]{64}$ ]]; then
      timeout --foreground --kill-after=2s 8s docker rm --force "$container_id" >/dev/null 2>&1 || true
      if [[ -n "$private_scope" ]]; then
        local remaining
        if remaining=$(timeout --foreground --kill-after=2s 8s docker container ls --all --no-trunc --filter "id=$container_id" --format '{{.ID}}') && [[ -z "$remaining" ]]; then
          proved_absent=1
        else
          echo "Cannot confirm owned psql container cleanup; cidfile retained at $LERNA_PSQL_CIDFILE" >&2
          result=1
        fi
      fi
    elif [[ -n "$private_scope" ]]; then
      echo "Unknown Docker CREATE result; no psql was started, cidfile retained at $LERNA_PSQL_CIDFILE" >&2
      result=1
    fi
  elif [[ -n "$private_scope" ]]; then
    echo "Unknown Docker CREATE result; no psql was started, scope retained at $private_scope" >&2
    result=1
  fi
  if [[ -n "$private_scope" && "$proved_absent" = 1 ]]; then rm -rf -- "$private_scope"; fi
  return "$result"
}
# A supplied cidfile survives for the Go caller's independent bounded cleanup.
trap cleanup EXIT
# No psql can start before Docker has written the successful create's exact ID.
# An unknown create result is never identified by image/name/time and never started.
timeout --foreground --kill-after=2s 15s docker create --rm --cidfile "$LERNA_PSQL_CIDFILE" --network host \
  --mount "type=bind,src=$TMPDIR,dst=$TMPDIR,readonly" \
  -e PGHOST -e PGPORT -e PGUSER -e PGPASSWORD -e PGDATABASE \
  -e PGCONNECT_TIMEOUT -e PGSSLMODE -e PGSSLROOTCERT -e PGSSLCERT -e PGSSLKEY \
  --entrypoint timeout \
  postgres@sha256:3725f4e2499eef5134592b3b4ab79a543ed7f8e533b05b5b637af926630f6650 \
  --signal=TERM --kill-after=2s 10s psql "$@" >/dev/null
container_id=$(<"$LERNA_PSQL_CIDFILE")
[[ "$container_id" =~ ^[0-9a-f]{64}$ ]] || { echo 'Docker did not record an exact created container ID' >&2; exit 1; }
timeout --foreground --kill-after=2s 15s docker start --attach "$container_id"
