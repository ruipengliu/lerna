import { decode, version } from './codec.ts';
import { supportedMethods, type MethodSupport } from './generated/values.ts';
import { ContractError } from './errors.ts';

// Compatibility is not authorization or execution eligibility.
export function negotiate(wire: string | Uint8Array): MethodSupport {
  let request;
  try {
    request = decode('NegotiationRequest', wire);
  } catch (cause) {
    throw new ContractError('schema_invalid', cause);
  }
  if (request.contract_version !== version)
    throw new ContractError('version_unsupported');
  const method = supportedMethods.find(
    (method) =>
      method.contract_version === request.contract_version &&
      method.profile === request.profile &&
      method.method === request.method,
  );
  if (!method) throw new ContractError('unsupported');
  if (
    method.input_schema_digest !== request.input_schema_digest ||
    method.output_schema_digest !== request.output_schema_digest
  )
    throw new ContractError('version_unsupported');
  return { ...method };
}
