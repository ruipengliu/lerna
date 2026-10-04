// Exact version-specific conformance; no arbitrary version registry.
import {
  mkdtempSync,
  readFileSync,
  rmSync,
  openSync,
  closeSync,
  fsyncSync,
  writeSync,
  statSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname, isAbsolute } from 'node:path';
import { requireBuild } from './bounded-build.mjs';
import { startRunner } from './contract-runner.mjs';
import assert from 'node:assert/strict';
const dir = mkdtempSync(join(tmpdir(), 'lerna-content-contract-'));
const identity = statSync(dir);
const directory = openSync(dir, 'r'),
  parent = openSync(dirname(dir), 'r');
try {
  fsyncSync(directory);
  fsyncSync(parent);
} finally {
  closeSync(directory);
  closeSync(parent);
}
const registry =
  process.env.LERNA_TEST_OWNED_SCOPE_REGISTRY ?? join(dir, 'owned-scopes.log');
if (!isAbsolute(registry))
  throw Error('absolute owned contract registry required');
function record(line) {
  const ledger = openSync(registry, 'a', 0o600);
  try {
    writeSync(ledger, line + '\n');
    fsyncSync(ledger);
  } finally {
    closeSync(ledger);
  }
  const fd = openSync(dirname(registry), 'r');
  try {
    fsyncSync(fd);
  } finally {
    closeSync(fd);
  }
}
record(`contract ${dir} ${identity.dev} ${identity.ino}`);
function processGroup(pid) {
  const stat = readFileSync(`/proc/${pid}/stat`, 'utf8');
  return Number(stat.slice(stat.lastIndexOf(')') + 2).split(' ')[2]);
}
const controller = new AbortController();
const interrupt = () => controller.abort();
process.once('SIGINT', interrupt);
process.once('SIGTERM', interrupt);
const deadline = setTimeout(interrupt, 50000);
let runners = [],
  failure,
  buildsConfirmed = true;
try {
  const executable = join(dir, 'go-values');
  try {
    await requireBuild(
      'go',
      ['build', '-o', executable, './conformance/component/valuerunner_v1_2'],
      {
        timeout: 60000,
        signal: controller.signal,
        stdio: 'inherit',
        onStart: (pid) => {
          const group = processGroup(pid);
          if (group !== pid) throw Error('compiler native group mismatch');
          record(`process_group ${pid} ${group} ${dir}`);
        },
      },
    );
  } catch (error) {
    buildsConfirmed = error.exitConfirmed === true;
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
  record(`producer ${go.pid} ${processGroup(go.pid)} ${dir}`);
  const ts = startRunner(
    process.execPath,
    ['sdk/typescript/src/v1_2/valuerunner.ts', '--batch'],
    { signal: controller.signal },
  );
  runners.push(ts);
  record(`producer ${ts.pid} ${processGroup(ts.pid)} ${dir}`);
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
  const errors = [
    failure,
    ...results
      .filter((result) => result.status === 'rejected')
      .map((result) => result.reason),
  ].filter(Boolean);
  for (const runner of runners)
    record(`producer_exit ${runner.pid} closed=${runner.exitConfirmed}`);
  if (buildsConfirmed && runners.every((runner) => runner.exitConfirmed)) {
    const current = statSync(dir);
    if (current.dev !== identity.dev || current.ino !== identity.ino)
      errors.push(Error('owned contract scope identity changed'));
    else {
      rmSync(dir, { recursive: true });
      if (registry !== join(dir, 'owned-scopes.log'))
        record(`contract_removed ${dir}`);
    }
  } else
    errors.push(
      Error(
        `native compiler/producer close unconfirmed; exact scope retained ${dir}`,
      ),
    );
  if (errors.length)
    throw new AggregateError(errors, 'exact 1.2 conformance failed');
}
