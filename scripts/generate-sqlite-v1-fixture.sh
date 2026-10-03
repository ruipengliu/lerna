#!/usr/bin/env bash
set -euo pipefail
if [[ $# != 2 ]]; then
  echo 'Usage: scripts/generate-sqlite-v1-fixture.sh IMMUTABLE_GIT_REVISION NEW_OUTPUT_DIRECTORY' >&2
  exit 2
fi
[[ $(go env CGO_ENABLED) == 1 && $(go env GOOS) == linux ]] || {
  echo 'SQLite fixture writer requires Linux, CGO_ENABLED=1 and a C compiler' >&2
  exit 1
}
revision=$(git rev-parse --verify "$1^{commit}")
output=$(realpath -m "$2")
[[ ! -e "$output" ]] || { echo 'Fixture output must be a new directory' >&2; exit 1; }
source_directory=$(mktemp -d)
trap 'rm -rf "$source_directory"' EXIT
git archive "$revision" | tar -x -C "$source_directory"
(
  cd "$source_directory"
  GOTOOLCHAIN=local GOFLAGS=-mod=readonly go build -o "$source_directory/v1-writer" ./cmd/sqlite-durable-work-fixture
  "$source_directory/v1-writer" -output "$output"
)
cp "$source_directory/adapters/sqlite/migrations/host/0001_admission.sql" "$output/0001_admission.sql"
cp "$source_directory/conformance/fixtures/durable-work/sqlite-v1/commands.json" "$output/commands.json"
printf '%s\n' "$revision" > "$output/writer-revision.txt"
(
  cd "$output"
  sha256sum 0001_admission.sql commands.json database.sqlite writer-observation.json writer-revision.txt > SHA256SUMS
)
echo 'Historical SQLite writer fixture complete; SHA256SUMS records exact inputs and output.'
