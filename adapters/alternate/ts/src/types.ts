import type { ComponentRef, ContentRef, JSONValue, ObjectRef } from "@harness/sdk";
import { ProtocolError } from "@harness/sdk";
export type Document = Record<string, JSONValue>;
export interface PeerEndpoint {
  origin: string;
  owner_id: string;
  token_file: string;
  ca_file: string;
  location: "local" | "cloud";
  policy_ref?: ComponentRef;
  peer_subject_id?: string;
  peer_generation?: number;
}
export interface ForeignSource extends PeerEndpoint {
  database_id: string;
  key_id: string;
  peer_subject_id: string;
  peer_generation: number;
  purposes: string[];
}
// Runtime Schema 由 Go 实际 Source.Register 的单一合同源派生。
export interface ForeignReference {
  content_ref: ContentRef;
  copy_id: string;
  register_command_id: string;
  release_command_id: string;
  reference_intent_ref: ObjectRef;
  holder_ref: ObjectRef;
  purpose: string;
  location: string;
  retain_until: string;
}
export interface ForeignUse {
  reference: ForeignReference;
  proof: Document;
}
export interface Principal {
  subject_id: string;
  generation: number;
  token_hash: string;
  roles: string[];
  expires_at: string;
  active: boolean;
}
export interface Config {
  system: "brain" | "memory" | "executor";
  tenant_id: string;
  owner_id: string;
  database: string;
  expected_database_id?: string;
  port: number;
  tls_cert_file: string;
  tls_key_file: string;
  credentials: Principal[];
  memory?: {
    origin: string;
    owner_id: string;
    token_file: string;
    policy_ref: ComponentRef;
    ca_file: string;
    location: "local" | "cloud";
  };
  model_profile_ref?: ComponentRef;
  answer_schema_ref?: ComponentRef;
  managed_root?: string;
  binding_ref?: ObjectRef;
  install_lock_ref?: ComponentRef;
  keys?: {
    kid: string;
    tenant_id: string;
    issuer: string;
    purposes: string[];
    jwk: { kty: "EC"; crv: "P-256"; x: string; y: string };
  }[];
  uses?: Document[];
  foreign_sources?: ForeignSource[];
  fault?: {
    drop_response_method?: string;
    pause_jobs?: boolean;
    crash_after_start?: boolean;
    crash_after_foreign_write?: boolean;
  };
}
export function object(value: unknown): Document {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new ProtocolError("object_required");
  return value as Document;
}
export function text(value: unknown): string {
  if (typeof value !== "string") throw new ProtocolError("string_required");
  return value;
}
export function integer(value: unknown): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0)
    throw new ProtocolError("integer_required");
  return value;
}
export function array(value: unknown): JSONValue[] {
  if (!Array.isArray(value)) throw new ProtocolError("array_required");
  return value as JSONValue[];
}
export function strings(value: unknown): string[] {
  return array(value).map(text);
}
