import type { Command, Query, JSONValue } from "@harness/sdk";
import { canonical, parseStrict, validateSchema } from "@harness/sdk";
import { createHmac, randomBytes } from "node:crypto";
import { current, hash, role, same } from "./authority";
import { reject, Rejection } from "./error";
import { manifest, type Handler } from "./runtime";
import type { Store } from "./store";
import { object, text, integer, strings, array, type Principal, type Document } from "./types";

const MAX_BYTES = 262144;
interface Snapshot {
  subject: string;
  generation: number;
  digest: string;
  visibility: string;
  expires: number;
  refs: Document[];
  items: JSONValue[];
  revision: number;
  partial: boolean;
  gaps: string[];
}
interface View {
  view_id: string;
  principal: Principal;
  scope: Document;
  purposes: string[];
  expires: number;
  visibility: string;
  snapshot: Document[];
  head: number;
  cursor: string;
  ack: string;
  issuedFrom: string;
  issued: Document | null;
  partial: boolean;
  gaps: string[];
}

// 独立的内容/记忆权威；不调用 Go Memory Service，也不把索引视为正文授权。
export class Memory implements Handler {
  private timer: ReturnType<typeof setInterval> | undefined;
  private permissionRemaining: number | undefined;
  constructor(readonly store: Store) {
    if (!store.meta("cursor_key"))
      store.tx(() => store.meta("cursor_key", randomBytes(32).toString("hex")));
  }
  start(): void {
    this.timer = setInterval(() => {
      try {
        this.expire();
      } catch {
        if (this.timer) clearInterval(this.timer);
      }
    }, 500);
  }
  async stop(): Promise<void> {
    if (this.timer) clearInterval(this.timer);
  }
  private expire(): void {
    const due = this.store
      .list("expiry_work", "", 1000)
      .filter((w) => w.state === "pending" && Date.parse(text(w.deadline)) <= this.store.now())
      .slice(0, 20);
    if (!due.length) return;
    this.store.tx(
      () => {
        for (const work of due) {
          const id = text(work.id),
            kind = text(work.kind);
          if (kind === "content") {
            const key = text(work.key),
              c = this.store.get("contents", key);
            if (c && c.state === "active") {
              c.state = "closed";
              c.control_revision = integer(c.control_revision) + 1;
              this.store.put("contents", key, c);
              this.store.db.prepare("DELETE FROM content_bytes WHERE key=?").run(key);
              this.impact(key);
            }
          } else if (kind === "copy") {
            const holder = this.store.get("copies", text(work.key));
            if (holder?.use_state === "allowed") {
              holder.use_state = "closing";
              this.store.put("copies", text(work.key), holder);
            }
          } else {
            const t = this.store.get("transfers", text(work.key));
            if (t && t.phase !== "published" && t.phase !== "cleanup") {
              t.phase = "cleanup";
              this.store.put("transfers", text(work.key), t);
              const key = this.key(t.content_ref);
              if (!this.store.get("contents", key))
                this.store.db.prepare("DELETE FROM content_bytes WHERE key=?").run(key);
            }
          }
          work.state = "complete";
          this.store.put("expiry_work", id, work);
        }
      },
      true,
      true,
    );
  }
  ref(id: string, revision: number): Document {
    return {
      tenant_id: this.store.config.tenant_id,
      owner_id: this.store.config.owner_id,
      object_id: id,
      revision,
    };
  }
  key(value: unknown): string {
    const r = object(value);
    validateSchema({ $ref: "#/$defs/ContentRef" }, r);
    if (r.tenant_id !== this.store.config.tenant_id) reject("forbidden", "tenant_mismatch");
    if (r.owner_id !== this.store.config.owner_id)
      reject("dependency_unavailable", "foreign_content_registration_required");
    return `${text(r.content_id)}:${integer(r.version)}`;
  }
  head(): number {
    return Number(this.store.meta("memory_head") || 0);
  }
  visibility(p: Principal): string {
    return hash(
      canonical([
        p.subject_id,
        p.generation,
        this.store.list("policies"),
        this.store
          .list("contents")
          .map((c) => [c.content_ref, c.state, c.control_revision, c.retention_until]),
      ]),
    );
  }
  target(target: string, id: unknown): void {
    if (target !== text(id)) reject("invalid_request", "target_mismatch");
  }
  future(value: unknown): number {
    validateSchema({ $ref: "#/$defs/Time" }, value);
    const n = Date.parse(text(value));
    if (!Number.isFinite(n) || n <= this.store.now()) reject("expired", "deadline_expired");
    return n;
  }
  allowed(p: Principal, ref: unknown, purpose: string, continuous = false): Document {
    if (this.permissionRemaining !== undefined) {
      if (this.permissionRemaining < 1) reject("overloaded", "permission_check_limit");
      this.permissionRemaining--;
    }
    current(this.store, p);
    const r = object(ref),
      policy = this.store.require("policies", text(r.component_id)),
      v = object(policy.values);
    if (
      !same(policy.policy_ref, r) ||
      policy.state !== "active" ||
      Date.parse(text(v.retain_until)) <= this.store.now() ||
      !strings(v.subjects).includes(p.subject_id) ||
      !strings(v.purposes).includes(purpose) ||
      !strings(v.locations).includes("local") ||
      (continuous && !v.continuous)
    )
      reject("forbidden", "policy_not_currently_allowed");
    return v;
  }
  content(
    p: Principal,
    ref: unknown,
    purpose: string,
    continuous = false,
    visited = new Set<string>(),
  ): Document {
    const key = this.key(ref);
    if (visited.has(key) || visited.size >= 64) reject("invalid_request", "source_cycle_or_depth");
    visited.add(key);
    const c = this.store.require("contents", key);
    if (!same(c.content_ref, ref)) reject("idempotency_conflict", "content_reference_changed");
    if (c.state !== "active" || Date.parse(text(c.retention_until)) <= this.store.now())
      reject("gone", "content_closed_or_expired");
    this.allowed(p, c.policy_ref, purpose, continuous);
    for (const r of [...array(c.processed_sources), ...array(c.disclosed_sources)])
      this.content(p, r, purpose, continuous, new Set(visited));
    return c;
  }
  bytes(p: Principal, ref: unknown, purpose: string, continuous = false): Buffer {
    this.content(p, ref, purpose, continuous);
    const r = object(ref),
      row = this.store.db.prepare("SELECT body FROM content_bytes WHERE key=?").get(this.key(ref));
    if (!row || !(row.body instanceof Uint8Array))
      reject("dependency_unavailable", "original_bytes_unavailable");
    const body = Buffer.from(row.body);
    if (body.length !== r.byte_length || hash(body) !== r.hash)
      reject("dependency_unavailable", "stored_bytes_integrity_failure");
    return body;
  }
  transfer(p: Principal, id: string, write = true): Document {
    const t = this.store.require("transfers", id);
    if (t.publisher !== p.subject_id || t.generation !== p.generation)
      reject("forbidden", "transfer_principal_mismatch");
    if (write) {
      this.future(t.expires_at);
      this.future(t.retention_until);
      this.allowed(p, t.policy_ref, "content.write");
      for (const r of array(t.processed_sources)) this.content(p, r, "content.write");
    }
    return t;
  }
  transferStatus(t: Document): Document {
    return {
      transfer_id: t.transfer_id ?? null,
      content_ref: t.content_ref ?? null,
      phase: t.phase ?? null,
      expires_at: t.expires_at ?? null,
      durability: ["ready", "published"].includes(text(t.phase)) ? "local_fsync" : "",
    };
  }
  receive(p: Principal, id: string, body: Buffer): Document {
    if (body.length > MAX_BYTES) reject("invalid_request", "content_too_large");
    return this.store.tx(() => {
      current(this.store, p);
      const t = this.transfer(p, id),
        r = object(t.content_ref);
      if (body.length !== r.byte_length || hash(body) !== r.hash)
        reject("invalid_request", "content_hash_or_length_mismatch");
      if (["ready", "published"].includes(text(t.phase))) return this.transferStatus(t);
      if (t.phase !== "reserved") reject("invalid_state", "transfer_closed");
      // SQLite FULL 提交同时保存准确 BLOB 与 ready；无外部介质启动窗口。
      const key = this.key(r),
        old = this.store.db.prepare("SELECT body FROM content_bytes WHERE key=?").get(key);
      if (old && (!(old.body instanceof Uint8Array) || !Buffer.from(old.body).equals(body)))
        reject("idempotency_conflict", "content_version_changed");
      if (
        !old &&
        Number(
          this.store.db
            .prepare("SELECT coalesce(sum(length(body)),0) AS n FROM content_bytes")
            .get()?.n,
        ) +
          body.length >
          32 * 1048576
      )
        reject("overloaded", "content_byte_capacity");
      this.store.db
        .prepare("INSERT INTO content_bytes VALUES(?,?) ON CONFLICT(key) DO NOTHING")
        .run(key, body);
      t.phase = "ready";
      this.store.put("transfers", id, t);
      return this.transferStatus(t);
    });
  }
  checkValues(p: Principal, v: Document): void {
    if (!["fact", "preference"].includes(text(v.type)))
      reject("unsupported", "memory_type_not_in_profile");
    if (
      v.confidence !== undefined &&
      (typeof v.confidence !== "number" || v.confidence < 0 || v.confidence > 1)
    )
      reject("invalid_request", "invalid_confidence");
    if (
      v.valid_from &&
      v.valid_to &&
      Date.parse(text(v.valid_from)) >= Date.parse(text(v.valid_to))
    )
      reject("invalid_request", "invalid_validity_interval");
    const policy = this.allowed(p, v.policy_ref, "memory.save");
    const refs = [
      v.content_ref,
      v.scope_ref,
      ...array(v.sources).map((s) => object(s).content_ref),
    ];
    if (refs.length > 32) reject("invalid_request", "source_count_limit");
    for (const r of refs) {
      const content = this.content(p, r, "memory.save"),
        source = this.allowed(p, content.policy_ref, "memory.save");
      for (const property of ["subjects", "purposes", "locations"]) {
        if (strings(policy[property]).some((s) => !strings(source[property]).includes(s)))
          reject("forbidden", "source_scope_expansion");
      }
      if (Date.parse(text(policy.retain_until)) > Date.parse(text(source.retain_until)))
        reject("forbidden", "source_retention_expansion");
    }
    const seen = new Set<string>();
    for (const s of array(v.sources)) {
      const e = object(s),
        k = canonical(e.content_ref);
      if (seen.has(k)) reject("invalid_request", "duplicate_source");
      seen.add(k);
      if (e.submission_ref) {
        const ref = object(e.submission_ref);
        if (ref.tenant_id !== this.store.config.tenant_id)
          reject("forbidden", "source_submission_tenant_mismatch");
      }
    }
  }
  record(p: Principal, record: Document, purpose = "memory.read", continuous = false): Document {
    if (record.state === "deleted") reject("gone", "memory_deleted");
    if (record.state !== "active") reject("forbidden", "memory_not_active");
    const v = object(record.values);
    this.allowed(p, v.policy_ref, purpose, continuous);
    for (const r of [
      v.content_ref,
      v.scope_ref,
      ...array(v.sources).map((s) => object(s).content_ref),
    ])
      this.content(p, r, purpose, continuous);
    return record;
  }
  change(record: Document, kind: string): number {
    const head = this.head() + 1;
    this.store.meta("memory_head", String(head));
    this.store.put("changes", String(head).padStart(16, "0"), {
      memory_id: record.memory_id ?? null,
      revision: record.revision ?? null,
      change_seq: head,
      kind,
    });
    return head;
  }
  command(p: Principal, c: Command): { output: unknown; accepted?: boolean } {
    const i = object(c.payload);
    switch (c.method) {
      case "content.policy.install": {
        role(p, "memory_admin");
        const ref = object(i.policy_ref),
          v = object(i.values);
        this.target(c.target_id, ref.component_id);
        if (hash(canonical(v)) !== ref.digest || i.revision !== 1 || i.state !== "active")
          reject("invalid_request", "policy_identity_mismatch");
        strings(v.subjects);
        strings(v.purposes);
        strings(v.locations);
        this.future(v.retain_until);
        const id = text(ref.component_id),
          old = this.store.get("policies", id);
        if (old && !same(old, i)) reject("idempotency_conflict", "policy_input_changed");
        if (!old) this.store.put("policies", id, i);
        return { output: { policy_ref: ref } };
      }
      case "content.upload_reserve": {
        const ref = object(i.content_ref);
        this.target(c.target_id, ref.content_id);
        this.key(ref);
        if (integer(ref.byte_length) > MAX_BYTES) reject("invalid_request", "content_too_large");
        const policy = this.allowed(p, i.policy_ref, "content.write");
        const until = this.future(i.retention_until),
          deadline = this.future(i.transfer_deadline);
        if (
          until > Date.parse(text(policy.retain_until)) ||
          deadline > until ||
          deadline > this.store.now() + 300000
        )
          reject("forbidden", "transfer_scope_expansion");
        for (const s of array(i.processed_sources)) this.content(p, s, "content.write");
        const id = text(i.transfer_id),
          t: Document = {
            ...i,
            transfer_id: id,
            expires_at: i.transfer_deadline ?? null,
            phase: "reserved",
            publisher: p.subject_id,
            generation: p.generation,
          };
        delete t.transfer_deadline;
        const old = this.store.get("transfers", id);
        if (
          old &&
          (!same(old.content_ref, ref) ||
            !same(old.policy_ref, i.policy_ref) ||
            !same(old.processed_sources, i.processed_sources) ||
            old.retention_until !== i.retention_until ||
            old.expires_at !== i.transfer_deadline ||
            old.publisher !== p.subject_id ||
            old.generation !== p.generation)
        )
          reject("idempotency_conflict", "transfer_input_changed");
        if (!old) {
          this.store.put("transfers", id, t);
          this.store.put("expiry_work", `transfer:${id}`, {
            id: `transfer:${id}`,
            kind: "transfer",
            key: id,
            deadline: t.expires_at ?? null,
            state: "pending",
          });
        }
        return {
          output: {
            transfer_id: id,
            content_ref: ref,
            expires_at: i.transfer_deadline,
            max_bytes: ref.byte_length,
            upload_location: `transfer:${id}`,
          },
        };
      }
      case "content.put": {
        const ref = object(i.content_ref),
          key = this.key(ref);
        this.target(c.target_id, ref.content_id);
        const t = this.transfer(p, text(i.transfer_id));
        if (
          !same(t.content_ref, ref) ||
          !same(t.policy_ref, i.policy_ref) ||
          !same(t.processed_sources, i.processed_sources) ||
          t.retention_until !== i.retention_until
        )
          reject("idempotency_conflict", "publication_input_changed");
        if (!["ready", "published"].includes(text(t.phase)))
          reject("invalid_state", "transfer_not_ready");
        const all = [...array(i.processed_sources), ...array(i.disclosed_sources)];
        for (const s of all) {
          if (same(s, ref)) reject("invalid_request", "source_cycle");
          this.content(p, s, "content.write");
        }
        if (
          array(i.disclosed_sources).some(
            (s) => !array(i.processed_sources).some((t) => same(s, t)),
          )
        )
          reject("invalid_request", "disclosed_source_not_processed");
        const value: Document = {
          content_ref: ref,
          policy_ref: i.policy_ref ?? null,
          processed_sources: i.processed_sources ?? [],
          disclosed_sources: i.disclosed_sources ?? [],
          retention_until: i.retention_until ?? null,
          publisher: p.subject_id,
          state: "active",
          control_revision: 1,
        };
        const old = this.store.get("contents", key);
        if (old && !same(old, value)) reject("idempotency_conflict", "content_version_changed");
        if (!old) {
          this.store.put("contents", key, value);
          this.store.put("expiry_work", `content:${key}`, {
            id: `content:${key}`,
            kind: "content",
            key,
            deadline: i.retention_until ?? null,
            state: "pending",
          });
        }
        if (t.phase !== "published") {
          t.phase = "published";
          this.store.put("transfers", text(i.transfer_id), t);
        }
        return { output: { content_ref: ref } };
      }
      case "content.close": {
        const ref = object(i.content_ref),
          key = this.key(ref),
          v = this.store.require("contents", key);
        this.target(c.target_id, ref.content_id);
        if (!same(v.content_ref, ref)) reject("idempotency_conflict", "content_reference_changed");
        if (v.publisher !== p.subject_id && !p.roles.includes("memory_admin"))
          reject("forbidden", "content_management_required");
        if (c.expected_revision !== v.control_revision)
          reject("revision_conflict", "content_control_revision_changed");
        v.state = "closed";
        v.control_revision = integer(v.control_revision) + 1;
        this.store.put("contents", key, v);
        // 当前来源门立即关闭；准确本地正文删除与关闭事实在同一个提交中。
        this.store.db.prepare("DELETE FROM content_bytes WHERE key=?").run(key);
        this.impact(key);
        return {
          output: {
            control_revision: v.control_revision,
            state: "closed",
            cleanup_state: "residual",
          },
        };
      }
      case "content.register_copy": {
        const ref = object(i.content_ref),
          holder = object(i.holder_ref),
          intent = object(i.reference_intent_ref),
          id = text(i.copy_id);
        validateSchema({ $ref: "#/$defs/Id" }, id);
        this.target(c.target_id, ref.content_id);
        if (
          holder.tenant_id !== this.store.config.tenant_id ||
          intent.tenant_id !== this.store.config.tenant_id ||
          holder.owner_id !== intent.owner_id ||
          i.location !== "local"
        )
          reject("forbidden", "copy_scope_mismatch");
        const source = this.content(p, ref, text(i.purpose)),
          until = this.future(i.retain_until);
        if (until > Date.parse(text(source.retention_until)))
          reject("forbidden", "retention_scope_expansion");
        const old = this.store.get("copies", id);
        if (old && (old.principal !== p.subject_id || !same(old.input, i)))
          reject("idempotency_conflict", "copy_input_changed");
        const h = old ?? {
          input: i,
          principal: p.subject_id,
          use_state: "allowed",
          cleanup_state: "pending",
          control_revision: source.control_revision ?? null,
          evidence_refs: [],
        };
        if (!old) {
          this.store.put("copies", id, h);
          this.store.put("expiry_work", `copy:${id}`, {
            id: `copy:${id}`,
            kind: "copy",
            key: id,
            deadline: i.retain_until ?? null,
            state: "pending",
          });
        }
        return { output: this.copyOutput(id, h) };
      }
      case "content.release_copy": {
        const id = text(i.copy_id),
          ref = object(i.content_ref),
          h = this.store.require("copies", id),
          original = object(h.input),
          source = this.store.require("contents", this.key(ref));
        this.target(c.target_id, ref.content_id);
        if (h.principal !== p.subject_id || !same(original.content_ref, ref))
          reject("forbidden", "copy_holder_mismatch");
        if (i.control_revision !== source.control_revision)
          reject("revision_conflict", "content_control_revision_changed");
        if (
          !["pending", "complete", "residual", "unknown"].includes(text(i.cleanup_state)) ||
          (i.cleanup_state === "complete" && (!i.use_stopped || !array(i.evidence_refs).length)) ||
          (i.cleanup_state === "residual" && !i.residual_reason)
        )
          reject("invalid_request", "invalid_cleanup_report");
        if (
          (h.use_state === "use_stopped" && !i.use_stopped) ||
          (h.cleanup_state === "complete" && i.cleanup_state !== "complete")
        )
          reject("invalid_state", "copy_cannot_resume_or_cleanup_regress");
        for (const evidence of array(i.evidence_refs)) {
          const held = this.store.require("contents", this.key(evidence));
          if (!same(held.content_ref, evidence))
            reject("idempotency_conflict", "cleanup_evidence_changed");
        }
        h.control_revision = source.control_revision ?? null;
        h.cleanup_state = i.cleanup_state ?? null;
        h.evidence_refs = i.evidence_refs ?? [];
        h.residual_reason = i.residual_reason ?? "";
        if (i.use_stopped) h.use_state = "use_stopped";
        this.store.put("copies", id, h);
        return { output: this.copyOutput(id, h) };
      }
      case "memory.create":
      case "memory.replace":
      case "memory.delete": {
        const id = text(i.memory_id);
        this.target(c.target_id, id);
        const old = this.store.get("memories", id);
        if (c.method === "memory.create") {
          if (this.store.list("memories", "", 201).length >= 200)
            reject("overloaded", "memory_profile_capacity");
          if (old) reject("idempotency_conflict", "memory_identity_exists");
          if (i.candidate_ref) reject("unsupported", "extraction_not_in_profile");
        } else {
          if (!old) reject("not_found", "memory_not_found");
          if (old.creator_id !== p.subject_id && !p.roles.includes("memory_admin"))
            reject("forbidden", "memory_management_required");
          if (c.expected_revision !== old.revision)
            reject("revision_conflict", "memory_revision_changed");
        }
        const values = c.method === "memory.delete" ? object(old?.values) : object(i.values);
        if (c.method !== "memory.delete") this.checkValues(p, values);
        const r: Document = {
          memory_id: id,
          revision: old ? integer(old.revision) + 1 : 1,
          values,
          state: c.method === "memory.delete" ? "deleted" : "active",
          recorded_at: new Date(this.store.now()).toISOString(),
          creator_id: old?.creator_id ?? p.subject_id,
          cleanup_state: c.method === "memory.delete" ? "cleaned" : "pending",
        };
        this.store.put("memories", id, r);
        const head = this.change(
          r,
          c.method === "memory.create"
            ? "created"
            : c.method === "memory.delete"
              ? "deleted"
              : "replaced",
        );
        return {
          output: {
            memory_ref: this.ref(id, integer(r.revision)),
            state: r.state,
            change_head: head,
          },
        };
      }
      case "memory.view.open": {
        const id = text(i.view_id),
          holder = object(i.holder_ref);
        this.target(c.target_id, id);
        if (
          holder.tenant_id !== this.store.config.tenant_id ||
          holder.owner_id !== this.store.config.owner_id ||
          holder.object_id !== p.subject_id ||
          holder.revision !== p.generation ||
          i.location !== "local"
        )
          reject("forbidden", "invalid_view_holder");
        const purposes = strings(i.purposes),
          max = integer(i.max_candidates);
        if (!purposes.length || purposes.length > 10 || max < 1 || max > 200)
          reject("invalid_request", "invalid_view_bound");
        this.content(p, i.scope_ref, "memory.sync", true);
        const records = this.store.list("memories", "", max + 1),
          refs: Document[] = [];
        for (const record of records.slice(0, max)) {
          try {
            for (const purpose of ["memory.sync", ...purposes])
              this.record(p, record, purpose, true);
            refs.push(this.ref(text(record.memory_id), integer(record.revision)));
          } catch (e) {
            if (!(e instanceof Rejection) || !["forbidden", "gone"].includes(e.wire.code)) throw e;
          }
        }
        if (this.store.get("views", id)) reject("idempotency_conflict", "view_identity_exists");
        const v: View = {
          view_id: id,
          principal: p,
          scope: object(i.scope_ref),
          purposes,
          expires: this.store.now() + 300000,
          visibility: this.visibility(p),
          snapshot: refs,
          head: this.head(),
          cursor: "",
          ack: "",
          issuedFrom: "",
          issued: null,
          partial: records.length > max,
          gaps: records.length > max ? ["candidate_limit"] : [],
        };
        v.cursor = this.cursor([id, "s", 0]);
        this.store.put("views", id, v);
        return {
          output: {
            view_ref: this.ref(id, 1),
            snapshot_head: v.head,
            cursor: v.cursor,
            expires_at: new Date(v.expires).toISOString(),
            partial: v.partial,
            gaps: v.gaps,
          },
        };
      }
      case "memory.view.ack": {
        const id = text(i.view_id);
        this.target(c.target_id, id);
        const v = this.view(p, id),
          r = object(i.receipt_ref);
        if (
          r.tenant_id !== this.store.config.tenant_id ||
          r.owner_id !== this.store.config.owner_id
        )
          reject("forbidden", "receipt_scope_mismatch");
        if (i.cursor === v.ack) return { output: { view_id: id, cursor: i.cursor } };
        if (!v.issued || i.cursor !== v.issued.cursor)
          reject("invalid_request", "cursor_not_issued");
        v.ack = text(i.cursor);
        v.cursor = text(i.cursor);
        this.store.put("views", id, v);
        return { output: { view_id: id, cursor: i.cursor } };
      }
      default:
        reject("unsupported", "method_not_supported");
    }
  }
  private copyOutput(id: string, holder: Document): Document {
    return {
      copy_id: id,
      control_revision: holder.control_revision ?? null,
      use_state: holder.use_state ?? null,
      cleanup_state: holder.cleanup_state ?? null,
    };
  }
  private depends(ref: unknown, key: string, visited = new Set<string>()): boolean {
    const id = this.key(ref);
    if (id === key) return true;
    if (visited.has(id) || visited.size >= 64) return false;
    visited.add(id);
    const c = this.store.get("contents", id);
    return (
      !!c &&
      [...array(c.processed_sources), ...array(c.disclosed_sources)].some((r) =>
        this.depends(r, key, new Set(visited)),
      )
    );
  }
  private impact(key: string): void {
    for (const record of this.store.list("memories")) {
      const v = object(record.values);
      if (
        record.state !== "active" ||
        ![v.content_ref, v.scope_ref, ...array(v.sources).map((s) => object(s).content_ref)].some(
          (r) => this.depends(r, key),
        )
      )
        continue;
      record.state = "quarantined";
      record.cleanup_state = "cleaned";
      record.review_reason = "source_control_closed";
      record.revision = integer(record.revision) + 1;
      this.store.put("memories", text(record.memory_id), record);
      this.change(record, "restricted");
    }
  }
  query(p: Principal, q: Query): unknown {
    if (q.method === "memory.query")
      this.permissionRemaining = integer(object(object(q.payload).limits).max_permission_checks);
    try {
      return this.store.tx(() => {
        current(this.store, p);
        const i = object(q.payload);
        switch (q.method) {
          case "content.transfer.read": {
            this.target(q.target_id, i.transfer_id);
            return this.transferStatus(this.transfer(p, text(i.transfer_id), false));
          }
          case "content.get": {
            const r = object(i.content_ref);
            this.target(q.target_id, r.content_id);
            if (i.mode === "bytes") {
              if (i.location !== "local" || !i.purpose)
                reject("invalid_request", "read_scope_required");
              if (integer(r.byte_length) > 2048)
                reject("unsupported", "use_bounded_byte_transport");
              return {
                content_ref: r,
                mode: "bytes",
                bytes_base64: this.bytes(p, r, text(i.purpose)).toString("base64"),
                control_revision: 0,
              };
            }
            if (i.mode !== "control" || !i.copy_id)
              reject("unsupported", "content_mode_not_in_profile");
            const h = this.store.require("copies", text(i.copy_id)),
              original = object(h.input),
              c = this.store.require("contents", this.key(r));
            if (h.principal !== p.subject_id || !same(original.content_ref, r))
              reject("forbidden", "copy_holder_mismatch");
            if (!same(c.content_ref, r))
              reject("idempotency_conflict", "content_reference_changed");
            let state = text(h.use_state);
            if (state === "allowed") {
              try {
                this.content(p, r, text(original.purpose));
                this.future(original.retain_until);
              } catch (error) {
                if (
                  !(error instanceof Rejection) ||
                  !["forbidden", "gone", "expired"].includes(error.wire.code)
                )
                  throw error;
                state = "closing";
              }
            }
            return {
              content_ref: r,
              mode: "control",
              control_revision: c.control_revision,
              use_state: state,
              cleanup_state: h.cleanup_state,
            };
          }
          case "memory.read":
          case "memory.inspect":
          case "memory.cleanup.get": {
            this.target(q.target_id, i.memory_id);
            const r = this.store.require("memories", text(i.memory_id));
            if (q.method !== "memory.read") {
              role(p, "memory_admin");
              if (r.creator_id !== p.subject_id && !p.roles.includes("admin"))
                reject("forbidden", "memory_management_required");
              return r;
            }
            if (i.revision && i.revision !== r.revision)
              reject("gone", "memory_revision_superseded");
            return this.record(p, r, i.purpose ? text(i.purpose) : "memory.read");
          }
          case "memory.list":
          case "memory.query":
            this.target(q.target_id, this.store.config.owner_id);
            return this.page(p, q, i);
          case "memory.index.inspect":
            role(p, "memory_admin");
            this.target(q.target_id, this.store.config.owner_id);
            return {
              change_head: this.head(),
              contiguous_watermark: this.head(),
              index_generation: 1,
              strategy_ref: manifest.ranking_profile,
              state: "ready",
              mode: "metadata_authority_scan",
            };
          case "memory.view.pull":
            this.target(q.target_id, i.view_id);
            return this.pull(p, i);
          default:
            reject("unsupported", "method_not_supported");
        }
      });
    } finally {
      this.permissionRemaining = undefined;
    }
  }
  private cursor(value: JSONValue[]): string {
    const body = Buffer.from(canonical(value)).toString("base64url"),
      mac = createHmac("sha256", this.store.meta("cursor_key")).update(body).digest("hex");
    return `${body}.${mac}`;
  }
  private decode(value: string): JSONValue[] {
    const parts = value.split(".");
    if (
      parts.length !== 2 ||
      !parts[0] ||
      parts[1] !==
        createHmac("sha256", this.store.meta("cursor_key")).update(parts[0]).digest("hex")
    )
      reject("invalid_request", "invalid_cursor");
    return array(parseStrict(Buffer.from(parts[0], "base64url")));
  }
  private page(p: Principal, q: Query, i: Document): Document {
    if (q.method === "memory.query") this.future(object(i.limits).deadline);
    const limit = i.limit ? integer(i.limit) : 20;
    if (limit < 1 || limit > 20) reject("invalid_request", "invalid_page_limit");
    const normalized = { ...i };
    delete normalized.limit;
    delete normalized.cursor;
    const digest = hash(canonical([q.method, normalized]));
    let id = q.query_id,
      position = 0;
    if (i.cursor) {
      const c = this.decode(text(i.cursor));
      id = text(c[0]);
      position = integer(c[1]);
      if (c[2] !== digest) reject("invalid_request", "cursor_input_changed");
    }
    let s = this.store.get<Snapshot>("snapshots", id);
    if (!s) {
      if (i.cursor) reject("cursor_expired", "snapshot_not_found");
      const rows = this.store.list("memories", "", 201),
        items: JSONValue[] = [],
        refs: Document[] = [],
        gaps: string[] = [];
      let max = 200,
        readBytes = 0,
        checks = 0,
        needle = "",
        spec: Document | undefined;
      if (q.method === "memory.query") {
        const limits = object(i.limits);
        max = integer(limits.max_candidates);
        const deadline = this.future(limits.deadline);
        if (
          max < 1 ||
          max > 200 ||
          strings(i.purposes).length < 1 ||
          strings(i.purposes).length > 10 ||
          integer(limits.max_read_bytes) < 1 ||
          integer(limits.max_read_bytes) > MAX_BYTES ||
          integer(limits.max_permission_checks) < 1 ||
          integer(limits.max_permission_checks) > 100000 ||
          deadline > this.store.now() + 300000
        )
          reject("invalid_request", "invalid_query_limits");
        const purpose = "memory.query",
          query = object(i.query_ref);
        this.content(p, i.scope_ref, purpose, true);
        refs.push(query, object(i.scope_ref));
        const bytes = this.bytes(p, query, purpose, true);
        readBytes += bytes.length;
        spec = object(parseStrict(bytes));
        validateSchema(manifest.schemas.query_spec, spec);
        if (!same(spec.ranking_profile_ref, manifest.ranking_profile))
          reject("unsupported", "ranking_profile_not_in_profile");
        refs.push(object(spec.text_ref));
        const body = this.bytes(p, spec.text_ref, purpose, true);
        readBytes += body.length;
        needle = new TextDecoder("utf-8", { fatal: true }).decode(body).toLowerCase();
        if (!needle) reject("invalid_request", "empty_query_text");
        if (readBytes > integer(limits.max_read_bytes))
          reject("invalid_request", "query_input_byte_budget");
      }
      for (const r of rows.slice(0, max)) {
        try {
          if (
            q.method === "memory.query" &&
            Date.parse(text(object(i.limits).deadline)) <= this.store.now()
          ) {
            gaps.push("deadline_expired");
            break;
          }
          const v = object(r.values);
          if (spec) {
            const types = strings(spec.type_filter);
            if (types.length && !types.includes(text(v.type))) continue;
            if (spec.scope_filter && !same(spec.scope_filter, v.scope_ref)) continue;
            const at = spec.valid_at ? Date.parse(text(spec.valid_at)) : this.store.now();
            if (
              (v.valid_from && at < Date.parse(text(v.valid_from))) ||
              (v.valid_to && at >= Date.parse(text(v.valid_to)))
            )
              continue;
            const limits = object(i.limits);
            checks += 3 + array(v.sources).length;
            if (checks > integer(limits.max_permission_checks)) {
              gaps.push("permission_check_limit");
              break;
            }
            for (const purpose of strings(i.purposes)) this.record(p, r, purpose, true);
            const body = this.bytes(p, v.content_ref, "memory.query", true);
            if (readBytes + body.length > integer(limits.max_read_bytes)) {
              gaps.push("read_byte_limit");
              break;
            }
            readBytes += body.length;
            if (
              new TextDecoder("utf-8", { fatal: true }).decode(body).toLowerCase().includes(needle)
            )
              items.push({
                memory_ref: this.ref(text(r.memory_id), integer(r.revision)),
                content_ref: v.content_ref ?? null,
                score: 1,
                explanation: ["literal_substring"],
              });
          } else {
            this.record(p, r, text(i.purpose));
            items.push(r);
          }
        } catch (e) {
          if (e instanceof Rejection && e.wire.reason === "permission_check_limit") {
            gaps.push("permission_check_limit");
            break;
          }
          if (
            !(e instanceof Rejection) ||
            !["forbidden", "gone", "dependency_unavailable"].includes(e.wire.code)
          )
            throw e;
          if (e.wire.code === "dependency_unavailable")
            gaps.push("permission_authority_unavailable");
        }
      }
      if (rows.length > max) gaps.push("candidate_limit");
      s = {
        subject: p.subject_id,
        generation: p.generation,
        digest,
        visibility: this.visibility(p),
        expires: this.store.now() + 300000,
        refs,
        items,
        revision: Math.max(1, this.head()),
        partial: !!gaps.length,
        gaps: [...new Set(gaps)],
      };
      this.store.put("snapshots", id, s);
    }
    if (s.subject !== p.subject_id || s.generation !== p.generation)
      reject("forbidden", "snapshot_principal_mismatch");
    if (s.digest !== digest) reject("idempotency_conflict", "snapshot_input_changed");
    if (s.expires <= this.store.now()) reject("cursor_expired", "snapshot_expired");
    if (s.visibility !== this.visibility(p)) reject("snapshot_required", "visibility_changed");
    for (const ref of s.refs) this.content(p, ref, "memory.query", true);
    const items = s.items.slice(position, position + limit);
    for (const item of items) {
      const r = object(item);
      if (r.memory_ref) {
        const ref = object(r.memory_ref),
          record = this.store.require("memories", text(ref.object_id));
        if (record.revision !== ref.revision)
          reject("snapshot_required", "memory_revision_changed");
        this.record(p, record, "memory.query", true);
      } else {
        const record = this.store.require("memories", text(r.memory_id));
        if (record.revision !== r.revision) reject("snapshot_required", "memory_revision_changed");
        this.record(p, record, text(i.purpose));
      }
    }
    const next = position + items.length,
      exhausted = next >= s.items.length;
    return {
      items,
      collection_revision: s.revision,
      exhausted,
      partial: s.partial,
      gaps: s.gaps,
      ...(!exhausted ? { next_cursor: this.cursor([id, next, digest]) } : {}),
    };
  }
  private view(p: Principal, id: string): View {
    const v = this.store.require<View>("views", id);
    current(this.store, v.principal);
    if (v.principal.subject_id !== p.subject_id || v.principal.generation !== p.generation)
      reject("forbidden", "view_principal_mismatch");
    if (v.expires <= this.store.now()) reject("cursor_expired", "view_expired");
    if (v.visibility !== this.visibility(p)) reject("snapshot_required", "view_visibility_changed");
    this.content(p, v.scope, "memory.sync", true);
    return v;
  }
  private pull(p: Principal, i: Document): Document {
    const id = text(i.view_id),
      v = this.view(p, id),
      cursor = i.cursor ? text(i.cursor) : v.cursor,
      limit = i.limit ? integer(i.limit) : 20;
    if (limit < 1 || limit > 20) reject("invalid_request", "invalid_page_limit");
    if (v.issued && cursor === v.issuedFrom) {
      for (const r of array(v.issued.records)) {
        const record = object(r),
          now = this.store.require("memories", text(record.memory_id));
        if (now.revision !== record.revision) reject("snapshot_required", "view_record_changed");
        for (const purpose of v.purposes) this.record(p, now, purpose, true);
      }
      return v.issued;
    }
    if (cursor !== v.cursor || (v.issued && v.ack !== v.issued.cursor))
      reject("invalid_request", "view_cursor_not_acknowledged");
    const c = this.decode(cursor);
    if (c[0] !== id) reject("invalid_request", "invalid_view_cursor");
    const phase = text(c[1]),
      pos = integer(c[2]);
    let records: Document[] = [],
      changes: Document[] = [],
      next: string;
    if (phase === "s") {
      const refs = v.snapshot.slice(pos, pos + limit);
      for (const ref of refs) {
        const r = this.store.require("memories", text(ref.object_id));
        if (r.revision !== ref.revision) reject("snapshot_required", "view_record_changed");
        for (const purpose of ["memory.sync", ...v.purposes]) this.record(p, r, purpose, true);
        records.push(r);
      }
      const n = pos + refs.length;
      next = this.cursor(n >= v.snapshot.length ? [id, "c", v.head] : [id, "s", n]);
    } else if (phase === "c") {
      changes = this.store.list("changes", String(pos).padStart(16, "0"), limit);
      next = this.cursor([
        id,
        "c",
        changes.length ? integer(changes[changes.length - 1]?.change_seq) : pos,
      ]);
    } else reject("invalid_request", "invalid_view_phase");
    const result: Document = {
      view_id: id,
      records,
      changes,
      cursor: next,
      snapshot_complete: phase === "c" || pos + records.length >= v.snapshot.length,
      change_head: this.head(),
      partial: v.partial,
      gaps: v.gaps,
    };
    v.issued = result;
    v.issuedFrom = cursor;
    this.store.put("views", id, v);
    return result;
  }
  disclose(p: Principal, q: Query, result: unknown): void {
    const i = object(q.payload);
    if (q.method === "memory.read") {
      const r = object(result),
        now = this.store.require("memories", text(r.memory_id));
      if (now.revision !== r.revision) reject("gone", "memory_revision_superseded");
      this.record(p, now, i.purpose ? text(i.purpose) : "memory.read");
    }
    if (q.method === "content.get" && i.mode === "bytes")
      this.content(p, i.content_ref, text(i.purpose));
    if (q.method === "memory.view.pull") this.view(p, text(i.view_id));
  }
}
