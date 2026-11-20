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

type posted struct {
	URL    string `json:"url"`
	Signal bool   `json:"signal"`
}

type autofillRun struct {
	Result outcome  `json:"result"`
	Begins int      `json:"begins"`
	Asked  []asked  `json:"asked"`
	Posted []posted `json:"posted"`
}

type autofillRuns struct {
	Runs           map[string]autofillRun `json:"runs"`
	FetchGotSignal bool                   `json:"fetchGotSignal"`
}

func runAutofillJS(t *testing.T) autofillRuns {
	t.Helper()
	var got autofillRuns
	raw := nodetest.Run(t, nodetest.Cmd{
		Args: []string{"js/conditional_node.mjs"}})
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

// The autofill sign-in navigates only after an ok:true finish, says
// nothing when it fails, and retries a refused finish once with a new
// challenge, because the autofill request can outlive the one begin
// minted. Every request is conditional and carries the page's signal.
func TestTheAutofillSignInRetriesOnceAndFailsSilently(t *testing.T) {
	got := runAutofillJS(t).Runs
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

// Sending the form cancels the autofill sign-in, wherever it is: no
// request goes out after the abort, nothing navigates, and every
// request carries the signal so one in flight is aborted too.
func TestSendingTheFormStopsTheAutofillSignIn(t *testing.T) {
	got := runAutofillJS(t)
	if !got.FetchGotSignal {
		t.Error("postJSON does not hand the signal to fetch")
	}
	want := map[string][]string{
		"sent before it started":                {},
		"sent while begin was in flight":        {"/begin"},
		"sent while the authenticator was open": {"/begin"},
		"sent while finish was in flight":       {"/begin", "/finish"},
		"refused after the form was sent":       {"/begin", "/finish"},
	}
	for name, urls := range want {
		run := got.Runs[name]
		var sent []string
		for _, post := range run.Posted {
			sent = append(sent, post.URL)
			if !post.Signal {
				t.Errorf("%s: %s went without the signal", name, post.URL)
			}
		}
		if sent == nil {
			sent = []string{}
		}
		if !reflect.DeepEqual(sent, urls) || run.Result != (outcome{}) {
			t.Errorf("%s: posted %v, ended %+v; want %v and nothing more",
				name, sent, run.Result, urls)
		}
	}
}
