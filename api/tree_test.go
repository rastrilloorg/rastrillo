package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/op"
)

// The tokens the fake verifier knows. Each names a different kind of
// caller, because the tree treats a person's token, an agent's token and
// a read-only token differently at different steps.
var (
	personWrite = rastrillo.Actor{Subject: "paul", Credential: "pw000000000a", Access: rastrillo.Write}
	personRead  = rastrillo.Actor{Subject: "paul", Credential: "pr000000000a", Access: rastrillo.Read}
	agentWrite  = rastrillo.Actor{Subject: "paul", Agent: "Claude", Credential: "aw000000000a", Access: rastrillo.Write}
	agentRead   = rastrillo.Actor{Subject: "paul", Agent: "Claude", Credential: "ar000000000a", Access: rastrillo.Read}
)

type fakeVerifier struct {
	tokens map[string]rastrillo.Actor
	err    error     // returned for every request when set
	calls  *[]string // records "verify" when set, for ordering tests
}

func (v fakeVerifier) Verify(_ context.Context, r *http.Request) (rastrillo.Actor, error) {
	if v.calls != nil {
		*v.calls = append(*v.calls, "verify")
	}
	if v.err != nil {
		return rastrillo.Actor{}, v.err
	}
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if a, known := v.tokens[tok]; ok && known {
		return a, nil
	}
	return rastrillo.Actor{}, fmt.Errorf("fake verifier: unknown token: %w", ErrUnauthorized)
}

func verifier() fakeVerifier {
	return fakeVerifier{tokens: map[string]rastrillo.Actor{
		"person-write": personWrite,
		"person-read":  personRead,
		"agent-write":  agentWrite,
		"agent-read":   agentRead,
	}}
}

func desc(id string, a rastrillo.Access) op.Descriptor {
	return op.Descriptor{
		Operation: rastrillo.Operation{ID: id, Resource: "things", Scoped: true, Access: a},
		Exposed:   true,
		Input:     []byte(`{"type":"object"}`),
		Result:    []byte(`{"type":"object"}`),
	}
}

func allow(context.Context, rastrillo.Actor, rastrillo.Operation) error { return nil }

// config is a valid Config whose logger discards, so a test that does
// not read the log keeps the output clean.
func config() Config {
	return Config{
		Verify:    verifier(),
		Ctx:       func(*http.Request) *rastrillo.Ctx { return &rastrillo.Ctx{} },
		Authorize: allow,
		Logger:    slog.New(slog.DiscardHandler),
	}
}

// ok is a handler that answers 200 with the actor it was given, so a
// test can see what reached it.
func ok(_ context.Context, rc *rastrillo.Ctx, w http.ResponseWriter, _ *http.Request) {
	Write(w, http.StatusOK, map[string]string{"session": rc.Actor.Session, "harness": rc.Actor.Harness})
}

// decodes is a write handler that reads its body, so the body cap and
// the tree's refusals before it can be told apart.
func decodes(called *bool) func(context.Context, *rastrillo.Ctx, http.ResponseWriter, *http.Request) {
	return func(_ context.Context, _ *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
		if called != nil {
			*called = true
		}
		var in struct {
			Title string `json:"title"`
		}
		if !Decode(w, r, &in) {
			return
		}
		Write(w, http.StatusCreated, in)
	}
}

func getThing(h func(context.Context, *rastrillo.Ctx, http.ResponseWriter, *http.Request)) Route {
	return Route{Method: http.MethodGet, Pattern: "/api/v1/things/{id}", Op: desc("things.get", rastrillo.Read), Handle: h}
}

func createThing(h func(context.Context, *rastrillo.Ctx, http.ResponseWriter, *http.Request)) Route {
	return Route{Method: http.MethodPost, Pattern: "/api/v1/things", Op: desc("things.create", rastrillo.Write), Handle: h}
}

func mustNew(t *testing.T, cfg Config, routes ...Route) http.Handler {
	t.Helper()
	h, err := New(cfg, routes...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h
}

func serve(h http.Handler, method, target, token string, body string, header ...string) *httptest.ResponseRecorder {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(header); i += 2 {
		r.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func assertCode(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body %s", rec.Code, status, rec.Body)
	}
	if got := decodeError(t, rec).Code; got != code {
		t.Fatalf("code = %q, want %q", got, code)
	}
}

// syncBuffer is a log sink an httptest.Server's handler goroutine and
// the test can share without a race.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestNewRefuses(t *testing.T) {
	get := getThing(ok)
	cases := []struct {
		name   string
		mutate func(*Config, *[]Route)
	}{
		{"nil Verify", func(c *Config, _ *[]Route) { c.Verify = nil }},
		{"nil Ctx", func(c *Config, _ *[]Route) { c.Ctx = nil }},
		{"nil Authorize", func(c *Config, _ *[]Route) { c.Authorize = nil }},
		{"empty Op.ID", func(_ *Config, rs *[]Route) { (*rs)[0].Op.ID = "" }},
		{"zero Op.Access", func(_ *Config, rs *[]Route) { (*rs)[0].Op.Access = 0 }},
		{"empty Op.Input", func(_ *Config, rs *[]Route) { (*rs)[0].Op.Input = nil }},
		{"empty Op.Result", func(_ *Config, rs *[]Route) { (*rs)[0].Op.Result = nil }},
		{"nil Handle", func(_ *Config, rs *[]Route) { (*rs)[0].Handle = nil }},
		{"GET with Write", func(_ *Config, rs *[]Route) { (*rs)[0].Op.Access = rastrillo.Write }},
		{"POST with Read", func(_ *Config, rs *[]Route) { *rs = []Route{createThing(ok)}; (*rs)[0].Op.Access = rastrillo.Read }},
		{"unknown method", func(_ *Config, rs *[]Route) { (*rs)[0].Method = "OPTIONS" }},
		{"duplicate method and pattern", func(_ *Config, rs *[]Route) { *rs = append(*rs, get) }},
		{"patterns that conflict", func(_ *Config, rs *[]Route) {
			other := get
			other.Pattern = "/api/v1/things/{key}"
			*rs = append(*rs, other)
		}},
		{"malformed pattern", func(_ *Config, rs *[]Route) { (*rs)[0].Pattern = "api/v1/things/{id" }},
		{"nil Before", func(c *Config, _ *[]Route) { c.Before = []func(http.Handler) http.Handler{nil} }},
		{"After returning nil", func(c *Config, _ *[]Route) {
			c.After = []func(http.Handler) http.Handler{func(http.Handler) http.Handler { return nil }}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config()
			routes := []Route{get}
			tc.mutate(&cfg, &routes)
			if _, err := New(cfg, routes...); err == nil {
				t.Fatal("New returned no error")
			}
		})
	}
}

func TestPathValueReachesHandler(t *testing.T) {
	var got string
	h := mustNew(t, config(), getThing(func(_ context.Context, _ *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
		got = r.PathValue("id")
		Write(w, http.StatusOK, struct{}{})
	}))
	rec := serve(h, http.MethodGet, "/api/v1/things/42", "person-read", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	if got != "42" {
		t.Fatalf(`r.PathValue("id") = %q, want "42"`, got)
	}
}

func TestHandlerAuthorizeIsConfigAuthorize(t *testing.T) {
	policy := errors.New("the app's own policy")
	cfg := config()
	cfg.Authorize = func(context.Context, rastrillo.Actor, rastrillo.Operation) error { return policy }
	var got error
	h := mustNew(t, cfg, getThing(func(ctx context.Context, rc *rastrillo.Ctx, w http.ResponseWriter, _ *http.Request) {
		if rc.Authorize == nil {
			got = errors.New("rc.Authorize is nil")
		} else {
			got = rc.Authorize(ctx, rc.Actor, rastrillo.Operation{})
		}
		Write(w, http.StatusOK, struct{}{})
	}))
	serve(h, http.MethodGet, "/api/v1/things/1", "person-read", "")
	if !errors.Is(got, policy) {
		t.Fatalf("rc.Authorize returned %v, want Config.Authorize's error", got)
	}
}

func TestHandlerGetsCtxFromFactoryWithVerifiedActor(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	cfg := config()
	// The factory's own actor must lose to the verified one: the API's
	// identity comes from the token, never from app code or the request.
	cfg.Ctx = func(*http.Request) *rastrillo.Ctx {
		return &rastrillo.Ctx{Logger: logger, Actor: rastrillo.Actor{Subject: "someone-else"}}
	}
	var got *rastrillo.Ctx
	h := mustNew(t, cfg, getThing(func(_ context.Context, rc *rastrillo.Ctx, w http.ResponseWriter, _ *http.Request) {
		got = rc
		Write(w, http.StatusOK, struct{}{})
	}))
	serve(h, http.MethodGet, "/api/v1/things/1", "person-read", "")
	if got == nil {
		t.Fatal("handler not called")
	}
	if got.Logger != logger {
		t.Error("rc is not the one Config.Ctx built: Logger differs")
	}
	if got.Actor != personRead {
		t.Errorf("rc.Actor = %+v, want the verified %+v", got.Actor, personRead)
	}
}

// A factory that hands back one shared Ctx must not have the tree write
// one request's actor into it, where the next request would find it.
func TestSharedCtxIsNotMutated(t *testing.T) {
	shared := &rastrillo.Ctx{}
	cfg := config()
	cfg.Ctx = func(*http.Request) *rastrillo.Ctx { return shared }
	var got rastrillo.Actor
	h := mustNew(t, cfg, getThing(func(_ context.Context, rc *rastrillo.Ctx, w http.ResponseWriter, _ *http.Request) {
		got = rc.Actor
		Write(w, http.StatusOK, struct{}{})
	}))
	if rec := serve(h, http.MethodGet, "/api/v1/things/1", "person-read", ""); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	if got != personRead {
		t.Fatalf("handler's rc.Actor = %+v, want the verified %+v", got, personRead)
	}
	if shared.Actor != (rastrillo.Actor{}) || shared.Authorize != nil {
		t.Fatalf("the factory's Ctx was changed: Actor %+v, Authorize set %v", shared.Actor, shared.Authorize != nil)
	}
}

func TestPrecedence(t *testing.T) {
	cfg := config()
	cfg.MaxBody = 10
	var called bool
	h := mustNew(t, cfg, getThing(ok), createThing(decodes(&called)))

	t.Run("routing before the credential", func(t *testing.T) {
		assertCode(t, serve(h, http.MethodGet, "/api/v1/nope", "bad", ""), http.StatusNotFound, "not_found")
	})
	t.Run("the credential on a known path", func(t *testing.T) {
		rec := serve(h, http.MethodGet, "/api/v1/things/1", "bad", "")
		assertCode(t, rec, http.StatusUnauthorized, "unauthorized")
		if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
			t.Errorf("WWW-Authenticate = %q, want Bearer", got)
		}
	})
	t.Run("agent headers before read-only", func(t *testing.T) {
		rec := serve(h, http.MethodPost, "/api/v1/things", "agent-read", `{}`, "Agent-Session", "bad\x01")
		assertCode(t, rec, http.StatusUnprocessableEntity, "invalid")
	})
	t.Run("read-only before the body", func(t *testing.T) {
		called = false
		rec := serve(h, http.MethodPost, "/api/v1/things", "person-read", `{"title":"far too long"}`)
		assertCode(t, rec, http.StatusForbidden, "forbidden")
		if called {
			t.Error("handler ran for a read-only token on a write route")
		}
	})
}

func TestUnknownPathIsJSON404(t *testing.T) {
	h := mustNew(t, config(), getThing(ok))
	rec := serve(h, http.MethodGet, "/api/v1/nope", "person-read", "")
	assertCode(t, rec, http.StatusNotFound, "not_found")
	assertHeaders(t, rec)
}

// The outer ServeMux cleans paths before the tree sees them, but a tree
// mounted some other way must not let the inner mux answer a redirect
// in its own HTML.
func TestUncleanPathIsJSON404(t *testing.T) {
	h := mustNew(t, config(), getThing(ok))
	rec := serve(h, http.MethodGet, "/api/v1//things/1", "person-read", "")
	assertCode(t, rec, http.StatusNotFound, "not_found")
	assertHeaders(t, rec)
}

func TestWrongMethodIsJSON405WithAllow(t *testing.T) {
	h := mustNew(t, config(), getThing(ok))
	rec := serve(h, http.MethodDelete, "/api/v1/things/1", "person-write", "")
	assertCode(t, rec, http.StatusMethodNotAllowed, "method_not_allowed")
	assertHeaders(t, rec)
	if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
		t.Fatalf("Allow = %q, want %q", got, "GET, HEAD")
	}
}

// HEAD goes through a real server: net/http drops a HEAD body on the
// wire, and a recorder would keep it.
func TestHeadOnGetRoute(t *testing.T) {
	srv := httptest.NewServer(mustNew(t, config(), getThing(ok)))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodHead, srv.URL+"/api/v1/things/1", nil)
	req.Header.Set("Authorization", "Bearer person-read")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(body) != 0 {
		t.Fatalf("body = %q, want empty", body)
	}
	// The headers are the GET's, so a client can HEAD to learn the type
	// and length without the body.
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestEveryResponseIsJSONNoStore(t *testing.T) {
	h := mustNew(t, config(), getThing(ok), Route{
		Method: http.MethodGet, Pattern: "/api/v1/panics", Op: desc("things.panic", rastrillo.Read),
		Handle: func(context.Context, *rastrillo.Ctx, http.ResponseWriter, *http.Request) { panic("boom") },
	})
	cases := []struct {
		name, method, target, token string
		status                      int
	}{
		{"200", http.MethodGet, "/api/v1/things/1", "person-read", http.StatusOK},
		{"404", http.MethodGet, "/api/v1/nope", "person-read", http.StatusNotFound},
		{"405", http.MethodPut, "/api/v1/things/1", "person-write", http.StatusMethodNotAllowed},
		{"401", http.MethodGet, "/api/v1/things/1", "bad", http.StatusUnauthorized},
		{"500 from a panic", http.MethodGet, "/api/v1/panics", "person-read", http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := serve(h, tc.method, tc.target, tc.token, "")
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tc.status, rec.Body)
			}
			assertHeaders(t, rec)
		})
	}
}

// A handler or middleware that writes without api.Write still answers
// JSON headers: net/http would otherwise sniff a body as text/plain, and
// leave a header-only answer with no type at all.
func TestDirectWritesAreJSONNoStore(t *testing.T) {
	shortCircuit := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Short") != "" {
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":{"code":"rate_limited","message":"Slow down."}}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	cfg := config()
	cfg.Before = []func(http.Handler) http.Handler{shortCircuit}
	h := mustNew(t, cfg,
		getThing(func(_ context.Context, _ *rastrillo.Ctx, w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"id":1}`))
		}),
		Route{
			Method: http.MethodDelete, Pattern: "/api/v1/things/{id}", Op: desc("things.delete", rastrillo.Write),
			Handle: func(_ context.Context, _ *rastrillo.Ctx, w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			},
		},
	)
	cases := []struct {
		name, method, token string
		header              []string
		status              int
	}{
		{"handler writes the body itself", http.MethodGet, "person-read", nil, http.StatusOK},
		{"handler writes only a status", http.MethodDelete, "person-write", nil, http.StatusNoContent},
		{"Before answers on its own", http.MethodGet, "bad", []string{"X-Short", "1"}, http.StatusTooManyRequests},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := serve(h, tc.method, "/api/v1/things/1", tc.token, "", tc.header...)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tc.status, rec.Body)
			}
			assertHeaders(t, rec)
		})
	}
}

func TestReadOnlyTokenOnWriteRouteIsForbidden(t *testing.T) {
	var called bool
	h := mustNew(t, config(), createThing(decodes(&called)))
	rec := serve(h, http.MethodPost, "/api/v1/things", "person-read", `{"title":"x"}`)
	assertCode(t, rec, http.StatusForbidden, "forbidden")
	if called {
		t.Fatal("handler ran for a read-only token on a write route")
	}
}

func TestBeforeRunsBeforeVerifyAndAfterSeesActor(t *testing.T) {
	var calls []string
	cfg := config()
	v := verifier()
	v.calls = &calls
	cfg.Verify = v
	cfg.Before = []func(http.Handler) http.Handler{
		func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, "before 1")
				next.ServeHTTP(w, r)
			})
		},
		func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, "before 2")
				next.ServeHTTP(w, r)
			})
		},
	}
	cfg.After = []func(http.Handler) http.Handler{
		func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				a, ok := rastrillo.ActorFromContext(r.Context())
				calls = append(calls, fmt.Sprintf("after %v %s", ok, a))
				next.ServeHTTP(w, r)
			})
		},
	}
	h := mustNew(t, cfg, getThing(func(_ context.Context, _ *rastrillo.Ctx, w http.ResponseWriter, _ *http.Request) {
		calls = append(calls, "handler")
		Write(w, http.StatusOK, struct{}{})
	}))
	serve(h, http.MethodGet, "/api/v1/things/1", "person-read", "")
	want := []string{"before 1", "before 2", "verify", "after true " + personRead.String(), "handler"}
	if !slices.Equal(calls, want) {
		t.Fatalf("calls = %q, want %q", calls, want)
	}

	// A refused token still passed through Before, which is where a
	// rate limiter has to see it.
	calls = nil
	serve(h, http.MethodGet, "/api/v1/things/1", "bad", "")
	if want := []string{"before 1", "before 2", "verify"}; !slices.Equal(calls, want) {
		t.Fatalf("calls with a bad token = %q, want %q", calls, want)
	}
}

func TestRequireAgentSession(t *testing.T) {
	cfg := config()
	cfg.RequireAgentSession = true
	h := mustNew(t, cfg, getThing(ok), createThing(decodes(nil)))

	t.Run("agent write without it", func(t *testing.T) {
		rec := serve(h, http.MethodPost, "/api/v1/things", "agent-write", `{"title":"x"}`)
		assertCode(t, rec, http.StatusUnprocessableEntity, "invalid")
	})
	t.Run("agent write with it", func(t *testing.T) {
		rec := serve(h, http.MethodPost, "/api/v1/things", "agent-write", `{"title":"x"}`, "Agent-Session", "s-1")
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body %s", rec.Code, rec.Body)
		}
	})
	t.Run("agent read without it", func(t *testing.T) {
		rec := serve(h, http.MethodGet, "/api/v1/things/1", "agent-write", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
		}
	})
	t.Run("person write without it", func(t *testing.T) {
		rec := serve(h, http.MethodPost, "/api/v1/things", "person-write", `{"title":"x"}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body %s", rec.Code, rec.Body)
		}
	})
}

func TestAgentHeaders(t *testing.T) {
	var got rastrillo.Actor
	h := mustNew(t, config(), getThing(func(_ context.Context, rc *rastrillo.Ctx, w http.ResponseWriter, _ *http.Request) {
		got = rc.Actor
		Write(w, http.StatusOK, struct{}{})
	}))
	get := func(token string, header ...string) *httptest.ResponseRecorder {
		got = rastrillo.Actor{}
		return serve(h, http.MethodGet, "/api/v1/things/1", token, "", header...)
	}

	t.Run("copied for an agent", func(t *testing.T) {
		rec := get("agent-read", "Agent-Session", strings.Repeat("s", 200), "Agent-Harness", "claude-code/2.1.288")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
		}
		if got.Session != strings.Repeat("s", 200) || got.Harness != "claude-code/2.1.288" {
			t.Fatalf("Session %q, Harness %q: not copied from the headers", got.Session, got.Harness)
		}
	})
	for _, name := range []string{"Agent-Session", "Agent-Harness"} {
		t.Run(name+" of 201 bytes", func(t *testing.T) {
			assertCode(t, get("agent-read", name, strings.Repeat("s", 201)), http.StatusUnprocessableEntity, "invalid")
		})
		t.Run(name+" with a newline", func(t *testing.T) {
			assertCode(t, get("agent-read", name, "s\n1"), http.StatusUnprocessableEntity, "invalid")
		})
		t.Run(name+" with DEL", func(t *testing.T) {
			assertCode(t, get("agent-read", name, "s\x7f1"), http.StatusUnprocessableEntity, "invalid")
		})
	}
	t.Run("ignored for a person", func(t *testing.T) {
		rec := get("person-read", "Agent-Session", "s\n1", "Agent-Harness", "pretend-agent")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
		}
		if got.Session != "" || got.Harness != "" {
			t.Fatalf("a person's token set Session %q, Harness %q", got.Session, got.Harness)
		}
	})
}

func TestPanicBeforeWriteIs500WithRef(t *testing.T) {
	var logs syncBuffer
	cfg := config()
	cfg.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	h := mustNew(t, cfg, getThing(func(context.Context, *rastrillo.Ctx, http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	rec := serve(h, http.MethodGet, "/api/v1/things/1", "person-read", "")
	assertCode(t, rec, http.StatusInternalServerError, "internal")
	assertHeaders(t, rec)
	ref := decodeError(t, rec).Ref
	if ref == "" {
		t.Fatal("no ref in the 500")
	}
	if line := logs.String(); !strings.Contains(line, "ref="+ref) || !strings.Contains(line, "boom") {
		t.Fatalf("log %q does not carry the ref %q and the panic value", line, ref)
	}
}

func TestPanicAfterWriteAbortsConnection(t *testing.T) {
	var logs, serverLog syncBuffer
	cfg := config()
	cfg.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	h := mustNew(t, cfg, getThing(func(_ context.Context, _ *rastrillo.Ctx, w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"partial":`))
		_ = http.NewResponseController(w).Flush()
		panic("boom mid-stream")
	}))
	srv := httptest.NewUnstartedServer(h)
	srv.Config.ErrorLog = log.New(&serverLog, "", 0)
	srv.Start()
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/things/1", nil)
	req.Header.Set("Authorization", "Bearer person-read")
	resp, err := srv.Client().Do(req)
	if err == nil {
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr == nil {
			t.Fatalf("client read a whole response (%d, %q); want the connection aborted", resp.StatusCode, body)
		}
		if strings.Contains(string(body), `"error"`) {
			t.Fatalf("JSON error appended to a started body: %q", body)
		}
	}
	// The tree logs the value itself, then aborts with
	// http.ErrAbortHandler, which net/http does not log. The value must
	// be in the tree's log, or the abort would swallow it.
	if !strings.Contains(logs.String(), "boom mid-stream") {
		t.Fatalf("the panic value is not in the tree's log: %q", logs.String())
	}
	if s := serverLog.String(); strings.Contains(s, "panic") {
		t.Fatalf("net/http logged a panic, so the tree did not abort cleanly: %q", s)
	}
}

func TestBodyCap(t *testing.T) {
	cfg := config()
	cfg.MaxBody = 10
	h := mustNew(t, cfg, createThing(decodes(nil)))
	rec := serve(h, http.MethodPost, "/api/v1/things", "person-write", `{"t":"123"}`) // 11 bytes
	assertCode(t, rec, http.StatusRequestEntityTooLarge, "too_large")
}

func TestRedactIsAppliedToPanicLog(t *testing.T) {
	secret := "tickets_k3Jx9QpL2mWa_" + strings.Repeat("A", 43)
	cases := []struct {
		name, target, value, want string
	}{
		{"in the panic value", "/api/v1/things/1", "tok " + secret, "tok tickets_k3Jx9QpL2mWa"},
		// A client that pastes its token into a path segment puts it in
		// r.URL.Path, which the panic line logs beside the value.
		{"in the path", "/api/v1/things/" + secret, "boom", "/api/v1/things/tickets_k3Jx9QpL2mWa"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logs syncBuffer
			cfg := config()
			cfg.Logger = slog.New(slog.NewTextHandler(&logs, nil))
			cfg.Redact = func(s string) string { return strings.ReplaceAll(s, secret, "tickets_k3Jx9QpL2mWa") }
			h := mustNew(t, cfg, getThing(func(context.Context, *rastrillo.Ctx, http.ResponseWriter, *http.Request) {
				panic(tc.value)
			}))
			rec := serve(h, http.MethodGet, tc.target, "person-read", "")
			assertCode(t, rec, http.StatusInternalServerError, "internal")
			line := logs.String()
			if strings.Contains(line, secret) {
				t.Fatalf("the token reached the log: %q", line)
			}
			if !strings.Contains(line, tc.want) {
				t.Fatalf("the redacted text %q is not in the log: %q", tc.want, line)
			}
		})
	}
}

// Refuse, called from a handler, must log through the tree's logger and
// redactor, not slog.Default: the tree stamps both on the request.
func TestRefuseInHandlerUsesTreeLoggerAndRedactor(t *testing.T) {
	var logs syncBuffer
	cfg := config()
	cfg.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	cfg.Redact = func(s string) string { return strings.ReplaceAll(s, "s3cret", "[redacted]") }
	h := mustNew(t, cfg, getThing(func(_ context.Context, _ *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
		Refuse(w, r, errors.New("store said s3cret"))
	}))
	assertCode(t, serve(h, http.MethodGet, "/api/v1/things/1", "person-read", ""), http.StatusInternalServerError, "internal")
	if line := logs.String(); !strings.Contains(line, "store said [redacted]") {
		t.Fatalf("log %q did not go through the tree's logger and redactor", line)
	}
}

func TestVerifierUnavailableIs503(t *testing.T) {
	cfg := config()
	cfg.Verify = fakeVerifier{err: fmt.Errorf("db down: %w", ErrUnavailable)}
	h := mustNew(t, cfg, getThing(ok))
	assertCode(t, serve(h, http.MethodGet, "/api/v1/things/1", "person-read", ""), http.StatusServiceUnavailable, "unavailable")
}

// A Verifier that returns no error and no credential has a bug. Serving
// the request anyway would run it as the app itself, which DefaultPolicy
// lets do anything, so the tree answers 500 instead.
func TestVerifiedActorWithoutCredentialIs500(t *testing.T) {
	for name, a := range map[string]rastrillo.Actor{
		"no credential": {Subject: "paul", Access: rastrillo.Write},
		"no access":     {Subject: "paul", Credential: "pw000000000a"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := config()
			cfg.Verify = fakeVerifier{tokens: map[string]rastrillo.Actor{"t": a}}
			var called bool
			h := mustNew(t, cfg, getThing(func(_ context.Context, rc *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
				called = true
				ok(context.Background(), rc, w, r)
			}))
			assertCode(t, serve(h, http.MethodGet, "/api/v1/things/1", "t", ""), http.StatusInternalServerError, "internal")
			if called {
				t.Fatal("handler ran for an actor with no credential")
			}
		})
	}
}

// The tree logs the actor for every request that reaches a handler:
// the audit string, and the agent's session and harness when it sent
// them. An eventlog-free app has nothing else that says which token
// made a write, so without the line a revoked token's last act would
// be untraceable.
func TestRequestLogsTheActor(t *testing.T) {
	var logs syncBuffer
	cfg := config()
	cfg.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	cfg.Redact = func(s string) string { return strings.ReplaceAll(s, "secret", "[redacted]") }
	h := mustNew(t, cfg, createThing(decodes(nil)), getThing(ok))

	rec := serve(h, http.MethodPost, "/api/v1/things", "agent-write", `{"title":"x"}`,
		"Agent-Session", "sess-1", "Agent-Harness", "claude-code/2.1.288")
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST = %d, want 201; body %s", rec.Code, rec.Body)
	}
	line := logs.String()
	for _, want := range []string{
		`actor="agent:Claude for user:paul token:aw000000000a"`,
		`agent_session=sess-1`,
		`agent_harness=claude-code/2.1.288`,
		`method=POST`,
		`path=/api/v1/things`,
	} {
		if !strings.Contains(line, want) {
			t.Errorf("the request log %q lacks %s", line, want)
		}
	}
	// The bearer is never a field: the line names the token by its
	// public ID only.
	if strings.Contains(line, "Bearer") || strings.Contains(line, "agent-write") {
		t.Errorf("the request log %q carries the Authorization header", line)
	}

	// The session is the client's own text, so a harness that pastes
	// its token there must not put the secret in the log.
	logs = syncBuffer{}
	rec = serve(h, http.MethodPost, "/api/v1/things", "agent-write", `{"title":"x"}`, "Agent-Session", "pasted-secret")
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST = %d, want 201; body %s", rec.Code, rec.Body)
	}
	if line := logs.String(); !strings.Contains(line, "agent_session=pasted-[redacted]") {
		t.Errorf("the request log %q did not redact the agent session", line)
	}

	// A person's own token has no session or harness to log, and the
	// path goes through the redactor like every other free-text field.
	logs = syncBuffer{}
	rec = serve(h, http.MethodGet, "/api/v1/things/secret", "person-read", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d, want 200", rec.Code)
	}
	line = logs.String()
	if !strings.Contains(line, `actor="user:paul token:pr000000000a"`) {
		t.Errorf("the request log %q does not name the person's token", line)
	}
	if strings.Contains(line, "agent_session") || strings.Contains(line, "agent_harness") {
		t.Errorf("the request log %q carries agent fields for a person's token", line)
	}
	if !strings.Contains(line, "path=/api/v1/things/[redacted]") {
		t.Errorf("the request log %q did not redact the path", line)
	}

	// A refused request never reaches a handler and logs no actor line:
	// the refusal is Refuse's to log.
	logs = syncBuffer{}
	rec = serve(h, http.MethodPost, "/api/v1/things", "agent-read", `{"title":"x"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("read-only POST = %d, want 403", rec.Code)
	}
	if line := logs.String(); strings.Contains(line, "api: request") {
		t.Errorf("a refused request logged an actor line: %q", line)
	}
}
