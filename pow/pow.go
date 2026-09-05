// Package pow is the front door for a form anyone on the internet can
// post to: an address-bound proof of work, a sealed challenge, a
// single-use nonce and a honeypot, with the browser half of the proof
// of work shipped alongside the Go half that verifies it.
//
// It is movement's front door, extracted at the point a second app
// needed it. The extraction is not about saving the lines. The two
// halves of the proof of work have to build a byte-identical preimage,
// nothing in a copied file enforces that, and a disagreement fails
// silently in the browser for a subset of addresses — so the framework,
// which is the only place that can ship both halves and keep them in
// step, is where they belong.
//
// The shape:
//
//	g, err := pow.New(pow.Config{InstanceKey: key, Nonces: pow.SQLNonces(db)})
//	...
//	c := g.Issue(time.Now())         // render: c.Fields(), c.FormAttrs(workerURL)
//	reason, ok := g.Check(r, email)  // on POST, before anything that writes
//
// What this does not do: 2^18 SHA-256 is under a millisecond on a
// commodity GPU. Proof of work prices out casual and scripted abuse and
// nothing more. Against a serious bulk attack the defence is a
// persisted budget on whatever the form spends — mail, rows, money —
// not this.
package pow

import (
	"crypto/sha256"
	"math/bits"
	"strings"
)

// DefaultDifficulty is how many leading zero bits a solution must have:
// roughly 262k expected hashes.
//
// It is an estimate, not a measurement. Re-set it against real
// mid-range hardware before you launch, recording p95 and p99 rather
// than the mean — solve time is geometric, so p99 is about 4.6x the
// average, and calibrating on the average ships a form that hangs for
// one visitor in a hundred.
const DefaultDifficulty = 18

// normalize lowercases ASCII and nothing else.
//
// Go's strings.ToLower and JavaScript's toLowerCase disagree on parts
// of Unicode, and the preimage has to be byte-identical on both sides.
// A disagreement here is a silent ReasonShort that no visitor can
// diagnose and no log explains, so the two implementations are kept
// deliberately dumb and deliberately identical. browser/powcore.js
// carries the twin, and the browser test is what proves they agree.
//
// It runs inside the package, on the binding, on both sides. That is
// what lets a caller pass an email address as the binding without
// having to match Go's case folding to JavaScript's itself — which is
// the mistake this package exists to make unavailable.
func normalize(s string) string {
	b := []byte(strings.TrimSpace(s))
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// preimage is the exact string both sides hash. browser/powcore.js
// builds the same one.
func preimage(nonce, normalizedBinding, counter string) string {
	return nonce + ":" + normalizedBinding + ":" + counter
}

func leadingZeroBits(sum []byte) int {
	n := 0
	for _, b := range sum {
		if b == 0 {
			n += 8
			continue
		}
		return n + bits.LeadingZeros8(b)
	}
	return n
}

// Verify checks one candidate solution. O(1) here, ~2^difficulty for
// whoever had to find it.
//
// binding is whatever the challenge was bound to — the address the form
// submitted, in the usual case. The binding is inside the hash and that
// is the whole point: an unbound challenge is solved once and replayed
// against every address in a list, so it costs an attacker one solve in
// total. Bound, it costs one solve per address, which is the axis a
// bulk signup attack actually scales along. It is also why a form like
// this needs JavaScript at all — the work cannot begin until the
// visitor has typed the value it is bound to.
//
// Guard.Check calls this for you. Reach for it directly only when you
// are wiring the pieces yourself.
func Verify(nonce, binding, counter string, difficulty int) bool {
	if counter == "" {
		return false
	}
	sum := sha256.Sum256([]byte(preimage(nonce, normalize(binding), counter)))
	return leadingZeroBits(sum[:]) >= difficulty
}
