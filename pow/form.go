package pow

import (
	"fmt"
	"html/template"
	"time"
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

// Script loads the module. Render it once per page.
func (f Form) Script() template.HTML {
	if !f.NeedsScript() {
		return ""
	}
	return template.HTML(fmt.Sprintf(`<script type="module" src="%s"></script>`, template.HTMLEscapeString(f.scriptURL)))
}

// StatusLine is visible from first paint and hidden by the module when
// the form is ready, so a module that never runs (blocked, missing,
// thrown) leaves the visitor a reason instead of a dead button. Its
// words come from the caller because pow has no locale; they must be
// true whether or not the module ever runs. Render it inside the form.
func (f Form) StatusLine(text string) template.HTML {
	if !f.NeedsScript() {
		return ""
	}
	return template.HTML(`<p data-pow-status>` + template.HTMLEscapeString(text) + `</p>`)
}
