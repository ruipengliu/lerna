import { request } from "node:https";
import { readFileSync } from "node:fs";
import type { Command, JSONValue, Receipt } from "@harness/sdk";
import {
  canonical,
  parseStrict,
  ContractRegistry,
  PROTOCOL,
  PROFILE,
  newID,
  apiError,
} from "@harness/sdk";
import { hash, current, same } from "./authority";
import { privateFile } from "./config";
import { Rejection, reject } from "./error";
import { object, text, integer, type Document, type Principal } from "./types";
import type { Store } from "./store";

interface Outbox {
  command: Command;
  digest: string;
  scope: string;
  schema: string;
  core: string;
  receipt: Receipt | null;
}
interface Publication {
  ref: Document;
  body_base64: string;
  reserve: string;
  put: string;
  transfer: string;
  until: string;
  deadline: string;
  sources: JSONValue[];
  disclosed: JSONValue[];
  published: boolean;
}
export function stable(prefix: string, key: string): string {
  return `${prefix}_${hash(key).slice(7, 39)}`;
}

// 只访问配置中的准确 HTTPS origin；单次调用无重试、重定向或刷新原默认值。
export class ContentPeer {
  private registry?: ContractRegistry;
  private readonly controller = new AbortController();
  constructor(readonly store: Store) {}
  private settings() {
    const m = this.store.config.memory;
    if (!m) reject("dependency_unavailable", "content_owner_unconfigured");
    return m;
  }
  private async http(path: string, body?: Buffer, max = 1048576): Promise<Buffer> {
    this.store.assertCurrent();
    const m = this.settings(),
      token = privateFile(m.token_file).toString("utf8").trim();
    if (!token || token.length > 4096) reject("forbidden", "content_credential_required");
    return await new Promise<Buffer>((resolve, rejectPromise) => {
      const req = request(
        new URL(path, m.origin),
        {
          method: body ? "POST" : "GET",
          signal: this.controller.signal,
          ca: readFileSync(m.ca_file),
          minVersion: "TLSv1.2",
          headers: {
            Authorization: `Bearer ${token}`,
            ...(body
              ? { "Content-Type": "application/octet-stream", "Content-Length": body.length }
              : {}),
          },
        },
        (res) => {
          const chunks: Buffer[] = [];
          let count = 0;
          res.on("data", (chunk) => {
            const b = Buffer.from(chunk);
            count += b.length;
            if (count > max) {
              req.destroy(new Error("response_bound"));
              return;
            }
            chunks.push(b);
          });
          res.once("error", rejectPromise);
          res.once("end", () => {
            const data = Buffer.concat(chunks);
            if (res.statusCode !== 200) {
              try {
                const e = apiError(parseStrict(data));
                rejectPromise(new Rejection(e.code, e.reason));
              } catch {
                rejectPromise(new Rejection("dependency_unavailable", "content_endpoint_failure"));
              }
              return;
            }
            resolve(data);
          });
        },
      );
      req.once("error", () =>
        rejectPromise(new Rejection("dependency_unavailable", "content_transport_unavailable")),
      );
      req.setTimeout(5000, () => req.destroy(new Error("content_timeout")));
      if (body) req.end(body);
      else req.end();
    });
  }
  stop(): void {
    this.controller.abort();
  }
  private async contracts(p: Principal): Promise<ContractRegistry> {
    const d = object(parseStrict(await this.http("/api/discovery"), 1048576)),
      m = this.settings();
    if (
      d.logical_service_id !== m.owner_id ||
      d.identity_scope !== hash(canonical([this.store.config.tenant_id, p.subject_id])) ||
      d.identity_revision !== p.generation
    )
      reject("forbidden", "content_owner_mismatch");
    const registry = await ContractRegistry.create(d, await this.http("/api/schema/core"));
    if (
      this.registry &&
      (this.registry.discovery.identity_scope !== registry.discovery.identity_scope ||
        this.registry.discovery.identity_revision !== registry.discovery.identity_revision ||
        this.registry.methodsDigest !== registry.methodsDigest)
    )
      reject("forbidden", "content_identity_or_contract_changed");
    this.registry = registry;
    return registry;
  }
  async read(p: Principal, ref: unknown, purpose: string): Promise<Buffer> {
    current(this.store, p);
    await this.contracts(p);
    const r = object(ref),
      m = this.settings();
    if (
      r.owner_id !== m.owner_id ||
      r.tenant_id !== this.store.config.tenant_id ||
      integer(r.byte_length) > 262144
    )
      reject("forbidden", "content_scope_mismatch");
    const params = new URLSearchParams({
      ref: Buffer.from(canonical(r)).toString("base64url"),
      purpose,
      location: m.location,
    });
    const b = await this.http(`/api/content?${params}`, undefined, 262144);
    current(this.store, p);
    if (b.length !== r.byte_length || hash(b) !== r.hash)
      reject("forbidden", "content_integrity_mismatch");
    return b;
  }
  async send(
    p: Principal,
    id: string,
    method: string,
    target: string,
    payload: JSONValue,
    deadline: string,
  ): Promise<Receipt> {
    current(this.store, p);
    const registry = await this.contracts(p);
    let saved = this.store.get<Outbox>("outbox", id);
    if (!saved) {
      const command: Command = {
        protocol: PROTOCOL,
        profile: PROFILE,
        logical_service_id: this.settings().owner_id,
        command_id: id,
        method,
        target_id: target,
        expires_at: deadline,
        payload,
      };
      registry.command(command);
      saved = {
        command,
        digest: hash(canonical(command)),
        scope: registry.discovery.identity_scope,
        schema: registry.method(method, "command").schema_digest,
        core: registry.discovery.schema_digest,
        receipt: null,
      };
      this.store.tx(() => {
        current(this.store, p);
        if (!this.store.get("outbox", id)) this.store.put("outbox", id, saved);
      });
    }
    if (
      saved.core !== registry.discovery.schema_digest ||
      saved.scope !== registry.discovery.identity_scope ||
      saved.schema !== registry.method(method, "command").schema_digest ||
      saved.command.method !== method ||
      saved.command.target_id !== target ||
      !same(saved.command.payload, payload) ||
      saved.command.expires_at !== deadline
    )
      reject("idempotency_conflict", "original_outbound_command_changed");
    if (saved.receipt) return saved.receipt;
    let receipt: Receipt;
    try {
      const body = await this.http(
        "/api/call",
        Buffer.from(
          canonical({
            kind: "receipt_lookup",
            payload: { logical_service_id: saved.command.logical_service_id, command_id: id },
          }),
        ),
      );
      receipt = registry.receipt(parseStrict(body), saved.command, saved.digest);
    } catch (error) {
      if (!(error instanceof Rejection) || error.wire.code !== "not_found") throw error;
      current(this.store, p);
      const body = await this.http(
        "/api/call",
        Buffer.from(canonical({ kind: "command", payload: saved.command })),
      );
      receipt = registry.receipt(parseStrict(body), saved.command, saved.digest);
    }
    const original = saved;
    this.store.tx(() => {
      current(this.store, p);
      original.receipt = receipt;
      this.store.put("outbox", id, original);
    });
    return receipt;
  }
  plan(
    p: Principal,
    id: string,
    body: Buffer,
    media: string,
    sources: JSONValue[],
    disclosed: JSONValue[],
    deadline: string,
  ): Document {
    current(this.store, p);
    const old = this.store.get<Publication>("publications", id);
    if (old) {
      if (
        old.body_base64 !== body.toString("base64") ||
        !same(old.sources, sources) ||
        !same(old.disclosed, disclosed) ||
        object(old.ref).media_type !== media
      )
        reject("idempotency_conflict", "original_publication_changed");
      return old.ref;
    }
    const m = this.settings(),
      now = this.store.now(),
      until = new Date(Math.min(Date.parse(deadline), now + 300000)).toISOString(),
      ref: Document = {
        tenant_id: this.store.config.tenant_id,
        owner_id: m.owner_id,
        content_id: id,
        version: 1,
        hash: hash(body),
        media_type: media,
        byte_length: body.length,
      };
    if (body.length > 131072 || Date.parse(until) <= now)
      reject("invalid_request", "publication_bound_or_deadline");
    this.store.put("publications", id, {
      ref,
      body_base64: body.toString("base64"),
      reserve: stable("command", `${id}/reserve`),
      put: stable("command", `${id}/put`),
      transfer: stable("transfer", id),
      until,
      deadline: until,
      sources,
      disclosed,
      published: false,
    });
    return ref;
  }
  async publish(p: Principal, id: string): Promise<Document> {
    current(this.store, p);
    const pub = this.store.require<Publication>("publications", id);
    if (pub.published) return pub.ref;
    const policy = this.settings().policy_ref;
    const reserve = await this.send(
      p,
      pub.reserve,
      "content.upload_reserve",
      text(pub.ref.content_id),
      {
        transfer_id: pub.transfer,
        content_ref: pub.ref,
        policy_ref: policy as unknown as JSONValue,
        processed_sources: pub.sources,
        retention_until: pub.until,
        transfer_deadline: pub.deadline,
      },
      pub.deadline,
    );
    if (reserve.stage !== "applied")
      throw new Rejection(
        reserve.error?.code ?? "invalid_state",
        reserve.error?.reason ?? "content_reservation_not_applied",
      );
    current(this.store, p);
    await this.http(`/api/transfers/${pub.transfer}`, Buffer.from(pub.body_base64, "base64"));
    const put = await this.send(
      p,
      pub.put,
      "content.put",
      text(pub.ref.content_id),
      {
        content_ref: pub.ref,
        transfer_id: pub.transfer,
        policy_ref: policy as unknown as JSONValue,
        processed_sources: pub.sources,
        disclosed_sources: pub.disclosed,
        retention_until: pub.until,
      },
      pub.deadline,
    );
    if (put.stage !== "applied")
      throw new Rejection(
        put.error?.code ?? "invalid_state",
        put.error?.reason ?? "content_publication_not_applied",
      );
    this.store.tx(() => {
      current(this.store, p);
      pub.published = true;
      this.store.put("publications", id, pub);
    });
    return pub.ref;
  }
  async query(p: Principal, method: string, target: string, payload: JSONValue): Promise<unknown> {
    current(this.store, p);
    const registry = await this.contracts(p),
      q = {
        protocol: PROTOCOL,
        profile: PROFILE,
        logical_service_id: this.settings().owner_id,
        query_id: newID("query"),
        method,
        target_id: target,
        payload,
      };
    registry.query(q);
    const body = await this.http(
      "/api/call",
      Buffer.from(canonical({ kind: "query", payload: q })),
    );
    current(this.store, p);
    return registry.validateOutput(method, parseStrict(body));
  }
}
