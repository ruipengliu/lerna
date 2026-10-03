#!/usr/bin/env bash
set -euo pipefail
: "${LERNA_TEST_POSTGRES_DSN:?Supply a dedicated PostgreSQL test database}"
if [[ $# != 1 || -e "$1" ]]; then
  echo 'Usage: scripts/generate-durable-v2-fixtures.sh NEW_OUTPUT_DIRECTORY' >&2
  exit 2
fi
repository=$(git rev-parse --show-toplevel)
output=$(realpath -m "$1")
revision=b1674b2d0252da73f3dfd753857484597dc0a080
dump_tool=${LERNA_V2_PGDUMP:-pg_dump}
if [[ $("$dump_tool" --version) != 'pg_dump (PostgreSQL) 18.6'* ]]; then
  echo 'PostgreSQL18.6 pg_dump is required' >&2
  exit 1
fi
source_directory=$(mktemp -d)
trap 'rm -rf "$source_directory"' EXIT
git -C "$repository" archive "$revision" | tar -x -C "$source_directory"
mkdir -p "$source_directory/cmd/legacy-v2-writer" "$output/pg-v2" "$output/sqlite-v2"
cp "$repository/conformance/fixtures/durable-work/v2-writer.go.txt" "$source_directory/cmd/legacy-v2-writer/main.go"
cp "$repository/conformance/fixtures/durable-work/v2-writer.go.txt" "$output/v2-writer.go.txt"
(cd "$source_directory" && GOTOOLCHAIN=local GOFLAGS=-mod=readonly go build -o "$source_directory/v2-writer" ./cmd/legacy-v2-writer)
for backend in postgres sqlite; do
  if [[ "$backend" == postgres ]]; then folder=pg-v2; else folder=sqlite-v2; fi
  schema="lerna_test_$(python3 -c 'import secrets; print(secrets.token_hex(12))')"
  LERNA_LEGACY_BACKEND="$backend" LERNA_LEGACY_SCHEMA="$schema" LERNA_LEGACY_PATH="$source_directory/v2.sqlite" LERNA_LEGACY_CREATE=1 LERNA_LEGACY_EXPORT="$output/$folder" LERNA_V2_PGDUMP="$dump_tool" "$source_directory/v2-writer" >/dev/null
  if [[ "$backend" == postgres ]]; then printf '%s\n' "$schema" > "$output/$folder/writer-schema.txt"; else cp "$source_directory/v2.sqlite" "$output/$folder/database.sqlite"; fi
  cp "$source_directory/adapters/$backend/migrations/host/0001_admission.sql" "$output/$folder/0001_admission.sql"
  cp "$source_directory/adapters/$backend/migrations/host/0002_claims.sql" "$output/$folder/0002_claims.sql"
  printf '%s\n' "$revision" > "$output/$folder/writer-revision.txt"
  (cd "$output/$folder" && sha256sum 0001_admission.sql 0002_claims.sql database.* migrations.json writer-*.json writer-*.txt ../v2-writer.go.txt > SHA256SUMS)
done
echo 'Real v2 exports complete; generation scopes and source cleaned.'
