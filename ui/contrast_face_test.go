package ui

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The contrast gate in contrast_test.go measures token pairs. A button's
// label does not sit on a token: it sits on the fill, under a face
// gradient that lightens the top by --rst-depth-face and darkens the
// bottom by the same, under a sheen while the pointer is on it, and
// over a press that darkens by a wash plus a spot while it is held
// (spec C4, A2). This file composes those points and holds each to
// 4.5:1. The review computed 4.48:1 for a 15% sheen on the light
// primary and the cap came down (A7); if a value fails, lower the shade
// or the sheen, never lighten the fill.

func faceShadeShape(t *testing.T) {
	t.Helper()
	if !regexp.MustCompile(`--rst-btn-face: linear-gradient\(rgb\(255 255 255 / var\(--rst-depth-face, 0\)\), rgb\(0 0 0 / var\(--rst-depth-face, 0\)\)\)`).Match(TokensCSS()) {
		t.Fatal("tokens.css no longer shades a filled face with white at the top and black at the bottom by --rst-depth-face; this test measures that shape")
	}
}

func alphaOf(t *testing.T, v string) float64 {
	t.Helper()
	v = strings.TrimSpace(v)
	if v == "transparent" {
		return 0
	}
	if strings.HasPrefix(v, "#") {
		return 1
	}
	m := regexp.MustCompile(`rgba?\([^)]*?[,/]\s*([0-9.]+)\s*\)$`).FindStringSubmatch(v)
	if m == nil {
		t.Fatalf("cannot read an alpha from %q", v)
	}
	a, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func channelsOf(t *testing.T, v string) [3]float64 {
	t.Helper()
	v = strings.TrimSpace(v)
	if v == "transparent" {
		return [3]float64{}
	}
	if strings.HasPrefix(v, "#") {
		r, g, b, err := parseHex(v)
		if err != nil {
			t.Fatal(err)
		}
		return [3]float64{float64(r), float64(g), float64(b)}
	}
	m := regexp.MustCompile(`rgba?\(\s*([0-9]+)[,\s]+([0-9]+)[,\s]+([0-9]+)`).FindStringSubmatch(v)
	if m == nil {
		t.Fatalf("cannot read channels from %q", v)
	}
	var out [3]float64
	for i := range out {
		n, _ := strconv.Atoi(m[i+1])
		out[i] = float64(n)
	}
	return out
}

// over composites fg at alpha a over an opaque bg in sRGB, which is what
// a gradient layer does, and returns hex for ContrastRatio.
func over(t *testing.T, bg, fg string, a float64) string {
	t.Helper()
	B, F := channelsOf(t, bg), channelsOf(t, fg)
	var out [3]uint8
	for i := range out {
		out[i] = uint8(math.Round(F[i]*a + B[i]*(1-a)))
	}
	return hexOf(out)
}

func hexChannels(t *testing.T, v string) string {
	t.Helper()
	c := channelsOf(t, v)
	return fmt.Sprintf("#%02x%02x%02x", int(c[0]), int(c[1]), int(c[2]))
}

// pressNumber reads a number token, from the dark scheme blocks for
// dark, and fails on anything that is not a number: a value that parsed
// as 0 would make every press look safe. (Not "number": the package
// imports golang.org/x/text/number.)
func pressNumber(t *testing.T, theme, scheme, name string) float64 {
	t.Helper()
	v := themeTokens(t, theme)["light"][name]
	if scheme == "dark" {
		css, _ := ThemeCSS(theme)
		for _, block := range schemeNumberBlocks(t, theme, string(css)) {
			if m := regexp.MustCompile(regexp.QuoteMeta(name) + `\s*:\s*([^;]+);`).FindStringSubmatch(block); m != nil {
				v = strings.TrimSpace(m[1])
			}
		}
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		t.Fatalf("%s/%s %s = %q is not a number", theme, scheme, name, v)
	}
	return f
}

// TestButtonFacesClearContrastAtTheirLightestAndDarkestPoints is C4.
func TestButtonFacesClearContrastAtTheirLightestAndDarkestPoints(t *testing.T) {
	faceShadeShape(t)
	for _, theme := range ThemeNames() {
		for _, scheme := range []string{"light", "dark"} {
			tok := resolvedTokens(t, theme)[scheme]
			shade := pressNumber(t, theme, scheme, "--rst-depth-face")
			check := func(label, text, face string) {
				t.Helper()
				r, err := ContrastRatio(text, face)
				if err != nil {
					t.Fatal(err)
				}
				if r < 4.5 {
					t.Errorf("%s/%s %s: %s on the composed face %s = %.2f:1, want >= 4.5:1", theme, scheme, label, text, face, r)
				}
				t.Logf("%s/%s %s: %.2f:1", theme, scheme, label, r)
			}
			for _, v := range []struct{ name, fill, hover, text, wash, spot string }{
				{"primary", tok["--rst-accent-fill"], tok["--rst-accent-fill-hover"], tok["--rst-on-accent-fill"], "--rst-press-wash", "--rst-press-spot"},
				{"danger", tok["--rst-danger-fill"], tok["--rst-danger-fill-hover"], tok["--rst-on-danger-fill"], "--rst-press-wash", "--rst-press-spot"},
			} {
				lightest := over(t, over(t, v.fill, "#ffffff", shade), "#ffffff", alphaOf(t, tok["--rst-sheen-fill"]))
				check(v.name+" lightest (top of the shade, full sheen)", v.text, lightest)
				darkest := over(t, over(t, over(t, v.hover, "#000000", shade), "#000000", pressNumber(t, theme, scheme, v.wash)), "#000000", pressNumber(t, theme, scheme, v.spot))
				check(v.name+" darkest (bottom of the shade, whole press)", v.text, darkest)
			}
			sheen := tok["--rst-sheen-plain"]
			check("plain lightest (hovered, full sheen)", tok["--rst-control-text-hover"], over(t, tok["--rst-control-face-hover"], hexChannels(t, sheen), alphaOf(t, sheen)))
			check("plain darkest (hovered, whole press)", tok["--rst-control-text-hover"], over(t, over(t, tok["--rst-control-face-hover"], "#000000", pressNumber(t, theme, scheme, "--rst-press-wash-plain")), "#000000", pressNumber(t, theme, scheme, "--rst-press-spot-plain")))
			if tok["--rst-control-face-press"] != "transparent" {
				check("ghost pressed", tok["--rst-text"], tok["--rst-control-face-press"])
			}
		}
	}
}
