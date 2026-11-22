package paritytest

import (
	"fmt"
	"net/http"
	"slices"
	"testing"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/api"
	"amadan.net/rastrillo/rastrillo/mcp"
	"amadan.net/rastrillo/rastrillo/op"
)

// The doors' names in failures, and where each takes a Descriptor.
const (
	apiDoor     = "the API"
	apiTakes    = "api.Operations"
	mcpDoor     = "MCP"
	mcpTakes    = "mcp.New"
	operationID = "Rastrillo-Operation"
)

// declaration is every field of a Descriptor a door must serve as
// declared. Invoke is left out, because funcs can't be compared, and
// the schemas are strings, so a failure prints them as JSON.
type declaration struct {
	Operation   rastrillo.Operation
	Exposed     bool
	Description string
	Confirm     string
	Destructive bool
	Idempotent  bool
	InQuery     bool
	Input       string
	Result      string
}

func declarationOf(descriptor op.Descriptor) declaration {
	return declaration{
		Operation:   descriptor.Operation,
		Exposed:     descriptor.Exposed,
		Description: descriptor.Description,
		Confirm:     descriptor.Confirm,
		Destructive: descriptor.Destructive,
		Idempotent:  descriptor.Idempotent,
		InQuery:     descriptor.InQuery,
		Input:       string(descriptor.Input),
		Result:      string(descriptor.Result),
	}
}

// checkServed holds everything a door serves to the declarations, at
// every route, not only an operation's expected one: a declared ID at a
// second path must be the declared operation too, or a door could serve
// a human-only operation there without its refusal. A served operation
// neither declared nor generated is unexpected. Each failure is reported
// once, however many routes repeat it.
func checkServed(
	t testing.TB, door string, served, declared []op.Descriptor,
	known map[string]bool,
) {
	t.Helper()
	declaredAs := map[string]declaration{}
	for _, descriptor := range declared {
		declaredAs[descriptor.ID] = declarationOf(descriptor)
	}
	reported := map[string]bool{}
	for _, descriptor := range served {
		var failure string
		wanted, isDeclared := declaredAs[descriptor.ID]
		servedAs := declarationOf(descriptor)
		switch {
		case isDeclared && servedAs != wanted:
			failure = fmt.Sprintf(msgDiffers, door, descriptor.ID,
				servedAs, wanted)
		case !isDeclared && !known[descriptor.ID]:
			failure = fmt.Sprintf(msgUnexpected, door, descriptor.ID)
		}
		if failure != "" && !reported[failure] {
			reported[failure] = true
			t.Error(failure)
		}
	}
}

// checkAPI compares the API's whole inventory with the declarations.
// Each declared operation is looked for at the method and pattern
// api.Operations gives it, so a route moved by hand is found out.
func checkAPI(
	t testing.TB, handler http.Handler, declared []op.Descriptor,
	known map[string]bool,
) {
	t.Helper()
	routes, err := api.Served(handler)
	if err != nil {
		t.Error(err)
		return
	}
	byPlace := map[string]op.Descriptor{}
	var served []op.Descriptor
	for _, each := range routes {
		byPlace[each.Method+" "+each.Pattern] = each.Op
		served = append(served, each.Op)
	}
	for _, descriptor := range declared {
		expected := api.Operations(descriptor)[0]
		found, present := byPlace[expected.Method+" "+expected.Pattern]
		switch {
		case !present:
			t.Errorf(msgMissing, descriptor.ID, apiDoor, apiTakes)
		case found.ID != descriptor.ID:
			t.Errorf(msgWrongOperation, found.ID, expected.Method,
				expected.Pattern, descriptor.ID)
		}
	}
	checkServed(t, apiDoor, served, declared, known)
}

// checkMCP compares MCP's whole inventory with the declarations.
func checkMCP(
	t testing.TB, handler http.Handler, declared []op.Descriptor,
	known map[string]bool,
) {
	t.Helper()
	served, err := mcp.Served(handler)
	if err != nil {
		t.Error(err)
		return
	}
	for _, descriptor := range declared {
		isServed := slices.ContainsFunc(served, func(each op.Descriptor) bool {
			return each.ID == descriptor.ID
		})
		if !isServed {
			t.Errorf(msgMissing, descriptor.ID, mcpDoor, mcpTakes)
		}
	}
	checkServed(t, mcpDoor, served, declared, known)
}

// checkMounted asks the whole app for each declared operation's route
// with no credential. Only the API tree answers that 401 with the
// operation's ID, so a tree mounted elsewhere, or behind a catch-all
// or middleware that answers first, is found out even when the status
// is right.
func checkMounted(
	t testing.TB, surface Surface, declared []op.Descriptor,
) {
	t.Helper()
	for _, descriptor := range declared {
		expected := api.Operations(descriptor)[0]
		answer := send(t, surface.Handler, expected.Method,
			expected.Pattern, http.Header{}, nil)
		named := answer.header.Get(operationID)
		if answer.status != http.StatusUnauthorized ||
			named != descriptor.ID {
			t.Errorf(msgNotMounted, expected.Method, expected.Pattern,
				answer.status, named, descriptor.ID)
		}
	}
}

// checkListed holds MCP's tool list for Surface.Token to the
// declarations: every operation an agent may call, and no human-only
// one, which no token may call.
func checkListed(
	t testing.TB, surface Surface, declared []op.Descriptor,
) {
	t.Helper()
	listed := ToolNames(t, surface)
	for _, descriptor := range declared {
		shown := slices.Contains(listed, mcp.ToolName(descriptor.ID))
		humanOnly := descriptor.HumanOnly != ""
		switch {
		case humanOnly && shown:
			t.Errorf(msgHumanListed, descriptor.ID)
		case !humanOnly && !shown:
			t.Errorf(msgNotListed, descriptor.ID)
		}
	}
}

// checkHumanOnly calls each human-only operation through both doors as
// the agent Surface.Token belongs to, which both must refuse.
func checkHumanOnly(
	t testing.TB, surface Surface, declared []op.Descriptor,
) {
	t.Helper()
	for _, descriptor := range declared {
		if descriptor.HumanOnly == "" {
			continue
		}
		outcomes := []struct {
			door    string
			outcome Outcome
		}{
			{apiDoor, CallAPI(t, surface, descriptor, nil)},
			{mcpDoor, CallTool(t, surface, descriptor, nil)},
		}
		for _, each := range outcomes {
			if each.outcome.Kind != "human_only" {
				t.Errorf(msgHumanCallable, descriptor.ID, each.door,
					each.outcome.Kind)
			}
		}
	}
}
