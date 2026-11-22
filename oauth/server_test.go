package oauth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/api"
	"amadan.net/rastrillo/rastrillo/apitoken"
	"amadan.net/rastrillo/rastrillo/oauth"
	"amadan.net/rastrillo/rastrillo/sessions"
)

func config(t *testing.T, base string) oauth.Config {
	t.Helper()
	return oauth.Config{
		Store: newStore(t), Tokens: new(apitoken.Store),
		Origin: "https://a.test", Base: base, SiteName: "A",
		Session: func(*http.Request) (sessions.Session, bool) {
			return sessions.Session{}, false
		},
		SignIn: func(string, bool) string { return "/in" },
		Allowed: func(context.Context, string) (bool, string, error) {
			return true, "", nil
		},
		Render: func(http.ResponseWriter, *http.Request, oauth.Page) error {
			return nil
		},
	}
}

func newServer(t *testing.T, base string) *oauth.Server {
	t.Helper()
	server, err := oauth.New(config(t, base))
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func TestNewRefuses(t *testing.T) {
	tests := []struct {
		name string
		edit func(*oauth.Config)
	}{
		{"store", func(config *oauth.Config) { config.Store = nil }},
		{"tokens", func(config *oauth.Config) { config.Tokens = nil }},
		{"origin", func(config *oauth.Config) { config.Origin = "" }},
		{"session", func(config *oauth.Config) { config.Session = nil }},
		{"sign-in", func(config *oauth.Config) { config.SignIn = nil }},
		{"allowed", func(config *oauth.Config) { config.Allowed = nil }},
		{"render", func(config *oauth.Config) { config.Render = nil }},
		{"insecure origin", func(config *oauth.Config) {
			config.Origin = "http://a.test"
		}},
		{"origin with a path", func(config *oauth.Config) {
			config.Origin = "https://a.test/t"
		}},
		{
			"base without a slash",
			func(config *oauth.Config) { config.Base = "t" },
		},
		{"base with a trailing slash", func(config *oauth.Config) {
			config.Base = "/t/"
		}},
		// Each would panic in ServeMux at Mount, or name another path.
		{"base with a brace", func(config *oauth.Config) {
			config.Base = "/{t}"
		}},
		{"base with a space", func(config *oauth.Config) {
			config.Base = "/a b"
		}},
		{"base with an empty segment", func(config *oauth.Config) {
			config.Base = "/a//b"
		}},
		{"base with a query", func(config *oauth.Config) {
			config.Base = "/a?b"
		}},
		{"base with a dot segment", func(config *oauth.Config) {
			config.Base = "/a/../b"
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := config(t, "")
			tt.edit(&settings)
			if _, err := oauth.New(settings); err == nil {
				t.Fatal("New accepted it")
			}
		})
	}
	for _, base := range []string{"/t", "/tenant-1/a.b_c~d"} {
		if _, err := oauth.New(config(t, base)); err != nil {
			t.Errorf("base %s: %v", base, err)
		}
	}
	for _, origin := range []string{
		"http://127.0.0.1:8080", "http://localhost:3000", "http://[::1]:80",
	} {
		settings := config(t, "")
		settings.Origin = origin
		if _, err := oauth.New(settings); err != nil {
			t.Errorf("loopback origin %s: %v", origin, err)
		}
	}
}

func get(server *oauth.Server, path string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	server.Mount(mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	return recorder
}

func TestMetadata(t *testing.T) {
	for _, base := range []string{"", "/t"} {
		metadataServer := newServer(t, base)
		iss := "https://a.test" + base
		const props = `"scopes_supported":["read","write"],` +
			`"bearer_methods_supported":["header"]}` + "\n"
		resource := func(door string) string {
			return `{"resource":"` + iss + door +
				`","authorization_servers":["` + iss + `"],` + props
		}
		const wellKnown = "/.well-known/oauth-protected-resource"
		docs := map[string]string{
			wellKnown:                 resource("/api/v1/mcp"),
			wellKnown + "/api/v1/mcp": resource("/api/v1/mcp"),
			wellKnown + "/api/v1":     resource("/api/v1"),
		}
		server := `{"issuer":"` + iss + `",` +
			`"authorization_endpoint":"` + iss + `/oauth/authorize",` +
			`"token_endpoint":"` + iss + `/oauth/token",` +
			`"registration_endpoint":"` + iss + `/oauth/register",` +
			`"revocation_endpoint":"` + iss + `/oauth/revoke",` +
			`"response_types_supported":["code"],` +
			`"grant_types_supported":["authorization_code","refresh_token"],` +
			`"code_challenge_methods_supported":["S256"],` +
			`"token_endpoint_auth_methods_supported":["none"],` +
			`"scopes_supported":["read","write"],` +
			`"client_id_metadata_document_supported":true,` +
			`"authorization_response_iss_parameter_supported":true}` + "\n"
		docs["/.well-known/oauth-authorization-server"] = server
		docs["/.well-known/openid-configuration"] = server
		for path, want := range docs {
			recorder := get(metadataServer, base+path)
			if recorder.Code != 200 || recorder.Body.String() != want {
				t.Errorf("%s: status %d\n got %s\nwant %s",
					base+path, recorder.Code, recorder.Body, want)
			}
			contentType := recorder.Header().Get("Content-Type")
			if contentType != "application/json" {
				t.Errorf("%s: Content-Type %q", path, contentType)
			}
		}
	}
}

func TestChallengePerDoor(t *testing.T) {
	for _, base := range []string{"", "/t"} {
		server := newServer(t, base)
		for _, door := range []string{"/api/v1", "/api/v1/mcp"} {
			challenge := server.Challenge(door)
			if challenge.Scope != "read write" {
				t.Errorf("scope = %q", challenge.Scope)
			}
			path, ok := strings.CutPrefix(
				challenge.ResourceMetadata,
				"https://a.test",
			)
			if !ok {
				t.Fatalf("challenge URL %q", challenge.ResourceMetadata)
			}
			recorder := get(server, path)
			if recorder.Code != 200 {
				t.Fatalf("%s: status %d", path, recorder.Code)
			}
			want := `"resource":"https://a.test` + base + door + `"`
			if !strings.Contains(recorder.Body.String(), want) {
				t.Errorf("%s: %s lacks %s", path, recorder.Body, want)
			}
		}
	}
}

func TestRoutesNoStore(t *testing.T) {
	server := newServer(t, "/t")
	routes := server.Routes()
	if len(routes) != 10 {
		t.Fatalf("%d routes", len(routes))
	}
	for _, rt := range routes {
		if rt.Reason == "" || !strings.HasPrefix(rt.Pattern, "/t/") {
			t.Errorf("%s %s: reason %q", rt.Method, rt.Pattern, rt.Reason)
		}
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(rt.Method, rt.Pattern, nil)
		rt.Handler.ServeHTTP(recorder, request)
		if cc := recorder.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("%s %s: Cache-Control %q (status %d)",
				rt.Method, rt.Pattern, cc, recorder.Code)
		}
	}
}

func TestRoutesBrowserFlag(t *testing.T) {
	var got []string
	for _, rt := range newServer(t, "").Routes() {
		if rt.Browser {
			got = append(got, rt.Method+" "+rt.Pattern)
		}
	}
	want := "GET /oauth/authorize POST /oauth/authorize"
	if strings.Join(got, " ") != want {
		t.Fatalf("browser routes %v", got)
	}
}

func TestMountServesRegister(t *testing.T) {
	server := newServer(t, "")
	mux := http.NewServeMux()
	server.Mount(mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost,
		"/oauth/register", strings.NewReader(`{"client_name":"A",`+
			`"redirect_uris":["https://x.test/cb"],`+
			`"token_endpoint_auth_method":"none"}`)))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", recorder.Code, recorder.Body)
	}
	recorder = httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost,
		"/oauth/token", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("token: %d", recorder.Code)
	}
}

type failingStore struct{ oauth.Store }

func (failingStore) Connection(context.Context, string) (
	oauth.Connection, error,
) {
	return oauth.Connection{}, errors.New("disk on fire")
}

func TestGrant(t *testing.T) {
	ctx := context.Background()
	settings := config(t, "/t")
	allowed := true
	settings.Allowed = func(context.Context, string) (bool, string, error) {
		return allowed, "closed", nil
	}
	server, err := oauth.New(settings)
	if err != nil {
		t.Fatal(err)
	}
	mcp := "https://a.test/t/api/v1/mcp"
	add := func(id, resource string, access rastrillo.Access) {
		conn := connection(id)
		conn.Resource, conn.Access = resource, access
		if err := settings.Store.CreateConnection(ctx, conn,
			code("h"+id, id)); err != nil {
			t.Fatal(err)
		}
	}
	add("w", mcp, rastrillo.Write)
	add("r", "https://a.test/t/api/v1", rastrillo.Read)
	add("x", "https://elsewhere.test/api/v1", rastrillo.Read)
	add("e", mcp, rastrillo.Read)
	if err := settings.Store.EndConnection(ctx, "e", t0); err != nil {
		t.Fatal(err)
	}

	grant, err := server.Grant(ctx, "w", rastrillo.Write)
	want := rastrillo.Grant{
		ID: "w", Issuer: "https://a.test/t",
		IdentityIssuer: "https://a.test/t", Audience: mcp,
		Scopes: []string{"read", "write"},
	}
	if err != nil || grant.ID != want.ID || grant.Issuer != want.Issuer ||
		grant.IdentityIssuer != want.IdentityIssuer ||
		grant.Audience != want.Audience ||
		strings.Join(grant.Scopes, ",") != "read,write" {
		t.Fatalf("live: %+v, %v", grant, err)
	}
	grant, err = server.Grant(ctx, "r", rastrillo.Write)
	if err != nil || grant.Audience != "https://a.test/t/api/v1" ||
		strings.Join(grant.Scopes, ",") != "read" {
		t.Fatalf("read: %+v, %v", grant, err)
	}
	// A refresh may narrow a token below its connection's access.
	grant, err = server.Grant(ctx, "w", rastrillo.Read)
	if err != nil || strings.Join(grant.Scopes, ",") != "read" {
		t.Fatalf("narrowed: %+v, %v", grant, err)
	}
	for _, id := range []string{"e", "x", "missing"} {
		if _, err := server.Grant(ctx, id, rastrillo.Write); !errors.Is(
			err,
			api.ErrUnauthorized,
		) {
			t.Errorf("%s: %v, want ErrUnauthorized", id, err)
		}
	}
	allowed = false
	if _, err := server.Grant(ctx, "w", rastrillo.Write); !errors.Is(
		err,
		api.ErrUnauthorized,
	) {
		t.Errorf("not allowed: %v", err)
	}

	settings.Allowed = func(context.Context, string) (bool, string, error) {
		return false, "", errors.New("directory down")
	}
	server, _ = oauth.New(settings)
	if _, err := server.Grant(ctx, "w", rastrillo.Write); !errors.Is(
		err,
		api.ErrUnavailable,
	) {
		t.Errorf("Allowed failed: %v", err)
	}
	settings.Store = failingStore{settings.Store}
	server, _ = oauth.New(settings)
	if _, err := server.Grant(ctx, "w", rastrillo.Write); !errors.Is(
		err,
		api.ErrUnavailable,
	) {
		t.Errorf("store failed: %v", err)
	}
}
