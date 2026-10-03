import test from 'node:test';
import assert from 'node:assert/strict';
import {
  schema,
  decodeCommand,
  supportedMethods,
  ContractError,
} from './index.ts';

// node:test runs this file in its own process, so the mutation precedes the
// first command.get validator compilation rather than testing a cached schema.
test('published schema cannot broaden the contract before its first validation', () => {
  const before = JSON.stringify(schema);
  Reflect.set(schema.$defs.CommandGetPayload, 'additionalProperties', true);
  const request = {
    contract_version: '1.0.0',
    profile: 'command',
    method: 'command.get',
    command_id: 'read',
    target: { tenant_id: 't', owner_id: 'o', kind: 'command', id: 'original' },
    payload: {
      command_ref: {
        owner: { tenant_id: 't', owner_id: 'o' },
        command_id: 'original',
      },
    },
    accept_before: '2026-10-03T01:00:00.000000Z',
  };
  assert.throws(
    () =>
      decodeCommand(
        JSON.stringify({
          ...request,
          payload: { ...request.payload, unexpected: 'extra' },
        }),
      ),
    (error) =>
      error instanceof ContractError && error.code === 'schema_invalid',
  );
  assert.equal(
    decodeCommand(JSON.stringify(request)).payload.command_ref.command_id,
    'original',
  );
  // Also protect nested property objects and arrays that describe the public
  // source; a private validator clone alone cannot protect this advertisement.
  Reflect.set(
    schema.$defs.CommandGetRequest.properties.method,
    'const',
    'task.get',
  );
  Reflect.set(schema.$defs.CommandGetRequest.required, '0', 'unexpected');
  Reflect.set(schema.$defs.CommandGetResponse.oneOf, '0', {});
  assert.equal(JSON.stringify(schema), before);
  assert.equal(
    supportedMethods[0].input_schema_digest,
    'sha256:bd37d7bb6f69006352bc74c27d04e13faaac912812d28f91c36794d6dbb8278b',
  );
});
