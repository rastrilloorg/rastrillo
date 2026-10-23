package apitoken_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/apitoken"
	"amadan.net/rastrillo/rastrillo/sessions"
)

const tokensPath = "/account/tokens"

// page records what the handlers hand to Render, which is the whole of
// what the app's template can show.
type page struct {
	calls int
	last  apitoken.PageData
}

func (p *page) render(w http.ResponseWriter, r *http.Request, d apitoken.PageData) {
	p.calls++
	p.last = d
	w.Write([]byte("PAGE"))
}

func newPage(t *testing.T, mut func(*apitoken.HandlersConfig)) (*apitoken.Store, *apitoken.Handlers, *page) {
	t.Helper()
	s, _, _ := newStore(t, nil)
	p := &page{}
	cfg := apitoken.HandlersConfig{Store: s, Path: tokensPath, Render: p.render}
	if mut != nil {
		mut(&cfg)
	}
	h, err := apitoken.NewHandlers(cfg)
	if err != nil {
		t.Fatalf("NewHandlers: %v", err)
	}
	return s, h, p
}

func as(r *http.Request, subject string) *http.Request {
	if subject == "" {
		return r
	}
	return sessions.WithSession(r, sessions.Session{Subject: subject})
}

func get(subject string) *http.Request {
	return as(httptest.NewRequest("GET", tokensPath, nil), subject)
}

func post(path string, form url.Values, subject string) *http.Request {
	r := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return as(r, subject)
}

func do(h http.HandlerFunc, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

func create(h *apitoken.Handlers, subject string, kv ...string) *httptest.ResponseRecorder {
	form := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		form.Set(kv[i], kv[i+1])
	}
	return do(h.Create, post(tokensPath+"/create", form, subject))
}

func wantNoStore(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestNewHandlersRequiresStorePathAndRender(t *testing.T) {
	s, _, _ := newStore(t, nil)
	render := func(http.ResponseWriter, *http.Request, apitoken.PageData) {}
	for name, cfg := range map[string]apitoken.HandlersConfig{
		"no store":  {Path: tokensPath, Render: render},
		"no path":   {Store: s, Render: render},
		"no render": {Store: s, Path: tokensPath},
	} {
		if _, err := apitoken.NewHandlers(cfg); err == nil {
			t.Errorf("%s: NewHandlers accepted %+v", name, cfg)
		}
	}
}

func TestPageListsOnlyTheCallersTokens(t *testing.T) {
	s, h, p := newPage(t, func(c *apitoken.HandlersConfig) {
		c.Name = func(_ context.Context, subject string) string {
			if subject == "alice" {
				return "Alice"
			}
			return ""
		}
	})
	_, mine := mint(t, s, "alice", claude)
	_, own := mint(t, s, "alice", apitoken.MintInput{ForSelf: true, Access: rastrillo.Read})
	mint(t, s, "bob", claude)

	w := do(h.Page, get("alice"))
	if w.Code != http.StatusOK {
		t.Fatalf("Page = %d, want 200", w.Code)
	}
	wantNoStore(t, w)
	if p.calls != 1 {
		t.Fatalf("Render called %d times, want 1", p.calls)
	}
	d := p.last
	if len(d.Tokens) != 2 {
		t.Fatalf("Page listed %d tokens, want alice's 2: %+v", len(d.Tokens), d.Tokens)
	}
	got := map[string]apitoken.Row{}
	for _, row := range d.Tokens {
		if row.Subject != "alice" {
			t.Errorf("Page listed %s's token %s", row.Subject, row.ID)
		}
		got[row.ID] = row
	}
	if got[mine.ID].Display != "Claude for Alice" {
		t.Errorf("agent row Display = %q, want %q", got[mine.ID].Display, "Claude for Alice")
	}
	if got[own.ID].Display != "Alice" {
		t.Errorf("own-token row Display = %q, want %q", got[own.ID].Display, "Alice")
	}
	if d.Created != nil {
		t.Errorf("Page set Created = %+v on a GET", d.Created)
	}
	if len(d.Errors) != 0 {
		t.Errorf("Page set Errors = %v on a GET", d.Errors)
	}
	if d.Form["access"] != "write" || d.Form["expiry"] != "90" {
		t.Errorf("Page Form = %v, want access=write expiry=90 preselected", d.Form)
	}
	if !d.CSRFOK {
		t.Error("Page CSRFOK = false, want true")
	}
}

func TestPageNamesTheSubjectWithoutName(t *testing.T) {
	s, h, p := newPage(t, nil)
	mint(t, s, "alice", claude)
	do(h.Page, get("alice"))
	if len(p.last.Tokens) != 1 || p.last.Tokens[0].Display != "Claude for alice" {
		t.Errorf("Tokens = %+v, want one row shown as %q", p.last.Tokens, "Claude for alice")
	}
}

// Behind the Locales middleware the row is worded in the request's
// language, and the form error with it. The wording comes from an app
// catalog, because the shipped es.toml holds the English placeholder
// until the translations land and would make this test say nothing.
func TestPageSpeaksTheRequestsLanguage(t *testing.T) {
	s, h, p := newPage(t, nil)
	mint(t, s, "alice", claude)
	app := fstest.MapFS{"locales/es.toml": {Data: []byte("rastrillo.ui.agent_for = \"{agent} para {person}\"\n" +
		"rastrillo.ui.api_tokens_error_agent = \"Ponle nombre al agente.\"\n")}}
	es, err := rastrillo.NewLocales([]string{"en", "es"}, "en", rastrillo.BaseCatalog(), app)
	if err != nil {
		t.Fatal(err)
	}
	es.Middleware(http.HandlerFunc(h.Page)).ServeHTTP(httptest.NewRecorder(), as(httptest.NewRequest("GET", "/es"+tokensPath, nil), "alice"))
	if len(p.last.Tokens) != 1 || p.last.Tokens[0].Display != "Claude para alice" {
		t.Errorf("Tokens on /es = %+v, want one row shown as %q", p.last.Tokens, "Claude para alice")
	}
	form := url.Values{"access": {"write"}, "expiry": {"90"}}
	es.Middleware(http.HandlerFunc(h.Create)).ServeHTTP(httptest.NewRecorder(), post("/es"+tokensPath+"/create", form, "alice"))
	if got := p.last.Errors["agent"]; got != "Ponle nombre al agente." {
		t.Errorf("Errors[agent] on /es = %q, want the Spanish", got)
	}
}

func TestCreateRevealsTheSecretOnceInThePageItself(t *testing.T) {
	s, h, p := newPage(t, nil)
	w := create(h, "alice", "agent", "Claude", "access", "write", "expiry", "90")
	if w.Code != http.StatusOK {
		t.Fatalf("Create = %d, want 200 (the page, with the reveal)", w.Code)
	}
	wantNoStore(t, w)
	if p.calls != 1 {
		t.Fatalf("Render called %d times, want 1", p.calls)
	}
	d := p.last
	if d.Created == nil {
		t.Fatal("Create rendered without Created: the person never sees the secret")
	}
	if !grammar.MatchString(d.Created.Secret) {
		t.Errorf("Created.Secret = %q, want a token matching %s", d.Created.Secret, grammar)
	}
	if d.Created.Token.Agent != "Claude" || d.Created.Token.Access != rastrillo.Write || d.Created.Token.Subject != "alice" {
		t.Errorf("Created.Token = %+v, want alice's write token for Claude", d.Created.Token)
	}
	if d.Created.Token.Display != "Claude for alice" {
		t.Errorf("Created.Token.Display = %q, want %q", d.Created.Token.Display, "Claude for alice")
	}
	if want := t0.Add(90 * 24 * time.Hour); d.Created.Token.ExpiresAt == nil || !d.Created.Token.ExpiresAt.Equal(want) {
		t.Errorf("Created.Token.ExpiresAt = %v, want %v", d.Created.Token.ExpiresAt, want)
	}
	if len(d.Tokens) != 1 || d.Tokens[0].ID != d.Created.Token.ID {
		t.Errorf("Tokens = %+v, want the new token listed", d.Tokens)
	}
	if len(d.Errors) != 0 {
		t.Errorf("Errors = %v on a successful create", d.Errors)
	}
	if d.Form["agent"] != "" || d.Form["access"] != "write" || d.Form["expiry"] != "90" {
		t.Errorf("Form = %v after a create, want it back at the defaults", d.Form)
	}
	if loc := w.Header().Get("Location"); loc != "" {
		t.Errorf("Create redirected to %q; the secret must be shown in this response", loc)
	}
	for k, vs := range w.Header() {
		for _, v := range vs {
			if strings.Contains(v, d.Created.Secret) {
				t.Errorf("header %s carries the secret", k)
			}
		}
	}
	if cs := w.Result().Cookies(); len(cs) != 0 {
		t.Errorf("Create set cookies %v; the secret must not be stored", cs)
	}
	if _, err := verify(s, bearer(d.Created.Secret)); err != nil {
		t.Errorf("the revealed token does not verify: %v", err)
	}
}

// Mint has inserted the row before the page lists the caller's tokens.
// If that list then fails, a 500 would leave a live token whose secret
// nobody saw and nobody can recover. The reveal must survive the list.
// The failure here is a row List cannot read, which is the one way to
// make List fail after Mint succeeded on the same database.
func TestCreateKeepsTheRevealWhenTheListFails(t *testing.T) {
	s, d, _ := newStore(t, nil)
	p := &page{}
	h, err := apitoken.NewHandlers(apitoken.HandlersConfig{Store: s, Path: tokensPath, Render: p.render})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO api_tokens (id, subject, agent, access, secret_hash, created_at)
		VALUES ('damagedrow00', 'alice', 'Old', 'write', X'00', 'not a time')`); err != nil {
		t.Fatal(err)
	}
	w := create(h, "alice", "agent", "Claude", "access", "write", "expiry", "90")
	if w.Code != http.StatusOK {
		t.Fatalf("Create = %d, want 200: the token was minted, so its secret must be shown", w.Code)
	}
	if p.calls != 1 || p.last.Created == nil {
		t.Fatalf("Render called %d times, Created = %+v; want the reveal", p.calls, p.last.Created)
	}
	if _, err := verify(s, bearer(p.last.Created.Secret)); err != nil {
		t.Errorf("the revealed token does not verify: %v", err)
	}
	if len(p.last.Tokens) != 1 || p.last.Tokens[0].ID != p.last.Created.Token.ID {
		t.Errorf("Tokens = %+v, want the new token alone when the list cannot be read", p.last.Tokens)
	}
}

func TestCreateForSelfMintsWithoutAnAgent(t *testing.T) {
	_, h, p := newPage(t, nil)
	w := create(h, "alice", "for_self", "on", "access", "read", "expiry", "90")
	if w.Code != http.StatusOK || p.last.Created == nil {
		t.Fatalf("Create = %d, Created = %+v; want 200 with a token", w.Code, p.last.Created)
	}
	if tok := p.last.Created.Token; tok.Agent != "" || tok.Access != rastrillo.Read {
		t.Errorf("Created.Token = %+v, want alice's own read token", tok)
	}
	if p.last.Created.Token.Display != "alice" {
		t.Errorf("Display = %q, want the person alone", p.last.Created.Token.Display)
	}
}

func TestCreateExpiry(t *testing.T) {
	_, h, p := newPage(t, nil)
	create(h, "alice", "agent", "Claude", "access", "write", "expiry", "never")
	if p.last.Created == nil {
		t.Fatal("Create with expiry=never minted nothing")
	}
	if exp := p.last.Created.Token.ExpiresAt; exp != nil {
		t.Errorf("expiry=never gave ExpiresAt %v, want nil", exp)
	}
	create(h, "alice", "agent", "Claude", "access", "write", "expiry", "30")
	if p.last.Created == nil {
		t.Fatal("Create with expiry=30 minted nothing")
	}
	if want := t0.Add(30 * 24 * time.Hour); p.last.Created.Token.ExpiresAt == nil || !p.last.Created.Token.ExpiresAt.Equal(want) {
		t.Errorf("expiry=30 gave ExpiresAt %v, want %v", p.last.Created.Token.ExpiresAt, want)
	}
}

func TestCreateRefusesAnInvalidAgent(t *testing.T) {
	s, h, p := newPage(t, nil)
	for _, agent := range []string{"", "   ", "Cl@ude", strings.Repeat("a", 41)} {
		p.calls = 0
		w := create(h, "alice", "agent", agent, "access", "write", "expiry", "90")
		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("agent %q: Create = %d, want 422", agent, w.Code)
		}
		wantNoStore(t, w)
		if p.calls != 1 {
			t.Errorf("agent %q: Render called %d times, want 1", agent, p.calls)
			continue
		}
		if p.last.Errors["agent"] == "" {
			t.Errorf("agent %q: Errors = %v, want one under \"agent\"", agent, p.last.Errors)
		}
		if p.last.Created != nil {
			t.Errorf("agent %q: Created = %+v, want nil", agent, p.last.Created)
		}
		if p.last.Form["agent"] != agent {
			t.Errorf("agent %q: Form echoes %q", agent, p.last.Form["agent"])
		}
	}
	if list, _ := s.List(context.Background(), "alice"); len(list) != 0 {
		t.Errorf("an invalid agent minted %+v", list)
	}
}

func TestCreateRefusesTamperedAccessAndExpiry(t *testing.T) {
	s, h, _ := newPage(t, nil)
	for _, kv := range [][]string{
		{"agent", "Claude", "access", "admin", "expiry", "90"},
		{"agent", "Claude", "access", "", "expiry", "90"},
		{"agent", "Claude", "access", "write", "expiry", "7"},
		{"agent", "Claude", "access", "write", "expiry", ""},
	} {
		if w := create(h, "alice", kv...); w.Code != http.StatusBadRequest {
			t.Errorf("%v: Create = %d, want 400", kv, w.Code)
		}
	}
	if list, _ := s.List(context.Background(), "alice"); len(list) != 0 {
		t.Errorf("a tampered form minted %+v", list)
	}
}

func TestCreateIgnoresAForgedSubject(t *testing.T) {
	s, h, p := newPage(t, nil)
	create(h, "alice", "subject", "bob", "agent", "Claude", "access", "write", "expiry", "90")
	if p.last.Created == nil {
		t.Fatal("Create minted nothing")
	}
	if p.last.Created.Token.Subject != "alice" {
		t.Errorf("minted for %q, want the session's alice", p.last.Created.Token.Subject)
	}
	if list, _ := s.List(context.Background(), "bob"); len(list) != 0 {
		t.Errorf("a forged subject field gave bob %+v", list)
	}
}

func TestCreateIsPostOnly(t *testing.T) {
	s, h, _ := newPage(t, nil)
	// A GET with the fields in the query would let a link mint a token,
	// and csrf.Protect only guards POST.
	r := as(httptest.NewRequest("GET", tokensPath+"/create?agent=Claude&access=write&expiry=never", nil), "alice")
	if w := do(h.Create, r); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET Create = %d, want 405", w.Code)
	}
	if list, _ := s.List(context.Background(), "alice"); len(list) != 0 {
		t.Errorf("a GET minted %+v", list)
	}
}

func TestRevokeIsBoundToTheOwner(t *testing.T) {
	s, h, _ := newPage(t, nil)
	bobSecret, bobs := mint(t, s, "bob", claude)
	aliceSecret, alices := mint(t, s, "alice", claude)

	w := do(h.Revoke, post(tokensPath+"/revoke", url.Values{"id": {bobs.ID}}, "alice"))
	if w.Code != http.StatusNotFound {
		t.Errorf("Revoke of bob's token as alice = %d, want 404", w.Code)
	}
	wantNoStore(t, w)
	if _, err := verify(s, bearer(bobSecret)); err != nil {
		t.Errorf("alice revoked bob's token: %v", err)
	}

	w = do(h.Revoke, post(tokensPath+"/revoke", url.Values{"id": {alices.ID}}, "alice"))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != tokensPath {
		t.Errorf("Revoke = %d to %q, want 303 to %s", w.Code, w.Header().Get("Location"), tokensPath)
	}
	wantNoStore(t, w)
	if _, err := verify(s, bearer(aliceSecret)); err == nil {
		t.Error("alice's token still verifies after Revoke")
	}
}

func TestRevokeAgentAndRevokeAllTouchOnlyTheCaller(t *testing.T) {
	s, h, _ := newPage(t, nil)
	bobSecret, _ := mint(t, s, "bob", claude)
	claudeSecret, _ := mint(t, s, "alice", claude)
	codexSecret, _ := mint(t, s, "alice", apitoken.MintInput{Agent: "Codex", Access: rastrillo.Read})

	w := do(h.RevokeAgent, post(tokensPath+"/revoke-agent", url.Values{"agent": {"Claude"}}, "alice"))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != tokensPath {
		t.Errorf("RevokeAgent = %d to %q, want 303 to %s", w.Code, w.Header().Get("Location"), tokensPath)
	}
	wantNoStore(t, w)
	if _, err := verify(s, bearer(claudeSecret)); err == nil {
		t.Error("alice's Claude token still verifies after RevokeAgent")
	}
	if _, err := verify(s, bearer(codexSecret)); err != nil {
		t.Errorf("RevokeAgent(Claude) took the Codex token: %v", err)
	}
	if _, err := verify(s, bearer(bobSecret)); err != nil {
		t.Errorf("RevokeAgent as alice took bob's token: %v", err)
	}

	if w := do(h.RevokeAgent, post(tokensPath+"/revoke-agent", url.Values{"agent": {""}}, "alice")); w.Code != http.StatusBadRequest {
		t.Errorf("RevokeAgent with no name = %d, want 400", w.Code)
	}

	w = do(h.RevokeAll, post(tokensPath+"/revoke-all", nil, "alice"))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != tokensPath {
		t.Errorf("RevokeAll = %d to %q, want 303 to %s", w.Code, w.Header().Get("Location"), tokensPath)
	}
	wantNoStore(t, w)
	if _, err := verify(s, bearer(codexSecret)); err == nil {
		t.Error("alice's Codex token still verifies after RevokeAll")
	}
	if _, err := verify(s, bearer(bobSecret)); err != nil {
		t.Errorf("RevokeAll as alice took bob's token: %v", err)
	}
}

func TestNoSessionIs403Everywhere(t *testing.T) {
	s, h, p := newPage(t, nil)
	secret, tok := mint(t, s, "alice", claude)
	for name, w := range map[string]*httptest.ResponseRecorder{
		"Page":        do(h.Page, get("")),
		"Create":      create(h, "", "agent", "Claude", "access", "write", "expiry", "90"),
		"Revoke":      do(h.Revoke, post(tokensPath+"/revoke", url.Values{"id": {tok.ID}}, "")),
		"RevokeAgent": do(h.RevokeAgent, post(tokensPath+"/revoke-agent", url.Values{"agent": {"Claude"}}, "")),
		"RevokeAll":   do(h.RevokeAll, post(tokensPath+"/revoke-all", nil, "")),
	} {
		if w.Code != http.StatusForbidden {
			t.Errorf("%s without a session = %d, want 403", name, w.Code)
		}
		if got := w.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s without a session: Cache-Control = %q, want no-store", name, got)
		}
	}
	if p.calls != 0 {
		t.Errorf("Render called %d times with no session", p.calls)
	}
	if _, err := verify(s, bearer(secret)); err != nil {
		t.Errorf("a sessionless request changed alice's token: %v", err)
	}
	if list, _ := s.List(context.Background(), "alice"); len(list) != 1 {
		t.Errorf("a sessionless Create minted: %+v", list)
	}
}

// Each row says whether it is revoked or expired, against the store's
// clock, and Agents is each agent name with a live token, once, in
// list order. Both used to be worked out by every app's render
// function; a template has no clock, so the page carries the answer.
// Row.Actor is the token's actor, credential and all, for the actor
// partial.
func TestPageMarksRowStateAndListsLiveAgents(t *testing.T) {
	s, _, c := newStore(t, nil)
	p := &page{}
	h, err := apitoken.NewHandlers(apitoken.HandlersConfig{Store: s, Path: tokensPath, Render: p.render})
	if err != nil {
		t.Fatalf("NewHandlers: %v", err)
	}
	_, old := mint(t, s, "alice", apitoken.MintInput{Agent: "Codex", Access: rastrillo.Read, TTL: time.Hour})
	_, gone := mint(t, s, "alice", apitoken.MintInput{Agent: "Nightly import", Access: rastrillo.Write})
	if err := s.Revoke(context.Background(), "alice", gone.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	c.t = c.t.Add(2 * time.Hour) // Codex's token has expired by now
	_, live := mint(t, s, "alice", claude)
	mint(t, s, "alice", claude) // a second Claude token: one agent, listed once
	_, own := mint(t, s, "alice", apitoken.MintInput{ForSelf: true, Access: rastrillo.Read})

	w := do(h.Page, get("alice"))
	if w.Code != http.StatusOK {
		t.Fatalf("Page = %d, want 200", w.Code)
	}
	d := p.last
	state := map[string][2]bool{}
	for _, row := range d.Tokens {
		state[row.ID] = [2]bool{row.Revoked, row.Expired}
	}
	for id, want := range map[string][2]bool{old.ID: {false, true}, gone.ID: {true, false}, live.ID: {false, false}, own.ID: {false, false}} {
		if state[id] != want {
			t.Errorf("token %s: revoked, expired = %v, want %v", id, state[id], want)
		}
	}
	if len(d.Agents) != 1 || d.Agents[0] != "Claude" {
		t.Errorf("Agents = %q, want [Claude]: the expired and revoked agents have no live token, and Claude is listed once", d.Agents)
	}
	want := rastrillo.Actor{Subject: "alice", Agent: "Claude", Credential: live.ID, Access: rastrillo.Write}
	for _, row := range d.Tokens {
		if row.ID == live.ID && row.Actor() != want {
			t.Errorf("row.Actor() = %+v, want %+v", row.Actor(), want)
		}
	}
}
