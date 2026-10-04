import {
  mkdtempSync,
  readFileSync,
  rmSync,
  statSync,
  openSync,
  closeSync,
  fsyncSync,
  writeSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname, isAbsolute } from 'node:path';
import { requireBuild } from './bounded-build.mjs';
import { startRunner } from './contract-runner.mjs';
import assert from 'node:assert/strict';
const dir = mkdtempSync(join(tmpdir(), 'lerna-contract-'));

// Exact test-resource ownership; native closes remain the existing runner/build APIs.
const identity = statSync(dir);
let ownershipConfirmed = true;
// One short-lived FD at a time. Preserve action and native Close causes together.
function useFD(path, flags, action) {
  let fd;
  const causes = [];
  try {
    fd = openSync(path, flags, 0o600);
    action(fd);
  } catch (error) {
    causes.push(error);
  }
  if (fd !== undefined) {
    try {
      closeSync(fd);
    } catch (error) {
      causes.push(error);
    }
  }
  if (causes.length) {
    ownershipConfirmed = false;
    throw new AggregateError(
      causes,
      `owned conformance FD unconfirmed: ${path}`,
    );
  }
}
for (const path of [dir, dirname(dir)]) useFD(path, 'r', fsyncSync);
const registry =
  process.env.LERNA_TEST_OWNED_SCOPE_REGISTRY ?? join(dir, 'owned-scopes.log');
if (!isAbsolute(registry))
  throw Error('absolute owned contract registry required');
function record(line) {
  useFD(registry, 'a', (fd) => {
    const bytes = Buffer.from(line + '\n');
    for (let offset = 0; offset < bytes.length; ) {
      const written = writeSync(fd, bytes, offset, bytes.length - offset);
      if (written <= 0) throw Error('ownership ledger short write');
      offset += written;
    }
    fsyncSync(fd);
  });
  useFD(dirname(registry), 'r', fsyncSync);
}
function processGroup(pid) {
  try {
    const stat = readFileSync(`/proc/${pid}/stat`, 'utf8');
    return Number(stat.slice(stat.lastIndexOf(')') + 2).split(' ')[2]);
  } catch (error) {
    ownershipConfirmed = false;
    throw error;
  }
}
record(`contract ${dir} ${identity.dev} ${identity.ino}`);
function ownRunner(runner) {
  allRunners.push(runner);
  record(`producer ${runner.pid} ${processGroup(runner.pid)} ${dir}`);
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
let buildsConfirmed = true;
async function runBuild(command, args, options) {
  try {
    let startedPID;
    await requireBuild(command, args, {
      ...options,
      signal: controller.signal,
      onStart: (pid) => {
        startedPID = pid;
        const group = processGroup(pid);
        if (group !== pid) {
          ownershipConfirmed = false;
          throw Error('native group mismatch');
        }
        record(`process_group ${pid} ${group} ${dir}`);
      },
    });
    record(`process_exit ${startedPID} confirmed=true`);
  } catch (error) {
    buildsConfirmed &&= error.exitConfirmed === true;
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
  const errors = [
    failure,
    ...results
      .filter((result) => result.status === 'rejected')
      .map((result) => result.reason),
  ].filter(Boolean);
  clearTimeout(overallDeadline);
  process.removeListener('SIGINT', interrupt);
  process.removeListener('SIGTERM', interrupt);
  for (const runner of allRunners)
    record(`producer_exit ${runner.pid} closed=${runner.exitConfirmed}`);
  if (
    ownershipConfirmed &&
    buildsConfirmed &&
    allRunners.every((runner) => runner.exitConfirmed)
  ) {
    try {
      const current = statSync(dir);
      if (current.dev !== identity.dev || current.ino !== identity.ino)
        throw Error('owned contract scope identity changed');
      rmSync(dir, { recursive: true });
      if (registry !== join(dir, 'owned-scopes.log'))
        record(`contract_removed ${dir}`);
    } catch (error) {
      errors.push(error);
    }
  } else {
    errors.push(Error(`process exit unconfirmed; retained exact scope ${dir}`));
  }
  if (errors.length)
    throw new AggregateError(
      [...new Set(errors)],
      'contract conformance and cleanup failed',
    );
}
