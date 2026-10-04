import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
  getCommand,
  decode,
  decodeCommandResponse,
  ContractError,
  type ReadAuthorizer,
  type OwnerDirectory,
  type SubjectBinding,
} from './index.ts';
function object(value: unknown): Record<string, unknown> {
  assert.ok(
    value !== null && typeof value === 'object' && !Array.isArray(value),
  );
  return Object.fromEntries(Object.entries(value));
}
function text(value: unknown): string {
  assert.equal(typeof value, 'string');
  if (typeof value !== 'string') throw Error('fixture string');
  return value;
}
function principal(value: unknown): SubjectBinding | undefined {
  if (value === undefined) return undefined;
  const v = object(value);
  assert.ok(Array.isArray(v.delegation_chain));
  return {
    tenant_id: text(v.tenant_id),
    subject_id: text(v.subject_id),
    delegation_chain: v.delegation_chain.map((x) =>
      decode('DelegatedSubject', JSON.stringify(x)),
    ),
  };
}
test('1.1 authenticated command.get reads exact new receipts and rejects common isolation failures', async () => {
  const fixtures: unknown = JSON.parse(
    readFileSync(
      new URL(
        '../../../../conformance/fixtures/1.1.0/queries.json',
        import.meta.url,
      ),
      'utf8',
    ),
  );
  assert.ok(Array.isArray(fixtures));
  for (const raw of fixtures) {
    const f = object(raw);
    assert.equal(typeof f.request, 'string');
    if (typeof f.request !== 'string') throw Error('fixture request');
    const trusted = principal(f.trusted_subject);
    const auth: ReadAuthorizer = {
      async authorizeCommandRead(_signal, s, r) {
        if (f.authorization === 'failure')
          throw Error('fixture auth unavailable');
        const delegated =
          s.delegation_chain.length === 0 ||
          (s.delegation_chain.length === 1 &&
            s.delegation_chain[0]?.tenant_id === 't' &&
            s.delegation_chain[0]?.subject_id === 'delegator');
        return (
          s.tenant_id === 't' &&
          s.subject_id === 'allowed' &&
          delegated &&
          r.owner.tenant_id === 't' &&
          r.owner.owner_id === 'o' &&
          ['original', 'absent'].includes(r.command_id)
        );
      },
    };
    const directory: OwnerDirectory = {
      async resolveCommandOwner(_signal, owner) {
        if (f.directory === 'failure' || f.directory === 'missing_reader')
          throw Error('fixture directory unavailable');
        if (f.directory === 'wrong_owner') owner.owner_id = 'other';
        if (f.directory === 'wrong_tenant') owner.tenant_id = 'other';
        return {
          owner,
          reader: {
            async readCommand(_signal, ref) {
              return decodeCommandResponse(JSON.stringify(f.observation), ref);
            },
          },
        };
      },
    };
    const request = f.request;
    const invoke = () =>
      getCommand(
        new AbortController().signal,
        request,
        trusted,
        auth,
        directory,
        () => '2026-10-03T00:00:00.000000Z',
        { maxReadDurationMs: 1000 },
      );
    if (f.error !== undefined)
      await assert.rejects(
        invoke,
        (e) => e instanceof ContractError && e.code === f.error,
        String(f.name),
      );
    else
      assert.deepEqual(
        JSON.parse(JSON.stringify(await invoke())),
        f.expected,
        String(f.name),
      );
  }
});
