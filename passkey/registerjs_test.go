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

// registerRun is what js/register_node.mjs reports: the real-fetch
// walk's requests, what register was asked, the finish body's keys and
// where the page went; then each stubbed scenario's outcome.
type registerRun struct {
	Requests   []string           `json:"requests"`
	Registered map[string]any     `json:"registered"`
	FinishKeys []string           `json:"finishKeys"`
	Walk       outcome            `json:"walk"`
	Stale      outcome            `json:"stale"`
	OffSite    outcome            `json:"staleOffSite"`
	NoAddress  outcome            `json:"staleNoAddress"`
	Outcomes   map[string]outcome `json:"outcomes"`
}

func runRegisterJS(t *testing.T) registerRun {
	t.Helper()
	in, err := json.Marshal(map[string]any{"origin": "https://app.test"})
	if err != nil {
		t.Fatal(err)
	}
	var got registerRun
	out := nodetest.Run(t, nodetest.Cmd{
		Args: []string{"js/register_node.mjs"}, Stdin: in,
	})
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	return got
}

// The add page's module: begin, register, finish, then to the answer's
// "to", with the real postJSON over a stubbed fetch.
func TestRegisterJSPostsBeginRegistersFinishesThenNavigates(t *testing.T) {
	got := runRegisterJS(t)
	if want := []string{"POST /passkey/register/begin",
		"POST /passkey/register/finish?return_to=%2Fdocs"}; !reflect.
		DeepEqual(got.Requests, want) {
		t.Fatalf("requests %v, want %v", got.Requests, want)
	}
	registered := got.Registered
	if registered["challenge"] != "Y2g" || registered["rpId"] != "app.test" ||
		registered["rpName"] != "Harbour" ||
		registered["userName"] != "ada@example.com" ||
		registered["userIdLength"] != float64(22) {
		t.Fatalf("register was asked %v", registered)
	}
	if !reflect.DeepEqual(got.FinishKeys,
		[]string{"attestationObject", "clientDataJSON"}) {
		t.Fatalf("finish carried %v", got.FinishKeys)
	}
	if got.Walk != (outcome{Navigate: "/signin/add/passkey?done=1"}) {
		t.Fatalf("the page went %+v", got.Walk)
	}
}

// A session gone stale since the page loaded: the 401's "reauth" is
// where the page goes, before any passkey is made, and only if local.
func TestRegisterJSFollowsAReauthAnswer(t *testing.T) {
	got := runRegisterJS(t)
	want := "/signin?reauth=1&return_to=%2Fsignin%2Fadd%2Fpasskey&force=1"
	if got.Stale != (outcome{Navigate: want}) {
		t.Errorf("stale: %+v, want navigate %q", got.Stale, want)
	}
	if got.OffSite != (outcome{Navigate: "/"}) {
		t.Errorf("an off-site reauth: %+v, want navigate /", got.OffSite)
	}
	if got.NoAddress != (outcome{Message: "failed"}) {
		t.Errorf("a 401 naming nothing: %+v, want failed", got.NoAddress)
	}
}

// Every other ending is a message and no navigation.
func TestRegisterJSNavigatesOnlyAfterASuccess(t *testing.T) {
	want := map[string]outcome{
		"finish says ok false":      {Message: "failed"},
		"finish not 2xx":            {Message: "failed"},
		"begin without a challenge": {Message: "failed"},
		"the person cancelled":      {Message: "failed"},
		"finish names another site": {Navigate: "/"},
	}
	if got := runRegisterJS(t).Outcomes; !reflect.DeepEqual(got, want) {
		t.Fatalf("outcomes:\n got %v\nwant %v", got, want)
	}
}

func TestRegisterJSIsTheModuleAndSelfContained(t *testing.T) {
	file, err := os.ReadFile("js/register.mjs")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(passkey.RegisterJS(), file) {
		t.Fatal("passkey.RegisterJS() is not js/register.mjs")
	}
	for _, bad := range []string{"http://", "https://", "eval(",
		"new Function", "innerHTML"} {
		if bytes.Contains(file, []byte(bad)) {
			t.Errorf("register.mjs contains %q", bad)
		}
	}
	for _, want := range []string{`from "./signin.mjs"`,
		"export async function enrol",
		`typeof document !== "undefined"`} {
		if !bytes.Contains(file, []byte(want)) {
			t.Errorf("register.mjs lacks %q", want)
		}
	}
}
