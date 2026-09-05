// SHA-256, synchronously, over a Uint8Array.
//
// The browser already has SHA-256 in crypto.subtle, and it is not usable
// here: subtle.digest is asynchronous, returning a promise per call, and this
// runs a few hundred thousand times to solve one challenge. A promise per
// attempt is not an inner loop.
//
// So it is transcribed rather than invented — FIPS 180-4, the ordinary
// reference implementation. Its test is comparison against Go's
// crypto/sha256 over a corpus, not cleverness. Nothing in here should ever
// be optimised in a way that makes it disagree with the Go verifier by even
// one byte: a disagreement is a signup that silently fails for a real person
// with no error anyone can read.

const K = new Uint32Array([
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1,
  0x923f82a4, 0xab1c5ed5, 0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3,
  0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174, 0xe49b69c1, 0xefbe4786,
  0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147,
  0x06ca6351, 0x14292967, 0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
  0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85, 0xa2bfe8a1, 0xa81a664b,
  0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a,
  0x5b9cca4f, 0x682e6ff3, 0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
  0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
]);

// Reused across calls. The solver runs this hundreds of thousands of times
// in a tight loop, and allocating two arrays per attempt is most of the cost.
const W = new Uint32Array(64);
const H = new Uint32Array(8);
const OUT = new Uint8Array(32);

function rotr(x, n) {
  return (x >>> n) | (x << (32 - n));
}

// digest hashes len bytes of buf and writes 32 bytes into OUT, which it
// returns. The result is only valid until the next call — the caller reads it
// immediately, which is all this solver ever does.
export function sha256(buf, len) {
  H[0] = 0x6a09e667; H[1] = 0xbb67ae85; H[2] = 0x3c6ef372; H[3] = 0xa54ff53a;
  H[4] = 0x510e527f; H[5] = 0x9b05688c; H[6] = 0x1f83d9ab; H[7] = 0x5be0cd19;

  // One padded copy: the message, a 0x80 byte, zeroes, then the length in
  // bits as a big-endian 64-bit integer.
  const withLen = len + 9;
  const total = withLen + ((64 - (withLen % 64)) % 64);
  const m = new Uint8Array(total);
  m.set(buf.subarray(0, len));
  m[len] = 0x80;

  const bits = len * 8;
  // Lengths here are far below 2^32 bits, so the high word is always zero.
  m[total - 4] = (bits >>> 24) & 0xff;
  m[total - 3] = (bits >>> 16) & 0xff;
  m[total - 2] = (bits >>> 8) & 0xff;
  m[total - 1] = bits & 0xff;

  for (let off = 0; off < total; off += 64) {
    for (let i = 0; i < 16; i++) {
      const j = off + i * 4;
      W[i] = (m[j] << 24) | (m[j + 1] << 16) | (m[j + 2] << 8) | m[j + 3];
    }
    for (let i = 16; i < 64; i++) {
      const s0 = rotr(W[i - 15], 7) ^ rotr(W[i - 15], 18) ^ (W[i - 15] >>> 3);
      const s1 = rotr(W[i - 2], 17) ^ rotr(W[i - 2], 19) ^ (W[i - 2] >>> 10);
      W[i] = (W[i - 16] + s0 + W[i - 7] + s1) | 0;
    }

    let a = H[0], b = H[1], c = H[2], d = H[3];
    let e = H[4], f = H[5], g = H[6], h = H[7];

    for (let i = 0; i < 64; i++) {
      const S1 = rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25);
      const ch = (e & f) ^ (~e & g);
      const t1 = (h + S1 + ch + K[i] + W[i]) | 0;
      const S0 = rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22);
      const maj = (a & b) ^ (a & c) ^ (b & c);
      const t2 = (S0 + maj) | 0;

      h = g; g = f; f = e; e = (d + t1) | 0;
      d = c; c = b; b = a; a = (t1 + t2) | 0;
    }

    H[0] = (H[0] + a) | 0; H[1] = (H[1] + b) | 0;
    H[2] = (H[2] + c) | 0; H[3] = (H[3] + d) | 0;
    H[4] = (H[4] + e) | 0; H[5] = (H[5] + f) | 0;
    H[6] = (H[6] + g) | 0; H[7] = (H[7] + h) | 0;
  }

  for (let i = 0; i < 8; i++) {
    OUT[i * 4] = (H[i] >>> 24) & 0xff;
    OUT[i * 4 + 1] = (H[i] >>> 16) & 0xff;
    OUT[i * 4 + 2] = (H[i] >>> 8) & 0xff;
    OUT[i * 4 + 3] = H[i] & 0xff;
  }
  return OUT;
}

// leadingZeroBits counts zero bits from the most significant end, the same
// way the Go verifier does.
export function leadingZeroBits(sum) {
  let n = 0;
  for (let i = 0; i < sum.length; i++) {
    const b = sum[i];
    if (b === 0) {
      n += 8;
      continue;
    }
    return n + Math.clz32(b) - 24;
  }
  return n;
}

// hex is used only by tests, to compare a digest against Go's.
export function hex(bytes) {
  let s = "";
  for (let i = 0; i < bytes.length; i++) {
    s += bytes[i].toString(16).padStart(2, "0");
  }
  return s;
}
