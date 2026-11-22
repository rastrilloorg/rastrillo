package paritytest

// The check's failures, each reported with t.Errorf. The reader is the
// app's developer, so each says what to change.
const (
	msgUnmarked = `%s %s names no operation and no page-only reason. ` +
		`Name its operation with route.Operation, ` +
		`or say why an agent has no use for it with route.PageOnly`
	msgUndeclared = `%s %s names %s, ` +
		`which Surface.Operations doesn't list. ` +
		`Add it to Surface.Operations, api.Operations and mcp.New`
	msgListedTwice = `Surface.Operations lists %s twice. ` +
		`Remove the duplicate`
	msgNoPage     = `%s has no page. Add a page route for it`
	msgMissing    = `%s isn't served by %s. Pass its Descriptor to %s`
	msgUnexpected = `%s serves %s, ` +
		`which Surface.Operations doesn't list. ` +
		`Add it to Surface.Operations and give it a page, ` +
		`or stop serving it`
	msgDiffers = `%s serves %s as %+v, but it is declared as %+v. ` +
		`Pass the declared Descriptor`
	msgWrongOperation = `the API serves %s at %s %s, ` +
		`where %s belongs. Move it to its declared route`
	msgNotMounted = `%s %s answered %d with Rastrillo-Operation %q, ` +
		`not 401 with %q. ` +
		`Mount the API tree at /api/v1/, ahead of any catch-all`
	msgNotListed = `%s isn't in the MCP tool list for Surface.Token. ` +
		`Check that Authorize allows that token every operation`
	msgHumanListed   = `%s is human-only, but the MCP tool list shows it`
	msgHumanCallable = `%s is human-only, ` +
		`but %s answered an agent with %q, not human_only`
	msgUnrecorded = `Register %s %s through the table with Handle, ` +
		`or use On to register a sub-router's routes`
)

// Failures to read a door's answer, so a broken answer is reported
// rather than compared as nothing. Each names the door and operation.
const (
	msgNoBody = `%s answered %s with no body for its result; ` +
		`return the result`
	msgNotJSON = `%s answered %s with a body that isn't one JSON value: ` +
		`%v; return one JSON value`
	msgNotObject = `%s answered %s with %s, not a JSON object; ` +
		`return an object`
	msgEmptyToolError = `MCP answered %s with a tool error ` +
		`that has no content`
	msgListUnread = `the MCP tool list for Surface.Token answered %d ` +
		`with %s, not a list of tools`
)

// Run's failures.
const (
	msgDoorsDiffer  = `step %q: %s returned %s %v, but %s returned %s %v`
	msgBodiesDiffer = `step %q: API returned body %v; ` +
		`MCP returned body %v`
	msgMessagesDiffer = `step %q: API refused with message %q; ` +
		`MCP refused with message %q`
	msgFieldsDiffer = `step %q: API refused fields with %v; ` +
		`MCP refused fields with %v; ` +
		`make both doors refuse the same fields with the same messages`
	msgStateDiffers = `after step %q: %s left state %v; ` +
		`%s left state %v`
	msgScenarioHumanOnly = `step %q calls human-only %s. ` +
		`Test it with paritytest.Routes`
)
