import type { ComponentRef, JSONValue, ObjectRef } from "@harness/sdk";
import { ProtocolError } from "@harness/sdk";
export type Document = Record<string, JSONValue>;
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
  fault?: { drop_response_method?: string; pause_jobs?: boolean; crash_after_start?: boolean };
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
