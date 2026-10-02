// Verify public, constructed protocol vectors; this is not a service authenticator.
import { createHash, createPublicKey, verify } from 'node:crypto';
import { readFileSync } from 'node:fs';

function scalarString(value) {
  for (let i = 0; i < value.length; i++) {
    const code = value.charCodeAt(i);
    if (code >= 0xd800 && code <= 0xdbff) {
      const next = value.charCodeAt(++i);
      if (!(next >= 0xdc00 && next <= 0xdfff)) throw new Error('invalid Unicode');
    } else if (code >= 0xdc00 && code <= 0xdfff) throw new Error('invalid Unicode');
  }
  return JSON.stringify(value);
}

export function canonical(value) {
  if (value === null || typeof value === 'boolean') return JSON.stringify(value);
  if (typeof value === 'string') return scalarString(value);
  if (typeof value === 'number') {
    if (!Number.isFinite(value) || (Number.isInteger(value) && !Number.isSafeInteger(value))) {
      throw new Error('outside profile numeric range');
    }
    return JSON.stringify(value);
  }
  if (Array.isArray(value)) return '[' + value.map(canonical).join(',') + ']';
  if (typeof value === 'object') {
    return '{' + Object.keys(value).sort().map(k => scalarString(k) + ':' + canonical(value[k])).join(',') + '}';
  }
  throw new Error('not a JSON value');
}

if (process.argv.includes('--canonical')) {
  // The Python caller rejects duplicate keys and unsafe integers before this bridge.
  process.stdout.write(canonical(JSON.parse(readFileSync(0, 'utf8'))));
} else {
  const fixturePath = new URL('../../../contracts/examples/transport/proofs.json', import.meta.url);
  const fixtures = JSON.parse(readFileSync(fixturePath, 'utf8'));
  function check(item) {
    const [header64, payload64, sig64, extra] = item.proof.split('.');
    if (extra !== undefined || !header64 || !payload64 || !sig64) return false;
    let header, payload;
    try {
      header = JSON.parse(Buffer.from(header64, 'base64url'));
      payload = JSON.parse(Buffer.from(payload64, 'base64url'));
    } catch { return false; }
    if (canonical(header) !== canonical({ alg: 'ES256', typ: item.expected_type, kid: item.expected_kid })) return false;
    if (canonical(payload) !== canonical(item.expected_payload)) return false;
    if (Buffer.from(payload64, 'base64url').toString('utf8') !== canonical(payload)) return false;
    const signature = Buffer.from(sig64, 'base64url');
    if (signature.length !== 64) return false;
    const key = createPublicKey({ key: item.public_key, format: 'jwk' });
    return verify('sha256', Buffer.from(header64 + '.' + payload64),
      { key, dsaEncoding: 'ieee-p1363' }, signature);
  }
  let rejected = 0;
  for (const item of fixtures) {
    if (!check(item)) throw new Error(item.name + ': invalid positive signature vector');
    const tampered = structuredClone(item);
    const sig = Buffer.from(item.proof.split('.')[2], 'base64url');
    sig[0] ^= 1;
    tampered.proof = item.proof.split('.').slice(0, 2).join('.') + '.' + sig.toString('base64url');
    if (check(tampered)) throw new Error('tampered signature accepted');
    rejected++;
    const wrongKid = structuredClone(item); wrongKid.expected_kid += '-wrong';
    if (check(wrongKid)) throw new Error('wrong pinned key accepted');
    rejected++;
    const wrongType = structuredClone(item); wrongType.expected_type = 'another-profile+jws';
    if (check(wrongType)) throw new Error('wrong proof purpose accepted');
    rejected++;
    for (const key of Object.keys(item.expected_payload)) {
      const changed = structuredClone(item);
      changed.expected_payload[key] = 'different-bound-value';
      if (check(changed)) throw new Error('changed bound field accepted: ' + key);
      rejected++;
    }
  }
  const canonicalCases = [
    [{ z: 1, a: '\u20ac', nested: { b: 2, a: 1 } }, '{"a":"€","nested":{"a":1,"b":2},"z":1}'],
    [{ '\ue000': 1, '😀': 2 }, '{"😀":2,"":1}'],
    [[1e-7, 1e-6, -0], '[1e-7,0.000001,0]'],
  ];
  for (const [input, expected] of canonicalCases) {
    if (canonical(input) !== expected) throw new Error('JCS vector mismatch');
  }
  for (const value of ['\ud800', Infinity, 9007199254740992]) {
    let failed = false;
    try { canonical(value); } catch { failed = true; }
    if (!failed) throw new Error('invalid canonical input accepted');
    rejected++;
  }
  console.log(`PASS: ${fixtures.length} ES256 public vectors; ${canonicalCases.length} canonical vectors; ${rejected} negative checks`);
  console.log('Scope: constructed bytes, signatures and pinned bindings; no live identity, key custody, revocation or interoperability tested');
}
