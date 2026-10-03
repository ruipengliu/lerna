// Public generation command must fail closed rather than publish ambiguous contracts.
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
  readFileSync('contract/schema/1.0.0/values.json', 'utf8'),
);
const inventory = readFileSync('contract/schema/1.0.0/methods.json', 'utf8');
const probes = [
  [
    'missing output schema',
    (_schema, methods) => {
      methods.methods[0].output_schema = 'Missing';
    },
    /unresolved method schema/,
  ],
  [
    'inventory unknown field',
    (_schema, methods) => {
      methods.methods[0].future = true;
    },
    /invalid method registration/,
  ],
  [
    'inventory duplicate',
    (_schema, methods) => {
      methods.methods.push(structuredClone(methods.methods[0]));
    },
    /duplicate method registration/,
  ],
  [
    'open output branch',
    (schema) => {
      schema.$defs.CommandGetResponseFound.properties.escape = {
        $ref: '#/$defs/CommandPayload',
      };
    },
    /closed payload schema/,
  ],
  [
    'unsafe schema integer',
    (schema) => {
      schema.$defs.CommandGetPayload.maxProperties = 9007199254740992;
    },
    /unsafe schema number/,
  ],
  [
    'union without unique discriminant',
    (schema) => {
      schema.$defs.CommandReceiptApplied.properties.state.const = 'accepted';
      schema.$defs.CommandReceiptRejected.properties.state.const = 'accepted';
    },
    /unique required string discriminant/,
  ],
  [
    'union inline structural escape',
    (schema) => {
      schema.$defs.CommandReceipt.oneOf[0] = {
        type: 'object',
        properties: {},
        required: [],
        additionalProperties: false,
      };
    },
    /named local refs/,
  ],

  [
    'unknown semantic keyword',
    (schema) => {
      schema.$defs.ID.unevaluatedProperties = false;
    },
    /unsupported schema keyword/,
  ],
  [
    'dynamic object outside exact exception',
    (schema) => {
      schema.$defs.OtherPayload = {
        type: 'object',
        additionalProperties: true,
        maxProperties: 1024,
      };
    },
    /object must be closed/,
  ],
  [
    'exception without property bound',
    (schema) => {
      delete schema.$defs.CommandPayload.maxProperties;
    },
    /object must be closed/,
  ],
  [
    'duplicate registration',
    (schema) => {
      schema.$defs.DuplicateGet = structuredClone(
        schema.$defs.CommandGetRequest,
      );
    },
    /duplicate method registration/,
  ],
  [
    'registered open payload',
    (schema) => {
      schema.$defs.CommandGetRequest.properties.payload.$ref =
        '#/$defs/CommandPayload';
    },
    /closed payload schema/,
  ],
  [
    'registered nested open payload',
    (schema) => {
      schema.$defs.CommandGetPayload.properties.escape = {
        $ref: '#/$defs/CommandPayload',
      };
    },
    /closed payload schema/,
  ],
];
for (const [name, mutate, expected] of probes) {
  const dir = mkdtempSync(join(tmpdir(), 'lerna-generator-'));
  try {
    const schema = structuredClone(original);
    const changedInventory = JSON.parse(inventory);
    mutate(schema, changedInventory);
    mkdirSync(join(dir, 'contract/schema/1.0.0'), { recursive: true });
    writeFileSync(
      join(dir, 'contract/schema/1.0.0/values.json'),
      JSON.stringify(schema),
    );
    writeFileSync(
      join(dir, 'contract/schema/1.0.0/methods.json'),
      JSON.stringify(changedInventory),
    );
    const result = spawnSync('node', [generator], {
      cwd: dir,
      encoding: 'utf8',
      timeout: 10000,
    });
    assert.ifError(result.error);
    assert.notEqual(result.status, 0, name);
    assert.match(result.stderr, expected, name);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}
console.log(`${probes.length} generation refusal probes passed.`);

// Exercise the real public generation command on valid changed source. These
// observations guard complete input/output and common-definition fingerprints.
const digestPair = (changed, methods = JSON.parse(inventory)) => {
  const dir = mkdtempSync(join(tmpdir(), 'lerna-schema-change-'));
  try {
    mkdirSync(join(dir, 'contract/schema/1.0.0'), { recursive: true });
    writeFileSync(
      join(dir, 'contract/schema/1.0.0/values.json'),
      JSON.stringify(changed),
    );
    writeFileSync(
      join(dir, 'contract/schema/1.0.0/methods.json'),
      JSON.stringify(methods),
    );
    const result = spawnSync('node', [generator], {
      cwd: dir,
      encoding: 'utf8',
      timeout: 30000,
    });
    assert.ifError(result.error);
    assert.equal(result.status, 0, result.stderr);
    const output = readFileSync(join(dir, 'contract/gen/go/values.go'), 'utf8');
    const input = /InputSchemaDigest: "(sha256:[0-9a-f]{64})"/.exec(output);
    const response = /OutputSchemaDigest: "(sha256:[0-9a-f]{64})"/.exec(output);
    assert.ok(input);
    assert.ok(response);
    return [input[1], response[1]];
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
};
const baseline = digestPair(original);
const goldens = JSON.parse(
  readFileSync('conformance/fixtures/1.0.0/schema-digests.json', 'utf8'),
);
assert.deepEqual(
  baseline,
  goldens.map((golden) => golden.digest),
);
const reorder = (value) =>
  Array.isArray(value)
    ? value.map(reorder)
    : value && typeof value === 'object'
      ? Object.fromEntries(
          Object.keys(value)
            .reverse()
            .map((key) => [key, reorder(value[key])]),
        )
      : value;
assert.deepEqual(
  digestPair(reorder(original)),
  baseline,
  'JSON object ordering and formatting are not contract changes',
);
for (const [name, mutate, affected] of [
  [
    'root constraint',
    (s) =>
      (s.$defs.CommandGetRequest.properties.command_id = {
        type: 'string',
        pattern: '^different$',
      }),
    [true, false],
  ],
  [
    'reachable common definition',
    (s) => (s.$defs.ID.pattern = '^[A-Z]{1,128}$'),
    [true, true],
  ],
  [
    'deep output union branch',
    (s) => s.$defs.CommandProgressTask.properties.status.enum.pop(),
    [false, true],
  ],
  [
    'optional field addition',
    (s) =>
      (s.$defs.CommandGetPayload.properties.optional_new = { type: 'boolean' }),
    [true, false],
  ],
  [
    'unreachable value definition',
    (s) => (s.$defs.Amount.properties.unit.maxLength = 63),
    [false, false],
  ],
  [
    'unlisted request remains unsupported',
    (s) => {
      s.$defs.FutureRequest = structuredClone(s.$defs.CommandGetRequest);
      s.$defs.FutureRequest.properties.method.const = 'task.submit';
    },
    [false, false],
  ],
]) {
  const changed = structuredClone(original);
  mutate(changed);
  const got = digestPair(changed);
  for (let direction = 0; direction < 2; direction++)
    assert.equal(
      got[direction] !== baseline[direction],
      affected[direction],
      name,
    );
}
console.log('8 schema generation golden/change scenarios passed.');
