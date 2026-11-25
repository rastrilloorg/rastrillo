//go:build browser

package galleryrig

import (
	"context"
	"testing"

	"github.com/chromedp/chromedp"
)

// WithoutAnchorPositioning deletes, from the stylesheet the page already
// loaded, every rule under tokens.css's anchor-positioning @supports
// condition, which is what an engine without the feature gets: the menu
// panel positioned absolutely under its summary instead of fixed against
// it. It answers how many it deleted. The match allows any spacing
// because the tree serves tokens.css minified, as apps do.
//
// It once fetched tokens.css again and swapped in an edited copy, and
// on the fleet that second request failed with "Failed to fetch"
// (rastrillo#427), failing a sweep though the page was fine. The page's
// own copy needs no request.
const WithoutAnchorPositioning = `(() => {
  const link = document.querySelector(
    'link[href*="/tokens."][href$=".css"]');
  const anchored = new RegExp(
    "^\\(position-area:\\s*block-end\\)\\s*and\\s*" +
    "\\(position-try-fallbacks:\\s*flip-block\\)$");
  let deleted = 0;
  (function walk(owner) {
    for (let i = owner.cssRules.length - 1; i >= 0; i--) {
      const rule = owner.cssRules[i];
      if (rule instanceof CSSSupportsRule &&
          anchored.test(rule.conditionText)) {
        owner.deleteRule(i);
        deleted++;
      } else if (rule.cssRules) {
        walk(rule);
      }
    }
  })(link.sheet);
  return deleted;
})()`

// SwitchAnchorPositioningOff runs WithoutAnchorPositioning on the page
// in ctx and fails the test if it found nothing to delete, so a leg
// that deleted none fails rather than measuring the anchored page.
func SwitchAnchorPositioningOff(
	t *testing.T, ctx context.Context, where string,
) {
	t.Helper()
	deleted, err := chromedp.Run(ctx,
		chromedp.Evaluate[int](WithoutAnchorPositioning))
	if err != nil {
		t.Fatalf("%s: switching anchor positioning off: %v", where, err)
	}
	if deleted == 0 {
		t.Fatalf("%s: switching anchor positioning off: tokens.css has"+
			" no @supports rule for it left to delete", where)
	}
}
