import { expect, it } from "vitest";
import { validateFormSchema } from "./development";
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
