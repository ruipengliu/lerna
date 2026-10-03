import { test } from 'node:test';
import { strict as assert } from 'node:assert';
import { readFileSync } from 'node:fs';
import { commandDigest, ContractError, decodeCommand } from './index.ts';
const fixtures: Array<{
  name: string;
  wire: string;
  subject_wire: string;
  digest: string;
  valid: boolean;
  code?: string;
  body_repeat_count?: number;
}> = JSON.parse(
  readFileSync(
    new URL(
      '../../../conformance/fixtures/1.0.0/digests.json',
      import.meta.url,
    ),
    'utf8',
  ),
);
test('Application digest matches the independently hashed UTF-8 literal', async () => {
  for (const f of fixtures) {
    const wire = f.body_repeat_count
      ? f.wire.replace('@B@', 'x'.repeat(f.body_repeat_count))
      : f.wire;
    if (f.valid)
      assert.equal(await commandDigest(wire, f.subject_wire), f.digest, f.name);
    else
      await assert.rejects(
        commandDigest(wire, f.subject_wire),
        (error: unknown) =>
          error instanceof ContractError && error.code === f.code,
        f.name,
      );
  }
});

test('digest fixture success never registers its future method for execution', () => {
  const fixture = fixtures[0];
  assert.ok(fixture);
  assert.throws(
    () => decodeCommand(fixture.wire),
    (error: unknown) =>
      error instanceof ContractError && error.code === 'unsupported',
  );
});
