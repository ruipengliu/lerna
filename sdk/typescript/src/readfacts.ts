// Low-level fact primitive. Deliberately absent from SDK business exports;
// the authenticated command.get service must authorize before calling this.
import { decode } from './codec.ts';
import { decodeCommand } from './commands.ts';
import { encodeCommandResponse, decodeCommandResponse } from './receipts.ts';
import type { CommandGetResponse, CommandRef } from './generated/values.ts';
export interface CommandFactReader {
  readCommand(
    signal: AbortSignal,
    original: CommandRef,
  ): Promise<CommandGetResponse>;
}
export async function readCommandFacts(
  signal: AbortSignal,
  wire: string | Uint8Array,
  reader: CommandFactReader,
  now: () => string,
): Promise<CommandGetResponse> {
  const request = decodeCommand(wire),
    ref = request.payload.command_ref;
  const unavailable: CommandGetResponse = {
    status: 'unavailable',
    command_ref: ref,
    reason: 'dependency_unavailable',
  };
  // Both validated UTC timestamps have identical microsecond representation.
  // The injected trusted clock must use this exact canonical representation.
  let instant: string;
  try {
    instant = decode('Time', JSON.stringify(now()));
  } catch {
    return unavailable;
  }
  if (instant >= request.accept_before)
    return { status: 'rejected', reason: 'expired' };
  if (signal.aborted) return unavailable;
  try {
    const result = await reader.readCommand(signal, structuredClone(ref));
    if (signal.aborted) return unavailable;
    const encoded = encodeCommandResponse(result, ref);
    return decodeCommandResponse(encoded, ref);
  } catch {
    return unavailable;
  }
}
