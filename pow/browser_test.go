//go:build browser

// The gate the whole scheme rests on.
//
// A Go-only test proves the verifier and nothing else. The failure it
// cannot see is the one that actually happens: the browser and the
// server building a different preimage from the same inputs — a stray
// Unicode case fold, a different separator, a counter formatted another
// way — so every honest visitor's solution is rejected as too short,
// with no error they can read and nothing in the logs to explain it.
// The only way to know the two agree is to run the shipped module in a
// real browser and check its answer here.
//
// This is also the test that makes shipping both halves from one module
// worth something. Vendored into an app, these files could drift from
// the verifier they are checked against and nothing would notice.
//
// Run it with:
//
//	go test -tags browser ./pow/

package pow_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"testing"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/pow"
)

// awaitPromise makes Evaluate wait for the expression's promise instead
// of handing back a pending object. Every script here uses dynamic
// import, which is asynchronous by nature. chromedp exports no such
// option of its own.
func awaitPromise(p *runtime.EvaluateParams) *runtime.EvaluateParams {
	return p.WithAwaitPromise(true)
}

// powRig serves the package's own browser assets and one blank page to
// run them from — nothing else, because nothing else is under test.
func powRig(t *testing.T) *harness.Rig {
	t.Helper()
	return harness.New(t, func(string) http.Handler {
		assets := rastrillo.NewAssets(pow.Assets())
		mux := http.NewServeMux()
		mux.Handle("GET /pow/", http.StripPrefix("/pow/", assets.Handler()))
		mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte("<!doctype html><title>pow</title><main>ready</main>"))
		})
		return mux
	})
}

// browserSolve runs the shipped solver in the page and returns its
// counter.
const browserSolve = `(async () => {
	const m = await import("/pow/powcore.js");
	return m.solve(%q, %q, %d, null);
})()`

// browserDifficulty is low deliberately: the property under test is
// agreement, not cost. A realistic 18 bits would make this take as long
// as a real submission for no extra information.
const browserDifficulty = 12

const testNonce = "0f1e2d3c4b5a69788796a5b4c3d2e1f0"

func TestBrowserSolutionSatisfiesGoVerifier(t *testing.T) {
	rig := powRig(t)
	rig.Run(chromedp.Navigate(rig.Origin + "/"))

	// Mixed case and surrounding space, because the normaliser is
	// exactly where the two sides disagree if they are going to.
	const binding = "  Alice@Example.COM "

	var counter string
	rig.Run(chromedp.Evaluate(
		fmt.Sprintf(browserSolve, testNonce, binding, browserDifficulty),
		&counter, awaitPromise))

	if counter == "" {
		t.Fatal("the browser returned no counter")
	}
	if !pow.Verify(testNonce, binding, counter, browserDifficulty) {
		t.Fatalf("the browser solved %q for %q but Go rejects it: the two sides "+
			"are building different preimages", counter, binding)
	}
}

func TestBrowserSolutionDoesNotTransfer(t *testing.T) {
	// The binding is inside the hash, and this is what that buys. If it
	// were ever dropped — or built differently on the two sides — one
	// solve would buy every address in a list.
	rig := powRig(t)
	rig.Run(chromedp.Navigate(rig.Origin + "/"))

	var counter string
	rig.Run(chromedp.Evaluate(
		fmt.Sprintf(browserSolve, testNonce, "alice@example.com", browserDifficulty),
		&counter, awaitPromise))

	if pow.Verify(testNonce, "bob@example.com", counter, browserDifficulty) {
		t.Fatal("a solution the browser found for one address verified for another")
	}
}

func TestBrowserSHA256MatchesGo(t *testing.T) {
	// The two implementations must agree byte for byte, not merely
	// agree about which counters pass. This catches a divergence
	// directly rather than waiting for one to show up as a rejected
	// submission.
	rig := powRig(t)
	rig.Run(chromedp.Navigate(rig.Origin + "/"))

	inputs := []string{
		"",
		"a",
		"abc",
		"the quick brown fox jumps over the lazy dog",
		// 55, 56 and 64 bytes: the padding boundaries, where a
		// transcribed SHA-256 goes wrong if it is going to.
		"5555555555555555555555555555555555555555555555555555555",
		"66666666666666666666666666666666666666666666666666666666",
		"6666666666666666666666666666666666666666666666666666666666666666",
	}

	const digestJS = `(async () => {
		const m = await import("/pow/sha256.js");
		const b = new TextEncoder().encode(%q);
		return m.hex(m.sha256(b, b.length));
	})()`

	for _, in := range inputs {
		var got string
		rig.Run(chromedp.Evaluate(fmt.Sprintf(digestJS, in), &got, awaitPromise))

		sum := sha256.Sum256([]byte(in))
		if want := hex.EncodeToString(sum[:]); got != want {
			t.Errorf("sha256(%q):\n browser %s\n go      %s", in, got, want)
		}
	}
}

func TestBrowserNormaliserMatchesGo(t *testing.T) {
	// normalize has no exported Go twin, so this reaches it through
	// Verify: if asciiLower and normalize ever disagree on a string,
	// the solution the browser finds for it stops verifying. Checking
	// the fold directly says which string broke, and how.
	rig := powRig(t)
	rig.Run(chromedp.Navigate(rig.Origin + "/"))

	const lowerJS = `(async () => {
		const m = await import("/pow/powcore.js");
		return m.asciiLower(%q);
	})()`

	// Each of these folds differently under a Unicode-aware lowercase,
	// which is why neither side is allowed to use one.
	for _, in := range []string{"İSTANBUL", "ǅ", "STRASSE", "Alice@Example.COM", "ΣΊΣΥΦΟΣ"} {
		var got string
		rig.Run(chromedp.Evaluate(fmt.Sprintf(lowerJS, in), &got, awaitPromise))

		// The Go side, spelled out here rather than imported, so this
		// test fails if either implementation drifts from the rule.
		want := []byte(in)
		for i, c := range want {
			if c >= 'A' && c <= 'Z' {
				want[i] = c + 32
			}
		}
		if got != string(want) {
			t.Errorf("asciiLower(%q) = %q, want %q — the two normalisers disagree",
				in, got, string(want))
		}
	}
}
