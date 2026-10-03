import { canonical, digest, jsonBytes, parseStrict, ProtocolError, sha256 } from "./json";
import type { JSONValue } from "./json";
import { ContractRegistry, apiError, responseFrame, validateRecord } from "./schema";
import { IndexedDBCommands } from "./storage";
import type { CommandStore, IndexedDBOptions, StoredCommand } from "./storage";
import { PROFILE, PROTOCOL, RequestError, SOCKET_SUBPROTOCOL, TRANSPORT_PROFILE } from "./protocol";
import type {
  Command,
  ConnectionState,
  Query,
  Receipt,
  ReceiptLookup,
  ResponseFrame,
} from "./protocol";
import type { ContentRef } from "./contracts.gen";

const MAX_QUEUE_BYTES = 4 * 1024 * 1024;
const CONTROL_BYTES = 1024 * 1024;
const CONTROL_SLOTS = 4;
const controlMethods = new Set([
  "task.cancel",
  "task.pause",
  "task.resume",
  "execution.cancel",
  "grant.revoke",
  "schedule.pause",
  "schedule.delete",
]);
export function newID(prefix: string): string {
  if (!/^[a-z][a-z0-9_]*$/.test(prefix)) throw new ProtocolError("invalid_id_prefix");
  return `${prefix}_${Array.from(crypto.getRandomValues(new Uint8Array(16)), (x) => x.toString(16).padStart(2, "0")).join("")}`;
}
interface Waiting {
  kind: "command" | "query" | "receipt_lookup";
  payload: Command | Query | ReceiptLookup;
  control: boolean;
  size: number;
  decode: (frame: ResponseFrame) => Promise<JSONValue | Receipt>;
  resolve: (value: JSONValue | Receipt) => void;
  reject: (error: unknown) => void;
  timer?: ReturnType<typeof setTimeout>;
  sequence?: number;
}
export interface ClientOptions {
  registry: ContractRegistry;
  store: CommandStore;
  url: string;
  requestTimeoutMs?: number;
  connectTimeoutMs?: number;
}
export interface ServerOptions {
  baseURL?: string;
  fetcher?: typeof fetch;
  storage?: IndexedDBOptions;
  requestTimeoutMs?: number;
}
export interface RecoveryResult {
  command_id: string;
  receipt?: Receipt;
  error?: string;
}

/** 原生 WebSocket 客户端；唯一出队循环才分配序号，业务责任不随等待结束而删除。 */
export class HarnessClient {
  readonly registry: ContractRegistry;
  readonly store: CommandStore;
  private readonly url: string;
  private readonly timeout: number;
  private readonly connectTimeout: number;
  private socket: WebSocket | undefined;
  private epoch = 0;
  private sequence = 0;
  private queue: Waiting[] = [];
  private readonly waiting = new Map<number, Waiting>();
  private state: ConnectionState = "disconnected";
  private connecting: Promise<void> | undefined;
  private connectionReject: ((error: unknown) => void) | undefined;
  private connectTimer: ReturnType<typeof setTimeout> | undefined;
  private flushTimer: ReturnType<typeof setTimeout> | undefined;
  private heartbeat: ReturnType<typeof setInterval> | undefined;
  private lastReceived = 0;
  private incomingBytes = 0;
  private readonly originalDecoders = new Map<string, Promise<ContractRegistry>>();
  private readonly listeners = new Set<(state: ConnectionState) => void>();
  constructor(options: ClientOptions) {
    if (options.store.identityScope !== options.registry.discovery.identity_scope)
      throw new ProtocolError("identity_scope_mismatch");
    this.registry = options.registry;
    this.store = options.store;
    this.url = options.url;
    this.timeout = options.requestTimeoutMs ?? 30000;
    this.connectTimeout = options.connectTimeoutMs ?? 10000;
    const url = new URL(this.url);
    if (
      url.username ||
      url.password ||
      url.search ||
      url.hash ||
      !["ws:", "wss:"].includes(url.protocol)
    )
      throw new ProtocolError("unsafe_connect_url");
    if (url.protocol === "ws:" && !["127.0.0.1", "localhost", "[::1]"].includes(url.hostname))
      throw new ProtocolError("tls_required");
  }
  static async fromServer(options: ServerOptions = {}): Promise<HarnessClient> {
    const base = new URL(options.baseURL ?? globalThis.location.origin);
    const fetcher = options.fetcher ?? fetch;
    const publicInfo = await getJSON(fetcher, endpoint(base, "/.well-known/harness"), 16384);
    if (
      !isObject(publicInfo) ||
      publicInfo.protocol !== PROTOCOL ||
      publicInfo.transport_profile !== TRANSPORT_PROFILE ||
      typeof publicInfo.connect_path !== "string" ||
      typeof publicInfo.login_path !== "string" ||
      Object.keys(publicInfo).some(
        (key) => !["protocol", "transport_profile", "login_path", "connect_path"].includes(key),
      )
    )
      throw new ProtocolError("invalid_public_discovery");
    const discovery = await getJSON(fetcher, endpoint(base, "/api/discovery"), 1048576);
    if (!isObject(discovery) || typeof discovery.core_schema_path !== "string")
      throw new ProtocolError("invalid_discovery");
    const schemaResponse = await fetcher(endpoint(base, discovery.core_schema_path), {
      credentials: "same-origin",
      redirect: "error",
    });
    if (!schemaResponse.ok) throw new ProtocolError("schema_unavailable");
    const schema = await readBounded(schemaResponse, 1048576);
    const registry = await ContractRegistry.create(discovery, schema);
    const connect = endpoint(base, publicInfo.connect_path);
    connect.protocol = connect.protocol === "https:" ? "wss:" : "ws:";
    return new HarnessClient({
      registry,
      store: new IndexedDBCommands(registry.discovery.identity_scope, {
        ...options.storage,
        maxPending: registry.discovery.limits.max_pending,
      }),
      url: connect.href,
      ...(options.requestTimeoutMs ? { requestTimeoutMs: options.requestTimeoutMs } : {}),
    });
  }
  get connectionState(): ConnectionState {
    return this.state;
  }
  subscribe(listener: (state: ConnectionState) => void): () => void {
    this.listeners.add(listener);
    listener(this.state);
    return () => {
      this.listeners.delete(listener);
    };
  }
  private setState(state: ConnectionState): void {
    this.state = state;
    for (const listener of this.listeners) listener(state);
  }
  async connect(): Promise<void> {
    if (this.state === "closed") throw new ProtocolError("client_closed");
    if (this.state === "ready") return;
    if (this.connecting) return this.connecting;
    const epoch = ++this.epoch;
    this.sequence = 0;
    this.setState("connecting");
    let socket: WebSocket;
    try {
      socket = new WebSocket(this.url, SOCKET_SUBPROTOCOL);
    } catch (error) {
      this.setState("disconnected");
      throw error;
    }
    this.socket = socket;
    this.connecting = new Promise<void>((resolve, reject) => {
      this.connectionReject = reject;
      this.connectTimer = setTimeout(
        () => this.disconnect(new ProtocolError("ready_timeout")),
        this.connectTimeout,
      );
      socket.addEventListener("message", (event) => {
        if (epoch !== this.epoch) return;
        try {
          if (typeof event.data !== "string") throw new ProtocolError("text_frame_required");
          const frame = parseStrict(event.data, this.registry.discovery.limits.max_frame_bytes);
          if (!isObject(frame) || typeof frame.type !== "string")
            throw new ProtocolError("invalid_frame");
          this.lastReceived = Date.now();
          if (frame.type === "ready") {
            if (this.state !== "connecting") throw new ProtocolError("duplicate_ready");
            this.registry.ready(frame);
            clearTimeout(this.connectTimer);
            this.connectTimer = undefined;
            this.connectionReject = undefined;
            this.setState("ready");
            this.connecting = undefined;
            this.heartbeat = setInterval(() => {
              if (Date.now() - this.lastReceived >= 90000)
                this.disconnect(new ProtocolError("heartbeat_timeout"));
              else this.sendControl({ type: "ping", nonce: newID("ping") });
            }, 30000);
            resolve();
            this.flush();
            return;
          }
          if (this.state !== "ready") throw new ProtocolError("frame_before_ready");
          if (frame.type === "ping" || frame.type === "pong") {
            if (
              Object.keys(frame).length !== 2 ||
              typeof frame.nonce !== "string" ||
              frame.nonce.length > 256
            )
              throw new ProtocolError("invalid_heartbeat");
            if (frame.type === "ping") this.sendControl({ type: "pong", nonce: frame.nonce });
            return;
          }
          const response = responseFrame(frame);
          const waiting = this.waiting.get(response.request_seq);
          if (!waiting) return; // 原已过等待期限的序号不能完成任何新请求。
          this.waiting.delete(response.request_seq);
          const bytes = jsonBytes(
            response.payload,
            this.registry.discovery.limits.max_domain_bytes,
          ).byteLength;
          this.incomingBytes += bytes;
          if (this.incomingBytes > MAX_QUEUE_BYTES) {
            clearTimeout(waiting.timer);
            waiting.reject(new ProtocolError("inbound_backpressure"));
            throw new ProtocolError("inbound_backpressure");
          }
          void (async () => {
            try {
              if (response.result_kind === "error")
                throw new RequestError(apiError(response.payload));
              const value = await waiting.decode(response);
              if (epoch !== this.epoch) throw new ProtocolError("connection_changed");
              waiting.resolve(value);
            } catch (error) {
              waiting.reject(error);
            } finally {
              clearTimeout(waiting.timer);
              this.incomingBytes -= bytes;
            }
          })();
        } catch (error) {
          this.disconnect(error);
        }
      });
      socket.addEventListener("close", () => {
        if (epoch === this.epoch) this.disconnect(new ProtocolError("connection_lost"));
      });
      socket.addEventListener("error", () => {
        if (epoch === this.epoch) this.disconnect(new ProtocolError("connection_unavailable"));
      });
    });
    return this.connecting;
  }
  private sendControl(frame: unknown): void {
    if (this.socket?.readyState !== WebSocket.OPEN) return;
    const text = canonical(frame);
    if (this.socket.bufferedAmount + new TextEncoder().encode(text).byteLength > MAX_QUEUE_BYTES) {
      this.disconnect(new ProtocolError("control_backpressure"));
      return;
    }
    this.socket.send(text);
  }
  private request(
    kind: Waiting["kind"],
    payload: Waiting["payload"],
    decode: Waiting["decode"],
    control = false,
  ): Promise<JSONValue | Receipt> {
    if (this.state !== "ready") return Promise.reject(new ProtocolError("connection_not_ready"));
    const size =
      jsonBytes(payload, this.registry.discovery.limits.max_domain_bytes).byteLength + 128;
    const all = [...this.queue, ...this.waiting.values()];
    const count = all.length;
    const bytes =
      all.reduce((sum, entry) => sum + entry.size, 0) + (this.socket?.bufferedAmount ?? 0);
    const limit = this.registry.discovery.limits.max_pending;
    if (
      count >= limit ||
      bytes + size > MAX_QUEUE_BYTES ||
      (!control &&
        (all.filter((entry) => !entry.control).length >= Math.max(0, limit - CONTROL_SLOTS) ||
          all.filter((entry) => !entry.control).reduce((sum, entry) => sum + entry.size, 0) + size >
            MAX_QUEUE_BYTES - CONTROL_BYTES))
    )
      return Promise.reject(new ProtocolError("outbound_backpressure"));
    return new Promise((resolve, reject) => {
      const entry: Waiting = { kind, payload, control, size, decode, resolve, reject };
      entry.timer = setTimeout(() => {
        if (entry.sequence !== undefined) this.waiting.delete(entry.sequence);
        const index = this.queue.indexOf(entry);
        if (index >= 0) this.queue.splice(index, 1);
        reject(new ProtocolError("query_original:response_timeout"));
      }, this.timeout);
      this.queue.push(entry);
      this.flush();
    });
  }
  private flush(): void {
    if (this.state !== "ready" || this.socket?.readyState !== WebSocket.OPEN || this.flushTimer)
      return;
    while (this.queue.length) {
      if (this.socket.bufferedAmount > MAX_QUEUE_BYTES - CONTROL_BYTES) {
        this.flushTimer = setTimeout(() => {
          this.flushTimer = undefined;
          this.flush();
        }, 10);
        return;
      }
      const controlIndex = this.queue.findIndex((entry) => entry.control);
      const [waiting] = this.queue.splice(controlIndex >= 0 ? controlIndex : 0, 1);
      if (!waiting) return;
      if (this.sequence >= Number.MAX_SAFE_INTEGER) {
        waiting.reject(new ProtocolError("sequence_exhausted"));
        this.disconnect(new ProtocolError("sequence_exhausted"));
        return;
      }
      const sequence = ++this.sequence;
      const text = canonical({
        type: "request",
        request_seq: sequence,
        kind: waiting.kind,
        payload: waiting.payload,
      });
      if (
        new TextEncoder().encode(text).byteLength > this.registry.discovery.limits.max_frame_bytes
      ) {
        waiting.reject(new ProtocolError("frame_too_large"));
        continue;
      }
      waiting.sequence = sequence;
      this.waiting.set(sequence, waiting);
      try {
        this.socket.send(text);
      } catch (error) {
        this.disconnect(error);
        return;
      }
    }
  }
  private disconnect(error: unknown): void {
    const socket = this.socket;
    this.socket = undefined;
    this.epoch++;
    clearTimeout(this.connectTimer);
    clearTimeout(this.flushTimer);
    clearInterval(this.heartbeat);
    this.connectTimer = undefined;
    this.flushTimer = undefined;
    this.heartbeat = undefined;
    this.connecting = undefined;
    this.connectionReject?.(error);
    this.connectionReject = undefined;
    for (const pending of [...this.queue, ...this.waiting.values()]) {
      clearTimeout(pending.timer);
      pending.reject(error);
    }
    this.queue = [];
    this.waiting.clear();
    if (this.state !== "closed") this.setState("disconnected");
    if (socket && socket.readyState !== WebSocket.CLOSED) socket.close();
  }
  async command(value: Command): Promise<Receipt> {
    const raw = canonical(value);
    const previous = await this.store.get(value.logical_service_id, value.command_id);
    if (previous) {
      if (previous.command_json !== raw) throw new ProtocolError("idempotency_conflict");
      await this.verifyStored(previous);
      await this.connect();
      try {
        return (await this.request(
          "receipt_lookup",
          { logical_service_id: previous.logical_service_id, command_id: previous.command_id },
          (frame) => this.decodeReceipt(frame, previous),
          true,
        )) as Receipt;
      } catch (error) {
        if (!(error instanceof RequestError) || error.error.code !== "not_found") throw error;
        return this.transmit(previous);
      }
    }
    const command = this.registry.command(parseStrict(raw));
    const requestDigest = await digest(command);
    const stored = await this.store.save({
      version: 1,
      identity_scope: this.registry.discovery.identity_scope,
      logical_service_id: command.logical_service_id,
      command_id: command.command_id,
      command_json: canonical(command),
      request_digest: requestDigest,
      core_schema_digest: this.registry.discovery.schema_digest,
      contract: this.registry.method(command.method, "command"),
      status: "pending",
    });
    await this.connect();
    return this.transmit(stored);
  }
  private async decodeReceipt(frame: ResponseFrame, stored: StoredCommand): Promise<Receipt> {
    if (frame.result_kind !== "receipt") throw new ProtocolError("response_kind_mismatch");
    const decoder = await this.decoder(stored);
    const original = decoder.command(parseStrict(stored.command_json));
    const receipt = decoder.receipt(frame.payload, original, stored.request_digest);
    await this.store.receipt(stored.logical_service_id, stored.command_id, receipt);
    return receipt;
  }
  private async transmit(stored: StoredCommand): Promise<Receipt> {
    await this.verifyStored(stored);
    const command = (await this.decoder(stored)).command(parseStrict(stored.command_json));
    return (await this.request(
      "command",
      command,
      (frame) => this.decodeReceipt(frame, stored),
      controlMethods.has(command.method),
    )) as Receipt;
  }
  private decoder(stored: StoredCommand): Promise<ContractRegistry> {
    const key = `${stored.contract.name}:${stored.contract.schema_digest}`;
    let decoder = this.originalDecoders.get(key);
    if (!decoder) {
      if (this.originalDecoders.size >= 32) throw new ProtocolError("original_decoder_capacity");
      decoder = this.registry.original(stored.contract);
      this.originalDecoders.set(key, decoder);
    }
    return decoder;
  }
  private async verifyStored(stored: StoredCommand): Promise<void> {
    const decoder = await this.decoder(stored);
    const command = decoder.command(parseStrict(stored.command_json));
    if (
      stored.version !== 1 ||
      stored.identity_scope !== this.store.identityScope ||
      stored.logical_service_id !== this.registry.discovery.logical_service_id ||
      stored.core_schema_digest !== this.registry.discovery.schema_digest ||
      stored.command_id !== command.command_id ||
      command.method !== stored.contract.name ||
      (await digest(command)) !== stored.request_digest
    )
      throw new ProtocolError("original_decoder_or_identity_unavailable");
  }
  async query(value: Query): Promise<JSONValue> {
    const query = this.registry.query(parseStrict(canonical(value)));
    await this.connect();
    return (await this.request("query", query, async (frame) => {
      if (frame.result_kind !== "query_result") throw new ProtocolError("response_kind_mismatch");
      this.registry.validateOutput(query.method, frame.payload);
      return frame.payload;
    })) as JSONValue;
  }
  makeCommand(
    method: string,
    targetID: string,
    payload: JSONValue,
    expiresAt: string,
    expectedRevision?: number,
  ): Command {
    return {
      protocol: PROTOCOL,
      profile: PROFILE,
      logical_service_id: this.registry.discovery.logical_service_id,
      command_id: newID("command"),
      method,
      target_id: targetID,
      expires_at: expiresAt,
      ...(expectedRevision === undefined ? {} : { expected_revision: expectedRevision }),
      payload,
    };
  }
  makeQuery(method: string, targetID: string, payload: JSONValue): Query {
    return {
      protocol: PROTOCOL,
      profile: PROFILE,
      logical_service_id: this.registry.discovery.logical_service_id,
      query_id: newID("query"),
      method,
      target_id: targetID,
      payload,
    };
  }
  async recover(): Promise<RecoveryResult[]> {
    const pending = await this.store.pending();
    const results: RecoveryResult[] = [];
    for (const stored of pending) {
      try {
        await this.verifyStored(stored);
        await this.connect();
        let receipt: Receipt;
        try {
          receipt = (await this.request(
            "receipt_lookup",
            { logical_service_id: stored.logical_service_id, command_id: stored.command_id },
            (frame) => this.decodeReceipt(frame, stored),
            true,
          )) as Receipt;
        } catch (error) {
          if (!(error instanceof RequestError) || error.error.code !== "not_found") throw error;
          receipt = await this.transmit(stored); // 原权威确定尚无决定；仅重传原身份与准确期限。
        }
        results.push({ command_id: stored.command_id, receipt });
      } catch (error) {
        results.push({
          command_id: stored.command_id,
          error: error instanceof Error ? error.message : "recovery_unavailable",
        });
      }
    }
    return results;
  }
  async pending(): Promise<StoredCommand[]> {
    return this.store.pending();
  }
  async close(): Promise<void> {
    this.setState("closed");
    this.disconnect(new ProtocolError("client_closed"));
    await this.store.close();
  }
}

export function isObject(value: unknown): value is Record<string, JSONValue> {
  return !!value && typeof value === "object" && !Array.isArray(value);
}
export function endpoint(base: URL, path: string): URL {
  if (!path.startsWith("/") || path.startsWith("//") || path.includes("\\"))
    throw new ProtocolError("unsafe_same_origin_path");
  const url = new URL(path, base);
  if (url.origin !== base.origin || url.username || url.password || url.hash)
    throw new ProtocolError("unsafe_same_origin_path");
  return url;
}
export async function readBounded(response: Response, maximum: number): Promise<Uint8Array> {
  if (!response.body || maximum < 0) throw new ProtocolError("body_unavailable");
  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let length = 0;
  try {
    while (true) {
      const result = await reader.read();
      if (result.done) break;
      length += result.value.byteLength;
      if (length > maximum) throw new ProtocolError("response_too_large");
      chunks.push(result.value);
    }
  } catch (error) {
    await reader.cancel();
    throw error;
  } finally {
    reader.releaseLock();
  }
  const result = new Uint8Array(length);
  let offset = 0;
  for (const chunk of chunks) {
    result.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return result;
}
export async function getJSON(
  fetcher: typeof fetch,
  url: URL,
  maximum: number,
): Promise<JSONValue> {
  const response = await fetcher(url, { credentials: "same-origin", redirect: "error" });
  if (!response.ok) {
    const body = parseStrict(await readBounded(response, Math.min(maximum, 16384)));
    if (isObject(body) && body.error) throw new RequestError(apiError(body.error));
    if (isObject(body) && typeof body.code === "string") throw new RequestError(apiError(body));
    throw new ProtocolError(`http_${response.status}`);
  }
  return parseStrict(await readBounded(response, maximum), maximum);
}
export async function fetchContent(
  ref: ContentRef,
  options: {
    baseURL?: string;
    fetcher?: typeof fetch;
    purpose?: string;
    location?: string;
    maxBytes?: number;
    signal?: AbortSignal;
  } = {},
): Promise<Uint8Array> {
  validateRecord("ContentRef", ref);
  const maximum = options.maxBytes ?? 262144;
  if (!Number.isSafeInteger(ref.byte_length) || ref.byte_length < 0 || ref.byte_length > maximum)
    throw new ProtocolError("preview_too_large");
  const base = new URL(options.baseURL ?? globalThis.location.origin);
  const url = endpoint(base, "/api/content");
  const raw = new TextEncoder().encode(canonical(ref));
  const encoded = btoa(Array.from(raw, (entry) => String.fromCharCode(entry)).join(""))
    .replaceAll("+", "-")
    .replaceAll("/", "_")
    .replace(/=+$/, "");
  url.searchParams.set("ref", encoded);
  url.searchParams.set("purpose", options.purpose ?? "read");
  url.searchParams.set("location", options.location ?? "cloud");
  const response = await (options.fetcher ?? fetch)(url, {
    credentials: "same-origin",
    redirect: "error",
    ...(options.signal ? { signal: options.signal } : {}),
  });
  if (!response.ok) throw new ProtocolError(`content_unavailable:${response.status}`);
  const bytes = await readBounded(response, ref.byte_length);
  if (bytes.byteLength !== ref.byte_length || (await sha256(bytes)) !== ref.hash)
    throw new ProtocolError("content_digest_mismatch");
  return bytes;
}
