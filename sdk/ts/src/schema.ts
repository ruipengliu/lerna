import { Validator } from "@cfworker/json-schema";
import type { Schema as ValidationSchema } from "@cfworker/json-schema";
import { CORE_SCHEMA, CORE_SCHEMA_DIGEST } from "./contracts.gen";
import { canonical, digest, jsonBytes, parseStrict, ProtocolError, sha256 } from "./json";
import type {
  APIError,
  Command,
  Discovery,
  Limits,
  MethodContract,
  Query,
  Receipt,
  Ready,
  ResponseFrame,
  Schema,
} from "./protocol";
import { PROFILE, PROTOCOL, TRANSPORT_PROFILE } from "./protocol";
const id = { type: "string", pattern: "^[a-z][a-z0-9_]*_[0-9a-f]{32}$" };
const hash = { type: "string", pattern: "^sha256:[0-9a-f]{64}$" };
const count = { type: "integer", minimum: 1, maximum: Number.MAX_SAFE_INTEGER };
const text = { type: "string", minLength: 1, maxLength: 4096 };
const utc = { type: "string", format: "date-time", pattern: "Z$" };
function object(
  properties: Record<string, unknown>,
  required = Object.keys(properties),
): Record<string, unknown> {
  return { type: "object", properties, required, additionalProperties: false };
}
const limitsSchema = object({
  max_domain_bytes: { ...count, maximum: 262144 },
  max_frame_bytes: { ...count, maximum: 1048576 },
  max_pending: { ...count, maximum: 32 },
});
const methodSchema = object({
  name: text,
  owner: text,
  kind: { enum: ["command", "query"] },
  cas: { type: "boolean" },
  allows_accepted: { type: "boolean" },
  input_schema: { type: "object" },
  output_schema: { type: "object" },
  schema_digest: hash,
  recovery: text,
});
const discoverySchema = object(
  {
    protocol: { const: PROTOCOL },
    profile: { const: PROFILE },
    logical_service_id: id,
    identity_scope: hash,
    identity_revision: count,
    schema_digest: hash,
    methods: { type: "array", items: methodSchema, maxItems: 1000 },
    methods_digest: hash,
    limits: limitsSchema,
    core_schema_path: text,
    core_schema: {},
  },
  [
    "protocol",
    "profile",
    "logical_service_id",
    "identity_scope",
    "identity_revision",
    "schema_digest",
    "methods",
    "methods_digest",
    "limits",
    "core_schema_path",
  ],
);
const errorSchema = object(
  {
    code: {
      enum: [
        "invalid_request",
        "forbidden",
        "expired",
        "revision_conflict",
        "idempotency_conflict",
        "not_found",
        "gone",
        "unsupported",
        "invalid_state",
        "overloaded",
        "dependency_unavailable",
        "cursor_expired",
        "snapshot_required",
        "effect_unknown",
        "accounting_unknown",
      ],
    },
    scope: text,
    reason: text,
    retry: { enum: ["none", "same_request", "query_original", "after_change", "backoff"] },
    detail: { type: "string", maxLength: 4096 },
  },
  ["code", "scope", "reason", "retry"],
);
const receiptSchema = object(
  {
    command_id: id,
    request_digest: hash,
    stage: { enum: ["accepted", "applied", "rejected"] },
    accepted_at: utc,
    decided_at: utc,
    output: {},
    error: errorSchema,
  },
  ["command_id", "request_digest", "stage"],
);
const commandSchema = object(
  {
    protocol: { const: PROTOCOL },
    profile: { const: PROFILE },
    logical_service_id: id,
    command_id: id,
    method: text,
    target_id: id,
    expires_at: utc,
    expected_revision: count,
    payload: {},
  },
  [
    "protocol",
    "profile",
    "logical_service_id",
    "command_id",
    "method",
    "target_id",
    "expires_at",
    "payload",
  ],
);
const querySchema = object({
  protocol: { const: PROTOCOL },
  profile: { const: PROFILE },
  logical_service_id: id,
  query_id: id,
  method: text,
  target_id: id,
  payload: {},
});
const readySchema = object({
  type: { const: "ready" },
  connection_id: id,
  logical_service_id: id,
  profile: { const: PROFILE },
  transport_profile: { const: TRANSPORT_PROFILE },
  methods_digest: hash,
  identity_scope: hash,
  identity_revision: count,
  limits: limitsSchema,
});
const responseSchema = object({
  type: { const: "response" },
  request_seq: count,
  result_kind: { enum: ["receipt", "query_result", "error"] },
  payload: {},
});
type ValidateFunction = (value: unknown) => boolean;
function compiler(): { compile: (schema: unknown) => ValidateFunction } {
  return {
    compile: (schema) => {
      const validator = new Validator(structuredClone(schema) as ValidationSchema, "2020-12", true);
      return (value) => validator.validate(value).valid;
    },
  };
}
const envelope = compiler();
const validateError = envelope.compile(errorSchema);
const validateReceipt = envelope.compile(receiptSchema);
const validateCommand = envelope.compile(commandSchema);
const validateQuery = envelope.compile(querySchema);
const validateDiscovery = envelope.compile(discoverySchema);
const validateReady = envelope.compile(readySchema);
const validateResponse = envelope.compile(responseSchema);
function checked<T>(validator: ValidateFunction, value: unknown, reason: string): T {
  jsonBytes(value, 1048576);
  if (!validator(value)) throw new ProtocolError(reason);
  return value as T;
}

function localReferences(schema: unknown): void {
  if (Array.isArray(schema)) {
    for (const entry of schema) localReferences(entry);
    return;
  }
  if (!schema || typeof schema !== "object") return;
  for (const [key, value] of Object.entries(schema)) {
    if (
      (key === "$ref" || key === "$dynamicRef") &&
      (typeof value !== "string" || !value.startsWith("#/"))
    )
      throw new ProtocolError("remote_schema_forbidden");
    localReferences(value);
  }
}

/** 方法只来自当前认证发现；编译时不下载远端 Schema，也不改变请求默认值。 */
export class ContractRegistry {
  readonly discovery: Discovery;
  readonly methodsDigest: string;
  private constructor(
    discovery: Discovery,
    methodsDigest: string,
    private readonly input: Map<string, ValidateFunction>,
    private readonly output: Map<string, ValidateFunction>,
    private readonly coreBytes: Uint8Array,
  ) {
    this.discovery = discovery;
    this.methodsDigest = methodsDigest;
  }
  static async create(untrusted: unknown, coreBytes: Uint8Array): Promise<ContractRegistry> {
    const discovery = checked<Discovery>(validateDiscovery, untrusted, "invalid_discovery");
    if (
      (await sha256(coreBytes)) !== discovery.schema_digest ||
      discovery.schema_digest !== CORE_SCHEMA_DIGEST
    )
      throw new ProtocolError("schema_digest_mismatch");
    const core = parseStrict(coreBytes, 1048576);
    if (
      !core ||
      Array.isArray(core) ||
      typeof core !== "object" ||
      !core.$defs ||
      Array.isArray(core.$defs) ||
      typeof core.$defs !== "object"
    )
      throw new ProtocolError("invalid_core_schema");
    const validator = compiler();
    const input = new Map<string, ValidateFunction>();
    const output = new Map<string, ValidateFunction>();
    for (const method of discovery.methods) {
      if (input.has(method.name)) throw new ProtocolError("duplicate_method");
      if ((await digest([method.input_schema, method.output_schema])) !== method.schema_digest)
        throw new ProtocolError("method_digest_mismatch");
      for (const schema of [method.input_schema, method.output_schema]) localReferences(schema);
      input.set(method.name, validator.compile({ ...method.input_schema, $defs: core.$defs }));
      output.set(method.name, validator.compile({ ...method.output_schema, $defs: core.$defs }));
    }
    if ((await digest(discovery.methods)) !== discovery.methods_digest)
      throw new ProtocolError("methods_digest_mismatch");
    return new ContractRegistry(
      discovery,
      await digest(discovery.methods),
      input,
      output,
      new Uint8Array(coreBytes),
    );
  }
  async original(contract: MethodContract): Promise<ContractRegistry> {
    return ContractRegistry.create(
      { ...this.discovery, methods: [contract], methods_digest: await digest([contract]) },
      this.coreBytes,
    );
  }
  method(name: string, kind?: "command" | "query"): MethodContract {
    const method = this.discovery.methods.find((value) => value.name === name);
    if (!method || (kind && method.kind !== kind)) throw new ProtocolError(`unsupported:${name}`);
    return method;
  }
  validateInput(name: string, value: unknown, kind: "command" | "query"): unknown {
    this.method(name, kind);
    const validator = this.input.get(name);
    if (!validator) throw new ProtocolError("unsupported");
    jsonBytes(value, this.discovery.limits.max_domain_bytes);
    if (!validator(value)) throw new ProtocolError(`input_schema_violation:${name}`);
    return value;
  }
  validateOutput(name: string, value: unknown): unknown {
    const validator = this.output.get(name);
    if (!validator) throw new ProtocolError("unsupported");
    jsonBytes(value, this.discovery.limits.max_domain_bytes);
    if (!validator(value)) throw new ProtocolError(`output_schema_violation:${name}`);
    return value;
  }
  command(value: unknown): Command {
    const command = checked<Command>(validateCommand, value, "command_schema_violation");
    if (command.logical_service_id !== this.discovery.logical_service_id)
      throw new ProtocolError("owner_mismatch");
    const method = this.method(command.method, "command");
    if (method.cas && command.expected_revision === undefined)
      throw new ProtocolError("expected_revision_required");
    this.validateInput(command.method, command.payload, "command");
    jsonBytes(command, this.discovery.limits.max_domain_bytes);
    return command;
  }
  query(value: unknown): Query {
    const query = checked<Query>(validateQuery, value, "query_schema_violation");
    if (query.logical_service_id !== this.discovery.logical_service_id)
      throw new ProtocolError("owner_mismatch");
    this.validateInput(query.method, query.payload, "query");
    jsonBytes(query, this.discovery.limits.max_domain_bytes);
    return query;
  }
  receipt(value: unknown, command: Command, expectedDigest: string): Receipt {
    const receipt = checked<Receipt>(validateReceipt, value, "receipt_schema_violation");
    if (receipt.command_id !== command.command_id || receipt.request_digest !== expectedDigest)
      throw new ProtocolError("receipt_identity_mismatch");
    if (receipt.stage === "accepted") {
      if (
        !this.method(command.method).allows_accepted ||
        !receipt.accepted_at ||
        receipt.decided_at ||
        receipt.error ||
        receipt.output === undefined
      )
        throw new ProtocolError("invalid_accepted_receipt");
      this.validateOutput(command.method, receipt.output);
    } else if (receipt.stage === "applied") {
      if (!receipt.decided_at || receipt.error || receipt.output === undefined)
        throw new ProtocolError("invalid_applied_receipt");
      this.validateOutput(command.method, receipt.output);
    } else if (!receipt.decided_at || !receipt.error || receipt.output !== undefined)
      throw new ProtocolError("invalid_rejected_receipt");
    return receipt;
  }
  ready(value: unknown): Ready {
    if (
      !value ||
      typeof value !== "object" ||
      !Object.hasOwn(value, "identity_scope") ||
      !Object.hasOwn(value, "identity_revision")
    )
      throw new ProtocolError("unsupported_ready_identity_binding");
    const ready = checked<Ready>(validateReady, value, "ready_schema_violation");
    if (
      ready.identity_scope !== this.discovery.identity_scope ||
      ready.identity_revision !== this.discovery.identity_revision
    )
      throw new ProtocolError("ready_identity_mismatch");
    if (
      ready.logical_service_id !== this.discovery.logical_service_id ||
      ready.methods_digest !== this.methodsDigest ||
      canonical(ready.limits) !== canonical(this.discovery.limits)
    )
      throw new ProtocolError("ready_contract_mismatch");
    return ready;
  }
}
export function apiError(value: unknown): APIError {
  return checked<APIError>(validateError, value, "error_schema_violation");
}
export function responseFrame(value: unknown): ResponseFrame {
  return checked<ResponseFrame>(validateResponse, value, "response_schema_violation");
}
export function validateLimits(value: unknown): Limits {
  return checked<Limits>(envelope.compile(limitsSchema), value, "invalid_limits");
}
export function schemaFields(schema: Schema): Record<string, unknown> {
  return schema.properties &&
    !Array.isArray(schema.properties) &&
    typeof schema.properties === "object"
    ? schema.properties
    : {};
}

const recordValidators = new Map<string, ValidateFunction>();
export function validateRecord<T = unknown>(name: string, value: unknown): T {
  if (!Object.hasOwn(CORE_SCHEMA.$defs, name)) throw new ProtocolError("unknown_record_schema");
  let validator = recordValidators.get(name);
  if (!validator) {
    validator = envelope.compile({ $ref: `#/$defs/${name}`, $defs: CORE_SCHEMA.$defs });
    recordValidators.set(name, validator);
  }
  return checked<T>(validator, value, "record_schema_violation");
}

export function validateSchema(schema: unknown, value: unknown): void {
  jsonBytes(schema, 262144);
  jsonBytes(value, 262144);
  localReferences(schema);
  if (!schema || Array.isArray(schema) || typeof schema !== "object")
    throw new ProtocolError("invalid_local_schema");
  const validator = compiler().compile({ ...schema, $defs: CORE_SCHEMA.$defs });
  if (!validator(value)) throw new ProtocolError("schema_violation");
}
