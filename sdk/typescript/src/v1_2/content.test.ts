import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
  decodePut,
  declaredMethods,
  supportedMethods,
  ContractError,
} from './index.ts';
test('canonical body bound keeps exact input_over_limit classification', () => {
  const normal = readFileSync(
    new URL(
      '../../../../conformance/fixtures/1.2.0/put-alpha.json',
      import.meta.url,
    ),
    'utf8',
  );
  const request = decodePut(normal);
  assert.equal(request.payload.bytes_base64, 'YWxwaGEK');
  request.payload.bytes_base64 = Buffer.alloc(262145, 97).toString('base64');
  request.payload.content_ref.byte_length = '262145';
  assert.throws(
    () => decodePut(JSON.stringify(request)),
    (error: unknown) =>
      error instanceof ContractError && error.code === 'input_over_limit',
  );
});
test('version1.2 declares only Content and Command and does not advertise a partial profile', () => {
  assert.deepEqual(declaredMethods.map((method) => method.method).sort(), [
    'command.get',
    'content.get',
    'content.put',
  ]);
  assert.deepEqual(supportedMethods, []);
});
