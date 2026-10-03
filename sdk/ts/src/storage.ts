import { canonical, ProtocolError } from "./json";
import type { MethodContract, Receipt } from "./protocol";
export interface StoredCommand {
  version: 1;
  identity_scope: string;
  logical_service_id: string;
  command_id: string;
  command_json: string;
  request_digest: string;
  core_schema_digest: string;
  contract: MethodContract;
  status: "pending" | "done";
  receipt?: Receipt;
}
export interface CommandStore {
  readonly identityScope: string;
  save(command: StoredCommand): Promise<StoredCommand>;
  receipt(owner: string, commandID: string, receipt: Receipt): Promise<void>;
  pending(): Promise<StoredCommand[]>;
  get(owner: string, commandID: string): Promise<StoredCommand | undefined>;
  close(): Promise<void>;
}
export interface IndexedDBOptions {
  name?: string;
  factory?: IDBFactory;
  maxPending?: number;
  maxRecords?: number;
}

/** Promise 只在 IndexedDB 事务 complete 后成功；request.onsuccess 不是耐久提交。 */
export class IndexedDBCommands implements CommandStore {
  private readonly database: Promise<IDBDatabase>;
  private readonly maxPending: number;
  private readonly maxRecords: number;
  constructor(
    readonly identityScope: string,
    options: IndexedDBOptions = {},
  ) {
    const factory = options.factory ?? globalThis.indexedDB;
    this.maxPending = options.maxPending ?? 32;
    this.maxRecords = options.maxRecords ?? 1000;
    this.database = new Promise((resolve, reject) => {
      if (!factory || !identityScope) {
        reject(new ProtocolError("durable_storage_unavailable"));
        return;
      }
      const open = factory.open(options.name ?? "harness-original-commands-v1", 1);
      open.onupgradeneeded = () => {
        const table = open.result.createObjectStore("commands", {
          keyPath: ["identity_scope", "logical_service_id", "command_id"],
        });
        table.createIndex("scope", "identity_scope");
        table.createIndex("pending", ["identity_scope", "status"]);
      };
      open.onerror = () => reject(open.error ?? new ProtocolError("durable_storage_unavailable"));
      open.onblocked = () => reject(new ProtocolError("durable_storage_blocked"));
      open.onsuccess = () => {
        const database = open.result;
        database.onversionchange = () => database.close();
        resolve(database);
      };
    });
    // 不可用状态保持在公开调用处报告，避免构造后产生未处理的 rejection。
    void this.database.catch(() => undefined);
  }
  async save(command: StoredCommand): Promise<StoredCommand> {
    if (command.identity_scope !== this.identityScope)
      throw new ProtocolError("identity_scope_mismatch");
    const database = await this.database;
    return new Promise((resolve, reject) => {
      const tx = database.transaction("commands", "readwrite", { durability: "strict" });
      const table = tx.objectStore("commands");
      let result = command;
      let failure: Error | undefined;
      const abort = (reason: string) => {
        failure = new ProtocolError(reason);
        tx.abort();
      };
      tx.oncomplete = () => resolve(result);
      tx.onabort = () => reject(failure ?? tx.error ?? new ProtocolError("durable_storage_failed"));
      tx.onerror = () => {
        failure ??= tx.error ?? new ProtocolError("durable_storage_failed");
      };
      const read = table.get([this.identityScope, command.logical_service_id, command.command_id]);
      read.onsuccess = () => {
        const previous = read.result as StoredCommand | undefined;
        if (previous) {
          if (
            previous.request_digest !== command.request_digest ||
            previous.command_json !== command.command_json ||
            previous.core_schema_digest !== command.core_schema_digest ||
            previous.contract.schema_digest !== command.contract.schema_digest
          ) {
            abort("idempotency_conflict");
            return;
          }
          result = previous;
          return;
        }
        const pending = table.index("pending").count([this.identityScope, "pending"]);
        pending.onsuccess = () => {
          if (pending.result >= this.maxPending) {
            abort("durable_pending_queue_full");
            return;
          }
          const total = table.index("scope").count(this.identityScope);
          total.onsuccess = () => {
            if (total.result >= this.maxRecords) abort("durable_archive_full");
            else table.add(command);
          };
        };
      };
    });
  }
  async receipt(owner: string, commandID: string, receipt: Receipt): Promise<void> {
    const database = await this.database;
    return new Promise((resolve, reject) => {
      const tx = database.transaction("commands", "readwrite", { durability: "strict" });
      const table = tx.objectStore("commands");
      let failure: Error | undefined;
      tx.oncomplete = () => resolve();
      tx.onabort = () => reject(failure ?? tx.error ?? new ProtocolError("durable_storage_failed"));
      tx.onerror = () => {
        failure ??= tx.error ?? new ProtocolError("durable_storage_failed");
      };
      const request = table.get([this.identityScope, owner, commandID]);
      request.onsuccess = () => {
        const original = request.result as StoredCommand | undefined;
        if (
          !original ||
          original.request_digest !== receipt.request_digest ||
          receipt.command_id !== commandID ||
          (original.status === "done" && canonical(original.receipt) !== canonical(receipt))
        ) {
          failure = new ProtocolError("receipt_identity_conflict");
          tx.abort();
          return;
        }
        table.put({
          ...original,
          receipt,
          status: receipt.stage === "accepted" ? "pending" : "done",
        });
      };
    });
  }
  async pending(): Promise<StoredCommand[]> {
    const database = await this.database;
    return new Promise((resolve, reject) => {
      const tx = database.transaction("commands", "readonly");
      const read = tx
        .objectStore("commands")
        .index("pending")
        .getAll([this.identityScope, "pending"], this.maxPending + 1);
      tx.oncomplete = () => {
        if (read.result.length > this.maxPending)
          reject(new ProtocolError("durable_pending_queue_full"));
        else resolve(read.result as StoredCommand[]);
      };
      tx.onabort = () => reject(tx.error ?? new ProtocolError("durable_storage_failed"));
    });
  }
  async get(owner: string, commandID: string): Promise<StoredCommand | undefined> {
    const database = await this.database;
    return new Promise((resolve, reject) => {
      const tx = database.transaction("commands", "readonly");
      const read = tx.objectStore("commands").get([this.identityScope, owner, commandID]);
      tx.oncomplete = () => resolve(read.result as StoredCommand | undefined);
      tx.onabort = () => reject(tx.error ?? new ProtocolError("durable_storage_failed"));
    });
  }
  async forgetCompleted(owner: string, commandID: string): Promise<void> {
    const database = await this.database;
    return new Promise((resolve, reject) => {
      const tx = database.transaction("commands", "readwrite", { durability: "strict" });
      const table = tx.objectStore("commands");
      let failure: Error | undefined;
      const read = table.get([this.identityScope, owner, commandID]);
      read.onsuccess = () => {
        if (read.result?.status === "done") table.delete([this.identityScope, owner, commandID]);
        else {
          failure = new ProtocolError("unresolved_command_cannot_forget");
          tx.abort();
        }
      };
      tx.oncomplete = () => resolve();
      tx.onabort = () => reject(failure ?? tx.error ?? new ProtocolError("durable_storage_failed"));
    });
  }
  async close(): Promise<void> {
    (await this.database).close();
  }
}
