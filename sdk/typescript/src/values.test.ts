import { test } from 'node:test';
import { strict as assert } from 'node:assert';
import { decode, encode } from './index.ts';
test('SDK retains an exact revision beyond JS safe integer range', () => {
  const value = decode('Revision', '"9223372036854775807"');
  assert.equal(encode('Revision', value), '"9223372036854775807"');
});

import { parseJSON } from './index.ts';
test('raw boundary preserves valid Unicode, rejects duplicate names and invalid UTF-8', () => {
  assert.deepEqual(
    Object.keys(parseJSON('{"__proto__":"safe","text":"👩‍💻 汉字"}') as object),
    ['__proto__', 'text'],
  );
  for (const wire of [
    '{"a":true,"\\u0061":false}',
    '{"text":"\\ud800"}',
    '{"text":"\\udfff"}',
    '{} {}',
    '{"n":1}',
    '\ufeff{}',
  ])
    assert.throws(() => parseJSON(wire), wire);
  assert.throws(() => parseJSON(new Uint8Array([34, 255, 34])));
});

import { readFileSync } from 'node:fs';
import { type Values } from './index.ts';
test('SDK passes common schema acceptance and exact-value fixtures', () => {
  const fixtures: Array<{
    name: string;
    schema: keyof Values;
    wire: string;
    valid: boolean;
  }> = JSON.parse(
    readFileSync(
      new URL(
        '../../../conformance/fixtures/1.0.0/values.json',
        import.meta.url,
      ),
      'utf8',
    ),
  );
  for (const fixture of fixtures) {
    if (fixture.valid) {
      const value = decode(fixture.schema, fixture.wire);
      assert.deepEqual(
        decode(fixture.schema, encode(fixture.schema, value)),
        value,
        fixture.name,
      );
    } else
      assert.throws(() => decode(fixture.schema, fixture.wire), fixture.name);
  }
});
test('SDK encoding rejects isolated surrogate rather than replacing it', () => {
  const view = decode(
    'CollectionView',
    '{"items":[],"cursor":"next","exhausted":false,"partial":false,"gaps":[],"read_scope":{"owner":{"tenant_id":"t1","owner_id":"o1"},"object_type":"task"},"watermark":"1"}',
  );
  view.cursor = '\ud800';
  assert.throws(() => encode('CollectionView', view));
});
test('raw wire accepts exact bounds and rejects byte and nesting overflow', () => {
  assert.equal(parseJSON(' '.repeat(1048572) + 'null'), null);
  assert.throws(() => parseJSON(' '.repeat(1048573) + 'null'));
  assert.doesNotThrow(() =>
    parseJSON('['.repeat(64) + 'null' + ']'.repeat(64)),
  );
  assert.throws(() => parseJSON('['.repeat(65) + 'null' + ']'.repeat(65)));
});
