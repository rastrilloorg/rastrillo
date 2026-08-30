// Package markup is the class→attribute codemod for the ratified markup
// grammar (design spec §6-v3), and the one place that grammar is
// written down as code.
//
// Rastrillo's UI vocabulary is moving from classes to attributes:
//
//	<div class="rst-box">                       ->  <div rst-box>
//	<div class="rst-callout__body">             ->  <div rst-callout-body>
//	<a class="rst-btn rst-btn--primary">        ->  <a rst-btn="primary">
//	<span class="rst-status" data-tone="ok">    ->  <span rst-status rst-tone="ok">
//	<td class="rst-m-hide rst-cell-mut">        ->  unchanged: utilities keep class
//
// The rules, in full:
//
//   - A kind becomes a bare attribute.
//   - A BEM part (name__part) becomes a flat attribute (name-part). The
//     value slot never names a part, so the flattening is what keeps
//     `rst-btn="primary"` meaning "variant" on every attribute.
//   - A BEM modifier (name--variant) becomes a token in its kind's
//     value, matched with ~= — the same space-separated matching a class
//     list gets, so two variants compose with no new mechanism.
//   - data-tone becomes rst-tone. Every other data-* attribute carries
//     runtime state (data-busy, data-poll, data-theme) and is untouched.
//   - Seven utilities keep class, because class is what cross-cutting
//     styling is for. An element with a kind, a variant and a utility
//     ends up with an attribute and a class, and that is correct.
//
// # Why this is a program
//
// The framework had ~700 call sites; an app has its own. Hand edits at
// that scale contain hand errors, and the same translation has to run
// over repositories this one cannot see. So the transformation is a
// tool, `rastrillo markup`, and this package is what the tool, the
// stage-1 tokens.css gate and the browser drive all call — one grammar,
// not three implementations that agree until they do not.
//
// Rewriting is idempotent: the output carries no class= attribute the
// input side of the grammar can match, so a second pass changes
// nothing.
package markup

import (
	"fmt"
	"regexp"
	"strings"
)

// Utilities are the seven classes that keep the class spelling, with
// the reason. They are not kinds — they are cross-cutting styling,
// which is what class is for — so they stay class past stage 3 rather
// than being migrated and un-migrated.
var Utilities = map[string]string{
	"rst-sr-only":  "utility: visually-hidden text",
	"rst-mono":     "utility: monospaced value",
	"rst-m-hide":   "utility: hidden on a narrow screen",
	"rst-grow":     "utility: this flex child takes the slack",
	"rst-nm":       "utility: the name cell's type",
	"rst-danger":   "utility: a destructive item's colour",
	"rst-cell-mut": "utility: a muted, truncating cell",
}

// Renamed are the classes the flip renames outright rather than
// translating, because translating them would collide.
//
// rst-form__foot and rst-form-foot both existed and both were live:
// the plain action row the form-foot partial emits, and the sticky save
// bar an app writes by hand. BEM's __ flattens to a hyphen, so the two
// wanted one attribute. The partial's row takes the name the partial is
// called; the save bar, which no partial emits, becomes rst-form-bar.
//
// Order does not matter here: a class token is looked up once, so
// rst-form-foot -> rst-form-bar and rst-form__foot -> rst-form-foot do
// not chain.
var Renamed = map[string]string{
	"rst-form__foot":      "rst-form-foot",
	"rst-form-foot":       "rst-form-bar",
	"rst-form-foot__note": "rst-form-bar__note",
}

// Dropped are classes that are deleted rather than translated, because
// they style nothing: an attribute spelling of them would be a name the
// stylesheet has never heard of, sitting in markup an LLM copies.
var Dropped = map[string]string{
	"rst-dropdown__summary": "no rule: the summary is styled structurally, by [rst-dropdown] > summary",
	"rst-btn__spin":         "no rule: the busy spinner is styled structurally, by [rst-btn] > [rst-spin]",
}

// Attribute translates one rst- class name into the attribute that
// styles the same thing. ok is false for a class that keeps its
// spelling. It is the pure grammar — no renames, no deletions — so it
// is also what translates a tokens.css selector, where the renames have
// already happened in the file itself.
func Attribute(class string) (name, variant string, ok bool) {
	if _, exempt := Utilities[class]; exempt {
		return "", "", false
	}
	body := strings.TrimPrefix(class, "rst-")
	if k := strings.Index(body, "--"); k >= 0 {
		variant = body[k+2:]
		body = body[:k]
	}
	return "rst-" + strings.ReplaceAll(body, "__", "-"), variant, true
}

// MigrateClass is Attribute plus the two things only a markup rewrite
// does: the renames, and the deletions. drop is true for a class that
// leaves the markup entirely.
func MigrateClass(class string) (name, variant string, ok, drop bool) {
	if _, gone := Dropped[class]; gone {
		return "", "", false, true
	}
	if to, ok := Renamed[class]; ok {
		class = to
	}
	name, variant, ok = Attribute(class)
	return name, variant, ok, false
}

var (
	classInSelector = regexp.MustCompile(`\.(rst-[A-Za-z0-9_-]+)`)
	toneInSelector  = regexp.MustCompile(`\[data-tone="([a-z]+)"\]`)
	// A data-tone that is a real attribute rather than a selector. A
	// selector's follows a [; an attribute's follows whitespace, or the
	// opening quote of the Go literal a test writes it in.
	toneInMarkup = regexp.MustCompile("([\\s`'\"])data-tone=(\\\\?\")")
)

// Selector is the attribute twin of one tokens.css selector: every
// translatable rst- class swapped for its attribute, and data-tone for
// rst-tone. Everything else — type selectors, combinators,
// pseudo-classes, .icon, an app's own class, and the data-* attributes
// that carry runtime state — is left exactly as it is. A selector with
// nothing to translate comes back unchanged, which is how the stage-1
// gate tells "needs a twin" from "is one".
func Selector(selector string) string {
	out := classInSelector.ReplaceAllStringFunc(selector, func(m string) string {
		name, variant, ok := Attribute(m[1:])
		if !ok {
			return m
		}
		if variant == "" {
			return "[" + name + "]"
		}
		return fmt.Sprintf("[%s~=%q]", name, variant)
	})
	return toneInSelector.ReplaceAllString(out, `[rst-tone~="$1"]`)
}
