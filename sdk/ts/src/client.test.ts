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
async function registry() {
  const contract: MethodContract = {
    name: "probe.save",
    owner: "probe",
    kind: "command",
    cas: false,
    allows_accepted: false,
    input_schema: input,
    output_schema: output,
    schema_digest: await digest([input, output]),
    recovery: "query original",
  };
  const discovery: Discovery = {
    protocol: "harness/1",
    profile: "architecture-2026-10-data1",
    logical_service_id: owner,
    identity_scope: identity,
    identity_revision: 1,
    schema_digest: CORE_SCHEMA_DIGEST,
    core_schema_path: "/api/schema/core",
    methods: [contract],
    limits: { max_domain_bytes: 262144, max_frame_bytes: 1048576, max_pending: 32 },
  };
  const core = new Uint8Array(
    await readFile(
      new URL("../../../docs/architecture/protocol/core.schema.json", import.meta.url),
    ),
  );
  return ContractRegistry.create(discovery, core);
}
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
  const contract = await registry();
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
