//go:build browser

package ui

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	cdppage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/locales"
	"amadan.net/rastrillo/rastrillo/pow"
)

// powSubmitCase is one pow form an app writes, drawn on the stage
// shell with the slice of tokens.css ui.Pages links for it.
type powSubmitCase struct {
	name, content string
	forced        bool // under forced colours
}

// The forms opt out of busy.js, so nothing but pow.js puts a spinner in
// them, and the slice keeps its rules only if pow's own hooks do.
var powSubmitCases = []powSubmitCase{
	{name: "a form-foot submit", content: `<form rst-form method="post" ` +
		`action="/submit" data-busy="false" {{.Proof.Attrs}}>` +
		`{{.Proof.Fields}}<label for="name">Name</label>` +
		`<input id="name" name="name">` +
		`{{template "form-foot" dict "L" (localizer $) "Submit" "Send" ` +
		`"Proof" .Proof}}` +
		`</form>{{.Proof.Script}}`},
	// markup-spelling: old-spelling begin — a button in the class
	// spelling, which pow's spinner must match as well.
	{name: "a class-spelled submit", content: `<form class="rst-form" ` +
		`method="post" action="/submit" data-busy="false" ` +
		`{{.Proof.Attrs}}>{{.Proof.Fields}}` +
		`<button class="rst-btn rst-btn--primary" type="submit" disabled ` +
		`data-pow-submit>Send</button>{{.Proof.StatusLine "Reload"}}` +
		`</form>{{.Proof.Script}}`},
	// markup-spelling: old-spelling end
	{name: "a form-foot submit under forced colours", forced: true},
}

// powSubmitWatch runs before any page script. It keeps the submit's
// box and where its words are drawn in the first frame, again the
// moment it first goes busy, and notes when the proof is done.
const powSubmitWatch = `(() => {
  const submit = () =>
    document.querySelector("form[action='/submit'] [data-pow-submit]");
  const place = (b) => {
    const text = [...b.childNodes].find((n) =>
      n.nodeType === 3 && n.textContent.trim());
    const range = document.createRange();
    range.selectNodeContents(text);
    const words = range.getBoundingClientRect();
    const r = b.getBoundingClientRect();
    return [r.x, r.y, r.width, r.height, words.x, words.y].join(",");
  };
  window.__place = () => place(submit());
  const frame = () => {
    if (!submit()) return requestAnimationFrame(frame);
    window.__first = place(submit());
  };
  requestAnimationFrame(frame);
  new MutationObserver(() => {
    const b = submit();
    if (!window.__busy && b && b.getAttribute("aria-busy") === "true")
      window.__busy = place(b);
  }).observe(document, { subtree: true, childList: true, attributes: true });
  document.addEventListener("pow:solved", () => { window.__solved = true; },
    true);
})()`

// TestAPowSubmitKeepsItsPlace is #437's review: the spinner pow.js
// puts in a submit while the proof computes must not move the button
// or its words, on the slice an app's page links, for a button in
// either spelling, and under forced colours, where Fiesta would draw
// the spinner beside the words, so pow.js draws none.
func TestAPowSubmitKeepsItsPlace(t *testing.T) {
	for _, test := range powSubmitCases {
		t.Run(test.name, func(t *testing.T) {
			if test.content == "" {
				test.content = powSubmitCases[0].content
			}
			release := make(chan struct{})
			rig := harness.New(t, func(string) http.Handler {
				return powSubmitPage(t, test.content, release)
			})
			t.Cleanup(func() { closeOnce(release) })
			first, busy, last := drivePowSubmit(t, rig, test, release)
			t.Logf("button and words %s first, %s busy, %s solved",
				first, busy, last)
			if busy == "" {
				t.Fatal("the submit never went busy")
			}
			if first != busy || first != last {
				t.Errorf("the submit and its words moved: %s first, %s "+
					"busy, %s once solved (x,y,w,h of the button, x,y of "+
					"the words)", first, busy, last)
			}
		})
	}
}

func closeOnce(gate chan struct{}) {
	select {
	case <-gate:
	default:
		close(gate)
	}
}

func drivePowSubmit(
	t *testing.T, rig *harness.Rig, test powSubmitCase,
	release chan struct{},
) (first, busy, last string) {
	t.Helper()
	features := []*emulation.MediaFeature{}
	if test.forced {
		features = append(features,
			&emulation.MediaFeature{Name: "forced-colors", Value: "active"})
	}
	rig.Run(chromedp.EmulateViewport(320, 640), chromedp.Func(func(
		ctx context.Context, target *chromedp.Target,
	) error {
		_, err := cdp.Call(ctx, target, emulation.SetEmulatedMedia,
			emulation.SetEmulatedMediaParams{Features: features})
		if err != nil {
			return err
		}
		_, err = cdp.Call(ctx, target,
			cdppage.AddScriptToEvaluateOnNewDocument,
			cdppage.AddScriptToEvaluateOnNewDocumentParams{
				Source: powSubmitWatch})
		if err != nil {
			return err
		}
		// Not chromedp.Navigate: it waits for the load event, which
		// waits for the held pow.js.
		_, err = cdp.Call(ctx, target, cdppage.Navigate,
			cdppage.NavigateParams{URL: rig.Origin + "/contact"})
		return err
	}), chromedp.Poll[chromedp.Void](`!!window.__first`,
		chromedp.WithPollingTimeout(10*time.Second)))
	first = harness.Evaluate[string](rig, `window.__first`)
	closeOnce(release)
	rig.Run(chromedp.Poll[chromedp.Void](`window.__solved === true && `+
		`!document.querySelector("[data-pow-submit][aria-busy]")`,
		chromedp.WithPollingTimeout(20*time.Second)))
	busy = harness.Evaluate[string](rig, `window.__busy || ""`)
	last = harness.Evaluate[string](rig, `window.__place()`)
	return first, busy, last
}

// powSubmitPage serves content as an app's page on the stage shell,
// through ui.Pages with the day theme's vendored files, so it links
// the page's slice; pow.js waits for release.
func powSubmitPage(
	t *testing.T, content string, release <-chan struct{},
) http.Handler {
	t.Helper()
	assets := themeAssets(t, "day")
	config, err := locales.CheckConfig("en", []string{"en"})
	if err != nil {
		t.Fatal(err)
	}
	layout, _ := Layout("stage")
	app := fstest.MapFS{"layout.html": {Data: layout},
		"contact.html": {Data: []byte(`{{define "content"}}` + content +
			`{{end}}`)}}
	pages, err := NewPages(PagesConfig{App: app, Layout: "layout.html",
		Pages:  map[string]PageFile{"contact": {Path: "contact.html"}},
		Locale: config, LocaleFS: app, Assets: assets,
		Funcs: map[string]any{"asset": assets.Path,
			"iconAssets": func() string { return "" }}})
	if err != nil {
		t.Fatal(err)
	}
	powAssets := rastrillo.NewAssets(pow.Assets())
	script := "/pow" + powAssets.Path("pow.js")
	guard, err := pow.New(pow.Config{InstanceKey: "ui",
		Nonces: pow.MemoryNonces(), Difficulty: 14, ScriptURL: script,
		WorkerURL: "/pow" + powAssets.Path("pow-worker.js")})
	if err != nil {
		t.Fatal(err)
	}
	pows := http.StripPrefix("/pow/", powAssets.Handler())
	mux := http.NewServeMux()
	mux.Handle("GET /static/", assets.Handler())
	mux.HandleFunc("GET /pow/", func(
		response http.ResponseWriter, request *http.Request,
	) {
		if request.URL.Path == script {
			<-release
		}
		pows.ServeHTTP(response, request)
	})
	mux.HandleFunc("GET /contact", func(
		response http.ResponseWriter, _ *http.Request,
	) {
		form := guard.Form(time.Now(), "contact")
		data := struct {
			Localized
			Proof *pow.Form
		}{Localized{L: pages.Localizer("en")}, &form}
		var page strings.Builder
		if err := pages.Execute(&page, "contact", data); err != nil {
			t.Error(err)
		}
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		response.Write([]byte(page.String()))
	})
	return mux
}
