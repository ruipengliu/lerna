import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { decode, encode, schema, validate, type Values } from './index.ts';
function name(value: string): value is keyof Values {
  return Object.hasOwn(schema.$defs, value);
}
test('shared closed responses and exact bindings', () => {
  const fixtures: Array<{
    name: string;
    schema: string;
    wire: string;
    valid: boolean;
  }> = JSON.parse(
    readFileSync(
      new URL(
        '../../../conformance/fixtures/1.0.0/responses.json',
        import.meta.url,
      ),
      'utf8',
    ),
  );
  for (const f of fixtures) {
    if (!name(f.schema)) throw new Error(f.schema);
    assert.equal(validate(f.schema, JSON.parse(f.wire)), f.valid, f.name);
    if (f.valid)
      assert.deepEqual(
        JSON.parse(encode(f.schema, decode(f.schema, f.wire))),
        JSON.parse(f.wire),
      );
    else assert.throws(() => decode(f.schema, f.wire), f.name);
  }
});

test('bound result encoding and decoding reject another original command', async () => {
  const { encodeCommandResponse, decodeCommandResponse } = await import(
    './index.ts'
  );
  const original = {
    owner: { tenant_id: 't', owner_id: 'o' },
    command_id: 'c',
  };
  const result = { status: 'gone', command_ref: original } as const;
  const wire = encodeCommandResponse(result, original);
  assert.equal(decodeCommandResponse(wire, original).status, 'gone');
  const wrong = { ...original, command_id: 'other' };
  assert.throws(() => encodeCommandResponse(result, wrong), {
    code: 'schema_invalid',
  });
  assert.throws(() => decodeCommandResponse(wire, wrong), {
    code: 'schema_invalid',
  });
});
