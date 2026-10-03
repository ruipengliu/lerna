import test from 'node:test';
import { readFile } from 'node:fs/promises';
import assert from 'node:assert/strict';
import {
  getCommand,
  ContractError,
  type CommandGetResponse,
  type OwnerDirectory,
  type ReadAuthorizer,
  type CommandRef,
  type SubjectBinding,
} from './index.ts';
const original: CommandRef = {
  owner: { tenant_id: 't', owner_id: 'o' },
  command_id: 'original',
};
const subject: SubjectBinding = {
  tenant_id: 't',
  subject_id: 'allowed',
  delegation_chain: [],
};
const wire = (ref: CommandRef = original) =>
  JSON.stringify({
    contract_version: '1.0.0',
    profile: 'command',
    method: 'command.get',
    command_id: 'read',
    target: {
      tenant_id: ref.owner.tenant_id,
      owner_id: ref.owner.owner_id,
      kind: 'command',
      id: ref.command_id,
    },
    payload: { command_ref: ref },
    accept_before: '2026-10-03T01:00:00.000000Z',
  });
const same = (a: unknown, b: unknown) =>
  assert.deepEqual(
    JSON.parse(JSON.stringify(a)),
    JSON.parse(JSON.stringify(b)),
  );
const options = { maxReadDurationMs: 1000 };
const clock = () => '2026-10-03T00:00:00.000000Z';
const signal = new AbortController().signal;
const authorizer: ReadAuthorizer = {
  async authorizeCommandRead(_signal, s, r) {
    return (
      s.subject_id === 'allowed' &&
      r.owner.tenant_id === 't' &&
      r.owner.owner_id === 'o'
    );
  },
};
test('authenticated query has opaque denials paired with allowed controls', async () => {
  for (const exists of [true, false]) {
    const directory: OwnerDirectory = {
      async resolveCommandOwner(_signal, owner) {
        return {
          owner,
          reader: {
            async readCommand(_signal, ref) {
              return {
                status: exists ? 'gone' : 'not_found',
                command_ref: ref,
              };
            },
          },
        };
      },
    };
    same(
      await getCommand(
        signal,
        wire(),
        subject,
        authorizer,
        directory,
        clock,
        options,
      ),
      { status: exists ? 'gone' : 'not_found', command_ref: original },
    );
    same(
      await getCommand(
        signal,
        wire(),
        { ...subject, subject_id: 'denied' },
        authorizer,
        directory,
        clock,
        options,
      ),
      { status: 'rejected', reason: 'forbidden' },
    );
    same(
      await getCommand(
        signal,
        wire(),
        undefined,
        authorizer,
        directory,
        clock,
        options,
      ),
      { status: 'rejected', reason: 'forbidden' },
    );
  }
});

test('authenticated query snapshots mutable wire and host binding across callbacks', async () => {
  const data = new TextEncoder().encode(wire());
  const trusted = {
    ...subject,
    delegation_chain: [{ tenant_id: 't', subject_id: 'delegator' }],
  };
  const before = structuredClone(trusted);
  const auth: ReadAuthorizer = {
    async authorizeCommandRead(_signal, s, r) {
      data.set(
        new TextEncoder().encode(
          wire({ ...original, owner: { tenant_id: 't', owner_id: 'p' } }),
        ),
      );
      s.delegation_chain[0].subject_id = 'mutated';
      r.owner.owner_id = 'p';
      return true;
    },
  };
  const directory: OwnerDirectory = {
    async resolveCommandOwner(_signal, o) {
      return {
        owner: o,
        reader: {
          async readCommand(_signal, r) {
            return { status: 'gone', command_ref: r };
          },
        },
      };
    },
  };
  same(
    await getCommand(signal, data, trusted, auth, directory, clock, options),
    { status: 'gone', command_ref: original },
  );
  assert.deepEqual(trusted, before);
});

test('authenticated command.get consumes common isolation fixtures', async () => {
  const fixtures = JSON.parse(
    await readFile(
      new URL(
        '../../../conformance/fixtures/1.0.0/queries.json',
        import.meta.url,
      ),
      'utf8',
    ),
  ) as {
    name: string;
    request: string;
    trusted_subject?: SubjectBinding;
    authorization: string;
    directory: string;
    observation: CommandGetResponse;
    expected?: CommandGetResponse;
    error?: string;
  }[];
  for (const fixture of fixtures) {
    const auth: ReadAuthorizer = {
      async authorizeCommandRead(_signal, s, r) {
        if (fixture.authorization === 'failure')
          throw new Error('SECRET authentication infrastructure');
        const delegated =
          s.delegation_chain.length === 0 ||
          (s.delegation_chain.length === 1 &&
            s.delegation_chain[0].tenant_id === 't' &&
            s.delegation_chain[0].subject_id === 'delegator');
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
      async resolveCommandOwner(_signal, o) {
        if (fixture.directory === 'failure')
          throw new Error('SECRET directory');
        if (fixture.directory === 'wrong_owner') o.owner_id = 'other';
        if (fixture.directory === 'wrong_tenant') o.tenant_id = 'other';
        // Missing-reader fixture crosses the host boundary at runtime; it must fail closed.
        if (fixture.directory === 'missing_reader')
          return JSON.parse(JSON.stringify({ owner: o }));
        return {
          owner: o,
          reader: {
            async readCommand() {
              return fixture.observation;
            },
          },
        };
      },
    };
    const invoke = () =>
      getCommand(
        signal,
        fixture.request,
        fixture.trusted_subject,
        auth,
        directory,
        clock,
        options,
      );
    if (fixture.error)
      await assert.rejects(
        invoke,
        (error) =>
          error instanceof ContractError && error.code === fixture.error,
        fixture.name,
      );
    else same(await invoke(), fixture.expected);
  }
});

test('fixed owner permits process replacement and remains read-only after default changes', async () => {
  const fixed: CommandGetResponse = {
    status: 'found',
    command_ref: original,
    receipt: {
      state: 'accepted',
      command_ref: original,
      object_ref: {
        tenant_id: 't',
        owner_id: 'tasks',
        kind: 'task',
        id: 'task-1',
      },
    },
    progress: { kind: 'none' },
  };
  const facts = {
    fixed,
    originalDeadline: '2025-01-01T00:00:00.000000Z',
    jobs: ['already-existing'],
    effects: ['unknown'],
  };
  const before = structuredClone(facts);
  const readers = new Map(
    ['old-process', 'replacement-process'].map((address) => [
      address,
      {
        async readCommand() {
          return facts.fixed;
        },
      },
    ]),
  );
  let address = 'old-process',
    defaultOwner = 'o',
    available = true;
  const directory: OwnerDirectory = {
    async resolveCommandOwner(_signal, owner) {
      if (owner.owner_id === defaultOwner && owner.owner_id !== 'o')
        return {
          owner,
          reader: {
            async readCommand(_s, r) {
              return { status: 'not_found', command_ref: r };
            },
          },
        };
      if (owner.owner_id !== 'o' || !available) throw new Error('offline');
      const reader = readers.get(address);
      if (!reader) throw new Error('missing route');
      return { owner, reader };
    },
  };
  same(
    await getCommand(
      signal,
      wire(),
      subject,
      authorizer,
      directory,
      clock,
      options,
    ),
    fixed,
  );
  address = 'replacement-process';
  defaultOwner = 'new-default';
  same(
    await getCommand(
      signal,
      wire(),
      subject,
      authorizer,
      directory,
      clock,
      options,
    ),
    fixed,
  );
  const fresh = {
    ...original,
    owner: { ...original.owner, owner_id: defaultOwner },
  };
  const freshAuth: ReadAuthorizer = {
    async authorizeCommandRead(_s, p, r) {
      return p.subject_id === 'allowed' && r.owner.owner_id === defaultOwner;
    },
  };
  same(
    await getCommand(
      signal,
      wire(fresh),
      subject,
      freshAuth,
      directory,
      clock,
      options,
    ),
    { status: 'not_found', command_ref: fresh },
  );
  available = false;
  same(
    await getCommand(
      signal,
      wire(),
      subject,
      authorizer,
      directory,
      clock,
      options,
    ),
    {
      status: 'unavailable',
      command_ref: original,
      reason: 'dependency_unavailable',
    },
  );
  assert.deepEqual(facts, before);
});

test('read cutoff gates start, cancellation and finite resource bounds cover every provider', async () => {
  const unavailable = {
    status: 'unavailable',
    command_ref: original,
    reason: 'dependency_unavailable',
  };
  const directory: OwnerDirectory = {
    async resolveCommandOwner(_signal, owner) {
      return {
        owner,
        reader: {
          async readCommand() {
            return { status: 'gone', command_ref: original };
          },
        },
      };
    },
  };
  for (const ms of [0, -1, 0.5, 60_001, Infinity, NaN])
    same(
      await getCommand(signal, wire(), subject, authorizer, directory, clock, {
        maxReadDurationMs: ms,
      }),
      unavailable,
    );
  let instant = clock();
  const advance: ReadAuthorizer = {
    async authorizeCommandRead(s, p, r) {
      instant = '2026-10-03T02:00:00.000000Z';
      return authorizer.authorizeCommandRead(s, p, r);
    },
  };
  same(
    await getCommand(
      signal,
      wire(),
      subject,
      advance,
      directory,
      () => instant,
      options,
    ),
    { status: 'gone', command_ref: original },
  );
  for (const stage of ['authorization', 'directory', 'facts']) {
    const controller = new AbortController();
    const block = async (s: AbortSignal): Promise<never> => {
      controller.abort();
      assert.ok(s.aborted);
      return new Promise(() => {});
    };
    const auth: ReadAuthorizer = {
      async authorizeCommandRead(s, p, r) {
        if (stage === 'authorization') return block(s);
        return authorizer.authorizeCommandRead(s, p, r);
      },
    };
    const routes: OwnerDirectory = {
      async resolveCommandOwner(s, o) {
        if (stage === 'directory') return block(s);
        return {
          owner: o,
          reader: {
            async readCommand(s) {
              if (stage === 'facts') return block(s);
              return { status: 'gone', command_ref: original };
            },
          },
        };
      },
    };
    same(
      await getCommand(
        controller.signal,
        wire(),
        subject,
        auth,
        routes,
        clock,
        options,
      ),
      unavailable,
    );
  }
});

test(
  'host read timer bounds an uncooperative asynchronous provider and observes late rejection',
  { timeout: 3000 },
  async () => {
    let lateReject: (reason: Error) => void = () => {};
    const auth: ReadAuthorizer = {
      async authorizeCommandRead() {
        return new Promise<boolean>((_resolve, reject) => {
          lateReject = reject;
        });
      },
    };
    const directory: OwnerDirectory = {
      async resolveCommandOwner(_s, owner) {
        return {
          owner,
          reader: {
            async readCommand() {
              return { status: 'gone', command_ref: original };
            },
          },
        };
      },
    };
    same(
      await getCommand(signal, wire(), subject, auth, directory, clock, {
        maxReadDurationMs: 1,
      }),
      {
        status: 'unavailable',
        command_ref: original,
        reason: 'dependency_unavailable',
      },
    );
    lateReject(new Error('late SECRET'));
    await new Promise<void>((resolve) => setImmediate(resolve));
  },
);
