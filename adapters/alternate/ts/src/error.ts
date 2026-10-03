import type { APIError } from "@harness/sdk";
export class Rejection extends Error {
  readonly wire: APIError;
  constructor(code: APIError["code"], reason: string) {
    super(`${code}:${reason}`);
    const retry = [
      "forbidden",
      "revision_conflict",
      "invalid_state",
      "cursor_expired",
      "snapshot_required",
    ].includes(code)
      ? "after_change"
      : ["overloaded", "dependency_unavailable"].includes(code)
        ? "backoff"
        : ["effect_unknown", "accounting_unknown"].includes(code)
          ? "query_original"
          : "none";
    this.wire = { code, scope: "request", reason, retry };
  }
}
export function reject(code: APIError["code"], reason: string): never {
  throw new Rejection(code, reason);
}
export function failure(error: unknown): APIError {
  if (error instanceof Rejection) return error.wire;
  if (error instanceof Error && error.name === "ProtocolError")
    return { code: "invalid_request", scope: "request", reason: "schema_violation", retry: "none" };
  return {
    code: "dependency_unavailable",
    scope: "request",
    reason: "local_dependency_failure",
    retry: "backoff",
  };
}
