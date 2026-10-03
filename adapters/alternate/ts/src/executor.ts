import {
  constants,
  openSync,
  closeSync,
  fstatSync,
  lstatSync,
  readSync,
  readFileSync,
} from "node:fs";
import type { APIError, Command, Query } from "@harness/sdk";
import { canonical, parseStrict, validateSchema } from "@harness/sdk";
import { current, role, same, hash, use, signed } from "./authority";
import { reject } from "./error";
import { manifest, type Handler } from "./runtime";
import { ContentPeer, stable } from "./peer";
import { Runner, raise } from "./work";
import { initializeCursor, operationPage } from "./pagination";
import type { Store } from "./store";
import { object, text, integer, array, type Principal, type Document } from "./types";

interface Operation {
  id: string;
  operation: Document;
  input: Document;
  principal: Principal;
  intent: Document | null;
  args: Document | null;
  phase: string;
  new_closed: boolean;
  stopped: boolean;
  attempt: Document | null;
  result: string;
  observation: Document | null;
  tombstone: boolean;
  pid: number;
  pid_start: string;
}
const emptyContent = {
  tenant_id: "",
  owner_id: "",
  content_id: "",
  version: 0,
  hash: "",
  media_type: "",
  byte_length: 0,
};
export class Executor implements Handler {
  readonly peer: ContentPeer;
  readonly runner: Runner;
  private readonly root: number;
  constructor(readonly store: Store) {
    if (process.platform !== "linux" || !store.config.managed_root)
      reject("unsupported", "linux_fd_root_required");
    const stat = lstatSync(store.config.managed_root);
    if (
      !stat.isDirectory() ||
      stat.isSymbolicLink() ||
      stat.mode & 0o077 ||
      stat.uid !== process.getuid?.()
    )
      reject("forbidden", "private_managed_root_required");
    this.root = openSync(
      store.config.managed_root,
      constants.O_RDONLY | constants.O_DIRECTORY | constants.O_NOFOLLOW,
    );
    const rootStat = fstatSync(this.root, { bigint: true }),
      identity = `${rootStat.dev}:${rootStat.ino}`;
    store.tx(() => {
      if (store.meta("managed_root_identity") && store.meta("managed_root_identity") !== identity)
        reject("invalid_state", "managed_root_identity_changed");
      store.meta("managed_root_identity", identity);
    });
    initializeCursor(store);
    this.peer = new ContentPeer(store);
    this.runner = new Runner(
      store,
      (id) => this.advance(id),
      (id, error) => this.terminal(id, error),
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
  private bump(o: Operation): void {
    o.operation.revision = integer(o.operation.revision) + 1;
    this.store.put("operations", o.id, o);
    this.store.meta("operations_head", String(Number(this.store.meta("operations_head") || 0) + 1));
  }
  private output(o: Operation): Document {
    return {
      operation_ref: this.ref(o.id, integer(o.operation.revision)),
      execution_state: o.operation.execution_state ?? null,
      effect: o.operation.effect ?? null,
      may_apply_later: o.operation.may_apply_later ?? null,
      new_attempts_closed: o.new_closed,
      actually_stopped: o.stopped,
    };
  }
  private authorized(p: Principal, o: Operation): void {
    current(this.store, p);
    if (o.principal.subject_id !== p.subject_id) reject("forbidden", "operation_not_disclosed");
  }
  private closed(o: Operation, effect: string, stopped = true): void {
    o.new_closed = true;
    o.stopped = stopped;
    o.operation.execution_state = "closed";
    o.operation.effect = effect;
    o.operation.may_apply_later = stopped ? false : "unknown";
    o.operation.usage_final = true;
    o.operation.next_action = effect === "unknown" ? "query_original" : "none";
    o.operation.attempts = {
      collection_revision: integer(o.operation.revision) + 1,
      total_count: o.attempt ? 1 : 0,
      unresolved_count: effect === "unknown" ? 1 : 0,
      complete: effect !== "unknown",
    };
    if (o.attempt) {
      o.attempt.effect = effect;
      o.attempt.may_apply_later = o.operation.may_apply_later;
      o.attempt.actually_stopped = stopped;
      o.attempt.usage_final = true;
      o.attempt.phase = effect === "unknown" ? "unknown" : "closed";
      o.attempt.revision = integer(o.attempt.revision) + 1;
    }
  }
  private terminal(id: string, error: APIError): void {
    const o = this.store.require<Operation>("operations", id);
    if (o.phase === "complete" || o.tombstone) return;
    o.phase = "failed";
    if (o.observation) {
      this.closed(o, "applied");
      o.operation.next_action = "query_original";
    } else if (o.attempt) {
      this.closed(o, "unknown", this.dead(o));
    } else this.closed(o, "not_started");
    o.input = { ...o.input, failure_reason: error.reason };
    this.bump(o);
  }
  private dead(o: Operation): boolean {
    try {
      return processStart(o.pid) !== o.pid_start;
    } catch {
      return true;
    }
  }
  private gate(o: Operation): void {
    current(this.store, o.principal);
    role(o.principal, "service");
    if (o.new_closed || o.tombstone) reject("invalid_state", "new_attempts_closed");
    const i = o.input,
      w = object(i.control_snapshot),
      task = object(i.task_ref),
      g = this.store.require("gates", text(task.object_id));
    if (
      g.goal_revision !== i.goal_revision ||
      g.control_revision !== i.control_revision ||
      g.control !== "running" ||
      g.status !== "active" ||
      Date.parse(text(i.deadline)) <= this.store.now() ||
      Date.parse(text(w.start_before)) <= this.store.now()
    )
      reject("invalid_state", "current_task_gate_closed_or_window_expired");
    use(this.store, o.principal, i.use_refs, this.ref(o.id, 1), text(i.intent_hash), "operation");
  }
  async prepare(p: Principal, c: Command): Promise<unknown> {
    if (c.method !== "execution.invoke" && c.method !== "execution.control") return undefined;
    const i = object(c.payload);
    if (c.method === "execution.invoke") {
      const old = this.store.get<Operation>("operations", text(i.operation_id));
      if (old?.tombstone) reject("invalid_state", "operation_permanently_cancelled");
      use(
        this.store,
        p,
        i.use_refs,
        this.ref(text(i.operation_id), 1),
        text(i.intent_hash),
        "operation",
      );
    }
    const w = object(i.control_snapshot),
      body = await this.peer.read(p, w.proof_ref, "control.proof");
    return body.toString("utf8");
  }
  private saveControl(p: Principal, task: Document, w: Document, proof: unknown): Document {
    role(p, "service");
    if (
      task.tenant_id !== this.store.config.tenant_id ||
      task.owner_id !== p.subject_id ||
      w.orchestrator_id !== p.subject_id ||
      w.task_id !== task.object_id ||
      integer(w.goal_revision) < 1 ||
      integer(w.control_revision) < 1
    )
      reject("forbidden", "original_orchestrator_required");
    const unsigned = { ...w, proof_ref: emptyContent };
    signed(
      this.store,
      text(proof),
      {
        tenant_id: this.store.config.tenant_id,
        issuer: w.orchestrator_id ?? null,
        audience: this.store.config.owner_id,
        purpose: "control",
        object_ref: { ...task, revision: 1 },
        digest: hash(canonical(unsigned)),
        control_revision: w.control_revision ?? null,
        window_id: w.window_id ?? null,
        issued_at: w.issued_at ?? null,
        start_before: w.start_before ?? null,
      },
      false,
    );
    if (Date.parse(text(w.start_before)) - Date.parse(text(w.issued_at)) > 5000)
      reject("invalid_request", "control_window_exceeds_profile");
    const fact = {
        orchestrator_id: w.orchestrator_id,
        task_id: w.task_id,
        goal_revision: w.goal_revision,
        control_revision: w.control_revision,
        status: w.status,
        control: w.control,
      },
      digest = hash(canonical(fact)),
      id = text(task.object_id),
      old = this.store.get("gates", id);
    if (old && integer(w.control_revision) < integer(old.control_revision))
      reject("revision_conflict", "task_gate_stale");
    if (old && w.control_revision === old.control_revision && old.control_digest !== digest)
      reject("idempotency_conflict", "control_fact_changed");
    const g: Document =
      old && w.control_revision === old.control_revision
        ? old
        : {
            task_ref: task,
            revision: old ? integer(old.revision) + 1 : 1,
            goal_revision: w.goal_revision ?? null,
            control_revision: w.control_revision ?? null,
            status: w.status ?? null,
            control: w.control ?? null,
            control_digest: digest,
          };
    if (!old || g !== old) this.store.put("gates", id, g);
    const windowID = text(w.window_id),
      previous = this.store.get("windows", windowID);
    if (previous && !same(previous, w)) reject("idempotency_conflict", "control_window_changed");
    if (!previous) {
      if (this.store.list("windows").filter((s) => s.task_id === task.object_id).length >= 32)
        reject("overloaded", "control_window_capacity");
      this.store.put("windows", windowID, w);
    }
    return g;
  }
  private controlView(taskID: string): Document {
    const gate = this.store.require("gates", taskID),
      windows = this.store.list("windows").filter((w) => w.task_id === taskID);
    return { gate, windows, windows_complete: true, windows_gaps: [] };
  }
  private fresh(id: string, p: Principal, input: Document, tombstone = false): Operation {
    return {
      id,
      operation: {
        operation_id: id,
        owner_id: this.store.config.owner_id,
        task_ref: input.task_ref ?? null,
        revision: 1,
        execution_state: tombstone ? "closed" : "accepted",
        effect: "not_started",
        may_apply_later: false,
        attempts: { collection_revision: 1, total_count: 0, unresolved_count: 0, complete: true },
        evidence_refs: [],
        usage: [{ unit: "USD", value: "0" }],
        usage_final: true,
        next_action: tombstone ? "none" : "wait",
      },
      input,
      principal: p,
      intent: null,
      args: null,
      phase: tombstone ? "cancelled" : "accepted",
      new_closed: tombstone,
      stopped: tombstone,
      attempt: null,
      result: stable("content", `${id}/result`),
      observation: null,
      tombstone,
      pid: 0,
      pid_start: "",
    };
  }
  command(p: Principal, c: Command, prepared?: unknown): { output: unknown; accepted?: boolean } {
    const i = object(c.payload);
    switch (c.method) {
      case "execution.invoke": {
        validateSchema({ $ref: "#/$defs/Time" }, i.deadline);
        const id = text(i.operation_id);
        if (c.target_id !== id) reject("invalid_request", "target_mismatch");
        if (!same(i.capability_ref, manifest.read_capability))
          reject("unsupported", "capability_not_in_profile");
        if (!same(i.binding_ref, this.store.config.binding_ref))
          reject("forbidden", "binding_not_registered");
        const old = this.store.get<Operation>("operations", id);
        if (this.store.list("operations", "", 201).length >= 200)
          reject("overloaded", "operation_profile_capacity");
        if (old)
          reject(
            old.tombstone ? "invalid_state" : "idempotency_conflict",
            old.tombstone ? "operation_permanently_cancelled" : "operation_identity_exists",
          );
        const task = object(i.task_ref),
          reservation = object(i.reservation_ref);
        if (
          reservation.tenant_id !== this.store.config.tenant_id ||
          reservation.owner_id !== task.owner_id
        )
          reject("forbidden", "original_reservation_required");
        const g = this.saveControl(p, task, object(i.control_snapshot), prepared);
        if (g.goal_revision !== i.goal_revision || g.control_revision !== i.control_revision)
          reject("invalid_state", "task_gate_stale");
        if (Date.parse(text(i.deadline)) > this.store.now() + 300000)
          reject("invalid_request", "finite_operation_deadline_required");
        const o = this.fresh(id, p, i);
        this.gate(o);
        this.store.put("operations", id, o);
        this.store.meta(
          "operations_head",
          String(Number(this.store.meta("operations_head") || 0) + 1),
        );
        raise(this.store, id, text(i.deadline));
        return { output: this.output(o) };
      }
      case "execution.cancel": {
        const id = text(i.operation_id),
          task = object(i.task_ref);
        if (
          c.target_id !== id ||
          task.owner_id !== p.subject_id ||
          i.orchestrator_id !== p.subject_id ||
          task.tenant_id !== this.store.config.tenant_id
        )
          reject("forbidden", "cancel_source_untrusted");
        role(p, "orchestrator");
        let o = this.store.get<Operation>("operations", id);
        if (!o) {
          if (this.store.list("operations", "", 201).length >= 200)
            reject("overloaded", "operation_profile_capacity");
          o = this.fresh(id, p, i, true);
          this.store.put("operations", id, o);
          this.store.meta(
            "operations_head",
            String(Number(this.store.meta("operations_head") || 0) + 1),
          );
          return { output: this.output(o) };
        }
        this.authorized(p, o);
        if (!same(object(o.operation.task_ref).object_id, task.object_id))
          reject("forbidden", "cancel_task_mismatch");
        if (!o.new_closed) {
          o.phase = "cancelled";
          this.closed(
            o,
            o.observation ? "applied" : o.attempt ? "unknown" : "not_started",
            o.attempt ? this.dead(o) : true,
          );
          this.bump(o);
        }
        return { output: this.output(o) };
      }
      case "execution.control": {
        const task = object(i.task_ref);
        if (c.target_id !== task.object_id) reject("invalid_request", "target_mismatch");
        const g = this.saveControl(p, task, object(i.control_snapshot), prepared);
        if (g.control !== "running" || g.status !== "active")
          for (const r of this.store.list("operations")) {
            const o = r as unknown as Operation;
            if (object(o.operation.task_ref).object_id !== task.object_id || o.new_closed) continue;
            o.phase = "cancelled";
            this.closed(
              o,
              o.observation ? "applied" : o.attempt ? "unknown" : "not_started",
              o.attempt ? this.dead(o) : true,
            );
            this.bump(o);
          }
        return { output: this.controlView(text(task.object_id)) };
      }
      case "execution.reconcile": {
        const id = text(i.operation_id);
        if (c.target_id !== id) reject("invalid_request", "target_mismatch");
        const o = this.store.require<Operation>("operations", id);
        this.authorized(p, o);
        if (i.source_revision && i.source_revision !== o.operation.revision)
          reject("revision_conflict", "original_source_changed");
        if (o.operation.effect === "unknown" && !o.stopped && this.dead(o)) {
          this.closed(o, "unknown", true);
          this.bump(o);
        }
        return { output: this.output(o) };
      }
      default:
        reject("unsupported", "method_not_supported");
    }
  }
  async query(p: Principal, q: Query): Promise<unknown> {
    const i = object(q.payload);
    if (q.method === "execution.control.get") {
      if (q.target_id !== i.task_id) reject("invalid_request", "target_mismatch");
      const view = this.controlView(text(i.task_id)),
        g = object(view.gate);
      if (object(g.task_ref).owner_id !== p.subject_id)
        reject("forbidden", "task_control_not_disclosed");
      return view;
    }
    if (q.method === "execution.list") {
      if (q.target_id !== this.store.config.owner_id) reject("invalid_request", "target_mismatch");
      const rows = this.store
        .list("operations", "", 201)
        .map((r) => r as unknown as Operation)
        .filter((o) => o.principal.subject_id === p.subject_id);
      return operationPage(
        this.store,
        p,
        q,
        rows.map((o) => this.output(o)),
        Math.max(1, Number(this.store.meta("operations_head") || 0)),
      );
    }
    const id = text(i.operation_id);
    if (q.target_id !== id) reject("invalid_request", "target_mismatch");
    const o = this.store.require<Operation>("operations", id);
    this.authorized(p, o);
    if (q.method === "execution.get")
      return {
        operation: o.operation,
        new_attempts_closed: o.new_closed,
        actually_stopped: o.stopped,
        effect_disputed: false,
        attempts: {
          items: o.attempt ? [o.attempt] : [],
          collection_revision: integer(o.operation.revision),
          exhausted: true,
          partial: false,
          gaps: [],
        },
      };
    if (q.method === "execution.usage.get") {
      if (o.tombstone) reject("accounting_unknown", "original_usage_basis_unavailable");
      const u: Document = {
        source_ref: this.ref(id, integer(o.operation.revision)),
        usage_revision: o.operation.revision ?? null,
        usage_digest: "",
        cumulative: o.operation.usage ?? [],
        spending_closed: o.new_closed,
        usage_final: o.operation.usage_final ?? false,
        proof_refs: [],
      };
      const proof: Document = {
        operation_ref: u.source_ref ?? null,
        intent_hash: o.input.intent_hash ?? null,
        usage_revision: o.operation.revision ?? null,
        cumulative: o.operation.usage ?? [],
        spending_closed: o.new_closed,
        usage_final: o.operation.usage_final ?? false,
        send_started_count: o.attempt ? 1 : 0,
        physical_count_min: o.observation ? 1 : 0,
        physical_count_max: o.attempt ? 1 : 0,
        attempts: o.attempt ? [o.attempt] : [],
      };
      const proofID = stable("content", `${id}/usage/${o.operation.revision}`);
      this.store.tx(() =>
        this.peer.plan(
          p,
          proofID,
          Buffer.from(canonical(proof)),
          "application/vnd.harness.usage-proof+json",
          [o.input.intent_ref ?? null, ...array(o.operation.evidence_refs)],
          [],
          new Date(this.store.now() + 60000).toISOString(),
        ),
      );
      const ref = await this.peer.publish(p, proofID);
      u.proof_refs = [ref];
      u.usage_digest = hash(canonical(u));
      return u;
    }
    reject("unsupported", "method_not_supported");
  }
  async advance(id: string): Promise<void> {
    let o = this.store.require<Operation>("operations", id);
    if (o.phase === "complete" || o.phase === "cancelled" || o.tombstone) return;
    if (o.phase === "started" && !o.observation) {
      this.store.tx(() => {
        o = this.store.require<Operation>("operations", id);
        this.closed(o, "unknown", this.dead(o));
        o.phase = "unknown";
        this.bump(o);
      });
      return;
    }
    if (o.phase === "accepted") {
      this.gate(o);
      const bytes = await this.peer.read(o.principal, o.input.intent_ref, "execution.arguments"),
        intent = object(parseStrict(bytes));
      validateSchema(manifest.schemas.execution_intent, intent);
      if (hash(canonical(intent)) !== o.input.intent_hash)
        reject("idempotency_conflict", "intent_hash_mismatch");
      for (const key of [
        "operation_id",
        "task_ref",
        "goal_revision",
        "control_revision",
        "capability_ref",
        "binding_ref",
        "deadline",
      ])
        if (!same(intent[key], o.input[key])) reject("forbidden", "intent_binding_mismatch");
      if (
        intent.executor_id !== this.store.config.owner_id ||
        !same(intent.install_lock_ref, this.store.config.install_lock_ref) ||
        array(intent.resource_refs).length ||
        intent.retry_of_operation_ref ||
        array(intent.cost_bound).some((v) => object(v).value !== "0")
      )
        reject("unsupported", "intent_not_in_profile");
      const argBytes = await this.peer.read(
          o.principal,
          intent.arguments_ref,
          "execution.arguments",
        ),
        args = object(parseStrict(argBytes));
      validateSchema(manifest.schemas.file_read_input, args);
      const path = text(args.path);
      if (
        !path ||
        path.length > 200 ||
        path === "." ||
        path === ".." ||
        path.includes("/") ||
        path.includes("\\") ||
        path.includes("\0") ||
        path.startsWith(".harness")
      )
        reject("forbidden", "path_outside_managed_root");
      for (const ref of array(intent.processed_source_refs))
        await this.peer.read(o.principal, ref, "execution.arguments");
      this.gate(o);
      this.store.tx(() => {
        o = this.store.require<Operation>("operations", id);
        this.gate(o);
        o.intent = intent;
        o.args = args;
        o.phase = "prepared";
        this.bump(o);
      });
    }
    o = this.store.require<Operation>("operations", id);
    if (o.phase === "prepared") {
      this.gate(o);
      if (!o.args || !o.intent) reject("invalid_state", "original_preparation_missing");
      let fd: number;
      try {
        fd = openSync(
          `/proc/self/fd/${this.root}/${text(o.args.path)}`,
          constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK,
        );
      } catch {
        reject("forbidden", "managed_file_not_regular");
      }
      try {
        const stat = fstatSync(fd, { bigint: true });
        if (!stat.isFile() || stat.nlink !== 1n || stat.size > 65536n)
          reject("forbidden", "managed_file_not_regular_or_bounded");
        this.store.tx(() => {
          o = this.store.require<Operation>("operations", id);
          this.gate(o);
          if (o.phase !== "prepared") reject("invalid_state", "attempt_already_started");
          const attemptID = stable("attempt", id),
            w = object(o.input.control_snapshot);
          o.attempt = {
            attempt_id: attemptID,
            operation_id: id,
            revision: 1,
            attempt_no: 1,
            phase: "started",
            request_digest: hash(canonical({ intent: o.intent, args: o.args })),
            control_window_id: w.window_id ?? null,
            effect: "unknown",
            may_apply_later: "unknown",
            started_at: new Date(this.store.now()).toISOString(),
            fact_revision: 1,
            evidence_refs: [],
            usage: [{ unit: "USD", value: "0" }],
            usage_final: true,
            effect_disputed: false,
            actually_stopped: false,
            cell_committed: false,
          };
          o.phase = "started";
          o.new_closed = true;
          o.pid = process.pid;
          o.pid_start = processStart(process.pid);
          o.operation.execution_state = "started";
          o.operation.effect = "unknown";
          o.operation.may_apply_later = "unknown";
          o.operation.attempts = {
            collection_revision: integer(o.operation.revision) + 1,
            total_count: 1,
            unresolved_count: 1,
            complete: false,
          };
          this.bump(o);
        });
        if (this.store.config.fault?.crash_after_start) process.kill(process.pid, "SIGKILL");
        const buffer = Buffer.alloc(65537);
        let n = 0;
        while (n < buffer.length) {
          const got = readSync(fd, buffer, n, buffer.length - n, n);
          if (!got) break;
          n += got;
        }
        const after = fstatSync(fd, { bigint: true });
        if (
          n > 65536 ||
          stat.dev !== after.dev ||
          stat.ino !== after.ino ||
          stat.size !== after.size ||
          stat.mtimeNs !== after.mtimeNs ||
          BigInt(n) !== after.size
        )
          reject("revision_conflict", "source_changed_during_read");
        const body = buffer.subarray(0, n),
          result: Document = {
            path: o.args?.path ?? null,
            version: hash(body),
            data_base64: body.toString("base64"),
            observed_at: new Date(this.store.now()).toISOString(),
          };
        validateSchema(manifest.schemas.file_read_output, result);
        this.store.tx(() => {
          o = this.store.require<Operation>("operations", id);
          o.observation = result;
          o.phase = "observed";
          o.stopped = true;
          o.operation.effect = "applied";
          o.operation.may_apply_later = false;
          if (o.attempt) {
            o.attempt.effect = "applied";
            o.attempt.may_apply_later = false;
            o.attempt.observed_at = result.observed_at ?? null;
            o.attempt.actually_stopped = true;
            o.attempt.fact_revision = 2;
          }
          this.bump(o);
        });
      } finally {
        closeSync(fd);
      }
    }
    o = this.store.require<Operation>("operations", id);
    if (o.phase === "observed") {
      current(this.store, o.principal);
      use(
        this.store,
        o.principal,
        o.input.use_refs,
        this.ref(o.id, 1),
        text(o.input.intent_hash),
        "operation",
      );
      if (!o.intent || !o.observation) reject("invalid_state", "original_observation_missing");
      const sources = [
        o.input.intent_ref ?? null,
        o.intent.arguments_ref ?? null,
        ...array(o.intent.processed_source_refs),
      ];
      for (const ref of sources) await this.peer.read(o.principal, ref, "execution.arguments");
      this.store.tx(() =>
        this.peer.plan(
          o.principal,
          o.result,
          Buffer.from(canonical(o.observation)),
          "application/json",
          sources,
          [],
          text(o.input.deadline),
        ),
      );
      const result = await this.peer.publish(o.principal, o.result);
      await this.peer.read(o.principal, result, "execution.result");
      this.store.tx(() => {
        o = this.store.require<Operation>("operations", id);
        o.operation.result_ref = result;
        o.operation.evidence_refs = [result];
        this.closed(o, "applied");
        if (o.attempt) {
          o.attempt.result_ref = result;
          o.attempt.evidence_refs = [result];
        }
        o.phase = "complete";
        this.bump(o);
      });
    }
  }
  async stop(): Promise<void> {
    const done = this.runner.stop();
    this.peer.stop();
    await done;
    closeSync(this.root);
  }
}
function processStart(pid: number): string {
  const stat = readFileSync(`/proc/${pid}/stat`, "utf8"),
    end = stat.lastIndexOf(")");
  return `${readFileSync("/proc/sys/kernel/random/boot_id", "utf8").trim()}:${stat.slice(end + 2).split(" ")[19] ?? ""}`;
}
