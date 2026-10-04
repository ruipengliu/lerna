# 1.1.0 typed fixture runner

This independent runner decodes a named 1.1.0 public Go type and encodes that
same type. `DecideInput`, `GetInput`, `CancelInput`, and `CommandInput` also
exercise their method-specific public decoders. `--batch` preserves raw JSON
bytes in a private bounded base64 IPC frame and supports one request at a time.

`node scripts/test-contract-1_1.mjs` builds the runner in a temporary directory,
uses the TypeScript counterpart, checks both directions and canonical byte
preservation, and joins or kills both processes under finite deadlines. It is
also run by the existing shared contract command. Generated `types.go` must be
changed through the schemas and `scripts/generate.mjs`.
