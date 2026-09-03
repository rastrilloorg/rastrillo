package designsystem

import (
	"fmt"
	"html/template"
	"strings"
)

// The Screens page: whole screens rather than pieces of them.
//
// Every other page on this site documents something a caller names — a
// partial it executes, an idiom whose attributes it writes, a token it
// reads. Nothing here is any of those, and the page says so in its own
// first line: a screen is ordinary markup assembled from the parts on
// the other pages, published so somebody can copy the one nearest what
// they need.
//
// That is why this is a page kind and not a family. A family is bound
// to ui.Templates() at both ends — buildFamilies fails on a partial no
// family claims, and on a family claiming a partial ui does not define
// — which is right for components and wrong for a composition ui will
// never export. Making screens a family would have meant either
// shipping partials nobody asked for or weakening the check that keeps
// the component pages honest.
//
// Sign-in is the first group because it is the screen every app needs
// on its first day and the one the framework leaves people to invent.
// The order the doors appear in is the recommendation (spec §6-v2.10):
// one primary way in, the rest underneath.

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
	// Markup is the screen itself: complete HTML with no template
	// actions, on the same footing as ui.Styleguide's samples. It is
	// what a reader copies, so it is written the way it should be
	// copied rather than the way it is easiest to generate.
	Markup string
}

// screenDocs is the page's content, in page order.
//
// The markup is deliberately plain. A screen using a wrapper nobody
// else has would be a screen nobody can copy, so each is built only
// from what the other pages document: rst-box, rst-form, the field
// markup, rst-btn.
func screenDocs() []screenDoc {
	return []screenDoc{
		{
			Key:   "signin-link",
			Name:  "A link in your email",
			Blurb: "One field and one button. Nothing to remember and nothing to steal. Rastrillo does this out of the box.",
			Markup: `<section rst-box>
  <h1>Sign in</h1>
  <form rst-form method="post" action="/signin">
    <div rst-field>
      <label rst-field-label for="email">Email</label>
      <input rst-input id="email" name="email" type="email" autocomplete="email" required>
    </div>
    <button rst-btn="primary" type="submit">Continue</button>
  </form>
  <p><a href="/signin/other">Other ways to sign in</a></p>
</section>`,
		},
		{
			Key:   "signin-sent",
			Name:  "After the link is sent",
			Blurb: "Repeat the address back. It is the only chance to notice a typo before waiting for an email that will never come.",
			Markup: `<section rst-box>
  <h1>Check your email</h1>
  <p>We sent a link to <bdi>grace@example.com</bdi>.</p>
  <p><a href="/signin">Use a different address</a></p>
</section>`,
		},
		{
			Key:   "signin-passkey",
			Name:  "A passkey",
			Blurb: "The fastest way in for someone who has one, and nothing to type. Offer a second way as well: people lose devices, and a passkey that will not work is a locked door.",
			Markup: `<section rst-box>
  <h1>Sign in</h1>
  <button rst-btn="primary" type="button">Use a passkey</button>
  <p><a href="/signin">Email me a link instead</a></p>
</section>`,
		},
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
			WarningBody:  "People reuse them, they leak, and you inherit the job of storing them safely. Use a link in your email plus a passkey, or passkeys on their own, or sign-in with an account people already have. The screen below is here because some products still need it, not because it is a good default.",
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
    <p><a href="/signin/reset">Forgotten your password?</a></p>
  </form>
</section>`,
		},
	}
}

// buildScreens renders every screen for one theme and locale.
func buildScreens(mount, theme, locale string, tmpl *template.Template) ([]screenView, error) {
	docs := screenDocs()
	out := make([]screenView, 0, len(docs))
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
		view.Preview = newPreview(mount, theme, locale, id+"-0",
			previewTitle(locale, doc.Key, "Screens"), doc.Markup, id)
		out = append(out, view)
	}
	return out, nil
}

// screenNav is the Screens page's rail entries.
func screenNav(mount, theme, locale string, view pageView) []navItem {
	file := fileOf("screens")
	items := make([]navItem, 0, len(view.Screens))
	for _, s := range view.Screens {
		items = append(items, navItem{Label: s.Name, Href: anchorHrefIn(mount, theme, locale, file, s.ID)})
	}
	return items
}

const screensBody = `{{define "ds-body-screens"}}
<div class="ds-head"><h2 id="screens">{{P "Screens"}}</h2></div>
<p class="ds-lead">{{P "The screens here are examples to copy rather than components."}}</p>
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
