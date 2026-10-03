import type { Amount, ComponentRef, ObjectRef } from "./contracts.gen";
import { jsonBytes, ProtocolError } from "./json";
import type { JSONValue } from "./json";
import type { Schema } from "./protocol";
import { validateSchema } from "./schema";

export interface ApplicationEventContract {
  name: string;
  schema: Schema;
  requires_rendered: boolean;
}
export interface DevelopmentConfig {
  tenant_id: string;
  content_policy_ref: ComponentRef;
  task_policy_ref: ComponentRef;
  budget: Amount[];
  goal_schema: Schema;
  retention_seconds: number;
  task_deadline_seconds: number;
  application_binding_ref?: ObjectRef;
  application_events?: ApplicationEventContract[];
}
const record = (name: string) => ({ $ref: `#/$defs/${name}` });
const configSchema = {
  type: "object",
  additionalProperties: false,
  properties: {
    tenant_id: record("Id"),
    content_policy_ref: record("ComponentRef"),
    task_policy_ref: record("ComponentRef"),
    budget: { type: "array", minItems: 1, maxItems: 100, items: record("Amount") },
    goal_schema: { type: "object" },
    retention_seconds: { type: "integer", minimum: 60, maximum: 31536000 },
    task_deadline_seconds: { type: "integer", minimum: 1, maximum: 31536000 },
    application_binding_ref: record("ObjectRef"),
    application_events: {
      type: "array",
      maxItems: 64,
      items: {
        type: "object",
        additionalProperties: false,
        required: ["name", "schema", "requires_rendered"],
        properties: {
          name: { type: "string", pattern: "^[a-z][a-z0-9_.]{0,63}$" },
          schema: { type: "object" },
          requires_rendered: { type: "boolean" },
        },
      },
    },
  },
  required: [
    "tenant_id",
    "content_policy_ref",
    "task_policy_ref",
    "budget",
    "goal_schema",
    "retention_seconds",
    "task_deadline_seconds",
  ],
};
/** 与受信 Renderer 相同的有界自包含表单；不接受脚本、远端或递归引用。 */
export function validateFormSchema(schema: unknown): void {
  jsonBytes(schema);
  let nodes = 0;
  const allowed = new Set([
    "type",
    "oneOf",
    "properties",
    "required",
    "additionalProperties",
    "enum",
    "const",
    "minLength",
    "maxLength",
    "minimum",
    "maximum",
    "items",
    "minItems",
    "maxItems",
    "description",
    "title",
  ]);
  const inspect = (value: unknown, depth: number) => {
    if (++nodes > 256 || depth > 8 || !value || Array.isArray(value) || typeof value !== "object")
      throw new ProtocolError("renderer_schema_budget");
    const spec = value as Record<string, unknown>;
    if (Object.keys(spec).some((key) => !allowed.has(key)))
      throw new ProtocolError("renderer_schema_unsupported");
    if (spec.oneOf !== undefined) {
      if (!Array.isArray(spec.oneOf) || spec.oneOf.length < 2 || spec.oneOf.length > 8)
        throw new ProtocolError("renderer_schema_union_limit");
      for (const branch of spec.oneOf) inspect(branch, depth + 1);
      return;
    }
    const choices = spec.const !== undefined ? [spec.const] : spec.enum;
    let kind = spec.type;
    if (choices !== undefined) {
      if (
        !Array.isArray(choices) ||
        !choices.length ||
        choices.length > 100 ||
        choices.some((item) => !["string", "number", "boolean"].includes(typeof item))
      )
        throw new ProtocolError("renderer_schema_choices");
      kind ??= typeof choices[0];
      if (
        choices.some(
          (item) =>
            typeof item !== (kind === "integer" ? "number" : kind) ||
            (typeof item === "string" && item.length > 65536),
        )
      )
        throw new ProtocolError("renderer_schema_choices");
    }
    if (kind === "object") {
      if (
        spec.additionalProperties !== false ||
        !spec.properties ||
        Array.isArray(spec.properties) ||
        typeof spec.properties !== "object" ||
        Object.keys(spec.properties).length > 64
      )
        throw new ProtocolError("renderer_schema_open_object");
      for (const field of Object.values(spec.properties)) inspect(field, depth + 1);
    } else if (kind === "array") {
      if (
        !Number.isSafeInteger(spec.maxItems) ||
        (spec.maxItems as number) < 1 ||
        (spec.maxItems as number) > 100
      )
        throw new ProtocolError("renderer_schema_unbounded_array");
      inspect(spec.items, depth + 1);
    } else if (kind === "string") {
      if (
        !choices &&
        (!Number.isSafeInteger(spec.maxLength) ||
          (spec.maxLength as number) < 1 ||
          (spec.maxLength as number) > 65536)
      )
        throw new ProtocolError("renderer_schema_unbounded_string");
    } else if (kind === "integer" || kind === "number") {
      if (
        !choices &&
        (typeof spec.minimum !== "number" ||
          typeof spec.maximum !== "number" ||
          spec.minimum > spec.maximum)
      )
        throw new ProtocolError("renderer_schema_unbounded_number");
    } else if (kind !== "boolean") throw new ProtocolError("renderer_schema_unsupported_type");
  };
  inspect(schema, 0);
}
export function developmentConfig(value: JSONValue): DevelopmentConfig {
  validateSchema(configSchema, value);
  const config = value as unknown as DevelopmentConfig;
  validateFormSchema(config.goal_schema);
  if (!!config.application_binding_ref !== !!config.application_events)
    throw new ProtocolError("development_binding_incomplete");
  const names = new Set<string>();
  for (const event of config.application_events ?? []) {
    if (names.has(event.name)) throw new ProtocolError("development_event_duplicate");
    names.add(event.name);
    validateFormSchema(event.schema);
  }
  return config;
}
