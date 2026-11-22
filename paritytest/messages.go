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
