import { expect, it } from "vitest";
import { developmentConfig, validateFormSchema } from "./development";
it("受信表单允许有限闭合 Goal 变体，拒绝脚本/开放字段/远程引用和无界数组", () => {
  const branch = (kind: string) => ({
    type: "object",
    additionalProperties: false,
    required: ["kind", "body"],
    properties: { kind: { const: kind }, body: { type: "string", minLength: 1, maxLength: 65536 } },
  });
  expect(() => validateFormSchema({ oneOf: [branch("answer"), branch("report")] })).not.toThrow();
  expect(() => validateFormSchema({ ...branch("answer"), script: "alert(1)" })).toThrow(
    /unsupported/,
  );
  expect(() => validateFormSchema({ type: "object", properties: {} })).toThrow(/open_object/);
  expect(() => validateFormSchema({ $ref: "https://other/schema" })).toThrow(/unsupported/);
  expect(() => validateFormSchema({ type: "array", items: { type: "boolean" } })).toThrow(
    /unbounded_array/,
  );
});
it("准确开发配置是闭合契约；事件不得携带任意 method/URL 或未绑定配置", () => {
  const schema = {
    type: "object",
    additionalProperties: false,
    required: ["reason"],
    properties: { reason: { type: "string", minLength: 1, maxLength: 200 } },
  };
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
    goal_schema: schema,
    retention_seconds: 86400,
    task_deadline_seconds: 1800,
    application_binding_ref: {
      tenant_id: "tenant_00000000000000000000000000000001",
      owner_id: "owner_00000000000000000000000000000001",
      object_id: "binding_00000000000000000000000000000001",
      revision: 1,
    },
    application_events: [{ name: "archive_demo_session", schema, requires_rendered: true }],
  };
  expect(developmentConfig(config).budget).toEqual([{ unit: "USD", value: "20" }]);
  expect(() => developmentConfig({ ...config, method: "memory.delete" })).toThrow(
    /schema_violation/,
  );
  expect(() =>
    developmentConfig({
      ...config,
      application_events: [{ ...config.application_events[0], url: "https://other/execute" }],
    }),
  ).toThrow(/schema_violation/);
  const { application_events: _events, ...unbound } = config;
  expect(() => developmentConfig(unbound)).toThrow(/binding_incomplete/);
});
