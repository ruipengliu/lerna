import { readFileSync } from 'node:fs';
import {
  decode,
  encode,
  schema,
  decodeCommand,
  ContractError,
  type Values,
} from './index.ts';
function supportedName(name: string | undefined): name is keyof Values {
  return name !== undefined && Object.hasOwn(schema.$defs, name);
}
// Both tool modes enter this one public typed codec path.
function roundtrip(name: string | undefined, input: Uint8Array): string {
  if (name === 'CommandInput')
    return encode('CommandGetRequest', decodeCommand(input));
  if (supportedName(name)) return encode(name, decode(name, input));
  throw new Error('usage: valuerunner SCHEMA < JSON');
}
function publicError(cause: unknown) {
  return (
    cause instanceof ContractError
      ? cause
      : new ContractError('schema_invalid', cause)
  ).toPublicError();
}
function batch(): void {
  const maxFrameBytes = 8 * 1024 * 1024;
  let input = Buffer.alloc(0);
  const fail = (cause: unknown) => {
    process.stderr.write(String(cause) + '\n');
    process.exitCode = 1;
    process.stdin.destroy();
  };
  process.stdin.on('data', (chunk: Buffer) => {
    try {
      if (input.length + chunk.length > maxFrameBytes + 1)
        throw Error('request frame exceeds 8 MiB');
      input = Buffer.concat([input, chunk]);
      let end: number;
      while ((end = input.indexOf(10)) >= 0) {
        const line = input.subarray(0, end);
        input = input.subarray(end + 1);
        if (line.length > maxFrameBytes)
          throw Error('request frame exceeds 8 MiB');
        const request: unknown = JSON.parse(
          new TextDecoder('utf-8', { fatal: true }).decode(line),
        );
        if (
          request === null ||
          typeof request !== 'object' ||
          Array.isArray(request) ||
          Object.keys(request).sort().join() !== 'id,schema,wire_base64' ||
          !('id' in request) ||
          typeof request.id !== 'string' ||
          request.id.length < 1 ||
          request.id.length > 128 ||
          !('schema' in request) ||
          typeof request.schema !== 'string' ||
          request.schema.length < 1 ||
          request.schema.length > 128 ||
          !('wire_base64' in request) ||
          typeof request.wire_base64 !== 'string'
        )
          throw Error('invalid request shape');
        const wire = Buffer.from(request.wire_base64, 'base64');
        if (wire.toString('base64') !== request.wire_base64)
          throw Error('invalid request base64');
        let response;
        try {
          response = {
            id: request.id,
            ok: true,
            wire_base64: Buffer.from(roundtrip(request.schema, wire)).toString(
              'base64',
            ),
          };
        } catch (cause) {
          response = { id: request.id, ok: false, error: publicError(cause) };
        }
        process.stdout.write(JSON.stringify(response) + '\n');
      }
    } catch (cause) {
      fail(cause);
    }
  });
  process.stdin.on('end', () => {
    if (input.length) fail(Error('incomplete request frame'));
  });
  process.stdin.on('error', fail);
}
if (process.argv[2] === '--batch') batch();
else {
  try {
    process.stdout.write(roundtrip(process.argv[2], readFileSync(0)));
  } catch (cause) {
    process.stderr.write(encode('PublicError', publicError(cause)) + '\n');
    process.exitCode = 1;
  }
}
