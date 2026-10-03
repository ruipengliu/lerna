import test from 'node:test';
import assert from 'node:assert/strict';
import { startRunner } from './contract-runner.mjs';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { execFileSync, spawnSync } from 'node:child_process';

const echo = `
const readline = require('node:readline');
readline.createInterface({ input: process.stdin }).on('line', line => {
  const request = JSON.parse(line);
  const wire = Buffer.from(request.wire_base64, 'base64').toString();
  process.stdout.write(JSON.stringify(wire === 'bad'
    ? { id: request.id, ok: false, error: { code: 'schema_invalid' } }
    : { id: request.id, ok: true, wire_base64: request.wire_base64 }) + '\\n');
});`;

test('one runner continues after a classified refusal and repeats exact bytes', async () => {
  const runner = startRunner(process.execPath, ['-e', echo]);
  try {
    const input = Buffer.from([0xff, 0x00, 0x0a]);
    assert.deepEqual(
      (await runner.run('Fixture', input, 'normal')).wire,
      input,
    );
    assert.deepEqual(
      await runner.run('Fixture', Buffer.from('bad'), 'refusal'),
      { ok: false, error: { code: 'schema_invalid' } },
    );
    assert.deepEqual(
      (await runner.run('Fixture', input, 'repeat')).wire,
      input,
    );
  } finally {
    await runner.close();
  }
  assert.throws(() => process.kill(runner.pid, 0), { code: 'ESRCH' });
});

test('actual typed Go and TS runners retain exact values after a public refusal', async () => {
  const directory = mkdtempSync(join(tmpdir(), 'lerna-runner-test-'));
  try {
    const executable = join(directory, 'go-values');
    execFileSync(
      'go',
      ['build', '-o', executable, './conformance/component/valuerunner'],
      { timeout: 60000 },
    );
    for (const [command, args] of [
      [executable, ['--batch']],
      [process.execPath, ['sdk/typescript/src/valuerunner.ts', '--batch']],
    ]) {
      const runner = startRunner(command, args);
      try {
        const value = Buffer.from('"9223372036854775807"');
        assert.deepEqual((await runner.run('Revision', value)).wire, value);
        for (const invalid of [
          Buffer.from('"01"'),
          Buffer.from([34, 255, 34]),
        ]) {
          assert.deepEqual(await runner.run('Revision', invalid), {
            ok: false,
            error: { code: 'schema_invalid' },
          });
        }
        assert.deepEqual((await runner.run('Revision', value)).wire, value);
      } finally {
        await runner.close();
      }
      for (const input of [
        '{"id":"x","schema":"Revision"}\n',
        '{"ID":"x","schema":"Revision","wire_base64":"IjEi"}\n',
        '\ufeff{"id":"x","schema":"Revision","wire_base64":"IjEi"}\n',
        Buffer.concat([
          Buffer.from('{"id":"'),
          Buffer.from([255]),
          Buffer.from('","schema":"Revision","wire_base64":"IjEi"}\n'),
        ]),
      ]) {
        const invalid = spawnSync(command, args, {
          input,
          encoding: 'utf8',
          timeout: 10000,
        });
        assert.ifError(invalid.error);
        assert.notEqual(
          invalid.status,
          0,
          'malformed IPC must not become a public refusal',
        );
        assert.equal(invalid.stdout, '');
      }
    }
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

const response = (expression) => `process.stdin.once('data', chunk => {
const request = JSON.parse(chunk); ${expression}
}); process.stdin.resume();`;
const faults = [
  [
    'timeout',
    `setInterval(() => {}, 1000); process.stdin.resume();`,
    /deadline/,
  ],
  ['crash', response('process.exit(7)'), /exited/],
  [
    'wrong id',
    response(
      `process.stdout.write(JSON.stringify({ id: 'wrong', ok: true, wire_base64: request.wire_base64 }) + '\\n')`,
    ),
    /id mismatch/,
  ],
  ['illegal frame', response(`process.stdout.write('not JSON\\n')`), /JSON/],
  [
    'BOM stdout',
    response(
      `process.stdout.write('\\ufeff' + JSON.stringify({ id: request.id, ok: false, error: { code: 'schema_invalid' } }) + '\\n')`,
    ),
    /JSON/,
  ],
  [
    'wrong shape',
    response(
      `process.stdout.write(JSON.stringify({ id: request.id, ok: false, error: { code: 'schema_invalid' }, unexpected: true }) + '\\n')`,
    ),
    /shape/,
  ],
  [
    'polluted stdout',
    response(
      `process.stdout.write('debug\\n' + JSON.stringify({ id: request.id, ok: true, wire_base64: request.wire_base64 }) + '\\n')`,
    ),
    /stdout frame/,
  ],
  [
    'bad base64',
    response(
      `process.stdout.write(JSON.stringify({ id: request.id, ok: true, wire_base64: '!!!!' }) + '\\n')`,
    ),
    /base64/,
  ],
  [
    'oversized stdout',
    response(`process.stdout.write('x'.repeat(8 * 1024 * 1024 + 2))`),
    /8 MiB/,
  ],
  [
    'oversized stderr',
    response(`process.stderr.write('x'.repeat(64 * 1024 + 1))`),
    /64 KiB/,
  ],
];
for (const [name, code, expected] of faults) {
  test(`infrastructure ${name} fails finitely and cannot pass as a negative fixture`, async () => {
    const runner = startRunner(process.execPath, ['-e', code], {
      timeoutMs: name === 'timeout' ? 200 : 10000,
    });
    try {
      await assert.rejects(
        runner.run('Revision', Buffer.from('"1"'), name),
        expected,
      );
      await assert.rejects(runner.close());
      assert.throws(() => process.kill(runner.pid, 0), { code: 'ESRCH' });
      await assert.rejects(
        runner.run('Revision', Buffer.from('"1"')),
        expected,
      );
    } finally {
      await runner.close().catch(() => {});
    }
  });
}

test('cancellation observes the rejection and reaps the child', async () => {
  const controller = new AbortController();
  const runner = startRunner(process.execPath, ['-e', faults[0][1]], {
    signal: controller.signal,
  });
  const rejected = assert.rejects(
    runner.run('Revision', Buffer.from('"1"')),
    /cancelled/,
  );
  controller.abort();
  await rejected;
  await assert.rejects(runner.close(), /cancelled/);
  assert.throws(() => process.kill(runner.pid, 0), { code: 'ESRCH' });
});

test('only one request is in flight and assertion failure still closes the process', async () => {
  const runner = startRunner(process.execPath, ['-e', echo]);
  await assert.rejects(async () => {
    try {
      const first = runner.run('Revision', Buffer.from('"1"'));
      await assert.rejects(
        runner.run('Revision', Buffer.from('"2"')),
        /in flight/,
      );
      assert.equal((await first).ok, true);
      assert.fail('independent assertion failure');
    } finally {
      await runner.close();
    }
  }, /independent assertion failure/);
  assert.throws(() => process.kill(runner.pid, 0), { code: 'ESRCH' });
});

test('stdin shutdown has a finite fallback for an uncooperative child', async () => {
  const runner = startRunner(process.execPath, [
    '-e',
    echo + '\nsetInterval(() => {}, 1000);',
  ]);
  await runner.run('Revision', Buffer.from('"1"'));
  await assert.rejects(runner.close(), /stdin closed/);
  assert.throws(() => process.kill(runner.pid, 0), { code: 'ESRCH' });
});

test('oversized input is an infrastructure failure without sending a truncated wire', async () => {
  const runner = startRunner(process.execPath, ['-e', echo]);
  await assert.rejects(
    runner.run('Revision', Buffer.alloc(8 * 1024 * 1024)),
    /oversized request/,
  );
  await assert.rejects(runner.close());
  assert.throws(() => process.kill(runner.pid, 0), { code: 'ESRCH' });
});
