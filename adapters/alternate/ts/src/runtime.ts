import type { Command, Query, Receipt, Discovery, MethodContract, JSONValue } from "@harness/sdk";
import {
  ContractRegistry,
  PROTOCOL,
  PROFILE,
  canonical,
  digestLimit,
  validateSchema,
} from "@harness/sdk";
import frozen from "./contracts.gen.json";
import { hash, current } from "./authority";
import { Rejection, reject } from "./error";
import type { Store } from "./store";
import type { Principal } from "./types";

export const manifest = frozen;
export interface Ledger {
  command: Command;
  digest: string;
  principal: Principal;
  receipt: Receipt;
}
export interface Handler {
  command(p: Principal, c: Command, prepared?: unknown): { output: unknown; accepted?: boolean };
  prepare?(p: Principal, c: Command): Promise<unknown>;
  query(p: Principal, q: Query): unknown | Promise<unknown>;
  disclose?(p: Principal, q: Query, result: unknown): void;
  receive?(p: Principal, id: string, body: Buffer): unknown;
  bytes?(p: Principal, ref: unknown, purpose: string): Buffer;
  advance?(id: string): Promise<void>;
  stop?(): Promise<void>;
}
export class Runtime {
  registry!: ContractRegistry;
  constructor(
    readonly store: Store,
    readonly handler: Handler,
  ) {}
  discovery(p: Principal): Discovery {
    const methods = manifest.methods[this.store.config.system] as unknown as MethodContract[];
    return {
      protocol: PROTOCOL,
      profile: PROFILE,
      logical_service_id: this.store.config.owner_id,
      identity_scope: hash(canonical([this.store.config.tenant_id, p.subject_id])),
      identity_revision: p.generation,
      schema_digest: manifest.core_digest,
      core_schema_path: "/api/schema/core",
      methods,
      methods_digest: this.methodsDigest,
      limits: { max_domain_bytes: 262144, max_frame_bytes: 1048576, max_pending: 32 },
    };
  }
  private methodsDigest = "";
  async initialize(): Promise<void> {
    const methods = manifest.methods[this.store.config.system];
    this.methodsDigest = await digestLimit(methods, 1048576);
    const initial = this.store.config.credentials[0];
    if (!initial) reject("invalid_request", "credential_configuration_required");
    this.registry = await ContractRegistry.create(
      this.discovery(initial),
      new TextEncoder().encode(manifest.core_source),
    );
  }
  async command(p: Principal, value: unknown): Promise<Receipt> {
    validateSchema(manifest.schemas.command, value);
    const c = value as Command;
    if (c.logical_service_id !== this.store.config.owner_id) reject("forbidden", "owner_mismatch");
    const digest = hash(canonical(c));
    const original = this.store.get<Ledger>("commands", c.command_id);
    let prepared: unknown, preparationError: unknown;
    if (!original && this.handler.prepare) {
      try {
        this.registry.command(c);
        prepared = await this.handler.prepare(p, c);
      } catch (error) {
        preparationError = error;
      }
    }
    return this.store.tx(
      () => {
        current(this.store, p);
        const old = this.store.get<Ledger>("commands", c.command_id);
        if (old) {
          if (old.digest !== digest || old.principal.subject_id !== p.subject_id)
            reject("idempotency_conflict", "command_input_changed");
          return old.receipt;
        }
        let receipt: Receipt;
        try {
          if (
            !this.registry.discovery.methods.some(
              (m) => m.name === c.method && m.kind === "command",
            )
          )
            reject("unsupported", "method_not_supported");
          this.registry.command(c);
          if (preparationError) throw preparationError;
          if (this.store.now() >= Date.parse(c.expires_at)) reject("expired", "command_expired");
          this.store.db.exec("SAVEPOINT business");
          let outcome: { output: unknown; accepted?: boolean };
          try {
            outcome = this.handler.command(p, c, prepared);
            this.registry.validateOutput(c.method, outcome.output);
            this.store.db.exec("RELEASE business");
          } catch (error) {
            this.store.db.exec("ROLLBACK TO business; RELEASE business");
            throw error;
          }
          receipt = {
            command_id: c.command_id,
            request_digest: digest,
            stage: outcome.accepted ? "accepted" : "applied",
            [outcome.accepted ? "accepted_at" : "decided_at"]: new Date(
              this.store.now(),
            ).toISOString(),
            output: outcome.output as JSONValue,
          };
        } catch (error) {
          if (
            !(error instanceof Rejection) &&
            !(error instanceof Error && error.name === "ProtocolError")
          )
            throw error;
          if (
            error instanceof Rejection &&
            ["overloaded", "dependency_unavailable", "effect_unknown"].includes(error.wire.code)
          )
            throw error;
          receipt = {
            command_id: c.command_id,
            request_digest: digest,
            stage: "rejected",
            decided_at: new Date(this.store.now()).toISOString(),
            error:
              error instanceof Rejection
                ? error.wire
                : {
                    code: "invalid_request",
                    scope: "request",
                    reason: "schema_violation",
                    retry: "none",
                  },
          };
        }
        this.store.put("commands", c.command_id, { command: c, digest, principal: p, receipt });
        return receipt;
      },
      true,
      controlMethod(c.method),
    );
  }
  async query(p: Principal, value: unknown): Promise<unknown> {
    validateSchema(manifest.schemas.query, value);
    const q = value as Query;
    if (q.logical_service_id !== this.store.config.owner_id) reject("forbidden", "owner_mismatch");
    current(this.store, p);
    if (!this.registry.discovery.methods.some((m) => m.name === q.method && m.kind === "query"))
      reject("unsupported", "method_not_supported");
    this.registry.query(q);
    const prior = this.store.get<{ digest: string; subject: string; result: unknown }>(
      "queries",
      q.query_id,
    );
    const digest = hash(canonical(q));
    if (prior && (prior.digest !== digest || prior.subject !== p.subject_id))
      reject("idempotency_conflict", "query_input_changed");
    // 当前披露门禁即使在原 query_id 已封存时也重新执行。
    const result = await this.handler.query(p, q);
    current(this.store, p);
    this.registry.validateOutput(q.method, result);
    return this.store.tx(() => {
      current(this.store, p);
      this.handler.disclose?.(p, q, result);
      const sealed = this.store.get<{ digest: string; subject: string; result: unknown }>(
        "queries",
        q.query_id,
      );
      if (sealed) {
        if (sealed.digest !== digest || sealed.subject !== p.subject_id)
          reject("idempotency_conflict", "query_input_changed");
        this.handler.disclose?.(p, q, sealed.result);
        if (canonical(sealed.result) !== canonical(result))
          reject("revision_conflict", "original_query_changed_new_query_required");
        return sealed.result;
      }
      this.store.put("queries", q.query_id, { digest, subject: p.subject_id, result });
      return result;
    });
  }
  lookup(p: Principal, value: unknown): Receipt {
    const schema = {
      type: "object",
      properties: {
        logical_service_id: { $ref: "#/$defs/Id" },
        command_id: { $ref: "#/$defs/Id" },
      },
      required: ["logical_service_id", "command_id"],
      additionalProperties: false,
    };
    validateSchema(schema, value);
    const ref = value as { logical_service_id: string; command_id: string };
    current(this.store, p);
    if (ref.logical_service_id !== this.store.config.owner_id)
      reject("forbidden", "owner_mismatch");
    const saved = this.store.require<Ledger>("commands", ref.command_id);
    if (saved.principal.subject_id !== p.subject_id && !p.roles.includes("admin"))
      reject("forbidden", "receipt_not_disclosed");
    return saved.receipt;
  }
}

export function controlMethod(method: string): boolean {
  return (
    method === "execution.control" ||
    method === "content.release_copy" ||
    method === "memory.delete" ||
    /\.(cancel|pause|revoke|close|stop|takeover|deactivate|billing_reconcile)$/.test(method)
  );
}
