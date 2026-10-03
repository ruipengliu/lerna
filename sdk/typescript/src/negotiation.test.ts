import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import {
  supportedMethods,
  negotiate,
  schema,
  ContractError,
  decodeCommand,
  getCommand,
  encode,
  decodeCommandResponse,
  encodeCommandResponse,
} from './index.ts';

test('negotiation advertises only the exact completed command.get contract', () => {
  assert.equal(supportedMethods.length, 1);
  const method = supportedMethods[0];
  assert.deepEqual(
    [
      method.contract_version,
      method.profile,
      method.method,
      method.input_schema,
      method.output_schema,
    ],
    [
      '1.0.0',
      'command',
      'command.get',
      'CommandGetRequest',
      'CommandGetResponse',
    ],
  );
  const { input_schema: _input, output_schema: _output, ...request } = method;
  assert.deepEqual(negotiate(JSON.stringify(request)), method);
});

test('negotiation consumes the same exact-version refusal fixtures', async () => {
  const fixtures = JSON.parse(
    await readFile(
      new URL(
        '../../../conformance/fixtures/1.0.0/negotiations.json',
        import.meta.url,
      ),
      'utf8',
    ),
  ) as Array<{ name: string; wire: string; valid: boolean; code: string }>;
  for (const fixture of fixtures) {
    if (fixture.valid)
      assert.deepEqual(negotiate(fixture.wire), supportedMethods[0]);
    else
      assert.throws(
        () => negotiate(fixture.wire),
        (error) =>
          error instanceof ContractError && error.code === fixture.code,
        fixture.name,
      );
  }
});

test('negotiation then trusted query preserves fixed receipt and current progress', async () => {
  const {
    input_schema: _i,
    output_schema: _o,
    ...desired
  } = supportedMethods[0];
  const chosen = negotiate(JSON.stringify(desired));
  const original = {
    owner: { tenant_id: 't', owner_id: 'o' },
    command_id: 'original',
  };
  const request = decodeCommand(
    JSON.stringify({
      contract_version: chosen.contract_version,
      profile: chosen.profile,
      method: chosen.method,
      command_id: 'fresh-read',
      target: {
        tenant_id: 't',
        owner_id: 'o',
        kind: 'command',
        id: 'original',
      },
      payload: { command_ref: original },
      accept_before: '2026-10-03T01:00:00.000000Z',
    }),
  );
  const observation = decodeCommandResponse(
    '{"status":"found","command_ref":{"owner":{"tenant_id":"t","owner_id":"o"},"command_id":"original"},"receipt":{"state":"accepted","command_ref":{"owner":{"tenant_id":"t","owner_id":"o"},"command_id":"original"},"object_ref":{"tenant_id":"t","owner_id":"tasks","kind":"task","id":"task-1"},"revision":"5"},"progress":{"kind":"task","object_ref":{"tenant_id":"t","owner_id":"tasks","kind":"task","id":"task-1"},"revision":"6","status":"succeeded"}}',
    original,
  );
  const result = await getCommand(
    new AbortController().signal,
    encode('CommandGetRequest', request),
    { tenant_id: 't', subject_id: 'allowed', delegation_chain: [] },
    {
      async authorizeCommandRead(_s, p, r) {
        return p.subject_id === 'allowed' && r.owner.owner_id === 'o';
      },
    },
    {
      async resolveCommandOwner(_s, owner) {
        return {
          owner,
          reader: {
            async readCommand() {
              return observation;
            },
          },
        };
      },
    },
    () => '2026-10-03T00:00:00.000000Z',
    { maxReadDurationMs: 1000 },
  );
  const decoded = decodeCommandResponse(
    encodeCommandResponse(result, original),
    original,
  );
  assert.deepEqual(decoded, observation);
  assert.equal(decoded.status, 'found');
  if (decoded.status !== 'found') throw Error('missing observation');
  assert.equal(decoded.receipt.state, 'accepted');
  assert.equal(decoded.progress.kind, 'task');
  if (decoded.progress.kind === 'task')
    assert.equal(decoded.progress.status, 'succeeded');
});

// This test independently traverses the embedded schema, without the generator
// or runtime command canonicalizer. Python hashlib literals are the authority.
function goldenSchemaDigest(root: string): string {
  const source: Record<string, unknown> = schema;
  const defs: Record<string, unknown> = schema.$defs;
  const reachable: Record<string, unknown> = Object.create(null);
  function visit(value: unknown): void {
    if (Array.isArray(value)) {
      value.forEach(visit);
      return;
    }
    if (!value || typeof value !== 'object') return;
    const ref: unknown = Reflect.get(value, '$ref');
    if (typeof ref === 'string') {
      assert.ok(ref.startsWith('#/$defs/'));
      const name = ref.slice(8);
      assert.ok(Object.hasOwn(defs, name));
      if (!Object.hasOwn(reachable, name)) {
        reachable[name] = defs[name];
        visit(defs[name]);
      }
    }
    Object.values(value).forEach(visit);
  }
  visit({ $ref: '#/$defs/' + root });
  function canonical(value: unknown): string {
    if (typeof value === 'number') {
      assert.ok(
        Number.isSafeInteger(value) && value >= 0 && !Object.is(value, -0),
      );
      return String(value);
    }
    if (Array.isArray(value)) return '[' + value.map(canonical).join(',') + ']';
    if (value && typeof value === 'object')
      return (
        '{' +
        Object.keys(value)
          .sort()
          .map(
            (key) =>
              JSON.stringify(key) + ':' + canonical(Reflect.get(value, key)),
          )
          .join(',') +
        '}'
      );
    const scalar = JSON.stringify(value);
    assert.notEqual(scalar, undefined);
    return scalar;
  }
  const bundle = {
    $schema: source.$schema,
    $id: source.$id,
    $ref: '#/$defs/' + root,
    $defs: reachable,
  };
  return (
    'sha256:' +
    createHash('sha256')
      .update('lerna-schema-digest-1\n' + canonical(bundle), 'utf8')
      .digest('hex')
  );
}

test('embedded input and output schemas independently match published digest goldens', async () => {
  const goldens = JSON.parse(
    await readFile(
      new URL(
        '../../../conformance/fixtures/1.0.0/schema-digests.json',
        import.meta.url,
      ),
      'utf8',
    ),
  ) as Array<{ algorithm: string; root: string; digest: string }>;
  const method = supportedMethods[0];
  for (const golden of goldens) {
    assert.equal(golden.algorithm, 'lerna-schema-digest-1');
    assert.equal(goldenSchemaDigest(golden.root), golden.digest);
    assert.equal(
      golden.root === 'CommandGetRequest'
        ? method.input_schema_digest
        : method.output_schema_digest,
      golden.digest,
    );
  }
  assert.ok(Object.isFrozen(supportedMethods));
  assert.ok(Object.isFrozen(method));
  const { input_schema: _i, output_schema: _o, ...request } = method;
  const returned = negotiate(JSON.stringify(request));
  returned.method = 'task.submit';
  assert.equal(negotiate(JSON.stringify(request)).method, 'command.get');
});

test('designed business methods never enter the authenticated query path', async () => {
  const base = JSON.stringify({
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
  });
  for (const method of [
    'task.submit',
    'memory.query',
    'delegation.create',
    'schedule.create',
    'environment.install',
  ])
    await assert.rejects(
      () =>
        getCommand(
          new AbortController().signal,
          base.replace('command.get', method),
          undefined,
          undefined,
          undefined,
          () => '2026-10-03T00:00:00.000000Z',
          { maxReadDurationMs: 1000 },
        ),
      (error) => error instanceof ContractError && error.code === 'unsupported',
    );
});
