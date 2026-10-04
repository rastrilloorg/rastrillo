package rastrillo

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const rulesHeader = `"` + SpeculationRulesPath + `"`

func catchAll() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("app")) })
	return mux
}

func mustBuild(t *testing.T, o Options) http.Handler {
	t.Helper()
	h, err := buildHandler(o)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	return h
}

// Every response names the rules file (a structured-field list of one
// string): browsers act on it for documents and ignore it elsewhere, so
// it is not sniffed for. The CSP is untouched: a header-delivered
// ruleset is not a script, and no CSP stops it loading.
func TestEveryResponseNamesTheSpeculationRules(t *testing.T) {
	h := mustBuild(t, Options{Mux: helloMux(&captured{})})
	for _, path := range []string{"/hello", "/healthz"} {
		rec := get(h, path, "")
		if got := rec.Header().Get("Speculation-Rules"); got != rulesHeader {
			t.Errorf("%s: Speculation-Rules = %q, want %q", path, got, rulesHeader)
		}
		if got := rec.Header().Get("Content-Security-Policy"); got != defaultCSP {
			t.Errorf("%s: the CSP changed: %q", path, got)
		}
	}
}

// The file: 200, the one content type browsers accept for a ruleset,
// cached a day, and document rules scoped by selector in both spellings
// to shell navigation and the back control, inside a root that names
// its view. A bare root is an app's layout copied before the phone
// index, whose pages may run startup scripts nobody wrote to run early.
func TestTheSpeculationRulesFileIsServed(t *testing.T) {
	for _, o := range []Options{
		{Mux: helloMux(&captured{})},
		{Mux: helloMux(&captured{}), Locales: []string{"en", "fr"}},
	} {
		rec := get(mustBuild(t, o), SpeculationRulesPath, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("locales %v: GET %s = %d", o.Locales, SpeculationRulesPath, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/speculationrules+json" {
			t.Errorf("Content-Type = %q; a browser refuses a ruleset served as anything else", ct)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=86400" {
			t.Errorf("Cache-Control = %q", cc)
		}
		var rules struct {
			Prerender []struct {
				Source string `json:"source"`
				Where  struct {
					SelectorMatches string `json:"selector_matches"`
				} `json:"where"`
				Eagerness string `json:"eagerness"`
			} `json:"prerender"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &rules); err != nil {
			t.Fatalf("the rules are not JSON: %v\n%s", err, rec.Body.String())
		}
		if len(rules.Prerender) != 1 || rules.Prerender[0].Source != "document" || rules.Prerender[0].Eagerness != "moderate" {
			t.Fatalf("rules = %+v, want one moderate document rule", rules)
		}
		sel := rules.Prerender[0].Where.SelectorMatches
		for _, want := range []string{
			"[rst-shell-sidebar~=page]", "[rst-shell-sidebar~=index]", "[rst-shell-console~=page]", "[rst-shell-console~=index]",
			".rst-shell-sidebar--page", ".rst-shell-sidebar--index", ".rst-shell-console--page", ".rst-shell-console--index",
			"[rst-shell-nav]", ".rst-shell__nav", "[rst-shell-back]", ".rst-shell__back", "a[href]"} {
			if !strings.Contains(sel, want) {
				t.Errorf("the selector %q does not name %q", sel, want)
			}
		}
		for _, bare := range []string{"[rst-shell-sidebar]", "[rst-shell-console]", ".rst-shell-sidebar,", ".rst-shell-console,", ".rst-shell-console)"} {
			if strings.Contains(sel, bare) {
				t.Errorf("the selector %q names the bare root %q, which an old layout matches", sel, bare)
			}
		}
	}
}

// The method-and-path pattern is more specific than an app's "/", so the
// framework answers the rules path even under a catch-all.
func TestTheFrameworkAnswersTheRulesPathOverACatchAll(t *testing.T) {
	rec := get(mustBuild(t, Options{Mux: catchAll()}), SpeculationRulesPath, "")
	if rec.Body.String() == "app" || !strings.Contains(rec.Body.String(), `"prerender"`) {
		t.Errorf("under a catch-all the app answered the rules path: %q", rec.Body.String())
	}
}

// The switch: no header and no route, so the path falls to the app like
// any other: a 404 from an app with nothing there, the app's own answer
// from one with a catch-all.
func TestNoSpeculationRulesSendsNothingAndServesNothing(t *testing.T) {
	h := mustBuild(t, Options{Mux: http.NewServeMux(), NoSpeculationRules: true})
	rec := get(h, "/", "")
	if got := rec.Header().Get("Speculation-Rules"); got != "" {
		t.Errorf("NoSpeculationRules: the header is still sent: %q", got)
	}
	if rec := get(h, SpeculationRulesPath, ""); rec.Code != http.StatusNotFound {
		t.Errorf("NoSpeculationRules: GET %s = %d, want the app's 404", SpeculationRulesPath, rec.Code)
	}
	if rec := get(mustBuild(t, Options{Mux: catchAll(), NoSpeculationRules: true}), SpeculationRulesPath, ""); rec.Body.String() != "app" {
		t.Errorf("NoSpeculationRules: a catch-all app did not get the path: %q", rec.Body.String())
	}
}

// Set before any app code runs, like the other baseline headers, so a
// handler or Options.Wrap that deletes it wins.
func TestAHandlerCanDropTheSpeculationRulesHeader(t *testing.T) {
	h := mustBuild(t, Options{Mux: helloMux(&captured{}), Wrap: func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Del("Speculation-Rules")
			next.ServeHTTP(w, r)
		})
	}})
	if got := get(h, "/hello", "").Header().Get("Speculation-Rules"); got != "" {
		t.Errorf("a Wrap that Dels the header did not remove it: %q", got)
	}
}

// The framework's own route only answers GET (and so HEAD, which
// net/http derives from it); a POST to the path is the app's business,
// not the framework's to intercept.
func TestSpeculationRulesRouteIsGETOnly(t *testing.T) {
	h := mustBuild(t, Options{Mux: catchAll()})
	head := httptest.NewRecorder()
	h.ServeHTTP(head, httptest.NewRequest(http.MethodHead, SpeculationRulesPath, nil))
	if head.Code != http.StatusOK || head.Header().Get("Content-Type") != "application/speculationrules+json" {
		t.Errorf("HEAD %s = %d %q, want the GET route's answer", SpeculationRulesPath, head.Code, head.Header().Get("Content-Type"))
	}
	post := httptest.NewRecorder()
	h.ServeHTTP(post, httptest.NewRequest(http.MethodPost, SpeculationRulesPath, nil))
	if post.Body.String() != "app" {
		t.Errorf("POST %s went to the framework (%d %q), want the app", SpeculationRulesPath, post.Code, post.Body.String())
	}
}
