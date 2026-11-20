// Package provisiontest is a fake Carlos console and edge for testing code
// that creates instances and publishes path maps through them.
//
// Every rule it applies was read from the platform's handlers at platform
// commit 5b4c2f64 (create, get and paths first at 04819f9b), and the
// plan's table (docs/superpowers/plans/2026-10-17-provision.md, Task 2)
// cites the file and lines of each. The list, its health words and the
// catch-all were read at 5b4c2f64:
//
//	routedecls.go:613     the list door admits instance and deploy
//	apiinstances.go:1252-1400
//	                      the list: q, limit 1..499, opaque cursor,
//	                      total, truncated, next_cursor
//	apiinstances.go:65    instanceListMax: the list reads at most 500
//	                      records (listRecordsComplete) before it filters
//	                      or pages, and says truncated when more exist
//	listquery.go:25       the health words
//	canonical.go:170-176  public_host is "" without a name host
//	AGENTS.md             "Instance catch-alls": <label>.<parent> serves
//	                      the app's instance with that label
//
// Known differences from the platform, each deliberate and small:
//
//   - Sleep never clears when a request reaches the instance, where the
//     real edge wakes it.
//   - A path target bypasses Fail: the edge serves the target's app even
//     when the target instance is failed.
//   - The list cursor encodes the raw host, where the platform encodes a
//     JSON object; callers must treat it as opaque either way.
//   - The role gate is the one at 5b4c2f64. Current platform main also
//     admits the publish role at the list door.
//
// A fake that guessed would let a readiness or routing bug in a caller
// pass, so when the platform changes, change that table, the rules here
// and this comment together.
//
// The fake is an http.RoundTripper that routes on the request's host: the
// console's origin goes to the console half, every other host to the edge
// half. There are no sockets and no TLS.
package provisiontest
