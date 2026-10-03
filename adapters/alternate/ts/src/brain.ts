import type { Command, Query } from "@harness/sdk";
import { canonical, parseStrict, validateSchema, ProtocolError } from "@harness/sdk";
import { current, hash, role, same, use } from "./authority";
import { reject } from "./error";
import { manifest, type Handler } from "./runtime";
import { ContentPeer, stable } from "./peer";
import { Runner, raise, settle } from "./work";
import type { Store } from "./store";
import { object, text, integer, array, type Principal, type Document } from "./types";

interface Decision {
  id: string;
  record: Document;
  input: Document;
  principal: Principal;
  command_id: string;
  phase: string;
  call_id: string;
  cancelled: boolean;
  snapshot: Document | null;
  publications: string[];
  proposal: string;
}
export class Brain implements Handler {
  readonly peer: ContentPeer;
  readonly runner: Runner;
  constructor(readonly store: Store) {
    this.peer = new ContentPeer(store);
    this.runner = new Runner(
      store,
      (id) => this.advance(id),
      (id, error) => {
        const d = store.require<Decision>("decisions", id);
        if (d.record.status === "completed" || d.record.status === "cancelled") return;
        d.record.status = "failed";
        d.record.revision = integer(d.record.revision) + 1;
        d.phase = "failed";
        store.put("decisions", id, d);
        settle(store, d.command_id, undefined, error);
      },
    );
  }
  start(): void {
    this.runner.start();
  }
  ref(id: string, r: number): Document {
    return {
      tenant_id: this.store.config.tenant_id,
      owner_id: this.store.config.owner_id,
      object_id: id,
      revision: r,
    };
  }
  private gate(d: Decision): void {
    current(this.store, d.principal);
    role(d.principal, "service");
    if (d.cancelled) reject("invalid_state", "decision_cancelled");
    if (Date.parse(text(d.input.deadline)) <= this.store.now())
      reject("expired", "decision_deadline_expired");
    use(
      this.store,
      d.principal,
      d.input.use_refs,
      this.ref(d.id, 1),
      hash(canonical(d.input)),
      "decision",
    );
  }
  command(p: Principal, c: Command): { output: unknown; accepted?: boolean } {
    const i = object(c.payload),
      id = text(i.decision_id);
    if (c.target_id !== id) reject("invalid_request", "target_mismatch");
    if (c.method === "brain.decide") {
      role(p, "service");
      const task = object(i.task_ref);
      if (task.tenant_id !== this.store.config.tenant_id || task.owner_id !== p.subject_id)
        reject("forbidden", "original_orchestrator_required");
      if (!same(i.model_profile_ref, manifest.brain_profile))
        reject("unsupported", "model_profile_not_registered");
      if (!same(i.limits, [{ unit: "USD", value: "0" }]))
        reject("unsupported", "brain_cost_profile_requires_usd_zero");
      if (
        Date.parse(text(i.deadline)) <= this.store.now() ||
        Date.parse(text(i.deadline)) > this.store.now() + 300000
      )
        reject("invalid_request", "finite_decision_deadline_required");
      const old = this.store.get<Decision>("decisions", id);
      if (old) reject("idempotency_conflict", "decision_identity_exists");
      const d: Decision = {
        id,
        record: {
          decision_id: id,
          owner_id: this.store.config.owner_id,
          revision: 1,
          snapshot_revision: i.snapshot_revision ?? null,
          status: "accepted",
          send_started: false,
          physical_request_count: 0,
          usage: [{ unit: "USD", value: "0" }],
          usage_final: true,
        },
        input: i,
        principal: p,
        command_id: c.command_id,
        phase: "accepted",
        call_id: stable("call", id),
        cancelled: false,
        snapshot: null,
        publications: [],
        proposal: stable("content", `${id}/proposal`),
      };
      this.gate(d);
      this.store.put("decisions", id, d);
      raise(this.store, id, text(i.deadline));
      return { accepted: true, output: { decision_ref: this.ref(id, 1), status: "accepted" } };
    }
    if (c.method === "brain.cancel") {
      const d = this.store.require<Decision>("decisions", id);
      if (d.principal.subject_id !== p.subject_id || !same(d.input.task_ref, i.task_ref))
        reject("forbidden", "decision_principal_or_task_mismatch");
      if (!["completed", "cancelled", "failed"].includes(text(d.record.status))) {
        d.cancelled = true;
        d.phase = "cancelled";
        d.record.status = "cancelled";
        d.record.revision = integer(d.record.revision) + 1;
        this.store.put("decisions", id, d);
        settle(this.store, d.command_id, {
          decision_ref: this.ref(id, integer(d.record.revision)),
          status: "cancelled",
        });
      }
      return {
        output: { decision_ref: this.ref(id, integer(d.record.revision)), status: d.record.status },
      };
    }
    reject("unsupported", "method_not_supported");
  }
  async query(p: Principal, q: Query): Promise<unknown> {
    const i = object(q.payload),
      id = text(i.decision_id);
    if (q.method !== "brain.get" || q.target_id !== id)
      reject("invalid_request", "target_mismatch");
    const d = this.store.require<Decision>("decisions", id);
    if (d.principal.subject_id !== p.subject_id) reject("forbidden", "decision_not_disclosed");
    await this.peer.read(p, d.input.snapshot_ref, "brain.input");
    if (d.record.proposal_ref) await this.peer.read(p, d.record.proposal_ref, "brain.output");
    return {
      decision: d.record,
      task_ref: d.input.task_ref,
      snapshot_ref: d.input.snapshot_ref,
      publication: d.phase === "completed" ? "published" : d.phase,
      call_id: d.call_id,
      cancel_requested: d.cancelled,
    };
  }
  private async sources(d: Decision): Promise<void> {
    this.gate(d);
    if (d.snapshot) {
      for (const ref of array(d.snapshot.processed_sources))
        await this.peer.read(d.principal, ref, "brain.input");
    }
    this.gate(d);
  }
  async advance(id: string): Promise<void> {
    let d = this.store.require<Decision>("decisions", id);
    if (["completed", "cancelled", "failed"].includes(text(d.record.status))) return;
    this.gate(d);
    if (d.phase === "accepted") {
      const bytes = await this.peer.read(d.principal, d.input.snapshot_ref, "brain.input"),
        snapshot = object(parseStrict(bytes));
      validateSchema(manifest.schemas.snapshot, snapshot);
      if (
        !same(snapshot.task_ref, d.input.task_ref) ||
        snapshot.revision !== d.input.snapshot_revision ||
        !same(snapshot.model_profile_ref, d.input.model_profile_ref) ||
        snapshot.purpose !== "answer" ||
        snapshot.count_mode !== "upper_bound"
      )
        reject("forbidden", "snapshot_input_binding_mismatch");
      if (
        hash(canonical(snapshot.requirements)) !== snapshot.requirements_digest ||
        array(snapshot.processed_sources).length > 32 ||
        !array(snapshot.processed_sources).some((r) => same(r, snapshot.goal_ref))
      )
        reject("forbidden", "snapshot_source_or_requirement_binding_mismatch");
      const goal = await this.peer.read(d.principal, snapshot.goal_ref, "brain.input"),
        unsigned = { ...snapshot, input_tokens: 0, encoded_digest: "" },
        encoding = Buffer.from(
          canonical({ snapshot: unsigned, goal_bytes: goal.toString("base64") }),
        );
      if (
        encoding.length > 131072 ||
        snapshot.encoded_digest !== hash(encoding) ||
        snapshot.input_tokens !== encoding.length ||
        integer(snapshot.input_tokens) +
          integer(snapshot.reserved_output_tokens) +
          integer(snapshot.safety_margin_tokens) >
          262144
      )
        reject("forbidden", "encoded_input_digest_or_bound_mismatch");
      for (const ref of array(snapshot.processed_sources))
        await this.peer.read(d.principal, ref, "brain.input");
      let parsed: Document | undefined;
      try {
        const value = parseStrict(goal);
        validateSchema(manifest.schemas.goal, value);
        parsed = object(value);
      } catch (error) {
        if (!(error instanceof ProtocolError)) throw error;
      }
      if (parsed?.kind === "report") reject("unsupported", "report_not_in_brain_profile");
      const body = parsed
          ? Buffer.from(text(parsed.body))
          : Buffer.from(
              "请按已登记 GoalSpec 提供 answer 正文。此组件不会把自由文本解释为任务已完成。",
            ),
        reason = Buffer.from(
          parsed
            ? "按准确 answer 模板产生建议；Task 完成仍由 Orchestrator 核验。"
            : "目标需要受限模板澄清。",
        ),
        sources = [d.input.snapshot_ref ?? null, ...array(snapshot.processed_sources)];
      if (body.length > 65536) reject("invalid_request", "answer_byte_bound");
      this.store.tx(() => {
        d = this.store.require<Decision>("decisions", id);
        this.gate(d);
        if (d.phase !== "accepted") return;
        const reasonID = stable("content", `${id}/reason`),
          bodyID = stable("content", `${id}/${parsed ? "answer" : "question"}`),
          reasonRef = this.peer.plan(
            d.principal,
            reasonID,
            reason,
            "text/plain",
            sources,
            [],
            text(d.input.deadline),
          ),
          bodyRef = this.peer.plan(
            d.principal,
            bodyID,
            body,
            "text/plain",
            sources,
            parsed ? [snapshot.goal_ref ?? null] : [],
            text(d.input.deadline),
          );
        const proposal: Document = parsed
          ? {
              kind: "complete",
              reason_ref: reasonRef,
              artifact_refs: [bodyRef],
              check_suggestions: [],
            }
          : {
              kind: "request_input",
              reason_ref: reasonRef,
              question_ref: bodyRef,
              answer_schema_ref: this.store.config.answer_schema_ref as unknown as Document,
              preview_refs: [bodyRef],
              purpose: "clarify_goal",
            };
        validateSchema(manifest.schemas.proposal, proposal);
        this.peer.plan(
          d.principal,
          d.proposal,
          Buffer.from(canonical(proposal)),
          "application/vnd.harness.proposal+json",
          [...sources, reasonRef, bodyRef],
          [],
          text(d.input.deadline),
        );
        d.snapshot = snapshot;
        d.publications = [reasonID, bodyID, d.proposal];
        d.phase = "publishing";
        d.record.status = "running";
        d.record.revision = integer(d.record.revision) + 1;
        this.store.put("decisions", id, d);
      });
    }
    d = this.store.require<Decision>("decisions", id);
    await this.sources(d);
    for (const publication of d.publications) {
      await this.sources(d);
      await this.peer.publish(d.principal, publication);
    }
    const proposal = this.store.require<{ ref: Document }>("publications", d.proposal).ref;
    await this.peer.read(d.principal, proposal, "brain.output");
    this.store.tx(() => {
      const n = this.store.require<Decision>("decisions", id);
      this.gate(n);
      n.record.proposal_ref = proposal;
      n.record.status = "completed";
      n.record.revision = integer(n.record.revision) + 1;
      n.phase = "completed";
      this.store.put("decisions", id, n);
      settle(this.store, n.command_id, {
        decision_ref: this.ref(id, integer(n.record.revision)),
        status: "completed",
      });
    });
  }
  async stop(): Promise<void> {
    const done = this.runner.stop();
    this.peer.stop();
    await done;
  }
}
