import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
  decodePut,
  declaredMethods,
  supportedMethods,
  ContractError,
} from './index.ts';
test('canonical body bound keeps exact input_over_limit classification', () => {
  const normal = readFileSync(
    new URL(
      '../../../../conformance/fixtures/1.2.0/put-alpha.json',
      import.meta.url,
    ),
    'utf8',
  );
  const request = decodePut(normal);
  assert.equal(request.payload.bytes_base64, 'YWxwaGEK');
  request.payload.bytes_base64 = Buffer.alloc(262145, 97).toString('base64');
  request.payload.content_ref.byte_length = '262145';
  assert.throws(
    () => decodePut(JSON.stringify(request)),
    (error: unknown) =>
      error instanceof ContractError && error.code === 'input_over_limit',
  );
});
test('version1.2 declares only Content and Command and does not advertise a partial profile', () => {
  assert.deepEqual(declaredMethods.map((method) => method.method).sort(), [
    'command.get',
    'content.get',
    'content.put',
  ]);
  assert.deepEqual(supportedMethods, []);
});

test('independent canonical digest golden preserves trace and binds purpose', async () => {
  const { commandDigest } = await import('./digest.ts');
  const golden = JSON.parse(
    readFileSync(
      new URL(
        '../../../../conformance/fixtures/1.2.0/digests.json',
        import.meta.url,
      ),
      'utf8',
    ),
  ) as { command: Record<string, unknown>; subject: unknown; digest: string };
  const digest = await commandDigest(
    JSON.stringify(golden.command),
    JSON.stringify(golden.subject),
  );
  assert.equal(digest, golden.digest);
  golden.command.trace_context = { trace_id: 'different-connection' };
  assert.equal(
    await commandDigest(
      JSON.stringify(golden.command),
      JSON.stringify(golden.subject),
    ),
    digest,
  );
  const request = decodePut(JSON.stringify(golden.command));
  request.payload.purpose = 'different-purpose';
  assert.notEqual(
    await commandDigest(
      JSON.stringify(request),
      JSON.stringify(golden.subject),
    ),
    digest,
  );
});

test('response binds original full ref and exact same-size range', async () => {
  const { encodeContentResponse, decodeContentResponse } = await import(
    './content.ts'
  );
  const input = decodePut(
    readFileSync(
      new URL(
        '../../../../conformance/fixtures/1.2.0/put-alpha.json',
        import.meta.url,
      ),
      'utf8',
    ),
  );
  const request = {
    content_ref: input.payload.content_ref,
    purpose: 'verification',
    range: { offset: '1', length: '3' },
  };
  const wire = encodeContentResponse(
    {
      status: 'published',
      content_ref: request.content_ref,
      bytes_base64: 'bHBo',
      range: request.range,
    },
    request,
  );
  assert.equal(decodeContentResponse(wire, request).status, 'published');
  for (const wrong of [
    { ...request, content_ref: { ...request.content_ref, version: '2' } },
    {
      ...request,
      content_ref: {
        ...request.content_ref,
        media_type: 'application/octet-stream',
      },
    },
    { ...request, range: { offset: '2', length: '3' } },
    { content_ref: request.content_ref, purpose: request.purpose },
  ])
    assert.throws(() => decodeContentResponse(wire, wrong), ContractError);
});
