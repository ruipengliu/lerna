#!/bin/sh
# Reproducible implementation evidence; not a whole-ticket acceptance decision.
set -eu
cd "$(dirname "$0")/.."
export GOTOOLCHAIN=go1.26.1
extraction_report_dir=build/extraction-verification
mkdir -p "$extraction_report_dir"
rm -f "$extraction_report_dir/quality.json" "$extraction_report_dir/tests.jsonl" "$extraction_report_dir/stages.json"
extraction_build=not_run
extraction_analysis=not_run
extraction_contracts=not_run
extraction_quality=not_run
write_extraction_stages() {
  printf '{"profile":"memory-extraction-evidence-v1","ticket_acceptance":"not_evaluated","build":"%s","analysis":"%s","contracts_and_processes":"%s","fixed_dataset_quality":"%s"}\n' \
    "$extraction_build" "$extraction_analysis" "$extraction_contracts" "$extraction_quality" > "$extraction_report_dir/stages.json"
}
trap write_extraction_stages EXIT
extraction_build=failed
go build -mod=readonly -trimpath -o "$extraction_report_dir/extractioncheck" ./cmd/extractioncheck
extraction_build=passed
extraction_analysis=failed
go vet -mod=readonly ./extraction ./adapters/extraction/auth ./adapters/extraction/cleanup ./adapters/extraction/execution ./adapters/extraction/inputs ./adapters/extraction/sourceguard ./adapters/extraction/rules ./adapters/extraction/localsource ./adapters/extraction/sqlite ./profiles/extractioncheck ./cmd/extractioncheck
extraction_analysis=passed
extraction_contracts=failed
# Includes real Core/SDK/SQLite/file paths and subprocess crash probes. Keep
# package disk contention bounded; individual concurrency tests remain active.
go test -mod=readonly -p 1 -race -count=1 -timeout 45m -json ./extraction ./adapters/extraction/auth ./adapters/extraction/cleanup ./adapters/extraction/execution ./adapters/extraction/inputs ./adapters/extraction/sourceguard ./adapters/extraction/rules ./adapters/extraction/localsource ./adapters/extraction/sqlite ./profiles/extractioncheck > "$extraction_report_dir/tests.jsonl"
extraction_contracts=passed
extraction_quality=failed
"$extraction_report_dir/extractioncheck" -profile local-rules-quality-v1 > "$extraction_report_dir/quality.json"
extraction_quality=passed
