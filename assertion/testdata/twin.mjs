// Node WebCrypto is independent of the framework's Go signing implementation.
// The fixture is synthetic. Generate once; ECDSA signatures are randomized.
import { webcrypto } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
const { subtle } = webcrypto;
const context = Buffer.from('rastrillo/assertion/v1\0');
if (process.argv[2] === 'generate') {
 const keys = await subtle.generateKey({name:'ECDSA',namedCurve:'P-256'}, true, ['sign','verify']);
 const payload = readFileSync(new URL('./payload.json',import.meta.url),'utf8');
 const signature = await subtle.sign({name:'ECDSA',hash:'SHA-256'}, keys.privateKey, Buffer.concat([context,Buffer.from(payload)]));
 const publicKey = await subtle.exportKey('raw',keys.publicKey);
 writeFileSync(new URL('./golden.json',import.meta.url),JSON.stringify({payload,publicKey:Buffer.from(publicKey).toString('base64url'),signature:Buffer.from(signature).toString('base64url')},null,2)+'\n');
} else if (process.argv[2] === 'verify') {
 const input=JSON.parse(readFileSync(0,'utf8')); const [payload,sig]=input.token.split('.');
 const key=await subtle.importKey('raw',Buffer.from(input.publicKey,'base64url'),{name:'ECDSA',namedCurve:'P-256'},false,['verify']);
 if(!await subtle.verify({name:'ECDSA',hash:'SHA-256'},key,Buffer.from(sig,'base64url'),Buffer.concat([context,Buffer.from(payload,'base64url')]))) process.exit(1);
} else { process.exit(2); }
