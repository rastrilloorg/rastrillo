package webauthn_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"amadan.net/rastrillo/rastrillo/nodetest"
)

type ceremony struct {
	Ceremony string  `json:"ceremony"`
	RPID     *string `json:"rpId"`
	Allowed  [][]int `json:"allowed"`
}

type registerRun struct {
	OK       bool       `json:"ok"`
	Error    string     `json:"error"`
	PRFBytes int        `json:"prfBytes"`
	Calls    []ceremony `json:"calls"`
}

// Where create() returns no PRF output (Safari with iCloud Keychain),
// register() asks for it with a second prompt, and that prompt must
// name the relying party the passkey was made under. Left out, the
// browser uses the page's host, which finds nothing when the app
// registers under a parent domain, and setup fails after the passkey
// was already made.
func TestRegisterAsksForPRFUnderItsOwnRelyingParty(t *testing.T) {
	var got map[string]registerRun
	raw := nodetest.Run(t, nodetest.Cmd{
		Args: []string{"js/register_node.mjs"}})
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	rp := "box.example.test"
	madeHere := [][]int{{1, 2, 3}}
	want := map[string]registerRun{
		"PRF at creation": {OK: true, PRFBytes: 32,
			Calls: []ceremony{{Ceremony: "create", RPID: &rp}}},
		"PRF by a second prompt": {OK: true, PRFBytes: 32,
			Calls: []ceremony{
				{Ceremony: "create", RPID: &rp},
				{Ceremony: "get", RPID: &rp, Allowed: madeHere}}},
		"PRF by a second prompt, no relying party given": {
			OK: true, PRFBytes: 32,
			Calls: []ceremony{
				{Ceremony: "create"},
				{Ceremony: "get", Allowed: madeHere}}},
	}
	for name, run := range want {
		if !reflect.DeepEqual(got[name], run) {
			t.Errorf("%s:\n got %s\nwant %s",
				name, describe(got[name]), describe(run))
		}
	}
}

// describe prints a run with its pointers followed, so a failure shows
// the relying party each ceremony asked for rather than an address.
func describe(run registerRun) string {
	out, _ := json.Marshal(run)
	return string(out)
}
