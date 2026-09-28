package passkey_test

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"amadan.net/rastrillo/rastrillo/nodetest"
	"amadan.net/rastrillo/rastrillo/passkey"
)

type outcome struct {
	Navigate string `json:"navigate,omitempty"`
	Message  string `json:"message,omitempty"`
}

func runSigninJS(t *testing.T, destinations []any) (map[string]outcome, map[string][]string, []string, []bool) {
	t.Helper()
	in, err := json.Marshal(map[string]any{"origin": "https://app.test", "destinations": destinations})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Outcomes     map[string]outcome  `json:"outcomes"`
		FinishKeys   map[string][]string `json:"finishKeys"`
		Destinations []string            `json:"destinations"`
		LocalPaths   []bool              `json:"localPaths"`
	}
	if err := json.Unmarshal(nodetest.Run(t, nodetest.Cmd{Args: []string{"js/signin_node.mjs"}, Stdin: in}), &got); err != nil {
		t.Fatal(err)
	}
	return got.Outcomes, got.FinishKeys, got.Destinations, got.LocalPaths
}

// Every branch a real ceremony can end in. The rule the table holds: the
// page navigates only when finish answered ok:true, and then only to a
// safe destination; everything else is a message and focus stays put.
func TestThePasskeyDoorNavigatesOnlyAfterASuccess(t *testing.T) {
	outcomes, keys, _, _ := runSigninJS(t, nil)
	want := map[string]outcome{
		"signed in, local destination":    {Navigate: "/home"},
		"held for a second factor":        {Navigate: "/signin/confirm"},
		"signed in, destination off-site": {Navigate: "/"},
		"signed in, no destination":       {Navigate: "/"},
		"finish says ok false":            {Message: "failed"},
		"finish has no ok":                {Message: "failed"},
		"finish answers null":             {Message: "failed"},
		"begin not 2xx":                   {Message: "failed"},
		"begin without a challenge":       {Message: "failed"},
		"finish not 2xx":                  {Message: "failed"},
		"the network is down":             {Message: "failed"},
		"dismissed, or no passkey":        {Message: "cancelled"},
		"the authenticator failed":        {Message: "failed"},
	}
	if !reflect.DeepEqual(outcomes, want) {
		t.Fatalf("outcomes:\n got %v\nwant %v", outcomes, want)
	}
	if got := keys["signed in, local destination"]; !reflect.DeepEqual(got, []string{"authenticatorData", "clientDataJSON", "id", "signature"}) {
		t.Fatalf("finish carried %v; it sends the assertion as id and never the PRF output, which is a client-side secret", got)
	}
}

// §1.6 step 4: the server's "to" is followed only when it is a local
// absolute path by sessions.SafeReturn's rule AND resolves to this
// origin. A same-origin absolute URL is refused too: the server never
// sends one. ui/rastrillo.js keeps its own copy of the local-path rule
// for Rastrillo-Location; its contract test pins that copy's text.
func TestThePasskeyDoorNavigatesOnlyToALocalPath(t *testing.T) {
	cases := []any{
		"//evil.example/x", "/\t/evil.example", "/\n/evil.example", "/\\evil.example",
		"https://evil.example/x", "https://app.test/home",
		"/", "/home", "/confirm?x=1",
		nil, 42, "",
	}
	want := []string{"/", "/", "/", "/", "/", "/", "/", "/home", "/confirm?x=1", "/", "/", "/"}
	if _, _, got, _ := runSigninJS(t, cases); !reflect.DeepEqual(got, want) {
		t.Fatalf("safeNext:\n got %q\nwant %q", got, want)
	}
}

// The spec calls localPath out by name as the rule sessions.SafeReturn
// and ui/rastrillo.js both carry; it must hold on its own, not only as
// a step inside safeNext's origin check, so a future edit to the
// origin comparison cannot silently loosen what counts as local.
func TestLocalPathIsExportedAndRejectsSchemesAndControlCharacters(t *testing.T) {
	cases := []any{
		"//evil.example/x", "/\t/evil.example", "/\n/evil.example", "/\\evil.example",
		"https://evil.example/x", "https://app.test/home",
		"/", "/home", "/confirm?x=1",
		nil, 42, "",
	}
	want := []bool{false, false, false, false, false, false, true, true, true, false, false, false}
	if _, _, _, got := runSigninJS(t, cases); !reflect.DeepEqual(got, want) {
		t.Fatalf("localPath:\n got %v\nwant %v", got, want)
	}
}

func TestJSIsTheModuleAndSelfContained(t *testing.T) {
	file, err := os.ReadFile("js/signin.mjs")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(passkey.JS(), file) {
		t.Fatal("passkey.JS() is not js/signin.mjs")
	}
	for _, bad := range []string{"http://", "https://", "eval(", "new Function", "innerHTML"} {
		if bytes.Contains(file, []byte(bad)) {
			t.Errorf("signin.mjs contains %q; it must run under the default CSP and write only text", bad)
		}
	}
	// "export function ceremony" (no "async") is a plan typo: the
	// preflight ruling pins the real export's signature instead.
	for _, want := range []string{"export async function ceremony", "export function safeNext", "export function localPath", `typeof document !== "undefined"`, "aria-disabled", "pageshow"} {
		if !bytes.Contains(file, []byte(want)) {
			t.Errorf("signin.mjs lacks %q", want)
		}
	}
}
