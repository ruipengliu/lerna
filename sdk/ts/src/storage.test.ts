import { IDBFactory } from "fake-indexeddb";
import { expect, it } from "vitest";
import { IndexedDBCommands } from "./storage";
import type { StoredCommand } from "./storage";

const command: StoredCommand = {
  version: 1,
  identity_scope: "scope-a",
  logical_service_id: "service_00000000000000000000000000000001",
  command_id: "command_00000000000000000000000000000001",
  command_json: '{"original":true}',
  request_digest: `sha256:${"a".repeat(64)}`,
  core_schema_digest: `sha256:${"b".repeat(64)}`,
  contract: {
    name: "task.submit",
    owner: "task",
    kind: "command",
    cas: false,
    allows_accepted: false,
    input_schema: {},
    output_schema: {},
    schema_digest: `sha256:${"c".repeat(64)}`,
    recovery: "query original",
  },
  status: "pending",
};
it("耐久原命令可在关闭重开后恢复，其他主体不能读取或覆盖", async () => {
  const factory = new IDBFactory();
  const store = new IndexedDBCommands("scope-a", { factory, name: "persist" });
  await store.save(command);
  await store.close();
  const reopened = new IndexedDBCommands("scope-a", { factory, name: "persist" });
  expect(await reopened.pending()).toEqual([command]);
  const other = new IndexedDBCommands("scope-b", { factory, name: "persist" });
  expect(await other.pending()).toEqual([]);
  await expect(
    reopened.save({ ...command, request_digest: `sha256:${"f".repeat(64)}` }),
  ).rejects.toThrow(/idempotency_conflict/);
  await expect(other.save(command)).rejects.toThrow(/identity_scope/);
  await reopened.close();
  await other.close();
});

it("未结命令不能被清理，最终原回执不能被异内容或倒退阶段覆盖", async () => {
  const store = new IndexedDBCommands("scope-a", { factory: new IDBFactory(), name: "terminal" });
  await store.save(command);
  await expect(
    store.forgetCompleted(command.logical_service_id, command.command_id),
  ).rejects.toThrow(/unresolved/);
  const receipt = {
    command_id: command.command_id,
    request_digest: command.request_digest,
    stage: "applied" as const,
    decided_at: "2026-10-03T00:00:00Z",
    output: { saved: true },
  };
  await store.receipt(command.logical_service_id, command.command_id, receipt);
  expect(await store.pending()).toEqual([]);
  await expect(
    store.receipt(command.logical_service_id, command.command_id, {
      ...receipt,
      output: { saved: false },
    }),
  ).rejects.toThrow(/receipt_identity_conflict/);
  expect((await store.get(command.logical_service_id, command.command_id))?.receipt).toEqual(
    receipt,
  );
  await store.forgetCompleted(command.logical_service_id, command.command_id);
  expect(await store.get(command.logical_service_id, command.command_id)).toBeUndefined();
  await store.close();
});
