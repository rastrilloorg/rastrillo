package webauthn_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"amadan.net/rastrillo/rastrillo/nodetest"
)

type credentialsCall struct {
	RPID      string  `json:"rpId"`
	Mediation *string `json:"mediation"`
	Signal    bool    `json:"signal"`
}

type authenticateRun struct {
	OK    bool              `json:"ok"`
	Error string            `json:"error"`
	Calls []credentialsCall `json:"calls"`
}

// Conditional mediation is asked for only where the browser has it and
// never with a legacy relying party; an abort reaches the caller; and a
// request that asks for neither is exactly the request it always was.
func TestAuthenticateKeepsConditionalMediationsRules(t *testing.T) {
	var got map[string]authenticateRun
	raw := nodetest.Run(t, nodetest.Cmd{
		Args: []string{"js/conditional_node.mjs"}})
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	conditional := "conditional"
	want := map[string]authenticateRun{
		"modal, as before": {OK: true,
			Calls: []credentialsCall{{RPID: "app.test"}}},
		"conditional": {OK: true, Calls: []credentialsCall{
			{RPID: "app.test", Mediation: &conditional, Signal: true}}},
		"conditional where the browser has none": {
			Error: "conditional mediation is not available"},
		"conditional with a legacy relying party": {
			Error: "conditional mediation cannot use a legacy relying party"},
		"conditional, aborted": {Error: "AbortError",
			Calls: []credentialsCall{
				{RPID: "app.test", Mediation: &conditional, Signal: true}}},
	}
	for name, run := range want {
		if run.Calls == nil {
			run.Calls = []credentialsCall{}
		}
		if !reflect.DeepEqual(got[name], run) {
			t.Errorf("%s: %+v, want %+v", name, got[name], run)
		}
	}
}
