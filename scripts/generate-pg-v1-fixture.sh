#!/usr/bin/env bash
set -euo pipefail
: "${LERNA_TEST_POSTGRES_DSN:?LERNA_TEST_POSTGRES_DSN must identify a dedicated test database}"
if [[ $# != 2 ]]; then
  echo 'Usage: scripts/generate-pg-v1-fixture.sh IMMUTABLE_GIT_REVISION OUTPUT_DIRECTORY' >&2
  exit 2
fi
revision=$(git rev-parse --verify "$1^{commit}")
output=$(realpath -m "$2")
dump_version=$(pg_dump --version)
if [[ "$dump_version" != 'pg_dump (PostgreSQL) 18.6' && "$dump_version" != 'pg_dump (PostgreSQL) 18.6 '* ]]; then
  echo 'PostgreSQL 18.6 pg_dump is required' >&2
  exit 1
fi
source_directory=$(mktemp -d)
trap 'rm -rf "$source_directory"' EXIT
git archive "$revision" | tar -x -C "$source_directory"
(
  cd "$source_directory"
  GOTOOLCHAIN=local GOFLAGS=-mod=readonly go build -o "$source_directory/v1-writer" ./cmd/durable-work-fixture
  "$source_directory/v1-writer" -output "$output"
)
cp "$source_directory/adapters/postgres/migrations/host/0001_admission.sql" "$output/0001_admission.sql"
cp "$source_directory/conformance/fixtures/durable-work/pg-v1/commands.json" "$output/commands.json"
printf '%s\n' "$revision" > "$output/writer-revision.txt"
(
  cd "$output"
  sha256sum 0001_admission.sql commands.json database.sql writer-observation.json writer-revision.txt > SHA256SUMS
)
echo 'Historical writer export complete; SHA256SUMS records exact inputs and output.'
