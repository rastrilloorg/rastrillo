//go:build browser

package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// Task 11b: day derives its accent-dependent tokens from --rst-accent.
// Two drives hold the two halves of that promise. The first reads every
// derived token as Chromium paints it, in both schemes, and holds it to
// the hex the design was measured with (the values day.css declared
// before the derivation) within two units per channel, and to the Go
// evaluator the contrast gate relies on within one, so the gate's
// numbers are the browser's. The second puts an app stylesheet on the
// page that sets --rst-accent alone, the way docs/site/design-system.md
// says to, and reads the primary button, a hovered row, a focused
// field, the current menu item and the current nav item: the first four
// follow the override, in both schemes, and the nav item stays the
// surface thumb day made it (C15), which is not an accent colour and so
// has nothing to follow.

// documentedDayColours is what day.css declared for each derived token
// before Task 11b, the colours the contrast table and the mockups were
// measured with. The derivations have to land on these, not the other
// way round: a formula that drifted would move the measured ratios.
var documentedDayColours = map[string]map[string]string{
	"light": {
		"--rst-accent-strong": "#1844ac", "--rst-accent-soft": "#ecf1ff",
		"--rst-accent-fill": "#2257df", "--rst-accent-fill-hover": "#1d4dc8", "--rst-accent-fill-edge": "#153ca0", "--rst-accent-fill-edge-hover": "#12358f",
		"--rst-tint-hover": "#f3f7ff", "--rst-tint-press": "#eaf1fe", "--rst-tint-tap": "#eaf1fe",
		"--rst-tint-card-hover": "#f3f7ff", "--rst-tint-tile-hover": "#f3f7ff", "--rst-tint-tile-press": "#eaf1fe", "--rst-tint-group-hover": "#f3f7ff",
		"--rst-row-tint-hover": "#f3f7ff", "--rst-row-tint-press": "#eaf1fe",
		"--rst-focus-edge": "#1f56d6", "--rst-focus-halo": "rgba(31, 86, 214, 0.22)",
		"--rst-sheen-plain": "rgba(34, 87, 223, 0.09)", "--rst-light-rim": "#b9c6e4",
	},
	"dark": {
		"--rst-accent-strong": "#a3c0ff", "--rst-accent-soft": "#16234a",
		"--rst-accent-fill": "#2f61e8", "--rst-accent-fill-hover": "#2956d0", "--rst-accent-fill-edge": "#1f43a6", "--rst-accent-fill-edge-hover": "#1a3b95",
		"--rst-tint-hover": "#171c28", "--rst-tint-press": "#182031", "--rst-tint-tap": "#182031",
		"--rst-tint-card-hover": "#171c28", "--rst-tint-tile-hover": "#171c28", "--rst-tint-tile-press": "#182031", "--rst-tint-group-hover": "#171c28",
		"--rst-row-tint-hover": "#171c28", "--rst-row-tint-press": "#182031",
		"--rst-focus-edge": "#7aa2ff", "--rst-focus-halo": "rgba(122, 162, 255, 0.22)",
		"--rst-sheen-plain": "rgba(255, 255, 255, 0.04)", "--rst-light-rim": "#5d6a86",
	},
}

// paintedToken reads one token as the page resolves it: a probe span
// painted with var(<token>), its computed background colour, and that
// colour painted into a 1x1 canvas and read back as eight-bit sRGB, so
// a relative colour (oklch) is checked as the engine converts it rather
// than as a serialisation this suite parses. For a translucent token
// the canvas read is useless (an un-premultiply at 9% alpha loses the
// channels), so the serialisation is returned too and the caller reads
// that instead.
func paintedToken(t *testing.T, ctx context.Context, token string) (serialised string, pixel [4]int) {
	t.Helper()
	var raw string
	mustRun(t, ctx, chromedp.Evaluate(`(() => {
	  const e = document.createElement("span"); e.style.background = "var(`+token+`)"; document.body.append(e);
	  const css = getComputedStyle(e).backgroundColor; e.remove();
	  const c = document.createElement("canvas"); c.width = c.height = 1;
	  const g = c.getContext("2d", { willReadFrequently: true });
	  g.clearRect(0, 0, 1, 1); g.fillStyle = "#000000"; g.fillStyle = css; g.fillRect(0, 0, 1, 1);
	  const d = g.getImageData(0, 0, 1, 1).data;
	  return JSON.stringify({Css: css, Px: [d[0], d[1], d[2], d[3]]});
	})()`, &raw))
	var out struct {
		Css string
		Px  [4]int
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("%s: %v (%s)", token, err, raw)
	}
	return canonColours(out.Css), out.Px
}

// nearColour reports whether two colours (hex or rgba()) agree on every
// channel within tol units and on alpha within tol/255.
func nearColour(t *testing.T, got, want string, tol float64) bool {
	t.Helper()
	g, w := channelsOf(t, got), channelsOf(t, want)
	for i := range g {
		if math.Abs(g[i]-w[i]) > tol {
			return false
		}
	}
	// alphaOf reads the last number of any rgb() as the alpha, which
	// is right for rgba() and wrong for Chromium's opaque rgb(r, g, b).
	alpha := func(v string) float64 {
		if strings.HasPrefix(v, "rgb(") {
			return 1
		}
		return alphaOf(t, v)
	}
	return math.Abs(alpha(got)-alpha(want))*255 <= tol
}

func TestDaysDerivedTokensReproduceTheirDocumentedColours(t *testing.T) {
	for _, scheme := range []string{"light", "dark"} {
		t.Run(scheme, func(t *testing.T) {
			rig := sizingRig(t, false, depthDoc(t))
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			open(t, ctx, rig, "/", false)
			mustRun(t, ctx, chromedp.Evaluate(`document.documentElement.setAttribute("data-theme", "`+scheme+`"), true`, nil))
			resolved := resolvedTokens(t, "day")[scheme]
			for token, want := range documentedDayColours[scheme] {
				serialised, px := paintedToken(t, ctx, token)
				got := serialised
				if alphaOf(t, want) >= 1 {
					got = fmt.Sprintf("#%02x%02x%02x", px[0], px[1], px[2])
					if px[3] != 255 {
						t.Errorf("%s painted with alpha %d, want opaque", token, px[3])
					}
				}
				if !nearColour(t, got, want, 2) {
					t.Errorf("%s paints as %s, want %s within 2 per channel (the hex the design was measured with)", token, got, want)
				}
				if !nearColour(t, got, resolved[token], 1) {
					t.Errorf("%s paints as %s but the Go evaluator resolves it to %s; the contrast gate is reading a colour the browser does not paint", token, got, resolved[token])
				}
				t.Logf("%-30s %-26s documented %-26s evaluator %s", token, got, want, resolved[token])
			}
		})
	}
}

// overrideAccent is the one line docs/site/design-system.md shows an
// app writing: the docs' own violet, not a blue, so a token that kept
// day's cobalt is caught.
const overrideAccent = `<style>:root { --rst-accent: light-dark(#7c3aed, #a78bfa); }</style>`

var overrideAccentHex = map[string]string{"light": "#7c3aed", "dark": "#a78bfa"}

func TestOneAccentOverrideRecoloursDay(t *testing.T) {
	doc := depthDoc(t)
	doc["/"] = strings.Replace(doc["/"], `<link rel="stylesheet" href="/theme.css">`, `<link rel="stylesheet" href="/theme.css">`+overrideAccent, 1)
	for _, scheme := range []string{"light", "dark"} {
		t.Run(scheme, func(t *testing.T) {
			rig := sizingRig(t, false, doc, harness.WithHover())
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			open(t, ctx, rig, "/", false)
			mustRun(t, ctx, chromedp.Evaluate(`document.documentElement.setAttribute("data-theme", "`+scheme+`"), true`, nil))
			still(t, ctx)
			// The expectation is the theme's own formulas evaluated with
			// the override in place (derived_test.go), not a second copy
			// of them; and it must differ from the default, or the drive
			// would pass on a page the override never reached.
			want := resolveWith(t, "day", scheme, map[string]string{"--rst-accent": overrideAccentHex[scheme]})
			def := resolvedTokens(t, "day")[scheme]
			for _, tok := range []string{"--rst-accent-fill", "--rst-accent-fill-hover", "--rst-accent-fill-edge-hover", "--rst-tint-hover", "--rst-focus-edge", "--rst-focus-halo", "--rst-accent-soft"} {
				if nearColour(t, want[tok], def[tok], 2) {
					t.Fatalf("%s resolves to %s with the override and %s without; the override does not reach it, so this drive could not tell", tok, want[tok], def[tok])
				}
			}
			pixel := func(css string) string { return paintCSS(t, ctx, css) }
			check := func(what, got, tok string) {
				t.Helper()
				if !nearColour(t, got, want[tok], 2) {
					t.Errorf("%s is %s, want %s (%s derived from the overridden accent)", what, got, want[tok], tok)
				}
			}
			check("primary button at rest", pixel(computed(t, ctx, "#b-primary", "background-color")), "--rst-accent-fill")
			forceState(t, ctx, "#b-primary", "hover")
			check("primary button hovered", pixel(computed(t, ctx, "#b-primary", "background-color")), "--rst-accent-fill-hover")
			check("primary button's hovered edge", pixel(computed(t, ctx, "#b-primary", "border-top-color")), "--rst-accent-fill-edge-hover")
			forceState(t, ctx, "#b-primary")
			forceState(t, ctx, "#row-a", "hover")
			check("hovered row", computed(t, ctx, "#row-a", "background-color"), "--rst-tint-hover")
			forceState(t, ctx, "#row-a")
			forceState(t, ctx, "#field", "focus", "focus-visible")
			check("focused field's edge", computed(t, ctx, "#field", "border-top-color"), "--rst-focus-edge")
			if sh := computed(t, ctx, "#field", "box-shadow"); !strings.Contains(sh, cssColour(t, want["--rst-focus-halo"])) {
				t.Errorf("focused field's shadow is %s, want the halo %s in it", sh, cssColour(t, want["--rst-focus-halo"]))
			}
			forceState(t, ctx, "#field")
			check("current menu item", computed(t, ctx, "#menu-cur", "background-color"), "--rst-accent-soft")
			// Not accent-dependent, by design: the current nav item is a
			// surface thumb in day (C15), so the override leaves it.
			if got := computed(t, ctx, "#nav-y", "background-color"); got != cssColour(t, def["--rst-nav-current-bg"]) {
				t.Errorf("current nav item is %s, want the surface %s: day's nav thumb is not an accent colour", got, cssColour(t, def["--rst-nav-current-bg"]))
			}
		})
	}
}

// paintCSS paints a computed colour through a 1x1 canvas and returns
// the eight-bit hex, as paintedToken does: a fill is a relative colour,
// and the engine's own conversion is the thing under test.
func paintCSS(t *testing.T, ctx context.Context, css string) string {
	t.Helper()
	var raw string
	mustRun(t, ctx, chromedp.Evaluate(`(() => { const c = document.createElement("canvas"); c.width = c.height = 1; const g = c.getContext("2d", { willReadFrequently: true }); g.fillStyle = "#000000"; g.fillStyle = `+jsStr(css)+`; g.fillRect(0, 0, 1, 1); const d = g.getImageData(0, 0, 1, 1).data; return "#" + [d[0], d[1], d[2]].map(n => n.toString(16).padStart(2, "0")).join(""); })()`, &raw))
	return raw
}

// accentSweep is the set of single-colour overrides the label check
// walks: twelve hues at 30 degree steps, at a chroma a brand would use
// (oklch 0.6 0.15), three achromatic accents, and the three sRGB
// primaries' edges (cyan, green, yellow), the accents whose fills clip
// hardest and gain the most luminance (Astra A1 computed cyan's dark
// hover at 4.43:1 under the sheen). A single colour, not a light-dark()
// pair, so the same accent serves both schemes, which is what an app
// that writes one hex gets.
func accentSweep() []string {
	out := []string{}
	for h := 0; h < 360; h += 30 {
		rgb, _ := oklchRGB(0.6, 0.15, float64(h))
		out = append(out, hexOf(rgb))
	}
	return append(out, "#808080", "#111111", "#000000", "#00ffff", "#00ff00", "#ffff00")
}

// brightestInside photographs one element in the given states and
// returns the brightest pixel of its interior as hex: the face as it is
// painted, fill, shade and sheen composed by the engine. The interior
// is the box inset by 3px, which leaves out the 1px border, the 1px
// inset highlight under it (offset sideways by the light), and the
// antialiased corner of a 4px radius, none of which sits under the
// label; the label itself is at least 7px in. The caller hides the
// label first, or its white glyphs would be the brightest pixels.
func brightestInside(t *testing.T, ctx context.Context, selector string, states ...string) string {
	t.Helper()
	img := decodePNG(t, snap(t, ctx, selector, states...))
	b := img.Bounds()
	const inset = 3
	if b.Dx() <= 2*inset || b.Dy() <= 2*inset {
		t.Fatalf("%s photographs as %v, too small to have an interior", selector, b)
	}
	best, bestLum := "", -1.0
	for y := b.Min.Y + inset; y < b.Max.Y-inset; y++ {
		for x := b.Min.X + inset; x < b.Max.X-inset; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			px := [3]uint8{uint8(r >> 8), uint8(g >> 8), uint8(bl >> 8)}
			if lum := relLuminance(px[0], px[1], px[2]); lum > bestLum {
				best, bestLum = hexOf(px), lum
			}
		}
	}
	return best
}

// TestAnyAccentKeepsTheFillLabelReadable is review I2 and I3, and
// Astra A1: a green-to-cyan accent at day's fill lightness clips to
// sRGB and gains luminance, and a grey accent used to take a fixed
// chroma at a powerless hue and come out crimson. For every accent in
// the sweep, in both schemes, the primary button's label clears 4.5:1
// on the fill at rest and hovered, measured from the colours Chromium
// paints, and on the composed face, the brightest pixel of the button
// as photographed with the sheen pinned under its top edge: the fill
// under the face shade's white at rest, and under the shade and the
// full sheen hovered. The bare fill cleared for every hue while the
// composed face did not (cyan in dark: 4.74:1 bare, 4.15:1 composed),
// which is what A1 found. An achromatic accent gives an achromatic
// fill.
func TestAnyAccentKeepsTheFillLabelReadable(t *testing.T) {
	for _, scheme := range []string{"light", "dark"} {
		t.Run(scheme, func(t *testing.T) {
			rig := sizingRig(t, false, depthDoc(t), harness.WithHover())
			ctx, cancel := context.WithTimeout(rig.Context(), 180*time.Second)
			defer cancel()
			open(t, ctx, rig, "/", false)
			mustRun(t, ctx, chromedp.Evaluate(`document.documentElement.setAttribute("data-theme", "`+scheme+`"), true`, nil))
			still(t, ctx)
			// The sheen's centre is where light.js puts the pointer; 4px
			// under the top edge is a pointer resting there, and puts the
			// sheen's full strength on the shade's lightest rows.
			mustRun(t, ctx, chromedp.Evaluate(`(() => { const s = document.createElement("style"); s.id = "override"; document.head.append(s); const b = document.getElementById("b-primary"); b.style.setProperty("--rst-mx", "50%"); b.style.setProperty("--rst-my", "4px"); return true; })()`, nil))
			hideLabel := func(hide bool) {
				v := `""`
				if hide {
					v = `"transparent"`
				}
				mustRun(t, ctx, chromedp.Evaluate(`document.getElementById("b-primary").style.color = `+v+`, true`, nil))
			}
			worst, worstComposed := 99.0, 99.0
			for _, accent := range accentSweep() {
				mustRun(t, ctx, chromedp.Evaluate(`document.getElementById("override").textContent = `+jsStr(":root { --rst-accent: "+accent+"; }")+`, true`, nil))
				label := paintCSS(t, ctx, computed(t, ctx, "#b-primary", "color"))
				fill := paintCSS(t, ctx, computed(t, ctx, "#b-primary", "background-color"))
				forceState(t, ctx, "#b-primary", "hover")
				hover := paintCSS(t, ctx, computed(t, ctx, "#b-primary", "background-color"))
				forceState(t, ctx, "#b-primary")
				hideLabel(true)
				faceRest := brightestInside(t, ctx, "#b-primary")
				faceHover := brightestInside(t, ctx, "#b-primary", "hover")
				hideLabel(false)
				for _, c := range []struct {
					state, bg, face string
				}{{"rest", fill, faceRest}, {"hover", hover, faceHover}} {
					ratio, err := ContrastRatio(label, c.bg)
					if err != nil {
						t.Fatal(err)
					}
					composed, err := ContrastRatio(label, c.face)
					if err != nil {
						t.Fatal(err)
					}
					worst, worstComposed = min(worst, ratio), min(worstComposed, composed)
					if ratio < 4.5 {
						t.Errorf("accent %s: label %s on the %s fill %s is %.2f:1, under 4.5:1", accent, label, c.state, c.bg, ratio)
					}
					if composed < 4.5 {
						t.Errorf("accent %s: label %s on the composed %s face %s (fill %s under the shade and sheen) is %.2f:1, under 4.5:1", accent, label, c.state, c.face, c.bg, composed)
					}
					if lf, lb := relLuminance(channels8(t, c.face)), relLuminance(channels8(t, c.bg)); lf < lb {
						t.Errorf("accent %s: the composed %s face %s is darker than its fill %s; the photograph missed the shade and sheen, so its ratio proves nothing", accent, c.state, c.face, c.bg)
					}
					t.Logf("accent %s %-5s %s on %s = %.2f, on the composed face %s = %.2f", accent, c.state, label, c.bg, ratio, c.face, composed)
				}
				if accent == "#808080" || accent == "#111111" || accent == "#000000" {
					r, g, b, _ := parseHex(fill)
					if int(r)-int(g) > 2 || int(g)-int(r) > 2 || int(g)-int(b) > 2 || int(b)-int(g) > 2 {
						t.Errorf("accent %s: the fill is %s, want a grey; the chroma must come from the accent, not from a fixed number at a powerless hue", accent, fill)
					}
				}
			}
			t.Logf("tightest pair across the sweep: %.2f:1 on the bare fill, %.2f:1 on the composed face", worst, worstComposed)
		})
	}
}

// channels8 is a hex colour's three channels, for relLuminance.
func channels8(t *testing.T, hex string) (r, g, b uint8) {
	t.Helper()
	r, g, b, err := parseHex(hex)
	if err != nil {
		t.Fatal(err)
	}
	return r, g, b
}

// TestWithoutRelativeColourDayKeepsItsCobalt is review I1: the derived
// fills live in an @supports block for oklch(from …), which is above
// the stated Firefox floor. Served with that block's test string made
// false, the way an engine without relative colour sees the file, the
// primary button is day's cobalt with its white label, not a fill that
// resolved to nothing. The control is the same page with the real
// string, which paints the derived fill, so the rewrite is known to
// take effect.
func TestWithoutRelativeColourDayKeepsItsCobalt(t *testing.T) {
	const real, forced = "oklch(from red l c h)", "oklch(from red l c zz)"
	for _, c := range []struct {
		name, test string
		fill       map[string]string
	}{
		{"forced false", forced, map[string]string{"light": "rgb(34, 87, 223)", "dark": "rgb(47, 97, 232)"}},
		{"control", real, map[string]string{"light": "rgb(32, 89, 221)", "dark": "rgb(49, 96, 234)"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			theme, _ := ThemeCSS("day")
			if !bytes.Contains(theme, []byte(real)) {
				t.Fatalf("day.css no longer gates its derived fills on %q; this drive has nothing to force", real)
			}
			theme = bytes.ReplaceAll(theme, []byte(real), []byte(c.test))
			pages := depthDoc(t)
			rig := harness.New(t, func(string) http.Handler {
				mux := sizingMux(t, pages)
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/theme.css" {
						w.Header().Set("Content-Type", "text/css")
						w.Write(theme)
						return
					}
					mux.ServeHTTP(w, r)
				})
			})
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			open(t, ctx, rig, "/", false)
			for scheme, want := range c.fill {
				mustRun(t, ctx, chromedp.Evaluate(`document.documentElement.setAttribute("data-theme", "`+scheme+`"), true`, nil))
				fill, label := computed(t, ctx, "#b-primary", "background-color"), computed(t, ctx, "#b-primary", "color")
				if fill != want || label != "rgb(255, 255, 255)" {
					t.Errorf("%s: primary button is %s on %s, want rgb(255, 255, 255) on %s", scheme, label, fill, want)
				}
			}
		})
	}
}
