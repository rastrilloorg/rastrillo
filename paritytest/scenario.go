package paritytest

import (
	"maps"
	"reflect"
	"slices"
	"testing"

	"amadan.net/rastrillo/rastrillo/op"
)

// pageDoor names the app's own pages in Run's failures, beside
// apiDoor and mcpDoor.
const pageDoor = "the page"

// Fixture is one fresh copy of the app, for one door of a scenario.
type Fixture interface {
	Surface() Surface
	// Token is the agent's write token that Run sends for person.
	Token(t testing.TB, person string) string
	// Page does step as person through the app's own pages and reports
	// it as an Outcome (Kind and Fields; Body may be nil).
	Page(t testing.TB, person string, step Step) Outcome
	// State is what the app has stored, without ids and times, which
	// differ between fresh fixtures by design.
	State(t testing.TB) any
}

// Step is one thing a person, or their agent, does.
type Step struct {
	Name      string
	Person    string
	Operation op.Descriptor
	Input     map[string]any
}

// Scenario is a run of steps, each done on a fresh fixture per door.
type Scenario struct {
	Fresh func(t testing.TB) Fixture
	Steps []Step
}

// Run does every step of scenario on three fresh fixtures, one through
// the pages, one through the API and one through MCP, the agents with
// Token(person). After each step it reports, with t.Errorf, a door
// whose answer differs from the page's, API and MCP bodies or refusal
// messages that differ, and a door whose state starts to differ from
// the page's. It never stops early.
//
// A step naming a human-only operation is reported and done on no
// door, because the API and MCP refuse it to an agent by design.
func Run(t testing.TB, scenario Scenario) {
	t.Helper()
	// The page comes first: it is what a person does, and the agents'
	// doors are held to it.
	doors := []*door{
		{name: pageDoor, take: byPage},
		{name: apiDoor, take: byAPI},
		{name: mcpDoor, take: byTool},
	}
	for _, each := range doors {
		each.fixture = scenario.Fresh(t)
		each.agrees = true
	}
	for _, step := range scenario.Steps {
		if step.Operation.HumanOnly != "" {
			t.Errorf(msgScenarioHumanOnly, step.Name, step.Operation.ID)
			continue
		}
		outcomes := make([]Outcome, len(doors))
		for index, each := range doors {
			outcomes[index] = each.take(t, each.fixture, step)
		}
		compareOutcomes(t, step, doors, outcomes)
		compareStates(t, step, doors)
	}
}

// door is one fixture and how a step is done on it.
type door struct {
	name    string
	fixture Fixture
	take    func(t testing.TB, fixture Fixture, step Step) Outcome
	// agrees is whether its state matched the page's after the last
	// step.
	agrees bool
}

func byPage(t testing.TB, fixture Fixture, step Step) Outcome {
	return fixture.Page(t, step.Person, step)
}

func byAPI(t testing.TB, fixture Fixture, step Step) Outcome {
	return CallAPI(t, agentOf(t, fixture, step.Person), step.Operation,
		step.Input)
}

func byTool(t testing.TB, fixture Fixture, step Step) Outcome {
	return CallTool(t, agentOf(t, fixture, step.Person), step.Operation,
		step.Input)
}

// agentOf is fixture's surface with person's agent's token.
func agentOf(t testing.TB, fixture Fixture, person string) Surface {
	surface := fixture.Surface()
	surface.Token = fixture.Token(t, person)
	return surface
}

// compareOutcomes holds each agent's door to the page's Kind and
// Fields, and the API's body, refusal message and field messages to
// MCP's. A page renders its own HTML, so its body and wording aren't
// compared.
// outcomes are in doors' order.
func compareOutcomes(
	t testing.TB, step Step, doors []*door, outcomes []Outcome,
) {
	t.Helper()
	page := outcomes[0]
	for index, each := range doors[1:] {
		outcome := outcomes[index+1]
		if outcome.Kind != page.Kind ||
			!slices.Equal(outcome.Fields, page.Fields) {
			t.Errorf(msgDoorsDiffer, step.Name, doors[0].name, page.Kind,
				page.Fields, each.name, outcome.Kind, outcome.Fields)
		}
	}
	viaAPI, viaMCP := outcomes[1], outcomes[2]
	if !reflect.DeepEqual(viaAPI.Body, viaMCP.Body) {
		t.Errorf(msgBodiesDiffer, step.Name, viaAPI.Body, viaMCP.Body)
	}
	if viaAPI.Message != viaMCP.Message {
		t.Errorf(msgMessagesDiffer, step.Name, viaAPI.Message,
			viaMCP.Message)
	}
	if !maps.Equal(viaAPI.FieldMessages, viaMCP.FieldMessages) {
		t.Errorf(msgFieldsDiffer, step.Name, viaAPI.FieldMessages,
			viaMCP.FieldMessages)
	}
}

// compareStates holds each agent's door's state to the page's. Once
// they differ they differ after every later step, so each difference
// is reported only at the step where it starts.
func compareStates(t testing.TB, step Step, doors []*door) {
	t.Helper()
	page := doors[0].fixture.State(t)
	for _, each := range doors[1:] {
		state := each.fixture.State(t)
		agrees := reflect.DeepEqual(state, page)
		if !agrees && each.agrees {
			t.Errorf(msgStateDiffers, step.Name, doors[0].name, page,
				each.name, state)
		}
		each.agrees = agrees
	}
}
