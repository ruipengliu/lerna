import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { execFileSync, spawnSync } from 'node:child_process';
import { decode, encode } from './index.ts';

test('compact body at 1 MiB retains Unicode through Go and TS in both directions', () => {
  const prefix =
    '{"contract_version":"1.0.0","profile":"command","method":"fixture.write","command_id":"x","target":{"tenant_id":"t","owner_id":"o","kind":"task","id":"x"},"payload":{"text":"';
  const suffix = '"},"accept_before":"2026-10-03T01:00:00.000000Z"}';
  const sample = '<>&\u2028\u2029';
  const literal = '\\u2028\\u2029';
  const escapedLiteral = JSON.stringify(literal).slice(1, -1);
  const room = 1_048_576 - Buffer.byteLength(prefix + escapedLiteral + suffix);
  const text =
    sample.repeat(Math.floor(room / Buffer.byteLength(sample))) +
    'x'.repeat(room % Buffer.byteLength(sample));
  const wire = prefix + text + escapedLiteral + suffix;
  const value = decode('CommandEnvelope', wire);
  const directory = mkdtempSync(join(tmpdir(), 'lerna-codec-boundary-'));
  try {
    const executable = join(directory, 'go-values');
    execFileSync(
      'go',
      ['build', '-o', executable, './conformance/component/valuerunner'],
      {
        cwd: fileURLToPath(new URL('../../../', import.meta.url)),
        timeout: 60_000,
      },
    );
    const go = (input: string) =>
      spawnSync(executable, ['CommandEnvelope'], {
        input,
        encoding: 'utf8',
        timeout: 10_000,
        maxBuffer: 2_097_152,
      });
    for (const input of [wire, encode('CommandEnvelope', value)]) {
      const output = go(input);
      assert.ifError(output.error);
      assert.equal(output.status, 0, output.stderr);
      assert.equal(Buffer.byteLength(output.stdout), 1_048_576);
      assert.equal(
        decode('CommandEnvelope', output.stdout).payload.text,
        text + literal,
      );
      const fromTS = encode(
        'CommandEnvelope',
        decode('CommandEnvelope', output.stdout),
      );
      assert.equal(Buffer.byteLength(fromTS), 1_048_576);
      assert.equal(go(fromTS).status, 0);
    }
    const oversized = prefix + text + 'x' + escapedLiteral + suffix;
    assert.throws(() => decode('CommandEnvelope', oversized));
    assert.throws(() =>
      encode('CommandEnvelope', {
        ...value,
        payload: { text: text + 'x' + literal },
      }),
    );
    assert.notEqual(go(oversized).status, 0);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});
