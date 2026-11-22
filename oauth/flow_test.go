package oauth_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/api"
	"amadan.net/rastrillo/rastrillo/apitoken"
	"amadan.net/rastrillo/rastrillo/mcp"
	"amadan.net/rastrillo/rastrillo/oauth"
	"amadan.net/rastrillo/rastrillo/op"
	"amadan.net/rastrillo/rastrillo/sessions"
)

const (
	sessionCookie = "test-session"
	bigAnswer     = 200 << 10
	listCall      = `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	createCall    = `{"jsonrpc":"2.0","id":2,"method":"tools/call",` +
		`"params":{"name":"notes_create","arguments":{"title":"x"}}}`
	dumpCall = `{"jsonrpc":"2.0","id":3,"method":"tools/call",` +
		`"params":{"name":"notes_dump","arguments":{}}}`
)

type (
	noteTitle struct {
		Title string `json:"title"`
	}
	noteList struct {
		Notes []string `json:"notes"`
	}
	noteDump struct {
		Text string `json:"text"`
	}
)

// flowApp is a whole app on a real TLS server, as a client meets it:
// the JSON door and the MCP door behind apitoken, and oauth mounted
// beside them. The agent and the person's browser are separate
// clients, and only the browser is signed in.
type flowApp struct {
	t       *testing.T
	srv     *httptest.Server
	oauth   *oauth.Server
	agent   *http.Client
	browser *http.Client
	// onGrant runs after each call to the server's Grant.
	onGrant func(ctx context.Context, connection string)

	mu     sync.Mutex
	writer rastrillo.Actor
}

func newFlowApp(t *testing.T, fetcher *oauth.Fetcher) *flowApp {
	t.Helper()
	app := &flowApp{t: t}
	mux := http.NewServeMux()
	app.srv = httptest.NewUnstartedServer(mux)
	// An answer cut mid-body aborts its connection on purpose.
	app.srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	app.srv.StartTLS()
	t.Cleanup(app.srv.Close)
	quiet := slog.New(slog.DiscardHandler)
	tokens, err := apitoken.New(apitoken.Config{
		DB: tokenSchema.Open(t), Prefix: "a", Grant: app.grant,
	})
	if err != nil {
		t.Fatal(err)
	}
	app.oauth, err = oauth.New(oauth.Config{
		Store: newStore(t), Tokens: tokens, Origin: app.srv.URL,
		SiteName: "Notes", Session: signedIn,
		SignIn: func(string, bool) string { return "/signin" },
		Allowed: func(context.Context, string) (bool, string, error) {
			return true, "", nil
		},
		Render: renderJSON, Fetcher: fetcher, Logger: quiet,
	})
	if err != nil {
		t.Fatal(err)
	}
	app.oauth.Mount(mux)
	cfg := api.Config{
		Verify: tokens,
		Ctx: func(*http.Request) *rastrillo.Ctx {
			return &rastrillo.Ctx{}
		},
		Authorize: op.DefaultPolicy, Logger: quiet, Redact: tokens.Redact,
		RequireAgentSession: true,
		Challenge:           app.oauth.Challenge("/api/v1"),
	}
	ops := app.operations()
	tree, err := api.New(cfg, api.Operations(ops...)...)
	if err != nil {
		t.Fatal(err)
	}
	gate := cfg.Gate()
	gate.Challenge = app.oauth.Challenge("/api/v1/mcp")
	tools, err := mcp.New(mcp.Config{
		Gate: gate, Ctx: cfg.Ctx, Authorize: cfg.Authorize,
		Name: "notes", Version: "v1",
	}, ops...)
	if err != nil {
		t.Fatal(err)
	}
	mux.Handle("/api/v1/", tree)
	mux.Handle("/api/v1/mcp", tools)
	app.agent = app.srv.Client()
	app.agent.CheckRedirect = noFollow
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(app.srv.URL)
	jar.SetCookies(parsed, []*http.Cookie{{Name: sessionCookie, Value: "ana"}})
	app.browser = &http.Client{
		Transport: app.srv.Client().Transport, Jar: jar,
		CheckRedirect: noFollow,
	}
	return app
}

func noFollow(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}

// signedIn reads the test cookie as the session of whoever it names,
// signed in a minute ago.
func signedIn(request *http.Request) (sessions.Session, bool) {
	cookie, err := request.Cookie(sessionCookie)
	if err != nil {
		return sessions.Session{}, false
	}
	now := time.Now()
	return sessions.Session{
		Subject: cookie.Value, AuthTime: now.Add(-time.Minute), At: now,
	}, true
}

// renderJSON draws the approval page as JSON, for the browser to read.
func renderJSON(
	response http.ResponseWriter,
	_ *http.Request,
	page oauth.Page,
) error {
	return json.NewEncoder(response).Encode(page)
}

func (app *flowApp) grant(
	ctx context.Context, connection string, access rastrillo.Access,
) (*rastrillo.Grant, error) {
	grant, err := app.oauth.Grant(ctx, connection, access)
	if app.onGrant != nil {
		app.onGrant(ctx, connection)
	}
	return grant, err
}

// operations are a read, a write that records who made it, and a read
// whose answer is 200 KiB.
func (app *flowApp) operations() []op.Descriptor {
	list := op.Define("notes.list", op.Spec{
		Description: "List the notes.", Access: rastrillo.Read,
		Scoped: true,
	}, func(context.Context, *rastrillo.Ctx, op.None) (noteList, error) {
		return noteList{Notes: []string{"x"}}, nil
	})
	create := op.Define("notes.create", op.Spec{
		Description: "Create a note.", Access: rastrillo.Write,
		Scoped: true,
	}, func(
		_ context.Context, rc *rastrillo.Ctx, in noteTitle,
	) (noteTitle, error) {
		app.mu.Lock()
		defer app.mu.Unlock()
		app.writer = rc.Actor
		return in, nil
	})
	dump := op.Define("notes.dump", op.Spec{
		Description: "Export every note as text.", Access: rastrillo.Read,
		Scoped: true,
	}, func(context.Context, *rastrillo.Ctx, op.None) (noteDump, error) {
		return noteDump{Text: strings.Repeat("x", bigAnswer)}, nil
	})
	return []op.Descriptor{
		list.Descriptor, create.Descriptor, dump.Descriptor,
	}
}

func (app *flowApp) lastWriter() rastrillo.Actor {
	app.mu.Lock()
	defer app.mu.Unlock()
	return app.writer
}

// reply is one answer, read off the connection whole or cut.
type reply struct {
	status  int
	header  http.Header
	body    []byte
	readErr error
}

func (app *flowApp) send(
	client *http.Client, method, target, kind, body string,
	header ...string,
) reply {
	app.t.Helper()
	request, err := http.NewRequestWithContext(
		app.t.Context(), method, target, strings.NewReader(body))
	if err != nil {
		app.t.Fatal(err)
	}
	if kind != "" {
		request.Header.Set("Content-Type", kind)
	}
	for index := 0; index+1 < len(header); index += 2 {
		request.Header.Set(header[index], header[index+1])
	}
	resp, err := client.Do(request)
	if err != nil {
		app.t.Fatalf("%s %s: %v", method, target, err)
	}
	defer resp.Body.Close()
	read, readErr := io.ReadAll(resp.Body)
	return reply{resp.StatusCode, resp.Header, read, readErr}
}

// decode reads an answer that must be status with a JSON body.
func (answer reply) decode(t *testing.T, status int, target any) {
	t.Helper()
	if answer.status != status || answer.readErr != nil {
		t.Fatalf("status %d (%v): %s; want %d",
			answer.status, answer.readErr, answer.body, status)
	}
	if err := json.Unmarshal(answer.body, target); err != nil {
		t.Fatalf("%v: %s", err, answer.body)
	}
}

// rpc posts one JSON-RPC message to the MCP door as token's holder.
func (app *flowApp) rpc(token, body string) reply {
	app.t.Helper()
	return app.send(app.agent, http.MethodPost, app.srv.URL+"/api/v1/mcp",
		"application/json", body,
		"Accept", "application/json, text/event-stream",
		"Authorization", "Bearer "+token)
}

// tools lists the tools token's holder is shown, sorted.
func (app *flowApp) tools(token string) []string {
	app.t.Helper()
	var listed struct {
		Result struct {
			Tools []struct{ Name string } `json:"tools"`
		} `json:"result"`
	}
	app.rpc(token, listCall).decode(app.t, http.StatusOK, &listed)
	var names []string
	for _, tool := range listed.Result.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	return names
}

// create calls the write tool, with no Agent-Session header, and
// checks it succeeded.
func (app *flowApp) create(token string) {
	app.t.Helper()
	var called struct {
		Result struct {
			IsError           bool            `json:"isError"`
			StructuredContent json.RawMessage `json:"structuredContent"`
		} `json:"result"`
	}
	app.rpc(token, createCall).decode(app.t, http.StatusOK, &called)
	if called.Result.IsError ||
		string(called.Result.StructuredContent) != `{"title":"x"}` {
		app.t.Fatalf("notes_create answered %+v", called.Result)
	}
}

// serverMeta is the authorization server metadata a client reads.
type serverMeta struct {
	Issuer        string   `json:"issuer"`
	Authorization string   `json:"authorization_endpoint"`
	Token         string   `json:"token_endpoint"`
	Registration  string   `json:"registration_endpoint"`
	Challenges    []string `json:"code_challenge_methods_supported"`
	ClientDocs    bool     `json:"client_id_metadata_document_supported"`

	IssParam bool `json:"authorization_response_iss_parameter_supported"`
}

var resourceMetadataParam = regexp.MustCompile(
	`^Bearer resource_metadata="([^"]+)", scope="read write"$`)

// discover starts from a request with no token, as a client does: the
// 401's challenge, the door's resource metadata, then the server's.
func (app *flowApp) discover(path, body, door string) serverMeta {
	app.t.Helper()
	answer := app.send(app.agent, http.MethodPost, app.srv.URL+path,
		"application/json", body,
		"Accept", "application/json, text/event-stream")
	challenge := answer.header.Get("WWW-Authenticate")
	match := resourceMetadataParam.FindStringSubmatch(challenge)
	if answer.status != http.StatusUnauthorized || match == nil {
		app.t.Fatalf("POST %s: status %d, WWW-Authenticate %q",
			path, answer.status, challenge)
	}
	want := app.srv.URL + "/.well-known/oauth-protected-resource" + door
	if match[1] != want {
		app.t.Fatalf("resource_metadata = %q, want %q", match[1], want)
	}
	var resource struct {
		Resource string   `json:"resource"`
		Servers  []string `json:"authorization_servers"`
	}
	app.send(app.agent, http.MethodGet, match[1], "", "").
		decode(app.t, http.StatusOK, &resource)
	if resource.Resource != app.srv.URL+door ||
		len(resource.Servers) != 1 {
		app.t.Fatalf("resource metadata %+v", resource)
	}
	var meta serverMeta
	app.send(app.agent, http.MethodGet, resource.Servers[0]+
		"/.well-known/oauth-authorization-server", "", "").
		decode(app.t, http.StatusOK, &meta)
	if meta.Issuer != resource.Servers[0] || !meta.ClientDocs ||
		!meta.IssParam || !slices.Equal(meta.Challenges, []string{"S256"}) {
		app.t.Fatalf("server metadata %+v", meta)
	}
	return meta
}

// metadata reads the server metadata straight, for tests about what
// happens after discovery.
func (app *flowApp) metadata() serverMeta {
	app.t.Helper()
	var meta serverMeta
	app.send(app.agent, http.MethodGet,
		app.srv.URL+"/.well-known/oauth-authorization-server", "", "").
		decode(app.t, http.StatusOK, &meta)
	return meta
}

func (app *flowApp) register(meta serverMeta, redirect string) string {
	app.t.Helper()
	var registered struct {
		ClientID string `json:"client_id"`
	}
	body := `{"client_name":"Codex","redirect_uris":["` + redirect + `"]}`
	app.send(app.agent, http.MethodPost, meta.Registration,
		"application/json", body).
		decode(app.t, http.StatusCreated, &registered)
	return registered.ClientID
}

// authorize has the signed-in browser load the approval page and
// approve access, and returns the code the redirect carries.
func (app *flowApp) authorize(
	meta serverMeta, client, redirect, resource, access string,
) string {
	app.t.Helper()
	query := url.Values{
		"response_type": {"code"}, "client_id": {client},
		"redirect_uri": {redirect}, "code_challenge": {challenge()},
		"code_challenge_method": {"S256"}, "resource": {resource},
		"scope": {"read write"}, "state": {"s1"},
	}
	var page oauth.Page
	app.send(app.browser, http.MethodGet,
		meta.Authorization+"?"+query.Encode(), "", "").
		decode(app.t, http.StatusOK, &page)
	if page.Kind != "approve" || page.Pending == "" {
		app.t.Fatalf("approval page %+v", page)
	}
	form := url.Values{
		"pending": {page.Pending}, "decision": {"approve"},
		"access": {access},
	}
	answer := app.send(app.browser, http.MethodPost, meta.Authorization,
		"application/x-www-form-urlencoded", form.Encode(),
		"Origin", app.srv.URL)
	loc := answer.header.Get("Location")
	if answer.status != http.StatusSeeOther ||
		!strings.HasPrefix(loc, redirect+"?") {
		app.t.Fatalf("approve: status %d, Location %q", answer.status, loc)
	}
	parsed, err := url.Parse(loc)
	if err != nil {
		app.t.Fatal(err)
	}
	back := parsed.Query()
	if back.Get("iss") != meta.Issuer || back.Get("state") != "s1" ||
		back.Get("code") == "" {
		app.t.Fatalf("redirected with %v", back)
	}
	return back.Get("code")
}

func (app *flowApp) token(meta serverMeta, form url.Values) tokenAnswer {
	app.t.Helper()
	var issued tokenAnswer
	app.send(app.agent, http.MethodPost, meta.Token,
		"application/x-www-form-urlencoded", form.Encode()).
		decode(app.t, http.StatusOK, &issued)
	return issued
}

func (app *flowApp) exchange(
	meta serverMeta, client, redirect, resource, code string,
) tokenAnswer {
	app.t.Helper()
	return app.token(meta, url.Values{
		"grant_type": {"authorization_code"}, "code": {code},
		"code_verifier": {verifier}, "client_id": {client},
		"redirect_uri": {redirect}, "resource": {resource},
	})
}

// connect registers a client and approves it for the MCP door with
// access, and returns its tokens.
func (app *flowApp) connect(access string) tokenAnswer {
	app.t.Helper()
	meta := app.metadata()
	client := app.register(meta, agentCB)
	resource := app.srv.URL + "/api/v1/mcp"
	code := app.authorize(meta, client, agentCB, resource, access)
	return app.exchange(meta, client, agentCB, resource, code)
}

// connection is ana's one live connection.
func (app *flowApp) connection() string {
	app.t.Helper()
	cs, err := app.oauth.Connections(context.Background(), "ana")
	if err != nil || len(cs) != 1 {
		app.t.Fatalf("connections %+v, %v; want one", cs, err)
	}
	return cs[0].ID
}

func TestFlowDocumentClient(t *testing.T) {
	fetcher, docs, hits := docServer(t, goodDoc)
	app := newFlowApp(t, fetcher)
	meta := app.discover("/api/v1/mcp", listCall, "/api/v1/mcp")
	client, resource := docs+"/client.json", app.srv.URL+"/api/v1/mcp"
	code := app.authorize(meta, client, agentCB, resource, "write")
	if hits.Load() == 0 {
		t.Fatal("the client's document was never fetched")
	}
	tokens := app.exchange(meta, client, agentCB, resource, code)
	if tokens.Scope != "read write" {
		t.Fatalf("scope %q", tokens.Scope)
	}
	if names := app.tools(tokens.AccessToken); !slices.Contains(
		names, "notes_create") {
		t.Fatalf("tools %v, want notes_create among them", names)
	}

	app.create(tokens.AccessToken)
	id := app.connection()
	actor := app.lastWriter()
	if actor.Session != "oauth:"+id || actor.Grant == nil ||
		actor.Grant.ID != id ||
		actor.Agent != "Agent" ||
		actor.Subject != "ana" {
		t.Fatalf("the write ran as %+v, grant %+v; want connection %s",
			actor, actor.Grant, id)
	}

	again := app.token(meta, url.Values{
		"grant_type": {"refresh_token"}, "client_id": {client},
		"refresh_token": {tokens.RefreshToken},
	})
	app.create(again.AccessToken)

	if err := app.oauth.End(context.Background(), "ana", id); err != nil {
		t.Fatal(err)
	}
	answer := app.rpc(again.AccessToken, createCall)
	if answer.status != http.StatusUnauthorized ||
		!resourceMetadataParam.MatchString(
			answer.header.Get("WWW-Authenticate")) {
		t.Fatalf("after End: status %d, WWW-Authenticate %q",
			answer.status, answer.header.Get("WWW-Authenticate"))
	}
}

func TestFlowRegisteredLoopbackClient(t *testing.T) {
	app := newFlowApp(t, nil)
	meta := app.discover("/api/v1/mcp", listCall, "/api/v1/mcp")
	client := app.register(meta, "http://127.0.0.1:1/cb")
	const redirect = "http://127.0.0.1:53111/cb"
	resource := app.srv.URL + "/api/v1/mcp"
	code := app.authorize(meta, client, redirect, resource, "write")
	tokens := app.exchange(meta, client, redirect, resource, code)
	app.create(tokens.AccessToken)

	// The JSON door points at its own metadata, and takes the token
	// the MCP door's approval issued.
	app.discover("/api/v1/notes/list", "{}", "/api/v1")
	var listed noteList
	app.send(app.agent, http.MethodPost, app.srv.URL+"/api/v1/notes/list",
		"application/json", "{}",
		"Authorization", "Bearer "+tokens.AccessToken).
		decode(t, http.StatusOK, &listed)
	var made noteTitle
	app.send(app.agent, http.MethodPost, app.srv.URL+"/api/v1/notes/create",
		"application/json", `{"title":"y"}`,
		"Authorization", "Bearer "+tokens.AccessToken).
		decode(t, http.StatusOK, &made)
	if actor := app.lastWriter(); made.Title != "y" ||
		actor.Session != "oauth:"+app.connection() {
		t.Fatalf("made %+v as %+v", made, actor)
	}
}

func TestFlowReadOnly(t *testing.T) {
	app := newFlowApp(t, nil)
	tokens := app.connect("read")
	if tokens.Scope != "read" {
		t.Fatalf("scope %q", tokens.Scope)
	}
	got := app.tools(tokens.AccessToken)
	if want := []string{"notes_dump", "notes_list"}; !slices.Equal(
		got, want) {
		t.Fatalf("tools %v, want %v", got, want)
	}
}

func TestFlowEndedMidAnswer(t *testing.T) {
	app := newFlowApp(t, nil)
	tokens := app.connect("read")
	calls := 0
	app.onGrant = func(ctx context.Context, connection string) {
		// The first call is Verify's, the second the recheck before
		// the headers; ending here lets the headers and the first
		// chunk go, and the next recheck finds the connection gone.
		if calls++; calls == 2 {
			if err := app.oauth.End(ctx, "ana", connection); err != nil {
				t.Error(err)
			}
		}
	}
	answer := app.rpc(tokens.AccessToken, dumpCall)
	if answer.status != http.StatusOK {
		t.Fatalf("status %d, want 200 before the connection ended",
			answer.status)
	}
	whole, err := strconv.Atoi(answer.header.Get("Content-Length"))
	if err != nil || whole < bigAnswer {
		t.Fatalf("Content-Length %q, want the whole answer's",
			answer.header.Get("Content-Length"))
	}
	if !errors.Is(answer.readErr, io.ErrUnexpectedEOF) {
		t.Fatalf("read error %v, want an unexpected EOF", answer.readErr)
	}
	if len(answer.body) > 64<<10 {
		t.Fatalf("the client read %d of %d bytes, more than one "+
			"64 KiB chunk", len(answer.body), whole)
	}
	cs, err := app.oauth.Connections(context.Background(), "ana")
	if err != nil || len(cs) != 0 {
		t.Fatalf("connections %+v, %v; want none", cs, err)
	}
}
