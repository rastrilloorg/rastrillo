package passkey_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"amadan.net/rastrillo/rastrillo/nodetest"
)

type asked struct {
	Challenge string `json:"challenge"`
	Mediation string `json:"mediation"`
	Signal    bool   `json:"signal"`
}

type autofillRun struct {
	Result outcome `json:"result"`
	Begins int     `json:"begins"`
	Asked  []asked `json:"asked"`
}

// The autofill sign-in navigates only after an ok:true finish, says
// nothing when it fails, and retries a refused finish once with a new
// challenge, because the autofill request can outlive the one begin
// minted. Every request is conditional and carries the page's signal.
func TestTheAutofillSignInRetriesOnceAndFailsSilently(t *testing.T) {
	var got map[string]autofillRun
	raw := nodetest.Run(t, nodetest.Cmd{
		Args: []string{"js/conditional_node.mjs"}})
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		result outcome
		begins int
	}{
		"signed in at once":               {outcome{Navigate: "/home"}, 1},
		"refused once, then signed in":    {outcome{Navigate: "/home"}, 2},
		"refused twice":                   {outcome{}, 2},
		"finish says ok false twice":      {outcome{}, 2},
		"begin fails":                     {outcome{}, 1},
		"the request was aborted":         {outcome{}, 1},
		"refused after the form was sent": {outcome{}, 1},
		"off-site destination":            {outcome{Navigate: "/"}, 1},
	}
	for name, run := range want {
		if got[name].Result != run.result || got[name].Begins != run.begins {
			t.Errorf("%s: %+v after %d begins, want %+v after %d", name,
				got[name].Result, got[name].Begins, run.result, run.begins)
		}
	}
	retried := got["refused once, then signed in"].Asked
	wantAsked := []asked{{"c1", "conditional", true},
		{"c2", "conditional", true}}
	if !reflect.DeepEqual(retried, wantAsked) {
		t.Errorf("the retry asked %+v, want %+v", retried, wantAsked)
	}
}
