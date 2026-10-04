import { mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { execFileSync } from 'node:child_process';
import { startRunner } from './contract-runner.mjs';
import assert from 'node:assert/strict';
const dir = mkdtempSync(join(tmpdir(), 'lerna-contract-'));
const controller = new AbortController();
const interrupt = () => {
  process.exitCode = 130;
  controller.abort();
};
process.once('SIGINT', interrupt);
process.once('SIGTERM', interrupt);
let runners = [],
  failure;
try {
  const executable = join(dir, 'go-values');
  execFileSync(
    'go',
    ['build', '-o', executable, './conformance/component/valuerunner'],
    { stdio: 'inherit', timeout: 60000 },
  );
  const fixtures = ['values', 'commands', 'responses'].flatMap((name) =>
    JSON.parse(readFileSync(`conformance/fixtures/1.0.0/${name}.json`, 'utf8')),
  );
  if (process.argv.includes('--reverse')) fixtures.reverse();
  const go = startRunner(executable, ['--batch'], {
    signal: controller.signal,
  });
  runners.push(go);
  const ts = startRunner(
    process.execPath,
    ['sdk/typescript/src/valuerunner.ts', '--batch'],
    { signal: controller.signal },
  );
  runners.push(ts);
  const run = (lang, name, wire, context) =>
    (lang === 'go' ? go : ts).run(
      name,
      Buffer.from(wire),
      `${lang}: ${context}`,
    );
  for (const fixture of fixtures) {
    for (const lang of ['go', 'ts']) {
      const first = await run(
        lang,
        fixture.schema,
        ' '.repeat(fixture.leading_spaces ?? 0) + fixture.wire,
        `${fixture.name} first`,
      );
      if (!fixture.valid) {
        assert.equal(first.ok, false, `${lang}: ${fixture.name}`);
        if (fixture.code)
          assert.deepEqual(
            first.error,
            { code: fixture.code },
            `${lang} classified refusal: ${fixture.name}`,
          );
        continue;
      }
      assert.equal(
        first.ok,
        true,
        `${lang}: ${fixture.name}: ${JSON.stringify(first.error)}`,
      );
      const second = await run(
        lang === 'go' ? 'ts' : 'go',
        fixture.schema,
        first.wire,
        `${fixture.name} cross-roundtrip`,
      );
      assert.equal(
        second.ok,
        true,
        `${lang} cross-roundtrip: ${fixture.name}: ${JSON.stringify(second.error)}`,
      );
      assert.deepEqual(
        JSON.parse(first.wire.toString('utf8')),
        JSON.parse(fixture.wire),
        `${lang} exact values: ${fixture.name}`,
      );
      assert.deepEqual(
        JSON.parse(second.wire.toString('utf8')),
        JSON.parse(fixture.wire),
        `${lang} cross-language exact values: ${fixture.name}`,
      );
    }
  }
  await Promise.all(runners.map((runner) => runner.close()));
  runners = [];
  // Shared independent goldens test the public digest in each implementation.
  execFileSync('go', ['test', './conformance/component', '-run', 'Digest'], {
    stdio: 'inherit',
    timeout: 60000,
  });
  execFileSync('node', ['--test', 'sdk/typescript/src/digests.test.ts'], {
    stdio: 'inherit',
    timeout: 60000,
  });
  // The same authenticated query scenarios run through each public entry point.
  execFileSync(
    'go',
    ['test', './conformance/component', '-run', 'AuthenticatedQuery'],
    {
      stdio: 'inherit',
      timeout: 60000,
    },
  );
  execFileSync('node', ['--test', 'sdk/typescript/src/query.test.ts'], {
    stdio: 'inherit',
    timeout: 60000,
  });
  execFileSync(
    'go',
    ['test', './conformance/component', '-run', 'Negotiation'],
    { stdio: 'inherit', timeout: 60000 },
  );
  execFileSync('node', ['--test', 'sdk/typescript/src/negotiation.test.ts'], {
    stdio: 'inherit',
    timeout: 60000,
  });
  execFileSync(
    process.execPath,
    [
      'scripts/test-contract-1_1.mjs',
      ...(process.argv.includes('--reverse') ? ['--reverse'] : []),
    ],
    { stdio: 'inherit', timeout: 60000 },
  );
  console.log(
    `${fixtures.length} shared fixtures (${process.argv.includes('--reverse') ? 'reverse' : 'forward'} order) passed; all positive values completed real Go→TS and TS→Go roundtrips.`,
  );
} catch (error) {
  failure = error;
  throw error;
} finally {
  try {
    const results = await Promise.allSettled(
      runners.map((runner) => runner.close()),
    );
    const errors = results
      .filter((result) => result.status === 'rejected')
      .map((result) => result.reason);
    if (!failure && errors.length)
      throw new AggregateError(errors, 'runner cleanup failed');
  } finally {
    process.removeListener('SIGINT', interrupt);
    process.removeListener('SIGTERM', interrupt);
    rmSync(dir, { recursive: true, force: true });
  }
}
