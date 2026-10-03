/** 与 api/json.go 同版：原字节检查先于普通对象解码，摘要采用 RFC 8785。 */
export type JSONValue =
  | null
  | boolean
  | number
  | string
  | JSONValue[]
  | { [key: string]: JSONValue };
export const MAX_DOMAIN_BYTES = 256 * 1024;
export const MAX_SAFE_NUMBER = 9_007_199_254_740_991;
const encoder = new TextEncoder();

export class ProtocolError extends Error {
  constructor(public readonly reason: string) {
    super(reason);
    this.name = "ProtocolError";
  }
}

function validUnicode(value: string): void {
  for (let i = 0; i < value.length; i++) {
    const unit = value.charCodeAt(i);
    if (unit >= 0xd800 && unit <= 0xdbff) {
      const next = value.charCodeAt(++i);
      if (!(next >= 0xdc00 && next <= 0xdfff)) throw new ProtocolError("invalid_unicode");
    } else if (unit >= 0xdc00 && unit <= 0xdfff) throw new ProtocolError("invalid_unicode");
  }
}

export function parseStrict(input: string | Uint8Array, maxBytes = MAX_DOMAIN_BYTES): JSONValue {
  const text =
    typeof input === "string"
      ? input
      : new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(input);
  validUnicode(text);
  if (!text.length || encoder.encode(text).byteLength > maxBytes)
    throw new ProtocolError("invalid_json_bytes");
  let offset = 0;
  const whitespace = () => {
    while (offset < text.length && /[\t\n\r ]/.test(text[offset] ?? "")) offset++;
  };
  const string = (): string => {
    const start = offset++;
    let escaped = false;
    while (offset < text.length) {
      const ch = text[offset++];
      if (!escaped && ch === '"') {
        let result: unknown;
        try {
          result = JSON.parse(text.slice(start, offset));
        } catch {
          throw new ProtocolError("invalid_json");
        }
        if (typeof result !== "string") throw new ProtocolError("invalid_json");
        validUnicode(result);
        return result;
      }
      if (!escaped && ch === "\\") escaped = true;
      else escaped = false;
    }
    throw new ProtocolError("invalid_json");
  };
  const value = (depth: number): JSONValue => {
    if (depth > 64) throw new ProtocolError("json_depth");
    whitespace();
    const ch = text[offset];
    if (ch === '"') return string();
    if (ch === "{") {
      offset++;
      whitespace();
      const result: { [key: string]: JSONValue } = Object.create(null);
      const keys = new Set<string>();
      if (text[offset] === "}") {
        offset++;
        return result;
      }
      while (true) {
        whitespace();
        if (text[offset] !== '"') throw new ProtocolError("invalid_json");
        const key = string();
        if (keys.has(key)) throw new ProtocolError("duplicate_key");
        keys.add(key);
        whitespace();
        if (text[offset++] !== ":") throw new ProtocolError("invalid_json");
        result[key] = value(depth + 1);
        whitespace();
        const end = text[offset++];
        if (end === "}") return result;
        if (end !== ",") throw new ProtocolError("invalid_json");
      }
    }
    if (ch === "[") {
      offset++;
      whitespace();
      const result: JSONValue[] = [];
      if (text[offset] === "]") {
        offset++;
        return result;
      }
      while (true) {
        if (result.length >= 10000) throw new ProtocolError("array_bound");
        result.push(value(depth + 1));
        whitespace();
        const end = text[offset++];
        if (end === "]") return result;
        if (end !== ",") throw new ProtocolError("invalid_json");
      }
    }
    for (const [literal, result] of [
      ["true", true],
      ["false", false],
      ["null", null],
    ] as const) {
      if (text.startsWith(literal, offset)) {
        offset += literal.length;
        return result;
      }
    }
    const token = /^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?/.exec(
      text.slice(offset),
    )?.[0];
    if (!token) throw new ProtocolError("invalid_json");
    offset += token.length;
    const number = Number(token);
    if (!Number.isFinite(number) || Math.abs(number) > MAX_SAFE_NUMBER)
      throw new ProtocolError("unsafe_number");
    return number;
  };
  const result = value(0);
  whitespace();
  if (offset !== text.length) throw new ProtocolError("trailing_json");
  return result;
}

/** 只接受可准确表述的 JSON 值，不静默省略 undefined、转换 bigint 或序列化类实例。 */
export function canonical(value: unknown): string {
  const seen = new Set<object>();
  const write = (item: unknown, depth: number): string => {
    if (depth > 64) throw new ProtocolError("json_depth");
    if (item === null) return "null";
    if (typeof item === "string") {
      validUnicode(item);
      return JSON.stringify(item);
    }
    if (typeof item === "boolean") return item ? "true" : "false";
    if (typeof item === "number") {
      if (!Number.isFinite(item) || Math.abs(item) > MAX_SAFE_NUMBER)
        throw new ProtocolError("unsafe_number");
      return JSON.stringify(item);
    }
    if (typeof item !== "object") throw new ProtocolError("invalid_json_value");
    if (seen.has(item)) throw new ProtocolError("cyclic_json");
    seen.add(item);
    let result: string;
    if (Array.isArray(item)) {
      if (item.length > 10000) throw new ProtocolError("array_bound");
      const entries: string[] = [];
      for (let i = 0; i < item.length; i++) {
        if (!Object.hasOwn(item, i)) throw new ProtocolError("sparse_json_array");
        entries.push(write(item[i], depth + 1));
      }
      result = `[${entries.join(",")}]`;
    } else {
      if (Object.getPrototypeOf(item) !== Object.prototype && Object.getPrototypeOf(item) !== null)
        throw new ProtocolError("invalid_json_object");
      const record = item as Record<string, unknown>;
      result = `{${Object.keys(record)
        .sort()
        .map((key) => `${write(key, depth + 1)}:${write(record[key], depth + 1)}`)
        .join(",")}}`;
    }
    seen.delete(item);
    return result;
  };
  return write(value, 0);
}

export function jsonBytes(value: unknown, maxBytes = MAX_DOMAIN_BYTES): Uint8Array {
  const bytes = encoder.encode(canonical(value));
  if (bytes.byteLength > maxBytes) throw new ProtocolError("invalid_json_bytes");
  return bytes;
}

export async function sha256(bytes: Uint8Array): Promise<string> {
  const hash = await crypto.subtle.digest("SHA-256", new Uint8Array(bytes).buffer);
  return `sha256:${Array.from(new Uint8Array(hash), (x) => x.toString(16).padStart(2, "0")).join("")}`;
}
export async function digest(value: unknown): Promise<string> {
  return digestLimit(value, MAX_DOMAIN_BYTES);
}
/** 只有清单等明确规定更大容量的契约可使用此入口；业务摘要保持默认域上限。 */
export async function digestLimit(value: unknown, maxBytes: number): Promise<string> {
  return sha256(jsonBytes(value, maxBytes));
}
