import { createHash, timingSafeEqual, createPublicKey, verify } from "node:crypto";
import { canonical, parseStrict, validateSchema } from "@harness/sdk";
import { reject } from "./error";
import type { Store } from "./store";
import { object, text, array, integer, type Principal, type Document } from "./types";
import { manifest } from "./runtime";
export function hash(bytes: string | Uint8Array): string {
  return `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
}
export function current(store: Store, original: Principal): Principal {
  store.assertCurrent();
  const p = store.credential(original.subject_id);
  if (
    !p.active ||
    p.generation !== original.generation ||
    p.token_hash !== original.token_hash ||
    !same(p.roles, original.roles) ||
    store.now() >= Date.parse(p.expires_at)
  )
    reject("forbidden", "credential_not_current");
  return p;
}
export function authenticate(store: Store, token: string): Principal {
  if (!token || token.length > 4096) reject("forbidden", "authentication_required");
  const digest = Buffer.from(hash(token));
  for (const row of store.list("credentials", "", 100)) {
    const p = row as unknown as Principal;
    const expected = Buffer.from(p.token_hash);
    if (expected.length === digest.length && timingSafeEqual(expected, digest))
      return current(store, p);
  }
  return reject("forbidden", "authentication_required");
}
export function role(p: Principal, name: string): void {
  if (!p.roles.includes(name)) reject("forbidden", `${name}_required`);
}
export function same(a: unknown, b: unknown): boolean {
  return canonical(a) === canonical(b);
}

function instant(value: string): bigint {
  validateSchema({ $ref: "#/$defs/Time" }, value);
  const match = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?Z$/.exec(value);
  if (!match?.[1]) reject("invalid_request", "invalid_utc_time");
  const ms = Date.parse(`${match[1]}Z`);
  if (!Number.isFinite(ms)) reject("invalid_request", "invalid_utc_time");
  return BigInt(ms) * 1000000n + BigInt((match[2] ?? "").padEnd(9, "0") || "0");
}
export function signed(store: Store, compact: string, expected: Document, window = true): Document {
  if (compact.length > 32768) reject("invalid_request", "proof_too_large");
  const parts = compact.split(".");
  if (parts.length !== 3 || parts.some((p) => !/^[A-Za-z0-9_-]+$/.test(p)))
    reject("forbidden", "invalid_jws");
  const header = object(parseStrict(Buffer.from(parts[0] ?? "", "base64url"))),
    claims = object(parseStrict(Buffer.from(parts[1] ?? "", "base64url")));
  if (Object.keys(header).length !== 3 || header.alg !== "ES256" || header.typ !== "JWS")
    reject("forbidden", "invalid_jws_algorithm");
  const key = store.config.keys?.find((k) => k.kid === header.kid);
  if (!key) reject("forbidden", "unregistered_verification_key");
  const signature = Buffer.from(parts[2] ?? "", "base64url");
  if (
    signature.length !== 64 ||
    !verify(
      "sha256",
      Buffer.from(`${parts[0]}.${parts[1]}`),
      { key: createPublicKey({ key: key.jwk, format: "jwk" }), dsaEncoding: "ieee-p1363" },
      signature,
    )
  )
    reject("forbidden", "invalid_jws_signature");
  validateSchema(manifest.schemas.proof_claims, claims);
  if (
    key.tenant_id !== claims.tenant_id ||
    key.issuer !== claims.issuer ||
    !key.purposes.includes(text(claims.purpose))
  )
    reject("forbidden", "unregistered_proof_binding");
  for (const [k, v] of Object.entries(expected))
    if (!same(claims[k], v)) reject("forbidden", "proof_binding_mismatch");
  const issued = instant(text(claims.issued_at)),
    until = instant(text(claims.start_before)),
    now = BigInt(store.now()) * 1000000n;
  // 本机裁决时钟精度为毫秒；结束界取保守上界，不能扩张纳秒期限。
  if (issued >= until || issued > now || (window && now + 999999n >= until))
    reject("expired", "proof_window_expired");
  return claims;
}
export function use(
  store: Store,
  p: Principal,
  refs: unknown,
  target: Document,
  intentHash: string,
  kind: string,
  window = true,
): Document {
  current(store, p);
  const list = array(refs);
  if (list.length !== 1) reject("forbidden", "exact_original_use_required");
  const ref = object(list[0]);
  if (ref.tenant_id !== store.config.tenant_id || ref.revision !== 1)
    reject("forbidden", "use_reference_mismatch");
  const receipt = store.require("uses", text(ref.object_id));
  validateSchema(manifest.schemas.use_receipt, receipt);
  const subject = object(receipt.subject_ref);
  if (
    receipt.decision !== "allowed" ||
    receipt.target_kind !== kind ||
    receipt.intent_hash !== intentHash ||
    !same(receipt.target_ref, target) ||
    subject.object_id !== p.subject_id ||
    subject.revision !== p.generation ||
    subject.tenant_id !== store.config.tenant_id ||
    receipt.recipient !== store.config.owner_id ||
    receipt.location !== "local" ||
    !array(receipt.purposes).includes(kind === "operation" ? "execute" : "decision")
  )
    reject("forbidden", "original_use_binding_mismatch");
  for (const r of [ref, ...array(receipt.grant_refs), subject]) {
    const obj = object(r);
    if (store.get("denials", `${text(obj.owner_id)}:${text(obj.object_id)}`))
      reject("forbidden", "known_authority_revoked");
  }
  const unsigned = { ...receipt };
  delete unsigned.proof;
  delete unsigned.proof_ref;
  signed(
    store,
    text(receipt.proof),
    {
      tenant_id: store.config.tenant_id,
      issuer: ref.owner_id ?? null,
      audience: store.config.owner_id,
      purpose: "grant_use",
      object_ref: ref,
      digest: hash(canonical(unsigned)),
      control_revision: 0,
      window_id: ref.object_id ?? null,
      issued_at: receipt.issued_at ?? null,
      start_before: receipt.start_before ?? null,
    },
    window,
  );
  integer(ref.revision);
  return receipt;
}
