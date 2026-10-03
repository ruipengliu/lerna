import test from 'node:test';
import assert from 'node:assert/strict';
import { readCommandFacts, type CommandFactReader } from './readfacts.ts';
import type { CommandGetResponse, CommandReceipt } from './index.ts';
const ref = { owner: { tenant_id: 't', owner_id: 'o' }, command_id: 'c' };
const request = JSON.stringify({
  contract_version: '1.0.0',
  profile: 'command',
  method: 'command.get',
  command_id: 'read-new',
  target: { tenant_id: 't', owner_id: 'o', kind: 'command', id: 'c' },
  payload: { command_ref: ref },
  accept_before: '2026-10-03T01:00:00.000000Z',
});
const clock = () => '2026-10-03T00:00:00.000000Z';
const signal = new AbortController().signal;
const same = (a: unknown, b: unknown) =>
  assert.deepEqual(
    JSON.parse(JSON.stringify(a)),
    JSON.parse(JSON.stringify(b)),
  );
test('fact seam keeps receipt fixed across current task outcomes and remains read-only', async () => {
  const fixed: CommandReceipt = {
    state: 'accepted',
    command_ref: ref,
    object_ref: {
      tenant_id: 't',
      owner_id: 'tasks',
      kind: 'task',
      id: 'x',
      revision: '5',
    },
  };
  const facts = {
    receipt: fixed,
    jobs: ['old'],
    effects: ['unchanged'],
    originalDeadline: 'yesterday',
  };
  for (const status of ['active', 'succeeded', 'failed'] as const) {
    const snapshot = structuredClone(facts);
    const reader: CommandFactReader = {
      async readCommand() {
        return {
          status: 'found',
          command_ref: ref,
          receipt: fixed,
          progress: {
            kind: 'task',
            object_ref: {
              tenant_id: 't',
              owner_id: 'tasks',
              kind: 'task',
              id: 'x',
              revision: '6',
            },
            revision: '6',
            status,
          },
        };
      },
    };
    const response = await readCommandFacts(signal, request, reader, clock);
    assert.equal(response.status, 'found');
    if (response.status === 'found') assert.deepEqual(response.receipt, fixed);
    assert.deepEqual(facts, snapshot);
  }
});
test('fact seam separates absence, retention, faults and untrusted bindings', async () => {
  for (const status of ['not_found', 'gone', 'unavailable'] as const) {
    const response: CommandGetResponse =
      status === 'unavailable'
        ? { status, command_ref: ref, reason: 'dependency_unavailable' }
        : { status, command_ref: ref };
    assert.deepEqual(
      await readCommandFacts(
        signal,
        request,
        {
          async readCommand() {
            return response;
          },
        },
        clock,
      ),
      response,
    );
  }
  const unavailable = {
    status: 'unavailable',
    command_ref: ref,
    reason: 'dependency_unavailable',
  };
  same(
    await readCommandFacts(
      signal,
      request,
      {
        async readCommand() {
          throw new Error('SECRET backend detail');
        },
      },
      clock,
    ),
    unavailable,
  );
  same(
    await readCommandFacts(
      signal,
      request,
      {
        async readCommand() {
          return {
            status: 'gone',
            command_ref: { ...ref, command_id: 'wrong' },
          };
        },
      },
      clock,
    ),
    unavailable,
  );
  same(
    await readCommandFacts(
      signal,
      request,
      {
        async readCommand() {
          throw new Error('not relevant');
        },
      },
      () => '2026-10-03T01:00:00.000000Z',
    ),
    { status: 'rejected', reason: 'expired' },
  );
  const controller = new AbortController();
  controller.abort();
  same(
    await readCommandFacts(
      controller.signal,
      request,
      {
        async readCommand() {
          return { status: 'gone', command_ref: ref };
        },
      },
      clock,
    ),
    unavailable,
  );
});
