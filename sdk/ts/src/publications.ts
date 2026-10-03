import { endpoint, getJSON, isObject, newID, readBounded } from "./client";
import type { HarnessClient } from "./client";
import { canonical, digest, ProtocolError, sha256 } from "./json";
import type { JSONValue } from "./json";
import { RequestError } from "./protocol";
import type { Command, Receipt } from "./protocol";
import type { ComponentRef, ContentRef } from "./contracts.gen";
import { apiError, validateRecord } from "./schema";
export interface PublicationIntent {
  version: 1;
  identity_scope: string;
  transfer_id: string;
  content_ref: ContentRef;
  bytes: Uint8Array;
  reserve_command: Command;
  put_command: Command;
  next_command?: Command;
  intent_digest: string;
  state: "pending" | "done";
}
export interface PublicationStore {
  readonly identityScope: string;
  save(intent: PublicationIntent): Promise<PublicationIntent>;
  pending(): Promise<PublicationIntent[]>;
  finish(transferID: string): Promise<void>;
  close(): Promise<void>;
}
export class IndexedDBPublications implements PublicationStore {
  private readonly database: Promise<IDBDatabase>;
  constructor(
    readonly identityScope: string,
    options: { factory?: IDBFactory; name?: string } = {},
  ) {
    this.database = new Promise((resolve, reject) => {
      const factory = options.factory ?? globalThis.indexedDB;
      if (!factory) {
        reject(new ProtocolError("durable_storage_unavailable"));
        return;
      }
      const open = factory.open(options.name ?? "harness-content-publications-v1", 1);
      open.onupgradeneeded = () => {
        const table = open.result.createObjectStore("publications", {
          keyPath: ["identity_scope", "transfer_id"],
        });
        table.createIndex("pending", ["identity_scope", "state"]);
        table.createIndex("scope", "identity_scope");
      };
      open.onerror = () => reject(open.error ?? new ProtocolError("durable_storage_failed"));
      open.onblocked = () => reject(new ProtocolError("durable_storage_blocked"));
      open.onsuccess = () => {
        open.result.onversionchange = () => open.result.close();
        resolve(open.result);
      };
    });
    void this.database.catch(() => undefined);
  }
  async save(intent: PublicationIntent): Promise<PublicationIntent> {
    if (intent.identity_scope !== this.identityScope || intent.bytes.byteLength > 262144)
      throw new ProtocolError("publication_scope_or_size_mismatch");
    const database = await this.database;
    return new Promise((resolve, reject) => {
      const tx = database.transaction("publications", "readwrite", { durability: "strict" });
      const table = tx.objectStore("publications");
      let original = intent;
      let failure: Error | undefined;
      tx.oncomplete = () => resolve(original);
      tx.onabort = () => reject(failure ?? tx.error ?? new ProtocolError("durable_storage_failed"));
      const read = table.get([this.identityScope, intent.transfer_id]);
      read.onsuccess = () => {
        const stored = read.result as PublicationIntent | undefined;
        if (stored) {
          if (stored.intent_digest !== intent.intent_digest) {
            failure = new ProtocolError("publication_identity_conflict");
            tx.abort();
          } else original = stored;
          return;
        }
        const count = table.index("pending").count([this.identityScope, "pending"]);
        count.onsuccess = () => {
          if (count.result >= 8) {
            failure = new ProtocolError("publication_queue_full");
            tx.abort();
            return;
          }
          const total = table.index("scope").count(this.identityScope);
          total.onsuccess = () => {
            if (total.result >= 100) {
              failure = new ProtocolError("publication_archive_full");
              tx.abort();
            } else table.add(intent);
          };
        };
      };
    });
  }
  async pending(): Promise<PublicationIntent[]> {
    const database = await this.database;
    return new Promise((resolve, reject) => {
      const tx = database.transaction("publications", "readonly");
      const read = tx
        .objectStore("publications")
        .index("pending")
        .getAll([this.identityScope, "pending"], 9);
      tx.oncomplete = () => {
        if (read.result.length > 8) reject(new ProtocolError("publication_queue_full"));
        else resolve(read.result as PublicationIntent[]);
      };
      tx.onabort = () => reject(tx.error ?? new ProtocolError("durable_storage_failed"));
    });
  }
  async finish(transferID: string): Promise<void> {
    const database = await this.database;
    return new Promise((resolve, reject) => {
      const tx = database.transaction("publications", "readwrite", { durability: "strict" });
      const table = tx.objectStore("publications");
      const read = table.get([this.identityScope, transferID]);
      let failure: Error | undefined;
      read.onsuccess = () => {
        if (!read.result) {
          failure = new ProtocolError("original_publication_missing");
          tx.abort();
        } else {
          const original = read.result as PublicationIntent;
          table.put({ ...original, state: "done" });
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
export async function preparePublication(
  client: HarnessClient,
  input: {
    bytes: Uint8Array;
    tenantID: string;
    mediaType: string;
    policyRef: ComponentRef;
    retentionUntil: string;
    transferDeadline: string;
    expiresAt: string;
    next?: (ref: ContentRef) => Command;
    processedSources?: ContentRef[];
    disclosedSources?: ContentRef[];
  },
): Promise<PublicationIntent> {
  validateRecord("ComponentRef", input.policyRef);
  const content: ContentRef = {
    tenant_id: input.tenantID,
    owner_id: client.registry.discovery.logical_service_id,
    content_id: newID("content"),
    version: 1,
    hash: await sha256(input.bytes),
    media_type: input.mediaType,
    byte_length: input.bytes.byteLength,
  };
  validateRecord("ContentRef", content);
  const transfer = newID("transfer");
  const reserve = client.makeCommand(
    "content.upload_reserve",
    content.content_id,
    {
      transfer_id: transfer,
      content_ref: content as unknown as JSONValue,
      policy_ref: input.policyRef as unknown as JSONValue,
      processed_sources: (input.processedSources ?? []) as unknown as JSONValue,
      retention_until: input.retentionUntil,
      transfer_deadline: input.transferDeadline,
    },
    input.expiresAt,
  );
  const put = client.makeCommand(
    "content.put",
    content.content_id,
    {
      content_ref: content as unknown as JSONValue,
      transfer_id: transfer,
      policy_ref: input.policyRef as unknown as JSONValue,
      processed_sources: (input.processedSources ?? []) as unknown as JSONValue,
      disclosed_sources: (input.disclosedSources ?? []) as unknown as JSONValue,
      retention_until: input.retentionUntil,
    },
    input.expiresAt,
  );
  const next = input.next?.(content);
  client.registry.command(reserve);
  client.registry.command(put);
  if (next) client.registry.command(next);
  const intent: PublicationIntent = {
    version: 1,
    identity_scope: client.registry.discovery.identity_scope,
    transfer_id: transfer,
    content_ref: content,
    bytes: new Uint8Array(input.bytes),
    reserve_command: reserve,
    put_command: put,
    ...(next ? { next_command: next } : {}),
    intent_digest: "",
    state: "pending",
  };
  intent.intent_digest = await publicationDigest(intent);
  return intent;
}
async function publicationDigest(intent: PublicationIntent): Promise<string> {
  return digest([
    intent.version,
    intent.identity_scope,
    intent.transfer_id,
    intent.content_ref,
    await sha256(intent.bytes),
    intent.reserve_command,
    intent.put_command,
    intent.next_command ?? null,
  ]);
}
function requireApplied(receipt: Receipt): void {
  if (receipt.stage === "rejected" && receipt.error) throw new RequestError(receipt.error);
  if (receipt.stage !== "applied") throw new ProtocolError("original_preparation_pending");
}
export async function currentCSRF(
  options: { baseURL?: string; fetcher?: typeof fetch } = {},
): Promise<string> {
  const response = await getJSON(
    options.fetcher ?? fetch,
    endpoint(new URL(options.baseURL ?? globalThis.location.origin), "/auth/session"),
    16384,
  );
  if (
    !isObject(response) ||
    response.authenticated !== true ||
    typeof response.csrf_token !== "string" ||
    response.csrf_token.length > 4096 ||
    !response.csrf_token
  )
    throw new ProtocolError("current_browser_session_unavailable");
  return response.csrf_token;
}
/** 准确 bytes 与后续原命令先共同保存；恢复不改变引用、身份、CAS 或期限。 */
export async function publishOriginal(
  client: HarnessClient,
  store: PublicationStore,
  intent: PublicationIntent,
  options: { baseURL?: string; fetcher?: typeof fetch; onStage?: (stage: string) => void } = {},
): Promise<{ content_ref: ContentRef; receipt?: Receipt }> {
  if (
    store.identityScope !== client.registry.discovery.identity_scope ||
    intent.identity_scope !== store.identityScope ||
    (await publicationDigest(intent)) !== intent.intent_digest ||
    (await sha256(intent.bytes)) !== intent.content_ref.hash ||
    intent.bytes.byteLength !== intent.content_ref.byte_length
  )
    throw new ProtocolError("publication_original_identity_mismatch");
  const original = await store.save(intent);
  options.onStage?.("准确原文与投递意图已耐久保存");
  requireApplied(await client.command(original.reserve_command));
  options.onStage?.("原上传额度与票据已接纳；正文尚未发布");
  const transferMethod = client.registry.method("content.transfer.read", "query");
  const transfer = await client.query(
    client.makeQuery(transferMethod.name, original.transfer_id, {
      transfer_id: original.transfer_id,
    }),
  );
  if (
    !isObject(transfer) ||
    canonical(transfer.content_ref) !== canonical(original.content_ref) ||
    transfer.transfer_id !== original.transfer_id ||
    !["reserved", "writing", "ready", "published"].includes(String(transfer.phase))
  )
    throw new ProtocolError("original_transfer_unavailable");
  if (transfer.phase === "reserved" || transfer.phase === "writing") {
    const fetcher = options.fetcher ?? fetch;
    const base = new URL(options.baseURL ?? globalThis.location.origin);
    const csrf = await currentCSRF({ fetcher, baseURL: base.href });
    const response = await fetcher(endpoint(base, `/api/transfers/${original.transfer_id}`), {
      method: "POST",
      credentials: "same-origin",
      redirect: "error",
      headers: { "Content-Type": "application/octet-stream", "X-CSRF-Token": csrf },
      body: new Uint8Array(original.bytes).buffer,
    });
    const body = parseResponse(await readBounded(response, 16384));
    if (!response.ok)
      throw new RequestError(apiError(isObject(body) && body.error ? body.error : body));
    client.registry.validateOutput(transferMethod.name, body);
    if (
      !isObject(body) ||
      body.transfer_id !== original.transfer_id ||
      !["ready", "published"].includes(String(body.phase)) ||
      canonical(body.content_ref) !== canonical(original.content_ref)
    )
      throw new ProtocolError("transfer_not_durable");
  }
  options.onStage?.("准确 bytes 已耐久；等待 Content 发布决定");
  requireApplied(await client.command(original.put_command));
  options.onStage?.("准确 Content 已发布；尚未代表 Task 接纳或完成");
  let receipt: Receipt | undefined;
  if (original.next_command) {
    receipt = await client.command(original.next_command);
    if (receipt.stage === "accepted") options.onStage?.("原准备已保存，仍须沿原命令查询业务决定");
    else if (receipt.stage === "rejected")
      options.onStage?.("原后续业务被拒绝；准确 Content 的清理责任另核");
    else options.onStage?.("原业务决定已提交；执行、效果与最终 Result 分别核对");
  }
  if (receipt?.stage !== "accepted") await store.finish(original.transfer_id);
  return { content_ref: original.content_ref, ...(receipt ? { receipt } : {}) };
}
import { parseStrict } from "./json";
function parseResponse(bytes: Uint8Array): JSONValue {
  return parseStrict(bytes, 16384);
}
