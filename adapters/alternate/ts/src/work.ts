import type { APIError, JSONValue } from "@harness/sdk";
import { validateSchema } from "@harness/sdk";
import type { Store } from "./store";
import { manifest, type Ledger } from "./runtime";
import { failure, Rejection, reject } from "./error";

export interface Job {
  id: string;
  state: "pending" | "complete" | "failed";
  attempts: number;
  due: number;
  deadline: number;
  last_error: APIError | null;
}
export function raise(store: Store, id: string, deadline: string): void {
  const old = store.get<Job>("jobs", id);
  if (old) {
    if (old.state !== "pending") return;
    return;
  }
  if (store.list("jobs").filter((j) => j.state === "pending").length >= 32)
    reject("overloaded", "work_capacity");
  store.put("jobs", id, {
    id,
    state: "pending",
    attempts: 0,
    due: store.now(),
    deadline: Date.parse(deadline),
    last_error: null,
  });
}
export function settle(
  store: Store,
  commandID: string,
  output?: JSONValue,
  error?: APIError,
): void {
  const ledger = store.require<Ledger>("commands", commandID);
  if (ledger.receipt.stage !== "accepted") return;
  const method = manifest.methods[store.config.system].find(
    (m) => m.name === ledger.command.method,
  );
  if (!method) reject("unsupported", "original_method_missing");
  if (!error) validateSchema(method.output_schema, output);
  ledger.receipt = {
    command_id: commandID,
    request_digest: ledger.digest,
    stage: error ? "rejected" : "applied",
    accepted_at: ledger.receipt.accepted_at ?? new Date(store.now()).toISOString(),
    decided_at: new Date(store.now()).toISOString(),
    ...(error ? { error } : { output: output ?? null }),
  };
  store.put("commands", commandID, ledger);
}
export class Runner {
  private timer?: ReturnType<typeof setInterval>;
  private active: Promise<void> | undefined;
  private stopping = false;
  constructor(
    readonly store: Store,
    readonly advance: (id: string) => Promise<void>,
    readonly terminal: (id: string, error: APIError) => void,
  ) {}
  start(): void {
    this.timer = setInterval(() => {
      if (this.stopping || this.active || this.store.config.fault?.pause_jobs) return;
      this.active = this.tick().finally(() => {
        this.active = undefined;
      });
    }, 100);
  }
  private async tick(): Promise<void> {
    let job: Job | undefined;
    try {
      this.store.assertCurrent();
      job = this.store
        .list("jobs", "", 100)
        .find((j) => j.state === "pending" && Number(j.due) <= this.store.now()) as unknown as
        | Job
        | undefined;
      if (!job) return;
      if (job.deadline <= this.store.now() || job.attempts >= 8)
        reject("expired", "original_work_window_closed");
      this.store.tx(() => {
        const n = this.store.require<Job>("jobs", job?.id ?? "");
        n.attempts++;
        this.store.put("jobs", n.id, n);
      });
      await this.advance(job.id);
      if (this.stopping) return;
      this.store.tx(() => {
        const n = this.store.require<Job>("jobs", job?.id ?? "");
        n.state = "complete";
        this.store.put("jobs", n.id, n);
      });
    } catch (error) {
      if (!job || this.stopping) return;
      try {
        this.store.tx(() => {
          const n = this.store.require<Job>("jobs", job?.id ?? "");
          const wire = failure(error);
          n.last_error = wire;
          if (
            error instanceof Rejection &&
            ["dependency_unavailable", "overloaded"].includes(wire.code) &&
            n.attempts < 8 &&
            n.deadline > this.store.now()
          ) {
            n.due = this.store.now() + Math.min(1000, 100 * 2 ** n.attempts);
          } else {
            n.state = "failed";
            this.terminal(n.id, wire);
          }
          this.store.put("jobs", n.id, n);
        });
      } catch {
        this.stopping = true;
        if (this.timer) clearInterval(this.timer);
      }
    }
  }
  async stop(): Promise<void> {
    this.stopping = true;
    if (this.timer) clearInterval(this.timer);
    await this.active;
  }
}
