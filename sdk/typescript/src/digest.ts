import { decode } from './codec.ts';
import { parseCommand, ContractError } from './commands.ts';

export const commandDigestAlgorithm = 'lerna-command-digest-1';

// The host provides trusted subject JSON; payload never supplies this binding.
// This format-level digest provides no method support or execution permission.
export async function commandDigest(
  commandWire: string | Uint8Array,
  subjectWire: string | Uint8Array,
): Promise<string> {
  const envelope = parseCommand(commandWire);
  let subject: unknown;
  try {
    subject = decode('SubjectBinding', subjectWire);
  } catch (cause) {
    throw new ContractError('schema_invalid', cause);
  }
  // Each raw wire was bounded before building this internal hash preimage.
  const content: Record<string, unknown> = {
    contract_version: envelope.contract_version,
    profile: envelope.profile,
    method: envelope.method,
    target: envelope.target,
    payload: envelope.payload,
    accept_before: envelope.accept_before,
    subject_binding: subject,
  };
  if (envelope.expected_revision !== undefined)
    content.expected_revision = envelope.expected_revision;
  const preimage = new TextEncoder().encode(
    commandDigestAlgorithm + '\n' + canonical(content),
  );
  const digest = await globalThis.crypto.subtle.digest('SHA-256', preimage);
  return (
    'sha256:' +
    Array.from(new Uint8Array(digest), (b) =>
      b.toString(16).padStart(2, '0'),
    ).join('')
  );
}

// Inputs already passed the strict scalar/no-number raw JSON boundary.
function canonical(value: unknown): string {
  if (value === null || typeof value === 'boolean' || typeof value === 'string')
    return JSON.stringify(value);
  if (Array.isArray(value)) return '[' + value.map(canonical).join(',') + ']';
  if (typeof value === 'object' && value !== null) {
    return (
      '{' +
      Object.keys(value)
        .sort()
        .map((key) => {
          const item: unknown = Reflect.get(value, key);
          return JSON.stringify(key) + ':' + canonical(item);
        })
        .join(',') +
      '}'
    );
  }
  throw new ContractError('schema_invalid');
}
