import { decode, encode } from './codec.ts';
import { ContractError } from './errors.ts';
import { sameCommandRef } from './response-semantics.ts';
import type { CommandGetResponse, CommandRef } from './generated/values.ts';
export function decodeCommandResponse(
  wire: string | Uint8Array,
  original: CommandRef,
): CommandGetResponse {
  try {
    const result = decode('CommandGetResponse', wire);
    if (result.status !== 'rejected') {
      encode('CommandRef', original);
      if (!sameCommandRef(result.command_ref, original))
        throw new Error('original identity mismatch');
    }
    return result;
  } catch (cause) {
    throw new ContractError('schema_invalid', cause);
  }
}
export function encodeCommandResponse(
  result: CommandGetResponse,
  original: CommandRef,
): string {
  try {
    const wire = encode('CommandGetResponse', result);
    decodeCommandResponse(wire, original);
    return wire;
  } catch (cause) {
    throw new ContractError('schema_invalid', cause);
  }
}
