package designsystem

import (
	"fmt"
	"html/template"
	"strings"

	"amadan.net/rastrillo/rastrillo/auth"
)

// The Screens page: whole screens rather than pieces of them.
//
// Sign-in is the first group because it is the screen every app needs
// on its first day. Most of it is now the shipped signin partial,
// rendered in its states; the two it does not ship — a password screen
// and third-party buttons — stay as examples to copy.
//
// Each sign-in state is rendered from the partial itself, in a frame
// that draws the stage shell the way layouts/stage.html does, rather
// than kept as hand-written markup beside it: a copy drifts, and this
// page would then teach a screen the framework does not produce. The
// hand-written screens it replaced had drifted that way already —
// posting a field name the handler never read, linking two routes
// nothing served.
//
// So this page claims signin and signin-title (screenPartials), the way
// a family page claims its partials. buildFamilies fails on a partial no
// page claims, and those two belong here rather than on a family page:
// they are a whole screen, and this is the page about whole screens.
//
// The two examples are built only from what the other pages document —
// rst-box, rst-form, the field markup, rst-btn — because a screen using
// a wrapper nobody else has would be a screen nobody can copy.

// screenView is one screen as the page renders it.
type screenView struct {
	Name    string
	ID      string
	Marker  template.HTML
	Blurb   string
	Warning template.HTML // a callout above the screen, or empty
	Preview previewView
}

// screenDoc is one screen's source, before it is rendered.
type screenDoc struct {
	// Key is the anchor id's suffix and previewHeights' key.
	Key string
	// Name and Blurb are English, and therefore prose keys.
	Name  string
	Blurb string
	// WarningTitle and WarningBody render a callout above the screen.
	// Both empty means no callout; prose keys when set.
	WarningTitle string
	WarningBody  string
	// Markup is a hand-written screen: complete HTML with no template
	// actions, on the same footing as ui.Styleguide's samples. It is
	// what a reader copies, so it is written the way it should be
	// copied rather than the way it is easiest to generate.
	Markup string
	// Signin is a state of the shipped signin partial. A screen with one
	// is rendered from the partial itself, in a stage frame, so this page
	// cannot show a sign-in screen the framework does not produce.
	Signin *auth.SigninState
}

// screenPartials are the partials the Screens page documents. A family
// IS a page and buildFamilies refuses a partial no page claims; the
// sign-in partials belong here, on the page about whole screens, and
// this is that claim.
var screenPartials = []string{"signin", "signin-title"}

// screenPartialView is one of screenPartials as the page shows it: its
// name under the page's lead, carrying the marker the coverage gates
// count and the anchor the rail links. A family page gives each partial
// an article of its own; here the screens below are its samples, so
// the name is all the partial needs.
type screenPartialView struct {
	Name   string
	ID     string
	Marker template.HTML
}

func screenPartialViews() []screenPartialView {
	out := make([]screenPartialView, 0, len(screenPartials))
	for _, name := range screenPartials {
		out = append(out, screenPartialView{Name: name, ID: anchorID("partial", name), Marker: marker("partial", name)})
	}
	return out
}

// galleryPasskey is a passkey door for the samples. In a Preview the
// partial shows the button as the enhanced page would and writes none
// of these paths, so nothing here needs to exist.
var galleryPasskey = &auth.PasskeyDoor{BeginPath: "/passkey/discover/begin", FinishPath: "/passkey/discover/finish", ModuleURL: "/static/webauthn.mjs", ScriptURL: "/static/passkey-signin.mjs"}

// galleryBrand is sample data, not the page's voice: it stays English on
// every page, like every other sample's names. It has all three parts a
// brand can have, because a brand column with only a name is half a card
// of nothing and not what an app would ship. The mark is drawn in
// currentColor with no colours of its own, so the card's CSS gives it
// the theme's accent, as it would an app's own mark drawn the same way.
var galleryBrand = map[string]any{
	"Name":  "Harbour",
	"Pitch": "Moorings and berths, booked in a minute.",
	"Mark": template.HTML(`<svg viewBox="0 0 48 48" aria-hidden="true" focusable="false">` +
		`<circle cx="24" cy="24" r="21" fill="none" stroke="currentColor" stroke-width="3"/>` +
		`<path d="M11 29c4.5 0 4.5-4 9-4s4.5 4 9 4 4.5-4 9-4" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round"/>` +
		`<path d="M24 11v11" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round"/></svg>`),
}

const graceAddress = "grace@example.com"

// signinScreen is the Ask state at the paths an app mounts by default,
// changed by mut: each screen below says only how it differs.
func signinScreen(mut func(*auth.SigninState)) *auth.SigninState {
	st := auth.SigninState{Step: auth.StepAsk, BeginPath: "/signin", ForgetPath: "/signin/forget"}
	mut(&st)
	return &st
}

// signinSource is the Code tab for every sign-in state: the two
// template calls an app writes, not the rendered markup. The markup is
// the partial's to produce, and a copied snapshot of it would freeze a
// screen the module keeps improving.
const signinSource = `{{define "title"}}{{template "signin-title" (dict "State" .Signin "Brand" .Brand)}}{{end}}
{{define "content"}}{{template "signin" (dict "State" .Signin "Brand" .Brand)}}{{end}}`

// screenFrame is the stage shell in miniature for a preview frame: the
// same attributes layouts/stage.html writes on <body>, on a wrapper.
// Only the first frame draws the backdrop: it is the same picture in
// every frame, and nine more escaped copies of it would add some 16 KB,
// near a quarter of the page, saying nothing new.
const screenFrame = `{{define "ds-screen-stage"}}<div rst-stage><div rst-stage-scene aria-hidden="true">{{if .Art}}{{stageArt "rastrillo"}}{{end}}</div><div rst-page>{{template "signin" .}}</div></div>{{end}}`

// screenDocs is the page's content, in page order.
func screenDocs() []screenDoc {
	kay := &auth.Remembered{Method: "keymail", Address: "kay@example.org"}
	return []screenDoc{
		{Key: "signin-ask", Name: "Asking for an address",
			Blurb:  "One field and one button. Nothing to remember and nothing to steal. Rastrillo does this out of the box.",
			Signin: signinScreen(func(s *auth.SigninState) { s.Passkey = galleryPasskey })},
		{Key: "signin-returning-keymail", Name: "Coming back with Keymail",
			Blurb: "The browser remembers how it got in last time. One tap sends the remembered address, which is checked again from scratch.",
			Signin: signinScreen(func(s *auth.SigninState) {
				s.Step, s.Remembered, s.Address = auth.StepReturning, kay, kay.Address
			})},
		{Key: "signin-returning-link", Name: "Coming back with an email link",
			Blurb: "The address is on the button, so there is nothing to type.",
			Signin: signinScreen(func(s *auth.SigninState) {
				s.Step, s.Remembered = auth.StepReturning, &auth.Remembered{Method: "magiclink", Address: graceAddress}
			})},
		{Key: "signin-returning-passkey", Name: "A passkey",
			Blurb: "The fastest way in for someone who has one, and nothing to type. Offer a second way as well: people lose devices, and a passkey that will not work is a locked door.",
			Signin: signinScreen(func(s *auth.SigninState) {
				s.Step, s.Remembered, s.Passkey = auth.StepReturning, &auth.Remembered{Method: "passkey"}, galleryPasskey
			})},
		{Key: "signin-sent", Name: "After the link is sent",
			Blurb:  "Repeat the address back. It is the only chance to notice a typo before waiting for an email that will never come.",
			Signin: signinScreen(func(s *auth.SigninState) { s.Step, s.SentTo = auth.StepSent, graceAddress })},
		{Key: "signin-sent-unbound", Name: "After the link is sent, in another browser",
			Blurb:  "When this browser did not send the address, the page does not guess it.",
			Signin: signinScreen(func(s *auth.SigninState) { s.Step = auth.StepSent })},
		{Key: "signin-sent-instead", Name: "When Keymail was offered and a link went out",
			Blurb: "One line says a link was sent this time. It does not guess why.",
			Signin: signinScreen(func(s *auth.SigninState) {
				s.Step, s.SentTo, s.SentInstead = auth.StepSent, kay.Address, true
			})},
		{Key: "signin-continue", Name: "On the way to Keymail",
			Blurb: "The page moves on by itself, with a link in case it does not.",
			Signin: signinScreen(func(s *auth.SigninState) {
				s.Step, s.ContinueURL = auth.StepContinue, "https://keymail.example/oauth/authorize"
			})},
		{Key: "signin-problem-address", Name: "An address that does not look right",
			Blurb: "The message belongs to the field, and that is where focus starts.",
			Signin: signinScreen(func(s *auth.SigninState) {
				s.Problem, s.Address = auth.ProblemAddress, "grace@example"
			})},
		{Key: "signin-problem-keymail", Name: "When Keymail could not confirm it",
			Blurb: "The button changes to send a link instead, so nobody is stuck.",
			Signin: signinScreen(func(s *auth.SigninState) {
				s.Problem, s.Address = auth.ProblemKeymail, kay.Address
			})},
		{
			Key:   "signin-social",
			Name:  "Google, Apple and the rest",
			Blurb: "We give you the buttons, drawn the way each company requires. We do not give you the sign-in itself — you wire that up to whichever provider you use.",
			Markup: `<section rst-box>
  <h1>Sign in</h1>
  <form rst-form method="post" action="/auth/google"><button rst-btn type="submit">Continue with Google</button></form>
  <form rst-form method="post" action="/auth/apple"><button rst-btn type="submit">Continue with Apple</button></form>
  <p><a href="/signin">Email me a link instead</a></p>
</section>`,
		},
		{
			Key:          "signin-password",
			Name:         "Email and password",
			Blurb:        "If you do use passwords, use the same one-way-in layout, and put a way to reset directly under the button rather than hiding it.",
			WarningTitle: "We do not recommend passwords",
			WarningBody:  "Rastrillo does not ship this screen. People reuse passwords, they leak, and you inherit the job of storing them safely. Use a link in your email plus a passkey, or passkeys on their own. The markup is here because some products still need it.",
			// name="email", not address: this form posts to the password
			// plugin, whose handler reads email (password/handlers.go).
			Markup: `<section rst-box>
  <h1>Sign in</h1>
  <form rst-form method="post" action="/signin">
    <div rst-field>
      <label rst-field-label for="pw-email">Email</label>
      <input rst-input id="pw-email" name="email" type="email" autocomplete="email" required>
    </div>
    <div rst-field>
      <label rst-field-label for="pw-pass">Password</label>
      <input rst-input id="pw-pass" name="password" type="password" autocomplete="current-password" required>
    </div>
    <button rst-btn="primary" type="submit">Continue</button>
  </form>
</section>`,
		},
	}
}

// buildScreens renders every screen for one theme and locale.
func buildScreens(mount, theme, locale string, tmpl *template.Template) ([]screenView, error) {
	docs := screenDocs()
	out := make([]screenView, 0, len(docs))
	first := true
	for _, doc := range docs {
		id := anchorID("screen", doc.Key)
		view := screenView{
			Name:   proseIn(locale, doc.Name),
			ID:     id,
			Marker: marker("screen", doc.Key),
			Blurb:  proseIn(locale, doc.Blurb),
		}
		if doc.WarningBody != "" {
			var buf strings.Builder
			err := tmpl.ExecuteTemplate(&buf, "callout", map[string]any{
				"Tone":  "warning",
				"Title": proseIn(locale, doc.WarningTitle),
				"Body":  proseIn(locale, doc.WarningBody),
			})
			if err != nil {
				return nil, fmt.Errorf("screen %s warning: %w", doc.Key, err)
			}
			view.Warning = template.HTML(buf.String())
		}
		title := previewTitle(locale, doc.Key, "Screens")
		if doc.Signin == nil {
			view.Preview = newPreview(mount, theme, locale, id+"-0", title, doc.Markup, id)
			out = append(out, view)
			continue
		}
		var buf strings.Builder
		err := tmpl.ExecuteTemplate(&buf, "ds-screen-stage", map[string]any{
			"State": *doc.Signin, "Brand": galleryBrand, "Preview": true, "Art": first,
		})
		if err != nil {
			return nil, fmt.Errorf("screen %s: %w", doc.Key, err)
		}
		first = false
		view.Preview = newPreview(mount, theme, locale, id+"-0", title, buf.String(), id)
		view.Preview.Source = signinSource
		out = append(out, view)
	}
	return out, nil
}

// screenNav is the Screens page's rail entries: the partials first,
// because the page names them above the screens and the rail is read in
// page order.
func screenNav(mount, theme, locale string, view pageView) []navItem {
	file := fileOf("screens")
	items := make([]navItem, 0, len(view.ScreenPartials)+len(view.Screens))
	for _, p := range view.ScreenPartials {
		items = append(items, navItem{Label: p.Name, Href: anchorHrefIn(mount, theme, locale, file, p.ID), Code: true})
	}
	for _, s := range view.Screens {
		items = append(items, navItem{Label: s.Name, Href: anchorHrefIn(mount, theme, locale, file, s.ID)})
	}
	return items
}

const screensBody = `{{define "ds-body-screens"}}
<div class="ds-head"><h2 id="screens">{{P "Screens"}}</h2></div>
<p class="ds-lead">{{P "The sign-in screens are the shipped signin partial, shown in its states. The last two are examples to copy."}}</p>
<p class="ds-lead">{{range .ScreenPartials}}{{.Marker}}<code id="{{.ID}}" data-ds-anchor>{{.Name}}</code> {{end}}</p>
<div class="ds-head"><h3 id="signing-in">{{P "Signing in"}}</h3></div>
<p class="ds-lead">{{P "Show one main way in, with the others underneath. Do not split sign-up from sign-in — one button that works for both is less to explain and less to get wrong."}}</p>
{{range .Screens}}
<article class="ds-partial" id="{{.ID}}" data-ds-anchor>
{{.Marker}}
<h4>{{.Name}}</h4>
<p class="ds-lead">{{.Blurb}}</p>
{{.Warning}}
<div class="ds-sample">
{{template "ds-view" .Preview}}
</div>
</article>
{{end}}
{{end}}`
