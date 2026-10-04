import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
  decode,
  encode,
  decodeDecide,
  decisionInputDigest,
  commandDigest,
  declaredMethods,
  schema,
  ContractError,
  type Values,
} from './index.ts';
import { decode as decodeOld } from '../index.ts';
const fixtures = new URL(
  '../../../../conformance/fixtures/1.1.0/',
  import.meta.url,
);
test('fixed input has independent command and decision digest goldens', async () => {
  const raw: unknown = JSON.parse(
    readFileSync(new URL('digest-input.json', fixtures), 'utf8'),
  );
  assert.ok(
    raw &&
      typeof raw === 'object' &&
      'request' in raw &&
      'subject' in raw &&
      'decision_digest' in raw &&
      'command_digest' in raw,
  );
  const request = decodeDecide(JSON.stringify(raw.request));
  const subject = decode('SubjectBinding', JSON.stringify(raw.subject));
  assert.equal(
    await decisionInputDigest(request, subject),
    raw.decision_digest,
  );
  assert.equal(
    await commandDigest(
      encode('DecisionDecideRequest', request),
      encode('SubjectBinding', subject),
    ),
    raw.command_digest,
  );
  const repeated = {
    ...request,
    command_id: 'another-command',
    accept_before: '2026-10-04T00:00:09.000000Z',
  };
  assert.equal(
    await decisionInputDigest(repeated, subject),
    raw.decision_digest,
  );
  assert.notEqual(
    await decisionInputDigest(request, {
      ...subject,
      subject_id: 'different-subject',
    }),
    raw.decision_digest,
  );
});
test('complete declared digests and schemas remain isolated', () => {
  assert.equal(declaredMethods.length, 4);
  const goldens: unknown = JSON.parse(
    readFileSync(new URL('schema-digests.json', fixtures), 'utf8'),
  );
  assert.ok(Array.isArray(goldens));
  declaredMethods.forEach((method, i) => {
    assert.equal(method.input_schema_digest, goldens[2 * i].digest);
    assert.equal(method.output_schema_digest, goldens[2 * i + 1].digest);
  });
  assert.throws(() => decodeOld('ErrorCode', '"decision_mismatch"'));
  assert.equal(decode('ErrorCode', '"decision_mismatch"'), 'decision_mismatch');
  assert.ok(Object.isFrozen(schema.$defs.Proposal.properties.advance));
});

test('cancelled before Input has exact zero observations from shared bytes', () => {
  const raw: unknown = JSON.parse(
    readFileSync(new URL('fixtures.json', fixtures), 'utf8'),
  );
  assert.ok(Array.isArray(raw));
  const selected: unknown[] = raw.filter((fixture: unknown) => {
    assert.ok(fixture && typeof fixture === 'object' && 'name' in fixture);
    return (
      fixture.name === 'cancel before decide no fake snapshot' ||
      fixture.name === 'current stop outside frozen cancelled Decision' ||
      (typeof fixture.name === 'string' &&
        fixture.name.startsWith('pre-input cancellation cannot invent '))
    );
  });
  assert.equal(selected.length, 5);
  for (const fixture of selected) {
    assert.ok(fixture && typeof fixture === 'object');
    assert.ok('name' in fixture && typeof fixture.name === 'string');
    assert.ok(
      'schema' in fixture &&
        (fixture.schema === 'Decision' ||
          fixture.schema === 'DecisionGetResponse'),
    );
    assert.ok('wire' in fixture && typeof fixture.wire === 'string');
    assert.ok('valid' in fixture && typeof fixture.valid === 'boolean');
    const roundtrip = () => {
      let decision: Values['Decision'];
      if (fixture.schema === 'Decision') {
        decision = decode('Decision', fixture.wire);
      } else {
        const response = decode('DecisionGetResponse', fixture.wire);
        assert.equal(response.status, 'found');
        if (response.status !== 'found')
          throw Error('missing controlled Decision');
        decision = response.decision;
      }
      return decode('Decision', encode('Decision', decision));
    };
    if (!fixture.valid) {
      assert.throws(
        roundtrip,
        (cause) =>
          cause instanceof ContractError && cause.code === 'schema_invalid',
        fixture.name,
      );
      continue;
    }
    const closed = roundtrip();
    assert.equal(closed.status, 'cancelled');
    if (closed.status !== 'cancelled') throw Error('missing cancellation');
    assert.equal(closed.input, undefined);
    for (const quantity of [
      closed.usage.input_bytes,
      closed.usage.output_bytes,
      closed.usage.rule_steps,
      closed.usage.rule_starts,
      closed.usage.model_requests,
      closed.usage.cost.integer_value,
    ])
      assert.equal(quantity, '0');
    assert.equal(closed.usage.measurements_complete, true);
  }
});
