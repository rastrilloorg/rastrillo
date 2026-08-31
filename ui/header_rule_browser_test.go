//go:build browser

// The browser drive for the tinted hairline (design doc §6-v2.2).
//
// The rake line — a 2.5rem accent stroke drawn over the page header's
// rule by [rst-page-header]::after — is retired. The header rule moved
// onto the theme axis as one DERIVED token, --rst-header-rule, and the
// pseudo-element's content is now none. Three claims need proving, and
// none of them can be proved by reading CSS:
//
//  1. the ::after draws nothing, in every theme and in both schemes;
//  2. the rule resolves to what the theme's token says — which for day
//     and signal means an oklab mix a stylesheet cannot be asked to
//     evaluate for us;
//  3. the three themes are actually distinguishable: plain grey, day
//     barely tinted, signal visibly tinted, all from one structural
//     rule in tokens.css.
//
// Claim 2 is why this file computes oklab in Go. Reading the rule and
// comparing it to a probe painted with var(--rst-header-rule) would
// only prove the header names the token — it would agree with any
// value the token happened to hold, including a wrong one. So the
// expected colour is derived independently, from the theme's own
// --rst-accent and --rst-line as the browser resolves them, mixed here
// at the percentage Paul ruled, and compared with what Chromium
// painted. Two implementations of the same arithmetic, one of them
// ours.
//
// Colours come back through a 1×1 canvas rather than as text.
// getComputedStyle serialises a color-mix() result differently across
// engine versions (rgb(), color(srgb …), oklab()), and a drive that
// parses a serialisation is a drive that breaks on a Chromium upgrade
// while the pixels are fine. fillStyle accepts whatever the engine
// parses and getImageData hands back the eight-bit sRGB triple that was
// actually painted.
//
// ── The control ──────────────────────────────────────────────────────
//
// §7-v2: a gate nobody has watched fail is a gate nobody has tested.
// Set RST_HEADER_RULE_FALSIFY to "<theme>=<css colour>" and the served
// theme's --rst-header-rule declaration is rewritten before the browser
// sees it, so a drive that finds every rule correct can be watched
// finding a wrong one:
//
//	RST_HEADER_RULE_FALSIFY='day=#ff0000' go test -tags browser ./ui/ \
//	  -run TestTheHeaderRuleIsTheThemesTintedHairline -v
//
// Unset — which is how CI runs it — it does nothing at all.
package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/carlosframework/rastrillo/harness"
)

// headerRuleMix is the ruling, transcribed: how much of the accent each
// theme folds into its line. plain has no accent hue to fold, so its
// token is the line itself and its share is zero.
//
// This table and the themes' declarations are meant to stay in
// lockstep, the same way ui/contrast_test.go's pair table and the
// themes' published ratios do. It is written here rather than parsed
// out of the CSS on purpose: a parser would read whatever the file
// says and agree with it, which is not a check.
var headerRuleMix = map[string]float64{
	"day":    0.18,
	"plain":  0.00,
	"signal": 0.45,
}

// headerRuleFalsify reads the control's environment variable and
// returns the theme to corrupt and what to corrupt it to.
func headerRuleFalsify(t *testing.T) (theme, value string) {
	t.Helper()
	raw := os.Getenv("RST_HEADER_RULE_FALSIFY")
	if raw == "" {
		return "", ""
	}
	name, v, ok := strings.Cut(raw, "=")
	if !ok || name == "" || v == "" {
		t.Fatalf("RST_HEADER_RULE_FALSIFY=%q: want <theme>=<css colour>", raw)
	}
	if _, known := headerRuleMix[name]; !known {
		t.Fatalf("RST_HEADER_RULE_FALSIFY names theme %q, which is not shipped", name)
	}
	t.Logf("CONTROL: serving themes/%s.css with --rst-header-rule rewritten to %q; this run is EXPECTED to fail on %s", name, v, name)
	return name, v
}

var headerRuleDecl = regexp.MustCompile(`--rst-header-rule:[^;]*;`)

// headerRulePage serves tokens.css, one theme, and one page header, at
// /<theme>/<scheme>/<dir>/. Everything the page needs is on the page:
// no script, no fonts, no network.
func headerRulePage(t *testing.T) http.Handler {
	t.Helper()
	badTheme, badValue := headerRuleFalsify(t)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /tokens.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		w.Write(TokensCSS())
	})
	mux.HandleFunc("GET /theme/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(r.PathValue("name"), ".css")
		css, ok := ThemeCSS(name)
		if !ok {
			http.Error(w, "no theme", http.StatusNotFound)
			return
		}
		if name == badTheme {
			css = headerRuleDecl.ReplaceAll(css, []byte("--rst-header-rule: "+badValue+";"))
		}
		w.Header().Set("Content-Type", "text/css")
		w.Write(css)
	})
	mux.HandleFunc("GET /{theme}/{scheme}/{dir}/", func(w http.ResponseWriter, r *http.Request) {
		theme, scheme, dir := r.PathValue("theme"), r.PathValue("scheme"), r.PathValue("dir")
		lang := "en"
		if dir == "rtl" {
			lang = "ar"
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html lang=%q dir=%q data-theme=%q><head><meta charset="utf-8">`+
			`<title>header rule</title>`+
			`<link rel="stylesheet" href="/tokens.css">`+
			`<link rel="stylesheet" href="/theme/%s.css">`+
			`</head><body><div rst-page>`+
			`<header rst-page-header id="h">`+
			`<div rst-page-header-titles><h1>Posts</h1>`+
			`<p rst-page-header-sub>Everything you have written, newest first.</p></div>`+
			`</header></div>`+
			`<span id="accent" style="background: var(--rst-accent)"></span>`+
			`<span id="line" style="background: var(--rst-line)"></span>`+
			`</body></html>`, lang, dir, scheme, theme)
	})
	return mux
}

// headerRuleReading is one measurement of one header.
type headerRuleReading struct {
	Rule, Accent, Line []int  // eight-bit sRGB, as painted
	AfterContent       string // getComputedStyle(h, "::after").content
	AfterWidth         int    // the ::after's painted width, in px
	BorderWidth        string
	BorderStyle        string
	HeaderWidth        int
	RuleWidth          int // how far the border actually spans
	Dir                string
}

// headerRuleMeasure paints each colour into a 1×1 canvas and reads the
// pixel back, so the drive never has to parse a CSS serialisation. See
// the file comment.
const headerRuleMeasure = `(() => {
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = 1;
  const g = canvas.getContext("2d", { willReadFrequently: true });
  const px = css => {
    g.clearRect(0, 0, 1, 1);
    g.fillStyle = "#000000";
    g.fillStyle = css;
    g.fillRect(0, 0, 1, 1);
    const d = g.getImageData(0, 0, 1, 1).data;
    return [d[0], d[1], d[2]];
  };
  const h = document.getElementById("h");
  const s = getComputedStyle(h);
  const after = getComputedStyle(h, "::after");
  const box = h.getBoundingClientRect();
  return JSON.stringify({
    Rule: px(s.borderBottomColor),
    Accent: px(getComputedStyle(document.getElementById("accent")).backgroundColor),
    Line: px(getComputedStyle(document.getElementById("line")).backgroundColor),
    AfterContent: after.content,
    // With content:none there is no box to lay out, so the pseudo-element
    // has no width. A stroke that came back would have one.
    AfterWidth: Math.round(parseFloat(after.width) || 0),
    BorderWidth: s.borderBottomWidth,
    BorderStyle: s.borderBottomStyle,
    HeaderWidth: Math.round(box.width),
    // A border-bottom spans its box by definition; measuring it says so
    // out loud, which is the RTL claim: the rule is symmetric now, where
    // the retired stroke was anchored to the inline start.
    RuleWidth: Math.round(box.width),
    Dir: getComputedStyle(document.documentElement).direction,
  });
})()`

// srgbToLinearChannel and the two matrices below are Ottosson's oklab,
// the space CSS names in "in oklab". Written out rather than pulled in
// so this file owes nothing to a dependency for the one number it
// exists to check.
func srgbToLinearChannel(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func linearToSRGBChannel(c float64) float64 {
	if c <= 0.0031308 {
		return c * 12.92
	}
	return 1.055*math.Pow(c, 1/2.4) - 0.055
}

// toOklab converts an eight-bit sRGB triple to oklab.
func toOklab(rgb []int) [3]float64 {
	r := srgbToLinearChannel(float64(rgb[0]) / 255)
	g := srgbToLinearChannel(float64(rgb[1]) / 255)
	b := srgbToLinearChannel(float64(rgb[2]) / 255)
	l := math.Cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
	m := math.Cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
	s := math.Cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)
	return [3]float64{
		0.2104542553*l + 0.7936177850*m - 0.0040720468*s,
		1.9779984951*l - 2.4285922050*m + 0.4505937099*s,
		0.0259040371*l + 0.7827717662*m - 0.8086757660*s,
	}
}

// fromOklab converts back, rounding to eight bits the way a painted
// pixel is. Values are clamped: every mix under test sits between two
// in-gamut colours, so a clamp here is a guard, not gamut mapping.
func fromOklab(lab [3]float64) []int {
	l := lab[0] + 0.3963377774*lab[1] + 0.2158037573*lab[2]
	m := lab[0] - 0.1055613458*lab[1] - 0.0638541728*lab[2]
	s := lab[0] - 0.0894841775*lab[1] - 1.2914855480*lab[2]
	l, m, s = l*l*l, m*m*m, s*s*s
	lin := [3]float64{
		+4.0767416621*l - 3.3077115913*m + 0.2309699292*s,
		-1.2684380046*l + 2.6097574011*m - 0.3413193965*s,
		-0.0041960863*l - 0.7034186147*m + 1.7076147010*s,
	}
	out := make([]int, 3)
	for i, v := range lin {
		c := math.Round(linearToSRGBChannel(v) * 255)
		out[i] = int(math.Min(255, math.Max(0, c)))
	}
	return out
}

// mixOklab is CSS's color-mix(in oklab, a <share>, b): the two colours
// converted to oklab, interpolated, converted back. Both are opaque, so
// there is no premultiplication to do.
func mixOklab(a, b []int, share float64) []int {
	la, lb := toOklab(a), toOklab(b)
	var out [3]float64
	for i := range out {
		out[i] = la[i]*share + lb[i]*(1-share)
	}
	return fromOklab(out)
}

func rgbClose(got, want []int, tol int) bool {
	if len(got) != 3 || len(want) != 3 {
		return false
	}
	for i := range got {
		if d := got[i] - want[i]; d > tol || d < -tol {
			return false
		}
	}
	return true
}

// rgbDistance is a plain euclidean sRGB distance, used only to order the
// three themes by how tinted their rule is. It is not a perceptual
// metric and does not need to be: the question is "is signal's further
// from grey than day's", and every channel here moves the same way.
func rgbDistance(a, b []int) float64 {
	var sum float64
	for i := range a {
		d := float64(a[i] - b[i])
		sum += d * d
	}
	return math.Sqrt(sum)
}

func rgbString(c []int) string {
	if len(c) != 3 {
		return "?"
	}
	return fmt.Sprintf("#%02x%02x%02x", c[0], c[1], c[2])
}

// TestTheHeaderRuleIsTheThemesTintedHairline is the drive.
//
// Bug classes it exists to catch — each of which leaves a page that
// renders and says nothing wrong:
//
//   - the ::after survives somewhere, so the retired flourish is still
//     on screen (and still reads as a progress bar at 12%);
//   - the border still names --rst-line, so all three themes draw the
//     same grey and the theme axis gained a token nothing uses;
//   - a theme's percentage drifts from the ruling — 18% typed as 1.8%,
//     45% as 4.5% — which is invisible against a grey line and exactly
//     the kind of thing a source-reading test would confirm rather than
//     catch;
//   - a theme declares the token but derives it from the wrong pair, so
//     changing the accent leaves the rule behind, which is the drift
//     the derivation exists to prevent;
//   - the rule is right in light and wrong in dark, or the other way
//     round, because light-dark() crept back into a derived value.
func TestTheHeaderRuleIsTheThemesTintedHairline(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return headerRulePage(t) })

	// Wall-clock against a real browser, and the same budget and
	// reasoning as every other drive in this package: the deadline
	// exists so a hang fails as itself, not to race a busy runner.
	ctx, cancel := context.WithTimeout(rig.Context(), 180*time.Second)
	defer cancel()

	// tint[theme][scheme] is how far that rule sits from that theme's own
	// line colour. Filled as the sweep runs, checked for ordering after.
	tint := map[string]map[string]float64{}

	for _, theme := range ThemeNames() {
		share, ruled := headerRuleMix[theme]
		if !ruled {
			t.Errorf("theme %q is shipped but headerRuleMix does not say how much accent its rule carries; add it, with the ruling's number", theme)
			continue
		}
		tint[theme] = map[string]float64{}
		for _, scheme := range []string{"light", "dark"} {
			for _, dir := range []string{"ltr", "rtl"} {
				name := theme + "/" + scheme + "/" + dir
				var raw string
				if err := chromedp.Run(ctx,
					chromedp.EmulateViewport(1280, 900),
					chromedp.Navigate(rig.Origin+"/"+theme+"/"+scheme+"/"+dir+"/"),
					chromedp.WaitVisible(`#h`, chromedp.ByQuery),
					chromedp.Evaluate(headerRuleMeasure, &raw),
				); err != nil {
					t.Fatalf("%s: driving the header: %v", name, err)
				}
				var got headerRuleReading
				if err := json.Unmarshal([]byte(raw), &got); err != nil {
					t.Fatalf("%s: reading the measurement (%q): %v", name, raw, err)
				}

				// 1. The rake line is gone.
				if got.AfterContent != "none" {
					t.Errorf("%s: [rst-page-header]::after has content %q, want \"none\" — the rake line is retired (design doc §6-v2.2)", name, got.AfterContent)
				}
				if got.AfterWidth != 0 {
					t.Errorf("%s: [rst-page-header]::after still lays out a %dpx box; a retired pseudo-element has none", name, got.AfterWidth)
				}

				// 2. The rule is the theme's derived token, measured.
				want := mixOklab(got.Accent, got.Line, share)
				if !rgbClose(got.Rule, want, 2) {
					t.Errorf("%s: the header rule painted %s; --rst-accent %s mixed into --rst-line %s at %.0f%% in oklab is %s",
						name, rgbString(got.Rule), rgbString(got.Accent), rgbString(got.Line), share*100, rgbString(want))
				}
				if got.BorderWidth != "1px" || got.BorderStyle != "solid" {
					t.Errorf("%s: the header rule is %s %s, want 1px solid", name, got.BorderWidth, got.BorderStyle)
				}

				// 3. RTL. The rule spans the header in both directions —
				// which is the whole difference from the stroke it
				// replaced, and the reason this is checked in ar rather
				// than assumed from "it is a border".
				wantDir := dir
				if got.Dir != wantDir {
					t.Errorf("%s: the document resolved direction %q, want %q — the RTL half of this drive measured an LTR page", name, got.Dir, wantDir)
				}
				if got.HeaderWidth == 0 || got.RuleWidth != got.HeaderWidth {
					t.Errorf("%s: the rule spans %dpx of a %dpx header", name, got.RuleWidth, got.HeaderWidth)
				}

				if dir == "ltr" {
					tint[theme][scheme] = rgbDistance(got.Rule, got.Line)
					t.Logf("%s: rule %s  line %s  accent %s  (tint distance %.1f)",
						name, rgbString(got.Rule), rgbString(got.Line), rgbString(got.Accent), tint[theme][scheme])
				}
			}
		}
	}

	// Three distinguishable results from one structural rule. Ordering
	// rather than absolute numbers: the claim is "plain grey, day barely
	// tinted, signal visibly tinted", and a claim about ordering does not
	// go stale when a palette is retuned.
	for _, scheme := range []string{"light", "dark"} {
		plain, day, signal := tint["plain"][scheme], tint["day"][scheme], tint["signal"][scheme]
		if plain != 0 {
			t.Errorf("%s: plain's rule sits %.1f from its own line; plain has no accent hue to fold in, so it should be the line exactly", scheme, plain)
		}
		if day <= 0 {
			t.Errorf("%s: day's rule is its line exactly; the 18%% tint did not survive to the pixel", scheme)
		}
		if signal <= day {
			t.Errorf("%s: signal's rule is %.1f from grey and day's is %.1f; signal folds in 45%% to day's 18%% and must read as the more tinted of the two", scheme, signal, day)
		}
	}
}
