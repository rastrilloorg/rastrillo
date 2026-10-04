// Package pow is the front door for a form anyone on the internet can
// post to: a sealed, single-use challenge, a honeypot, and a proof of
// work, with the browser half shipped alongside the Go half that
// verifies it.
//
// The browser half is here because the solver in the page and the
// verifier in Go must build a byte-identical preimage; nothing in a
// copied file enforces that, and a disagreement fails silently for
// some visitors. pow/browser_test.go runs the shipped solver in
// Chromium against this verifier.
//
// The shape:
//
//	g, err := pow.New(pow.Config{InstanceKey: key, Nonces: pow.SQLNonces(db),
//		ScriptURL: assets.Path("pow.js"), WorkerURL: assets.Path("pow-worker.js")})
//	f := g.Form(time.Now(), "apply:41")       // render f.Fields, f.Attrs, f.Script
//	adm := g.Admit(r, pow.Want{Scope: "apply:41"})
//	... validate, then in the handler's transaction:
//	err = adm.Commit(ctx, tx)                 // spends with the business write
//
// Check is Admit and Commit at once, for a handler that redirects after
// every POST. Never cache a page that carries a challenge: one token on
// a shared page is one token for every visitor.
//
// What this does not do: proof of work prices out scripted abuse and
// nothing more. Against a bulk attacker who pays for the solves the
// defence is a persisted budget on what the form spends.
package pow

import (
	"crypto/sha256"
	"math/bits"
	"strings"
)

// DefaultDifficulty is how many leading zero bits a solution must have:
// roughly 262k expected hashes. It suits a form people spend a while
// on, because the solve runs while they type.
//
// Sign-in is different: a returning visitor tapping a one-tap button
// waits for the whole solve. For sign-in use 16 bits, the largest
// difficulty whose p95 stayed at or under one second in Chromium at 6x
// CPU throttle (measure_browser_test.go, 2026-10-04: 16 bits p95 796ms,
// 18 bits p95 1494ms). Measure p95 and p99, never the mean: solve time
// is geometric, so p99 is about 4.6x the average, and a difficulty
// chosen on the average hangs for one visitor in a hundred.
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
// binding is "" for an unbound challenge, the default, or the value of
// the form's [data-pow-binding] input for a Guard with Config.Bind.
// Unbound is the default because the token is single use: one solve
// already buys exactly one submission whatever it carries, and leaving
// the value out of the hash lets the work start at page load instead of
// after the visitor has typed it.
//
// Guard.Admit, Check and Verify call this for you, and powtest uses it
// to solve in tests. Reach for it directly only when you are wiring the
// pieces yourself.
func Verify(nonce, binding, counter string, difficulty int) bool {
	if counter == "" {
		return false
	}
	sum := sha256.Sum256([]byte(preimage(nonce, normalize(binding), counter)))
	return leadingZeroBits(sum[:]) >= difficulty
}
