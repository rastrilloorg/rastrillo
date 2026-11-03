//go:build browser

package galleryrig

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// SameLookPath is where a drive's handler serves SameLookHost. ui's
// shell pages use this too, which is why it lives here and not in the
// gallery's own tests: one comparison, so a preview and a shell page
// are held to the same idea of "the same".
const SameLookPath = "/same-look"

// SameLookHost is the page SameLook loads a document's two forms into,
// side by side in frames. It must come from the documents' own origin,
// or the comparison could not reach into the frames.
//
// Each form is loaded at a desktop and a phone width, so the rules
// under a width query are compared as well as those outside one. The
// host is wide enough to show all four frames, so none is offscreen
// and throttled while its scripts run.
const SameLookHost = `<!doctype html>
<meta charset="utf-8">
<title>Same look</title>
<style>
body { margin: 0; display: flex; align-items: start; }
iframe { border: 0; height: 640px; flex: none; }
</style>
<script>` + sameLookScript + `</script>
`

// hostWidth fits the four frames: two at each of sameLookScript's
// widths.
const hostWidth = 2 * (1024 + 375)

// sameLookScript loads, settles and compares. Settling finishes each
// finite animation and holds each endless one at its start, in both
// forms alike, so two loads a moment apart read as the page at rest; a
// stylesheet that lost a keyframes rule still shows, as a different
// list of animations.
const sameLookScript = `
window.sameLook = (() => {
  const widths = [1024, 375];
  const frames = [];
  const load = (whole, sliced) => {
    for (const width of widths) {
      for (const src of [whole, sliced]) {
        const frame = document.createElement("iframe");
        frame.style.width = width + "px";
        frame.src = src;
        document.body.append(frame);
        frames.push({frame, width});
      }
    }
  };
  const loaded = ({frame}) => {
    const doc = frame.contentDocument;
    return !!doc && doc.URL === frame.src &&
      doc.readyState === "complete" && doc.fonts.status === "loaded";
  };
  // light.js aims its light by the clock, so two frames that start it
  // either side of a minute keep different values on the root, which
  // every element inherits. Its test hook pins the light, the same in
  // every frame and held against the clock, and the lit state is still
  // what is compared; two animation frames later it is written.
  let pinning = -1;
  const pin = () => {
    pinning = frames.length;
    for (const {frame} of frames) {
      const view = frame.contentWindow;
      view.document.dispatchEvent(new view.CustomEvent("rst:light",
        {detail: {x: 0.3, y: -0.8, hold: true}}));
      view.requestAnimationFrame(() =>
        view.requestAnimationFrame(() => { pinning--; }));
    }
  };
  const ready = () => {
    if (frames.length === 0 || !frames.every(loaded)) {
      return false;
    }
    if (pinning < 0) {
      pin();
    }
    return pinning === 0;
  };
  // unpinned is why a frame that loads light.js does not hold the
  // pinned light, "" when it does or loads no light.js: a renamed hook
  // must fail here, not leave the light to the clock again.
  const unpinned = frame => {
    const doc = frame.contentDocument;
    if (!doc.querySelector('script[src*="/light."]')) {
      return "";
    }
    const style = doc.documentElement.style;
    const at = style.getPropertyValue("--rst-light-x") + ", " +
      style.getPropertyValue("--rst-light-y");
    return at === "0.300, -0.800" ? "" : frame.src + " loads light.js, " +
      "but its light is at " + at + ", not the 0.300, -0.800 the " +
      "rst:light event pinned";
  };
  const settle = doc => {
    const names = [];
    for (const animation of doc.getAnimations()) {
      names.push(animation.animationName || animation.transitionProperty ||
        "a script's animation");
      const timing = animation.effect && animation.effect.getComputedTiming();
      if (timing && timing.endTime === Infinity) {
        animation.pause();
        animation.currentTime = 0;
      } else {
        animation.finish();
      }
    }
    return names.sort().join(", ");
  };
  const path = element => {
    const steps = [];
    for (let node = element; node; node = node.parentElement) {
      let step = node.localName + [...node.attributes]
        .filter(attribute => /^(data-)?rst-/.test(attribute.name))
        .map(attribute => "[" + attribute.name + "]").join("");
      if (node.parentElement) {
        const index = [...node.parentElement.children].indexOf(node);
        step += ":nth-child(" + (index + 1) + ")";
      }
      steps.unshift(step);
    }
    return steps.join(" > ");
  };
  const difference = (width, where, pseudo, property, whole, sliced) =>
    ({Width: width, Path: where, Pseudo: pseudo, Property: property,
      Whole: whole, Sliced: sliced});
  // A ::before or ::after whose content is none is not drawn, so none
  // of its properties can be seen: when neither form draws it, its
  // content, the one property that decides that, is all there is to
  // compare, and it is the same.
  const generated = style =>
    !["none", "normal"].includes(style.getPropertyValue("content"));
  // compareStyles is the first property whose computed value differs
  // between two styles, in either one's list of properties.
  const compareStyles = (whole, sliced, counted) => {
    for (let index = 0; index < whole.length; index++) {
      const name = whole[index];
      counted.properties++;
      const value = whole.getPropertyValue(name);
      if (sliced.getPropertyValue(name) !== value) {
        return [name, value, sliced.getPropertyValue(name)];
      }
    }
    // Every property of whole's matched, so only a longer list can
    // hold one whole lacks.
    if (sliced.length === whole.length) {
      return null;
    }
    const named = new Set(Array.from(whole));
    const extra = Array.from(sliced).find(name => !named.has(name));
    if (extra) {
      return [extra, "absent", sliced.getPropertyValue(extra)];
    }
    return ["the number of properties", String(whole.length),
      String(sliced.length)];
  };
  const comparePair = (whole, sliced, width, scheme, counted) => {
    for (const {frame} of [whole, sliced]) {
      const view = frame.contentWindow;
      const dark = view.matchMedia("(prefers-color-scheme: dark)").matches;
      if (view.innerWidth !== width || dark !== (scheme === "dark")) {
        return {Problem: frame.src + " is " + view.innerWidth +
          "px wide and " + (dark ? "dark" : "light") + ", not the " +
          width + "px and " + scheme + " the comparison is for"};
      }
      if (unpinned(frame)) {
        return {Problem: unpinned(frame)};
      }
    }
    const wholeDoc = whole.frame.contentDocument;
    const slicedDoc = sliced.frame.contentDocument;
    const animations = [settle(wholeDoc), settle(slicedDoc)];
    if (animations[0] !== animations[1]) {
      return difference(width, "the document", "", "its animations",
        animations[0], animations[1]);
    }
    const wholeElements = wholeDoc.querySelectorAll("*");
    const slicedElements = slicedDoc.querySelectorAll("*");
    const count = Math.max(wholeElements.length, slicedElements.length);
    for (let index = 0; index < count; index++) {
      const wholeElement = wholeElements[index];
      const slicedElement = slicedElements[index];
      if (!wholeElement || !slicedElement ||
          path(wholeElement) !== path(slicedElement)) {
        return difference(width, path(wholeElement || slicedElement), "",
          "the element at this place in the document",
          wholeElement ? path(wholeElement) : "none",
          slicedElement ? path(slicedElement) : "none");
      }
      counted.elements++;
      for (const pseudo of ["", "::before", "::after"]) {
        const wholeStyle =
          whole.frame.contentWindow.getComputedStyle(wholeElement, pseudo);
        const slicedStyle =
          sliced.frame.contentWindow.getComputedStyle(slicedElement, pseudo);
        if (pseudo && !generated(wholeStyle) && !generated(slicedStyle)) {
          continue;
        }
        const found = compareStyles(wholeStyle, slicedStyle, counted);
        if (found) {
          return difference(width, path(wholeElement), pseudo, ...found);
        }
      }
    }
    return null;
  };
  const compare = scheme => {
    const counted = {elements: 0, properties: 0};
    let found = null;
    for (let index = 0; index < frames.length && !found; index += 2) {
      found = comparePair(frames[index], frames[index + 1],
        frames[index].width, scheme, counted);
    }
    const problem = found && found.Problem;
    return JSON.stringify({Elements: counted.elements,
      Properties: counted.properties, Problem: problem || "",
      Difference: problem ? null : found});
  };
  return {load, ready, compare};
})();
`

// A LookPair is one document in its two forms: Whole links the whole
// stylesheet, Sliced the slice of it the document's hooks keep.
type LookPair struct {
	Name, Whole, Sliced string
}

// lookTabs is how many pairs are compared at once. Reading every
// property of every element is the cost of a comparison, and it is
// spent inside each tab's page, so tabs share it out.
const lookTabs = 4

// SameLooks compares each of pairs in a subtest of its own name, as
// SameLook does, lookTabs at a time in rig's browser. The rig serves
// the documents and SameLookHost at SameLookPath.
func SameLooks(t *testing.T, rig *harness.Rig, pairs []LookPair) {
	t.Helper()
	tabs := make(chan context.Context, lookTabs)
	tabs <- rig.Context()
	for range lookTabs - 1 {
		tab, cancel := chromedp.NewContext(rig.Context())
		t.Cleanup(cancel)
		if err := chromedp.Do(tab); err != nil {
			t.Fatalf("opening a tab: %v", err)
		}
		tabs <- tab
	}
	for _, pair := range pairs {
		t.Run(pair.Name, func(t *testing.T) {
			t.Parallel()
			tab := <-tabs
			defer func() { tabs <- tab }()
			ctx, cancel := context.WithTimeout(tab, time.Minute)
			defer cancel()
			SameLook(t, ctx, rig.Origin, pair)
		})
	}
}

// SameLook loads pair's two forms side by side, in the light scheme and
// then the dark, and fails the test naming the first element and
// property whose computed style differs between them. A difference is
// read twice, half a second apart, before it fails: a script still
// settling is not the page at rest, and a difference that stays is.
func SameLook(
	t *testing.T, ctx context.Context, origin string, pair LookPair,
) {
	t.Helper()
	loadForms(t, ctx, origin, pair)
	for _, scheme := range []string{"light", "dark"} {
		if err := chromedp.Do(ctx, emulateScheme(scheme)); err != nil {
			t.Fatalf("%s: switching to the %s scheme: %v",
				pair.Name, scheme, err)
		}
		reading := compareLooks(t, ctx, pair.Name, scheme)
		if reading.Problem != "" {
			t.Fatalf("%s, %s scheme: %s", pair.Name, scheme, reading.Problem)
		}
		if reading.Difference != nil {
			time.Sleep(500 * time.Millisecond)
			reading = compareLooks(t, ctx, pair.Name, scheme)
		}
		if reading.Elements == 0 || reading.Properties == 0 {
			t.Fatalf("%s, %s scheme: compared %d elements and %d "+
				"properties: nothing to hold the slice to",
				pair.Name, scheme, reading.Elements, reading.Properties)
		}
		if found := reading.Difference; found != nil {
			t.Errorf("%s, %s scheme, %dpx wide: %s%s: %s is %q on the "+
				"whole stylesheet and %q on its slice", pair.Name, scheme,
				found.Width, found.Path, found.Pseudo, found.Property,
				found.Whole, found.Sliced)
			return
		}
	}
}

// loadForms opens the host in a tab wide enough for its frames, loads
// pair's two forms into it and waits until every frame has loaded.
func loadForms(
	t *testing.T, ctx context.Context, origin string, pair LookPair,
) {
	t.Helper()
	err := chromedp.Do(ctx,
		chromedp.Func(func(ctx context.Context, tab *chromedp.Target) error {
			_, err := cdp.Call(ctx, tab, emulation.SetDeviceMetricsOverride,
				emulation.SetDeviceMetricsOverrideParams{
					Width: hostWidth, Height: 720, DeviceScaleFactor: 1,
				})
			return err
		}),
		emulateScheme("light"),
		chromedp.Navigate(origin+SameLookPath),
		chromedp.Evaluate[chromedp.Void](fmt.Sprintf(
			"sameLook.load(%s, %s)", jsString(pair.Whole),
			jsString(pair.Sliced))),
	)
	if err != nil {
		t.Fatalf("%s: loading the two forms: %v", pair.Name, err)
	}
	Until(t, ctx, pair.Name, "sameLook.ready()")
}

// lookReading is one comparison of a pair's forms: how much it compared
// and the first difference, nil for none, or the Problem that kept it
// from comparing.
type lookReading struct {
	Elements, Properties int
	Problem              string
	Difference           *struct {
		Width                                 int
		Path, Pseudo, Property, Whole, Sliced string
	}
}

// compareLooks reads one comparison of the loaded forms.
func compareLooks(
	t *testing.T, ctx context.Context, name, scheme string,
) lookReading {
	t.Helper()
	raw, err := chromedp.Run(ctx, chromedp.Evaluate[string](
		"sameLook.compare("+jsString(scheme)+")"))
	if err != nil {
		t.Fatalf("%s, %s scheme: comparing: %v", name, scheme, err)
	}
	var reading lookReading
	if err := json.Unmarshal([]byte(raw), &reading); err != nil {
		t.Fatalf("%s, %s scheme: reading the comparison %q: %v",
			name, scheme, raw, err)
	}
	return reading
}

// emulateScheme has the tab, and every frame in it, report scheme as
// the OS's colour scheme.
func emulateScheme(scheme string) chromedp.Action[chromedp.Void] {
	return chromedp.Func(func(ctx context.Context, tab *chromedp.Target) error {
		_, err := cdp.Call(ctx, tab, emulation.SetEmulatedMedia,
			emulation.SetEmulatedMediaParams{
				Features: []*emulation.MediaFeature{
					{Name: "prefers-color-scheme", Value: scheme},
				},
			})
		return err
	})
}

// ServeSameLookHost answers SameLookPath with the host page.
func ServeSameLookHost(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Write([]byte(SameLookHost))
}
