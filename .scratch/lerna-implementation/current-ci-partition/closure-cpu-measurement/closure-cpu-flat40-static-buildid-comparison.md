# Original flat40 warning: finite static comparison

Flat40 actually exited 0, completed Wait, had no remaining current group, closed its owned raw log, and released LOCAL. Cum40 has not run. The original warning/release are preserved unchanged.

The warning Build ID `79e0da0f05534522bbca82f3304c7f13428a2d33` belongs to the first mapping, the exact explicitly supplied `closure-race.test`. Its ELF GNU build-id note exactly matches. It is not the libc mapping (libc has `c495b62edadd6c356265942ec1282d98058a7b41`), nor the loader mapping (`c591a5df63f461bfdafb01908ca16845b375fa37`).

The installed Go 1.27.1 `cmd/pprof` uses its own `objTool` (pprof.go:38–43); `file.BuildID()` returns an empty string with comment “No support for build ID” (256–259). Discovery compares that empty return against a nonempty profile mapping ID and prints the observed warning (vendor driver/fetch.go:449–453). That search does not clear the mapping filename. The explicit binary argument then overrides the first mapping filename (475–489). Local symbolization independently opens that mapping, rejects mismatches only when the object ID is nonempty, and otherwise invokes symbolization (vendor internal/symbolizer/symbolizer.go:180–198).

This exact source path explains the warning without changing the profile, binary, options, or original log. The search-path warning does not mean the explicitly supplied main binary was absent or actually had a different ELF Build ID. This finite comparison does not certify complete per-frame symbolization or infer CPU causality; it reads only profile mappings/string-table metadata, main ELF notes, and installed source. Sample, location, and function messages were skipped. No third pprof view, Go execution, environment probe, database query, source modification, or retry occurred.

Original producer Close remains UNKNOWN for both the CPU profile writer and Go binary output, distinct from acknowledged readonly Close. Root remains protected. Original cum40 awaits a separate root decision and grant.

| Mapping | File | Build ID |
| --- | --- | --- |
| 1 | `/tmp/lerna-partition-durable-closure-cpu-execution/artifacts/closure-race.test` | `79e0da0f05534522bbca82f3304c7f13428a2d33` |
| 2 | `/usr/lib/x86_64-linux-gnu/libc.so.6` | `c495b62edadd6c356265942ec1282d98058a7b41` |
| 3 | `[vdso]` | `(empty)` |
| 4 | `/usr/lib/x86_64-linux-gnu/ld-linux-x86-64.so.2` | `c591a5df63f461bfdafb01908ca16845b375fa37` |

Exact original inputs, ELF note bytes, source-file and excerpt hashes, readonly identity checks, and preserved release provenance are recorded in `closure-cpu-flat40-static-buildid-comparison.json`.
