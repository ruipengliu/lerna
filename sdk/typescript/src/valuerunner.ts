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
const name = process.argv[2];
const input = readFileSync(0);
try {
  if (name === 'CommandInput')
    process.stdout.write(encode('CommandGetRequest', decodeCommand(input)));
  else if (supportedName(name))
    process.stdout.write(encode(name, decode(name, input)));
  else throw new Error('usage: valuerunner SCHEMA < JSON');
} catch (cause) {
  const error =
    cause instanceof ContractError
      ? cause
      : new ContractError('schema_invalid', cause);
  process.stderr.write(encode('PublicError', error.toPublicError()) + '\n');
  process.exitCode = 1;
}
