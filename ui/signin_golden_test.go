package ui

import (
	flags "flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/auth"
)

// flags, not flag: the browser build has a func named flag.
var updateSigninGolden = flags.Bool("update-signin-golden", false,
	"rewrite testdata/signin-golden.html from the signin partial")

// signinGolden is every sign-in state the partial drew before the
// one-way option existed, rendered with that option off. Equipo and
// every other app on the shipped screen depend on these bytes, so the
// file was written by the code before the option and only ever compared
// against since: a change that moves one byte of today's screen fails
// here, not in somebody's app.
const signinGolden = "testdata/signin-golden.html"

// powValue is a challenge's values and its nonce, fresh on every render:
// the one part of the screen that is meant to differ.
var powValue = regexp.MustCompile(
	`((?:name="pow_[a-z]+" value|data-pow-nonce)=")[^"]*"`)

func TestSigninWithTheOptionOffIsByteForByteUnchanged(t *testing.T) {
	got := renderSigninGolden(t)
	if *updateSigninGolden {
		if err := os.WriteFile(signinGolden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(signinGolden)
	if err != nil {
		t.Fatal(err)
	}
	if got == string(want) {
		return
	}
	wantCases := strings.Split(string(want), "\n<!-- case ")
	gotCases := strings.Split(got, "\n<!-- case ")
	for at := range min(len(wantCases), len(gotCases)) {
		if wantCases[at] != gotCases[at] {
			t.Fatalf("the screen with the one-way option off changed:\n"+
				"want:\n%s\ngot:\n%s", wantCases[at], gotCases[at])
		}
	}
	t.Fatalf("%d cases, want %d", len(gotCases), len(wantCases))
}

// renderSigninGolden draws every named state four ways: bare, with a
// return_to, with a challenge, and as the gallery's Preview.
func renderSigninGolden(t *testing.T) string {
	t.Helper()
	guard := proofGuard(t, 12)
	var page strings.Builder
	for _, state := range signinStates() {
		for _, variant := range []string{"bare", "return", "proof", "preview"} {
			st := state.st
			data := signinData(st)
			switch variant {
			case "return":
				st.ReturnTo = "/docs/7?tab=a&b=c"
				data = signinData(st)
			case "proof":
				form := guard.Form(time.Unix(1_800_000_000, 0),
					auth.ProofScope)
				st.Proof = &form
				data = signinData(st)
			case "preview":
				data["Preview"] = true
			}
			out := render(t, "signin", data) + "\n" +
				render(t, "signin-title", data)
			fmt.Fprintf(&page, "\n<!-- case %s, %s -->\n%s\n",
				state.name, variant,
				powValue.ReplaceAllString(out, `${1}…"`))
		}
	}
	return page.String()
}
