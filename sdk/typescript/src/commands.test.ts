import { test } from 'node:test';
import { strict as assert } from 'node:assert';
import {
  encode,
  validate,
  decode,
  decodeCommand,
  parseCommand,
  ContractError,
} from './index.ts';

test('public command errors use a closed code instead of string matching', () => {
  assert.equal(
    decode('PublicError', '{"code":"schema_invalid"}').code,
    'schema_invalid',
  );
  assert.throws(() => decode('PublicError', '{"code":"backend_stack_trace"}'));
  assert.throws(
    () => parseCommand('{} {}'),
    (error: unknown) =>
      error instanceof ContractError && error.code === 'schema_invalid',
  );
});

test('common envelope retains a future method without offering execution', () => {
  const value = parseCommand(
    '{"contract_version":"1.0.0","profile":"core","command_id":"q1","target":{"tenant_id":"t1","owner_id":"o1","kind":"task","id":"task1"},"method":"task.pause","expected_revision":"9007199254740993","payload":{"reason":"用户暂停"},"accept_before":"2026-10-03T10:00:00.000000Z","trace_context":{"trace_id":"trace1"}}',
  );
  assert.equal(value.method, 'task.pause');
  assert.equal(value.expected_revision, '9007199254740993');
});

test('common envelope rejects undeclared fields before interpreting a payload', () => {
  assert.throws(
    () =>
      parseCommand(
        '{"contract_version":"1.0.0","profile":"core","command_id":"q1","target":{"tenant_id":"t1","owner_id":"o1","kind":"task","id":"task1"},"method":"task.pause","payload":{},"accept_before":"2026-10-03T10:00:00.000000Z","tenant_id":"forged"}',
      ),
    (error: unknown) =>
      error instanceof ContractError && error.code === 'schema_invalid',
  );
});

const commandGetWire =
  '{"contract_version":"1.0.0","profile":"command","command_id":"query1","target":{"tenant_id":"t1","owner_id":"o1","kind":"command","id":"original1"},"method":"command.get","payload":{"command_ref":{"owner":{"tenant_id":"t1","owner_id":"o1"},"command_id":"original1"}},"accept_before":"2026-10-03T10:00:00.000000Z"}';
test('SDK checks original identity and method-specific read preconditions', () => {
  const value = decodeCommand(commandGetWire);
  assert.equal(value.command_id, 'query1');
  assert.equal(value.payload.command_ref.command_id, 'original1');
  for (const wire of [
    commandGetWire.replace(
      '"accept_before":',
      '"expected_revision":"7","accept_before":',
    ),
    commandGetWire.replace('"kind":"command"', '"kind":"task"'),
    commandGetWire.replace('"id":"original1"', '"id":"other"'),
    commandGetWire.replace('"tenant_id":"t1"', '"tenant_id":"other"'),
    commandGetWire.replace('"owner_id":"o1"', '"owner_id":"other"'),
  ])
    assert.throws(
      () => decodeCommand(wire),
      (error: unknown) =>
        error instanceof ContractError && error.code === 'schema_invalid',
    );
});

test('programmable payloads cannot be silently repaired by JSON.stringify', () => {
  const envelope = parseCommand(commandGetWire);
  for (const payload of [
    { counter: NaN },
    { counter: undefined },
    { counter: 1 },
    { counter: Infinity },
  ]) {
    envelope.payload = payload;
    assert.throws(() => encode('CommandEnvelope', envelope));
    assert.equal(validate('CommandEnvelope', envelope), false);
  }
});

test('public error projection excludes local cause and Error metadata', () => {
  const refusal = new ContractError(
    'dependency_unavailable',
    new Error('private database endpoint'),
  );
  assert.equal(
    encode('PublicError', refusal.toPublicError()),
    '{"code":"dependency_unavailable"}',
  );
});

import { readFileSync } from 'node:fs';
import { type ErrorCode } from './index.ts';
test('SDK accepts and classifies the common command conformance corpus', () => {
  const fixtures: Array<{
    name: string;
    schema: 'CommandInput' | 'CommandEnvelope' | 'PublicError';
    wire: string;
    valid: boolean;
    code?: ErrorCode;
    leading_spaces?: number;
  }> = JSON.parse(
    readFileSync(
      new URL(
        '../../../conformance/fixtures/1.0.0/commands.json',
        import.meta.url,
      ),
      'utf8',
    ),
  );
  for (const fixture of fixtures) {
    const wire = ' '.repeat(fixture.leading_spaces ?? 0) + fixture.wire;
    const run = () => {
      if (fixture.schema === 'CommandInput') return decodeCommand(wire);
      if (fixture.schema === 'CommandEnvelope') return parseCommand(wire);
      try {
        return decode('PublicError', wire);
      } catch (cause) {
        throw new ContractError('schema_invalid', cause);
      }
    };
    if (fixture.valid) assert.doesNotThrow(run, fixture.name);
    else
      assert.throws(
        run,
        (error: unknown) =>
          error instanceof ContractError && error.code === fixture.code,
        fixture.name,
      );
  }
});

test('machine tokens and UTC strings cannot include a trailing line terminator', () => {
  for (const text of ['\n', '\r', '\u2028', '\u2029']) {
    const command = JSON.parse(commandGetWire);
    command.command_id += text;
    assert.throws(() => decodeCommand(JSON.stringify(command)));
    assert.throws(() =>
      decode('Time', JSON.stringify('2026-10-03T10:00:00.000000Z' + text)),
    );
  }
});
