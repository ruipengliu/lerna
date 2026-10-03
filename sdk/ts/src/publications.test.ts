import { IDBFactory } from "fake-indexeddb";
import { expect, it } from "vitest";
import { IndexedDBPublications } from "./publications";
import type { PublicationIntent } from "./publications";
const owner = "service_00000000000000000000000000000001";
const fixture: PublicationIntent = {
  version: 1,
  identity_scope: "scope-a",
  transfer_id: "transfer_00000000000000000000000000000001",
  content_ref: {
    tenant_id: "tenant_00000000000000000000000000000001",
    owner_id: owner,
    content_id: "content_00000000000000000000000000000001",
    version: 1,
    hash: "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
    media_type: "text/plain",
    byte_length: 3,
  },
  bytes: new TextEncoder().encode("abc"),
  reserve_command: {
    protocol: "harness/1",
    profile: "architecture-2026-10-data1",
    logical_service_id: owner,
    command_id: "command_00000000000000000000000000000001",
    method: "content.upload_reserve",
    target_id: "content_00000000000000000000000000000001",
    expires_at: "2026-10-03T01:00:00Z",
    payload: {},
  },
  put_command: {
    protocol: "harness/1",
    profile: "architecture-2026-10-data1",
    logical_service_id: owner,
    command_id: "command_00000000000000000000000000000002",
    method: "content.put",
    target_id: "content_00000000000000000000000000000001",
    expires_at: "2026-10-03T01:00:00Z",
    payload: {},
  },
  intent_digest: `sha256:${"1".repeat(64)}`,
  state: "pending",
};
it("出版前共同保存准确bytes与固定原命令，重开后不换期限、身份或内容", async () => {
  const factory = new IDBFactory();
  const first = new IndexedDBPublications("scope-a", { factory, name: "publication-restart" });
  await first.save(fixture);
  await first.close();
  const reopened = new IndexedDBPublications("scope-a", { factory, name: "publication-restart" });
  expect(await reopened.pending()).toEqual([fixture]);
  const other = new IndexedDBPublications("scope-b", { factory, name: "publication-restart" });
  expect(await other.pending()).toEqual([]);
  await expect(
    reopened.save({ ...fixture, intent_digest: `sha256:${"2".repeat(64)}` }),
  ).rejects.toThrow(/identity_conflict/);
  await reopened.finish(fixture.transfer_id);
  expect(await reopened.pending()).toEqual([]);
  const unfinished = { ...fixture, transfer_id: "transfer_00000000000000000000000000000002" };
  await reopened.save(unfinished);
  expect(await reopened.clearCompleted()).toBe(1);
  expect(await reopened.pending()).toEqual([unfinished]);
  await reopened.close();
  await other.close();
});
it("浏览器出版并发有界，第九条原意图拒绝落位", async () => {
  const store = new IndexedDBPublications("scope-a", {
    factory: new IDBFactory(),
    name: "publication-limit",
  });
  for (let i = 1; i <= 8; i++)
    await store.save({ ...fixture, transfer_id: `transfer_${i.toString(16).padStart(32, "0")}` });
  await expect(
    store.save({ ...fixture, transfer_id: "transfer_00000000000000000000000000000009" }),
  ).rejects.toThrow(/queue_full/);
  expect((await store.pending()).length).toBe(8);
  await store.close();
});
