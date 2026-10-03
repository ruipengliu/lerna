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
const probes = [
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
    mutate(schema);
    mkdirSync(join(dir, 'contract/schema/1.0.0'), { recursive: true });
    writeFileSync(
      join(dir, 'contract/schema/1.0.0/values.json'),
      JSON.stringify(schema),
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
