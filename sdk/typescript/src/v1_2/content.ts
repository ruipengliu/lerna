import { createHash } from 'node:crypto';
import { decode, encode, version } from './codec.ts';
import { parseCommand } from './commands.ts';
import { ContractError } from './errors.ts';
import type {
  ContentPutRequest,
  ContentGetRequest,
  ContentGetPayload,
  ContentGetResponse,
} from './generated/values.ts';
export const maxContentBytes = 262144;
export function decodeBytes(text: string): Uint8Array {
  if (
    !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(
      text,
    )
  )
    throw new ContractError('schema_invalid');
  const bytes = Buffer.from(text, 'base64');
  if (bytes.toString('base64') !== text)
    throw new ContractError('schema_invalid');
  if (bytes.length > maxContentBytes)
    throw new ContractError('input_over_limit');
  return bytes;
}
type ObjectValue = Record<string, unknown>;
function object(value: unknown): ObjectValue {
  return value as ObjectValue;
}
export function contentSemantics(name: string, value: unknown): boolean {
  if (value === null || typeof value !== 'object') return true;
  const o = object(value);
  if (name === 'ContentPutRequest' || name === 'ContentGetRequest') {
    const payload = object(o.payload),
      r = object(payload.content_ref),
      owner = object(r.owner),
      target = object(o.target);
    if (
      target.kind !== 'content' ||
      target.tenant_id !== owner.tenant_id ||
      target.owner_id !== owner.owner_id ||
      target.id !== r.content_id
    )
      return false;
    return contentSemantics(
      name === 'ContentPutRequest' ? 'ContentPutPayload' : 'ContentGetPayload',
      payload,
    );
  }
  if (name === 'ContentPutPayload') {
    try {
      decodeBytes(String(o.bytes_base64));
    } catch {
      return false;
    }
    const seen = new Set<string>();
    for (const raw of o.sources as unknown[]) {
      const r = object(raw),
        owner = object(r.owner);
      const key = JSON.stringify([
        owner.tenant_id,
        owner.owner_id,
        r.content_id,
        r.version,
      ]);
      if (seen.has(key)) return false;
      seen.add(key);
    }
  }
  if (
    (name === 'ContentGetResponse' || name === 'ContentGetResponsePublished') &&
    o.status === 'published'
  ) {
    let bytes: Uint8Array;
    try {
      bytes = decodeBytes(String(o.bytes_base64));
    } catch {
      return false;
    }
    const r = object(o.content_ref),
      total = BigInt(String(r.byte_length));
    if (total > BigInt(maxContentBytes)) return false;
    let expected = total;
    if (o.range !== undefined) {
      const part = object(o.range),
        offset = BigInt(String(part.offset)),
        length = BigInt(String(part.length));
      if (offset > total || length > total - offset) return false;
      expected = length;
    } else if (
      'sha256:' + createHash('sha256').update(bytes).digest('hex') !==
      r.hash
    )
      return false;
    if (BigInt(bytes.length) !== expected) return false;
  }
  return true;
}
export function decodePut(wire: string | Uint8Array): ContentPutRequest {
  const envelope = parseCommand(wire);
  if (envelope.contract_version !== version)
    throw new ContractError('version_unsupported');
  if (envelope.profile !== 'content' || envelope.method !== 'content.put')
    throw new ContractError('unsupported');
  return decode('ContentPutRequest', wire);
}
export function decodeGet(wire: string | Uint8Array): ContentGetRequest {
  const envelope = parseCommand(wire);
  if (envelope.contract_version !== version)
    throw new ContractError('version_unsupported');
  if (envelope.profile !== 'content' || envelope.method !== 'content.get')
    throw new ContractError('unsupported');
  return decode('ContentGetRequest', wire);
}
export function decodeContentResponse(
  wire: string | Uint8Array,
  request: ContentGetPayload,
): ContentGetResponse {
  const response = decode('ContentGetResponse', wire);
  if (response.status === 'rejected') return response;
  if (
    encode('ContentRef', response.content_ref) !==
    encode('ContentRef', request.content_ref)
  )
    throw new ContractError('schema_invalid');
  if (response.status === 'published') {
    if (request.range === undefined) {
      if (response.range !== undefined)
        throw new ContractError('schema_invalid');
    } else if (
      response.range === undefined ||
      encode('ContentRange', request.range) !==
        encode('ContentRange', response.range)
    )
      throw new ContractError('schema_invalid');
  }
  return response;
}
export function encodeContentResponse(
  response: ContentGetResponse,
  request: ContentGetPayload,
): string {
  const wire = encode('ContentGetResponse', response);
  decodeContentResponse(wire, request);
  return wire;
}
