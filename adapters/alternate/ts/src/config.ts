import { isAbsolute } from "node:path";
import { lstatSync, readFileSync } from "node:fs";
import { createPublicKey } from "node:crypto";
import { parseStrict, validateSchema, canonical } from "@harness/sdk";
import { manifest } from "./runtime";
import { object, type Config } from "./types";
import { reject } from "./error";

const id = { $ref: "#/$defs/Id" },
  time = { $ref: "#/$defs/Time" },
  component = { $ref: "#/$defs/ComponentRef" },
  ref = { $ref: "#/$defs/ObjectRef" };
const string = { type: "string", minLength: 1, maxLength: 4096 },
  count = { type: "integer", minimum: 1, maximum: 9007199254740991 },
  boolean = { type: "boolean" };
function closed(properties: Record<string, unknown>, required = Object.keys(properties)): unknown {
  return { type: "object", properties, required, additionalProperties: false };
}
const list = (items: unknown, max = 100) => ({ type: "array", items, maxItems: max });
export const principalSchema = closed({
  subject_id: id,
  generation: count,
  token_hash: { $ref: "#/$defs/Digest" },
  roles: list(string, 20),
  expires_at: time,
  active: boolean,
});
const key = closed({
  kid: string,
  tenant_id: id,
  issuer: id,
  purposes: list(string, 10),
  jwk: closed({
    kty: { const: "EC" },
    crv: { const: "P-256" },
    x: { type: "string", pattern: "^[A-Za-z0-9_-]{43}$" },
    y: { type: "string", pattern: "^[A-Za-z0-9_-]{43}$" },
  }),
});
const schema = closed(
  {
    system: { enum: ["brain", "memory", "executor"] },
    tenant_id: id,
    owner_id: id,
    database: string,
    port: { type: "integer", minimum: 0, maximum: 65535 },
    tls_cert_file: string,
    tls_key_file: string,
    credentials: { ...list(principalSchema), minItems: 1 },
    memory: closed({
      origin: string,
      owner_id: id,
      token_file: string,
      policy_ref: component,
      ca_file: string,
      location: { enum: ["local", "cloud"] },
    }),
    model_profile_ref: component,
    answer_schema_ref: component,
    managed_root: string,
    binding_ref: ref,
    install_lock_ref: component,
    keys: list(key, 32),
    uses: list(manifest.schemas.use_receipt, 100),
    foreign_sources: list(
      closed({
        origin: string,
        owner_id: id,
        database_id: { type: "string", minLength: 1, maxLength: 160 },
        token_file: string,
        ca_file: string,
        key_id: string,
        peer_subject_id: id,
        peer_generation: count,
        purposes: { ...list(string, 16), minItems: 1 },
        location: { enum: ["local", "cloud"] },
      }),
      4,
    ),
    fault: closed(
      {
        drop_response_method: string,
        pause_jobs: boolean,
        crash_after_start: boolean,
        crash_after_foreign_write: boolean,
      },
      [],
    ),
  },
  [
    "system",
    "tenant_id",
    "owner_id",
    "database",
    "port",
    "tls_cert_file",
    "tls_key_file",
    "credentials",
  ],
);
export function privateFile(path: string): Buffer {
  if (!isAbsolute(path)) reject("invalid_request", "absolute_path_required");
  const stat = lstatSync(path);
  if (
    !stat.isFile() ||
    stat.isSymbolicLink() ||
    stat.mode & 0o077 ||
    stat.uid !== process.getuid?.()
  )
    reject("forbidden", "private_owner_file_required");
  return readFileSync(path);
}
export function readConfig(path: string): Config {
  const raw = object(parseStrict(privateFile(path)));
  validateSchema(schema, raw);
  const c = raw as unknown as Config;
  for (const path of [c.database, c.tls_cert_file, c.tls_key_file])
    if (!isAbsolute(path)) reject("invalid_request", "absolute_path_required");
  if (new Set(c.credentials.map((p) => p.subject_id)).size !== c.credentials.length)
    reject("invalid_request", "duplicate_subject");
  if (new Set(c.keys?.map((k) => k.kid)).size !== (c.keys?.length ?? 0))
    reject("invalid_request", "duplicate_verification_key");
  for (const key of c.keys ?? []) {
    if (key.tenant_id !== c.tenant_id || !key.purposes.length)
      reject("invalid_request", "verification_key_binding_required");
    try {
      createPublicKey({ key: key.jwk, format: "jwk" });
    } catch {
      reject("invalid_request", "invalid_verification_key");
    }
  }
  for (const endpoint of [...(c.memory ? [c.memory] : []), ...(c.foreign_sources ?? [])]) {
    const url = new URL(endpoint.origin);
    if (
      url.protocol !== "https:" ||
      url.pathname !== "/" ||
      url.username ||
      url.password ||
      url.search ||
      url.hash
    )
      reject("invalid_request", "fixed_https_origin_required");
    for (const path of [endpoint.token_file, endpoint.ca_file])
      if (!isAbsolute(path)) reject("invalid_request", "absolute_path_required");
  }
  if (c.foreign_sources?.length && c.system !== "memory")
    reject("unsupported", "foreign_host_only_for_memory_profile");
  if (new Set(c.foreign_sources?.map((s) => s.owner_id)).size !== (c.foreign_sources?.length ?? 0))
    reject("invalid_request", "duplicate_foreign_source_owner");
  for (const source of c.foreign_sources ?? []) {
    const key = c.keys?.find((k) => k.kid === source.key_id);
    if (
      source.owner_id === c.owner_id ||
      !key ||
      key.issuer !== source.owner_id ||
      key.tenant_id !== c.tenant_id ||
      !key.purposes.includes("foreign_content") ||
      new Set(source.purposes).size !== source.purposes.length
    )
      reject("invalid_request", "fixed_foreign_source_authority_required");
  }
  if (c.system === "brain" && (!c.memory || !c.model_profile_ref || !c.answer_schema_ref))
    reject("invalid_request", "brain_configuration_required");
  if (
    c.system === "brain" &&
    (canonical(c.model_profile_ref) !== canonical(manifest.brain_profile) ||
      canonical(c.answer_schema_ref) !== canonical(manifest.answer_schema))
  )
    reject("unsupported", "brain_profile_not_registered");
  if (
    c.system === "executor" &&
    (!c.memory ||
      !c.managed_root ||
      !isAbsolute(c.managed_root) ||
      !c.binding_ref ||
      !c.install_lock_ref ||
      !c.keys?.length)
  )
    reject("invalid_request", "executor_configuration_required");
  return c;
}
