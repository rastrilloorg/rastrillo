// Node WebCrypto signs and checks root calls independently of the Go code.
// The fixture is synthetic. Generate once; ECDSA signatures are randomized:
//   node rootcall/testdata/twin.mjs generate
import { webcrypto, createHash } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
const { subtle } = webcrypto;
const context = Buffer.from('rastrillo/rootcall/v1\0');
const bodyHash = (body) =>
  createHash('sha256').update(Buffer.from(body)).digest('base64url');
const curve = { name: 'ECDSA', namedCurve: 'P-256' };
const ecdsa = { name: 'ECDSA', hash: 'SHA-256' };

async function generate() {
  const keys = await subtle.generateKey(curve, true, ['sign', 'verify']);
  const request = {
    method: 'POST',
    target: '/_carlos/slopbox/plan?box=b%2F1',
    body: '{"plan":"paid","seq":3}',
  };
  const payload = JSON.stringify({
    v: 1, iss: 'https://root.example.com', aud: 'https://box1.example.net',
    kid: 'key_1', jti: 'AAECAwQFBgcICQoLDA0ODw', iat: 1700000000,
    exp: 1700000060, method: request.method, target: request.target,
    body: bodyHash(request.body),
  });
  const signature = await subtle.sign(
    ecdsa, keys.privateKey, Buffer.concat([context, Buffer.from(payload)]));
  const publicKey = await subtle.exportKey('raw', keys.publicKey);
  const token = Buffer.from(payload).toString('base64url') + '.' +
    Buffer.from(signature).toString('base64url');
  writeFileSync(new URL('./golden.json', import.meta.url), JSON.stringify({
    now: 1700000000,
    publicKey: Buffer.from(publicKey).toString('base64url'),
    request, payload, token,
  }, null, 2) + '\n');
}

// verify reads {token, publicKey, body} and exits 1 unless the signature
// holds under the root-call context and the signed hash is the body's.
async function verify() {
  const input = JSON.parse(readFileSync(0, 'utf8'));
  const [payload, signature] = input.token.split('.');
  const key = await subtle.importKey('raw',
    Buffer.from(input.publicKey, 'base64url'), curve, false, ['verify']);
  const signed = Buffer.from(payload, 'base64url');
  const valid = await subtle.verify(ecdsa, key,
    Buffer.from(signature, 'base64url'), Buffer.concat([context, signed]));
  if (!valid) process.exit(1);
  if (JSON.parse(signed).body !== bodyHash(input.body)) process.exit(1);
}

if (process.argv[2] === 'generate') await generate();
else if (process.argv[2] === 'verify') await verify();
else process.exit(2);
