//go:build browser

package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/csrf"
	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/passkey"
	"amadan.net/rastrillo/rastrillo/secondfactor"
	"amadan.net/rastrillo/rastrillo/sessions"
	"amadan.net/rastrillo/rastrillo/ui"
	"amadan.net/rastrillo/rastrillo/webauthn"
)

// screenApp is a whole app on the shipped screen: auth with
// SigninScreen, passkey discovery wired to its jar, the stage shell and
// the vendored assets — served through rastrillo.Handler, so every page
// carries the framework's real CSP, form-action 'self' included.
// Classification and the token exchange go to the in-process fake;
// the browser's own navigation to keymail.test is caught by CDP's Fetch
// domain (interceptProvider). Nothing real is contacted.
type screenApp struct {
	auth *Auth
	mail *captureMailer
}

func newScreenApp(t *testing.T) (*screenApp, func(origin string) http.Handler) {
	app := &screenApp{mail: &captureMailer{}}
	return app, func(origin string) http.Handler {
		d, err := db.Open(filepath.Join(t.TempDir(), "screen.db"), nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { d.Close() })
		if _, err := migrate.Apply(context.Background(), d, migrate.Merge(sessions.Schema, Schema, passkey.Schema, secondfactor.Schema)); err != nil {
			t.Fatal(err)
		}
		a, err := New(Config{DB: d.Writer(), Origin: origin, InstanceKey: "browser-instance-key", Mailer: app.mail, SigninScreen: true})
		if err != nil {
			t.Fatal(err)
		}
		wireKeymail(a, kayFake())
		app.auth = a
		sess, err := sessions.New(sessions.Config{DB: d.Writer(), Origin: origin})
		if err != nil {
			t.Fatal(err)
		}
		pk, err := passkey.New(passkey.Config{Sessions: sess, DB: d.Writer(), Origin: origin, Remember: a.RememberJar()})
		if err != nil {
			t.Fatal(err)
		}

		funcs := ui.Funcs()
		funcs["asset"] = func(p string) string { return "/" + p }
		stage, _ := ui.Layout("stage")
		tmpl := template.Must(template.New("layout").Funcs(funcs).ParseFS(ui.Templates(), "*.html"))
		template.Must(tmpl.Parse(string(stage)))
		template.Must(tmpl.Parse(`{{define "title"}}{{template "signin-title" (dict "State" .Signin "Brand" .Brand)}}{{end}}` +
			`{{define "content"}}{{template "signin" (dict "State" .Signin "Brand" .Brand)}}{{end}}`))

		mux := http.NewServeMux()
		assets, _ := ui.VendoredAssets("day")
		for name, body := range assets {
			ct := "text/css; charset=utf-8"
			if strings.HasSuffix(name, ".js") {
				ct = "text/javascript; charset=utf-8"
			}
			mux.HandleFunc("GET /static/"+name, serveBytes(body, ct))
		}
		mux.HandleFunc("GET /static/webauthn.mjs", serveBytes(webauthn.JS(), "text/javascript; charset=utf-8"))
		mux.HandleFunc("GET /static/passkey-signin.mjs", serveBytes(passkey.JS(), "text/javascript; charset=utf-8"))
		signinPage := func(door bool) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				st := a.SigninState(r)
				if door {
					st.Passkey = &PasskeyDoor{
						BeginPath: "/passkey/discover/begin", FinishPath: "/passkey/discover/finish",
						ModuleURL: "/static/webauthn.mjs", ScriptURL: "/static/passkey-signin.mjs",
					}
				}
				a.PrepareSigninResponse(w, st)
				var buf bytes.Buffer
				if err := tmpl.ExecuteTemplate(&buf, "layout", map[string]any{"Signin": st, "Brand": map[string]any{"Name": "Harbour"}}); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Write(buf.Bytes())
			}
		}
		mux.HandleFunc("GET /signin", signinPage(true))
		// The same screen in an app with no passkeys: email only.
		mux.HandleFunc("GET /signin-plain", signinPage(false))
		// An ordinary page, to show the door's module is not everyone's.
		mux.HandleFunc("GET /about", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<!doctype html><html lang="en"><title>About</title><main><h1 id="about">About</h1></main></html>`)
		})
		mux.HandleFunc("POST /signin", a.Begin)
		mux.HandleFunc("POST /signin/forget", a.Forget)
		mux.HandleFunc("GET /auth/callback", a.Callback)
		mux.HandleFunc("GET /auth/verify", a.Verify)
		mux.HandleFunc("POST /signout", a.Signout)
		mux.HandleFunc("POST /passkey/discover/begin", pk.DiscoverBegin)
		mux.HandleFunc("POST /passkey/discover/finish", pk.DiscoverFinish)
		mux.HandleFunc("POST /passkey/register/begin", pk.RegisterBegin)
		mux.HandleFunc("POST /passkey/register/finish", pk.RegisterFinish)
		// Home names who is signed in, so a drive proves where a sign-in
		// landed and as whom, not just that some page has an h1.
		mux.Handle("GET /{$}", a.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, _ := From(r)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<!doctype html><html lang="en"><title>Home</title><main><h1 id="home">%s</h1></main></html>`, template.HTMLEscapeString(id.Address))
		})))
		// csrf.Protect as an app mounts it, so the drive's passkey and form
		// POSTs pass the same check they would in production.
		h, closeAll, err := rastrillo.Handler(rastrillo.Options{Mux: mux, Wrap: csrf.Protect(origin)})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { closeAll() })
		return h
	}
}

func serveBytes(body []byte, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Write(body)
	}
}

// provider is the drive's view of everything that leaves the app: it
// answers every request to keymail.test with a stand-in page, reports
// only the authorize-document navigations (a provider page's own
// favicon must not look like a second sign-in), and — while holdRefresh
// is set — serves the continuation page without its meta refresh, so
// the fallback link can be clicked on the real page under its real CSP.
type provider struct {
	authorize   chan string
	holdRefresh atomic.Bool
}

var metaRefresh = regexp.MustCompile(`<meta http-equiv="refresh"[^>]*>`)

func interceptProvider(rig *harness.Rig) *provider {
	p := &provider{authorize: make(chan string, 8)}
	ctx := rig.Context()
	chromedp.ListenTarget(ctx, func(ev any) {
		e, ok := ev.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		// Answered from a goroutine: a CDP call made inside a listener
		// deadlocks the event loop it is waiting on.
		go func() {
			ectx := cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Target)
			if strings.Contains(e.Request.URL, "/signin?continue=") {
				if !p.holdRefresh.Load() {
					_ = fetch.ContinueRequest(e.RequestID).Do(ectx)
					return
				}
				body, err := fetch.GetResponseBody(e.RequestID).Do(ectx)
				if err != nil {
					_ = fetch.ContinueRequest(e.RequestID).Do(ectx)
					return
				}
				_ = fetch.FulfillRequest(e.RequestID, e.ResponseStatusCode).
					WithResponseHeaders(e.ResponseHeaders).
					WithBody(base64.StdEncoding.EncodeToString(metaRefresh.ReplaceAll(body, nil))).Do(ectx)
				return
			}
			if e.ResourceType == network.ResourceTypeDocument && strings.HasPrefix(e.Request.URL, "https://keymail.test/oauth/authorize?") {
				p.authorize <- e.Request.URL
			}
			body := base64.StdEncoding.EncodeToString([]byte(`<!doctype html><html lang="en"><title>Keymail</title><main><h1 id="provider">Keymail stand-in</h1></main></html>`))
			_ = fetch.FulfillRequest(e.RequestID, http.StatusOK).
				WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).
				WithBody(body).Do(ectx)
		}()
	})
	// The '?' is escaped: in a Fetch pattern it is a one-character
	// wildcard, not the query's start.
	rig.Run(fetch.Enable().WithPatterns([]*fetch.RequestPattern{
		{URLPattern: "https://keymail.test/*"},
		{URLPattern: `*/signin\?continue=*`, RequestStage: fetch.RequestStageResponse},
	}))
	return p
}

// requestsFor counts the requests the page makes whose URL path is path.
func requestsFor(rig *harness.Rig, path string) func() int {
	var mu sync.Mutex
	n := 0
	chromedp.ListenTarget(rig.Context(), func(ev any) {
		if e, ok := ev.(*network.EventRequestWillBeSent); ok {
			if u, err := url.Parse(e.Request.URL); err == nil && u.Path == path {
				mu.Lock()
				n++
				mu.Unlock()
			}
		}
	})
	return func() int {
		mu.Lock()
		defer mu.Unlock()
		return n
	}
}

// continuations records every /signin?continue= document the browser
// loaded, with its Content-Security-Policy.
type continuations struct {
	mu   sync.Mutex
	seen [][2]string
}

func watchContinuations(rig *harness.Rig) *continuations {
	c := &continuations{}
	chromedp.ListenTarget(rig.Context(), func(ev any) {
		e, ok := ev.(*network.EventResponseReceived)
		if !ok || e.Type != network.ResourceTypeDocument || !strings.Contains(e.Response.URL, "/signin?continue=") {
			return
		}
		csp, _ := e.Response.Headers["Content-Security-Policy"].(string)
		c.mu.Lock()
		c.seen = append(c.seen, [2]string{e.Response.URL, csp})
		c.mu.Unlock()
	})
	return c
}

func (c *continuations) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.seen)
}

func (c *continuations) last(t *testing.T) (id, csp string) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.seen) == 0 {
		t.Fatal("no /signin?continue= document was loaded")
	}
	u, err := url.Parse(c.seen[len(c.seen)-1][0])
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("continue"), c.seen[len(c.seen)-1][1]
}

func awaitURL(t *testing.T, p *provider) string {
	t.Helper()
	select {
	case u := <-p.authorize:
		return u
	case <-time.After(15 * time.Second):
		t.Fatal("the continuation page never navigated to keymail")
		return ""
	}
}

// driveBudget bounds each step of a drive. rig.Run has no deadline of
// its own, so a page that never shows what a step waits for — a
// sign-in that did not land — would hang until the test binary's own
// timeout, ten minutes later, instead of failing as itself.
const driveBudget = 30 * time.Second

// run is rig.Run with driveBudget, and the same report: what was on
// screen when the step failed.
func run(t *testing.T, rig *harness.Rig, actions ...chromedp.Action) {
	t.Helper()
	ctx, cancel := context.WithTimeout(rig.Context(), driveBudget)
	defer cancel()
	if err := chromedp.Run(ctx, actions...); err != nil {
		var text string
		_ = chromedp.Run(rig.Context(), chromedp.Evaluate(`document.body ? document.body.innerText : ""`, &text))
		t.Fatalf("drive failed: %v\non screen:\n%s", err, text)
	}
}

func eval(t *testing.T, rig *harness.Rig, expr string, out any) {
	t.Helper()
	run(t, rig, chromedp.Evaluate(expr, out, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
		return p.WithAwaitPromise(true)
	}))
}

// awaitAutofocus resolves to the focused element once autofocus has
// run. Autofocus is applied at a rendering update, not at load, so
// reading activeElement the moment the page is visible sometimes finds
// <body> on a page whose autofocus is right — a flaky red that says
// nothing. Two seconds is far past any rendering update; a page with no
// autofocus at all still resolves, to <body>, and fails as itself.
const awaitAutofocus = `(async () => {
  for (let i = 0; i < 40 && document.activeElement === document.body; i++) await new Promise(r => setTimeout(r, 50));
  return document.activeElement;
})()`

// sentMarker matches only the Sent page: the address it repeats back
// sits in a paragraph straight under the door. A Returning one-tap
// carries a <bdi> too, inside its button or form, so waiting on any bdi
// after clicking a one-tap would pass on the page being left and read
// its address back as if Sent had said it.
const sentMarker = `[rst-signin-door] > p > bdi`

// awaitSent waits for the Sent page and returns the address it names,
// after checking the URL is Sent's own: a marker alone could be met by
// a page that merely looks like it.
func awaitSent(t *testing.T, rig *harness.Rig) string {
	t.Helper()
	var here, s string
	run(t, rig, chromedp.WaitVisible(sentMarker, chromedp.ByQuery), chromedp.Location(&here))
	if u, err := url.Parse(here); err != nil || u.Path != "/signin" || u.Query().Get("sent") != "1" {
		t.Fatalf("the Sent marker showed on %q, not a /signin?sent=1 page", here)
	}
	eval(t, rig, `document.querySelector("`+sentMarker+`").textContent`, &s)
	return s
}

var verifyLink = regexp.MustCompile(`http://localhost:\d+/auth/verify\?token=[A-Za-z0-9_-]+`)

const registerPasskey = `(async () => {
  const m = await import("/static/webauthn.mjs");
  const post = (u, b) => fetch(u, {method: "POST", headers: {"Content-Type": "application/json"}, body: JSON.stringify(b || {})});
  const begin = await (await post("/passkey/register/begin")).json();
  const r = await m.register({challenge: begin.challenge, rpId: location.hostname, userId: "YWRh", userName: "ada@example.com"});
  return (await post("/passkey/register/finish", {clientDataJSON: r.clientDataJSON, attestationObject: r.attestationObject, label: "drive"})).status;
})()`

// signOut posts /signout as a page's own fetch would and requires what
// Signout promises — a same-origin redirect to the sign-in page — and
// then that the session is really gone: home sends the browser back to
// sign in.
func signOut(t *testing.T, rig *harness.Rig) {
	t.Helper()
	var raw string
	eval(t, rig, `fetch("/signout", {method: "POST"}).then(r => JSON.stringify({status: r.status, redirected: r.redirected, path: new URL(r.url).pathname}))`, &raw)
	var out struct {
		Status     int
		Redirected bool
		Path       string
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	if out.Status != http.StatusOK || !out.Redirected || out.Path != "/signin" {
		t.Fatalf("POST /signout answered %+v; want a redirect that lands on /signin", out)
	}
	var here string
	run(t, rig, chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible(`[rst-signin]`, chromedp.ByQuery), chromedp.Location(&here))
	if u, _ := url.Parse(here); u.Path != "/signin" {
		t.Fatalf("after signing out, home is %q; want the sign-in page", here)
	}
}

// reflowJS measures what WCAG 1.4.10 asks at 320px: nothing past either
// side, and nothing above the top. The edges are measured element by
// element, not only scrollWidth, because a centred card that is too
// wide spills off the START edge too, and no scrollbar reaches that.
// What sits inside a clipping or scrolling box is that box's business,
// not the page's: the backdrop's art is drawn wider than the screen on
// purpose and cut off by its scene.
const reflowJS = `(() => {
  const de = document.documentElement;
  const over = [];
  const clipped = el => {
    for (let p = el.parentElement; p && p !== document.body; p = p.parentElement) {
      if (getComputedStyle(p).overflowX !== "visible") return true;
    }
    return false;
  };
  document.querySelectorAll("body *").forEach(el => {
    const r = el.getBoundingClientRect();
    if (r.width === 0 || clipped(el)) return;
    if (r.right > de.clientWidth + 1 || r.left < -1 || r.top + scrollY < -1) {
      over.push(el.tagName.toLowerCase() + " [" + Math.round(r.left) + "…" + Math.round(r.right) + ", top " + Math.round(r.top + scrollY) + "]");
    }
  });
  if (de.scrollWidth > de.clientWidth) over.unshift("document " + de.scrollWidth + "px in " + de.clientWidth + "px");
  return over.slice(0, 8).join("; ");
})()`

func requireReflow(t *testing.T, rig *harness.Rig, what string) {
	t.Helper()
	var over string
	eval(t, rig, reflowJS, &over)
	if over != "" {
		t.Fatalf("%s spills out of a 320px viewport: %s", what, over)
	}
}

// longAddress has no break opportunity in it — no hyphen, no space — so
// only the card's own overflow-wrap can keep it inside 320px.
const longAddress = "averyverylonglocalpartwithnobreakopportunityatall@anequallylongdomainwithnobreaksanywhere.example"

func TestSigninScreenInTheBrowser(t *testing.T) {
	app, build := newScreenApp(t)
	rig := harness.New(t, build)
	keymail := interceptProvider(rig)
	conts := watchContinuations(rig)
	moduleLoads := requestsFor(rig, "/static/passkey-signin.mjs")
	catalog := rastrillo.BaseCatalog()
	var s string
	var ok bool

	// The control for every successful POST below: the wrapper is really
	// there. Discovery's begin has no origin check of its own, so without
	// csrf.Protect a cross-origin POST would get a challenge, not a 403.
	req, _ := http.NewRequest(http.MethodPost, rig.Origin+"/passkey/discover/begin", strings.NewReader("{}"))
	req.Header.Set("Origin", "https://evil.example")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	refused, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden || !strings.Contains(string(refused), "cross-origin") {
		t.Fatalf("a cross-origin POST got %d %q; csrf.Protect is not in front of the app", res.StatusCode, refused)
	}

	// Ordinary pages and an email-only sign-in screen never ask for the
	// passkey door's module; the screen with a door does.
	run(t, rig, chromedp.Navigate(rig.Origin+"/about"), chromedp.WaitVisible("#about", chromedp.ByQuery))
	run(t, rig, chromedp.Navigate(rig.Origin+"/signin-plain"), chromedp.WaitVisible("#rst-signin-email", chromedp.ByQuery))
	rig.Screen("[rst-signin]", "the email-only screen")
	if n := moduleLoads(); n != 0 {
		t.Fatalf("%d requests for the passkey module from pages with no passkey door", n)
	}

	// Ask: focus starts in the field, and Tab reaches Continue, then the
	// passkey door, which its module revealed.
	run(t, rig, chromedp.Navigate(rig.Origin+"/signin"), chromedp.WaitVisible(`[data-rst-passkey]`, chromedp.ByQuery))
	if moduleLoads() == 0 {
		t.Fatal("the screen with a passkey door never loaded its module")
	}
	rig.Screen("[rst-signin]", "the Ask screen")
	eval(t, rig, awaitAutofocus+`.then(el => el.id)`, &s)
	if s != "rst-signin-email" {
		t.Fatalf("focus starts on %q, want the email field", s)
	}
	run(t, rig, chromedp.KeyEvent(kb.Tab))
	eval(t, rig, `document.activeElement.type + " " + document.activeElement.closest("form")?.getAttribute("action")`, &s)
	if s != "submit /signin" {
		t.Fatalf("Tab from the field reached %q, want Continue", s)
	}
	run(t, rig, chromedp.KeyEvent(kb.Tab))
	eval(t, rig, `document.activeElement.hasAttribute("data-rst-passkey")`, &ok)
	if !ok {
		t.Fatal("the next Tab stop is not the passkey door")
	}

	// A typed address: the Sent page names it.
	run(t, rig,
		chromedp.SendKeys("#rst-signin-email", "ada@example.com", chromedp.ByQuery),
		chromedp.Click(`form[action="/signin"] button[type="submit"]`, chromedp.ByQuery),
	)
	if s = awaitSent(t, rig); s != "ada@example.com" {
		t.Fatalf("Sent names %q", s)
	}
	rig.Screen("[rst-signin]", "the Sent screen")

	// The link signs Ada in; she enrols a passkey and signs out.
	run(t, rig, chromedp.Navigate(verifyLink.FindString(app.mail.body)), chromedp.WaitVisible("#home", chromedp.ByQuery))
	var status float64
	eval(t, rig, registerPasskey, &status)
	if status != http.StatusOK {
		t.Fatalf("registering a passkey answered %v", status)
	}
	signOut(t, rig)

	// The next visit offers the one-tap, focused, naming Ada; Enter on it
	// — the keyboard, not a click — posts the remembered address.
	run(t, rig, chromedp.Navigate(rig.Origin+"/signin"), chromedp.WaitVisible(`form[action="/signin"] button[autofocus]`, chromedp.ByQuery))
	rig.Screen("[rst-signin]", "the Returning screen")
	eval(t, rig, awaitAutofocus+`.then(b => b.tagName + " " + (b.querySelector("bdi")?.textContent ?? ""))`, &s)
	if s != "BUTTON ada@example.com" {
		t.Fatalf("Returning focuses %q, want the one-tap naming Ada", s)
	}
	run(t, rig, chromedp.KeyEvent(kb.Enter))
	if s = awaitSent(t, rig); s != "ada@example.com" {
		t.Fatalf("the one-tap's Sent page names %q", s)
	}

	// "Use a different email" returns to an empty Ask. The field is the
	// marker: the Returning page being left has none.
	run(t, rig, chromedp.Navigate(rig.Origin+"/signin"), chromedp.WaitVisible(`form[action="/signin/forget"] button`, chromedp.ByQuery),
		chromedp.Click(`form[action="/signin/forget"] button`, chromedp.ByQuery), chromedp.WaitVisible("#rst-signin-email", chromedp.ByQuery))
	eval(t, rig, `document.getElementById("rst-signin-email").value + "|" + document.querySelectorAll("button[autofocus]").length`, &s)
	if s != "|0" {
		t.Fatalf("after Use a different email the field and one-taps are %q, want an empty field and none", s)
	}

	// Keymail, scripts on: POST → 303 → the continuation page → the
	// provider, with the page's CSP still form-action 'self'. A CSP
	// violation is a console error, which the Screen below reports.
	run(t, rig,
		chromedp.SendKeys("#rst-signin-email", "kay@example.org", chromedp.ByQuery),
		chromedp.Click(`form[action="/signin"] button[type="submit"]`, chromedp.ByQuery),
	)
	went := awaitURL(t, keymail)
	if !app.auth.validAuthorizeURL(went) {
		t.Fatalf("the continuation went to %q", went)
	}
	rig.Screen("#provider", "the provider, reached by the meta refresh")
	id, csp := conts.last(t)
	if !strings.Contains(csp, "form-action 'self'") || strings.Contains(csp, "keymail.test") {
		t.Fatalf("the continuation page's CSP is %q; the point is that form-action stays 'self'", csp)
	}
	// The fallback link, clicked: the same continuation page with its
	// refresh held back, so the link is what navigates — and it reaches
	// the same URL the refresh did.
	keymail.holdRefresh.Store(true)
	run(t, rig, chromedp.Navigate(rig.Origin+"/signin?continue="+id), chromedp.WaitVisible(`[rst-signin-door] a[rst-btn]`, chromedp.ByQuery))
	rig.Screen("[rst-signin]", "the Continue screen")
	select {
	case u := <-keymail.authorize:
		t.Fatalf("with its refresh held back the Continue page still went to %q by itself", u)
	default:
	}
	run(t, rig, chromedp.Click(`[rst-signin-door] a[rst-btn]`, chromedp.ByQuery))
	if byLink := awaitURL(t, keymail); byLink != went {
		t.Fatalf("the fallback link went to %q; the refresh went to %q", byLink, went)
	}
	rig.Screen("#provider", "the provider, reached by the fallback link")
	keymail.holdRefresh.Store(false)

	// Scripts off: the passkey door stays hidden, an address still gets
	// its Sent page, and the keymail continuation still moves on — the
	// meta refresh needs no script.
	// The same half-second is given to the page scripts-on would need to
	// reveal the door, so "hidden" is the scripts' absence and not a read
	// taken before they ran.
	run(t, rig, emulation.SetScriptExecutionDisabled(true), chromedp.Navigate(rig.Origin+"/signin"), chromedp.WaitVisible("#rst-signin-email", chromedp.ByQuery),
		chromedp.Sleep(500*time.Millisecond))
	eval(t, rig, `document.querySelector("[data-rst-passkey]").hidden`, &ok)
	if !ok {
		t.Fatal("with scripts off the passkey door is showing")
	}
	run(t, rig,
		chromedp.SetValue("#rst-signin-email", "sam@example.com", chromedp.ByQuery),
		chromedp.Click(`form[action="/signin"] button[type="submit"]`, chromedp.ByQuery),
	)
	if s = awaitSent(t, rig); s != "sam@example.com" {
		t.Fatalf("scripts off, Sent names %q", s)
	}
	rig.Screen("[rst-signin]", "the Sent screen, scripts off")
	before := conts.count()
	run(t, rig, chromedp.Navigate(rig.Origin+"/signin"), chromedp.WaitVisible("#rst-signin-email", chromedp.ByQuery))
	run(t, rig,
		chromedp.SetValue("#rst-signin-email", "kay@example.org", chromedp.ByQuery),
		chromedp.Click(`form[action="/signin"] button[type="submit"]`, chromedp.ByQuery),
	)
	if went := awaitURL(t, keymail); !app.auth.validAuthorizeURL(went) {
		t.Fatalf("scripts off, the continuation went to %q", went)
	}
	rig.Screen("#provider", "the provider, scripts off")
	if conts.count() == before {
		t.Fatal("scripts off, keymail reached the provider without passing the continuation page")
	}
	if _, csp := conts.last(t); !strings.Contains(csp, "form-action 'self'") {
		t.Fatalf("scripts off, the continuation page's CSP is %q", csp)
	}
	run(t, rig, emulation.SetScriptExecutionDisabled(false))

	// A passkey signs in from nothing, and the next visit is Returning
	// (passkey) with the typed address gone — kay's attempt cookie from
	// the keymail leg above is still set until the passkey ends it.
	run(t, rig, chromedp.Navigate(rig.Origin+"/signin"), chromedp.WaitVisible(`[data-rst-passkey]`, chromedp.ByQuery))
	eval(t, rig, `document.getElementById("rst-signin-email").value`, &s)
	if s != "kay@example.org" {
		t.Fatalf("before the passkey the field holds %q, want kay's typed address", s)
	}
	// Wait for home itself — the sign-in page has an h1 too — and check
	// the URL and who it says is signed in before leaving it, so the
	// ceremony's cookies are set before the next navigation.
	var here string
	run(t, rig, chromedp.Click(`[data-rst-passkey]`, chromedp.ByQuery), chromedp.WaitVisible("#home", chromedp.ByQuery), chromedp.Location(&here))
	eval(t, rig, `document.getElementById("home").textContent`, &s)
	if here != rig.Origin+"/" || s != "ada@example.com" {
		t.Fatalf("the passkey sign-in landed on %q as %q, want / as ada@example.com", here, s)
	}
	run(t, rig, chromedp.Navigate(rig.Origin+"/signin"), chromedp.WaitVisible(`[data-rst-passkey]`, chromedp.ByQuery))
	rig.Screen("[rst-signin]", "the Returning (passkey) screen")
	eval(t, rig, `document.getElementById("rst-signin-email").value + "|" + document.querySelector("[data-rst-passkey]").textContent.trim()`, &s)
	if want := "|" + catalog["rastrillo.ui.signin_passkey_remembered"]; s != want {
		t.Fatalf("after a passkey sign-in: %q, want %q", s, want)
	}

	// 320px, with an address no line break can split: Sent shows it back
	// and the one-tap carries it on its label, and neither may push the
	// page sideways.
	signOut(t, rig)
	run(t, rig, chromedp.EmulateViewport(320, 640))
	run(t, rig, chromedp.Navigate(rig.Origin+"/signin"), chromedp.WaitVisible("#rst-signin-email", chromedp.ByQuery),
		chromedp.SetValue("#rst-signin-email", longAddress, chromedp.ByQuery),
		chromedp.Click(`form[action="/signin"] button[type="submit"]`, chromedp.ByQuery))
	if s = awaitSent(t, rig); s != longAddress {
		t.Fatalf("Sent names %q", s)
	}
	requireReflow(t, rig, "the Sent page with a long address")
	run(t, rig, chromedp.Navigate(verifyLink.FindString(app.mail.body)), chromedp.WaitVisible("#home", chromedp.ByQuery))
	signOut(t, rig)
	run(t, rig, chromedp.Navigate(rig.Origin+"/signin"), chromedp.WaitVisible(`form[action="/signin"] button[autofocus]`, chromedp.ByQuery))
	eval(t, rig, `document.querySelector("form[action='/signin'] button[autofocus] bdi").textContent`, &s)
	if s != longAddress {
		t.Fatalf("the one-tap names %q", s)
	}
	requireReflow(t, rig, "the Returning one-tap with a long address")
	rig.Screen("[rst-signin]", "the Returning screen at 320px")
	run(t, rig, chromedp.EmulateViewport(1280, 800))
}

// The passkey door's failure and busy paths, with navigator.credentials
// replaced before any page script runs: a prompt the person dismissed
// (the same NotAllowedError as "no passkey here"), and one that never
// answers, to prove a second click starts nothing.
const passkeyStandIn = `(() => {
  const real = window.fetch.bind(window);
  window.__begins = 0;
  window.fetch = (u, o) => { if (String(u).includes("/passkey/discover/begin")) window.__begins++; return real(u, o); };
  navigator.credentials.get = () => window.__hold
    ? new Promise(() => {})
    : Promise.reject(new DOMException("dismissed", "NotAllowedError"));
})()`

func TestThePasskeyDoorFailsQuietlyAndOnce(t *testing.T) {
	_, build := newScreenApp(t)
	rig := harness.New(t, build)
	run(t, rig, chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument(passkeyStandIn).Do(ctx)
		return err
	}))
	run(t, rig, chromedp.Navigate(rig.Origin+"/signin"), chromedp.WaitVisible(`[data-rst-passkey]`, chromedp.ByQuery))

	var raw string
	eval(t, rig, `(async () => {
	  const b = document.querySelector("[data-rst-passkey]");
	  const msg = document.getElementById("rst-signin-passkey-msg");
	  b.focus(); b.click();
	  for (let i = 0; i < 100 && !msg.textContent; i++) await new Promise(r => setTimeout(r, 50));
	  return JSON.stringify({msg: msg.textContent, live: msg.getAttribute("aria-live"), want: b.getAttribute("data-rst-passkey-cancelled"),
	    focused: document.activeElement === b, disabled: b.disabled, busy: b.getAttribute("aria-busy")});
	})()`, &raw)
	var got struct {
		Msg, Live, Want   string
		Focused, Disabled bool
		Busy              *string
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if got.Msg == "" || got.Msg != got.Want || got.Live != "polite" || !got.Focused || got.Disabled || got.Busy != nil {
		t.Fatalf("after a dismissed prompt: %+v; want the cancelled words in the live region, focus still on the button, never disabled, no longer busy", got)
	}

	eval(t, rig, `(async () => {
	  const b = document.querySelector("[data-rst-passkey]");
	  window.__hold = true;
	  const before = window.__begins;
	  b.click(); await new Promise(r => setTimeout(r, 300));
	  b.click(); await new Promise(r => setTimeout(r, 300));
	  return JSON.stringify({started: window.__begins - before, busy: b.getAttribute("aria-busy"), disabled: b.disabled});
	})()`, &raw)
	var busy struct {
		Started  int
		Busy     string
		Disabled bool
	}
	if err := json.Unmarshal([]byte(raw), &busy); err != nil {
		t.Fatal(err)
	}
	if busy.Started != 1 || busy.Busy != "true" || busy.Disabled {
		t.Fatalf("two clicks while busy: %+v; want one ceremony, busy, and the button never disabled", busy)
	}
	rig.Screen("[rst-signin]", "the passkey door, busy")
}
