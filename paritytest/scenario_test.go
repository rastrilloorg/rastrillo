package paritytest

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/api"
)

// exampleFixture is one fresh example app, as Run drives it.
type exampleFixture struct {
	surface Surface
	app     *exampleApp
}

func (fixture *exampleFixture) Surface() Surface { return fixture.surface }

// exampleAgents are each person's agent's write token.
var exampleAgents = map[string]string{"a": "writer-a", "b": "writer-b"}

func (fixture *exampleFixture) Token(t testing.TB, person string) string {
	token, found := exampleAgents[person]
	if !found {
		t.Fatalf("the example has no agent for %q", person)
	}
	return token
}

// Page posts the step's input as a form to its operation's page, as
// person, and reads the page's answer as an Outcome.
func (fixture *exampleFixture) Page(
	t testing.TB, person string, step Step,
) Outcome {
	t.Helper()
	method, path := pageOf(t, step)
	form := url.Values{}
	for name, value := range step.Input {
		if name != "id" {
			form.Set(name, fmt.Sprint(value))
		}
	}
	request := httptest.NewRequestWithContext(t.Context(), method, path,
		strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("X-Example-Person", person)
	response := httptest.NewRecorder()
	fixture.surface.Handler.ServeHTTP(response, request)
	return pageOutcome(response)
}

// pageOf is the method and path of step's page. A note's id is in the
// path, as a person's browser sends it.
func pageOf(t testing.TB, step Step) (method, path string) {
	switch step.Operation.ID {
	case "notes.create":
		return http.MethodPost, "/notes"
	case "notes.list":
		return http.MethodGet, "/notes"
	case "notes.delete":
		return http.MethodPost, fmt.Sprintf("/notes/%v/delete",
			step.Input["id"])
	}
	t.Fatalf("the example has no page for %s", step.Operation.ID)
	return "", ""
}

// pageOutcome reads a page's answer: a redirect or a page is ok, and
// 422 lists Go field names, one per line, which the API would send as
// their wire names.
func pageOutcome(response *httptest.ResponseRecorder) Outcome {
	outcome := Outcome{Status: response.Code}
	switch response.Code {
	case http.StatusOK, http.StatusSeeOther:
		outcome.Kind = "ok"
	case http.StatusUnprocessableEntity:
		outcome.Kind = "invalid"
		for name := range strings.FieldsSeq(response.Body.String()) {
			outcome.Fields = append(outcome.Fields, api.FieldName(name))
		}
		slices.Sort(outcome.Fields)
	case http.StatusForbidden:
		outcome.Kind = "forbidden"
	case http.StatusNotFound:
		outcome.Kind = "not_found"
	case http.StatusConflict:
		outcome.Kind = "conflict"
	default:
		outcome.Kind = fmt.Sprintf("HTTP %d", response.Code)
	}
	return outcome
}

// State is the notes' titles, sorted: ids are left out, as Fixture
// asks.
func (fixture *exampleFixture) State(testing.TB) any {
	fixture.app.mutex.Lock()
	defer fixture.app.mutex.Unlock()
	return slices.Sorted(maps.Values(fixture.app.notes))
}

// stored is how many notes the app has ever stored.
func (app *exampleApp) stored() int64 {
	app.mutex.Lock()
	defer app.mutex.Unlock()
	return app.lastID
}

// examples makes fresh example apps over one router and keeps each,
// so a test can look inside them after Run.
type examples struct {
	router  string
	options exampleOptions
	made    []*exampleFixture
}

func (factory *examples) fresh(t testing.TB) Fixture {
	surface, app := buildExample(t, factory.router, factory.options)
	fixture := &exampleFixture{surface: surface, app: app}
	factory.made = append(factory.made, fixture)
	return fixture
}

// named holds the operations a step names. Only their declarations are
// read, so any example app's will do.
var named = newExampleApp(exampleOptions{})

// The example scenario: a creates "Hello"; a creates "" (invalid,
// title); b creates "Hi" (forbidden); a lists; a deletes note 1; a
// deletes note 1 again (not_found). "a creates nothing" leaves title
// out, so the operation's own validation must refuse its zero value.
var exampleSteps = []Step{
	{"a creates Hello", "a", named.create.Descriptor,
		map[string]any{"title": "Hello"}},
	{"a creates nothing", "a", named.create.Descriptor, map[string]any{}},
	{"b creates Hi", "b", named.create.Descriptor,
		map[string]any{"title": "Hi"}},
	{"a lists", "a", named.list.Descriptor, nil},
	{"a deletes note 1", "a", named.remove.Descriptor,
		map[string]any{"id": 1}},
	{"a deletes note 1 again", "a", named.remove.Descriptor,
		map[string]any{"id": 1}},
}

// runFailures runs scenario with a spy and returns what Run reported.
func runFailures(t *testing.T, scenario Scenario) []string {
	t.Helper()
	recorder := &spy{TB: t}
	Run(recorder, scenario)
	return recorder.failures
}

func wantRunFailures(t *testing.T, scenario Scenario, want ...string) {
	t.Helper()
	got := slices.Sorted(slices.Values(runFailures(t, scenario)))
	want = slices.Sorted(slices.Values(want))
	if !slices.Equal(got, want) {
		t.Errorf("Run reported:\n\t%s\nwant:\n\t%s",
			strings.Join(got, "\n\t"), strings.Join(want, "\n\t"))
	}
}

func TestTheExampleScenarioEndsAlikeOnEveryDoor(t *testing.T) {
	eachRouter(t, func(t *testing.T, router string) {
		factory := &examples{router: router}
		scenario := Scenario{Fresh: factory.fresh, Steps: exampleSteps}
		if failures := runFailures(t, scenario); len(failures) > 0 {
			t.Errorf("Run reported:\n\t%s",
				strings.Join(failures, "\n\t"))
		}
		// A silent run means something only if every door did the
		// steps: each app stored "Hello" alone, and deleted it.
		if len(factory.made) != 3 {
			t.Fatalf("Run made %d fixtures, want 3", len(factory.made))
		}
		for index, fixture := range factory.made {
			state := fixture.State(t).([]string)
			if fixture.app.stored() != 1 || len(state) != 0 {
				t.Errorf("fixture %d stored %d notes and kept %q, "+
					"want 1 and none", index, fixture.app.stored(), state)
			}
		}
	})
}

// pageSkipsValidation stores a title without Call, so its page lets
// "a creates nothing" and b's write through, where both agents' doors
// refuse them.
func TestRunCatchesAPageThatSkipsTheOperation(t *testing.T) {
	eachRouter(t, func(t *testing.T, router string) {
		factory := &examples{router: router,
			options: exampleOptions{pageSkipsValidation: true}}
		scenario := Scenario{Fresh: factory.fresh, Steps: exampleSteps}
		var want []string
		for _, door := range []string{apiDoor, mcpDoor} {
			want = append(want,
				fmt.Sprintf(msgDoorsDiffer, "a creates nothing",
					pageDoor, "ok", []string(nil),
					door, "invalid", []string{"title"}),
				fmt.Sprintf(msgStateDiffers, "a creates nothing",
					pageDoor, []string{"", "Hello"},
					door, []string{"Hello"}),
				fmt.Sprintf(msgDoorsDiffer, "b creates Hi",
					pageDoor, "ok", []string(nil),
					door, "forbidden", []string(nil)))
		}
		wantRunFailures(t, scenario, want...)
	})
}

func TestRunCatchesAnAPIAndMCPBodyThatDiffer(t *testing.T) {
	eachRouter(t, func(t *testing.T, router string) {
		factory := &examples{router: router,
			options: exampleOptions{mcpResultDiffers: true}}
		scenario := Scenario{Fresh: factory.fresh, Steps: exampleSteps}
		note := func(title string) map[string]any {
			return map[string]any{"id": json.Number("1"), "title": title}
		}
		wantRunFailures(t, scenario, fmt.Sprintf(msgBodiesDiffer,
			"a creates Hello", note("Hello"), note("Hello (MCP)")))
	})
}

func TestRunCatchesARefusalWhoseMessageDiffers(t *testing.T) {
	eachRouter(t, func(t *testing.T, router string) {
		factory := &examples{router: router,
			options: exampleOptions{mcpMessageDiffers: true}}
		scenario := Scenario{Fresh: factory.fresh, Steps: []Step{
			exampleSteps[0],
			{"a creates Hello again", "a", named.create.Descriptor,
				map[string]any{"title": "Hello"}},
		}}
		wantRunFailures(t, scenario, fmt.Sprintf(msgMessagesDiffer,
			"a creates Hello again",
			"A note has that title. Choose another.",
			"That title is taken."))
	})
}

func TestRunRefusesAHumanOnlyStep(t *testing.T) {
	eachRouter(t, func(t *testing.T, router string) {
		factory := &examples{router: router}
		scenario := Scenario{Fresh: factory.fresh, Steps: []Step{
			exampleSteps[0],
			{"a creates a token", "a", named.tokens.Descriptor,
				map[string]any{"agent": "Claude"}},
		}}
		wantRunFailures(t, scenario, fmt.Sprintf(msgScenarioHumanOnly,
			"a creates a token", "tokens.create"))
		// The doors differ for it by design, so no door runs it.
		for index, fixture := range factory.made {
			if minted := fixture.app.mintedCount(); minted != 0 {
				t.Errorf("fixture %d minted %d tokens, want none",
					index, minted)
			}
		}
	})
}

func TestRunCatchesAFieldWhoseMessageDiffers(t *testing.T) {
	eachRouter(t, func(t *testing.T, router string) {
		factory := &examples{router: router,
			options: exampleOptions{mcpFieldMessageDiffers: true}}
		scenario := Scenario{Fresh: factory.fresh,
			Steps: exampleSteps[1:2]}
		wantRunFailures(t, scenario, fmt.Sprintf(msgFieldsDiffer,
			"a creates nothing",
			map[string]string{"title": "Enter a title."},
			map[string]string{"title": "Title is required."}))
	})
}

// normalizing is an example fixture that drops the ID it generates.
type normalizing struct{ *exampleFixture }

func (normalizing) Normalize(
	_ string, body map[string]any,
) map[string]any {
	delete(body, "id")
	return body
}

// A generated ID differs between the API's and MCP's fresh fixtures, so
// only a fixture's Normalize lets a correct operation pass.
func TestRunComparesBodiesAsTheFixtureNormalizesThem(t *testing.T) {
	eachRouter(t, func(t *testing.T, router string) {
		factory := &examples{router: router,
			options: exampleOptions{generatedIDs: true}}
		strict := Scenario{Fresh: factory.fresh, Steps: exampleSteps[:1]}
		failures := runFailures(t, strict)
		differ := fmt.Sprintf(msgBodiesDiffer, "a creates Hello", nil, nil)
		prefix, _, _ := strings.Cut(differ, "<nil>")
		if len(failures) != 1 || !strings.HasPrefix(failures[0], prefix) {
			t.Errorf("without Normalize, Run reported %q, want one %q",
				failures, prefix)
		}
		normalized := Scenario{Steps: exampleSteps[:1],
			Fresh: func(t testing.TB) Fixture {
				return normalizing{factory.fresh(t).(*exampleFixture)}
			}}
		wantRunFailures(t, normalized)
	})
}
