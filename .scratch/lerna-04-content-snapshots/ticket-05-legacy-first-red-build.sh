#!/usr/bin/env bash
# Run only inside the explicitly granted finite native supervisor.
set -euo pipefail
source /workspace/.lerna-env/env.sh
ticket05_repo=/tmp/lerna-worktrees/content-snapshots-05
ticket05_frozen=/tmp/lerna-04-ticket05-execution/frozen-1a7-ticket05
ticket05_sha=1a7d910238eb74cddc712d92b0ba4014a72ff507
test "$(git -C "$ticket05_repo" rev-parse "$ticket05_sha^{commit}")" = "$ticket05_sha"
# No published source or old scope is edited. A fresh frozen build directory
# and a separate supervisor driver are materialized under this owner's overlay.
mkdir -m 700 "$ticket05_frozen"
git -C "$ticket05_repo" archive "$ticket05_sha" -o "$ticket05_frozen/source.tar"
mkdir -m 700 "$ticket05_frozen/source"
tar -xf "$ticket05_frozen/source.tar" -C "$ticket05_frozen/source"
mkdir -m 700 "$ticket05_frozen/source/.ticket05-producer"
cp "$ticket05_repo/conformance/internal/contentfixture/testdata/ticket05-producer/main.go.txt" "$ticket05_frozen/source/.ticket05-producer/main.go"
gofmt -w "$ticket05_frozen/source/.ticket05-producer/main.go"
cd "$ticket05_frozen/source"
go build -mod=readonly -o "$ticket05_frozen/legacy-producer" ./.ticket05-producer
sha256sum "$ticket05_frozen/source.tar" "$ticket05_frozen/source/.ticket05-producer/main.go" "$ticket05_frozen/legacy-producer"
sha256sum adapters/postgres/content/migrations/0001_content.sql
