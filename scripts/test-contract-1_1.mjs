import { mkdtempSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { requireBuild } from './bounded-build.mjs';
import { startRunner } from './contract-runner.mjs';
import { ownConformanceScope } from './conformance-ownership.mjs';
import assert from 'node:assert/strict';
const dir = mkdtempSync(join(tmpdir(), 'lerna-contract-'));

// Exact test-resource ownership; native closes remain the existing runner/build APIs.
const scope = ownConformanceScope(dir, 'contract');
function ownRunner(runner) {
  allRunners.push(runner);
  scope.start(runner.pid, 'producer');
}

const controller = new AbortController();
const interrupt = () => {
  process.exitCode = 130;
  controller.abort();
};
process.once('SIGINT', interrupt);
process.once('SIGTERM', interrupt);
// The invoking contract entry has a 60s bound; cancel and confirm holders
// inside that bound instead of letting its parent kill an active build.
const overallDeadline = setTimeout(() => controller.abort(), 50000);
let allRunners = [],
  runners = [],
  failure;
async function runBuild(command, args, options) {
  let startedPID;
  try {
    await requireBuild(command, args, {
      ...options,
      signal: controller.signal,
      onStart: (pid) => {
        startedPID = pid;
        scope.start(pid, 'compiler');
      },
    });
    scope.exited(startedPID, 'compiler', true);
  } catch (error) {
    scope.exited(startedPID, 'compiler', error.exitConfirmed === true);
    throw error;
  }
}
try {
  const executable = join(dir, 'go-values');
  await runBuild(
    'go',
    ['build', '-o', executable, './conformance/component/valuerunner_v1_1'],
    { stdio: 'inherit', timeout: 60000 },
  );
  let fixtures = JSON.parse(
    readFileSync('conformance/fixtures/1.1.0/fixtures.json', 'utf8'),
  );
  if (process.argv.includes('--usage-only'))
    fixtures = fixtures.filter((fixture) => fixture.name.startsWith('usage '));
  if (process.argv.includes('--reverse')) fixtures.reverse();
  const go = startRunner(executable, ['--batch'], {
    signal: controller.signal,
  });
  runners.push(go);
  ownRunner(go);
  const ts = startRunner(
    process.execPath,
    ['sdk/typescript/src/v1_1/valuerunner.ts', '--batch'],
    { signal: controller.signal },
  );
  runners.push(ts);
  ownRunner(ts);
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
        Buffer.concat([
          Buffer.from(' '.repeat(fixture.leading_spaces ?? 0)),
          fixture.wire_base64
            ? Buffer.from(fixture.wire_base64, 'base64')
            : Buffer.from(fixture.wire),
        ]),
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
        second.wire,
        first.wire,
        `${lang} preserved canonical raw bytes: ${fixture.name}`,
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
  await runBuild(
    'go',
    [
      'test',
      '-p=1',
      '-count=1',
      '-timeout=120s',
      './conformance/component',
      '-run',
      'FixedDecision|DecisionDigest|AuthenticatedQuery11',
    ],
    { stdio: 'inherit', timeout: 60000 },
  );
  await runBuild(
    process.execPath,
    [
      '--test',
      'sdk/typescript/src/v1_1/decision.test.ts',
      'sdk/typescript/src/v1_1/query.test.ts',
    ],
    { stdio: 'inherit', timeout: 60000 },
  );
  console.log(
    `${fixtures.length} shared fixtures (${process.argv.includes('--reverse') ? 'reverse' : 'forward'} order) passed; all positive values completed real Go→TS and TS→Go roundtrips.`,
  );
} catch (error) {
  failure = error;
} finally {
  const results = await Promise.allSettled(
    allRunners.map((runner) => runner.close()),
  );
  clearTimeout(overallDeadline);
  process.removeListener('SIGINT', interrupt);
  process.removeListener('SIGTERM', interrupt);
  const errors = results
    .filter((result) => result.status === 'rejected')
    .map((result) => result.reason);
  for (const runner of allRunners)
    scope.exited(runner.pid, 'producer', runner.exitConfirmed);
  scope.finish(failure, errors);
}
