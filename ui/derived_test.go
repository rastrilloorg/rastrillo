package ui

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// day derives its accent-dependent tokens from --rst-accent (Task 11b):
// the four fills and the soft ground take the accent's hue at day's own
// lightness, with the accent's chroma scaled and capped, as
// oklch(from var(--rst-accent) L C h) inside an @supports block for
// relative colour; the tints, the halo, the plain sheen and the rim
// fold the accent into a surface or into transparent with
// color-mix(in srgb, …); and the tokens that repeat another are a bare
// var(). The contrast gate and the composed-face test read declared
// values, so without an evaluator every pair that names a derived token
// would report "cannot parse" and the gate would be reduced to the
// neutrals. This file is that evaluator: a second implementation of the
// three shapes, in Go, held to Chromium's own pixels by
// TestDaysDerivedTokensReproduceTheirDocumentedColours.
//
// Only the shapes day writes are read. A new shape in a theme fails
// loudly here ("cannot evaluate"), which is the point: a value this
// file cannot evaluate is a value the gate is not checking.
//
// An out-of-gamut relative colour is clipped per channel, as Chromium
// paints it, not chroma-reduced as colour.go's oklchRGB does for the
// colour engine: the drives run in Chromium, and the sweep in
// accent_browser_test.go reads what it paints.

// rgba is a resolved colour: eight-bit channels as floats (so a mix is
// not rounded until it is printed) and an alpha in 0..1.
type rgba struct {
	r, g, b, a float64
}

// String spells the colour the way the rest of the tests read one: a
// hex when opaque, rgba() otherwise, so ContrastRatio, channelsOf and
// alphaOf all accept it.
func (c rgba) String() string {
	r, g, b := uint8(math.Round(c.r)), uint8(math.Round(c.g)), uint8(math.Round(c.b))
	if c.a >= 1 {
		return hexOf([3]uint8{r, g, b})
	}
	return fmt.Sprintf("rgba(%d, %d, %d, %s)", r, g, b, strconv.FormatFloat(c.a, 'f', -1, 64))
}

// accentSupportsRe matches the one block a theme may add beyond its
// :root, the toggles and the dark-number blocks: day's redeclaration of
// its hue-derived tokens where the engine has relative colour syntax.
// The test string is the exact one day.css writes; a block with another
// condition is not this block and the structure gates refuse it.
var accentSupportsRe = regexp.MustCompile(`(?s)@supports \(color: oklch\(from red l c h\)\) \{\s*:root \{(.*?)\n  \}\n\}\n?`)

// accentSupportsBlock returns the body of a theme's relative-colour
// block, or "" when it has none, and the stylesheet with that block
// removed, which is what the one-:root-block gates read.
func accentSupportsBlock(css string) (body, rest string) {
	m := accentSupportsRe.FindStringSubmatchIndex(css)
	if m == nil {
		return "", css
	}
	return css[m[2]:m[3]], css[:m[0]] + css[m[1]:]
}

// resolvedTokens is themeTokens with every derived colour evaluated for
// its scheme. A value that is not a colour expression (a font stack, a
// radius, a number, a shadow) is carried through unchanged, so a caller
// that reads numbers from it sees what themeTokens shows.
func resolvedTokens(t *testing.T, theme string) map[string]map[string]string {
	t.Helper()
	out := map[string]map[string]string{}
	for scheme, table := range themeTokens(t, theme) {
		res := make(map[string]string, len(table))
		for name, v := range table {
			res[name] = resolveValue(v, table)
		}
		out[scheme] = res
	}
	return out
}

// resolveWith evaluates one scheme's table with some tokens overridden,
// which is what an app stylesheet does to :root. The override test
// computes its expectation this way rather than from a second copy of
// the formulas.
func resolveWith(t *testing.T, theme, scheme string, override map[string]string) map[string]string {
	t.Helper()
	table := map[string]string{}
	for k, v := range themeTokens(t, theme)[scheme] {
		table[k] = v
	}
	for k, v := range override {
		table[k] = v
	}
	res := make(map[string]string, len(table))
	for name, v := range table {
		res[name] = resolveValue(v, table)
	}
	return res
}

// resolveValue evaluates a value only when it is a derivation; a
// declared literal (a hex, "transparent", a number, a shadow) is handed
// back as written, so the tests that compare one by string still see
// what the theme says rather than this file's spelling of it.
func resolveValue(v string, table map[string]string) string {
	if !strings.HasPrefix(v, "var(") && !strings.HasPrefix(v, "color-mix(") && !strings.HasPrefix(v, "oklch(from ") {
		return v
	}
	c, err := evalColour(v, table, 0)
	if err != nil {
		return v
	}
	return c.String()
}

var rgbFn = regexp.MustCompile(`^rgba?\(\s*([0-9.]+)\s*[, ]\s*([0-9.]+)\s*[, ]\s*([0-9.]+)\s*(?:[,/]\s*([0-9.]+%?)\s*)?\)$`)

// evalColour evaluates one declared value against its scheme's table.
func evalColour(v string, table map[string]string, depth int) (rgba, error) {
	v = strings.TrimSpace(v)
	if depth > 16 {
		return rgba{}, fmt.Errorf("%q: var() chain deeper than 16; a cycle", v)
	}
	switch {
	case v == "transparent":
		return rgba{}, nil
	case v == "white" || v == "#fff" || v == "#ffffff":
		return rgba{255, 255, 255, 1}, nil
	case v == "black" || v == "#000" || v == "#000000":
		return rgba{0, 0, 0, 1}, nil
	case strings.HasPrefix(v, "#"):
		r, g, b, err := parseHex(v)
		if err != nil {
			return rgba{}, err
		}
		return rgba{float64(r), float64(g), float64(b), 1}, nil
	case strings.HasPrefix(v, "rgb"):
		m := rgbFn.FindStringSubmatch(v)
		if m == nil {
			return rgba{}, fmt.Errorf("cannot evaluate %q", v)
		}
		var n [4]float64
		n[3] = 1
		for i := 0; i < 4 && i < len(m)-1; i++ {
			if m[i+1] == "" {
				continue
			}
			f, err := strconv.ParseFloat(strings.TrimSuffix(m[i+1], "%"), 64)
			if err != nil {
				return rgba{}, err
			}
			if i == 3 && strings.HasSuffix(m[i+1], "%") {
				f /= 100
			}
			n[i] = f
		}
		return rgba{n[0], n[1], n[2], n[3]}, nil
	case strings.HasPrefix(v, "var("):
		name, fallback, ok := splitVar(v)
		if !ok {
			return rgba{}, fmt.Errorf("cannot evaluate %q", v)
		}
		if ref, declared := table[name]; declared {
			return evalColour(ref, table, depth+1)
		}
		if fallback == "" {
			return rgba{}, fmt.Errorf("%s is not declared and %q has no fallback", name, v)
		}
		return evalColour(fallback, table, depth+1)
	case strings.HasPrefix(v, "light-dark("):
		// Only reachable inside a fallback the scheme split never saw;
		// the table is already one scheme's, so there is no right half
		// to pick and the value is refused rather than guessed.
		return rgba{}, fmt.Errorf("%q: light-dark() inside a derived value is not evaluated per scheme", v)
	case strings.HasPrefix(v, "color-mix(in srgb, "):
		args := splitArgs(strings.TrimSuffix(strings.TrimPrefix(v, "color-mix(in srgb, "), ")"))
		if len(args) != 2 {
			return rgba{}, fmt.Errorf("cannot evaluate %q: want two colours", v)
		}
		a, pa, err := colourAndShare(args[0], table, depth)
		if err != nil {
			return rgba{}, err
		}
		b, pb, err := colourAndShare(args[1], table, depth)
		if err != nil {
			return rgba{}, err
		}
		return mixSRGBPremultiplied(a, pa, b, pb), nil
	case strings.HasPrefix(v, "oklch(from "):
		parts := splitSpaces(strings.TrimSuffix(strings.TrimPrefix(v, "oklch("), ")"))
		if len(parts) != 5 || parts[0] != "from" {
			return rgba{}, fmt.Errorf("cannot evaluate %q: want oklch(from <colour> <L> <C> <H>)", v)
		}
		from, err := evalColour(parts[1], table, depth+1)
		if err != nil {
			return rgba{}, err
		}
		lab := oklabOf(uint8(math.Round(from.r)), uint8(math.Round(from.g)), uint8(math.Round(from.b)))
		l0, c0 := lab[0], math.Hypot(lab[1], lab[2])
		h0 := math.Atan2(lab[2], lab[1]) * 180 / math.Pi
		if h0 < 0 {
			h0 += 360
		}
		vars := map[string]float64{"l": l0, "c": c0, "h": h0}
		var ch [3]float64
		for i, expr := range parts[2:5] {
			f, err := evalChannel(expr, vars)
			if err != nil {
				return rgba{}, fmt.Errorf("%q: %v", v, err)
			}
			ch[i] = f
		}
		rgb := oklchClip(ch[0], ch[1], ch[2])
		return rgba{float64(rgb[0]), float64(rgb[1]), float64(rgb[2]), from.a}, nil
	}
	return rgba{}, fmt.Errorf("cannot evaluate %q", v)
}

// oklchClip renders OKLCh as eight-bit sRGB the way Chromium paints an
// out-of-gamut colour: each linear channel clipped to 0..1. Lightness
// and chroma are both kept, so a clipped colour is brighter than the
// one asked for; that is what the dark fill's lightness drop exists to
// absorb (day.css, the @supports comment).
func oklchClip(l, c, hue float64) [3]uint8 {
	rad := hue * math.Pi / 180
	lin := oklabToLinear(l, c*math.Cos(rad), c*math.Sin(rad))
	var v [3]uint8
	for i, ch := range lin {
		n := math.Round(linearToSRGB(math.Min(1, math.Max(0, ch))) * 255)
		v[i] = uint8(math.Min(255, math.Max(0, n)))
	}
	return v
}

// splitSpaces splits on top-level spaces, so a channel written as a
// function full of spaces stays one part.
func splitSpaces(s string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ' ':
			if depth == 0 {
				if p := strings.TrimSpace(s[start:i]); p != "" {
					out = append(out, p)
				}
				start = i + 1
			}
		}
	}
	if p := strings.TrimSpace(s[start:]); p != "" {
		out = append(out, p)
	}
	return out
}

// A channel of a relative colour: a number, the origin's l, c or h, or
// CSS math over those, which day writes as calc(), min(), max() and
// cos() with a deg unit. A quantity carries whether it is an angle, so
// cos() reads degrees as degrees and a bare number as radians, as CSS
// does.
type quantity struct {
	v   float64
	deg bool
}

func evalChannel(expr string, vars map[string]float64) (float64, error) {
	p := &channelParser{src: expr, vars: vars}
	q, err := p.sum()
	if err != nil {
		return 0, err
	}
	p.skip()
	if p.pos != len(p.src) {
		return 0, fmt.Errorf("unexpected %q at %d in %q", p.src[p.pos:], p.pos, expr)
	}
	return q.v, nil
}

type channelParser struct {
	src  string
	pos  int
	vars map[string]float64
}

func (p *channelParser) skip() {
	for p.pos < len(p.src) && p.src[p.pos] == ' ' {
		p.pos++
	}
}

func (p *channelParser) peek() byte {
	p.skip()
	if p.pos >= len(p.src) {
		return 0
	}
	return p.src[p.pos]
}

func (p *channelParser) sum() (quantity, error) {
	left, err := p.product()
	if err != nil {
		return left, err
	}
	for {
		switch p.peek() {
		case '+':
			p.pos++
			right, err := p.product()
			if err != nil {
				return left, err
			}
			left = quantity{left.v + right.v, left.deg || right.deg}
		case '-':
			p.pos++
			right, err := p.product()
			if err != nil {
				return left, err
			}
			left = quantity{left.v - right.v, left.deg || right.deg}
		default:
			return left, nil
		}
	}
}

func (p *channelParser) product() (quantity, error) {
	left, err := p.factor()
	if err != nil {
		return left, err
	}
	for {
		switch p.peek() {
		case '*':
			p.pos++
			right, err := p.factor()
			if err != nil {
				return left, err
			}
			left = quantity{left.v * right.v, left.deg || right.deg}
		case '/':
			p.pos++
			right, err := p.factor()
			if err != nil {
				return left, err
			}
			if right.v == 0 {
				return left, fmt.Errorf("division by zero in %q", p.src)
			}
			left = quantity{left.v / right.v, left.deg}
		default:
			return left, nil
		}
	}
}

func (p *channelParser) factor() (quantity, error) {
	switch c := p.peek(); {
	case c == '(':
		p.pos++
		q, err := p.sum()
		if err != nil {
			return q, err
		}
		if p.peek() != ')' {
			return q, fmt.Errorf("missing ) in %q", p.src)
		}
		p.pos++
		return q, nil
	case c == '-':
		p.pos++
		q, err := p.factor()
		return quantity{-q.v, q.deg}, err
	case c == '.' || unicode.IsDigit(rune(c)):
		start := p.pos
		for p.pos < len(p.src) && (p.src[p.pos] == '.' || unicode.IsDigit(rune(p.src[p.pos]))) {
			p.pos++
		}
		f, err := strconv.ParseFloat(p.src[start:p.pos], 64)
		if err != nil {
			return quantity{}, err
		}
		if strings.HasPrefix(p.src[p.pos:], "deg") {
			p.pos += 3
			return quantity{f, true}, nil
		}
		return quantity{f, false}, nil
	case unicode.IsLetter(rune(c)):
		start := p.pos
		for p.pos < len(p.src) && unicode.IsLetter(rune(p.src[p.pos])) {
			p.pos++
		}
		name := p.src[start:p.pos]
		if p.peek() != '(' {
			v, ok := p.vars[name]
			if !ok {
				return quantity{}, fmt.Errorf("unknown channel %q in %q", name, p.src)
			}
			return quantity{v, false}, nil
		}
		p.pos++
		var args []quantity
		for {
			q, err := p.sum()
			if err != nil {
				return q, err
			}
			args = append(args, q)
			if p.peek() == ',' {
				p.pos++
				continue
			}
			break
		}
		if p.peek() != ')' {
			return quantity{}, fmt.Errorf("missing ) after %s( in %q", name, p.src)
		}
		p.pos++
		switch name {
		case "calc":
			if len(args) == 1 {
				return args[0], nil
			}
		case "min", "max":
			out := args[0]
			for _, a := range args[1:] {
				if (name == "min") == (a.v < out.v) {
					out = a
				}
			}
			return out, nil
		case "cos", "sin":
			if len(args) == 1 {
				x := args[0].v
				if args[0].deg {
					x = x * math.Pi / 180
				}
				if name == "cos" {
					return quantity{math.Cos(x), false}, nil
				}
				return quantity{math.Sin(x), false}, nil
			}
		case "abs":
			if len(args) == 1 {
				return quantity{math.Abs(args[0].v), args[0].deg}, nil
			}
		}
		return quantity{}, fmt.Errorf("cannot evaluate %s() with %d arguments in %q", name, len(args), p.src)
	}
	return quantity{}, fmt.Errorf("unexpected %q at %d in %q", p.src[p.pos:], p.pos, p.src)
}

// splitVar reads var(--name) or var(--name, fallback); the fallback is
// everything after the first top-level comma, commas included.
func splitVar(v string) (name, fallback string, ok bool) {
	if !strings.HasPrefix(v, "var(") || !strings.HasSuffix(v, ")") {
		return "", "", false
	}
	inner := v[len("var(") : len(v)-1]
	parts := splitArgs(inner)
	if len(parts) == 0 || !strings.HasPrefix(parts[0], "--") {
		return "", "", false
	}
	if len(parts) == 1 {
		return parts[0], "", true
	}
	return parts[0], strings.TrimSpace(inner[len(parts[0])+1:]), true
}

// splitArgs splits on top-level commas, so an argument that is itself a
// call full of commas stays whole.
func splitArgs(s string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	return append(out, strings.TrimSpace(s[start:]))
}

// colourAndShare reads one color-mix() argument, "<colour> [<p>%]"; a
// share of -1 means "whatever is left", which CSS fills in.
func colourAndShare(arg string, table map[string]string, depth int) (rgba, float64, error) {
	share := -1.0
	if i := strings.LastIndex(arg, " "); i > 0 && strings.HasSuffix(arg, "%") {
		p, err := strconv.ParseFloat(strings.TrimSuffix(arg[i+1:], "%"), 64)
		if err != nil {
			return rgba{}, 0, err
		}
		share, arg = p/100, arg[:i]
	}
	c, err := evalColour(arg, table, depth+1)
	return c, share, err
}

// mixSRGBPremultiplied is color-mix(in srgb, a pa, b pb) as CSS Color 5
// defines it: the shares normalised, the colours premultiplied by their
// alpha, interpolated, and divided back out, so a mix with transparent
// keeps the opaque colour's channels at the mixed alpha rather than
// sliding them toward black.
func mixSRGBPremultiplied(a rgba, pa float64, b rgba, pb float64) rgba {
	switch {
	case pa < 0 && pb < 0:
		pa, pb = 0.5, 0.5
	case pa < 0:
		pa = 1 - pb
	case pb < 0:
		pb = 1 - pa
	}
	if sum := pa + pb; sum != 1 && sum > 0 {
		pa, pb = pa/sum, pb/sum
	}
	alpha := a.a*pa + b.a*pb
	if alpha == 0 {
		return rgba{}
	}
	mix := func(x, y float64) float64 { return (x*a.a*pa + y*b.a*pb) / alpha }
	return rgba{mix(a.r, b.r), mix(a.g, b.g), mix(a.b, b.b), alpha}
}

var (
	srgbSer  = regexp.MustCompile(`color\(srgb ([0-9.e-]+) ([0-9.e-]+) ([0-9.e-]+)(?: / ([0-9.e-]+))?\)`)
	oklchSer = regexp.MustCompile(`oklch\(([0-9.e-]+) ([0-9.e-]+) ([0-9.e-]+)(?: / ([0-9.e-]+))?\)`)
)

// canonColours rewrites the colour functions Chromium serialises a
// derived colour as, color(srgb …) for a color-mix() and oklch(…) for a
// relative colour, into the rgb()/rgba() the drives compare against.
// A computed value that is a legacy colour is already in that form.
// Without this every literal in the depth drives would have to be the
// six-decimal serialisation of a float, which changes with the engine's
// printing rather than with the colour.
func canonColours(s string) string {
	s = srgbSer.ReplaceAllStringFunc(s, func(m string) string {
		p := srgbSer.FindStringSubmatch(m)
		var ch [3]int
		for i := range ch {
			f, _ := strconv.ParseFloat(p[i+1], 64)
			ch[i] = int(math.Round(f * 255))
		}
		if p[4] == "" {
			return fmt.Sprintf("rgb(%d, %d, %d)", ch[0], ch[1], ch[2])
		}
		return fmt.Sprintf("rgba(%d, %d, %d, %s)", ch[0], ch[1], ch[2], p[4])
	})
	return oklchSer.ReplaceAllStringFunc(s, func(m string) string {
		p := oklchSer.FindStringSubmatch(m)
		l, _ := strconv.ParseFloat(p[1], 64)
		c, _ := strconv.ParseFloat(p[2], 64)
		h, _ := strconv.ParseFloat(p[3], 64)
		rgb := oklchClip(l, c, h)
		if p[4] == "" {
			return fmt.Sprintf("rgb(%d, %d, %d)", rgb[0], rgb[1], rgb[2])
		}
		return fmt.Sprintf("rgba(%d, %d, %d, %s)", rgb[0], rgb[1], rgb[2], p[4])
	})
}

// TestDerivedColourEvaluatorReadsDaysShapes pins the evaluator at the
// level a later tidy-up would touch it: each of the three shapes, the
// channel math day writes, the fallback, the premultiplied mix with
// transparent, and the refusal of a shape it does not read. The numbers
// are the ones Chromium painted for the same expressions
// (TestDaysDerivedTokensReproduceTheirDocumentedColours checks the live
// theme against the same pixels).
func TestDerivedColourEvaluatorReadsDaysShapes(t *testing.T) {
	table := map[string]string{
		"--rst-accent":  "#1f56d6",
		"--rst-surface": "#ffffff",
		"--rst-mixed":   "color-mix(in srgb, var(--rst-accent, #ff0000) 4.5%, var(--rst-surface))",
		"--rst-alias":   "var(--rst-mixed)",
	}
	const darkFill = "calc(0.543 - 0.06 * max(0, cos((h - 185) * 1deg) - 0.3)) min(calc(c * 1.5045), 0.215) h"
	for _, c := range []struct{ in, want string }{
		{"color-mix(in srgb, var(--rst-accent) 80%, #000000)", "#1945ab"},
		{"color-mix(in srgb, var(--rst-accent) 4.5%, var(--rst-surface))", "#f5f7fd"},
		{"color-mix(in srgb, var(--rst-accent) 22%, transparent)", "rgba(31, 86, 214, 0.22)"},
		{"oklch(from var(--rst-accent) 0.513 0.21 h)", "#2059dd"},
		{"oklch(from var(--rst-accent) 0.513 min(calc(c * 1.0244), 0.21) h)", "#2059dd"},
		{"oklch(from #7aa2ff " + darkFill + ")", "#3160ea"},
		{"oklch(from #14b8a6 " + darkFill + ")", "#00816d"},
		{"oklch(from #14b8a6 0.513 min(calc(c * 1.0244), 0.21) h)", "#007d6d"},
		{"oklch(from #808080 0.513 min(calc(c * 1.0244), 0.21) h)", "#676767"},
		{"var(--rst-alias)", "#f5f7fd"},
		{"var(--rst-missing, #123456)", "#123456"},
		{"var(--rst-missing, color-mix(in srgb, #000000 50%, #ffffff))", "#808080"},
	} {
		got, err := evalColour(c.in, table, 0)
		if err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		if got.String() != c.want {
			t.Errorf("%s = %s, want %s", c.in, got, c.want)
		}
	}
	for _, bad := range []string{"color-mix(in oklab, var(--rst-accent) 50%, #000)", "oklch(0.5 0.2 260)", "var(--rst-missing)", "hsl(0 0% 0%)", "oklch(from #000 0.5 pow(c, 2) h)", "oklch(from #000 0.5 c)"} {
		if _, err := evalColour(bad, table, 0); err == nil {
			t.Errorf("%s evaluated; a shape the gate cannot read must be refused, not guessed", bad)
		}
	}
	if got := canonColours("radial-gradient(90px, color(srgb 0.121569 0.337255 0.839216 / 0.09), rgba(0, 0, 0, 0) 70%) oklch(0.513 0.21 263.298)"); got != "radial-gradient(90px, rgba(31, 86, 214, 0.09), rgba(0, 0, 0, 0) 70%) rgb(32, 89, 221)" {
		t.Errorf("canonColours = %s", got)
	}
	body, rest := accentSupportsBlock("a\n@supports (color: oklch(from red l c h)) {\n  :root {\n    --rst-x: 1;\n  }\n}\nb")
	if !strings.Contains(body, "--rst-x: 1;") || rest != "a\nb" {
		t.Errorf("accentSupportsBlock: body %q rest %q", body, rest)
	}
}
