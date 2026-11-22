package oauth_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/apitoken"
	"amadan.net/rastrillo/rastrillo/dbtest"
	"amadan.net/rastrillo/rastrillo/oauth"
	"amadan.net/rastrillo/rastrillo/sessions"
)

var tokenSchema = dbtest.FromSet(apitoken.Schema)

const (
	verifier  = "a-verifier-long-enough-for-pkce-0123456789abcdef"
	agentCB   = "https://agent.example/cb"
	queryCB   = "https://agent.example/q?tenant=a%20b&x=1"
	loopCB    = "http://127.0.0.1/cb"
	signInURL = "/signin"
)

func challenge() string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

type signIn struct {
	returnTo string
	reauth   bool
}

// flow drives the approval page through Mount, as a person's browser
// would, with a session, a clock and a directory the test controls.
type flow struct {
	t       *testing.T
	cfg     oauth.Config
	srv     *oauth.Server
	mux     *http.ServeMux
	now     time.Time
	session *sessions.Session
	allowed bool
	refusal string
	page    *oauth.Page
	signIns []signIn
	// logs holds what the server logged, so failures stay out of the
	// test output and a test can check them.
	logs *bytes.Buffer
}

func newFlow(
	t *testing.T, base string, fetcher *oauth.Fetcher,
	edits ...func(*oauth.Config),
) *flow {
	t.Helper()
	fl := &flow{t: t, now: t0, allowed: true, logs: &bytes.Buffer{}}
	fl.signInAs("ana")
	settings := config(t, base)
	settings.Logger = slog.New(slog.NewTextHandler(fl.logs, nil))
	settings.Fetcher = fetcher
	settings.Now = func() time.Time { return fl.now }
	settings.Session = func(*http.Request) (sessions.Session, bool) {
		if fl.session == nil {
			return sessions.Session{}, false
		}
		return *fl.session, true
	}
	settings.SignIn = func(returnTo string, reauth bool) string {
		fl.signIns = append(fl.signIns, signIn{returnTo, reauth})
		return signInURL
	}
	settings.Allowed = func(context.Context, string) (bool, string, error) {
		return fl.allowed, fl.refusal, nil
	}
	settings.Render = func(
		response http.ResponseWriter, _ *http.Request, page oauth.Page,
	) error {
		fl.page = &page
		_, err := response.Write([]byte(page.Kind))
		return err
	}
	err := settings.Store.SaveClient(context.Background(), oauth.Client{
		ID: "reg", Name: "Agent",
		Redirects:    []string{agentCB, queryCB, loopCB},
		RegisteredAt: t0, UsedAt: t0,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, edit := range edits {
		edit(&settings)
	}
	server, err := oauth.New(settings)
	if err != nil {
		t.Fatal(err)
	}
	fl.cfg, fl.srv, fl.mux = settings, server, http.NewServeMux()
	server.Mount(fl.mux)
	return fl
}

func (fl *flow) signInAs(subject string) {
	fl.session = &sessions.Session{
		Subject: subject, AuthTime: fl.now.Add(-time.Minute),
		At: fl.now,
	}
}

func params() url.Values {
	return url.Values{
		"response_type":         {"code"},
		"client_id":             {"reg"},
		"redirect_uri":          {agentCB},
		"code_challenge":        {challenge()},
		"code_challenge_method": {"S256"},
		"resource":              {"https://a.test/api/v1/mcp"},
		"scope":                 {"read write"},
		"state":                 {"xyz"},
	}
}

func (fl *flow) path() string { return fl.cfg.Base + "/oauth/authorize" }

func (fl *flow) authorize(query url.Values) *httptest.ResponseRecorder {
	fl.page = nil
	return fl.get(fl.path() + "?" + query.Encode())
}

func (fl *flow) get(target string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	fl.mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder
}

// show runs the GET and returns the approve page it rendered.
func (fl *flow) show(query url.Values) oauth.Page {
	fl.t.Helper()
	recorder := fl.authorize(query)
	if recorder.Code != http.StatusOK || fl.page == nil ||
		fl.page.Kind != "approve" {
		fl.t.Fatalf("GET: status %d, page %+v, Location %q", recorder.Code,
			fl.page, recorder.Header().Get("Location"))
	}
	return *fl.page
}

func (fl *flow) answer(
	pending, decision, access string, edits ...func(*http.Request),
) *httptest.ResponseRecorder {
	fl.page = nil
	form := url.Values{
		"pending": {pending}, "decision": {decision}, "access": {access},
	}
	request := httptest.NewRequest(http.MethodPost, fl.path(),
		strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	for _, edit := range edits {
		edit(request)
	}
	recorder := httptest.NewRecorder()
	fl.mux.ServeHTTP(recorder, request)
	return recorder
}

// redirected checks recorder is a 303 to the redirect and returns its query.
func (fl *flow) redirected(
	recorder *httptest.ResponseRecorder, redirect string,
) url.Values {
	fl.t.Helper()
	loc := recorder.Header().Get("Location")
	if recorder.Code != http.StatusSeeOther ||
		!strings.HasPrefix(loc, redirect+"?") &&
			!strings.HasPrefix(loc, redirect+"&") {
		fl.t.Fatalf("status %d, Location %q, want a redirect to %s",
			recorder.Code, loc, redirect)
	}
	parsed, err := url.Parse(loc)
	if err != nil {
		fl.t.Fatal(err)
	}
	query := parsed.Query()
	if query.Get("iss") != "https://a.test"+fl.cfg.Base {
		fl.t.Errorf("iss = %q", query.Get("iss"))
	}
	return query
}

// errorPage checks recorder rendered the error page with status and no
// redirect, and returns what the page says.
func (fl *flow) errorPage(
	recorder *httptest.ResponseRecorder,
	status int,
) string {
	fl.t.Helper()
	if recorder.Code != status || fl.page == nil || fl.page.Kind != "error" ||
		fl.page.Error == "" || recorder.Header().Get("Location") != "" {
		fl.t.Fatalf("status %d, page %+v, Location %q; want error page %d",
			recorder.Code, fl.page, recorder.Header().Get("Location"), status)
	}
	if cc := recorder.Header().Get("Cache-Control"); cc != "no-store" {
		fl.t.Errorf("Cache-Control = %q", cc)
	}
	return fl.page.Error
}

func (fl *flow) connections() []oauth.Connection {
	fl.t.Helper()
	cs, err := fl.cfg.Store.ConnectionsOf(context.Background(), "ana", fl.now)
	if err != nil {
		fl.t.Fatal(err)
	}
	return cs
}

func (fl *flow) noCode(recorder *httptest.ResponseRecorder) {
	fl.t.Helper()
	if loc := recorder.Header().Get("Location"); strings.Contains(
		loc,
		"code=",
	) {
		fl.t.Fatalf("issued a code: %s", loc)
	}
	if cs := fl.connections(); len(cs) != 0 {
		fl.t.Fatalf("made connections %+v", cs)
	}
}

func TestAuthorizeUnknownClientErrorPage(t *testing.T) {
	fl := newFlow(t, "", nil)
	for _, id := range []string{"", "nobody"} {
		query := params()
		query.Set("client_id", id)
		fl.errorPage(fl.authorize(query), http.StatusBadRequest)
	}
	values := params()
	values.Add("client_id", "reg")
	fl.errorPage(fl.authorize(values), http.StatusBadRequest)
}

func TestAuthorizeBadRedirectErrorPage(t *testing.T) {
	fetcher, base, _ := docServer(t, func(
		response http.ResponseWriter, _ *http.Request, id string,
	) {
		writeDoc(response, map[string]any{
			"client_id": id, "client_name": "Doc",
			"redirect_uris": []string{"http://evil.example/cb", agentCB},
		})
	})
	fl := newFlow(t, "", fetcher)
	tests := []struct{ client, redirect string }{
		{"reg", ""},
		{"reg", "https://agent.example/other"},
		{"reg", "https://agent.example/cb?x=1"},
		{"reg", "https://evil.example/cb"},
		// The document lists it, but it is not a safe redirect.
		{base + "/client.json", "http://evil.example/cb"},
	}
	for _, tt := range tests {
		query := params()
		query.Set("client_id", tt.client)
		query.Set("redirect_uri", tt.redirect)
		fl.errorPage(fl.authorize(query), http.StatusBadRequest)
	}
	values := params()
	values.Add("redirect_uri", agentCB)
	fl.errorPage(fl.authorize(values), http.StatusBadRequest)
	if len(fl.signIns) != 0 {
		t.Errorf("sign-in before the redirect was checked: %v", fl.signIns)
	}
}

// linkBack checks the error page links back to redirect, and returns
// the link's query.
func (fl *flow) linkBack(redirect string) url.Values {
	fl.t.Helper()
	page := fl.page
	if page == nil || page.ReturnHost != hostOf(fl.t, redirect) ||
		!strings.HasPrefix(page.ReturnURL, redirect+"?") &&
			!strings.HasPrefix(page.ReturnURL, redirect+"&") {
		fl.t.Fatalf("page %+v, want a link back to %s", page, redirect)
	}
	parsed, err := url.Parse(page.ReturnURL)
	if err != nil {
		fl.t.Fatal(err)
	}
	query := parsed.Query()
	if query.Get("iss") != "https://a.test"+fl.cfg.Base || query.Has("code") {
		fl.t.Errorf("link query %v", query)
	}
	return query
}

func hostOf(t *testing.T, raw string) string {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Hostname()
}

// refused checks the GET with query shows the error page, with a link back
// to the client that carries code: it never redirects by itself.
func refused(t *testing.T, fl *flow, query url.Values, code string) {
	t.Helper()
	fl.errorPage(fl.authorize(query), http.StatusBadRequest)
	got := fl.linkBack(query.Get("redirect_uri"))
	if got.Get("error") != code || got.Get("state") != query.Get("state") {
		t.Errorf("%v: link query %v, want error %s", query, got, code)
	}
}

func TestAuthorizeMissingPKCERedirectsError(t *testing.T) {
	fl := newFlow(t, "", nil)
	for _, edit := range []func(url.Values){
		func(query url.Values) { query.Del("code_challenge") },
		func(query url.Values) { query.Del("code_challenge_method") },
		func(query url.Values) { query.Set("code_challenge", "short") },
	} {
		query := params()
		edit(query)
		refused(t, fl, query, "invalid_request")
	}
}

func TestAuthorizePlainPKCERefused(t *testing.T) {
	fl := newFlow(t, "", nil)
	query := params()
	query.Set("code_challenge_method", "plain")
	query.Set("code_challenge", verifier)
	refused(t, fl, query, "invalid_request")
}

func TestAuthorizeResponseTypeRefused(t *testing.T) {
	fl := newFlow(t, "", nil)
	for _, rt := range []string{"", "token"} {
		query := params()
		query.Set("response_type", rt)
		refused(t, fl, query, "invalid_request")
	}
}

func TestAuthorizeForeignResourceRefused(t *testing.T) {
	fl := newFlow(t, "", nil)
	for _, res := range []string{
		"", "https://elsewhere.test/api/v1/mcp", "https://a.test/api/v2",
		"https://a.test/api/v1/mcp/",
	} {
		query := params()
		query.Set("resource", res)
		refused(t, fl, query, "invalid_target")
	}
}

func TestAuthorizeBadScopeRefused(t *testing.T) {
	fl := newFlow(t, "", nil)
	for _, scope := range []string{"admin", "read  write", "read admin"} {
		query := params()
		query.Set("scope", scope)
		refused(t, fl, query, "invalid_scope")
	}
}

func TestAuthorizeRequestedAccess(t *testing.T) {
	fl := newFlow(t, "", nil)
	for scope, want := range map[string]rastrillo.Access{
		"read": rastrillo.Read, "write": rastrillo.Write,
		"read write": rastrillo.Write, "write read": rastrillo.Write,
		"": rastrillo.Read,
	} {
		query := params()
		query.Set("scope", scope)
		if page := fl.show(query); page.Requested != want {
			t.Errorf("scope %q: requested %v, want %v",
				scope, page.Requested, want)
		}
	}
	values := params()
	values.Del("scope")
	if page := fl.show(values); page.Requested != rastrillo.Read {
		t.Errorf("no scope: requested %v", page.Requested)
	}
}

func TestAuthorizeSignsInFirst(t *testing.T) {
	fl := newFlow(t, "/t", nil)
	fl.session = nil
	query := params()
	query.Set("resource", "https://a.test/t/api/v1")
	recorder := fl.authorize(query)
	want := signIn{"/t/oauth/authorize?" + query.Encode(), false}
	if recorder.Code != http.StatusSeeOther ||
		recorder.Header().Get("Location") != signInURL ||
		len(fl.signIns) != 1 || fl.signIns[0] != want {
		t.Fatalf("status %d, Location %q, sign-ins %+v",
			recorder.Code, recorder.Header().Get("Location"), fl.signIns)
	}
	if fl.page != nil {
		t.Errorf("rendered %+v", fl.page)
	}
}

func TestAuthorizeStaleSignIn(t *testing.T) {
	fl := newFlow(t, "", nil)
	fl.session.AuthTime = fl.now.Add(-11 * time.Minute)
	query := params()
	recorder := fl.authorize(query)
	want := signIn{"/oauth/authorize?" + query.Encode(), true}
	if recorder.Code != http.StatusSeeOther ||
		recorder.Header().Get("Location") != signInURL ||
		len(fl.signIns) != 1 || fl.signIns[0] != want {
		t.Fatalf("status %d, sign-ins %+v", recorder.Code, fl.signIns)
	}
	fl.signIns = nil
	fl.session.AuthTime = fl.now.Add(-10 * time.Minute)
	fl.show(query)
}

func TestAuthorizeNotAllowed(t *testing.T) {
	fl := newFlow(t, "", nil)
	fl.allowed, fl.refusal = false, "Your account is suspended."
	recorder := fl.authorize(params())
	if got := fl.errorPage(recorder, http.StatusForbidden); got != fl.refusal {
		t.Errorf("error = %q", got)
	}
	fl.refusal = ""
	if got := fl.errorPage(fl.authorize(params()),
		http.StatusForbidden); got == "" {
		t.Error("no words for a refusal without its own")
	}
}

func TestAuthorizeShowsClient(t *testing.T) {
	fetcher, base, _ := docServer(t, goodDoc)
	fl := newFlow(t, "", fetcher)

	query := params()
	query.Set("client_id", base+"/client.json")
	page := fl.show(query)
	if page.SiteName != "A" || page.ClientName != "Agent" ||
		page.ClientDomain != "example.com" || page.SelfNamed || page.Loopback ||
		page.ReturnHost != "agent.example" ||
		page.Requested != rastrillo.Write || page.Pending == "" {
		t.Errorf("document client: %+v", page)
	}

	page = fl.show(params())
	if page.ClientName != "Agent" || page.ClientDomain != "" ||
		!page.SelfNamed ||
		page.Loopback ||
		page.ReturnHost != "agent.example" {
		t.Errorf("registered client: %+v", page)
	}

	query = params()
	query.Set("redirect_uri", "http://127.0.0.1:53111/cb")
	page = fl.show(query)
	if !page.Loopback || page.ReturnHost != "127.0.0.1" || !page.SelfNamed {
		t.Errorf("loopback: %+v", page)
	}
}

func TestApproveIssuesCode(t *testing.T) {
	fl := newFlow(t, "/t", nil)
	query := params()
	query.Set("resource", "https://a.test/t/api/v1")
	page := fl.show(query)
	got := fl.redirected(fl.answer(page.Pending, "approve", "write"), agentCB)
	code := got.Get("code")
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`).MatchString(code) ||
		got.Get("state") != "xyz" || got.Get("error") != "" {
		t.Fatalf("redirect query %v", got)
	}
	cs := fl.connections()
	if len(cs) != 1 {
		t.Fatalf("connections %+v", cs)
	}
	connection := cs[0]
	if len(connection.ID) != 22 || connection.Subject != "ana" ||
		connection.ClientID != "reg" ||
		connection.ClientName != "Agent" ||
		connection.ReturnHost != "agent.example" ||
		connection.Resource != "https://a.test/t/api/v1" ||
		connection.Access != rastrillo.Write ||
		!connection.Created.Equal(t0) ||
		!connection.LastUsed.Equal(t0) {
		t.Errorf("connection %+v", connection)
	}
	sum := sha256.Sum256([]byte(code))
	stored, used, err := fl.cfg.Store.TakeCode(
		context.Background(), sum[:], t0)
	if err != nil || used {
		t.Fatalf("code: %v, used %v", err, used)
	}
	if stored.Connection != connection.ID || stored.ClientID != "reg" ||
		stored.Redirect != agentCB || stored.Challenge != challenge() ||
		stored.Resource != connection.Resource ||
		!stored.Expires.Equal(t0.Add(time.Minute)) {
		t.Errorf("stored code %+v", stored)
	}
	again := fl.answer(page.Pending, "approve", "write")
	fl.errorPage(again, http.StatusBadRequest)
	if len(fl.connections()) != 1 {
		t.Error("a pending approval was used twice")
	}
}

// Anyone can register a redirect, so Deny ends here, not at the
// client: only a link carries access_denied back (RFC 9700 §4.11.2).
func TestDenyEndsOnLocalPage(t *testing.T) {
	fl := newFlow(t, "", nil)
	page := fl.show(params())
	recorder := fl.answer(page.Pending, "deny", "")
	fl.errorPage(recorder, http.StatusOK)
	got := fl.linkBack(agentCB)
	if got.Get("error") != "access_denied" || got.Get("state") != "xyz" {
		t.Errorf("link query %v", got)
	}
	fl.noCode(recorder)
	fl.errorPage(fl.answer(page.Pending, "approve", "read"),
		http.StatusBadRequest)
}

func TestApproveCannotRaiseAccess(t *testing.T) {
	fl := newFlow(t, "", nil)
	query := params()
	query.Set("scope", "read")
	page := fl.show(query)
	if page.Requested != rastrillo.Read {
		t.Fatalf("requested %v", page.Requested)
	}
	fl.redirected(fl.answer(page.Pending, "approve", "write"), agentCB)

	page = fl.show(params())
	fl.redirected(fl.answer(page.Pending, "approve", "read"), agentCB)

	for _, connection := range fl.connections() {
		if connection.Access != rastrillo.Read {
			t.Errorf("connection %s has %v", connection.ID, connection.Access)
		}
	}
	page = fl.show(params())
	fl.errorPage(fl.answer(page.Pending, "approve", "admin"),
		http.StatusBadRequest)
	fl.errorPage(fl.answer(page.Pending, "maybe", "read"),
		http.StatusBadRequest)
	fl.redirected(fl.answer(page.Pending, "approve", "write"), agentCB)
}

func TestApproveBoundToSubject(t *testing.T) {
	fl := newFlow(t, "", nil)
	page := fl.show(params())
	fl.signInAs("bob")
	recorder := fl.answer(page.Pending, "approve", "write")
	fl.errorPage(recorder, http.StatusBadRequest)
	fl.noCode(recorder)
	bobs, _ := fl.cfg.Store.ConnectionsOf(context.Background(), "bob", fl.now)
	if len(bobs) != 0 {
		t.Fatalf("bob got %+v", bobs)
	}
	fl.signInAs("ana")
	fl.redirected(fl.answer(page.Pending, "approve", "write"), agentCB)
}

func TestApprovePendingExpires(t *testing.T) {
	fl := newFlow(t, "", nil)
	page := fl.show(params())
	fl.now = fl.now.Add(10 * time.Minute)
	fl.signInAs("ana")
	recorder := fl.answer(page.Pending, "approve", "write")
	fl.errorPage(recorder, http.StatusBadRequest)
	fl.noCode(recorder)
	fl.errorPage(fl.answer("", "approve", "write"), http.StatusBadRequest)
}

func TestApproveRechecksFreshness(t *testing.T) {
	fl := newFlow(t, "", nil)
	query := params()
	page := fl.show(query)
	fl.now = fl.now.Add(9*time.Minute + 30*time.Second)
	recorder := fl.answer(page.Pending, "approve", "write")
	want := signIn{"/oauth/authorize?" + query.Encode(), true}
	if recorder.Code != http.StatusSeeOther ||
		recorder.Header().Get("Location") != signInURL ||
		len(fl.signIns) != 1 || fl.signIns[0] != want {
		t.Fatalf("status %d, Location %q, sign-ins %+v", recorder.Code,
			recorder.Header().Get("Location"), fl.signIns)
	}
	fl.noCode(recorder)

	fl.signInAs("ana")
	fl.page = nil
	recorder = fl.get(fl.signIns[0].returnTo)
	if recorder.Code != http.StatusOK || fl.page == nil ||
		fl.page.Kind != "approve" || fl.page.Pending == page.Pending {
		t.Fatalf("return: status %d, page %+v", recorder.Code, fl.page)
	}
	got := fl.redirected(fl.answer(fl.page.Pending, "approve", "write"),
		agentCB)
	if got.Get("code") == "" {
		t.Fatalf("no code: %v", got)
	}
}

func TestApproveRechecksAllowed(t *testing.T) {
	fl := newFlow(t, "", nil)
	page := fl.show(params())
	fl.allowed, fl.refusal = false, "Your account is suspended."
	recorder := fl.answer(page.Pending, "approve", "write")
	if got := fl.errorPage(recorder, http.StatusForbidden); got != fl.refusal {
		t.Errorf("error = %q", got)
	}
	fl.noCode(recorder)
}

func TestApproveRefusesBearer(t *testing.T) {
	fl := newFlow(t, "", nil)
	page := fl.show(params())
	tokens, err := apitoken.New(apitoken.Config{
		DB: tokenSchema.Open(t), Prefix: "a",
	})
	if err != nil {
		t.Fatal(err)
	}
	secret, _, err := tokens.Mint(context.Background(), "ana",
		apitoken.MintInput{Agent: "Claude", Access: rastrillo.Write})
	if err != nil {
		t.Fatal(err)
	}
	bearer := func(request *http.Request) {
		request.Header.Set("Authorization", "Bearer "+secret)
	}
	incoming := httptest.NewRequest(http.MethodGet, "/", nil)
	bearer(incoming)
	if _, err := tokens.Verify(context.Background(), incoming); err != nil {
		t.Fatalf("the token is not valid: %v", err)
	}
	fl.session = nil
	recorder := fl.answer(page.Pending, "approve", "write", bearer)
	if recorder.Code < 400 {
		t.Fatalf(
			"status %d, Location %q",
			recorder.Code,
			recorder.Header().Get("Location"),
		)
	}
	fl.noCode(recorder)
}

func TestApproveRefusesCrossSite(t *testing.T) {
	fl := newFlow(t, "", nil)
	page := fl.show(params())
	for _, edit := range []func(*http.Request){
		func(request *http.Request) {
			request.Header.Set("Sec-Fetch-Site",
				"cross-site")
		},
		func(request *http.Request) {
			request.Header.Del("Sec-Fetch-Site")
			request.Header.Set("Origin", "https://evil.test")
		},
		func(request *http.Request) { request.Header.Del("Sec-Fetch-Site") },
	} {
		recorder := fl.answer(page.Pending, "approve", "write", edit)
		fl.errorPage(recorder, http.StatusForbidden)
		fl.noCode(recorder)
	}
	answer := fl.answer(
		page.Pending,
		"approve",
		"write",
		func(request *http.Request) {
			request.Header.Del("Sec-Fetch-Site")
			request.Header.Set("Origin", "https://a.test")
		},
	)
	fl.redirected(answer, agentCB)
}

func TestRedirectKeepsQueryAndState(t *testing.T) {
	fl := newFlow(t, "", nil)
	const state = "a&b=c d+e%f ü/?#"
	query := params()
	query.Set("redirect_uri", queryCB)
	query.Set("state", state)
	page := fl.show(query)
	recorder := fl.answer(page.Pending, "approve", "write")
	got := fl.redirected(recorder, queryCB)
	if got.Get("state") != state || got.Get("tenant") != "a b" ||
		got.Get("x") != "1" || got.Get("code") == "" {
		t.Errorf("redirect query %v", got)
	}

	query.Set("scope", "admin")
	fl.errorPage(fl.authorize(query), http.StatusBadRequest)
	got = fl.linkBack(queryCB)
	if got.Get("state") != state || got.Get("error") != "invalid_scope" ||
		got.Get("tenant") != "a b" {
		t.Errorf("error link query %v", got)
	}

	query = params()
	query.Del("state")
	page = fl.show(query)
	fl.errorPage(fl.answer(page.Pending, "deny", ""), http.StatusOK)
	if _, ok := fl.linkBack(agentCB)["state"]; ok {
		t.Errorf("state added: %s", fl.page.ReturnURL)
	}
}

func TestAuthorizeRefusesFraming(t *testing.T) {
	fl := newFlow(t, "", nil)
	framed := func(recorder *httptest.ResponseRecorder, what string) {
		t.Helper()
		if got := recorder.Header().Get("X-Frame-Options"); got != "DENY" {
			t.Errorf("%s: X-Frame-Options %q", what, got)
		}
		csp := recorder.Header().Values("Content-Security-Policy")
		if !slices.Contains(csp, "frame-ancestors 'none'") {
			t.Errorf("%s: Content-Security-Policy %q", what, csp)
		}
	}
	answer := fl.authorize(params())
	framed(answer, "approve page")
	pending := fl.page.Pending
	query := params()
	query.Set("client_id", "nobody")
	framed(fl.authorize(query), "error page")
	framed(fl.answer(pending, "approve", "write", func(request *http.Request) {
		request.Header.Set("Sec-Fetch-Site", "cross-site")
	}), "POST error page")
}

func TestAuthorizeEmptyStateComesBack(t *testing.T) {
	fl := newFlow(t, "", nil)
	query := params()
	query.Set("state", "")
	page := fl.show(query)
	got := fl.redirected(fl.answer(page.Pending, "approve", "write"), agentCB)
	if values, ok := got["state"]; !ok || len(values) != 1 || values[0] != "" {
		t.Errorf("approve: state %q", values)
	}
	query.Set("scope", "admin")
	fl.errorPage(fl.authorize(query), http.StatusBadRequest)
	if values, ok := fl.linkBack(agentCB)["state"]; !ok || values[0] != "" {
		t.Errorf("error link: state %q", values)
	}
}

func TestAuthorizeLongStateRefused(t *testing.T) {
	fl := newFlow(t, "", nil)
	query := params()
	query.Set("state", strings.Repeat("s", 2048))
	fl.show(query)
	query.Set("state", strings.Repeat("s", 2049))
	refused(t, fl, query, "invalid_request")
}

func TestAuthorizeRepeatedParameterRefused(t *testing.T) {
	fl := newFlow(t, "", nil)
	query := params()
	query.Add("state", "again")
	fl.errorPage(fl.authorize(query), http.StatusBadRequest)
	if got := fl.linkBack(agentCB); got.Get("error") != "invalid_request" ||
		got.Has("state") {
		t.Errorf("link query %v", got)
	}
}

func TestAuthorizeDocumentFetchFails(t *testing.T) {
	fetcher, base, _ := docServer(t, func(
		response http.ResponseWriter, _ *http.Request, _ string,
	) {
		http.Error(response, "down", http.StatusInternalServerError)
	})
	fl := newFlow(t, "", fetcher)
	query := params()
	query.Set("client_id", base+"/client.json")
	fl.errorPage(fl.authorize(query), http.StatusBadRequest)
}

// brokenStore fails the methods given an error.
type brokenStore struct {
	oauth.Store
	client, save, create error
}

func (broken *brokenStore) Client(
	ctx context.Context, id string,
) (oauth.Client, error) {
	if broken.client != nil {
		return oauth.Client{}, broken.client
	}
	return broken.Store.Client(ctx, id)
}

func (broken *brokenStore) SavePending(
	ctx context.Context,
	pending oauth.Pending,
) error {
	if broken.save != nil {
		return broken.save
	}
	return broken.Store.SavePending(ctx, pending)
}

func (broken *brokenStore) CreateConnection(
	ctx context.Context, connection oauth.Connection, code oauth.Code,
) error {
	if broken.create != nil {
		return broken.create
	}
	return broken.Store.CreateConnection(ctx, connection, code)
}

func brokenFlow(t *testing.T) (*flow, *brokenStore) {
	broken := &brokenStore{}
	fl := newFlow(t, "", nil, func(config *oauth.Config) {
		broken.Store = config.Store
		config.Store = broken
		config.Logger = slog.New(slog.DiscardHandler)
	})
	return fl, broken
}

func TestAuthorizeStoreFails(t *testing.T) {
	fl, broken := brokenFlow(t)
	broken.client = errors.New("disk on fire")
	fl.errorPage(fl.authorize(params()), http.StatusInternalServerError)
	if fl.page.ReturnURL != "" {
		t.Errorf("linked back before the redirect was checked: %+v",
			fl.page)
	}
}

func TestAuthorizeSavePendingFails(t *testing.T) {
	fl, broken := brokenFlow(t)
	broken.save = errors.New("disk on fire")
	fl.errorPage(fl.authorize(params()), http.StatusInternalServerError)
	if got := fl.linkBack(agentCB); got.Get("error") != "server_error" ||
		got.Get("state") != "xyz" {
		t.Errorf("link query %v", got)
	}
}

func TestApproveCreateConnectionFails(t *testing.T) {
	fl, broken := brokenFlow(t)
	page := fl.show(params())
	broken.create = errors.New("disk on fire")
	recorder := fl.answer(page.Pending, "approve", "write")
	got := fl.redirected(recorder, agentCB)
	if got.Get("error") != "server_error" || got.Get("state") != "xyz" {
		t.Errorf("redirect query %v", got)
	}
	fl.noCode(recorder)
}

func TestApproveKeepsShownName(t *testing.T) {
	var down atomic.Bool
	fetcher, base, _ := docServer(t, func(
		response http.ResponseWriter, request *http.Request, id string,
	) {
		if down.Load() {
			http.Error(response, "down", http.StatusInternalServerError)
			return
		}
		goodDoc(response, request, id)
	})
	fl := newFlow(t, "", fetcher)
	query := params()
	query.Set("client_id", base+"/client.json")
	page := fl.show(query)
	down.Store(true)
	fl.redirected(fl.answer(page.Pending, "approve", "write"), agentCB)
	if cs := fl.connections(); len(cs) != 1 || cs[0].ClientName != "Agent" {
		t.Fatalf("connections %+v", cs)
	}
}

func TestRenderFails(t *testing.T) {
	var logged bytes.Buffer
	var write bool
	fl := newFlow(t, "", nil, func(config *oauth.Config) {
		config.Logger = slog.New(slog.NewTextHandler(&logged, nil))
		config.Render = func(
			response http.ResponseWriter, _ *http.Request, page oauth.Page,
		) error {
			if write {
				response.WriteHeader(http.StatusOK)
				_, _ = response.Write([]byte("half"))
			}
			return errors.New("template broke")
		}
	})
	recorder := fl.authorize(params())
	if recorder.Code != http.StatusInternalServerError ||
		!strings.Contains(logged.String(), "template broke") {
		t.Errorf("status %d, log %q", recorder.Code, logged.String())
	}
	write = true
	query := params()
	query.Set("client_id", "nobody")
	recorder = fl.authorize(query)
	if recorder.Code != http.StatusBadRequest ||
		recorder.Body.String() != "half" {
		t.Errorf(
			"after writing: status %d, body %q",
			recorder.Code,
			recorder.Body,
		)
	}
}
