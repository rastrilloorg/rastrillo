//go:build browser

package signin

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/go-chi/chi/v5"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/action"
	"amadan.net/rastrillo/rastrillo/auth"
	"amadan.net/rastrillo/rastrillo/csrf"
	"amadan.net/rastrillo/rastrillo/harness"
	_ "amadan.net/rastrillo/rastrillo/locales/en"
	"amadan.net/rastrillo/rastrillo/totp"
	"amadan.net/rastrillo/rastrillo/ui"
)

// browserApp is a scaffolded app's sign-in in miniature: auth's
// one-way screen, every way signin adds, the gate in front of every
// page, and the stage shell drawing signin-view, served through
// rastrillo.Handler so the pages carry the framework's real CSP.
type browserApp struct {
	signins *Signins
	page    *template.Template
}

// newBrowserApp builds the app for the harness, which knows its origin
// only once it is listening.
func newBrowserApp(
	t *testing.T, app *browserApp,
) func(origin string) http.Handler {
	return func(origin string) http.Handler {
		database := schema.Open(t)
		authority, err := auth.New(auth.Config{
			DB: database, Origin: origin, InstanceKey: "browser-instance-key",
			Mailer: quietMailer{}, SigninScreen: true, SigninOneWay: true,
			ProofOff: true, KeymailServers: []string{},
		})
		if err != nil {
			t.Fatalf("auth.New: %v", err)
		}
		app.page = browserTemplate(t)
		app.signins, err = New(Config{
			Auth: authority, Sessions: authority.Sessions(), DB: database,
			Origin: origin, Key: [32]byte{7}, Issuer: "Harbour",
			Ways:     Passkey | Authenticator | Passphrase,
			ProofOff: true, Render: app.render,
			Logger: slog.New(slog.DiscardHandler),
		})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		return app.handler(t, authority, origin)
	}
}

func browserTemplate(t *testing.T) *template.Template {
	funcs := ui.Funcs()
	funcs["asset"] = func(path string) string { return "/" + path }
	stage, _ := ui.Layout("stage")
	page := template.Must(template.New("layout").Funcs(funcs).
		ParseFS(ui.Templates(), "*.html"))
	template.Must(page.Parse(string(stage)))
	template.Must(page.Parse(`{{define "title"}}` +
		`{{template "signin-view-title" .}}{{end}}` +
		`{{define "content"}}{{template "signin-view" .}}{{end}}`))
	return page
}

func (app *browserApp) render(
	response http.ResponseWriter, request *http.Request, view View,
) {
	var body bytes.Buffer
	err := app.page.ExecuteTemplate(&body, "layout", map[string]any{
		"View": view, "Brand": map[string]any{"Name": "Harbour"},
	})
	if err != nil {
		http.Error(response, err.Error(), http.StatusInternalServerError)
		return
	}
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	if view.Status != 0 {
		response.WriteHeader(view.Status)
	}
	response.Write(body.Bytes())
}

// handler mounts what the scaffold's mountAuth does, a home page only a
// signed-in person can open, and the vendored assets.
func (app *browserApp) handler(
	t *testing.T, authority *auth.Auth, origin string,
) http.Handler {
	router := chi.NewRouter()
	routes := action.NewRoutes(router, nil)
	routes.Handle(http.MethodGet, "/signin", http.HandlerFunc(func(
		response http.ResponseWriter, request *http.Request,
	) {
		state := authority.SigninState(request)
		app.signins.Fill(&state)
		authority.PrepareSigninResponse(response, state)
		app.render(response, request, View{Signin: &state})
	}), "signinScreen")
	routes.Handle(http.MethodPost, "/signin/forget",
		http.HandlerFunc(authority.Forget), "forgetAddress")
	routes.Handle(http.MethodPost, "/signout",
		http.HandlerFunc(authority.Signout), "signout")
	app.signins.Mount(routes)
	routes.Handle(http.MethodGet, "/",
		authority.RequireSession(http.HandlerFunc(home)), "home")
	assets, _ := ui.VendoredAssets("day")
	for name, body := range assets {
		routes.Handle(http.MethodGet, action.Path("/static/"+name),
			staticFile(name, body), "static "+name)
	}
	mux := http.NewServeMux()
	mux.Handle("/", app.signins.Gate(router))
	handler, closeAll, err := rastrillo.Handler(rastrillo.Options{
		Mux: mux, Wrap: csrf.Protect(origin)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeAll() })
	return handler
}

// home names who is signed in, so the walk proves as whom it landed,
// and offers Sign out as the scaffold's layout does.
func home(response http.ResponseWriter, request *http.Request) {
	identity, _ := auth.From(request)
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(response, `<!doctype html><html lang="en"><title>Home`+
		`</title><main><h1 id="home">%s</h1><form method="post" `+
		`action="/signout"><button id="signout">Sign out</button></form>`+
		`</main></html>`, template.HTMLEscapeString(identity.Address))
}

func staticFile(name string, body []byte) http.HandlerFunc {
	kind := "text/css; charset=utf-8"
	if strings.HasSuffix(name, ".js") {
		kind = "text/javascript; charset=utf-8"
	}
	return func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", kind)
		response.Write(body)
	}
}

// enrolBrowser confirms an authenticator for address with the previous
// step's code, leaving the current step's code unspent for the walk.
func enrolBrowser(t *testing.T, signins *Signins, address string) string {
	t.Helper()
	enrolment, err := signins.codes.Begin(address, address)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	earlier, _ := totp.Code(enrolment.Key, time.Now().Add(-30*time.Second))
	if confirmed, err := signins.codes.Confirm(address, earlier); err != nil ||
		!confirmed {
		t.Fatalf("Confirm: %v, %v", confirmed, err)
	}
	return enrolment.Key
}

// The walk's controls, by what they post or where they go.
const (
	otherWays     = `a[href*="ways=1"]`
	authenticator = `a[href*="way=authenticator"]`
	codeDoor      = `form[action="/signin/code"]`
	codeField     = `#rst-signin-code`
	addressField  = `#rst-signin-email`
)

// Signing in by code through the one-way screen remembers the way: after
// signing out and loading the page again, it leads with the code door,
// the address already in its field, and "Other ways to sign in" below.
func TestTheAuthenticatorWayRememberedInTheBrowser(t *testing.T) {
	app := &browserApp{}
	rig := harness.New(t, newBrowserApp(t, app))
	key := enrolBrowser(t, app.signins, ada)

	walk(t, rig, chromedp.Navigate(rig.Origin+"/signin"),
		chromedp.WaitVisible(chromedp.CSS(otherWays)),
		chromedp.Click(chromedp.CSS(otherWays)),
		arrive(authenticator),
		chromedp.Click(chromedp.CSS(authenticator)),
		arrive(codeField))
	code, err := totp.Code(key, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	walk(t, rig, chromedp.SendKeys(chromedp.CSS(addressField), ada),
		chromedp.SendKeys(chromedp.CSS(codeField), code),
		chromedp.Click(chromedp.CSS(codeDoor+` button[type="submit"]`)),
		arrive("#home"))
	if got := textAt(t, rig, "#home"); got != ada {
		t.Fatalf("signed in as %q, want %q", got, ada)
	}

	walk(t, rig, chromedp.Click(chromedp.CSS("#signout")),
		arrive("[rst-signin]"),
		chromedp.Reload(),
		chromedp.WaitVisible(chromedp.CSS(codeField)))
	rig.Screen("[rst-signin]", "the code door, remembered")
	leadsWithTheCodeDoor(t, rig)
}

// leadsWithTheCodeDoor checks the remembered page: the code door is
// the one way in, its address field holds the address, and "Other ways
// to sign in" sits below the door.
func leadsWithTheCodeDoor(t *testing.T, rig *harness.Rig) {
	t.Helper()
	var address string
	evaluate(t, rig, find(codeDoor+" "+addressField)+".value", &address)
	if address != ada {
		t.Errorf("the code door's address field holds %q, want %q",
			address, ada)
	}
	var doors int
	evaluate(t, rig, `document.querySelectorAll(`+
		strconv.Quote(`[rst-signin] form[action="/signin"]`)+`).length`,
		&doors)
	if doors != 0 {
		t.Errorf("the page leads with the code door and draws %d email "+
			"doors as well", doors)
	}
	var below bool
	evaluate(t, rig, find(otherWays)+".getBoundingClientRect().top >= "+
		find(codeDoor)+".getBoundingClientRect().bottom", &below)
	if !below {
		t.Error(`"Other ways to sign in" is not below the code door`)
	}
	want := rastrillo.BaseCatalog()["rastrillo.ui.signin_ways"]
	if got := textAt(t, rig, otherWays); got != want {
		t.Errorf("the link below reads %q, want %q", got, want)
	}
}

// walk drives the browser within walkBudget, so a page that never comes
// fails the test with what is on screen instead of hanging it.
func walk(
	t *testing.T, rig *harness.Rig, steps ...chromedp.Action[chromedp.Void],
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(rig.Context(), walkBudget)
	defer cancel()
	if err := chromedp.Do(ctx, steps...); err != nil {
		text, _ := chromedp.Run(rig.Context(), chromedp.Evaluate[string](
			`document.body ? document.body.innerText : ""`))
		t.Fatalf("walk failed: %v\non screen:\n%s", err, text)
	}
}

// arrive waits for the page a click navigated to: its marker visible,
// then the document loaded. A click does not wait for the load, as
// Navigate does, and the marker is parsed and visible before the
// page's stylesheets apply. A Click measured then can land where the
// element no longer is: seen as a click on <html> at y=-490 that left
// the walk on the chooser. And at DOMContentLoaded Chrome drops every DOM node
// id the driver holds, so a step that finds its node before then and
// uses it after fails "Could not find node with given id". The marker
// comes first because Poll's script dies with the page it runs in,
// while WaitVisible keeps looking across the navigation.
func arrive(marker string) chromedp.Action[chromedp.Void] {
	return chromedp.Steps(
		chromedp.WaitVisible(chromedp.CSS(marker)),
		chromedp.Poll[chromedp.Void](`document.readyState === "complete"`),
	)
}

// walkBudget is many times what each leg takes on a quiet machine
// (well under a second), for runners at a load of 40 to 80.
const walkBudget = 30 * time.Second

func evaluate[T any](t *testing.T, rig *harness.Rig, script string, out *T) {
	t.Helper()
	value, err := chromedp.Run(rig.Context(), chromedp.Evaluate[T](script))
	if err != nil {
		t.Fatalf("%s: %v", script, err)
	}
	*out = value
}

func textAt(t *testing.T, rig *harness.Rig, selector string) string {
	t.Helper()
	var text string
	evaluate(t, rig, find(selector)+
		`.textContent.replace(/\s+/g, " ").trim()`, &text)
	return text
}

func find(selector string) string {
	return "document.querySelector(" + strconv.Quote(selector) + ")"
}
