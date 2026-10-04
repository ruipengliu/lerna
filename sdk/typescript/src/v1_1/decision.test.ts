import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
  decode,
  encode,
  decodeDecide,
  decisionInputDigest,
  commandDigest,
  supportedMethods,
  declaredMethods,
  schema,
  negotiate,
  ContractError,
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
test('complete declared digests do not advertise unfinished profile and schemas remain isolated', () => {
  assert.equal(supportedMethods.length, 1);
  assert.equal(declaredMethods.length, 4);
  const goldens: unknown = JSON.parse(
    readFileSync(new URL('schema-digests.json', fixtures), 'utf8'),
  );
  assert.ok(Array.isArray(goldens));
  declaredMethods.forEach((method, i) => {
    assert.equal(method.input_schema_digest, goldens[2 * i].digest);
    assert.equal(method.output_schema_digest, goldens[2 * i + 1].digest);
  });
  const method = declaredMethods[1];
  assert.ok(method);
  assert.throws(
    () =>
      negotiate(
        encode('NegotiationRequest', {
          contract_version: '1.1.0',
          profile: 'decision_engine',
          method: 'decision_engine.decide',
          input_schema_digest: method.input_schema_digest,
          output_schema_digest: method.output_schema_digest,
        }),
      ),
    (e) => e instanceof ContractError && e.code === 'unsupported',
  );
  assert.throws(() => decodeOld('ErrorCode', '"decision_mismatch"'));
  assert.equal(decode('ErrorCode', '"decision_mismatch"'), 'decision_mismatch');
  assert.ok(Object.isFrozen(schema.$defs.Proposal.properties.advance));
});
