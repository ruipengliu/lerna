import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
  decode,
  negotiate,
  supportedMethods,
  ContractError,
  type MethodSupport,
  type NegotiationRequest,
} from './index.ts';

function completedMethods(): MethodSupport[] {
  const raw: unknown = JSON.parse(
    readFileSync(
      new URL(
        '../../../../conformance/fixtures/1.1.0/schema-digests.json',
        import.meta.url,
      ),
      'utf8',
    ),
  );
  assert.ok(Array.isArray(raw));
  const names = [
    ['command', 'command.get', 'CommandGetRequest', 'CommandGetResponse'],
    [
      'decision_engine',
      'decision_engine.decide',
      'DecisionDecideRequest',
      'CommandReceipt',
    ],
    [
      'decision_engine',
      'decision_engine.get',
      'DecisionGetRequest',
      'DecisionGetResponse',
    ],
    [
      'decision_engine',
      'decision_engine.cancel',
      'DecisionCancelRequest',
      'CommandReceipt',
    ],
  ];
  assert.equal(raw.length, 2 * names.length);
  return names.map(([profile, method, input, output], i) => {
    const pair: unknown[] = [raw[2 * i], raw[2 * i + 1]];
    const digests = pair.map((golden, j) => {
      assert.ok(
        golden &&
          typeof golden === 'object' &&
          'schema' in golden &&
          'digest' in golden,
      );
      assert.equal(golden.schema, j === 0 ? input : output);
      assert.equal(typeof golden.digest, 'string');
      return decode('SchemaDigest', JSON.stringify(golden.digest));
    });
    // Literal contract identities + independent goldens, not advertised metadata.
    return {
      ...decode(
        'MethodSupport',
        JSON.stringify({
          contract_version: '1.1.0',
          profile,
          method,
          input_schema: input,
          output_schema: output,
          input_schema_digest: digests[0],
          output_schema_digest: digests[1],
        }),
      ),
    };
  });
}

function requestFor(method: MethodSupport): NegotiationRequest {
  return {
    contract_version: method.contract_version,
    profile: method.profile,
    method: method.method,
    input_schema_digest: method.input_schema_digest,
    output_schema_digest: method.output_schema_digest,
  };
}

test('all four completed methods negotiate their exact profile and both goldens', () => {
  const expected = completedMethods();
  assert.equal(supportedMethods.length, expected.length);
  for (const method of expected) {
    const matches = supportedMethods.filter(
      (entry) => entry.method === method.method,
    );
    assert.deepEqual(matches, [method]);
    assert.deepEqual(negotiate(JSON.stringify(requestFor(method))), method);
  }
});

test('completed negotiation refuses inexact version, profile, either digest and absent methods', () => {
  const wrongDigest = 'sha256:' + '0'.repeat(64);
  for (const method of completedMethods()) {
    const original = requestFor(method);
    const cases: { name: string; code: string; request: NegotiationRequest }[] =
      [
        {
          name: 'version',
          code: 'version_unsupported',
          request: { ...original, contract_version: '1.0.0' },
        },
        {
          name: 'profile',
          code: 'unsupported',
          request: {
            ...original,
            profile:
              method.profile === 'command' ? 'decision_engine' : 'command',
          },
        },
        {
          name: 'input_digest',
          code: 'version_unsupported',
          request: { ...original, input_schema_digest: wrongDigest },
        },
        {
          name: 'output_digest',
          code: 'version_unsupported',
          request: { ...original, output_schema_digest: wrongDigest },
        },
        {
          name: 'both_digests',
          code: 'version_unsupported',
          request: {
            ...original,
            input_schema_digest: wrongDigest,
            output_schema_digest: wrongDigest,
          },
        },
        {
          name: 'missing_method',
          code: 'unsupported',
          request: { ...original, method: 'decision_engine.unknown' },
        },
      ];
    for (const invalid of cases) {
      assert.throws(
        () => negotiate(JSON.stringify(invalid.request)),
        (cause) =>
          cause instanceof ContractError && cause.code === invalid.code,
        `${method.method}/${invalid.name}`,
      );
    }
  }
});
