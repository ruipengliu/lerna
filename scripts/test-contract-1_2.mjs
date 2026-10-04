// Exact version-specific conformance; no arbitrary version registry.
import { mkdtempSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { requireBuild } from './bounded-build.mjs';
import { startRunner } from './contract-runner.mjs';
import { ownConformanceScope } from './conformance-ownership.mjs';
import assert from 'node:assert/strict';
const dir = mkdtempSync(join(tmpdir(), 'lerna-content-contract-'));
const scope = ownConformanceScope(dir, 'contract');
const controller = new AbortController();
const interrupt = () => controller.abort();
process.once('SIGINT', interrupt);
process.once('SIGTERM', interrupt);
const deadline = setTimeout(interrupt, 50000);
let runners = [],
  failure;
try {
  const executable = join(dir, 'go-values');
  let startedPID;
  try {
    await requireBuild(
      'go',
      ['build', '-o', executable, './conformance/component/valuerunner_v1_2'],
      {
        timeout: 60000,
        signal: controller.signal,
        stdio: 'inherit',
        onStart: (pid) => {
          startedPID = pid;
          scope.start(pid, 'compiler');
        },
      },
    );
    scope.exited(startedPID, 'compiler', true);
  } catch (error) {
    scope.exited(startedPID, 'compiler', error.exitConfirmed === true);
    throw error;
  }
  let fixtures = JSON.parse(
    readFileSync('conformance/fixtures/1.2.0/fixtures.json', 'utf8'),
  );
  if (process.argv.includes('--reverse')) fixtures.reverse();
  const go = startRunner(executable, ['--batch'], {
    signal: controller.signal,
  });
  runners.push(go);
  scope.start(go.pid, 'producer');
  const ts = startRunner(
    process.execPath,
    ['sdk/typescript/src/v1_2/valuerunner.ts', '--batch'],
    { signal: controller.signal },
  );
  runners.push(ts);
  scope.start(ts.pid, 'producer');
  for (const fixture of fixtures) {
    for (const [lang, runner, other] of [
      ['go', go, ts],
      ['ts', ts, go],
    ]) {
      const first = await runner.run(
        fixture.schema,
        Buffer.concat([
          Buffer.from(' '.repeat(fixture.leading_spaces ?? 0)),
          fixture.wire_base64
            ? Buffer.from(fixture.wire_base64, 'base64')
            : Buffer.from(fixture.wire),
        ]),
        `${lang}: ${fixture.name}`,
      );
      assert.equal(
        first.ok,
        fixture.valid,
        `${lang}: ${fixture.name}: ${JSON.stringify(first.error)}`,
      );
      if (!fixture.valid) {
        if (fixture.code)
          assert.deepEqual(
            first.error,
            { code: fixture.code },
            `${lang}: ${fixture.name}`,
          );
        continue;
      }
      const second = await other.run(
        fixture.schema,
        first.wire,
        `${lang}: ${fixture.name} cross-roundtrip`,
      );
      assert.equal(
        second.ok,
        true,
        `${lang} cross-roundtrip ${fixture.name}: ${JSON.stringify(second.error)}`,
      );
      assert.deepEqual(second.wire, first.wire);
      assert.deepEqual(
        JSON.parse(first.wire.toString('utf8')),
        JSON.parse(fixture.wire),
      );
    }
  }
  console.log(
    `${fixtures.length} exact 1.2 shared fixtures ${process.argv.includes('--reverse') ? 'reverse' : 'forward'}: real Go↔TS canonical roundtrips`,
  );
} catch (error) {
  failure = error;
} finally {
  const results = await Promise.allSettled(
    runners.map((runner) => runner.close()),
  );
  clearTimeout(deadline);
  process.removeListener('SIGINT', interrupt);
  process.removeListener('SIGTERM', interrupt);
  const errors = results
    .filter((result) => result.status === 'rejected')
    .map((result) => result.reason);
  for (const runner of runners)
    scope.exited(runner.pid, 'producer', runner.exitConfirmed);
  scope.finish(failure, errors);
}
