# Typed contract runner

This repository test tool enters the public Go `Decode[T]` / `Encode[T]` path selected by the generated schema inventory. `CommandInput` enters `DecodeCommand`. The TypeScript tool at `sdk/typescript/src/valuerunner.ts` uses the corresponding public typed paths.

The original single-request CLI remains available: build this Go package, then pass a schema name and provide the raw JSON body on stdin. It emits encoded wire bytes on stdout, or a closed public error on stderr with a nonzero exit status. This mode is retained for fresh-process regressions and startup measurements.

`--batch` is private conformance IPC. Each newline-delimited request contains only `id`, `schema`, and `wire_base64`; a response contains the same `id`, `ok`, and either `wire_base64` or `error: {code}`. Base64 preserves the original bytes, including malformed UTF-8, duplicate JSON keys and oversized product bodies. The IPC JSON is separate from the product wire contract. Frames are limited to 8 MiB. Public codec body and depth limits remain unchanged.

`scripts/contract-runner.mjs` owns one process per language, one request in flight, a 10-second deadline including first-request initialization, response correlation, bounded stderr, and finite process cleanup. A product refusal is a response and allows subsequent requests; malformed IPC, process crashes and missing responses fail the harness. Closing stdin allows 500 ms for normal exit, then SIGKILL and at most 1.5 seconds to observe termination. A runner is never restarted or a case replayed after an infrastructure failure.

`make test-contract` verifies the same shared corpus in forward and reverse order, with real Go→TS and TS→Go typed roundtrips against independent expected values. `make test` includes lifecycle fault probes; the SDK Schema mutation regression still runs in a fresh Node test process.
