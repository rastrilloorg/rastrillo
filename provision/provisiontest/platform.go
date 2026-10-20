// Package provisiontest is a fake Carlos console and edge for testing code
// that creates instances and publishes path maps through them.
//
// Every rule it applies was read from the platform's handlers at platform
// commit 04819f9b, and the plan's table (docs/superpowers/plans/
// 2026-10-17-provision.md, Task 2) cites the file and lines of each. A fake
// that guessed would let a readiness or routing bug in a caller pass, so
// when the platform changes, change that table, the rules here and this
// comment together.
//
// The fake is an http.RoundTripper that routes on the request's host: the
// console's origin goes to the console half, every other host to the edge
// half. There are no sockets and no TLS.
package provisiontest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"amadan.net/rastrillo/rastrillo/carlos"
)

// AppsDomain is the fake deployment's own domain. The real edge answers an
// unknown host under it with the pending page, because the host may have
// been created a moment ago and this edge not heard yet (edge.go
// unknownHost); any other unknown host is a plain 404.
const AppsDomain = "oncarlos.test"

const (
	consoleHost = "console.test"

	// maxPaths is domains.MaxPathsPerClaim.
	maxPaths = 10000
	// maxMessage is activityMessageMaxRunes.
	maxMessage = 240
)

// Reply is one scripted answer. Script and ScriptPaths queue them.
type Reply struct {
	// Status is the status to answer. A 202 carries the accepted-create
	// body (host, operation_id "op1", phase "creating") and no
	// Retry-After; any other status carries {"error": "scripted <status>"}.
	Status int
	// RetryAfter, when set, is the Retry-After header of the answer.
	RetryAfter string
	// Hang blocks until the request's context ends and returns its error,
	// so the client's own deadline fires. Status is then not used.
	Hang bool
	// Record applies the request first, as the platform does before an
	// answer that is lost, late or a 5xx: the instance (or path map) is
	// written, then the scripted answer goes out.
	Record bool
}

// Platform is the fake. Make one with New; it is safe for concurrent use.
type Platform struct {
	mu          sync.Mutex
	mux         *http.ServeMux
	creds       map[string]cred
	credN       int
	apps        map[appKey]*app
	instances   map[string]*instance
	claims      map[string]*claim
	otherBox    map[string]bool
	unready     int
	createReply []Reply
	pathsReply  []Reply
	calls       []string
}

type appKey struct{ account, app string }

type cred struct {
	appKey
	role string
}

type app struct {
	domain  string // empty until Enable: the app's instances.json
	version string
	serve   func(host string) http.Handler
	hosts   []string // instance hosts, in creation order
}

type instance struct {
	rec     record
	key     appKey
	pending int // requests still to answer with the pending page
}

type claim struct {
	key   appKey
	paths map[string]string
}

// New returns an empty platform.
func New() *Platform {
	p := &Platform{
		creds:     map[string]cred{},
		apps:      map[appKey]*app{},
		instances: map[string]*instance{},
		claims:    map[string]*claim{},
		otherBox:  map[string]bool{},
	}
	// The platform's own patterns (authcli.go:696-697, 826), and its own
	// roles at each door: a publish credential cannot create.
	p.mux = http.NewServeMux()
	p.mux.HandleFunc("POST /api/cli/apps/{account}/{app}/instances",
		p.door([]string{"instance"}, &p.createReply, p.create))
	p.mux.HandleFunc("GET /api/cli/apps/{account}/{app}/instances/{host}",
		p.door([]string{"instance"}, nil, p.get))
	p.mux.HandleFunc("PUT /api/cli/apps/{account}/{app}/domains/{host}/paths",
		p.door([]string{"publish", "instance"}, &p.pathsReply, p.putPaths))
	return p
}

// Client returns an http.Client whose transport is the platform. It has no
// timeout of its own: a caller's deadline is the thing under test.
func (p *Platform) Client() *http.Client { return &http.Client{Transport: p} }

// Console returns the console's origin.
func (p *Platform) Console() string { return "https://" + consoleHost }

func (p *Platform) app(k appKey) *app {
	a := p.apps[k]
	if a == nil {
		a = &app{}
		p.apps[k] = a
	}
	return a
}

// Enable turns instances on for the app, with domain as the one its
// instances' hosts sit under. It is what `carlos instances enable` writes.
func (p *Platform) Enable(account, app, domain string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.app(appKey{account, app}).domain = domain
}

// Credential returns a service credential bound to the app, with the role.
// The platform's service credentials are app-bound; its doors name the
// roles they accept.
func (p *Platform) Credential(account, app, role string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.credN++
	tok := fmt.Sprintf("cred-%d-%s-%s-%s", p.credN, account, app, role)
	p.creds[tok] = cred{appKey{account, app}, role}
	return tok
}

// Release sets the version the edge stamps in X-Carlos-Version on the app's
// routes.
func (p *Platform) Release(account, app, version string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.app(appKey{account, app}).version = version
}

// Serve sets what each of the app's instances runs. h is called with the
// host of the route serving the request.
func (p *Platform) Serve(account, app string, h func(host string) http.Handler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.app(appKey{account, app}).serve = h
}

// Attach gives the app a hostname of its own, whose route serves the app and
// can carry a path map.
func (p *Platform) Attach(account, app, host string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.claims[host] = &claim{key: appKey{account, app}}
}

// OtherBox homes the instance on another box. The instance's own host is
// still served, by backhaul, but this edge has no route for a path target
// there, so a path map that names it falls through to the hostname's own
// route.
func (p *Platform) OtherBox(host string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.otherBox[host] = true
}

// Unready makes every instance created from now on answer its first n edge
// requests with the pending page.
func (p *Platform) Unready(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.unready = n
}

// Script queues answers for the next creates, in order, before the real
// logic. Once they are spent, creates meet the real logic again.
func (p *Platform) Script(replies ...Reply) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.createReply = append(p.createReply, checkReplies(replies)...)
}

// ScriptPaths is Script for paths PUTs.
func (p *Platform) ScriptPaths(replies ...Reply) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pathsReply = append(p.pathsReply, checkReplies(replies)...)
}

// checkReplies refuses a reply that says nothing. A status of 0 would reach
// WriteHeader and panic inside whichever request met it, far from the
// Script call that is the mistake.
func checkReplies(replies []Reply) []Reply {
	for _, r := range replies {
		if !r.Hang && r.Status < 100 {
			panic(fmt.Sprintf("provisiontest: Reply %+v has no Status and does not Hang", r))
		}
	}
	return replies
}

// Instances returns the hosts of the app's instances, in creation order.
func (p *Platform) Instances(account, app string) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	a := p.apps[appKey{account, app}]
	if a == nil {
		return nil
	}
	return append([]string(nil), a.hosts...)
}

// Paths returns a copy of the path map published on a hostname.
func (p *Platform) Paths(host string) map[string]string {
	p.mu.Lock()
	defer p.mu.Unlock()
	c := p.claims[host]
	if c == nil {
		return nil
	}
	out := make(map[string]string, len(c.paths))
	for k, v := range c.paths {
		out[k] = v
	}
	return out
}

// Calls returns every request the platform has seen, oldest first. A
// console call is "POST /api/cli/apps/bdf/tenant/instances"; an edge
// request is "GET https://<host><path>". Neither carries a query.
func (p *Platform) Calls() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.calls...)
}

// RoundTrip answers the request from the console half or the edge half.
func (p *Platform) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	if hostOnly(req.URL.Host) == consoleHost {
		return p.roundTripConsole(req)
	}
	return p.roundTripEdge(req)
}

func hostOnly(h string) string {
	if name, _, err := net.SplitHostPort(h); err == nil {
		return name
	}
	return h
}

// ---- console ----

// exchange carries what a console handler needs besides the request: the
// body, read once so a scripted reply can read the host a create named, and
// the flag a Hang reply sets for roundTripConsole to act on after the lock
// is released.
type exchange struct {
	body []byte
	hang bool
}

type exchangeKey struct{}

func (p *Platform) roundTripConsole(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		var err error
		body, err = io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			return nil, err
		}
	}
	ex := &exchange{body: body}
	rec := httptest.NewRecorder()
	func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.calls = append(p.calls, req.Method+" "+req.URL.Path)
		p.mux.ServeHTTP(rec, req.WithContext(context.WithValue(req.Context(), exchangeKey{}, ex)))
	}()
	if ex.hang {
		// Outside the lock: a hung request must not stop the test from
		// reading Instances while the client's deadline runs down.
		<-req.Context().Done()
		return nil, req.Context().Err()
	}
	resp := rec.Result()
	resp.Request = req
	return resp, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// door is the platform's cliRoute plus the account and app gate
// (authcli.go:447-478, apicli.go:70-79). The order is the platform's: who
// you are, then whether this door takes your role, then whether the account
// and app are yours.
//
// A refusal that names the wrong thing changes what a caller does about it:
// an unknown account, another account's app and an app that is not yours
// are all the same bare 404, so nothing about them can be probed.
func (p *Platform) door(roles []string, script *[]Reply, h func(http.ResponseWriter, *http.Request, *exchange, appKey)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ex := r.Context().Value(exchangeKey{}).(*exchange)
		c, ok := p.creds[bearer(r)]
		if !ok {
			jsonError(w, http.StatusUnauthorized, "not signed in - run: carlos auth login --console "+p.Console())
			return
		}
		// admin passes every door the fake has: the platform builds each
		// one's set with svcrole.Allow, which adds Admin to whatever roles
		// it is given (internal/svcrole/svcrole.go:101-122; the doors at
		// internal/console/authcli.go:612-613, 708). Refusing it here would
		// fail an app's tests for a credential the platform takes.
		if c.role != "admin" && !contains(roles, c.role) {
			jsonError(w, http.StatusForbidden, fmt.Sprintf(
				"this credential holds role %q; this door needs one of %v", c.role, roles))
			return
		}
		key := appKey{r.PathValue("account"), r.PathValue("app")}
		if key != c.appKey {
			http.NotFound(w, r)
			return
		}
		if script != nil && len(*script) > 0 {
			rep := (*script)[0]
			*script = (*script)[1:]
			if rep.Record {
				// The write happens, and its answer is thrown away for the
				// scripted one.
				h(httptest.NewRecorder(), r, ex, key)
			}
			p.reply(w, r, ex, rep)
			return
		}
		h(w, r, ex, key)
	}
}

func (p *Platform) reply(w http.ResponseWriter, r *http.Request, ex *exchange, rep Reply) {
	if rep.Hang {
		ex.hang = true
		return
	}
	if rep.RetryAfter != "" {
		w.Header().Set("Retry-After", rep.RetryAfter)
	}
	if rep.Status == http.StatusAccepted {
		host := r.PathValue("host")
		if host == "" {
			var in struct{ Host string }
			json.Unmarshal(ex.body, &in)
			host = strings.TrimSpace(in.Host)
		}
		writeJSON(w, rep.Status, map[string]string{"host": host, "operation_id": "op1", "phase": "creating"})
		return
	}
	jsonError(w, rep.Status, fmt.Sprintf("scripted %d", rep.Status))
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) < 7 || !strings.EqualFold(h[:7], "bearer ") {
		return ""
	}
	return strings.TrimSpace(h[7:])
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// readJSON insists on a JSON content type before it reads anything
// (authcli.go:1117-1121); without it a cross-site form could drive the door.
func readJSON(w http.ResponseWriter, r *http.Request, ex *exchange, out any) bool {
	ct, _, _ := strings.Cut(r.Header.Get("Content-Type"), ";")
	if strings.TrimSpace(strings.ToLower(ct)) != "application/json" {
		jsonError(w, http.StatusUnsupportedMediaType, "send a JSON body with Content-Type: application/json")
		return false
	}
	if err := json.Unmarshal(ex.body, out); err != nil {
		jsonError(w, http.StatusBadRequest, "send a JSON body")
		return false
	}
	return true
}

type record struct {
	Schema    int       `json:"schema"`
	Host      string    `json:"host"`
	Slug      string    `json:"slug"`
	App       string    `json:"app"`
	Account   string    `json:"account"`
	Channel   string    `json:"channel"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by,omitempty"`
}

type recordView struct {
	record
	Gated bool `json:"gated"`
}

type servedBy struct {
	Box string `json:"box"`
}

type getView struct {
	record
	ServedBy []servedBy `json:"served_by,omitempty"`
	Gated    bool       `json:"gated"`
}

// create is handleAPIInstanceCreate (apiinstances.go:987-1723), in its
// order: the manifest, then the body, the message, the host, who holds the
// host, and last the write.
func (p *Platform) create(w http.ResponseWriter, r *http.Request, ex *exchange, key appKey) {
	a := p.app(key)
	if a.domain == "" {
		// No Retry-After: nothing a retry fixes (apiinstances.go:130-133).
		jsonError(w, http.StatusConflict, "instances are not enabled for "+key.app+
			" - an operator writes the app's instances.json (carlos instances enable)")
		return
	}
	var in struct {
		Host    string `json:"host"`
		Message string `json:"message"`
	}
	if !readJSON(w, r, ex, &in) {
		return
	}
	if !validMessage(in.Message) {
		jsonError(w, http.StatusBadRequest, "Message must be one line and 240 characters or fewer.")
		return
	}
	host := strings.TrimSpace(in.Host)
	if !validHost(host, a.domain) {
		jsonError(w, http.StatusBadRequest, "invalid host - an instance host is one DNS label followed by ."+a.domain)
		return
	}
	// Another app's instance or an attached hostname already has this host
	// (hostHolder and hostClaimHolder, apiinstances.go:1171-1189). The
	// app's own instance falls through to "already exists" below.
	if held, ok := p.instances[host]; ok && held.key != key {
		jsonError(w, http.StatusConflict, host+" is held by another app")
		return
	}
	if _, ok := p.claims[host]; ok {
		jsonError(w, http.StatusConflict, host+" is an attached hostname")
		return
	}
	if _, ok := p.instances[host]; ok {
		jsonError(w, http.StatusConflict, "instance "+host+" already exists")
		return
	}
	rec := record{
		Schema: 1, Host: host, Slug: strings.TrimSuffix(host, "."+a.domain),
		App: key.app, Account: key.account, Channel: "stable",
		CreatedAt: time.Now().UTC(), CreatedBy: "service:" + key.account + "/" + key.app,
	}
	p.instances[host] = &instance{rec: rec, key: key, pending: p.unready}
	a.hosts = append(a.hosts, host)
	writeJSON(w, http.StatusCreated, recordView{record: rec})
}

// get is handleAPIInstanceGet (apiinstances.go:859-902). A host with no
// record is the same bare 404 as no such app.
func (p *Platform) get(w http.ResponseWriter, r *http.Request, _ *exchange, key appKey) {
	in, ok := p.instances[r.PathValue("host")]
	if !ok || in.key != key {
		http.NotFound(w, r)
		return
	}
	box := "box1"
	if p.otherBox[in.rec.Host] {
		box = "box2"
	}
	writeJSON(w, http.StatusOK, getView{record: in.rec, ServedBy: []servedBy{{box}}})
}

// putPaths is handleAPIDomainPaths (apidomains.go:771-888): the whole map,
// for a hostname the app already holds. The map is validated before the
// claim is looked up, so a bad map is a 422 whoever holds the hostname.
func (p *Platform) putPaths(w http.ResponseWriter, r *http.Request, ex *exchange, key appKey) {
	host, ok := normalizeHost(r.PathValue("host"))
	if !ok {
		jsonError(w, http.StatusBadRequest, "invalid hostname")
		return
	}
	// A pointer, so an absent key is refused rather than read as an empty
	// map: the write is wholesale, and a misspelt key would clear every
	// prefix.
	var in struct {
		Paths *map[string]string `json:"paths"`
	}
	if !readJSON(w, r, ex, &in) {
		return
	}
	if in.Paths == nil {
		jsonError(w, http.StatusBadRequest,
			`send the whole prefix map as {"paths": {...}} - {"paths": {}} is how you clear it`)
		return
	}
	paths := *in.Paths
	if len(paths) > maxPaths {
		jsonError(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("a hostname may carry at most %d path prefixes", maxPaths))
		return
	}
	for _, prefix := range sortedKeys(paths) {
		if !validPrefix(prefix) {
			jsonError(w, http.StatusUnprocessableEntity,
				`each path prefix must be a single bare segment: "paulca", not "/paulca"`)
			return
		}
		if n, ok := normalizeHost(paths[prefix]); !ok || n != paths[prefix] {
			jsonError(w, http.StatusUnprocessableEntity, fmt.Sprintf(
				"path prefix %q: target %q is not a normalised hostname", prefix, paths[prefix]))
			return
		}
	}
	c := p.claims[host]
	if c == nil || c.key != key {
		http.NotFound(w, r)
		return
	}
	changed := !samePaths(c.paths, paths)
	if changed {
		c.paths = nil // an empty map and no map are one state
		if len(paths) > 0 {
			c.paths = make(map[string]string, len(paths))
			for k, v := range paths {
				c.paths[k] = v
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"host": host, "prefixes": len(paths), "changed": changed})
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func samePaths(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// validMessage is activity.go:27-44: one line of valid text, 240 runes at
// most.
func validMessage(m string) bool {
	if !utf8.ValidString(m) || utf8.RuneCountInString(m) > maxMessage {
		return false
	}
	for _, r := range m {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

var labelRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// validHost is provision.ValidHost (apiinstances.go:1089-1093): one DNS
// label followed by the app's domain.
func validHost(host, domain string) bool {
	if domain == "" || len(host) > 253 {
		return false
	}
	label, ok := strings.CutSuffix(host, "."+domain)
	return ok && labelRe.MatchString(label)
}

// normalizeHost is domains.NormalizeAliasHost (claims.go:635-642): lower
// case, one trailing dot dropped, every label a DNS label, and a dot inside.
func normalizeHost(raw string) (string, bool) {
	h := strings.TrimSuffix(strings.ToLower(raw), ".")
	if h == "" || len(h) > 253 || !strings.Contains(h, ".") {
		return "", false
	}
	for _, label := range strings.Split(h, ".") {
		if !labelRe.MatchString(label) {
			return "", false
		}
	}
	return h, true
}

// validPrefix is validPathPrefix (claims.go:738-755): a bare first segment.
func validPrefix(p string) bool {
	if p == "" || len(p) > 255 || p == "." || p == ".." {
		return false
	}
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-' || c == '_' || c == '.' || c == '~':
		default:
			return false
		}
	}
	return true
}

// ---- edge ----

// route is what the edge decided under the lock; the handler runs after it
// is released, so a handler that calls back into the platform cannot
// deadlock.
type route struct {
	serve   func(host string) http.Handler
	as      string // the host the handler is told it serves
	version string
	pending bool
	known   bool // some route answers; false is an unknown host
}

func (p *Platform) roundTripEdge(req *http.Request) (*http.Response, error) {
	host := hostOnly(req.URL.Host)
	p.mu.Lock()
	p.calls = append(p.calls, req.Method+" "+req.URL.Scheme+"://"+req.URL.Host+req.URL.Path)
	rt := p.resolve(host, req.URL.Path)
	p.mu.Unlock()

	rec := httptest.NewRecorder()
	switch {
	case rt.pending:
		pendingPage(rec, req)
	case !rt.known:
		http.Error(rec, "unknown host", http.StatusNotFound)
	default:
		var h http.Handler = http.NotFoundHandler()
		if rt.serve != nil {
			h = rt.serve(rt.as)
		}
		if rt.version != "" {
			rec.Header().Set("X-Carlos-Version", rt.version)
		}
		r := req.Clone(req.Context())
		r.Host = req.URL.Host
		r.RequestURI = req.URL.RequestURI()
		h.ServeHTTP(rec, r)
	}
	resp := rec.Result()
	resp.Request = req
	return resp, nil
}

// resolve is the edge's lookup, under the lock.
func (p *Platform) resolve(host, urlPath string) route {
	if in, ok := p.instances[host]; ok {
		// An instance homed on another box is still served on its own host:
		// this edge backhauls the request to the home box (edge.go, e.backhaul).
		// Only path routing is limited to this box, below. A fake that
		// stopped answering the host would make a tenant on another box look
		// unready, and hide the difference between "not up" and "up but not
		// reachable by the product path" that a caller's routing check reads.
		if in.pending > 0 {
			in.pending--
			return route{pending: true}
		}
		return p.routeOf(in.key, host)
	}
	if c, ok := p.claims[host]; ok {
		// Path routing (edge.go:1281-1371, aliases.go:395-408): the target
		// must be a ready route of the same account on this box. Every
		// other case, an unlisted prefix included, serves the hostname's
		// own route, so a migration in progress falls back to the old app
		// rather than failing.
		if target, ok := pathTarget(c.paths, urlPath); ok {
			if t := p.instances[target]; t != nil && t.key.account == c.key.account &&
				!p.otherBox[target] && t.pending == 0 {
				return p.routeOf(t.key, target)
			}
		}
		return p.routeOf(c.key, host)
	}
	if underAppsDomain(host) {
		return route{pending: true}
	}
	return route{}
}

func (p *Platform) routeOf(k appKey, as string) route {
	a := p.app(k)
	return route{serve: a.serve, as: as, version: a.version, known: true}
}

// underAppsDomain is the edge's underPlatformDomain (pending.go:198-209):
// only an exact lower-case host. "Acme.oncarlos.test" or one with a trailing
// dot keeps the plain unknown-host answer, so a visitor is never told a
// host is still being set up when it never will be.
func underAppsDomain(host string) bool {
	return host != "" && host == strings.ToLower(host) &&
		!strings.HasSuffix(host, ".") && strings.HasSuffix(host, "."+AppsDomain)
}

// pendingPage is pendingHost (pending.go:210-218). no-store, because the
// answer is about a moment and a cache holding it would keep a visitor on it
// after the host became servable.
func pendingPage(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Retry-After", "5")
	w.WriteHeader(http.StatusServiceUnavailable)
	if r.Method != http.MethodHead {
		io.WriteString(w, "<!doctype html><title>Setting up</title><p>It usually takes less than a minute. This page will try again on its own.")
	}
}

// pathTarget looks a request path up in a prefix map the way the edge does
// (edge.go:1346-1351): the first segment of the cleaned path first, then,
// if that misses, the raw first segment, and the cleaned one wins when both
// match. Either form alone leaves a bypass: "//acme/x" has an empty raw
// segment, and "/acme/../bob" cleans to "bob", so a flipped account could
// reach the old app by choosing the spelling the lookup does not see. The
// fake must agree, or a test of a caller's routing passes on a path the
// platform sends elsewhere.
func pathTarget(paths map[string]string, urlPath string) (string, bool) {
	clean := ""
	if urlPath != "" && urlPath[0] == '/' {
		clean = firstSegment(path.Clean(urlPath))
	}
	raw := firstSegment(urlPath)
	if t, ok := paths[clean]; ok {
		return t, true
	}
	if raw != clean {
		t, ok := paths[raw]
		return t, ok
	}
	return "", false
}

// firstSegment is the edge's: "/paulca/x" is "paulca", and "" for "/".
// Whole segments, so "/paulcarson" never matches "paulca".
func firstSegment(p string) string {
	if p == "" || p[0] != '/' {
		return ""
	}
	p = p[1:]
	if i := strings.IndexByte(p, '/'); i >= 0 {
		p = p[:i]
	}
	return p
}

// ---- apps the edge can serve ----

// Tenant returns the handler an app runs for each instance: GET /api/version
// answers 200 "test" and names the instance in the Rastrillo-Host header,
// as carlos.NameInstance does. With slug set, GET /<slug>/api/version
// answers the same, which is how a tenant built for path routing sees the
// request, because the edge forwards the path unchanged. Everything else is
// a 404.
func Tenant(slug string) func(host string) http.Handler {
	return func(host string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && (r.URL.Path == "/api/version" ||
				slug != "" && r.URL.Path == "/"+slug+"/api/version") {
				w.Header().Set(carlos.HostHeader, host)
				io.WriteString(w, "test")
				return
			}
			http.NotFound(w, r)
		})
	}
}

// Hub returns the handler of an app that is not path routed: GET
// /api/version answers as Tenant does, everything else is a 404.
func Hub() func(host string) http.Handler { return Tenant("") }
