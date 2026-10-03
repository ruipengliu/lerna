import { parseJSON, maxDepth } from './json.ts';
export { parseJSON, maxBodyBytes, maxDepth } from './json.ts';
import { Ajv2020 } from 'ajv/dist/2020.js';
import { schema, type Values } from './generated/values.ts';
export * from './generated/values.ts';
export const version = '1.0.0';
const ajv = new Ajv2020({
  strict: true,
  coerceTypes: false,
  useDefaults: false,
  removeAdditional: false,
});
ajv.addFormat('int64-decimal', {
  type: 'string',
  validate: (s: string) =>
    s.length <= 19 &&
    /^(0|[1-9][0-9]*)$/.test(s) &&
    BigInt(s) <= 9223372036854775807n,
});
ajv.addFormat('utc-microseconds', {
  type: 'string',
  validate: (s: string) => {
    const match =
      /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})\.\d{6}Z$/.exec(s);
    if (!match) return false;
    const y = Number(match[1]),
      m = Number(match[2]),
      d = Number(match[3]);
    const days = [
      31,
      y % 4 === 0 && (y % 100 !== 0 || y % 400 === 0) ? 29 : 28,
      31,
      30,
      31,
      30,
      31,
      31,
      30,
      31,
      30,
      31,
    ];
    return (
      y >= 1 &&
      m >= 1 &&
      m <= 12 &&
      d >= 1 &&
      d <= (days[m - 1] ?? 0) &&
      Number(match[4]) <= 23 &&
      Number(match[5]) <= 59 &&
      Number(match[6]) <= 59
    );
  },
});
ajv.addSchema(schema);

// Validate original JSON values before serialization can omit or coerce them.
function isWireValue(value: unknown, depth = 0): boolean {
  if (value === null || typeof value === 'boolean') return true;
  if (typeof value === 'string') {
    // A Unicode scalar cannot contain an unmatched UTF-16 surrogate.
    for (let i = 0; i < value.length; i++) {
      const code = value.charCodeAt(i);
      if (code >= 0xd800 && code <= 0xdbff) {
        const next = value.charCodeAt(++i);
        if (!(next >= 0xdc00 && next <= 0xdfff)) return false;
      } else if (code >= 0xdc00 && code <= 0xdfff) return false;
    }
    return true;
  }
  if (typeof value !== 'object' || depth >= maxDepth) return false;
  if (Array.isArray(value))
    return Array.from(value).every((item) => isWireValue(item, depth + 1));
  const prototype: unknown = Object.getPrototypeOf(value);
  if (prototype !== null && prototype !== Object.prototype) return false;
  for (const key of Reflect.ownKeys(value)) {
    if (typeof key !== 'string' || !isWireValue(key)) return false;
    const descriptor = Object.getOwnPropertyDescriptor(value, key);
    if (
      !descriptor?.enumerable ||
      !Object.hasOwn(descriptor, 'value') ||
      !isWireValue(descriptor.value, depth + 1)
    )
      return false;
  }
  return true;
}

export function validate<K extends keyof Values>(
  name: K,
  value: unknown,
): value is Values[K] {
  if (!isWireValue(value)) return false;
  const validator = ajv.getSchema(`${schema.$id}#/$defs/${name}`);
  if (!validator) throw new Error(`unsupported value type ${name}`);
  return validator(value) === true;
}
export function decode<K extends keyof Values>(
  name: K,
  wire: string | Uint8Array,
): Values[K] {
  const value: unknown = parseJSON(wire);
  if (!validate(name, value)) throw new Error(`schema_invalid: ${name}`);
  return value;
}
export function encode<K extends keyof Values>(
  name: K,
  value: Values[K],
): string {
  if (!validate(name, value)) throw new Error(`schema_invalid: ${name}`);
  const wire = JSON.stringify(value);
  decode(name, wire);
  return wire;
}
export { ContractError, parseCommand, decodeCommand } from './commands.ts';
