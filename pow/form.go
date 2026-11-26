package pow

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"amadan.net/rastrillo/rastrillo/basepath"
)

// Form is what a template renders: a challenge plus how the browser
// should treat it. auth, password and apps share this one shape.
type Form struct {
	Challenge
	scriptURL, workerURL string
	bound                bool
	minAgeLeft           time.Duration
}

// Issue mints a challenge and writes nothing.
func (g *Guard) Issue(now time.Time, scope string) Challenge {
	return g.form(now, now, scope, 0).Challenge
}

// Form is an ordinary form.
func (g *Guard) Form(now time.Time, scope string) Form { return g.form(now, now, scope, 0) }

// FollowOn is for a form the visitor reaches with nothing left to type
// (a prefilled details form, a confirm screen, and their re-renders):
// issued MinAge ago, so it is usable the instant it renders. It keeps
// the trap.
func (g *Guard) FollowOn(now time.Time, scope string) Form {
	return g.form(now, now.Add(-g.minAge), scope, 0)
}

// Recovery is the form to re-render after a refusal: always trapless
// and always follow-on, whatever the reason, because varying it by
// reason let the fixes undo each other (a fast visitor whose password
// manager fills the trap alternated honeypot and too_fast forever).
// It recovers the challenge, never the submission: whether the page
// around it is refilled with what the visitor typed is the app's call,
// and needs the app's idempotency.
func (g *Guard) Recovery(now time.Time, scope string) Form {
	return g.form(now, now.Add(-g.minAge), scope, flagTrapOmitted)
}

func (g *Guard) form(now, issued time.Time, scope string, flags uint8) Form {
	if g.bind {
		flags |= flagBound
	}
	c := newChallenge(g.key, issued, g.maxAge, scope, g.difficulty, flags)
	left := g.minAge - now.Sub(c.Issued)
	if left < 0 {
		left = 0
	}
	return Form{Challenge: c, scriptURL: g.scriptURL, workerURL: g.workerURL, bound: g.bind, minAgeLeft: left}
}

// At is f for the page r renders: a copy whose script and worker URLs
// went through basepath.Path. The guard's URLs are process-wide config,
// and one process answers both at /acme and, on a served direct host,
// at the root, so only the request knows which the page needs. Call it
// on every Form a page under a base path renders, or the browser asks
// the hub for the module and the worker.
func (f Form) At(r *http.Request) Form {
	f.scriptURL = basepath.Path(r, f.scriptURL)
	f.workerURL = basepath.Path(r, f.workerURL)
	return f
}

// NeedsScript is false under NoProof: render the submit enabled, and
// no module, status line or noscript, because nothing will run.
func (f Form) NeedsScript() bool { return f.Difficulty > 0 }

// Attrs are the attributes pow.js reads off the <form>. data-pow-min-age
// is the age the token still lacks at render, so a FollowOn or Recovery
// form carries 0 and is never held.
func (f Form) Attrs() template.HTMLAttr {
	if !f.NeedsScript() {
		return ""
	}
	s := fmt.Sprintf(`data-pow-form data-pow-nonce="%s" data-pow-difficulty="%d" data-pow-worker="%s" data-pow-min-age="%d"`,
		template.HTMLEscapeString(f.Nonce), f.Difficulty, template.HTMLEscapeString(f.workerURL), f.minAgeLeft.Milliseconds())
	if f.bound {
		s += " data-pow-bound"
	}
	return template.HTMLAttr(s)
}

// Script loads the module, after pow-hint.js, its sibling in Assets,
// which shows the status if the module fails to load: deferred and
// module scripts run in document order, so the hint is listening by
// then. Render it once per page.
func (f Form) Script() template.HTML {
	if !f.NeedsScript() {
		return ""
	}
	dir := f.scriptURL[:strings.LastIndex(f.scriptURL, "/")+1]
	return template.HTML(fmt.Sprintf(
		`<script defer src="%s"></script>`+
			`<script type="module" src="%s" data-pow-script></script>`,
		template.HTMLEscapeString(dir+"pow-hint.js"),
		template.HTMLEscapeString(f.scriptURL)))
}

// StatusLine is a live region, visually hidden by tokens.css's
// rst-sr-only from the first paint: a line that pow.js hid once it
// loaded moved everything under it (rastrillo discussion #437). Its
// words are there for a screen reader from the start; pow.js empties
// it once the form is wired, while the submit wears the busy look as
// the proof computes. It is drawn only when the form cannot work: by
// pow.js, saying its words again, if the worker fails, and by
// pow-hint.js if the module fails to load. The words come
// from the caller because pow has no locale; they must be true whether
// or not the module ever runs. Render it inside the form.
func (f Form) StatusLine(text string) template.HTML {
	if !f.NeedsScript() {
		return ""
	}
	return template.HTML(`<p class="rst-sr-only" role="status" ` +
		`data-pow-status>` + template.HTMLEscapeString(text) + `</p>`)
}
