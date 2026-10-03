import { mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { execFileSync, spawnSync } from 'node:child_process';
import assert from 'node:assert/strict';
const dir = mkdtempSync(join(tmpdir(), 'lerna-contract-'));
try {
  const executable = join(dir, 'go-values');
  execFileSync(
    'go',
    ['build', '-o', executable, './conformance/component/valuerunner'],
    { stdio: 'inherit' },
  );
  const fixtures = ['values', 'commands', 'responses'].flatMap((name) =>
    JSON.parse(readFileSync(`conformance/fixtures/1.0.0/${name}.json`, 'utf8')),
  );
  const run = (lang, name, wire) =>
    spawnSync(
      lang === 'go' ? executable : 'node',
      lang === 'go' ? [name] : ['sdk/typescript/src/valuerunner.ts', name],
      { input: wire, encoding: 'utf8', timeout: 10000, maxBuffer: 2_097_152 },
    );
  for (const fixture of fixtures) {
    for (const lang of ['go', 'ts']) {
      const first = run(
        lang,
        fixture.schema,
        ' '.repeat(fixture.leading_spaces ?? 0) + fixture.wire,
      );
      assert.ifError(first.error);
      if (!fixture.valid) {
        assert.notEqual(first.status, 0, `${lang}: ${fixture.name}`);
        if (fixture.code)
          assert.deepEqual(
            JSON.parse(first.stderr),
            { code: fixture.code },
            `${lang} classified refusal: ${fixture.name}`,
          );
        continue;
      }
      assert.equal(
        first.status,
        0,
        `${lang}: ${fixture.name}: ${first.stderr}`,
      );
      const second = run(
        lang === 'go' ? 'ts' : 'go',
        fixture.schema,
        first.stdout,
      );
      assert.ifError(second.error);
      assert.equal(
        second.status,
        0,
        `${lang} cross-roundtrip: ${fixture.name}: ${second.stderr}`,
      );
      assert.deepEqual(
        JSON.parse(first.stdout),
        JSON.parse(fixture.wire),
        `${lang} exact values: ${fixture.name}`,
      );
      assert.deepEqual(
        JSON.parse(second.stdout),
        JSON.parse(fixture.wire),
        `${lang} cross-language exact values: ${fixture.name}`,
      );
    }
  }
  // Shared independent goldens test the public digest in each implementation.
  execFileSync('go', ['test', './conformance/component', '-run', 'Digest'], {
    stdio: 'inherit',
  });
  execFileSync('node', ['--test', 'sdk/typescript/src/digests.test.ts'], {
    stdio: 'inherit',
  });
  // The same authenticated query scenarios run through each public entry point.
  execFileSync(
    'go',
    ['test', './conformance/component', '-run', 'AuthenticatedQuery'],
    {
      stdio: 'inherit',
    },
  );
  execFileSync('node', ['--test', 'sdk/typescript/src/query.test.ts'], {
    stdio: 'inherit',
  });
  execFileSync(
    'go',
    ['test', './conformance/component', '-run', 'Negotiation'],
    { stdio: 'inherit' },
  );
  execFileSync('node', ['--test', 'sdk/typescript/src/negotiation.test.ts'], {
    stdio: 'inherit',
  });
  console.log(
    `${fixtures.length} shared fixtures passed; all positive values completed real Go→TS and TS→Go roundtrips.`,
  );
} finally {
  rmSync(dir, { recursive: true, force: true });
}
