//go:build browser && flipevidence

package designsystem

// A one-shot drive, run by hand, that answers the stage-2 question the
// unit gates cannot: does the gallery RENDER the same after the markup
// flip as before it?
//
// It serves two generated trees — one built from the commit before the
// flip, one from this branch — and, for every element of every page in
// both, compares a digest of the computed style over the element and
// its two pseudo-elements. Only the markup should have changed, so
// every digest should match.
//
//	go run ./cmd/dsgen -out $BEFORE     (from the pre-flip checkout)
//	go run ./cmd/dsgen -out $AFTER
//	RST_BEFORE=$BEFORE RST_AFTER=$AFTER \
//	  go test -tags "browser flipevidence" ./internal/designsystem/ -run TestTheFlipDidNotMoveAPixel -v
//
// Run it against ONE tree first — RST_BEFORE and RST_AFTER the same
// directory — and require zero. That is the control this drive needs
// more than any other: a page holds up to thirty <iframe srcdoc>
// documents, and a drive that reads one before it has settled reports a
// difference between a tree and itself. RST_PAGES=day/en,signal/ar
// narrows it to a theme and locale while iterating.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/carlosframework/rastrillo/harness"
	"github.com/carlosframework/rastrillo/ui"
)

func TestTheFlipDidNotMoveAPixel(t *testing.T) {
	before, after := os.Getenv("RST_BEFORE"), os.Getenv("RST_AFTER")
	if before == "" || after == "" {
		t.Skip("set RST_BEFORE and RST_AFTER to two dsgen trees")
	}
	only := os.Getenv("RST_PAGES") // e.g. "day/en,signal/ar"

	serve := func(dir string) *httptest.Server {
		mux := http.NewServeMux()
		mux.Handle("/design-system/", http.StripPrefix("/design-system/", http.FileServer(http.Dir(dir))))
		return httptest.NewServer(mux)
	}
	a, b := serve(before), serve(after)
	defer a.Close()
	defer b.Close()

	var pages []string
	err := filepath.Walk(after, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".html" {
			return err
		}
		rel, _ := filepath.Rel(after, path)
		pages = append(pages, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(pages)
	if only != "" {
		var keep []string
		for _, p := range pages {
			for _, prefix := range strings.Split(only, ",") {
				if strings.HasPrefix(p, prefix+"/") {
					keep = append(keep, p)
				}
			}
		}
		pages = keep
	}
	if len(pages) == 0 {
		t.Fatal("no pages to compare")
	}

	ctx, cancel := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.ExecPath(harness.ChromePath(t)),
			chromedp.Flag("headless", "new"))...)
	defer cancel()
	ctx, cancel = chromedp.NewContext(ctx)
	defer cancel()
	ctx, cancel = context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	elements, differing, pagesWithDiffs, skipped := 0, 0, 0, 0
	for _, page := range pages {
		var da, db []string
		unstable := false
		for _, c := range []struct {
			origin string
			into   *[]string
		}{{a.URL, &da}, {b.URL, &db}} {
			got, ok := readTwiceUntilStable(ctx, t, c.origin+"/design-system/"+page)
			if !ok {
				t.Errorf("%s on %s: the page never gave the same reading twice, so it was not compared", page, c.origin)
				unstable = true
				break
			}
			*c.into = got
		}
		if unstable {
			skipped++
			continue
		}
		if len(da) != len(db) {
			t.Errorf("%s: %d elements before, %d after — the flip changed the DOM's shape", page, len(da), len(db))
			pagesWithDiffs++
			continue
		}
		elements += len(da)
		bad := 0
		for i := range da {
			if da[i] != db[i] {
				differing++
				if bad < 3 {
					t.Errorf("%s: element %d computes differently:\n\tbefore %s\n\tafter  %s", page, i, da[i], db[i])
				}
				bad++
			}
		}
		if bad > 0 {
			pagesWithDiffs++
		}
	}
	t.Logf("%d pages, %d elements, %d computed properties per element over the element and its ::before and ::after",
		len(pages), elements, len(props))
	t.Logf("%d elements differ, on %d pages", differing, pagesWithDiffs)
	if skipped > 0 {
		t.Errorf("%d page(s) never settled, so they were never compared", skipped)
	}
}

// readTwiceUntilStable navigates and reads the page twice, and accepts
// the reading only when the two agree.
//
// A shells page holds whole documents inside <iframe srcdoc>, each with
// its own deferred scripts, and waiting for readyState and fonts is not
// enough: the same tree gave the same page 271 elements one pass and
// 465 the next. Waiting longer is a guess about how long is long
// enough. Reading until the page stops changing is not — it is the
// question the drive actually wants answered, and an unstable page is
// reported as unstable rather than as a difference.
func readTwiceUntilStable(ctx context.Context, t *testing.T, url string) ([]string, bool) {
	t.Helper()
	var last []string
	for attempt := 0; attempt < 4; attempt++ {
		var raw string
		actions := []chromedp.Action{
			chromedp.EmulateViewport(1280, 900),
		}
		if attempt == 0 {
			actions = append(actions, chromedp.Navigate(url))
		} else {
			actions = append(actions, chromedp.Sleep(250*time.Millisecond))
		}
		actions = append(actions, chromedp.Evaluate(digest, &raw,
			func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) }))
		if err := chromedp.Run(ctx, actions...); err != nil {
			t.Fatalf("%s: %v", url, err)
		}
		var got []string
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatalf("%s: %v", url, err)
		}
		if last != nil && sameReading(last, got) {
			return got, true
		}
		last = got
	}
	return nil, false
}

func sameReading(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

var props = func() []string {
	css := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(string(ui.TokensCSS()), "")
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
}()

var digest = func() string {
	j, _ := json.Marshal(props)
	return fmt.Sprintf(`(async () => {
  const out = [];
  // Measure a settled page or measure noise. A gallery page holds thirty
  // <iframe srcdoc> documents; reading before their fonts have resolved
  // gives a layout that is a few pixels different from the one a person
  // sees, and — worse for a comparison — different from the one the same
  // page gives on the next run. A fixed sleep was doing that: a Bengali
  // page came out different between two runs of the SAME tree.
  await document.fonts.ready;
  // A fresh <iframe srcdoc> is about:blank with readyState "complete"
  // for a tick before its own document replaces it, so "complete" is
  // not the question — WHICH document is. Poll until the frame is
  // showing its srcdoc, then wait for that document's fonts. Without
  // this a frame is sometimes walked while empty, which shows up as a
  // page whose element COUNT differs between two runs of one tree.
  const settled = f => {
    let d = null;
    try { d = f.contentDocument; } catch (e) { return null; }
    if (!d || d.readyState !== "complete") return null;
    if (f.hasAttribute("srcdoc") && d.URL === "about:blank") return null;
    return d;
  };
  for (const f of document.querySelectorAll("iframe")) {
    let d = settled(f), waited = 0;
    while (d === null && waited < 5000) {
      await new Promise(function (r) { setTimeout(r, 20); });
      waited += 20;
      d = settled(f);
    }
    if (d === null) { out.push("IFRAME-NEVER-SETTLED"); continue; }
    if (d.fonts) { try { await d.fonts.ready; } catch (e) {} }
  }
  // One more frame, so anything the load handlers moved has been laid out.
  await new Promise(function (r) { requestAnimationFrame(function () { requestAnimationFrame(r); }); });
  // .rst-spin really is turning, so its transform is a different matrix
  // every millisecond. Freeze every animation at frame 0 first.
  for (const el of document.querySelectorAll("*")) {
    for (const an of el.getAnimations({subtree: false})) { an.currentTime = 0; an.pause(); }
  }
  const props = %s;
  const origins = [null, "::before", "::after"];

  // Every sample on a gallery page renders inside its own <iframe
  // srcdoc> document, so a walk of the top document alone would compare
  // the chrome and none of the components. srcdoc frames are
  // same-origin, so descend into each one.
  const walk = (doc, where) => {
    for (const el of doc.querySelectorAll("body *")) {
      let s = where + "/" + el.tagName;
      for (const o of origins) {
        const win = doc.defaultView || window;
        const cs = win.getComputedStyle(el, o);
        for (const p of props) s += "|" + cs.getPropertyValue(p);
      }
      out.push(s);
      if (el.tagName === "IFRAME") {
        let inner = null;
        try { inner = el.contentDocument; } catch (e) { inner = null; }
        if (inner) {
          for (const a of inner.getAnimations ? [] : []) {}
          for (const kid of inner.querySelectorAll("*")) {
            for (const an of kid.getAnimations({subtree: false})) { an.currentTime = 0; an.pause(); }
          }
          walk(inner, s.slice(0, where.length + 1 + el.tagName.length) + "#" + out.length);
        } else {
          out.push(where + "/IFRAME-UNREADABLE");
        }
      }
    }
  };
  walk(document, "");
  return JSON.stringify(out);
})()`, j)
}()
