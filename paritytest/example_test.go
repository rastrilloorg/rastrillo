package paritytest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// eachRouter runs test once over chi and once over a ServeMux, because
// the table and the doors must behave alike on both.
func eachRouter(t *testing.T, test func(t *testing.T, router string)) {
	for _, router := range []string{"chi", "ServeMux"} {
		t.Run(router, func(t *testing.T) { test(t, router) })
	}
}

func TestTheExampleAppPassesTheParityCheck(t *testing.T) {
	eachRouter(t, func(t *testing.T, router string) {
		surface, app := buildExample(t, router, exampleOptions{})
		recorder := &spy{TB: t}
		Routes(recorder, surface)
		if len(recorder.failures) > 0 {
			t.Errorf("Routes reported:\n%s",
				strings.Join(recorder.failures, "\n"))
		}
		// A silent check means something only if the doors serve.
		created := CallAPI(t, surface, app.create.Descriptor,
			map[string]any{"title": "Hello"})
		if created.Kind != "ok" {
			t.Fatalf("notes.create through the API: %+v, want ok", created)
		}
		listed := CallTool(t, surface, app.list.Descriptor, nil)
		want := map[string]any{"notes": []any{map[string]any{
			"id": json.Number("1"), "title": "Hello",
		}}}
		if listed.Kind != "ok" || !reflect.DeepEqual(listed.Body, want) {
			t.Errorf("notes.list through MCP: %+v, want ok with %v",
				listed, want)
		}
	})
}

// Spec §5.6: a read token sees only the read tools.
func TestAReadTokenListsOnlyReadTools(t *testing.T) {
	eachRouter(t, func(t *testing.T, router string) {
		surface, _ := buildExample(t, router, exampleOptions{})
		wants := map[string][]string{
			"reader-a": {"notes_list"},
			"writer-a": {"notes_create", "notes_delete", "notes_list"},
		}
		for token, want := range wants {
			surface.Token = token
			if names := ToolNames(t, surface); !slices.Equal(names, want) {
				t.Errorf("ToolNames as %s = %v, want %v", token, names, want)
			}
		}
	})
}

func TestAHumanOnlyOperationRefusesEveryTokenWhateverElseIsWrong(
	t *testing.T,
) {
	eachRouter(t, func(t *testing.T, router string) {
		surface, app := buildExample(t, router, exampleOptions{})
		tokens := app.tokens.Descriptor
		outcomes := map[string]Outcome{}
		for _, token := range []string{"writer-a", "person-a", "reader-a"} {
			surface.Token = token
			input := map[string]any{"agent": "Claude"}
			outcomes["API as "+token] = CallAPI(t, surface, tokens, input)
			outcomes["MCP as "+token] = CallTool(t, surface, tokens, input)
		}
		surface.Token = "writer-a"
		ill := map[string]any{"agent": 5}
		outcomes["API, ill-typed"] = CallAPI(t, surface, tokens, ill)
		outcomes["MCP, ill-typed"] = CallTool(t, surface, tokens, ill)
		bare := call{descriptor: tokens, token: "writer-a",
			arguments: []byte(`{"agent":"Claude"}`)}
		outcomes["API, no session"] = bare.api(t, surface.Handler)
		outcomes["MCP, no session"] = bare.tool(t, surface.Handler)
		for name, outcome := range outcomes {
			if outcome.Kind != "human_only" {
				t.Errorf("%s: %+v, want human_only", name, outcome)
			}
		}
		if minted := app.mintedCount(); minted != 0 {
			t.Fatalf("a token call minted %d tokens, want none", minted)
		}
		status := postPage(t, surface, "a", "/settings/tokens",
			url.Values{"agent": {"Claude"}})
		if status != http.StatusSeeOther || app.mintedCount() != 1 {
			t.Errorf("the page as a answered %d and minted %d, "+
				"want 303 and 1", status, app.mintedCount())
		}
	})
}

func TestMalformedInputIsAnsweredAlikeByTheAPIAndMCP(t *testing.T) {
	eachRouter(t, func(t *testing.T, router string) {
		surface, app := buildExample(t, router, exampleOptions{})
		create := app.create.Descriptor
		inputs := []struct {
			name  string
			input map[string]any
			want  []string
		}{
			{"a null title", map[string]any{"title": nil}, []string{"title"}},
			{"an unknown member", map[string]any{"colour": 1},
				[]string{"colour"}},
			{"a number for a title", map[string]any{"title": 5},
				[]string{"title"}},
		}
		for _, each := range inputs {
			viaAPI := CallAPI(t, surface, create, each.input)
			viaMCP := CallTool(t, surface, create, each.input)
			sameRefusal(t, each.name, viaAPI, viaMCP, each.want)
		}
		twice := call{descriptor: create, token: "writer-a",
			arguments: []byte(`{"title":"a","title":"b"}`),
			session:   agentSession}
		sameRefusal(t, "a title sent twice", twice.api(t, surface.Handler),
			twice.tool(t, surface.Handler), []string{"title"})
	})
}

func sameRefusal(
	t *testing.T, name string, viaAPI, viaMCP Outcome, fields []string,
) {
	t.Helper()
	refusedAlike := viaAPI.Kind == "invalid" && viaMCP.Kind == "invalid" &&
		slices.Equal(viaAPI.Fields, fields) &&
		slices.Equal(viaMCP.Fields, fields)
	if !refusedAlike {
		t.Errorf("%s: the API answered %+v and MCP %+v, "+
			"want both invalid with fields %v", name, viaAPI, viaMCP, fields)
	}
}

// postPage posts form to the app's page at path as person, signed in,
// and returns the status.
func postPage(
	t *testing.T, surface Surface, person, path string, form url.Values,
) int {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(),
		http.MethodPost, path, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("X-Example-Person", person)
	response := httptest.NewRecorder()
	surface.Handler.ServeHTTP(response, request)
	return response.Code
}
