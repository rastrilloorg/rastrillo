//go:build browser

package ui

// markup-spelling: old-spelling begin — the fixture in this file is
// authored in the class spelling on purpose: it is what the attribute
// spelling is compared against. It ends at the file's end.

// The evidence for stage 1 of the markup migration (design spec §6-v3).
//
// markup_v3_test.go proves the two spellings are paired in the text of
// tokens.css and weigh the same on paper. That is a string check, and a
// string check cannot say what a browser does with the file. This one
// can: it renders the same fixture twice — every partial and every
// styleguide sample, once written in classes and once rewritten into
// attributes by the same translation the gate uses — and requires the
// computed style of every element, and of its ::before and ::after, to
// come back identical, property by property.
//
// A third pass renders the fixture with every rst- class and attribute
// stripped out. It is the check on the check: if the stylesheet were
// not reaching the fixture at all, the first two passes would agree
// perfectly and mean nothing. The bare pass has to DIFFER, on a lot of
// elements, or this test is measuring an unstyled page against another
// unstyled page and calling it a match.
//
// Both spellings are read in separate navigations rather than side by
// side in one document, deliberately: two copies of the same markup in
// one page share <details name> exclusivity groups and element ids, and
// a menu closing its twin would show up here as a styling difference
// that is nothing of the sort.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/carlosframework/rastrillo/harness"
)

// ── The fixture ──────────────────────────────────────────────────────

// extraFixture is what the partials and the styleguide samples between
// them do not reach: the button and badge variants, the three tones in
// both their spellings, the card corner rules that are wrapped in
// :where(), the compact input, the large person, the menu surfaces, and
// the utilities that must NOT move. Every rule shape in tokens.css is
// represented somewhere across the three sources — the coverage check
// below is what says so, and fails when a new rule arrives with nothing
// rendering it.
const extraFixture = `
<div class="rst-page">
  <p><a class="rst-btn" href="#a">Plain</a>
     <a class="rst-btn rst-btn--primary" href="#a">Primary</a>
     <a class="rst-btn rst-btn--ghost" href="#a">Ghost</a>
     <button class="rst-btn rst-btn--danger" type="button">Danger</button>
     <button class="rst-btn" type="button" aria-busy="true" disabled><span class="rst-spin" aria-hidden="true"></span>Saving</button></p>

  <p><span class="rst-badge rst-badge--positive">Paid</span>
     <span class="rst-badge rst-badge--warning">Due</span>
     <span class="rst-badge rst-badge--negative">Failed</span>
     <span class="rst-badge rst-badge--neutral">Draft</span></p>

  <p><span class="rst-status" data-tone="positive">Published</span>
     <span class="rst-status" data-tone="warning">Pending</span>
     <span class="rst-status" data-tone="negative">Failed</span>
     <span class="rst-status">Neutral</span></p>

  <div class="rst-callout" data-tone="positive"><span class="rst-callout__ic"><svg class="icon" viewBox="0 0 16 16"><circle cx="8" cy="8" r="7"></circle></svg></span><div class="rst-callout__body"><strong>Saved</strong><p>Only child.</p></div></div>
  <div class="rst-callout" data-tone="warning"><span class="rst-callout__ic"><svg class="icon" viewBox="0 0 16 16"><circle cx="8" cy="8" r="7"></circle></svg></span><div class="rst-callout__body"><p>One</p><p>Two</p><ul><li>Item</li></ul></div></div>
  <div class="rst-callout" data-tone="negative"><span class="rst-callout__ic"><svg class="icon" viewBox="0 0 16 16"><circle cx="8" cy="8" r="7"></circle></svg></span><div class="rst-callout__body"><p>Gone.</p></div></div>

  <!-- The card corner rules: a full-bleed first and last child, and a
       self-shaped child that must keep its own corners. -->
  <div class="rst-list">
    <div class="rst-lbar"><search><form class="rst-search" method="get" action="/x"><svg class="icon" viewBox="0 0 16 16"><circle cx="8" cy="8" r="7"></circle></svg><input type="search" name="q" value="a" aria-label="Search"><a class="rst-search__clear" href="/x" aria-label="Clear">x</a><button class="rst-sr-only" type="submit">Search</button></form></search></div>
    <div class="rst-row"><span class="rst-row__lead" data-lead="positive"></span><div class="rst-row__main"><a href="#a">Name</a><span class="rst-row__sub">Sub</span></div><span class="rst-status" data-tone="positive">Live</span><a class="rst-row__action" href="#a">Edit</a></div>
    <div class="rst-row"><span class="rst-row__lead" data-lead="warning"></span><div class="rst-row__main"><a href="#a">Second</a></div></div>
  </div>
  <div class="rst-list"><form class="rst-search" method="get" action="/x"><input type="search" name="q" aria-label="Search"></form></div>
  <div class="rst-list"><div class="rst-empty"><h2 class="rst-empty__title">Nothing yet</h2><p class="rst-empty__body">Body.</p><a class="rst-btn rst-btn--primary rst-empty__cta" href="#a">Start</a></div></div>

  <div class="rst-card" style="--rst-cols: 2fr 110px 32px">
    <div class="rst-lrow rst-lrow--head"><span>Order</span><span class="rst-m-hide">Status</span><span></span></div>
    <div class="rst-lrow"><a class="rst-nm" href="#a">#1001<small>Yesterday</small></a><span class="rst-cell-mut">Paid</span>
      <details class="rst-row-menu"><summary aria-label="Actions">…</summary><div class="rst-row-menu__panel"><a href="#a">Open</a><hr><button class="rst-danger" type="button">Delete…</button></div></details></div>
    <div class="rst-lrow"><span class="rst-person rst-person--lg"><span class="rst-person__av">A</span><span class="rst-person__meta"><span class="rst-person__name">Ana</span><span class="rst-person__email">a@example.com</span></span></span><span class="rst-cell-mut">—</span><span></span></div>
    <div class="rst-lrow"><span class="rst-person"><span class="rst-person__av rst-person__av--empty"></span><span class="rst-person__meta"><span class="rst-person__name">Unassigned</span></span></span><span></span><span></span></div>
    <div class="rst-no-match">No match. <a href="#a">Clear</a></div>
  </div>
  <p class="rst-count-line">3 of 40</p>

  <p><span class="rst-ftok"><span class="rst-ftok__k">Status</span> Paid <a href="#a" aria-label="Remove">x</a></span></p>

  <details class="rst-dropdown" name="rst-fixture-menus"><summary class="rst-btn">Filter <span class="rst-caret"><svg class="icon" viewBox="0 0 16 16"><path d="M2 5l6 6 6-6"></path></svg></span></summary>
    <div class="rst-dropdown__menu"><a href="#a" aria-current="true">All</a><a href="#a">Draft</a><hr><button type="button" class="rst-danger">Reset</button>
      <details class="rst-menu-group"><summary>More</summary><div><a href="#a">Nested</a></div></details></div></details>
  <div class="rst-locale"><form method="post" action="#a"><button type="button" aria-current="true">English</button></form><hr><form method="post" action="#a"><button type="button">Español</button></form></div>

  <nav class="rst-pagination" aria-label="Pagination"><a href="#a">Previous</a><span class="rst-pagination__disabled">First</span><span class="rst-pagination__gap">…</span><a href="#a" aria-current="page">2</a><a href="#a">Next</a></nav>

  <div class="rst-seg-tabs"><a href="#a" aria-current="page">All</a><a href="#a">Open</a><a href="#a">Closed</a></div>

  <div class="rst-meter"><span class="rst-meter__bar"><i style="width:40%"></i></span><span class="rst-meter__num">40%</span></div>

  <dl class="rst-detail"><dt>Reference</dt><dd class="rst-mono">AB-1001</dd></dl>

  <form class="rst-form" method="post" action="#a">
    <div class="rst-form-flow">
      <div class="rst-field"><label class="rst-field__label" for="v1">Name <span class="rst-field__hint">(optional)</span><span class="rst-field__required">*</span></label><input class="rst-input" type="text" id="v1" name="v1"><small class="rst-field__hint">Small hint</small><p class="rst-field__help">Help</p><p class="rst-field__error">Error</p></div>
      <div class="rst-field"><label class="rst-field__label" for="v2">Notes</label><textarea class="rst-input rst-textarea" id="v2" name="v2"></textarea></div>
      <div class="rst-field-row"><div class="rst-field rst-grow"><label class="rst-field__label" for="v3">City</label><input class="rst-input" type="text" id="v3" name="v3"></div><div class="rst-field"><label class="rst-field__label" for="v4">ZIP</label><input class="rst-input rst-input--short" type="text" id="v4" name="v4"></div></div>
      <fieldset class="rst-field-range"><legend>When</legend><div class="rst-field-row"><div class="rst-field"><label class="rst-field__label" for="v5">From</label><input class="rst-input" type="date" id="v5" name="v5"></div></div></fieldset>
      <label class="rst-switch"><input type="checkbox" checked><span class="rst-switch__track"></span> On</label>
      <fieldset class="rst-choice"><legend>Plan</legend><div class="rst-choice__cards"><label><input type="radio" name="plan" checked><span class="rst-choice__title">Free</span><span class="rst-choice__desc">Desc</span></label><label><input type="radio" name="plan"><span class="rst-choice__title">Pro</span></label></div></fieldset>
    </div>
    <div class="rst-form-bar"><span class="rst-form-bar__note">Saves immediately.</span><div class="rst-form-actions"><button class="rst-btn rst-btn--primary" type="submit">Save</button></div></div>
    <div class="rst-form-foot"><button class="rst-btn" type="button">Cancel</button></div>
  </form>

  <div class="rst-selbox"><input type="checkbox" aria-label="Select"></div>
  <div class="rst-bulkbar"><button class="rst-bulkbar__close" type="button" aria-label="Clear"><svg class="icon" viewBox="0 0 16 16"><path d="M2 2l12 12"></path></svg></button><span class="rst-bulkbar__count">3 selected</span><button class="rst-bulkbar__escalate" type="button">Escalate</button><details class="rst-dropdown" name="rst-fixture-menus2"><summary class="rst-btn">Actions</summary><div class="rst-dropdown__menu"><button type="button">Archive</button></div></details></div>

  <div class="rst-notice">A notice.</div>
  <div class="rst-form-error">A form error.</div>
  <nav class="rst-back-nav"><a href="#a">Back</a></nav>
  <p><span class="rst-tip" data-tip="A tip">?</span> <span class="rst-help"><svg class="icon" viewBox="0 0 16 16"><circle cx="8" cy="8" r="7"></circle></svg></span></p>

  <div class="rst-combo"><div class="rst-combo__rows"><input class="rst-input" type="text" aria-label="Combo"></div><ul class="rst-combo__list"><li class="rst-combo__option" aria-selected="true">One</li><li class="rst-combo__option is-active">Two</li></ul><div class="rst-select__group">Group</div></div>

  <div class="rst-dtp"><input class="rst-input rst-dtp__input" type="text" aria-label="When"><button class="rst-dtp__pick" type="button" aria-label="Pick"><svg viewBox="0 0 16 16"><circle cx="8" cy="8" r="7"></circle></svg></button>
    <div class="rst-dtp__list"><div class="rst-dtp__row"><span class="rst-dtp__label">Today</span></div><div class="rst-dtp__row rst-dtp__row--set"><span class="rst-dtp__label">Set</span></div><div class="rst-dtp__row is-active" aria-selected="true"><span class="rst-dtp__label">Tomorrow</span></div><div class="rst-dtp__quick"><button class="rst-dtp__set" type="button">Now</button></div><p class="rst-dtp__hint">Hint</p></div></div>

  <a class="rst-skip" href="#a">Skip to content</a>
  <div class="rst-backdrop"></div>
  <div class="rst-modal-overlay"><div class="rst-modal-panel"><nav><a href="#a" aria-current="page">One</a><a href="#a">Two</a></nav><section>Body</section><a class="rst-modal-close" href="#a" aria-label="Close">x</a></div></div>

  <div class="rst-error"><p class="rst-error__status">404</p><h1 class="rst-error__title">Not found</h1><p class="rst-error__body">Body.</p><p class="rst-error__cta"><a class="rst-btn rst-btn--primary" href="#a">Home</a></p><p class="rst-error__ref">ref: abc</p></div>

  <div class="rst-shell-topbar"><div class="rst-shell__bar"><a class="rst-shell__brand" href="#a">Brand</a><nav class="rst-shell__nav"><a href="#a" aria-current="page">Home</a><a href="#a">Away</a></nav><details class="rst-shell__menu" name="rst-fixture-menus3"><summary>Menu</summary></details><div class="rst-shell__tail"><nav class="rst-shell__nav"><a href="#a">Tail</a></nav><details class="rst-shell__account rst-dropdown" name="rst-fixture-menus4"><summary class="rst-btn">Ana</summary><div class="rst-dropdown__menu"><a href="#a">Sign out</a></div></details></div></div><div class="rst-shell__foot"></div></div>
  <div class="rst-shell-sidebar"><details class="rst-shell__chrome" name="rst-fixture-menus5"><summary>Menu</summary></details><div class="rst-shell__rail"><div class="rst-shell__group">Group</div><nav class="rst-shell__nav"><a href="#a" aria-current="page">Home</a></nav><div class="rst-shell__rail-foot"><details class="rst-dropdown" name="rst-fixture-menus6"><summary class="rst-btn">Ana</summary><div class="rst-dropdown__menu"><a href="#a">Sign out</a></div></details></div></div><main class="rst-shell__main">Main</main></div>
  <div class="rst-shell-console"><div class="rst-shell__bar"><a class="rst-shell__brand" href="#a">Brand</a><details class="rst-shell__menu" name="rst-fixture-menus7"><summary>Menu</summary></details><div class="rst-shell__tail"><details class="rst-shell__account rst-dropdown" name="rst-fixture-menus8"><summary class="rst-btn">Ana</summary><div class="rst-dropdown__menu"><a href="#a">Sign out</a></div></details></div></div><div class="rst-shell__rail"><nav class="rst-shell__nav"><a href="#a" aria-current="page">Home</a><a href="#a">Away</a></nav></div><main class="rst-shell__main">Main</main><div class="rst-shell__foot">Foot</div></div>
</div>`

// classFixture is the whole fixture in the class spelling: the extras
// above, every styleguide sample, and every partial with its own test
// fixture — the same set TestRenderEverythingSmoke renders.
func classFixture(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	b.WriteString(extraFixture)

	names := make([]string, 0, len(Styleguide()))
	for name := range Styleguide() {
		names = append(names, name)
	}
	sort.Strings(names)
	samples := Styleguide()
	for _, name := range names {
		tmpl, err := template.New(name).Funcs(Funcs()).Parse(samples[name])
		if err != nil {
			t.Fatalf("styleguide %s: parse: %v", name, err)
		}
		b.WriteString(`<div class="rst-page">`)
		if err := tmpl.Execute(&b, nil); err != nil {
			t.Fatalf("styleguide %s: execute: %v", name, err)
		}
		b.WriteString(`</div>`)
	}

	for _, p := range allPartials() {
		b.WriteString(`<div class="rst-page">`)
		b.WriteString(render(t, p.Name, p.Data))
		b.WriteString(`</div>`)
	}
	return b.String()
}

// bareRSTMarkup strips the vocabulary out altogether — every rst- class
// token and the tone attribute — leaving the same elements with none of
// the styling. It is the control pass.
var (
	rstClassToken = regexp.MustCompile(`\brst-[A-Za-z0-9_-]+\s*`)
	toneAttribute = regexp.MustCompile(`\s*data-tone="[a-z]+"`)
)

func bareRSTMarkup(markup string) string {
	out := classInMarkup.ReplaceAllStringFunc(markup, func(m string) string {
		value := m[len(`class="`) : len(m)-1]
		return fmt.Sprintf("class=%q", strings.TrimSpace(rstClassToken.ReplaceAllString(value, "")))
	})
	return toneAttribute.ReplaceAllString(out, "")
}

// ── The drive ────────────────────────────────────────────────────────

// styleProperties is what each element is compared on: every property
// tokens.css declares anywhere, plus a fixed list of the ones a
// difference would show up in even if no rule names them directly. The
// declared set is read from the file rather than typed out, so a rule
// that starts painting something new is compared on it without anybody
// remembering to add it here.
func styleProperties() []string {
	css := stripCSSComments(string(TokensCSS()))
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`[;{]\s*([a-z-]+)\s*:`).FindAllStringSubmatch(css, -1) {
		if name := m[1]; !strings.HasPrefix(name, "--") {
			seen[name] = true
		}
	}
	for _, name := range []string{
		"display", "position", "visibility", "opacity", "z-index", "content", "cursor",
		"color", "background-color", "background-image", "box-shadow", "transform",
		"inline-size", "block-size", "font-size", "font-weight", "font-family", "line-height",
		"letter-spacing", "text-transform", "text-decoration-line", "white-space",
		"border-block-start-width", "border-block-start-color", "border-start-start-radius",
		"border-end-end-radius", "outline-color", "outline-width", "outline-offset",
		"margin-block-start", "margin-inline-start", "padding-block-start", "padding-inline-start",
		"inset-block-start", "inset-inline-start", "flex-grow", "flex-shrink", "flex-basis",
		"gap", "grid-template-columns", "align-items", "justify-content", "overflow-x", "overflow-y",
		"animation-name", "transition-property", "pointer-events", "list-style-type", "appearance",
	} {
		seen[name] = true
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// digestJS reads one pass: for every element under #fixture, a digest
// of its computed style and of its ::before and ::after, tagged with
// the element's tag so a structural difference reads as one. Digests
// rather than the values themselves because the values are ~100
// properties over three origins over several hundred elements, and the
// detail is only wanted where two passes disagree — detailJS below
// fetches exactly that, for exactly those elements.
//
// Animations are frozen at their first frame before anything is read.
// .rst-spin really is turning while the page is open, so its transform
// is a different matrix every millisecond — two passes read microseconds
// apart would disagree about it and about nothing else, which is a flake
// wearing the costume of a real finding.
const freezeAnimations = `if (document.getAnimations) document.getAnimations().forEach(function (a) { try { a.currentTime = 0; a.pause(); } catch (e) {} });`

const digestJS = `(function (props) {
  ` + freezeAnimations + `
  var out = [], els = document.querySelectorAll("#fixture *");
  for (var i = 0; i < els.length; i++) {
    var el = els[i], parts = [el.tagName];
    var origins = [null, "::before", "::after"];
    for (var o = 0; o < origins.length; o++) {
      var cs = getComputedStyle(el, origins[o]);
      for (var p = 0; p < props.length; p++) parts.push(cs.getPropertyValue(props[p]));
    }
    var s = parts.join(""), h = 5381;
    for (var c = 0; c < s.length; c++) h = ((h * 33) ^ s.charCodeAt(c)) >>> 0;
    out.push(el.tagName + "|" + h.toString(16));
  }
  return out;
})(%s)`

// detailJS is the same reading, unhashed, for a handful of elements: it
// is what turns "element 91 differs" into "element 91's background-color
// differs", which is the difference between a failure someone can act on
// and one they have to reproduce by hand.
const detailJS = `(function (props, want) {
  ` + freezeAnimations + `
  var out = {}, els = document.querySelectorAll("#fixture *");
  for (var w = 0; w < want.length; w++) {
    var i = want[w], el = els[i];
    if (!el) continue;
    var row = {tag: el.tagName, html: el.outerHTML.slice(0, 200), style: {}};
    var origins = [["", null], ["::before", "::before"], ["::after", "::after"]];
    for (var o = 0; o < origins.length; o++) {
      var cs = getComputedStyle(el, origins[o][1]);
      for (var p = 0; p < props.length; p++) row.style[origins[o][0] + props[p]] = cs.getPropertyValue(props[p]);
    }
    out[String(i)] = row;
  }
  return out;
})(%s, %s)`

type styleDetail struct {
	Tag   string            `json:"tag"`
	HTML  string            `json:"html"`
	Style map[string]string `json:"style"`
}

// TestBothSpellingsComputeTheSameStyles is the only evidence that
// matters for stage 1: a browser, the real tokens.css, and the same
// fixture in both vocabularies.
func TestBothSpellingsComputeTheSameStyles(t *testing.T) {
	// The fixture is assembled from the partials and the styleguide,
	// which write attributes since the stage-2 flip, plus a hand-written
	// block still in classes. A comparison of two spellings needs a
	// fixture that really is in the first one, so it is translated back
	// to classes here and forward again below. Everything that survives
	// the round trip unchanged is markup neither spelling owns.
	fixture := classSpelling(t, classFixture(t))
	assertFixtureCoversTokensCSS(t, fixture)

	spellings := map[string]string{
		"class": fixture,
		"attr":  attributeSpelling(fixture),
		"bare":  bareRSTMarkup(fixture),
	}
	if spellings["attr"] == spellings["class"] {
		t.Fatal("the attribute spelling of the fixture is identical to the class spelling — the translation did nothing")
	}
	if strings.Contains(spellings["attr"], `class="rst-b`) {
		t.Errorf("the attribute spelling still carries component classes: %s", firstMatch(spellings["attr"], `class="rst-b`))
	}

	rig := harness.New(t, func(string) http.Handler { return spellingPage(t, spellings) })
	ctx, cancel := context.WithTimeout(rig.Context(), 180*time.Second)
	defer cancel()

	props := styleProperties()
	propsJSON, err := json.Marshal(props)
	if err != nil {
		t.Fatalf("marshalling the property list: %v", err)
	}
	t.Logf("comparing %d computed properties, over the element and its ::before and ::after", len(props))

	read := func(spelling string) []string {
		t.Helper()
		var got []string
		if err := chromedp.Run(ctx,
			chromedp.Navigate(rig.Origin+"/?spelling="+spelling),
			chromedp.WaitVisible("#fixture", chromedp.ByQuery),
			chromedp.Evaluate(fmt.Sprintf(digestJS, propsJSON), &got),
		); err != nil {
			t.Fatalf("reading the %s pass: %v", spelling, err)
		}
		if len(got) == 0 {
			t.Fatalf("the %s pass found no elements under #fixture", spelling)
		}
		return got
	}

	byClass, byAttr, bare := read("class"), read("attr"), read("bare")
	t.Logf("%d elements in the fixture", len(byClass))

	if len(byClass) != len(byAttr) {
		t.Fatalf("the two spellings render different DOMs: %d elements in classes, %d in attributes — "+
			"the translation lost or gained markup, so nothing below is comparable", len(byClass), len(byAttr))
	}

	// The control first: if the stylesheet is not reaching the fixture,
	// class == attr is worth nothing.
	if len(bare) != len(byClass) {
		t.Fatalf("the bare pass renders %d elements against the class pass's %d", len(bare), len(byClass))
	}
	styled := 0
	for i := range bare {
		if bare[i] != byClass[i] {
			styled++
		}
	}
	if styled < len(byClass)/4 {
		t.Fatalf("stripping every rst- class and attribute changed only %d of %d elements: "+
			"tokens.css is not reaching this fixture, so an agreement between the two spellings proves nothing",
			styled, len(byClass))
	}
	t.Logf("the control pass differs on %d of %d elements, so the stylesheet is reaching the fixture", styled, len(byClass))

	var differ []int
	for i := range byClass {
		if byClass[i] != byAttr[i] {
			differ = append(differ, i)
		}
	}
	if len(differ) == 0 {
		rig.Screen("#fixture", "the fixture in both spellings")
		return
	}

	// Something disagrees. Go back for the values, and say which
	// property moved rather than which element did.
	show := differ
	if len(show) > 6 {
		show = show[:6]
	}
	wantJSON, err := json.Marshal(show)
	if err != nil {
		t.Fatalf("marshalling the element list: %v", err)
	}
	detail := func(spelling string) map[string]styleDetail {
		var got map[string]styleDetail
		if err := chromedp.Run(ctx,
			chromedp.Navigate(rig.Origin+"/?spelling="+spelling),
			chromedp.WaitVisible("#fixture", chromedp.ByQuery),
			chromedp.Evaluate(fmt.Sprintf(detailJS, propsJSON, wantJSON), &got),
		); err != nil {
			t.Fatalf("reading the %s detail: %v", spelling, err)
		}
		return got
	}
	classDetail, attrDetail := detail("class"), detail("attr")
	for _, i := range show {
		key := fmt.Sprint(i)
		c, a := classDetail[key], attrDetail[key]
		var moved []string
		names := make([]string, 0, len(c.Style))
		for name := range c.Style {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if c.Style[name] != a.Style[name] {
				moved = append(moved, fmt.Sprintf("%s: class %q, attribute %q", name, c.Style[name], a.Style[name]))
			}
		}
		t.Errorf("element %d (<%s>) computes differently in the two spellings:\n\tclass markup:     %s\n\tattribute markup: %s\n\t%s",
			i, strings.ToLower(c.Tag), c.HTML, a.HTML, strings.Join(moved, "\n\t"))
	}
	t.Errorf("%d of %d elements compute differently in the two spellings; the first %d are above",
		len(differ), len(byClass), len(show))
}

// classSpelling translates markup the other way — rst- attributes back
// into a class list — with tokens.css's own class list as the key. That
// key is unambiguous because TestNoTwoClassesWantTheSameAttribute
// holds: no two classes want one attribute, so no attribute has two
// classes to choose between.
//
// It exists because of the flip. The partials and the styleguide hand
// this drive attribute markup now, and a fixture half in each spelling
// would compare identical halves and prove nothing about them.
func classSpelling(t *testing.T, html string) string {
	t.Helper()
	byAttr := map[string]string{}
	byVariant := map[string]string{}
	for _, m := range classInSelector.FindAllStringSubmatch(stripCSSComments(string(TokensCSS())), -1) {
		class := m[1]
		name, variant, ok := attributeFor(class)
		if !ok {
			continue
		}
		if variant == "" {
			byAttr[name] = class
			continue
		}
		byVariant[name+"~="+variant] = class
	}

	out := openTagInFixture.ReplaceAllStringFunc(html, func(tag string) string {
		// The class attribute comes out of the tag before the scan: its
		// tokens follow whitespace exactly as an attribute does, and the
		// seven utilities that live there must survive the round trip.
		kept := ""
		if m := classInMarkup.FindStringSubmatch(tag); m != nil {
			kept = m[1]
		}
		stripped := classInMarkup.ReplaceAllString(tag, "")

		var classes []string
		rewritten := rstAttrInFixture.ReplaceAllStringFunc(stripped, func(m string) string {
			g := rstAttrInFixture.FindStringSubmatch(m)
			name, value := g[1], g[2]
			if name == "rst-tone" {
				return ` data-tone="` + value + `"`
			}
			base, known := byAttr[name]
			if !known {
				t.Errorf("the fixture writes [%s], which tokens.css has no class for — the round trip cannot be checked", name)
				return m
			}
			classes = append(classes, base)
			for _, variant := range strings.Fields(value) {
				v, ok := byVariant[name+"~="+variant]
				if !ok {
					t.Errorf("the fixture writes %s=%q, which tokens.css has no class for", name, variant)
					continue
				}
				classes = append(classes, v)
			}
			return ""
		})
		if kept != "" {
			classes = append(classes, strings.Fields(kept)...)
		}
		if len(classes) == 0 {
			return tag
		}
		// After the tag name, so the class lands where an author would
		// have written it rather than at the end of the attribute list.
		i := strings.IndexAny(rewritten, " \t\n>")
		return rewritten[:i] + ` class="` + strings.Join(classes, " ") + `"` + rewritten[i:]
	})
	return out
}

var (
	openTagInFixture = regexp.MustCompile(`<[a-zA-Z][^>]*>`)
	rstAttrInFixture = regexp.MustCompile(`(?:^|\s)(rst-[a-z0-9-]+)(?:="([^"]*)")?`)
)

// assertFixtureCoversTokensCSS is the coverage side of the drive: a
// pairing the fixture never renders is a pairing this test never
// checked, so every rst- class tokens.css styles has to appear in the
// markup above. A new rule with nothing rendering it fails here, by
// name, before the browser is even started.
func assertFixtureCoversTokensCSS(t *testing.T, fixture string) {
	t.Helper()
	styled := map[string]bool{}
	for _, m := range classInSelector.FindAllStringSubmatch(stripCSSComments(string(TokensCSS())), -1) {
		styled[m[1]] = true
	}
	used := map[string]bool{}
	for _, m := range classInMarkup.FindAllStringSubmatch(fixture, -1) {
		for _, token := range strings.Fields(m[1]) {
			used[token] = true
		}
	}
	var missing []string
	for class := range styled {
		if !used[class] {
			missing = append(missing, class)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("tokens.css styles %d classes the fixture never renders, so their pairing is unproven: %s\n"+
			"\tadd them to extraFixture", len(missing), strings.Join(missing, " "))
	}
	t.Logf("the fixture renders all %d rst- classes tokens.css styles", len(styled))
}

func firstMatch(s, needle string) string {
	i := strings.Index(s, needle)
	if i < 0 {
		return ""
	}
	end := i + 120
	if end > len(s) {
		end = len(s)
	}
	return s[i:end]
}

// spellingPage serves the three passes off one origin: same stylesheet,
// same theme, same viewport, only the markup differs.
func spellingPage(t *testing.T, spellings map[string]string) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tokens.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		w.Write(TokensCSS())
	})
	mux.HandleFunc("GET /theme.css", func(w http.ResponseWriter, r *http.Request) {
		css, ok := ThemeCSS(ThemeNames()[0])
		if !ok {
			http.Error(w, "no theme", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/css")
		w.Write(css)
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		body, ok := spellings[r.URL.Query().Get("spelling")]
		if !ok {
			http.Error(w, "unknown spelling", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8">`+
			`<title>two spellings</title><link rel="stylesheet" href="/tokens.css">`+
			`<link rel="stylesheet" href="/theme.css"></head><body><div id="fixture">%s</div></body></html>`, body)
	})
	return mux
}
