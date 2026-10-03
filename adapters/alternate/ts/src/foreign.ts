import { randomUUID } from "node:crypto";
import { canonical, parseStrict, validateSchema, type ContentRef } from "@harness/sdk";
import { current, hash, instant, same, signed } from "./authority";
import { reject, Rejection, failure } from "./error";
import { ContentPeer } from "./peer";
import { manifest } from "./runtime";
import type { Store } from "./store";
import {
  array,
  integer,
  object,
  strings,
  text,
  type Document,
  type ForeignReference,
  type ForeignSource,
  type ForeignUse,
  type Principal,
} from "./types";

interface Held {
  reference: ForeignReference;
  principal: Principal;
  phase: "reference_intent" | "writing" | "held" | "stopping" | "released";
  known_deny: boolean;
  proof: Document | null;
  write_intent: boolean;
  written: boolean;
  cleanup_state: "pending" | "residual" | "unknown";
  stop_report: Document | null;
  release_deadline: string | null;
}
interface Work {
  id: string;
  pending: boolean;
  due: number;
  attempts: number;
  claim: string;
  lease_until: number;
  last_error: Document | null;
}
export interface ForeignContext {
  proofs: Map<string, Document>;
  errors: Map<string, unknown>;
}

// 独立 Mirror 的原责任；Source authority/字节 IO 从不进入本方 SQL 事务。
export class ForeignCopies {
  private readonly peers = new Map<string, ContentPeer>();
  private timer: ReturnType<typeof setInterval> | undefined;
  private active: Promise<void> | undefined;
  private stopping = false;
  constructor(
    readonly store: Store,
    readonly changed: (ref: ContentRef) => void = () => {},
  ) {}
  key(ref: unknown): string {
    validateSchema({ $ref: "#/$defs/ContentRef" }, ref);
    const r = object(ref);
    if (r.tenant_id !== this.store.config.tenant_id)
      reject("forbidden", "foreign_reference_tenant_mismatch");
    return `foreign:${text(r.owner_id)}:${text(r.content_id)}:${integer(r.version)}`;
  }
  private settings(ref: ContentRef): ForeignSource {
    const source = this.store.config.foreign_sources?.find((s) => s.owner_id === ref.owner_id);
    if (!source) reject("dependency_unavailable", "foreign_source_not_paired");
    return source;
  }
  private peer(ref: ContentRef): ContentPeer {
    const source = this.settings(ref);
    let peer = this.peers.get(source.owner_id);
    if (!peer) {
      peer = new ContentPeer(this.store, source);
      this.peers.set(source.owner_id, peer);
    }
    return peer;
  }
  private reference(p: Principal, ref: ForeignReference, control = false): void {
    current(this.store, p);
    validateSchema(manifest.schemas.foreign_reference, ref);
    const source = this.settings(ref.content_ref);
    if (
      ref.content_ref.tenant_id !== this.store.config.tenant_id ||
      ref.content_ref.owner_id === this.store.config.owner_id ||
      ref.content_ref.byte_length > 262144 ||
      ref.register_command_id === ref.release_command_id ||
      ref.holder_ref.tenant_id !== this.store.config.tenant_id ||
      ref.reference_intent_ref.tenant_id !== this.store.config.tenant_id ||
      ref.holder_ref.owner_id !== this.store.config.owner_id ||
      ref.reference_intent_ref.owner_id !== this.store.config.owner_id ||
      ref.holder_ref.object_id !== p.subject_id ||
      (!control && ref.holder_ref.revision !== p.generation) ||
      (control && ref.holder_ref.revision > p.generation) ||
      !source.purposes.includes(ref.purpose) ||
      ref.location !== source.location ||
      ref.location !== "local"
    )
      reject("forbidden", "original_foreign_reference_scope_mismatch");
  }
  private async contracts(p: Principal, ref: ForeignReference): Promise<ContentPeer> {
    this.reference(p, ref, true);
    const peer = this.peer(ref.content_ref),
      registry = await peer.sourceContracts(p);
    for (const expected of manifest.source_methods) {
      const actual = registry.method(expected.name, expected.kind as "command" | "query");
      if (actual.schema_digest !== expected.schema_digest)
        reject("unsupported", "foreign_source_contract_version_not_retained");
    }
    return peer;
  }
  verify(p: Principal, ref: ForeignReference, proof: Document, control = false): void {
    this.reference(p, ref, control);
    validateSchema(manifest.schemas.foreign_proof, proof);
    const source = this.settings(ref.content_ref),
      values = object(proof.policy_values),
      policy = object(proof.policy_ref),
      header = object(parseStrict(Buffer.from(text(proof.proof).split(".")[0] ?? "", "base64url")));
    if (
      header.kid !== source.key_id ||
      proof.mode !== (control ? "control" : "use") ||
      !same(proof.content_ref, ref.content_ref) ||
      proof.source_database_id !== source.database_id ||
      !same(proof.subject_ref, ref.holder_ref) ||
      !same(proof.holder_ref, ref.holder_ref) ||
      !same(proof.reference_intent_ref, ref.reference_intent_ref) ||
      proof.copy_id !== ref.copy_id ||
      proof.purpose !== ref.purpose ||
      proof.location !== ref.location ||
      integer(proof.control_revision) < 1 ||
      !["published", "closed"].includes(text(proof.source_state)) ||
      !["allowed", "closing", "use_stopped"].includes(text(proof.use_state)) ||
      !["pending", "complete", "residual", "unknown"].includes(text(proof.cleanup_state)) ||
      hash(canonical(values)) !== policy.digest ||
      proof.continuous !== values.continuous ||
      Boolean(proof.independent_derived) !== Boolean(values.independent_derived) ||
      instant(text(proof.start_before)) - instant(text(proof.issued_at)) > 30000000000n ||
      instant(text(proof.retain_until)) > instant(ref.retain_until) ||
      instant(text(proof.retain_until)) > instant(text(values.retain_until))
    )
      reject("forbidden", "foreign_source_proof_binding_mismatch");
    for (const name of ["processed_sources", "disclosed_sources", "evidence_refs"]) {
      const refs = array(proof[name]);
      if (refs.length > 100) reject("overloaded", "foreign_source_graph_limit");
      const seen = new Set<string>();
      for (const value of refs) {
        validateSchema({ $ref: "#/$defs/ContentRef" }, value);
        const r = object(value),
          key = canonical(r);
        if (r.tenant_id !== this.store.config.tenant_id || seen.has(key))
          reject("forbidden", "foreign_source_graph_mismatch");
        seen.add(key);
      }
    }
    for (const ref of array(proof.disclosed_sources))
      if (!array(proof.processed_sources).some((processed) => same(ref, processed)))
        reject("forbidden", "foreign_disclosure_not_processed");
    signed(
      this.store,
      text(proof.proof),
      {
        tenant_id: this.store.config.tenant_id,
        issuer: ref.content_ref.owner_id,
        audience: this.store.config.owner_id,
        purpose: "foreign_content",
        object_ref: {
          tenant_id: ref.content_ref.tenant_id,
          owner_id: ref.content_ref.owner_id,
          object_id: ref.content_ref.content_id,
          revision: ref.content_ref.version,
        },
        digest: hash(canonical({ ...proof, proof: "" })),
        control_revision: proof.control_revision ?? null,
        window_id: ref.copy_id,
        issued_at: proof.issued_at ?? null,
        start_before: proof.start_before ?? null,
      },
      !control,
    );
  }
  private work(id: string): void {
    const old = this.store.get<Work>("foreign_work", id);
    if (old) {
      if (!old.pending) {
        old.pending = true;
        old.attempts = 0;
        old.due = this.store.now();
        this.store.put("foreign_work", id, old);
      }
      return;
    }
    if (this.store.list("foreign_work", "", 100).filter((w) => w.pending).length >= 32)
      reject("overloaded", "foreign_work_capacity");
    this.store.put("foreign_work", id, {
      id,
      pending: true,
      attempts: 0,
      due: this.store.now(),
      claim: "",
      lease_until: 0,
      last_error: null,
    });
  }
  freeze(p: Principal, reference: ForeignReference): void {
    this.reference(p, reference);
    this.store.tx(() => {
      current(this.store, p);
      const old = this.store.get<Held>("foreign_held", reference.copy_id);
      if (old) {
        if (!same(old.reference, reference))
          reject("idempotency_conflict", "original_foreign_reference_changed");
        return;
      }
      if (Date.parse(reference.retain_until) <= this.store.now())
        reject("expired", "original_foreign_retention_expired");
      if (this.store.list("foreign_held", "", 101).length >= 100)
        reject("overloaded", "foreign_holder_capacity");
      for (const held of this.store.list("foreign_held", "", 100)) {
        const r = object(held.reference);
        if (
          [r.register_command_id, r.release_command_id].some((id) =>
            [reference.register_command_id, reference.release_command_id].includes(text(id)),
          )
        )
          reject("idempotency_conflict", "foreign_outbound_identity_reused");
      }
      const held: Held = {
        reference,
        principal: p,
        phase: "reference_intent",
        known_deny: false,
        proof: null,
        write_intent: false,
        written: false,
        cleanup_state: "pending",
        stop_report: null,
        release_deadline: null,
      };
      // 先冻结原责任与Job；此事务提交前没有 Source RPC/物理写。
      this.store.put("foreign_intents", reference.copy_id, reference);
      this.store.put("foreign_held", reference.copy_id, held);
      this.work(reference.copy_id);
    });
  }
  private observe(p: Principal, ref: ForeignReference, proof: Document, control: boolean): Held {
    this.verify(p, ref, proof, control);
    return this.store.tx(
      () => {
        this.verify(p, ref, proof, control);
        const held = this.store.require<Held>("foreign_held", ref.copy_id);
        if (!same(held.reference, ref))
          reject("idempotency_conflict", "original_foreign_reference_changed");
        if (held.proof && integer(proof.control_revision) < integer(held.proof.control_revision))
          reject("forbidden", "original_foreign_proof_regressed");
        const sourceKey = this.key(ref.content_ref),
          previous = this.store.get("foreign_sources", sourceKey);
        if (
          previous &&
          (integer(proof.control_revision) < integer(previous.control_revision) ||
            (previous.state === "closed" && proof.source_state !== "closed"))
        )
          reject("forbidden", "original_foreign_source_regressed");
        if (
          previous &&
          integer(proof.control_revision) === integer(previous.control_revision) &&
          previous.state !== proof.source_state
        )
          reject("forbidden", "foreign_source_changed_without_revision");
        const denied =
          proof.source_state !== "published" ||
          proof.use_state !== "allowed" ||
          Date.parse(text(proof.retain_until)) <= this.store.now();
        held.proof = proof;
        if (denied) {
          held.known_deny = true;
          if (held.phase !== "released") {
            held.phase = "stopping";
            this.work(ref.copy_id);
          }
          this.changed(ref.content_ref);
        }
        if (
          !previous ||
          !same(previous, { state: proof.source_state, control_revision: proof.control_revision })
        )
          this.store.put("foreign_sources", sourceKey, {
            state: proof.source_state,
            control_revision: proof.control_revision,
          });
        this.store.put("foreign_held", ref.copy_id, held);
        return held;
      },
      true,
      true,
    );
  }
  async control(p: Principal, copyID: string): Promise<Document> {
    const held = this.store.require<Held>("foreign_held", copyID);
    this.reference(p, held.reference, true);
    const peer = await this.contracts(p, held.reference),
      proof = object(
        await peer.query(p, "content.foreign.current", held.reference.content_ref.content_id, {
          reference: held.reference as unknown as Document,
          control: true,
        }),
      );
    this.observe(p, held.reference, proof, true);
    return proof;
  }
  private async fresh(p: Principal, held: Held): Promise<Document> {
    const peer = await this.contracts(p, held.reference),
      proof = object(
        await peer.query(p, "content.foreign.current", held.reference.content_ref.content_id, {
          reference: held.reference as unknown as Document,
          control: false,
        }),
      );
    this.observe(p, held.reference, proof, false);
    return proof;
  }
  select(p: Principal, ref: unknown, purpose: string): Held {
    const held = this.store
      .list("foreign_held", "", 100)
      .map((r) => r as unknown as Held)
      .find(
        (h) =>
          same(h.reference.content_ref, ref) &&
          h.reference.purpose === purpose &&
          h.reference.holder_ref.object_id === p.subject_id &&
          h.reference.holder_ref.revision === p.generation,
      );
    if (!held) reject("dependency_unavailable", "foreign_reference_not_registered");
    return held;
  }
  async context(p: Principal, uses: { ref: unknown; purpose: string }[]): Promise<ForeignContext> {
    const context: ForeignContext = { proofs: new Map(), errors: new Map() },
      selected = new Map<string, Held>();
    for (const use of uses) {
      const ref = object(use.ref);
      if (ref.owner_id === this.store.config.owner_id) continue;
      const key = `${this.key(ref)}/${use.purpose}`;
      try {
        const held = this.select(p, ref, use.purpose);
        selected.set(held.reference.copy_id, held);
      } catch (error) {
        context.errors.set(key, error);
      }
    }
    if (selected.size > 32) reject("overloaded", "foreign_request_holder_limit");
    for (const held of selected.values()) {
      const key = `${this.key(held.reference.content_ref)}/${held.reference.purpose}`;
      try {
        context.proofs.set(held.reference.copy_id, await this.fresh(p, held));
      } catch (error) {
        context.errors.set(key, error);
      }
    }
    return context;
  }
  content(
    p: Principal,
    ref: unknown,
    purpose: string,
    context?: ForeignContext,
    continuous = false,
  ): Document {
    const key = `${this.key(ref)}/${purpose}`;
    if (context?.errors.has(key)) throw context.errors.get(key);
    const held = this.select(p, ref, purpose),
      proof = context?.proofs.get(held.reference.copy_id);
    if (!proof) reject("dependency_unavailable", "current_foreign_proof_required");
    this.verify(p, held.reference, proof);
    const values = object(proof.policy_values);
    if (
      held.phase !== "held" ||
      held.known_deny ||
      proof.source_state !== "published" ||
      proof.use_state !== "allowed" ||
      Date.parse(text(proof.retain_until)) <= this.store.now() ||
      !strings(values.subjects).includes(p.subject_id) ||
      !strings(values.purposes).includes(purpose) ||
      !strings(values.locations).includes(held.reference.location) ||
      (continuous && !proof.continuous)
    )
      reject("forbidden", "foreign_copy_not_currently_allowed");
    for (const ref of [
      held.reference.holder_ref,
      held.reference.reference_intent_ref,
      proof.policy_ref,
    ]) {
      const r = object(ref);
      if (
        this.store.get(
          "denials",
          `${text(r.owner_id ?? held.reference.content_ref.owner_id)}:${text(r.object_id ?? r.component_id)}`,
        )
      )
        reject("forbidden", "known_foreign_authority_revoked");
    }
    return {
      content_ref: proof.content_ref ?? null,
      policy_ref: proof.policy_ref ?? null,
      foreign_policy_values: values,
      retention_until: proof.retain_until ?? null,
      processed_sources: proof.processed_sources ?? [],
      disclosed_sources: proof.disclosed_sources ?? [],
      control_revision: proof.control_revision ?? null,
    };
  }
  bytes(
    p: Principal,
    ref: unknown,
    purpose: string,
    context?: ForeignContext,
    continuous = false,
  ): Buffer {
    this.content(p, ref, purpose, context, continuous);
    const held = this.select(p, ref, purpose),
      row = this.store.db
        .prepare("SELECT body FROM content_bytes WHERE key=?")
        .get(`foreign-copy:${held.reference.copy_id}`);
    if (!row || !(row.body instanceof Uint8Array))
      reject("dependency_unavailable", "original_foreign_mirror_unavailable");
    const body = Buffer.from(row.body),
      r = object(ref);
    if (body.length !== r.byte_length || hash(body) !== r.hash)
      reject("dependency_unavailable", "original_foreign_mirror_corrupted");
    return body;
  }
  async prepare(p: Principal, reference: ForeignReference): Promise<ForeignUse> {
    this.freeze(p, reference);
    await this.advance(reference.copy_id, p);
    const held = this.store.require<Held>("foreign_held", reference.copy_id),
      proof = await this.fresh(p, held);
    this.content(p, reference.content_ref, reference.purpose, {
      proofs: new Map([[reference.copy_id, proof]]),
      errors: new Map(),
    });
    return { reference, proof };
  }
  async stopCopy(p: Principal, copyID: string): Promise<void> {
    this.store.tx(
      () => {
        const held = this.store.require<Held>("foreign_held", copyID);
        this.reference(p, held.reference, true);
        held.known_deny = true;
        if (held.phase !== "released") held.phase = "stopping";
        this.store.put("foreign_held", copyID, held);
        this.work(copyID);
        this.changed(held.reference.content_ref);
      },
      true,
      true,
    );
    await this.advance(copyID, p);
  }
  inspect(p: Principal, copyID: string): Document {
    const held = this.store.require<Held>("foreign_held", copyID);
    this.reference(p, held.reference, true);
    return {
      reference: held.reference as unknown as Document,
      phase: held.phase,
      known_deny: held.known_deny,
      write_intent: held.write_intent,
      written: held.written,
      cleanup_state: held.cleanup_state,
      stop_report: held.stop_report,
    };
  }
  private claim(copyID: string): string {
    return this.store.tx(
      () => {
        const work = this.store.require<Work>("foreign_work", copyID);
        if (work.claim && work.lease_until > this.store.now())
          reject("dependency_unavailable", "original_foreign_work_in_progress");
        const token = randomUUID();
        work.claim = token;
        work.lease_until = this.store.now() + 10000;
        this.store.put("foreign_work", copyID, work);
        return token;
      },
      true,
      true,
    );
  }
  private guard(copyID: string, token: string): void {
    this.store.assertCurrent();
    const work = this.store.require<Work>("foreign_work", copyID);
    if (work.claim !== token || work.lease_until <= this.store.now())
      reject("forbidden", "original_foreign_claim_lost");
  }
  private renew(copyID: string, token: string): void {
    this.store.tx(
      () => {
        this.guard(copyID, token);
        const work = this.store.require<Work>("foreign_work", copyID);
        work.lease_until = this.store.now() + 10000;
        this.store.put("foreign_work", copyID, work);
      },
      true,
      true,
    );
  }
  async advance(copyID: string, p: Principal): Promise<void> {
    const token = this.claim(copyID);
    try {
      let held = this.store.require<Held>("foreign_held", copyID);
      this.reference(p, held.reference, true);
      if (held.phase === "released") return;
      if (
        held.known_deny ||
        held.phase === "stopping" ||
        Date.parse(held.reference.retain_until) <= this.store.now()
      ) {
        await this.cleanup(p, held, token);
        return;
      }
      this.reference(p, held.reference);
      const peer = await this.contracts(p, held.reference);
      if (held.phase === "reference_intent") {
        this.renew(copyID, token);
        const receipt = await peer.send(
          p,
          held.reference.register_command_id,
          "content.foreign.register",
          held.reference.content_ref.content_id,
          held.reference as unknown as Document,
          held.reference.retain_until,
        );
        this.guard(copyID, token);
        if (receipt.stage !== "applied")
          throw new Rejection(
            receipt.error?.code ?? "dependency_unavailable",
            receipt.error?.reason ?? "original_foreign_registration_pending",
          );
      }
      this.renew(copyID, token);
      let proof = await this.fresh(p, held);
      this.guard(copyID, token);
      held = this.store.require<Held>("foreign_held", copyID);
      if (held.known_deny) {
        await this.cleanup(p, held, token);
        return;
      }
      if (!held.written) {
        // 物理 BLOB 写责任先提交；写入和 written 标记再在同库原子提交。
        this.store.tx(() => {
          this.guard(copyID, token);
          this.verify(p, held.reference, proof);
          held.phase = "writing";
          held.write_intent = true;
          this.store.put("foreign_held", copyID, held);
        });
        const chunks: Buffer[] = [],
          count = Math.max(1, Math.ceil(held.reference.content_ref.byte_length / 65536));
        for (let index = 0; index < count; index++) {
          this.renew(copyID, token);
          const chunk = object(
            await peer.query(p, "content.foreign.get", held.reference.content_ref.content_id, {
              reference: held.reference as unknown as Document,
              chunk_index: index,
            }),
          );
          this.guard(copyID, token);
          const encoded = text(chunk.data_base64),
            part = Buffer.from(encoded, "base64"),
            expected = Math.min(65536, held.reference.content_ref.byte_length - index * 65536);
          if (
            !same(chunk.content_ref, held.reference.content_ref) ||
            chunk.chunk_index !== index ||
            chunk.chunk_count !== count ||
            part.toString("base64") !== encoded ||
            part.length !== expected
          )
            reject("forbidden", "foreign_source_chunk_corrupted");
          chunks.push(part);
        }
        const body = Buffer.concat(chunks);
        if (
          body.length !== held.reference.content_ref.byte_length ||
          hash(body) !== held.reference.content_ref.hash
        )
          reject("forbidden", "foreign_source_full_hash_mismatch");
        this.store.tx(() => {
          this.guard(copyID, token);
          this.verify(p, held.reference, proof);
          const now = this.store.require<Held>("foreign_held", copyID);
          if (now.known_deny) reject("forbidden", "original_foreign_copy_stopped");
          if (
            Number(
              this.store.db
                .prepare("SELECT coalesce(sum(length(body)),0) n FROM content_bytes")
                .get()?.n,
            ) +
              body.length >
            32 * 1048576
          )
            reject("overloaded", "foreign_mirror_byte_capacity");
          this.store.db
            .prepare("INSERT INTO content_bytes VALUES(?,?) ON CONFLICT(key) DO NOTHING")
            .run(`foreign-copy:${copyID}`, body);
          now.written = true;
          this.store.put("foreign_held", copyID, now);
        });
        if (this.store.config.fault?.crash_after_foreign_write)
          process.kill(process.pid, "SIGKILL");
      }
      this.renew(copyID, token);
      held = this.store.require<Held>("foreign_held", copyID);
      proof = await this.fresh(p, held);
      this.store.tx(() => {
        this.guard(copyID, token);
        this.verify(p, held.reference, proof);
        const now = this.store.require<Held>("foreign_held", copyID);
        if (now.known_deny) reject("forbidden", "original_foreign_copy_stopped");
        now.phase = "held";
        this.store.put("foreign_held", copyID, now);
      });
    } finally {
      this.store.tx(
        () => {
          const work = this.store.require<Work>("foreign_work", copyID);
          if (work.claim === token) {
            work.claim = "";
            work.lease_until = 0;
            const held = this.store.require<Held>("foreign_held", copyID);
            work.pending = !["held", "released"].includes(held.phase);
            work.due = this.store.now() + 1000;
            this.store.put("foreign_work", copyID, work);
          }
        },
        true,
        true,
      );
    }
  }
  private async cleanup(p: Principal, held: Held, token: string): Promise<void> {
    const ref = held.reference;
    this.renew(ref.copy_id, token);
    const proof = await this.control(p, ref.copy_id),
      peer = await this.contracts(p, ref);
    this.store.tx(
      () => {
        this.guard(ref.copy_id, token);
        const now = this.store.require<Held>("foreign_held", ref.copy_id);
        now.known_deny = true;
        now.phase = "stopping";
        this.store.db
          .prepare("DELETE FROM content_bytes WHERE key=?")
          .run(`foreign-copy:${ref.copy_id}`);
        // SQLite 页/WAL没有擦除证明；即使没有 location 也不虚构 complete。
        now.cleanup_state = now.write_intent ? "residual" : "unknown";
        if (!now.stop_report) {
          now.stop_report = {
            copy_id: ref.copy_id,
            content_ref: ref.content_ref as unknown as Document,
            control_revision: proof.control_revision ?? null,
            use_stopped: true,
            cleanup_state: now.cleanup_state,
            evidence_refs: [],
            ...(now.cleanup_state === "residual"
              ? { residual_reason: "logical_native_blob_deleted_sqlite_pages_wal_not_erased" }
              : {}),
          };
          now.release_deadline = new Date(this.store.now() + 600000).toISOString();
        }
        this.store.put("foreign_held", ref.copy_id, now);
      },
      true,
      true,
    );
    held = this.store.require<Held>("foreign_held", ref.copy_id);
    this.renew(ref.copy_id, token);
    const receipt = await peer.send(
      p,
      ref.release_command_id,
      "content.foreign.release",
      ref.content_ref.content_id,
      { reference: ref as unknown as Document, report: held.stop_report },
      text(held.release_deadline),
    );
    this.guard(ref.copy_id, token);
    if (receipt.stage !== "applied")
      throw new Rejection(
        receipt.error?.code ?? "dependency_unavailable",
        receipt.error?.reason ?? "original_foreign_release_pending",
      );
    this.store.tx(
      () => {
        this.guard(ref.copy_id, token);
        const now = this.store.require<Held>("foreign_held", ref.copy_id);
        now.phase = "released";
        this.store.put("foreign_held", ref.copy_id, now);
      },
      true,
      true,
    );
  }
  start(): void {
    if (!this.store.config.foreign_sources?.length) return;
    this.timer = setInterval(() => {
      if (this.active || this.stopping || this.store.config.fault?.pause_jobs) return;
      this.active = this.tick().finally(() => {
        this.active = undefined;
      });
    }, 500);
  }
  private async tick(): Promise<void> {
    const work = this.store
      .list("foreign_work", "", 100)
      .find(
        (w) =>
          w.pending &&
          Number(w.due) <= this.store.now() &&
          Number(w.lease_until) <= this.store.now(),
      );
    if (!work) return;
    const id = text(work.id),
      held = this.store.require<Held>("foreign_held", id);
    if (integer(work.attempts) >= 8) return;
    this.store.tx(
      () => {
        work.attempts = integer(work.attempts) + 1;
        this.store.put("foreign_work", id, work);
      },
      true,
      true,
    );
    try {
      await this.advance(id, this.store.credential(held.principal.subject_id));
    } catch (error) {
      this.store.tx(
        () => {
          const now = this.store.require<Work>("foreign_work", id);
          now.last_error = failure(error) as unknown as Document;
          now.due = this.store.now() + 1000;
          this.store.put("foreign_work", id, now);
        },
        true,
        true,
      );
    }
  }
  async close(): Promise<void> {
    this.stopping = true;
    if (this.timer) clearInterval(this.timer);
    for (const peer of this.peers.values()) peer.stop();
    await this.active;
  }
}

// OS受信宿主显式调用；构造本身零出站，不增加线业务方法或改写源CopyHolder。
export class NativeHost {
  readonly foreign: ForeignCopies;
  constructor(store: Store) {
    this.foreign = new ForeignCopies(store);
  }
  prepareForeignUse(p: Principal, ref: ForeignReference): Promise<ForeignUse> {
    return this.foreign.prepare(p, ref);
  }
  controlForeignCopy(p: Principal, copyID: string): Promise<Document> {
    return this.foreign.control(p, copyID);
  }
  stopForeignCopy(p: Principal, copyID: string): Promise<void> {
    return this.foreign.stopCopy(p, copyID);
  }
}
