import type { JSONValue } from "./json";
export const PROTOCOL = "harness/1" as const;
export const PROFILE = "architecture-2026-10-data1" as const;
export const TRANSPORT_PROFILE = "harness-wss/1" as const;
export const SOCKET_SUBPROTOCOL = "harness-wss.v1" as const;

export interface Command<P = JSONValue> {
  protocol: typeof PROTOCOL;
  profile: typeof PROFILE;
  logical_service_id: string;
  command_id: string;
  method: string;
  target_id: string;
  expires_at: string;
  expected_revision?: number;
  payload: P;
}
export interface Query<P = JSONValue> {
  protocol: typeof PROTOCOL;
  profile: typeof PROFILE;
  logical_service_id: string;
  query_id: string;
  method: string;
  target_id: string;
  payload: P;
}
export interface APIError {
  code: string;
  scope: string;
  reason: string;
  retry: "none" | "same_request" | "query_original" | "after_change" | "backoff";
  detail?: string;
}
export class RequestError extends Error {
  constructor(public readonly error: APIError) {
    super(`${error.code}: ${error.reason}`);
    this.name = "RequestError";
  }
}
export interface Receipt<O = JSONValue> {
  command_id: string;
  request_digest: string;
  stage: "accepted" | "applied" | "rejected";
  accepted_at?: string;
  decided_at?: string;
  output?: O;
  error?: APIError;
}
export interface ReceiptLookup {
  logical_service_id: string;
  command_id: string;
}
export type Schema = Record<string, JSONValue>;
export interface MethodContract {
  name: string;
  owner: string;
  kind: "command" | "query";
  cas: boolean;
  allows_accepted: boolean;
  input_schema: Schema;
  output_schema: Schema;
  schema_digest: string;
  recovery: string;
}
export interface Limits {
  max_domain_bytes: number;
  max_frame_bytes: number;
  max_pending: number;
}
export interface Discovery {
  protocol: typeof PROTOCOL;
  profile: typeof PROFILE;
  logical_service_id: string;
  identity_scope: string;
  identity_revision: number;
  schema_digest: string;
  methods: MethodContract[];
  limits: Limits;
  core_schema_path: string;
  core_schema?: JSONValue;
}
export interface PublicDiscovery {
  protocol: typeof PROTOCOL;
  transport_profile: typeof TRANSPORT_PROFILE;
  login_path: string;
  connect_path: string;
}
export interface Ready {
  type: "ready";
  connection_id: string;
  logical_service_id: string;
  profile: typeof PROFILE;
  transport_profile: typeof TRANSPORT_PROFILE;
  methods_digest: string;
  limits: Limits;
}
export interface RequestFrame {
  type: "request";
  request_seq: number;
  kind: "command" | "query" | "receipt_lookup";
  payload: Command | Query | ReceiptLookup;
}
export interface ResponseFrame {
  type: "response";
  request_seq: number;
  result_kind: "receipt" | "query_result" | "error";
  payload: JSONValue;
}
export type ConnectionState = "disconnected" | "connecting" | "ready" | "closed";
