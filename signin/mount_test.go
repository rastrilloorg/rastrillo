package signin

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/go-chi/chi/v5"

	"amadan.net/rastrillo/rastrillo/action"
)

func listed(routes *action.Routes) []string {
	var lines []string
	for _, route := range routes.List() {
		lines = append(lines, route.Method+" "+route.Path+" "+route.Handler)
	}
	slices.Sort(lines)
	return lines
}

var passkeyRoutes = []string{
	"GET /signin/add/passkey showAddPasskey",
	"GET /signin/js/register.mjs passkeyRegisterModule",
	"GET /signin/js/signin.mjs passkeyModule",
	"GET /signin/js/webauthn.mjs webauthnModule",
	"POST /passkey/discover/begin passkeySignInBegin",
	"POST /passkey/discover/finish passkeySignInFinish",
	"POST /passkey/register/begin passkeyRegisterBegin",
	"POST /passkey/register/finish passkeyRegisterFinish",
}

var authenticatorRoutes = []string{
	"GET /signin/add/authenticator showAddAuthenticator",
	"POST /signin/add/authenticator addAuthenticator",
	"POST /signin/code signInWithCode",
}

var passphraseRoutes = []string{
	"GET /signin/add/passphrase showAddPassphrase",
	"GET /signin/js/reveal.mjs revealModule",
	"POST /signin/add/passphrase addPassphrase",
	"POST /signin/passphrase signInWithPassphrase",
}

var managementRoutes = []string{
	"GET /account/signin signinOptions",
	"GET /signin/faster showOffer",
	"POST /account/signin/remove removeSigninWay",
	"POST /signin/faster/later laterOffer",
}

// rastrillo routes calls an app's Routes with an empty App, whose
// *Signins is nil: Mount lists every way's routes then, all a full app
// serves, rather than stopping the listing.
func TestANilSigninsMountsEveryWay(t *testing.T) {
	var unbuilt *Signins
	routes := action.NewRecorder()
	unbuilt.Mount(routes)
	want := slices.Concat(passkeyRoutes, authenticatorRoutes,
		passphraseRoutes, managementRoutes)
	slices.Sort(want)
	if got := listed(routes); !slices.Equal(got, want) {
		t.Errorf("routes\n got %q\nwant %q", got, want)
	}
}

func TestMountRegistersEveryRoute(t *testing.T) {
	for _, row := range []struct {
		name string
		ways Ways
		want [][]string
	}{
		{"all ways", Passkey | Authenticator | Passphrase,
			[][]string{passkeyRoutes, authenticatorRoutes,
				passphraseRoutes, managementRoutes}},
		{"passkey only", Passkey,
			[][]string{passkeyRoutes, managementRoutes}},
		{"authenticator only", Authenticator,
			[][]string{authenticatorRoutes, managementRoutes}},
		{"passphrase only", Passphrase,
			[][]string{passphraseRoutes, managementRoutes}},
		{"link only", 0, nil},
	} {
		test := newRig(t, func(config *Config) { config.Ways = row.ways })
		routes := action.NewRecorder()
		test.signins.Mount(routes)
		want := slices.Concat(row.want...)
		slices.Sort(want)
		if got := listed(routes); !slices.Equal(got, want) {
			t.Errorf("%s: routes\n got %q\nwant %q", row.name, got, want)
		}
	}
}

func TestModulesServeAsJavaScript(t *testing.T) {
	test := newRig(t, nil)
	router := chi.NewRouter()
	routes := action.NewRoutes(router, nil)
	test.signins.Mount(routes)
	for _, path := range []string{WebauthnModulePath, PasskeyModulePath,
		RegisterModulePath, RevealModulePath} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response,
			httptest.NewRequest(http.MethodGet, path, nil))
		header := response.Header()
		if response.Code != http.StatusOK ||
			header.Get("Content-Type") != "text/javascript; charset=utf-8" ||
			header.Get("Cache-Control") != "no-cache" ||
			response.Body.Len() == 0 {
			t.Errorf("%s: %d %q %q, %d bytes", path, response.Code,
				header.Get("Content-Type"), header.Get("Cache-Control"),
				response.Body.Len())
		}
		again := httptest.NewRequest(http.MethodGet, path, nil)
		again.Header.Set("If-None-Match", response.Header().Get("ETag"))
		second := httptest.NewRecorder()
		router.ServeHTTP(second, again)
		if second.Code != http.StatusNotModified {
			t.Errorf("%s: revalidation answered %d", path, second.Code)
		}
	}
}
