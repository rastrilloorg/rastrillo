package provisiontest

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
)

const (
	// steeringHost and edgeAddress are the fake deployment's DNS
	// targets: the name a subdomain is told to CNAME at, and the A
	// record's address (a documentation address, RFC 5737).
	steeringHost = "edge." + AppsDomain
	edgeAddress  = "192.0.2.1"
	// recordTTL is domainRecordTTL.
	recordTTL = 300
	// hostUnavailable is the attach door's one answer for a hostname
	// someone else holds, whoever it is.
	hostUnavailable = "hostname unavailable"
)

// reading is what an edge reported for a hostname: its DNS answer, when
// it checked, and the certificate's expiry in Unix seconds.
type reading struct {
	dnsStatus   string
	checkedAt   int64
	certExpires int64
}

// dnsRecord is the console's dnsRecord (domainstate.go:184-202).
type dnsRecord struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Value    string `json:"value"`
	TTL      int    `json:"ttl"`
	Set      string `json:"set"`
	Required bool   `json:"required,omitempty"`
}

// handleDomains adds the three domain doors, which take publish and
// instance (routedecls.go:860, 870, 888).
func (platform *Platform) handleDomains() {
	roles := []string{"publish", "instance"}
	const doors = "/api/cli/apps/{account}/{app}/domains"
	platform.mux.HandleFunc("POST "+doors,
		platform.door(roles, nil, platform.attachDomain))
	platform.mux.HandleFunc("GET "+doors+"/{host}",
		platform.door(roles, nil, platform.getDomain))
	platform.mux.HandleFunc("DELETE "+doors+"/{host}",
		platform.door(roles, nil, platform.detachDomain))
}

// claimRoute is how the edge answers for an attached hostname. No box
// materialises a pending claim (#215). An empty route serves the app
// itself, as Attach makes; any other serves that instance, if the
// claim's app owns it.
func (platform *Platform) claimRoute(
	found *claim, host, urlPath string,
) route {
	if found.pending {
		return route{}
	}
	// Path routing (edge.go:1281-1371, aliases.go:395-408): the target
	// must be a ready route of the same account on this box. Every other
	// case, an unlisted prefix included, serves the hostname's own route,
	// so a migration in progress falls back to the old app rather than
	// failing.
	if target, ok := pathTarget(found.paths, urlPath); ok {
		tenant := platform.instances[target]
		if tenant != nil && tenant.key.account == found.key.account &&
			!platform.otherBox[target] && tenant.pending == 0 {
			return platform.routeOf(tenant.key, target)
		}
	}
	if found.route == "" {
		return platform.routeOf(found.key, host)
	}
	served := platform.instances[found.route]
	if served == nil || served.key != found.key {
		return route{}
	}
	return platform.instanceRoute(served, served.rec.Host)
}

// DomainProof turns ownership proof on or off for attaches from now on,
// as CONSOLE_DOMAIN_PROOF does. It is off by default, as on the
// platform: every attach is then verified at once.
func (platform *Platform) DomainProof(on bool) {
	platform.mu.Lock()
	defer platform.mu.Unlock()
	platform.domainProof = on
}

// ProveDomain publishes the ownership TXT record for host, so the next
// attach of it is verified.
func (platform *Platform) ProveDomain(host string) {
	platform.mu.Lock()
	defer platform.mu.Unlock()
	platform.proven[host] = true
}

// DNSReady makes an edge report that host points at the deployment.
func (platform *Platform) DNSReady(host string) {
	platform.mu.Lock()
	defer platform.mu.Unlock()
	platform.reported(host).dnsStatus = "pointing"
}

// CertLive makes an edge report a certificate for host that expires at
// expiry. A certificate is issued only once DNS points here, so it
// reports that too.
func (platform *Platform) CertLive(host string, expiry time.Time) {
	platform.mu.Lock()
	defer platform.mu.Unlock()
	found := platform.reported(host)
	found.dnsStatus = "pointing"
	found.certExpires = expiry.Unix()
}

func (platform *Platform) reported(host string) *reading {
	found := platform.readings[host]
	if found == nil {
		found = &reading{}
		platform.readings[host] = found
	}
	found.checkedAt = time.Now().Unix()
	return found
}

// challenge is domainChallenge (domainpolicy.go:283-289) with
// domains.ChallengeToken (claims.go:619-622), bound to the fake
// console's host as the deployment.
func challenge(account, host string) dnsRecord {
	sum := sha256.Sum256([]byte("carlos-domain-challenge\x00" +
		consoleHost + "\x00" + account + "\x00" + host))
	return dnsRecord{
		Type: "TXT", Name: "_carlos-challenge." + host,
		Value: "carlos-verify=" + hex.EncodeToString(sum[:16]),
		TTL:   recordTTL, Set: "verify", Required: true,
	}
}

// attachDomain is handleAPIDomainAttach's alias branch
// (apidomains.go:303-678) with AttachClaimProven's rules (claims.go:
// 690-823): a new claim is pending until proved; another owner's
// verified claim, or its pending one against a caller who has not
// proved, is the neutral 409; the caller's own claim moves to the route
// it names and is verified once proved.
func (platform *Platform) attachDomain(
	response http.ResponseWriter, request *http.Request, ex *exchange,
	key appKey,
) {
	var in struct {
		Host  string `json:"host"`
		Route string `json:"route"`
	}
	if !readJSON(response, request, ex, &in) {
		return
	}
	host, ok := normalizeHost(in.Host)
	if !ok {
		jsonError(response, http.StatusBadRequest,
			"invalid hostname - attach a full hostname like "+
				"tickets.example.com")
		return
	}
	route, ok := platform.aliasRoute(response, key, in.Route)
	if !ok {
		return
	}
	current, created, ok := platform.claimHost(key, host, route)
	if !ok {
		jsonError(response, http.StatusConflict, hostUnavailable)
		return
	}
	answer := map[string]any{
		"host": host, "route": route, "created": created,
		"verified": !current.pending,
	}
	if current.pending {
		answer["verify"] = challenge(key.account, host)
	}
	writeJSON(response, http.StatusOK, answer)
}

// claimHost writes the claim, or reports false for a hostname someone
// else holds: another app's instance host, a verified claim, or a pending
// one while the caller has not proved.
func (platform *Platform) claimHost(
	key appKey, host, route string,
) (current *claim, created, ok bool) {
	if held, found := platform.instances[host]; found && held.key != key {
		return nil, false, false
	}
	proven := !platform.domainProof || platform.proven[host]
	current = platform.claims[host]
	switch {
	case current == nil:
	case current.key == key:
		current.route = route
		current.pending = current.pending && !proven
		return current, false, true
	case !current.pending || !proven:
		return nil, false, false
	}
	// A superseded reservation keeps nothing of its holder's.
	current = &claim{key: key, route: route, pending: !proven}
	platform.claims[host] = current
	return current, true, true
}

// aliasRoute is resolveAliasRoute (apidomains.go:218-243): a named route
// must be shaped like a hostname, and none names the app's one instance.
// Whether the route is the app's is the box's question, at serve time.
func (platform *Platform) aliasRoute(
	response http.ResponseWriter, key appKey, want string,
) (string, bool) {
	if want = strings.ToLower(strings.TrimSpace(want)); want != "" {
		if _, ok := normalizeHost(want); !ok {
			jsonError(response, http.StatusBadRequest,
				"invalid route hostname")
			return "", false
		}
		return want, true
	}
	hosts := platform.app(key).hosts
	if len(hosts) != 1 {
		jsonError(response, http.StatusBadRequest,
			"specify --route: the app has no single instance to "+
				"default to")
		return "", false
	}
	return hosts[0], true
}

// domainView is the per-host read's "domain" object (domainListItem,
// apidomains.go:1320-1385), less the wildcard and catch-all fields the
// fake has no use for.
type domainView struct {
	Host          string      `json:"host"`
	Route         string      `json:"route"`
	Status        string      `json:"status"`
	DNSStatus     string      `json:"dns_status,omitempty"`
	CertExpiresAt int64       `json:"cert_expires_at,omitempty"`
	Guidance      string      `json:"guidance,omitempty"`
	CNAMETarget   string      `json:"cname_target,omitempty"`
	Wildcard      bool        `json:"wildcard"`
	Catchall      bool        `json:"catchall"`
	State         string      `json:"state"`
	Reason        string      `json:"reason,omitempty"`
	Detail        string      `json:"detail,omitempty"`
	CheckedAt     int64       `json:"checked_at,omitempty"`
	Records       []dnsRecord `json:"records,omitempty"`
	Verified      bool        `json:"verified"`
}

// getDomain is handleAPIDomainGet (apidomains.go:1580-1675): a hostname
// this app does not hold is the same 404 as one nobody holds.
func (platform *Platform) getDomain(
	response http.ResponseWriter, request *http.Request, _ *exchange,
	key appKey,
) {
	host, ok := normalizeHost(request.PathValue("host"))
	if !ok {
		jsonError(response, http.StatusBadRequest, "invalid hostname")
		return
	}
	found := platform.claims[host]
	if found == nil || found.key != key {
		jsonError(response, http.StatusNotFound, "no such hostname")
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"domain": platform.domainView(host, found),
		"paths":  pathsOrEmpty(found.paths),
	})
}

// domainView is domainItem with claimState and claimDNSAdvice
// (apidomains.go:1495-1562, domainstate.go:171-180).
func (platform *Platform) domainView(host string, found *claim) domainView {
	view := domainView{
		Host: host, Route: found.route, Status: "pending",
		Verified: !found.pending,
	}
	observed, seen := platform.readings[host]
	if seen {
		view.Status = "live"
		view.DNSStatus = observed.dnsStatus
		view.CertExpiresAt = observed.certExpires
		view.CheckedAt = observed.checkedAt
	}
	view.State, view.Reason = claimState(found, observed, seen)
	if view.DNSStatus != "pointing" {
		view.Guidance = "Point a CNAME for " + host + " at " +
			steeringHost + ". The steering service chooses a healthy " +
			"edge. An A record for " + host + " at " + edgeAddress +
			" also works. Checks run every few minutes."
		view.Records = []dnsRecord{
			{"A", host, edgeAddress, recordTTL, "a", false},
		}
		if !apex(host) {
			view.CNAMETarget = steeringHost
			view.Records = append([]dnsRecord{
				{"CNAME", host, steeringHost, recordTTL, "cname", false},
			}, view.Records...)
		}
	}
	if found.pending {
		verify := challenge(found.key.account, host)
		view.Guidance = "Add a TXT record at " + verify.Name +
			" with the value " + verify.Value + " to prove you control " +
			"this hostname, then attach it again. " + view.Guidance
		view.Records = append(view.Records, verify)
	}
	return view
}

// claimState is the console's, for the readings the fake reports.
func claimState(
	found *claim, observed *reading, seen bool,
) (state, reason string) {
	switch {
	case found.pending:
		return "unverified", "ownership-unproven"
	case !seen:
		return "checking", "no-reading"
	case observed.dnsStatus != "pointing":
		return "pending-dns", "no-record"
	case observed.certExpires > time.Now().Unix():
		return "live", "serving"
	}
	return "issuing-cert", "awaiting-certificate"
}

// apex stands in for isApexDomain (view.go:229) without the public
// suffix list: a name of two labels is an apex.
func apex(host string) bool {
	return strings.Count(host, ".") < 2
}

// detachDomain is handleAPIDomainDetach (apidomains.go:1136-1306): it
// removes the app's own claim, and answers removed false for any other
// hostname, so the answer never says who holds it.
func (platform *Platform) detachDomain(
	response http.ResponseWriter, request *http.Request, _ *exchange,
	key appKey,
) {
	host, ok := normalizeHost(request.PathValue("host"))
	if !ok {
		jsonError(response, http.StatusBadRequest, "invalid hostname")
		return
	}
	found := platform.claims[host]
	removed := found != nil && found.key == key
	if removed {
		delete(platform.claims, host)
	}
	writeJSON(response, http.StatusOK,
		map[string]any{"host": host, "removed": removed})
}

func pathsOrEmpty(paths map[string]string) map[string]string {
	if paths == nil {
		return map[string]string{}
	}
	return paths
}
