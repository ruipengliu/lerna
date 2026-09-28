// Offline fixture keys only. Never installs credentials or contacts a service.
import { generateKeyPairSync, createPrivateKey, createPublicKey, sign, verify } from 'node:crypto';
import { readFileSync, writeFileSync, existsSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
const root = dirname(fileURLToPath(import.meta.url));
const keyPath = join(root, 'fixture-signing-key.json');
if (!existsSync(keyPath)) {
  const pair = generateKeyPairSync('ec', { namedCurve: 'P-256' });
  writeFileSync(keyPath, JSON.stringify({
    warning: 'PUBLIC TEST FIXTURE; NOT A DEPLOYMENT CREDENTIAL',
    private: pair.privateKey.export({ format: 'jwk' }),
    public: pair.publicKey.export({ format: 'jwk' })
  }, null, 2) + '\n');
}
const keys = JSON.parse(readFileSync(keyPath));
const input = JSON.parse(readFileSync(0, 'utf8'));
const canonical = value => value && typeof value === 'object'
  ? Array.isArray(value) ? '[' + value.map(canonical).join(',') + ']'
    : '{' + Object.keys(value).sort().map(k => JSON.stringify(k) + ':' + canonical(value[k])).join(',') + '}'
  : JSON.stringify(value);
if (input.mode === 'verify') {
  for (const item of input.items) {
    const [h, p, s] = item.proof.split('.');
    if (Buffer.from(p, 'base64url').toString() !== canonical(item.payload)) throw Error('payload mismatch');
    const header = JSON.parse(Buffer.from(h, 'base64url'));
    if (canonical(header) !== canonical({ alg: 'ES256', typ: 'harness-control+jws', kid: 'scenario-test-only' })) throw Error('header mismatch');
    if (!verify('sha256', Buffer.from(h + '.' + p), { key: createPublicKey({ key: keys.public, format: 'jwk' }), dsaEncoding: 'ieee-p1363' }, Buffer.from(s, 'base64url'))) throw Error('signature mismatch');
  }
  process.stdout.write(JSON.stringify({ verified: input.items.length }));
} else {
  const h = Buffer.from(canonical({ alg: 'ES256', typ: 'harness-control+jws', kid: 'scenario-test-only' })).toString('base64url');
  const p = Buffer.from(canonical(input.payload)).toString('base64url');
  const s = sign('sha256', Buffer.from(h + '.' + p), { key: createPrivateKey({ key: keys.private, format: 'jwk' }), dsaEncoding: 'ieee-p1363' }).toString('base64url');
  process.stdout.write(JSON.stringify({ proof: h + '.' + p + '.' + s, public_key: keys.public }));
}
