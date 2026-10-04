// Exercise the public two-version generator without changing the frozen source.
import {
  mkdtempSync,
  readFileSync,
  writeFileSync,
  mkdirSync,
  rmSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';
import assert from 'node:assert/strict';
const generator = resolve('scripts/generate.mjs');
const original = JSON.parse(
  readFileSync('contract/schema/1.1.0/values.json', 'utf8'),
);
const inventory = JSON.parse(
  readFileSync('contract/schema/1.1.0/methods.json', 'utf8'),
);
function generate(mutate = () => {}) {
  const dir = mkdtempSync(join(tmpdir(), 'lerna-generator-v1_1-'));
  try {
    writeFileSync(
      join(dir, '.prettierrc.json'),
      readFileSync('.prettierrc.json'),
    );
    for (const version of ['1.0.0', '1.1.0']) {
      mkdirSync(join(dir, 'contract/schema', version), { recursive: true });
      for (const name of ['values.json', 'methods.json'])
        writeFileSync(
          join(dir, 'contract/schema', version, name),
          readFileSync(`contract/schema/${version}/${name}`),
        );
    }
    const schema = structuredClone(original),
      methods = structuredClone(inventory);
    mutate(schema, methods);
    writeFileSync(
      join(dir, 'contract/schema/1.1.0/values.json'),
      JSON.stringify(schema),
    );
    writeFileSync(
      join(dir, 'contract/schema/1.1.0/methods.json'),
      JSON.stringify(methods),
    );
    const result = spawnSync(process.execPath, [generator], {
      cwd: dir,
      encoding: 'utf8',
      timeout: 30000,
    });
    assert.ifError(result.error);
    if (result.status !== 0) return { error: result.stderr };
    for (const path of [
      'contract/gen/go/values.go',
      'contract/values.go',
      'conformance/component/valuerunner/types.go',
      'sdk/typescript/src/generated/values.ts',
    ])
      assert.equal(
        readFileSync(join(dir, path), 'utf8'),
        readFileSync(path, 'utf8'),
        `1.0 output changed: ${path}`,
      );
    const output = readFileSync(
      join(dir, 'contract/gen/go/v1_1/values.go'),
      'utf8',
    );
    return {
      digests: [
        ...output.matchAll(
          /(?:Input|Output)SchemaDigest: "(sha256:[0-9a-f]{64})"/g,
        ),
      ].map((m) => m[1]),
      output,
    };
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}
const baseline = generate();
assert.equal(baseline.error, undefined);
const goldens = JSON.parse(
  readFileSync('conformance/fixtures/1.1.0/schema-digests.json', 'utf8'),
);
assert.deepEqual(
  baseline.digests.slice(0, 8),
  goldens.map((x) => x.digest),
);
for (const [name, mutate, expected] of [
  [
    'unknown advertisement field',
    (_s, m) => (m.methods[1].future = true),
    /invalid method registration/,
  ],
  [
    'nonboolean availability',
    (_s, m) => (m.methods[1].advertised = 'true'),
    /invalid method registration/,
  ],
  [
    'unknown nested proposal keyword',
    (s) => (s.$defs.ProposalAction.uniqueItems = true),
    /unsupported schema keyword/,
  ],
  [
    'open decision input',
    (s) => (s.$defs.DecisionDecidePayload.additionalProperties = true),
    /object must be closed/,
  ],
  [
    'duplicate method',
    (_s, m) => m.methods.push(structuredClone(m.methods[1])),
    /duplicate method registration/,
  ],
  [
    'missing unadvertised output',
    (_s, m) => (m.methods[1].output_schema = 'Missing'),
    /unresolved method schema/,
  ],
]) {
  const changed = generate(mutate);
  assert.match(changed.error, expected, name);
}
for (const [name, mutate, changedIndexes] of [
  [
    'reachable new common definition',
    (s) => (s.$defs.ID.pattern = '^[A-Z]{1,128}$'),
    [0, 1, 2, 3, 4, 5, 6, 7],
  ],
  [
    'deep proposal output branch',
    (s) => (s.$defs.ProposalAction.properties.purpose.maxLength = 1000),
    [5],
  ],
  [
    'new limit input rule',
    (s) => (s.$defs.DecisionLimits.properties.max_actions.pattern = '^[0-3]$'),
    [2, 5],
  ],
  [
    'unreachable schema',
    (s) => (s.$defs.Amount.description = 'New explanation'),
    [2, 5],
  ],
]) {
  const changed = generate(mutate);
  assert.equal(changed.error, undefined, name);
  for (let i = 0; i < 8; i++)
    assert.equal(
      changed.digests[i] !== baseline.digests[i],
      changedIndexes.includes(i),
      `${name} root ${i}`,
    );
}
const complete = generate((_s, m) =>
  m.methods
    .filter((x) => x.input_schema.startsWith('Decision'))
    .forEach((x) => (x.advertised = true)),
);
assert.equal(complete.error, undefined);
assert.equal(complete.digests.length, 16);
assert.deepEqual(complete.digests.slice(0, 8), baseline.digests.slice(0, 8));
console.log(
  '1.1.0: 6 generation refusals, 4 reachable digest changes, independent goldens, explicit advertisement and frozen 1.0 outputs passed.',
);
