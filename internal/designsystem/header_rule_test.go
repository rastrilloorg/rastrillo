package designsystem

import (
	"sort"
	"strings"
	"testing"

	"github.com/carlosframework/rastrillo/ui"
)

// TestEveryPageWithAHeaderLinksTheOneStylesheetThatRetiresTheRakeLine is
// the completeness argument the browser sweep above cannot afford to
// make by brute force: the tree is six hundred documents, and driving
// all of them to read one border would cost more than it proves.
//
// It is a cheap statement with a real edge. ui's
// TestTheRakeLineIsDeclaredRetiredExactlyOnce says the retirement is
// declared in exactly one stylesheet; this says every document in the
// tree that renders a page header links that stylesheet. Between them,
// and the sweep above showing what that declaration does in a browser,
// "the ::after is gone everywhere" is a claim about the whole tree
// rather than about the pages somebody remembered to drive.
func TestEveryPageWithAHeaderLinksTheOneStylesheetThatRetiresTheRakeLine(t *testing.T) {
	files, err := Render(mountPath)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	const link = `<link rel="stylesheet" href="` + mountPath + `/tokens.css">`
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	documents, withHeader, framesChecked := 0, 0, 0
	for _, name := range names {
		if !strings.HasSuffix(name, ".html") {
			continue
		}
		body := string(files[name])

		// A file is not a document. Every srcdoc preview inside a page is
		// a document of its own with its own <link>, and judging the file
		// as a whole would let a preview that lost its link ride on the
		// outer page's — while the previews are exactly the documents
		// this most needs to be true of. srcdocs() unescapes them; each
		// one is then held to the same rule as a file in the tree.
		for _, frame := range srcdocs(body) {
			documents++
			if !strings.Contains(frame, "rst-page-header") {
				continue
			}
			withHeader++
			framesChecked++
			if !strings.Contains(frame, link) {
				t.Errorf("%s: a preview frame renders a page header and links no %s/tokens.css of its own. It is a separate document; the page around it retires nothing on its behalf", name, mountPath)
			}
		}

		documents++
		outer := srcdocAttr.ReplaceAllString(body, "")
		if !strings.Contains(outer, "rst-page-header") {
			continue
		}
		withHeader++
		if !strings.Contains(outer, link) {
			t.Errorf("%s renders a page header and links no %s/tokens.css; whatever retires the rake line, it is not reaching this page", name, mountPath)
		}
	}
	if documents == 0 || withHeader == 0 || framesChecked == 0 {
		t.Fatalf("walked %d documents, %d of them with a page header, %d of those preview frames; the tree did not render, or the previews stopped being documents of their own", documents, withHeader, framesChecked)
	}
	t.Logf("%d of %d rendered documents carry a page header — %d of them preview frames judged on their own bytes — and every one links the stylesheet that retires the rake line", withHeader, documents, framesChecked)
}

// TestTheHeaderRuleTokenIsOnTheTokensPageWithItsColour is the gate for
// the two lines the Tokens page needed before it showed the new token
// properly, neither of which any existing test could see.
//
// --rst-header-rule is the system's first DERIVED colour: its value
// names other tokens (color-mix() in day and signal, a bare var() in
// plain) rather than being a literal. Two things followed, and both
// were silent:
//
//   - designsystem.go's colourValue only matched hex, rgb() and hsl(),
//     so the row rendered as text with a hole where its swatch belongs.
//     Nothing failed. A palette page that omits a colour is a palette
//     page that is wrong about the palette, and the omission is
//     invisible unless somebody looks at all three themes.
//   - colourGroups had no prefix claiming it, so it fell into "Other".
//     That one did trip a gate — "Other" has no prose entry, so
//     TestEveryProseKeyIsTranslated fails — but only at one remove, and
//     the failure names the untranslated heading rather than the
//     misgrouped token. This test says the intended thing directly, so
//     a reader of a failure learns what is actually wrong.
//
// The chip is painted with var(--rst-header-rule) and never with the
// value text, which is what makes a derived colour previewable at all:
// the browser resolves the derivation, so the swatch is exactly as
// correct as a hex one and stays correct when a theme retunes its
// accent.
//
// Checked in every theme and in two locales. The group's id is derived
// from the ENGLISH title before localisation, so the same anchor has to
// hold on a Japanese page whose heading reads 面と線 — which is also
// the cheapest way to notice the token quietly falling back into
// "Other" in some locales and not others.
func TestTheHeaderRuleTokenIsOnTheTokensPageWithItsColour(t *testing.T) {
	files, err := Render(mountPath)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	const (
		row   = `<span class="ds-tok__name">--rst-header-rule</span>`
		chip  = `<span class="ds-chip" style="background: var(--rst-header-rule)"></span>`
		group = `id="tokens-surfaces-and-lines"`
	)
	for _, theme := range ui.ThemeNames() {
		for _, locale := range []string{"en", "ja"} {
			name := theme + "/" + locale + "/tokens.html"
			body, ok := files[theme+"/"+locale+"/tokens.html"]
			if !ok {
				t.Fatalf("%s is not in the rendered tree", name)
			}
			page := string(body)
			at := strings.Index(page, row)
			if at < 0 {
				t.Errorf("%s does not list --rst-header-rule at all; every --rst-* declaration in a theme's :root block belongs on this page", name)
				continue
			}
			// The chip is the span immediately before the name, so the
			// slice between the enclosing <li> and the name is where it
			// has to be. Searching the whole page would be satisfied by
			// any other token's chip.
			li := strings.LastIndex(page[:at], `<li class="ds-tok">`)
			if li < 0 {
				t.Errorf("%s: the --rst-header-rule row is not inside a token <li>; the page's structure changed and this gate is reading the wrong thing", name)
				continue
			}
			if !strings.Contains(page[li:at], chip) {
				t.Errorf("%s: the --rst-header-rule row carries no colour chip. It is a derived colour — color-mix() or var() — so designsystem.go's colourValue has to recognise it, or the palette page shows a name and a hole. Row was:\n%s", name, page[li:at])
			}
			// Grouping. "Other" is the bucket an unclaimed token falls
			// into, and it is both the wrong place for a line colour and
			// a heading with no translation.
			head := strings.LastIndex(page[:at], `<h3 class="ds-sub"`)
			if head < 0 || !strings.Contains(page[head:at], group) {
				got := "no heading above it"
				if head >= 0 {
					if end := strings.Index(page[head:], ">"); end > 0 {
						got = page[head : head+end+1]
					}
				}
				t.Errorf("%s: --rst-header-rule is grouped under %s, want %s. It is a line colour and belongs beside --rst-line; the fallback bucket is \"Other\", whose heading has no prose entry", name, got, group)
			}
		}
	}
}

// TestColourValueRecognisesADerivedColour pins the regex the gate above
// depends on, at the level a later tidy-up would touch it. The two
// tables are the whole contract: what has to preview as a colour, and
// what must not.
//
// A shadow is the interesting negative. It CONTAINS a colour and is not
// one, it gets a different preview (a card wearing it rather than a
// chip painted with it), and a pattern loose enough to match one would
// silently reclassify all four depth tokens.
func TestColourValueRecognisesADerivedColour(t *testing.T) {
	for _, v := range []string{
		"#fff",
		"#ffffff",
		"rgb(0, 0, 0)",
		"rgba(0, 0, 0, 0.45)",
		"var(--rst-line)",
		"color-mix(in oklab, var(--rst-accent) 18%, var(--rst-line))",
		"color-mix(in oklab, var(--rst-accent) 45%, var(--rst-line))",
	} {
		if !colourValue.MatchString(v) {
			t.Errorf("colourValue does not match %q, so the Tokens page would draw its row with no swatch", v)
		}
	}
	for _, v := range []string{
		"0 8px 24px light-dark(rgba(0, 0, 0, 0.12), rgba(0, 0, 0, 0.5))",
		"0 1px 2px rgba(0, 0, 0, 0.3)",
		"8px",
		"system-ui, -apple-system, sans-serif",
		"999px",
	} {
		if colourValue.MatchString(v) {
			t.Errorf("colourValue matches %q, which is not a colour on its own; a chip painted with it would preview nothing and a shadow would lose the preview it has", v)
		}
	}
}
