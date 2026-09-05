// The solver itself, separate from the Worker that hosts it.
//
// It is its own module so that the browser test can import and run
// exactly this code. A test that re-implements the loop proves the two
// implementations agree with each other and says nothing about the one
// that ships.
//
// The search: find a counter where SHA-256(nonce ":" binding ":"
// counter) has at least `difficulty` leading zero bits. The binding —
// the address, usually — is inside the hash on purpose: an unbound
// challenge is solved once and replayed against every address in a
// list, so binding it is what makes each one cost its own work, which
// is the axis a bulk signup attack actually scales along.

import { sha256, leadingZeroBits } from "./sha256.js";

const encoder = new TextEncoder();

// asciiLower mirrors Go's normalize exactly: A-Z and nothing else.
//
// String.prototype.toLowerCase is the obvious choice and it is wrong.
// It folds parts of Unicode differently from Go's strings.ToLower, and
// a one-byte disagreement in the preimage produces a solution the
// server rejects as too short — a submission that fails for a real
// person, with no error anybody can read and nothing in the logs to
// explain it.
export function asciiLower(s) {
  let out = "";
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    out += c >= 65 && c <= 90 ? String.fromCharCode(c + 32) : s[i];
  }
  return out;
}

// solve returns the counter as a string, the same shape the form posts.
//
// onProgress, when given, is called every 20,000 attempts. Solve time
// is geometric: the p99 is roughly 4.6x the average, so a visitor on
// old hardware can legitimately wait several times what a laptop
// measures, and a button with no sign of life is a button people give
// up on.
export function solve(nonce, binding, difficulty, onProgress) {
  // The prefix never changes, so it is encoded once and the counter's
  // digits are written after it. Re-encoding the whole string every
  // attempt is most of the cost otherwise.
  const prefix = encoder.encode(nonce + ":" + asciiLower(binding.trim()) + ":");
  const buf = new Uint8Array(prefix.length + 16);
  buf.set(prefix);

  let counter = 0;
  let sinceReport = 0;

  for (;;) {
    let len = prefix.length;
    if (counter === 0) {
      buf[len++] = 48; // "0"
    } else {
      let n = counter;
      let digits = 0;
      while (n > 0) {
        n = (n / 10) | 0;
        digits++;
      }
      n = counter;
      for (let i = digits - 1; i >= 0; i--) {
        buf[len + i] = 48 + (n % 10);
        n = (n / 10) | 0;
      }
      len += digits;
    }

    if (leadingZeroBits(sha256(buf, len)) >= difficulty) {
      return String(counter);
    }

    counter++;
    if (onProgress && ++sinceReport >= 20000) {
      sinceReport = 0;
      onProgress(counter);
    }
  }
}
