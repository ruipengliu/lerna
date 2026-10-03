import { expect, it } from "vitest";
import { readFile } from "node:fs/promises";
import { CORE_SCHEMA_DIGEST } from "./contracts.gen";
import { digest, parseStrict } from "./json";
import { ContractRegistry } from "./schema";
import type { Discovery, MethodContract } from "./protocol";
const owner = "service_00000000000000000000000000000001";
const contract: MethodContract = {
  name: "content.read",
  owner: "memory",
  kind: "query",
  cas: false,
  allows_accepted: false,
  input_schema: {
    type: "object",
    properties: { ref: { $ref: "#/$defs/ContentRef" } },
    required: ["ref"],
    additionalProperties: false,
  },
  output_schema: { $ref: "#/$defs/ContentRef" },
  schema_digest: "",
  recovery: "query original",
};

it("只开放发现中摘要验证过的闭合方法，拒绝未知字段和不准确引用", async () => {
  contract.schema_digest = await digest([contract.input_schema, contract.output_schema]);
  const discovery: Discovery = {
    protocol: "harness/1",
    profile: "architecture-2026-10-data1",
    logical_service_id: owner,
    identity_scope: `sha256:${"1".repeat(64)}`,
    identity_revision: 1,
    schema_digest: CORE_SCHEMA_DIGEST,
    core_schema_path: "/api/schema/core",
    methods: [contract],
    limits: { max_domain_bytes: 262144, max_frame_bytes: 1048576, max_pending: 32 },
  };
  const raw = new Uint8Array(
    await readFile(
      new URL("../../../docs/architecture/protocol/core.schema.json", import.meta.url),
    ),
  );
  const registry = await ContractRegistry.create(
    parseStrict(JSON.stringify(discovery), 1048576),
    raw,
  );
  const ref = {
    tenant_id: "tenant_00000000000000000000000000000001",
    owner_id: owner,
    content_id: "content_00000000000000000000000000000001",
    version: 1,
    hash: `sha256:${"a".repeat(64)}`,
    media_type: "text/plain",
    byte_length: 3,
  };
  expect(registry.validateInput("content.read", { ref }, "query")).toEqual({ ref });
  expect(() => registry.validateInput("content.read", { ref, extra: true }, "query")).toThrow();
  expect(() => registry.validateOutput("content.read", { ...ref, version: 0 })).toThrow();
  expect(() => registry.validateInput("task.submit", {}, "command")).toThrow(/unsupported/);
  await expect(ContractRegistry.create(discovery, new TextEncoder().encode("{}"))).rejects.toThrow(
    /schema_digest/,
  );
  await expect(
    ContractRegistry.create(
      { ...discovery, methods: [{ ...contract, schema_digest: `sha256:${"0".repeat(64)}` }] },
      raw,
    ),
  ).rejects.toThrow(/method_digest/);
});
