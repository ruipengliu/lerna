#!/bin/sh
# H1：固定样本的绑定、体积、编解码、语义入口和进程峰值内存测量。
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
out=${1:-/tmp/lerna-protobuf-measurement}
mkdir -p "$out"
out=$(CDPATH= cd -- "$out" && pwd)
steps="$out/execution-steps.txt"
printf 'platform %s\n' "$(uname -s)" > "$steps"
printf 'baseline_build_options -trimpath -ldflags=-s -w\nbinding_build_options -trimpath -ldflags=-s -w\n' >> "$steps"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM
{
 git rev-parse HEAD
 git status --short
 go version
 go env GOOS GOARCH CGO_ENABLED
 go list -m google.golang.org/protobuf
 uname -a
 shasum -a 256 conformance/protobuf/testdata/mainline.json
} > "$out/environment.txt"
cat > "$work/baseline.go" <<'GO'
package main
import "fmt"
func main(){fmt.Println(0)}
GO
cat > "$work/binding.go" <<'GO'
package main
import (
 "fmt"
 v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
 "google.golang.org/protobuf/proto"
)
func main(){fmt.Println(proto.Size(&v1.Result{}))}
GO
go build -trimpath -ldflags='-s -w' -o "$work/baseline" "$work/baseline.go"
go build -trimpath -ldflags='-s -w' -o "$work/binding" "$work/binding.go"
{
 printf 'baseline_executable_bytes '; wc -c < "$work/baseline"
 printf 'binding_executable_bytes '; wc -c < "$work/binding"
 printf 'generated_go_source_bytes '; cat contracts/gen/go/lerna/v1/*.pb.go | wc -c
 printf 'protobuf_source_bytes '; cat contracts/proto/lerna/v1/*.proto | wc -c
} > "$out/sizes.txt"
go test -c -o "$work/measure.test" ./conformance/protobuf
printf 'builds_completed_before_timed_process true\n' >> "$steps"
# Record the exact compiled executables before temporary files are removed.
(cd "$work" && shasum -a 256 baseline.go binding.go baseline binding measure.test) > "$out/binaries-sha256.txt"
if test -n "${LERNA_PROTOBUF_BINARY_ARCHIVE:-}"; then
 mkdir -p "$LERNA_PROTOBUF_BINARY_ARCHIVE"
 cp "$work/baseline.go" "$work/binding.go" "$work/baseline" "$work/binding" "$work/measure.test" "$LERNA_PROTOBUF_BINARY_ARCHIVE/"
fi
cp conformance/protobuf/testdata/mainline.json "$out/measured-corpus.json"
git ls-files -z --cached --others --exclude-standard -- '*.go' '*.proto' 'go.mod' 'go.sum' 'scripts/measure-protobuf.sh' | xargs -0 shasum -a 256 > "$out/source-sha256.txt"
shasum -a 256 conformance/protobuf/testdata/mainline.json > "$out/corpus-sha256.txt"
cd conformance/protobuf
# 实际调用两端均取摘要，记录原进程退出码；失败时保留证据并原样退出。
record_state() {
 printf '%s_%s_utc %s\n' "$1" "$2" "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" >> "$steps"
 printf '%s_binary_%s %s\n' "$1" "$2" "$(shasum -a 256 "$work/measure.test" | cut -d ' ' -f 1)" >> "$steps"
 printf '%s_corpus_%s %s\n' "$1" "$2" "$(shasum -a 256 testdata/mainline.json | cut -d ' ' -f 1)" >> "$steps"
 printf '%s_source_manifest_%s %s\n' "$1" "$2" "$(shasum -a 256 "$out/source-sha256.txt" | cut -d ' ' -f 1)" >> "$steps"
 printf '%s_environment_manifest_%s %s\n' "$1" "$2" "$(shasum -a 256 "$out/environment.txt" | cut -d ' ' -f 1)" >> "$steps"
}
record_state inventory before
printf 'inventory_report_env %s\n' "$out/inventory.json" >> "$steps"
printf 'inventory_argv %s -test.run ^TestWriteProtobufMeasurementInventory$ -test.v\n' "$work/measure.test" >> "$steps"
set +e
LERNA_PROTOBUF_REPORT="$out/inventory.json" "$work/measure.test" -test.run '^TestWriteProtobufMeasurementInventory$' -test.v > "$out/inventory-run.txt"
inventory_exit=$?
set -e
printf 'inventory_exit %s\n' "$inventory_exit" >> "$steps"
record_state inventory after
test "$inventory_exit" -eq 0 || exit "$inventory_exit"
# 先编译再计时：峰值是测量进程 RSS，排除编译器和 SQLite；包含 Go 运行库、测试框架、样本和 GC。
record_state benchmark before
printf 'benchmark_report_env %s\n' "${LERNA_PROTOBUF_REPORT:-UNSET}" >> "$steps"
printf 'benchmark_argv %s -test.run ^$ -test.bench . -test.benchtime=100ms -test.count=3\n' "$work/measure.test" >> "$steps"
set +e
case $(uname -s) in
 Darwin) /usr/bin/time -l "$work/measure.test" -test.run '^$' -test.bench . -test.benchtime=100ms -test.count=3 > "$out/bench.txt" 2> "$out/peak-memory.txt" ;;
 Linux) /usr/bin/time -v "$work/measure.test" -test.run '^$' -test.bench . -test.benchtime=100ms -test.count=3 > "$out/bench.txt" 2> "$out/peak-memory.txt" ;;
 *) echo 'RSS measurement requires Darwin or Linux /usr/bin/time' >&2; exit 1 ;;
esac
benchmark_exit=$?
set -e
printf 'benchmark_exit %s\n' "$benchmark_exit" >> "$steps"
record_state benchmark after
test "$benchmark_exit" -eq 0 || exit "$benchmark_exit"
(cd "$out" && shasum -a 256 environment.txt inventory.json inventory-run.txt sizes.txt bench.txt peak-memory.txt binaries-sha256.txt source-sha256.txt corpus-sha256.txt measured-corpus.json) > "$out/artifacts-sha256.txt"
printf 'Measurement files: %s\n' "$out"
