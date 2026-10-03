import { decode, version } from './codec.ts';
import {
  inputSchemas,
  type CommandGetRequest,
  type CommandEnvelope,
} from './generated/values.ts';

import { ContractError } from './errors.ts';
export { ContractError } from './errors.ts';

export function parseCommand(wire: string | Uint8Array): CommandEnvelope {
  try {
    return decode('CommandEnvelope', wire);
  } catch (cause) {
    throw new ContractError('schema_invalid', cause);
  }
}

// A parsed envelope alone is never permission to execute a method.
export function decodeCommand(wire: string | Uint8Array): CommandGetRequest {
  const envelope = parseCommand(wire);
  if (envelope.contract_version !== version)
    throw new ContractError('version_unsupported');
  const registered = inputSchemas.find(
    (entry) =>
      entry.version === envelope.contract_version &&
      entry.profile === envelope.profile &&
      entry.method === envelope.method,
  );
  if (!registered) throw new ContractError('unsupported');
  let result: CommandGetRequest;
  try {
    result = decode(registered.schema, wire);
  } catch (cause) {
    throw new ContractError('schema_invalid', cause);
  }
  const ref = result.payload.command_ref;
  if (
    result.target.tenant_id !== ref.owner.tenant_id ||
    result.target.owner_id !== ref.owner.owner_id ||
    result.target.id !== ref.command_id
  )
    throw new ContractError('schema_invalid');
  return result;
}
