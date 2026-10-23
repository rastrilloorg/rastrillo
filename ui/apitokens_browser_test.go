//go:build browser

package ui

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// tokensFactHeadings are the four facts a token row shows beside its
// identity, in the row's order, each the catalog's own column heading.
func tokensFactHeadings() []string {
	return []string{
		defaultTf("rastrillo.ui.api_tokens_access"),
		defaultTf("rastrillo.ui.api_tokens_col_created"),
		defaultTf("rastrillo.ui.api_tokens_col_last_used"),
		defaultTf("rastrillo.ui.api_tokens_expires"),
	}
}

// tokensPageDoc is the token page with the four-token fixture in a
// page, the way an app renders it.
func tokensPageDoc(t *testing.T) string {
	t.Helper()
	return sizingDoc("api-tokens", `<div rst-page>`+render(t, "api-tokens", tokensData(tokensPage()))+`</div>`)
}

// tokensFact is one fact cell as measured: its box, its label's box and
// words, its font size, and the value's words.
type tokensFact struct {
	Box      box
	Label    *box
	Heading  string
	Value    string
	FontSize float64
}

// tokensRow is one token row as measured: the row, its identity cell,
// its facts, and its kebab when it has one.
type tokensRow struct {
	ID    string
	Row   box
	Name  box
	Facts []tokensFact
	Kebab *box
}

const tokensListJS = `(() => {
  const r = el => { if (!el) return null; const b = el.getBoundingClientRect(); return {L: b.left, T: b.top, R: b.right, B: b.bottom}; };
  const vis = el => { if (!el) return null; const b = el.getBoundingClientRect(); return b.width > 0 && b.height > 0 ? r(el) : null; };
  const rows = [];
  document.querySelectorAll('[rst-tokens-list] [rst-lrow]:not([rst-lrow~="head"])').forEach(row => {
    const facts = [];
    row.querySelectorAll("[rst-tokens-fact]").forEach(f => {
      const label = f.querySelector("[rst-tokens-fact-label]");
      facts.push({Box: r(f), Label: vis(label), Heading: label ? label.textContent : "",
        Value: (f.textContent || "").replace(label ? label.textContent : "", "").trim(),
        FontSize: parseFloat(getComputedStyle(f).fontSize)});
    });
    rows.push({ID: row.querySelector("code").textContent, Row: r(row), Name: r(row.querySelector(".rst-nm")),
      Facts: facts, Kebab: vis(row.querySelector("[rst-row-menu] > summary"))});
  });
  const heads = [...document.querySelectorAll('[rst-tokens-list] [rst-lrow~="head"] > .rst-m-hide')].filter(h => vis(h)).length;
  const floor = parseFloat(getComputedStyle(document.documentElement).getPropertyValue("--rst-target")) * parseFloat(getComputedStyle(document.documentElement).fontSize);
  return JSON.stringify({Rows: rows, Heads: heads, Floor: floor, Width: innerWidth});
})()`

type tokensReading struct {
	Rows  []tokensRow
	Heads int // the head row's fact headings that are visible
	Floor float64
	Width int
}

func readTokens(t *testing.T, ctx context.Context) tokensReading {
	t.Helper()
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(tokensListJS, &raw)); err != nil {
		t.Fatalf("reading the token list: %v", err)
	}
	var got tokensReading
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decoding %q: %v", raw, err)
	}
	if n := len(tokensPage().Tokens); len(got.Rows) != n {
		t.Fatalf("read %d rows, want %d; the selectors no longer match the partial", len(got.Rows), n)
	}
	return got
}

// TestAPITokensFactsStayVisibleOnAPhone is the list's claim on a phone:
// a person checks a token's access, when it was made, when it was last
// used and when it expires before revoking it, so the four facts are
// not dropped at a phone's width. They stack under the identity, each
// labelled with the column heading the desktop list shows in its head
// row, at the phone's 16px type; the kebab stays at the 44px floor.
// On a desktop the facts sit in the row's own line under the head row's
// headings, and the labels, which would repeat those, are not shown.
func TestAPITokensFactsStayVisibleOnAPhone(t *testing.T) {
	headings := tokensFactHeadings()
	for _, leg := range []struct {
		name   string
		w, h   int64
		coarse bool
		floor  float64
		phone  bool
	}{
		{"1024 mouse", 1024, 768, false, 32, false},
		{"390 touch", 390, 844, true, 44, true},
		{"320 touch", 320, 568, true, 44, true},
	} {
		t.Run(leg.name, func(t *testing.T) {
			rig := mediaRig(t, leg.coarse, sizingMux(t, map[string]string{"/": tokensPageDoc(t)}))
			ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
			defer cancel()
			if err := chromedp.Run(ctx, chromedp.EmulateViewport(leg.w, leg.h), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible("[rst-tokens-list]", chromedp.ByQuery)); err != nil {
				t.Fatal(err)
			}
			requirePointer(t, ctx, leg.coarse)
			got := readTokens(t, ctx)
			if got.Floor != leg.floor {
				t.Errorf("--rst-target is %vpx, want %v", got.Floor, leg.floor)
			}
			if leg.phone && got.Heads != 0 {
				t.Errorf("the head row shows %d fact headings on a phone; the facts carry their own labels there", got.Heads)
			}
			if !leg.phone && got.Heads != len(headings) {
				t.Errorf("the head row shows %d fact headings on a desktop, want %d", got.Heads, len(headings))
			}
			for _, row := range got.Rows {
				where := leg.name + ": " + row.ID
				if len(row.Facts) != len(headings) {
					t.Errorf("%s: the row shows %d facts, want its access, created, last used and expires", where, len(row.Facts))
					continue
				}
				for i, f := range row.Facts {
					if f.Value == "" && !leg.phone {
						// A token never used has nothing in its cell on a
						// desktop, and an empty cell has no height; on a
						// phone its label still shows it.
						continue
					}
					if f.Box.w() <= 0 || f.Box.B-f.Box.T <= 0 {
						t.Errorf("%s: fact %q is not visible", where, headings[i])
						continue
					}
					if !insideBox(f.Box, row.Row) {
						t.Errorf("%s: fact %q at %+v is outside its row %+v", where, headings[i], f.Box, row.Row)
					}
					if f.Heading != headings[i] {
						t.Errorf("%s: fact %d is labelled %q, want %q", where, i, f.Heading, headings[i])
					}
					if leg.phone {
						if f.Label == nil {
							t.Errorf("%s: fact %q has no visible label on a phone", where, headings[i])
						}
						if f.Box.T < row.Name.B-1 {
							t.Errorf("%s: fact %q at %+v is not stacked under the identity %+v", where, headings[i], f.Box, row.Name)
						}
						if f.FontSize < 16 {
							t.Errorf("%s: fact %q is %.1fpx type on a phone, want 16", where, headings[i], f.FontSize)
						}
					} else {
						if f.Label != nil {
							t.Errorf("%s: fact %q repeats its heading %q under the head row on a desktop", where, headings[i], f.Heading)
						}
						if f.Box.T >= row.Name.B-1 {
							t.Errorf("%s: fact %q at %+v is below the identity %+v on a desktop, want the same line", where, headings[i], f.Box, row.Name)
						}
					}
				}
				if row.Kebab != nil {
					if w, h := row.Kebab.w(), row.Kebab.B-row.Kebab.T; math.Min(w, h) < leg.floor-0.5 {
						t.Errorf("%s: the kebab is %.0f×%.0f, under the %.0fpx floor", where, w, h, leg.floor)
					}
					if row.Kebab.T < row.Row.T-1 || row.Kebab.T >= row.Name.B {
						t.Errorf("%s: the kebab at %+v is not on the identity's line %+v", where, *row.Kebab, row.Name)
					}
				}
			}
		})
	}
}
