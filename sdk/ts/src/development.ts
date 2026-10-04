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
// Only the exact closed inline ContentRef registered by the modern Goal owner.
// This does not permit a caller-selected pattern or a generic unbounded integer.
const boundedContentRefForm = {
  type: "object",
  additionalProperties: false,
  required: ["tenant_id", "owner_id", "content_id", "version", "hash", "media_type", "byte_length"],
  properties: {
    tenant_id: { type: "string", minLength: 1, maxLength: 4096 },
    owner_id: { type: "string", minLength: 1, maxLength: 4096 },
    content_id: { type: "string", minLength: 1, maxLength: 4096 },
    version: { type: "integer", minimum: 1 },
    hash: { type: "string", pattern: "^sha256:[a-f0-9]{64}$" },
    media_type: { type: "string", minLength: 1, maxLength: 4096 },
    byte_length: { type: "integer", minimum: 0, maximum: 16 << 20 },
  },
};
function sameFormShape(actual: unknown, expected: unknown): boolean {
  if (actual === expected) return true;
  if (Array.isArray(expected))
    return (
      Array.isArray(actual) &&
      actual.length === expected.length &&
      expected.every((v, i) => sameFormShape(actual[i], v))
    );
  if (
    !actual ||
    !expected ||
    typeof actual !== "object" ||
    typeof expected !== "object" ||
    Array.isArray(actual)
  )
    return false;
  const a = actual as Record<string, unknown>,
    e = expected as Record<string, unknown>;
  return (
    Object.keys(a).length === Object.keys(e).length &&
    Object.keys(e).every((key) => Object.hasOwn(a, key) && sameFormShape(a[key], e[key]))
  );
}
function boundedFormChoices(spec: Record<string, unknown>): { kind: unknown; choices?: unknown[] } {
  const choices = spec.const !== undefined ? [spec.const] : spec.enum;
  let kind = spec.type;
  if (spec.const !== undefined && spec.enum !== undefined)
    throw new ProtocolError("renderer_schema_choices");
  if (choices === undefined) return { kind };
  if (!Array.isArray(choices) || !choices.length || choices.length > 100)
    throw new ProtocolError("renderer_schema_choices");
  for (const item of choices) {
    if (!["string", "number", "boolean"].includes(typeof item))
      throw new ProtocolError("renderer_schema_choices");
    kind ??= typeof item;
    if (
      typeof item !== (kind === "integer" ? "number" : kind) ||
      (typeof item === "string" && new TextEncoder().encode(item).byteLength > 65536) ||
      (typeof item === "number" &&
        (!Number.isFinite(item) ||
          Math.abs(item) > Number.MAX_SAFE_INTEGER ||
          (kind === "integer" && !Number.isSafeInteger(item))))
    )
      throw new ProtocolError("renderer_schema_choices");
  }
  return { kind, choices };
}
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
    "uniqueItems",
    "description",
    "title",
  ]);
  const inspect = (value: unknown, depth: number) => {
    if (++nodes > 256 || depth > 8 || !value || Array.isArray(value) || typeof value !== "object")
      throw new ProtocolError("renderer_schema_budget");
    const spec = value as Record<string, unknown>;
    if (sameFormShape(spec, boundedContentRefForm)) {
      nodes += 7;
      if (nodes > 256 || depth + 1 > 8) throw new ProtocolError("renderer_schema_budget");
      return;
    }
    if (Object.keys(spec).some((key) => !allowed.has(key)))
      throw new ProtocolError("renderer_schema_unsupported");
    if (spec.oneOf !== undefined) {
      if (
        !Array.isArray(spec.oneOf) ||
        spec.oneOf.length < 2 ||
        spec.oneOf.length > 8 ||
        Object.keys(spec).length !== 1
      )
        throw new ProtocolError("renderer_schema_union_limit");
      for (const branch of spec.oneOf) inspect(branch, depth + 1);
      return;
    }
    const { kind, choices } = boundedFormChoices(spec);
    if (Object.hasOwn(spec, "uniqueItems") && kind !== "array")
      throw new ProtocolError("renderer_schema_uniqueness");
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
      if (Object.hasOwn(spec, "uniqueItems")) {
        if (
          typeof spec.uniqueItems !== "boolean" ||
          !spec.items ||
          Array.isArray(spec.items) ||
          typeof spec.items !== "object" ||
          !boundedFormChoices(spec.items as Record<string, unknown>).choices
        )
          throw new ProtocolError("renderer_schema_uniqueness");
      }
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
          spec.minimum > spec.maximum ||
          !Number.isFinite(spec.minimum) ||
          !Number.isFinite(spec.maximum) ||
          spec.minimum < -Number.MAX_SAFE_INTEGER ||
          spec.maximum > Number.MAX_SAFE_INTEGER)
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
