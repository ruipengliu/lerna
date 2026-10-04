// Mechanical coverage of the shell entry's external Go-tool protocol, not
// Component business or database fault evidence. The real suites run separately.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, writeFileSync, readFileSync, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { boundedBuild } from './bounded-build.mjs';
import { ownConformanceScope } from './conformance-ownership.mjs';

const entry = resolve('scripts/test-component-integration-race.sh');
async function run(
  inventory,
  { discoveryStatus = 0, raceStatus = 0, normal = false } = {},
) {
  const dir = mkdtempSync(join(tmpdir(), 'lerna-component-race-tool-'));
  const scope = ownConformanceScope(dir, 'contract');
  let startedPID, failure;
  try {
    const record = join(dir, 'selections.jsonl');
    writeFileSync(
      join(dir, 'go'),
      `#!${process.execPath}
import { appendFileSync } from 'node:fs';
const args = process.argv.slice(2);
if (args.includes('-list')) {
  process.stdout.write(process.env.LIST_OUTPUT);
  process.exit(Number(process.env.LIST_STATUS));
}
appendFileSync(process.env.SELECTION_RECORD, JSON.stringify(args) + '\\n');
process.exit(Number(process.env.RACE_STATUS));
`,
      { mode: 0o700 },
    );
    const result = await boundedBuild(
      'bash',
      [entry, ...(normal ? ['--normal'] : [])],
      {
        encoding: 'utf8',
        timeout: 5000,
        onStart(pid) {
          startedPID = pid;
          scope.start(pid, 'compiler');
        },
        env: {
          ...process.env,
          PATH: `${dir}:${process.env.PATH}`,
          LIST_OUTPUT: inventory,
          LIST_STATUS: String(discoveryStatus),
          RACE_STATUS: String(raceStatus),
          SELECTION_RECORD: record,
        },
      },
    );
    scope.exited(startedPID, 'compiler', result.exitConfirmed);
    const nativeCauses = [result.error, ...result.cleanupErrors].filter(
      Boolean,
    );
    if (nativeCauses.length)
      throw new AggregateError(nativeCauses, 'original bounded shell cause');
    return {
      ...result,
      selections: existsSync(record)
        ? readFileSync(record, 'utf8').trim().split('\n').map(JSON.parse)
        : [],
    };
  } catch (error) {
    failure = error;
  } finally {
    scope.finish(failure);
  }
}

test('native Go discovery failure preserves its exit status', async () => {
  const result = await run('TestDurableExisting\n', { discoveryStatus: 37 });
  assert.equal(result.status, 37);
  assert.deepEqual(result.selections, []);
});

test('positive partition includes future tests, Unicode, Examples and Fuzz seeds', async () => {
  const names = [
    'TestDurableExisting',
    'TestContentFullClosureIncludesIntermediateVersionsAndExact64Bound',
    'TestContentFuture',
    'TestFuture',
    'TestRésumé',
    'Example',
    'FuzzFuture',
  ];
  const result = await run(
    [
      ...names,
      'BenchmarkSpeed',
      'ok\tgithub.com/ruipengliu/lerna/conformance/component\t0.01s',
    ].join('\n'),
  );
  assert.equal(result.status, 0, result.stderr);
  assert.match(
    result.stdout,
    /7 runnables \(1 closure, 1 content, 1 durable, 4 other\)/,
  );
  const selected = [];
  for (const args of result.selections) {
    for (const flag of [
      '-p=1',
      '-count=1',
      '-race',
      '-tags=integration',
      '-timeout=120s',
    ])
      assert.ok(args.includes(flag), flag);
    const selector = args[args.indexOf('-run') + 1];
    const matches = names.filter((name) => new RegExp(selector).test(name));
    selected.push(...matches);
    assert.ok(!new RegExp(selector).test('TestFutureExtra'));
    assert.ok(!new RegExp(selector).test('PrefixTestFuture'));
  }
  assert.deepEqual(selected.sort(), [...names].sort());
  assert.equal(new Set(selected).size, names.length);
});

test('absent durable group never becomes an empty selector that runs all tests', async () => {
  const result = await run('Example\nFuzzFuture\n');
  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /empty durable group/);
  assert.deepEqual(
    result.selections.map((args) => args[args.indexOf('-run') + 1]),
    ['^(Example|FuzzFuture)$'],
  );
});

test('duplicate, empty and ambiguous inventories fail closed', async () => {
  for (const inventory of [
    '',
    'TestFuture\nTestFuture\n',
    'TestFuture unexpected text\n',
  ]) {
    const result = await run(inventory);
    assert.notEqual(result.status, 0);
    assert.deepEqual(result.selections, []);
  }
});

test('a native race-group failure remains a failure', async () => {
  const result = await run('TestDurableExisting\nTestFuture\n', {
    raceStatus: 41,
  });
  assert.equal(result.status, 41);
  assert.match(result.stdout, /running durable/);
  assert.doesNotMatch(result.stdout, /running other/);
});

test('normal entry uses the same complete positive partition without race', async () => {
  const names = [
    'TestContentFullClosureIncludesIntermediateVersionsAndExact64Bound',
    'TestContentFuture',
    'TestDurableFuture',
    'FuzzFuture',
  ];
  const result = await run(names.join('\n'), { normal: true });
  assert.equal(result.status, 0, result.stderr);
  assert.equal(result.selections.length, 4);
  const selected = [];
  for (const args of result.selections) {
    assert.ok(!args.includes('-race'));
    for (const flag of [
      '-p=1',
      '-count=1',
      '-tags=integration',
      '-timeout=120s',
    ])
      assert.ok(args.includes(flag), flag);
    const selector = args[args.indexOf('-run') + 1];
    selected.push(...names.filter((name) => new RegExp(selector).test(name)));
  }
  assert.deepEqual(selected.sort(), names.sort());
});

test('Content partitions remain finite and include every discovered case once', async () => {
  const names = ['TestContentAlpha', 'TestContentBeta', 'TestContentGamma'];
  const result = await run(names.join('\n'));
  assert.equal(result.status, 0, result.stderr);
  assert.equal(result.selections.length, 2);
  const selected = result.selections.flatMap((args) => {
    const selector = args[args.indexOf('-run') + 1];
    return names.filter((name) => new RegExp(selector).test(name));
  });
  assert.deepEqual(selected.sort(), names.sort());
});
