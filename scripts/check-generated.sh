#!/bin/sh
set -eu
# 在临时输出目录生成，既发现过期/多余文件，也不改动工作区。
buf=$1
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
sed "s|out: contracts/gen/go|out: $tmp/go|" buf.gen.yaml > "$tmp/buf.gen.yaml"
"$buf" generate --template "$tmp/buf.gen.yaml"
diff -ru contracts/gen/go "$tmp/go"
