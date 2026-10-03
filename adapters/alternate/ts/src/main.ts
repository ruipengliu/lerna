import { parseStrict, validateSchema, canonical } from "@harness/sdk";
import { object, text, type Document } from "./types";
import { Store } from "./store";
import { reject } from "./error";
import { Memory } from "./memory";
import { Brain } from "./brain";
import { Executor } from "./executor";
import { Runtime, manifest } from "./runtime";
import { serve } from "./transport";
import { readConfig, privateFile, principalSchema } from "./config";
import { signed, hash } from "./authority";
import { NativeHost } from "./foreign";
import type { ForeignReference } from "./types";

const mode = process.argv[2],
  configFile = argument("--config");
if (
  ![
    "migrate",
    "admin",
    "serve",
    "prepare-foreign-use",
    "control-foreign-copy",
    "stop-foreign-copy",
    "inspect-foreign-copy",
  ].includes(mode ?? "")
)
  throw new Error("mode must be migrate, serve or admin");
const config = readConfig(configFile),
  store = new Store(config, mode === "migrate", mode !== "serve");
if (mode === "migrate") {
  store.close();
} else if (mode === "admin") {
  const namespace = argument("--namespace"),
    record = object(parseStrict(privateFile(argument("--record"))));
  store.tx(() => {
    if (namespace === "credentials") {
      validateSchema(principalSchema, record);
      const id = text(record.subject_id),
        old = store.require("credentials", id);
      if (
        Number(record.generation) < Number(old.generation) ||
        (!old.active && record.active && record.generation === old.generation)
      )
        reject("revision_conflict", "credential_generation_required");
      if (
        record.generation === old.generation &&
        ["token_hash", "roles", "expires_at"].some(
          (k) => canonical(record[k]) !== canonical(old[k]),
        )
      )
        reject("revision_conflict", "credential_generation_required");
      store.put(namespace, id, record);
    } else if (namespace === "denials") {
      validateSchema(
        {
          type: "object",
          properties: {
            ref: { $ref: "#/$defs/ObjectRef" },
            reason: { type: "string", minLength: 1, maxLength: 200 },
          },
          required: ["ref", "reason"],
          additionalProperties: false,
        },
        record,
      );
      const r = object(record.ref);
      if (r.tenant_id !== config.tenant_id) reject("forbidden", "tenant_mismatch");
      const id = `${text(r.owner_id)}:${text(r.object_id)}`;
      if (!store.get(namespace, id)) store.put(namespace, id, record);
    } else if (namespace === "uses") {
      validateSchema(manifest.schemas.use_receipt, record);
      const compact = text(record.proof),
        claims = object(parseStrict(Buffer.from(compact.split(".")[1] ?? "", "base64url"))),
        target = object(record.target_ref);
      if (
        target.tenant_id !== config.tenant_id ||
        target.owner_id !== config.owner_id ||
        record.decision !== "allowed"
      )
        reject("forbidden", "use_owner_mismatch");
      const unsigned: Document = { ...record };
      delete unsigned.proof;
      delete unsigned.proof_ref;
      signed(store, compact, {
        tenant_id: config.tenant_id,
        issuer: claims.issuer ?? null,
        audience: config.owner_id,
        purpose: "grant_use",
        object_ref: {
          tenant_id: config.tenant_id,
          owner_id: claims.issuer ?? null,
          object_id: record.use_id ?? null,
          revision: 1,
        },
        digest: hash(canonical(unsigned)),
        control_revision: 0,
        window_id: record.use_id ?? null,
        issued_at: record.issued_at ?? null,
        start_before: record.start_before ?? null,
      });
      const id = text(record.use_id),
        old = store.get(namespace, id);
      if (old && canonical(old) !== canonical(record))
        reject("idempotency_conflict", "original_use_changed");
      if (!old) store.put(namespace, id, record);
    } else reject("unsupported", "management_namespace_not_supported");
  });
  store.close();
  process.stdout.write(`${canonical({ updated: true, namespace })}\n`);
} else if (mode?.endsWith("foreign-copy") || mode === "prepare-foreign-use") {
  if (config.system !== "memory") reject("unsupported", "foreign_host_only_for_memory");
  const host = new NativeHost(store),
    principal = store.credential(argument("--subject"));
  try {
    let result: unknown;
    if (mode === "prepare-foreign-use") {
      const reference = parseStrict(privateFile(argument("--record")));
      validateSchema(manifest.schemas.foreign_reference, reference);
      result = await host.prepareForeignUse(principal, reference as unknown as ForeignReference);
    } else if (mode === "control-foreign-copy")
      result = await host.controlForeignCopy(principal, argument("--copy-id"));
    else if (mode === "stop-foreign-copy") {
      await host.stopForeignCopy(principal, argument("--copy-id"));
      result = host.foreign.inspect(principal, argument("--copy-id"));
    } else result = host.foreign.inspect(principal, argument("--copy-id"));
    process.stdout.write(`${canonical(result)}\n`);
  } finally {
    await host.foreign.close();
    store.close();
  }
} else if (mode === "serve") {
  const handler =
    config.system === "memory"
      ? new Memory(store)
      : config.system === "brain"
        ? new Brain(store)
        : new Executor(store);
  const runtime = new Runtime(store, handler);
  await runtime.initialize();
  await serve(runtime);
  handler.start();
} else {
  store.close();
  throw new Error("mode must be migrate, serve or admin");
}
function argument(name: string): string {
  const i = process.argv.indexOf(name),
    v = process.argv[i + 1];
  if (i < 0 || !v) throw new Error(`${name} is required`);
  return v;
}
