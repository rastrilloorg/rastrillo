// Package provisiontest is a fake Carlos console and edge for testing code
// that creates instances, attaches domains and publishes path maps
// through them.
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
// The domain doors were read at 82e237a9 (internal/console unless named):
//
//	routedecls.go:845-888 attach, per-host read and detach admit publish
//	                      and instance (instance "for compatibility")
//	apidomains.go:303-678 attach: host normalised, route resolved, the
//	                      neutral 409 "hostname unavailable" (:1843)
//	apidomains.go:685-691 attachAnswer: host, route, created, verified,
//	                      and verify only while pending
//	apidomains.go:218-243 resolveAliasRoute: a named route is only
//	                      shape-checked; none means the one instance
//	domains/claims.go:690-823
//	                      AttachClaimProven: pending until proved, the
//	                      caller's own claim moves to the new route, a
//	                      pending claim yields to a proved caller
//	domains/claims.go:603-622
//	                      _carlos-challenge.<host>, carlos-verify=<hash>
//	domainpolicy.go:283-289
//	                      the challenge: TXT, TTL 300, set "verify",
//	                      required
//	apidomains.go:1580-1675
//	                      the per-host read: {"domain", "deployment",
//	                      "paths"}; the same 404 for a name held
//	                      elsewhere as for one nobody holds
//	apidomains.go:1320-1385
//	                      the domain object: state, reason, detail,
//	                      dns_status, checked_at, cert_expires_at,
//	                      guidance, cname_target, records, verified
//	apidomains.go:1495-1562
//	                      guidance and records only before DNS points
//	                      here, and the TXT record while pending
//	domainstate.go:13-91, 117-180
//	                      the state words and how they are derived
//	domainstate.go:184-202
//	                      a record: type, name, value, ttl, set, required
//	apidomains.go:1136-1306
//	                      detach: 200 {"host", "removed"}, removed false
//	                      for a name the app does not hold
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
//   - ProveDomain proves a hostname for any account that attaches it,
//     where the platform's token names the account. Plaza's accounts
//     share one platform account, so for plaza the two agree.
//   - A pending claim never lapses, where the platform's lapses after
//     seven days and then yields to anyone.
//   - The per-host read has no "deployment" block, the DNS targets are
//     fixed (a CNAME to edge.oncarlos.test, an A record to 192.0.2.1),
//     and a name of two labels stands in for an apex. Only the "pointing"
//     DNS answer and certificate expiry are reported, so the failed,
//     misdirected and unknown states never occur.
//   - Attach refuses another app's instance host with the neutral 409,
//     and skips the platform's checks for reserved and platform names,
//     plans, capabilities and catch-all reservations.
//
// A fake that guessed would let a readiness or routing bug in a caller
// pass, so when the platform changes, change that table, the rules here
// and this comment together.
//
// The fake is an http.RoundTripper that routes on the request's host: the
// console's origin goes to the console half, every other host to the edge
// half. There are no sockets and no TLS.
package provisiontest
