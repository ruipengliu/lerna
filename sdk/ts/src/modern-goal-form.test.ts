import { readFileSync } from "node:fs";
import { expect, it } from "vitest";
import { developmentConfig, validateFormSchema } from "./development";
import { validateSchema } from "./schema";

// The modern default producer in brain.GoalSchema uses this closed inline reference.
const string = { type: "string", minLength: 1, maxLength: 4096 };
const reference = {
  type: "object",
  additionalProperties: false,
  required: ["tenant_id", "owner_id", "content_id", "version", "hash", "media_type", "byte_length"],
  properties: {
    tenant_id: string,
    owner_id: string,
    content_id: string,
    version: { type: "integer", minimum: 1 },
    hash: { type: "string", pattern: "^sha256:[a-f0-9]{64}$" },
    media_type: string,
    byte_length: { type: "integer", minimum: 0, maximum: 16 << 20 },
  },
};
const modernGoalSchema = JSON.parse(
  readFileSync(new URL("./modern-goal-schema.fixture.json", import.meta.url), "utf8"),
);

it("development bootstrap accepts its modern default Goal schema without replacing original constraints", () => {
  const config = {
    tenant_id: "tenant_00000000000000000000000000000001",
    content_policy_ref: {
      component_id: "policy_00000000000000000000000000000001",
      version: "1",
      digest: `sha256:${"0".repeat(64)}`,
    },
    task_policy_ref: {
      component_id: "policy_00000000000000000000000000000002",
      version: "1",
      digest: `sha256:${"1".repeat(64)}`,
    },
    budget: [{ unit: "USD", value: "20" }],
    goal_schema: modernGoalSchema,
    retention_seconds: 86400,
    task_deadline_seconds: 1800,
  };
  expect(developmentConfig(config).goal_schema).toEqual(modernGoalSchema);
});

it("the exact modern constraints remain mandatory for submitted values", () => {
  const ref = {
    tenant_id: "tenant_00000000000000000000000000000001",
    owner_id: "owner_00000000000000000000000000000001",
    content_id: "content_00000000000000000000000000000001",
    version: 1,
    hash: `sha256:${"a".repeat(64)}`,
    media_type: "application/json",
    byte_length: 10,
  };
  const report = {
    kind: "report",
    title: "原报告",
    body: "原正文",
    save_path: "report.md",
    preference: {
      query_ref: ref,
      scope_ref: ref,
      allowed_formats: ["plain", "bullet"],
      default_format: "plain",
    },
  };
  expect(() => validateSchema(modernGoalSchema, report)).not.toThrow();
  for (const preference of [
    { ...report.preference, allowed_formats: ["plain", "plain"] },
    { ...report.preference, allowed_formats: [] },
    { ...report.preference, allowed_formats: ["html"] },
    { ...report.preference, default_format: "html" },
    { ...report.preference, query_ref: { ...ref, hash: "sha256:bad" } },
    { ...report.preference, query_ref: { ...ref, version: 0 } },
    { ...report.preference, query_ref: { ...ref, version: Number.MAX_SAFE_INTEGER + 1 } },
    { ...report.preference, query_ref: { ...ref, byte_length: (16 << 20) + 1 } },
    { ...report.preference, query_ref: { ...ref, unknown: true } },
  ])
    expect(() => validateSchema(modernGoalSchema, { ...report, preference })).toThrow();
});
it("renderer admission rejects unsafe schema extensions and incomplete ContentRef impostors", () => {
  const mutations = [
    {
      ...reference,
      properties: { ...reference.properties, hash: { type: "string", pattern: "^.*$" } },
    },
    { ...reference, properties: { ...reference.properties, url: string } },
    {
      ...reference,
      properties: {
        ...reference.properties,
        byte_length: { type: "integer", minimum: 0, maximum: 32 << 20 },
      },
    },
  ];
  for (const schema of [
    ...mutations,
    { type: "string", pattern: "^sha256:[a-f0-9]{64}$" },
    { type: "string", maxLength: 100, pattern: "(a+)+$" },
    { type: "integer", minimum: 1 },
    { type: "string", maxLength: 10, script: "run()" },
    { $ref: "https://other/schema" },
    { type: "array", uniqueItems: true, items: { enum: ["plain", "bullet"] } },
    { type: "array", maxItems: 2, uniqueItems: true, items: string },
    {
      type: "array",
      maxItems: 2,
      uniqueItems: true,
      items: { type: "object", additionalProperties: false, properties: {} },
    },
    { enum: ["plain", true] },
    { enum: [{}] },
    { type: "integer", enum: [1.5] },
    { type: "number", enum: [Number.MAX_SAFE_INTEGER + 1] },
    { oneOf: [{ const: true }, { const: false }], script: "run()" },
    { oneOf: [{ const: true }, { const: false }], uniqueItems: "x" },
    {
      oneOf: [{ type: "object", additionalProperties: false, properties: {} }, { const: false }],
      uniqueItems: true,
    },
    { type: "string", enum: [] },
    {
      type: "object",
      additionalProperties: false,
      properties: Object.fromEntries(Array.from({ length: 40 }, (_, i) => [`ref_${i}`, reference])),
    },
    Array.from({ length: 8 }).reduce(
      (child) => ({ type: "object", additionalProperties: false, properties: { ref: child } }),
      reference as unknown,
    ),
  ])
    expect(() => validateFormSchema(schema)).toThrow();
});
