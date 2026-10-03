import { readFile } from "node:fs/promises";
import { once } from "node:events";
import { IDBFactory } from "fake-indexeddb";
import { WebSocketServer } from "ws";
import { expect, it } from "vitest";
import { HarnessClient } from "./client";
import { CORE_SCHEMA_DIGEST } from "./contracts.gen";
import { ContractRegistry } from "./schema";
import { canonical, digest, parseStrict } from "./json";
import { IndexedDBCommands } from "./storage";
import type { CommandStore } from "./storage";
import type { Command, Discovery, MethodContract } from "./protocol";
const owner = "service_00000000000000000000000000000001";
const identity = `sha256:${"1".repeat(64)}`;
const input = {
  type: "object",
  properties: { text: { type: "string" } },
  required: ["text"],
  additionalProperties: false,
};
const output = {
  type: "object",
  properties: { saved: { type: "boolean" } },
  required: ["saved"],
  additionalProperties: false,
};
const command: Command = {
  protocol: "harness/1",
  profile: "architecture-2026-10-data1",
  logical_service_id: owner,
  command_id: "command_00000000000000000000000000000001",
  method: "probe.save",
  target_id: owner,
  expires_at: "2026-10-03T23:59:00Z",
  payload: { text: "准确原输入" },
};
async function registry(options: { query?: boolean; removed?: boolean; control?: string } = {}) {
  const contract: MethodContract = {
    name: options.query ? "probe.read" : "probe.save",
    owner: "probe",
    kind: options.query ? "query" : "command",
    cas: false,
    allows_accepted: false,
    input_schema: input,
    output_schema: output,
    schema_digest: await digest([input, output]),
    recovery: "query original",
  };
  const methods = options.removed ? [] : [contract];
  if (options.control) methods.push({ ...contract, name: options.control, kind: "command" });
  const discovery: Discovery = {
    protocol: "harness/1",
    profile: "architecture-2026-10-data1",
    logical_service_id: owner,
    identity_scope: identity,
    identity_revision: 1,
    schema_digest: CORE_SCHEMA_DIGEST,
    core_schema_path: "/api/schema/core",
    methods,
    methods_digest: await digest(methods),
    limits: { max_domain_bytes: 262144, max_frame_bytes: 1048576, max_pending: 32 },
  };
  const core = new Uint8Array(
    await readFile(
      new URL("../../../docs/architecture/protocol/core.schema.json", import.meta.url),
    ),
  );
  return ContractRegistry.create(discovery, core);
}
const ready = (contract: ContractRegistry) => ({
  type: "ready",
  connection_id: "connection_00000000000000000000000000000001",
  logical_service_id: owner,
  profile: command.profile,
  transport_profile: "harness-wss/1",
  methods_digest: contract.methodsDigest,
  identity_scope: contract.discovery.identity_scope,
  identity_revision: contract.discovery.identity_revision,
  limits: contract.discovery.limits,
});
it("满普通队列仍发送 schedule.resume，原输入和存储责任保持准确", async () => {
  const contract = await registry({ query: true, control: "schedule.resume" });
  const server = new WebSocketServer({ host: "127.0.0.1", port: 0 });
  await once(server, "listening");
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("no address");
  let held = 0;
  let releaseHeld: () => void = () => {};
  const allEntered = new Promise<void>((resolve) => {
    releaseHeld = resolve;
  });
  let sentControl: unknown;
  server.on("connection", (socket) => {
    socket.send(JSON.stringify(ready(contract)));
    socket.on("message", async (bytes) => {
      const frame = parseStrict(bytes.toString());
      if (
        !frame ||
        typeof frame !== "object" ||
        Array.isArray(frame) ||
        typeof frame.kind !== "string" ||
        typeof frame.request_seq !== "number"
      )
        throw new Error("invalid actual request frame");
      if (frame.kind === "query") {
        held++;
        if (held === 28) releaseHeld();
        return;
      }
      const original = contract.command(frame.payload);
      sentControl = original;
      socket.send(
        JSON.stringify({
          type: "response",
          request_seq: frame.request_seq,
          result_kind: "receipt",
          payload: {
            command_id: original.command_id,
            request_digest: await digest(original),
            stage: "applied",
            decided_at: "2026-10-03T00:00:00Z",
            output: { saved: true },
          },
        }),
      );
    });
  });
  const client = new HarnessClient({
    registry: contract,
    store: new IndexedDBCommands(identity, {
      factory: new IDBFactory(),
      name: "schedule-control-capacity",
    }),
    url: `ws://127.0.0.1:${address.port}`,
    requestTimeoutMs: 5000,
  });
  const pending: Promise<unknown>[] = [];
  try {
    await client.connect();
    for (let i = 0; i < 28; i++)
      pending.push(
        client
          .query(client.makeQuery("probe.read", owner, { text: "actual held query" }))
          .catch((error: unknown) => error),
      );
    await allEntered;
    await expect(
      client.query(client.makeQuery("probe.read", owner, { text: "ordinary overflow" })),
    ).rejects.toThrow("outbound_backpressure");
    const original = { ...command, method: "schedule.resume" };
    const receipt = await client.command(original);
    expect(receipt.stage).toBe("applied");
    expect(canonical(sentControl)).toBe(canonical(original));
    expect(held).toBe(28);
    expect(await client.pending()).toEqual([]);
  } finally {
    await client.close();
    await Promise.all(pending);
    await new Promise<void>((resolve, reject) =>
      server.close((error) => (error ? reject(error) : resolve())),
    );
  }
});
it("每次 ready 必须声明原认证身份；旧无身份协议明确拒绝", async () => {
  const contract = await registry();
  expect(() => contract.ready(ready(contract))).not.toThrow();
  const { identity_scope: _scope, identity_revision: _revision, ...legacy } = ready(contract);
  expect(() => contract.ready(legacy)).toThrow(/unsupported_ready_identity_binding/);
});
it("真实心跳 nonce 按 Go 的 UTF-8 字节上限核验，控制帧不占业务序号", async () => {
  const contract = await registry({ query: true });
  const server = new WebSocketServer({ host: "127.0.0.1", port: 0 });
  await once(server, "listening");
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("no address");
  server.on("connection", (socket) => socket.send(JSON.stringify(ready(contract))));
  const connected = once(server, "connection");
  const client = new HarnessClient({
    registry: contract,
    store: new IndexedDBCommands(identity, { factory: new IDBFactory(), name: "nonce-bytes" }),
    url: `ws://127.0.0.1:${address.port}`,
  });
  try {
    await client.connect();
    const [socket] = await connected;
    const reply = once(socket, "message");
    const nonce = "😀".repeat(64);
    socket.send(JSON.stringify({ type: "ping", nonce }));
    const [raw] = await reply;
    expect(parseStrict(String(raw))).toEqual({ type: "pong", nonce });
    expect(client.connectionState).toBe("ready");
    const sent = once(socket, "message");
    const query = client.query(client.makeQuery("probe.read", owner, { text: "after heartbeat" }));
    const [request] = await sent;
    expect(parseStrict(String(request))).toMatchObject({
      type: "request",
      kind: "query",
      request_seq: 1,
    });
    socket.send(
      JSON.stringify({
        type: "response",
        request_seq: 1,
        result_kind: "query_result",
        payload: { saved: true },
      }),
    );
    expect(await query).toEqual({ saved: true });
    socket.send(JSON.stringify({ type: "ping", nonce: "😀".repeat(65) }));
    await expect.poll(() => client.connectionState, { timeout: 1000 }).toBe("disconnected");
  } finally {
    await client.close();
    server.close();
  }
});
it("新连接身份 scope 或凭据修订改变时不能发送旧账本请求，原责任仍留原scope", async () => {
  const contract = await registry();
  for (const replacement of [
    { identity_scope: `sha256:${"2".repeat(64)}`, identity_revision: 1 },
    { identity_scope: identity, identity_revision: 2 },
  ]) {
    const server = new WebSocketServer({ host: "127.0.0.1", port: 0 });
    await once(server, "listening");
    const address = server.address();
    if (!address || typeof address === "string") throw new Error("no address");
    let messages = 0;
    server.on("connection", (socket) => {
      socket.send(JSON.stringify({ ...ready(contract), ...replacement }));
      socket.on("message", () => messages++);
    });
    const store = new IndexedDBCommands(identity, {
      factory: new IDBFactory(),
      name: "changed-connection-identity",
    });
    const client = new HarnessClient({
      registry: contract,
      store,
      url: `ws://127.0.0.1:${address.port}`,
    });
    await expect(client.command(command)).rejects.toThrow(/ready_identity_mismatch/);
    expect(messages).toBe(0);
    const pending = await client.pending();
    expect(pending).toHaveLength(1);
    expect(pending[0]?.identity_scope).toBe(identity);
    expect(pending[0]?.command_json).toBe(canonical(command));
    expect(pending[0]?.request_digest).toBe(await digest(command));
    await client.close();
    server.close();
  }
});
it("耐久存储拒绝时不连接与首次发送，返回真实存储失败", async () => {
  const contract = await registry();
  const server = new WebSocketServer({ host: "127.0.0.1", port: 0 });
  await once(server, "listening");
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("no address");
  let connections = 0;
  server.on("connection", () => connections++);
  const store: CommandStore = {
    identityScope: identity,
    save: async () => {
      throw new Error("disk full");
    },
    pending: async () => [],
    receipt: async () => {},
    get: async () => undefined,
    close: async () => {},
  };
  const client = new HarnessClient({
    registry: contract,
    store,
    url: `ws://127.0.0.1:${address.port}`,
  });
  await expect(client.command(command)).rejects.toThrow("disk full");
  expect(connections).toBe(0);
  await client.close();
  server.close();
});

it("业务已决定但丢答复后重开 SDK 查原命令，不产生新的命令或刷新期限", async () => {
  let contract = await registry();
  const factory = new IDBFactory();
  const server = new WebSocketServer({ host: "127.0.0.1", port: 0 });
  await once(server, "listening");
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("no address");
  const seen: string[] = [];
  const originalDigest = await digest(command);
  const receipt = {
    command_id: command.command_id,
    request_digest: originalDigest,
    stage: "applied",
    decided_at: "2026-10-03T00:00:00Z",
    output: { saved: true },
  };
  server.on("connection", (socket) => {
    socket.send(
      JSON.stringify({
        type: "ready",
        connection_id: "connection_00000000000000000000000000000001",
        logical_service_id: owner,
        profile: command.profile,
        transport_profile: "harness-wss/1",
        methods_digest: contract.methodsDigest,
        identity_scope: contract.discovery.identity_scope,
        identity_revision: contract.discovery.identity_revision,
        limits: contract.discovery.limits,
      }),
    );
    socket.on("message", (raw) => {
      const frame = parseStrict(raw.toString()) as {
        kind: string;
        request_seq: number;
        payload: unknown;
      };
      seen.push(frame.kind);
      if (frame.kind === "command") {
        expect(canonical(frame.payload)).toBe(canonical(command));
        socket.close();
      } else
        socket.send(
          JSON.stringify({
            type: "response",
            request_seq: frame.request_seq,
            result_kind: "receipt",
            payload: receipt,
          }),
        );
    });
  });
  const client = new HarnessClient({
    registry: contract,
    store: new IndexedDBCommands(identity, { factory, name: "lost-reply" }),
    url: `ws://127.0.0.1:${address.port}`,
    requestTimeoutMs: 2000,
  });
  await expect(client.command(command)).rejects.toThrow();
  await client.close();
  contract = await registry({ removed: true });
  const reopened = new HarnessClient({
    registry: contract,
    store: new IndexedDBCommands(identity, { factory, name: "lost-reply" }),
    url: `ws://127.0.0.1:${address.port}`,
  });
  expect(await reopened.recover()).toEqual([{ command_id: command.command_id, receipt }]);
  expect(seen).toEqual(["command", "receipt_lookup"]);
  await reopened.close();
  server.close();
});

it("原 owner 确定尚未接纳时只重传原字节，过期拒绝沿原命令落账", async () => {
  const contract = await registry();
  const store = new IndexedDBCommands(identity, { factory: new IDBFactory(), name: "unsent" });
  const originalDigest = await digest(command);
  await store.save({
    version: 1,
    identity_scope: identity,
    logical_service_id: owner,
    command_id: command.command_id,
    command_json: canonical(command),
    request_digest: originalDigest,
    core_schema_digest: CORE_SCHEMA_DIGEST,
    contract: contract.method(command.method),
    status: "pending",
  });
  const server = new WebSocketServer({ host: "127.0.0.1", port: 0 });
  await once(server, "listening");
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("no address");
  const seen: string[] = [];
  const receipt = {
    command_id: command.command_id,
    request_digest: originalDigest,
    stage: "rejected",
    decided_at: "2026-10-03T00:00:00Z",
    error: {
      code: "expired",
      scope: "command",
      reason: "first_acceptance_deadline",
      retry: "none",
    },
  };
  server.on("connection", (socket) => {
    socket.send(
      JSON.stringify({
        type: "ready",
        connection_id: "connection_00000000000000000000000000000001",
        logical_service_id: owner,
        profile: command.profile,
        transport_profile: "harness-wss/1",
        methods_digest: contract.methodsDigest,
        identity_scope: contract.discovery.identity_scope,
        identity_revision: contract.discovery.identity_revision,
        limits: contract.discovery.limits,
      }),
    );
    socket.on("message", (raw) => {
      const frame = parseStrict(raw.toString()) as {
        kind: string;
        request_seq: number;
        payload: unknown;
      };
      seen.push(frame.kind);
      if (frame.kind === "command") expect(canonical(frame.payload)).toBe(canonical(command));
      socket.send(
        JSON.stringify({
          type: "response",
          request_seq: frame.request_seq,
          result_kind: frame.kind === "command" ? "receipt" : "error",
          payload:
            frame.kind === "command"
              ? receipt
              : {
                  code: "not_found",
                  scope: "command",
                  reason: "not_accepted",
                  retry: "same_request",
                },
        }),
      );
    });
  });
  const client = new HarnessClient({
    registry: contract,
    store,
    url: `ws://127.0.0.1:${address.port}`,
  });
  expect(await client.recover()).toEqual([{ command_id: command.command_id, receipt }]);
  expect(seen).toEqual(["receipt_lookup", "command"]);
  expect(await client.pending()).toEqual([]);
  await client.close();
  server.close();
});

it("真实 WebSocket 上普通查询最多占 28 个槽，背压与等待都有限", async () => {
  const contract = await registry({ query: true });
  const server = new WebSocketServer({ host: "127.0.0.1", port: 0 });
  await once(server, "listening");
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("no address");
  let transmitted = 0;
  server.on("connection", (socket) => {
    socket.send(
      JSON.stringify({
        type: "ready",
        connection_id: "connection_00000000000000000000000000000001",
        logical_service_id: owner,
        profile: command.profile,
        transport_profile: "harness-wss/1",
        methods_digest: contract.methodsDigest,
        identity_scope: contract.discovery.identity_scope,
        identity_revision: contract.discovery.identity_revision,
        limits: contract.discovery.limits,
      }),
    );
    socket.on("message", () => transmitted++);
  });
  const client = new HarnessClient({
    registry: contract,
    store: new IndexedDBCommands(identity, { factory: new IDBFactory(), name: "pressure" }),
    url: `ws://127.0.0.1:${address.port}`,
    requestTimeoutMs: 150,
  });
  await client.connect();
  const results = await Promise.all(
    Array.from({ length: 40 }, () =>
      client.query(client.makeQuery("probe.read", owner, { text: "bounded" })).then(
        () => "unexpected",
        (failure: unknown) => (failure instanceof Error ? failure.message : "unknown"),
      ),
    ),
  );
  expect(results.filter((value) => value === "outbound_backpressure")).toHaveLength(12);
  expect(results.filter((value) => value === "query_original:response_timeout")).toHaveLength(28);
  expect(transmitted).toBe(28);
  await client.close();
  server.close();
});

it("收到回执后本地耐久提交停滞，等待仍有界且原命令保留未结", async () => {
  const contract = await registry();
  const durable = new IndexedDBCommands(identity, {
    factory: new IDBFactory(),
    name: "receipt-stall",
  });
  const store: CommandStore = {
    identityScope: identity,
    save: (value) => durable.save(value),
    get: (service, id) => durable.get(service, id),
    pending: () => durable.pending(),
    close: () => durable.close(),
    receipt: async () => new Promise(() => {}),
  };
  const server = new WebSocketServer({ host: "127.0.0.1", port: 0 });
  await once(server, "listening");
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("no address");
  const requestDigest = await digest(command);
  server.on("connection", (socket) => {
    socket.send(
      JSON.stringify({
        type: "ready",
        connection_id: "connection_00000000000000000000000000000001",
        logical_service_id: owner,
        profile: command.profile,
        transport_profile: "harness-wss/1",
        methods_digest: contract.methodsDigest,
        identity_scope: contract.discovery.identity_scope,
        identity_revision: contract.discovery.identity_revision,
        limits: contract.discovery.limits,
      }),
    );
    socket.on("message", (raw) => {
      const frame = parseStrict(raw.toString()) as { request_seq: number };
      socket.send(
        JSON.stringify({
          type: "response",
          request_seq: frame.request_seq,
          result_kind: "receipt",
          payload: {
            command_id: command.command_id,
            request_digest: requestDigest,
            stage: "applied",
            decided_at: "2026-10-03T00:00:00Z",
            output: { saved: true },
          },
        }),
      );
    });
  });
  const client = new HarnessClient({
    registry: contract,
    store,
    url: `ws://127.0.0.1:${address.port}`,
    requestTimeoutMs: 100,
  });
  try {
    await expect(client.command(command)).rejects.toThrow("response_timeout");
    expect((await client.pending()).map((value) => value.command_id)).toEqual([command.command_id]);
  } finally {
    await client.close();
    server.close();
  }
}, 1000);
