package route

// The route table's refusals. Each panics while the app registers its
// routes, so a table the parity check could not trust stops the start.
const (
	msgNoMethod = `route: %q names no method. ` +
		`Write it as "POST /posts/{id}/publish"`
	msgUnknownMethod = `route: %q: ` +
		`the method must be GET, POST, PUT, PATCH or DELETE`
	msgTwice       = `route: %s %s is registered twice. Register it once`
	msgNoOperation = `route: Operation needs a declared operation. ` +
		`Pass its Descriptor, such as posts.Publish.Descriptor`
	msgNoReason = `route: PageOnly needs a reason. ` +
		`Say why an agent has no use for this route, ` +
		`such as "the stylesheet"`
)
