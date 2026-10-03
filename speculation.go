package rastrillo

import (
	"io"
	"net/http"
)

// SpeculationRulesPath is where Serve answers with its speculation
// rules, and what the Speculation-Rules header on every response names.
// The rules prerender the navigation and back control of the sidebar
// and console layouts that mark their index, so a phone's next page is
// ready before it is tapped; they match nothing in an older layout or
// on any other page. Options.NoSpeculationRules turns both the header
// and the route off.
const SpeculationRulesPath = "/_speculation-rules"

// speculationRules is the whole ruleset: document rules scoped by a
// selector, in both markup spellings, to shell navigation and the back
// control inside a sidebar or console root that names its view. Only
// the layouts that ship the phone index write a view, and they ship
// with this ruleset; an app's own layout.html copied before them has a
// bare root and gets no prerender at all. Its pages were written to
// run when shown, and a prerender runs their scripts on a hover: a
// startup POST that marks an inbox read would fire for a page nobody
// opened. Unquoted ~=page, as in rastrillo.js, keeps the JSON free of
// escaped quotes. On desktop, moderate fires on a roughly 200ms hover or
// pointer-down; on Android Chrome it instead fires around 500ms after
// scrolling stops, for an anchor near the last pointer-down that is at
// least half the size of the largest anchor in view
// (https://developer.chrome.com/docs/web-platform/prerender-pages). A
// GET reachable from a shell's nav must not mutate — a handler can
// still tell a prerender apart from an ordinary navigation by its
// Sec-Purpose: prefetch;prerender request header.
//
// It is delivered by the header rather than inline because the default
// CSP refuses an inline <script type="speculationrules">, and a header
// ruleset is not a script: no CSP stops it loading. Each prerender is
// still a same-origin navigation that carries the app's own CSP.
const speculationRules = `{"prerender":[{"source":"document","where":{"selector_matches":":is([rst-shell-sidebar~=page],[rst-shell-sidebar~=index],[rst-shell-console~=page],[rst-shell-console~=index],.rst-shell-sidebar--page,.rst-shell-sidebar--index,.rst-shell-console--page,.rst-shell-console--index) :is([rst-shell-nav],[rst-shell-back],.rst-shell__nav,.rst-shell__back) a[href]"},"eagerness":"moderate"}]}`

// serveSpeculationRules answers SpeculationRulesPath. The content type
// is the one browsers accept for a ruleset: a static file would go out
// as application/json, which they refuse.
func serveSpeculationRules(w http.ResponseWriter, _ *http.Request) {
	h := w.Header()
	h.Set("Content-Type", "application/speculationrules+json")
	h.Set("Cache-Control", "public, max-age=86400")
	io.WriteString(w, speculationRules)
}
