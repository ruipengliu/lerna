import { readFileSync } from 'node:fs';
import { decode, encode, schema, type Values } from './index.ts';
function supportedName(name: string | undefined): name is keyof Values {
  return name !== undefined && Object.hasOwn(schema.$defs, name);
}
const name = process.argv[2];
if (!supportedName(name)) throw new Error('usage: valuerunner SCHEMA < JSON');
const input = readFileSync(0);
process.stdout.write(encode(name, decode(name, input)));
