//go:build browser

package ui

import (
	"context"
	"html/template"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	cdppage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/pow"
)

// powFormPage serves one form-foot form behind a real pow.Guard, with
// tokens.css, theme.css and busy.js linked as an app links them, and a
// paragraph after the form for anything in it to move. pow.js waits
// for release, or is refused outright when blocked.
func powFormPage(
	t *testing.T, release <-chan struct{}, blocked bool,
) http.Handler {
	t.Helper()
	assets := rastrillo.NewAssets(pow.Assets())
	script := "/pow" + assets.Path("pow.js")
	guard, err := pow.New(pow.Config{InstanceKey: "ui",
		Nonces: pow.MemoryNonces(), Difficulty: 14,
		MinAge:    50 * time.Millisecond,
		ScriptURL: script, WorkerURL: "/pow" + assets.Path("pow-worker.js")})
	if err != nil {
		t.Fatal(err)
	}
	tmpl := template.Must(template.New("").Funcs(Funcs()).
		ParseFS(Templates(), "*.html"))
	template.Must(tmpl.New("page").Parse(`<!doctype html><html lang="en">` +
		`<head><meta charset="utf-8"><title>Contact</title>` +
		`<link rel="stylesheet" href="/tokens.css">` +
		`<link rel="stylesheet" href="/theme.css">` +
		`<script defer src="/busy.js"></script></head><body>` +
		`<form rst-form method="post" action="/submit" {{.Attrs}}>` +
		`{{.Fields}}<label for="name">Name</label>` +
		`<input id="name" name="name">` +
		`{{template "form-foot" dict "Submit" "Send" "Proof" .}}</form>` +
		`<p id="after">We answer within a day.</p>{{.Script}}</body></html>`))
	pows := http.StripPrefix("/pow/", assets.Handler())
	mux := http.NewServeMux()
	mux.HandleFunc("GET /pow/", func(
		response http.ResponseWriter, request *http.Request,
	) {
		if request.URL.Path == script {
			if blocked {
				http.NotFound(response, request)
				return
			}
			<-release
		}
		pows.ServeHTTP(response, request)
	})
	serve := func(name string, body []byte, kind string) {
		mux.HandleFunc("GET /"+name, func(
			response http.ResponseWriter, _ *http.Request,
		) {
			response.Header().Set("Content-Type", kind)
			response.Write(body)
		})
	}
	theme, _ := ThemeCSS(ThemeNames()[0])
	serve("tokens.css", TokensCSS(), "text/css")
	serve("theme.css", theme, "text/css")
	serve("busy.js", BusyJS(), "text/javascript")
	mux.HandleFunc("GET /{$}", func(
		response http.ResponseWriter, _ *http.Request,
	) {
		form := guard.Form(time.Now(), "contact")
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.ExecuteTemplate(response, "page", &form); err != nil {
			t.Error(err)
		}
	})
	return mux
}

// powWatch runs before any page script. It keeps every layout shift,
// the submit's box in the first frame drawn with it, and whether the
// submit ever wore the busy look, and notes when the proof is done.
const powWatch = `(() => {
  const submit = () => document.querySelector("[rst-form-foot] button");
  const box = (b) => { const r = b.getBoundingClientRect();
    return [r.x, r.y, r.width, r.height].join(","); };
  window.__shifts = [];
  window.__box = box;
  new PerformanceObserver((list) => {
    for (const e of list.getEntries()) window.__shifts.push(
      e.value.toFixed(4) + " moving " + e.sources.map((s) =>
        s.node ? s.node.id || s.node.tagName : "?").join(", "));
  }).observe({ type: "layout-shift", buffered: true });
  const frame = () => {
    if (!submit()) return requestAnimationFrame(frame);
    window.__firstBox = box(submit());
  };
  requestAnimationFrame(frame);
  new MutationObserver(() => {
    const b = submit();
    if (b && b.getAttribute("aria-busy") === "true" &&
      b.querySelector("[rst-spin]")) window.__busy = true;
  }).observe(document, { subtree: true, childList: true, attributes: true });
  document.addEventListener("pow:solved", () => { window.__solved = true; },
    true);
})()`

// powFormState is what a drive reads once the form has settled.
type powFormState struct {
	FirstBox, Box, Status string
	Shifts                []string
	Busy                  bool
}

const powFormStateJS = `(() => {
  const b = document.querySelector("[rst-form-foot] button");
  const s = document.querySelector("[data-pow-status]");
  const r = s.getBoundingClientRect();
  return { FirstBox: window.__firstBox || "", Box: window.__box(b),
    Shifts: window.__shifts, Busy: !!window.__busy,
    Status: [r.width > 1 && s.checkVisibility() ? "drawn" : "not drawn",
      s.getAttribute("role") || "no role", s.textContent].join(" | ") };
})()`

func powFormRig(
	t *testing.T, release <-chan struct{}, blocked bool,
) *harness.Rig {
	t.Helper()
	rig := harness.New(t, func(string) http.Handler {
		return powFormPage(t, release, blocked)
	})
	rig.Run(chromedp.EmulateViewport(320, 640), chromedp.Func(func(
		ctx context.Context, target *chromedp.Target,
	) error {
		_, err := cdp.Call(ctx, target,
			cdppage.AddScriptToEvaluateOnNewDocument,
			cdppage.AddScriptToEvaluateOnNewDocumentParams{Source: powWatch})
		return err
	}))
	return rig
}

// TestAPowFormNeverMoves is #437 on a form-foot form: pow's status was
// a line from the first paint that pow.js hid once it had loaded,
// moving everything under it. Now nothing appears or disappears: the
// submit is busy while the proof computes, and the words are a live
// region only a screen reader hears.
func TestAPowFormNeverMoves(t *testing.T) {
	release := make(chan struct{})
	rig := powFormRig(t, release, false)
	// Not chromedp.Navigate: it waits for the load event, which waits
	// for the held pow.js.
	rig.Run(chromedp.Func(func(
		ctx context.Context, target *chromedp.Target,
	) error {
		_, err := cdp.Call(ctx, target, cdppage.Navigate,
			cdppage.NavigateParams{URL: rig.Origin + "/"})
		return err
	}), chromedp.WaitVisible(chromedp.CSS(`#after`)))
	// Long enough for the first frames to be drawn without pow.js.
	time.Sleep(300 * time.Millisecond)
	close(release)
	rig.Run(chromedp.Poll[chromedp.Void](`window.__solved === true && `+
		`!document.querySelector("[rst-form-foot] button[aria-busy]")`,
		chromedp.WithPollingTimeout(20*time.Second)))
	var got powFormState
	rig.Run(chromedp.Sleep(200*time.Millisecond),
		harness.Into(&got, chromedp.Evaluate[powFormState](powFormStateJS)))
	t.Logf("submit first drawn at %s, at %s once solved; %d layout "+
		"shifts %q; status %q", got.FirstBox, got.Box, len(got.Shifts),
		got.Shifts, got.Status)
	if len(got.Shifts) != 0 {
		t.Errorf("%d layout shifts while pow.js loaded and solved: %q",
			len(got.Shifts), got.Shifts)
	}
	if got.FirstBox == "" || got.FirstBox != got.Box {
		t.Errorf("the submit was first drawn at %q and is at %q once "+
			"the proof is done", got.FirstBox, got.Box)
	}
	if !got.Busy {
		t.Error("the submit never looked busy while the proof computed")
	}
	if want := "not drawn | status | "; got.Status != want {
		t.Errorf("once solved the status is %q, want %q", got.Status, want)
	}
}

// With pow.js refused, the form says why its button does nothing: the
// hint, hidden until then, is drawn. Moving the page is the lesser
// harm here.
func TestABlockedPowFormExplainsItself(t *testing.T) {
	rig := powFormRig(t, nil, true)
	rig.Run(chromedp.Navigate(rig.Origin+"/"),
		chromedp.WaitVisible(chromedp.CSS(`#after`)),
		chromedp.Sleep(500*time.Millisecond))
	var got powFormState
	rig.Run(harness.Into(&got,
		chromedp.Evaluate[powFormState](powFormStateJS)))
	if !strings.HasPrefix(got.Status, "drawn | status | ") ||
		!strings.Contains(got.Status, "reload") {
		t.Errorf("with pow.js blocked the status is %q, want the hint "+
			"drawn", got.Status)
	}
	disabled := harness.Evaluate[bool](rig,
		`document.querySelector("[rst-form-foot] button").disabled`)
	if !disabled {
		t.Error("with pow.js blocked the submit is enabled")
	}
}
