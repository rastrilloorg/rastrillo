//go:build browser

package galleryrig

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// Unstyled lists the top document's stylesheets that did not load.
// Chrome gives a stylesheet that failed an empty sheet rather than none,
// or one whose rules it will not show, and every stylesheet the tree
// serves has rules.
const Unstyled = `[...document.querySelectorAll('link[rel~="stylesheet"]')]
  .filter(link => {
    try { return !link.sheet.cssRules.length; } catch { return true; }
  }).map(link => link.href)`

// LoadStyled is LoadAt for a drive that measures or scans what it
// loads: it also requires every stylesheet the page links, and loads
// the page once more if Chrome dropped the navigation or one of them.
//
// On the fleet a page now and then loads without its stylesheets,
// though the tree serves each from memory and counts a request for a
// file it lacks (rastrillo#427): the pinned-bar sweep measured plain/en
// unstyled at all four widths, and the 320px reflow check measured
// Choices and actions at 1998px in a 320px page, the user agent's 8px
// body margin and no scrollbar gutter (failing tokens.css and
// gallery.css here gives 2004px and the same message). Loading waits
// for the load event, which waits for every stylesheet, so they had not
// arrived late: Chrome had given up on them. That is the family of the
// navigations Chrome drops under load, which LoadAt tries once more.
// LoadStyled does the same for a stylesheet, only for one Chrome
// reported failing and the tree did not refuse, and logs Chrome's
// reason, so the next fleet run that needs it says which error it was.
func LoadStyled(
	t *testing.T, rig *harness.Rig, ctx context.Context, where string,
	size chromedp.Action[chromedp.Void], url string,
	ready chromedp.Action[chromedp.Void],
) {
	t.Helper()
	if err := loadStyledErr(t, rig, ctx, size, url, ready); err != nil {
		t.Fatalf("%s: loading: %v", where, err)
	}
}

// loadStyledErr is LoadStyled without the test, so a test can check its
// failures. There is one more try in all, for whichever Chrome dropped
// first, the navigation or a stylesheet: a load that meets two drops is
// not the rare one this allows for, and fails naming both.
func loadStyledErr(
	t *testing.T, rig *harness.Rig, ctx context.Context,
	size chromedp.Action[chromedp.Void], url string,
	ready chromedp.Action[chromedp.Void],
) error {
	t.Helper()
	first := droppedWhat(loadStyled(t, rig, ctx, size, url, ready))
	if first.err != nil || first.why == "" {
		return first.err
	}
	t.Logf("%s: %s; loading it once more", url, first.why)
	again := droppedWhat(loadStyled(t, rig, ctx, size, url, ready))
	if again.why != "" {
		return fmt.Errorf("%s, after %s", again.why, first.why)
	}
	return again.err
}

// attempt is how one try at a styled load ended: what Chrome dropped,
// which may be tried once more, or an error that may not.
type attempt struct {
	why string
	err error
}

func droppedWhat(dropped []string, err error) attempt {
	switch {
	case worthAnotherTry(err):
		return attempt{why: fmt.Sprintf("Chrome dropped the navigation"+
			" (%v)", err)}
	case err != nil:
		return attempt{err: err}
	case len(dropped) > 0:
		return attempt{why: "Chrome dropped a stylesheet (" +
			strings.Join(dropped, "; ") + ")"}
	}
	return attempt{}
}

// loadStyled is one attempt. It answers each stylesheet Chrome reported
// failing, with Chrome's reason, and an error for one that is missing
// because the server refused it (Chrome reports a 404 stylesheet as
// net::ERR_ABORTED too) or with no failure reported: neither is Chrome
// dropping it.
func loadStyled(
	t *testing.T, rig *harness.Rig, ctx context.Context,
	size chromedp.Action[chromedp.Void], url string,
	ready chromedp.Action[chromedp.Void],
) ([]string, error) {
	t.Helper()
	watch, stop := watchSheets(rig, ctx, url)
	defer stop()
	// One navigation: loadStyledErr owns the one more try.
	if err := loadOnce(ctx, size, url, ready); err != nil {
		return nil, err
	}
	// Listen has handled every event sent before this answer arrives.
	missing, err := chromedp.Run(ctx, chromedp.Evaluate[[]string](Unstyled))
	if err != nil {
		return nil, fmt.Errorf("reading the page's stylesheets: %w", err)
	}
	return watch.dropped(missing)
}

// sheetWatch follows the requests the top frame makes once it asks for
// url: how Chrome failed each, and how the tree answered. Only those:
// the page being left, and its preview frames, ask for the same
// stylesheets, and leaving cancels theirs.
type sheetWatch struct {
	lock     sync.Mutex
	url      string
	top      cdp.FrameID
	asked    map[network.RequestID]string
	why      map[string]string
	answered map[string]int64
}

func watchSheets(
	rig *harness.Rig, ctx context.Context, url string,
) (*sheetWatch, func()) {
	watch := &sheetWatch{
		url:      url,
		asked:    map[network.RequestID]string{},
		why:      map[string]string{},
		answered: map[string]int64{},
	}
	stops := []func(){
		harness.Listen(rig, ctx, network.RequestWillBeSent, watch.ask),
		harness.Listen(rig, ctx, network.LoadingFailed, watch.fail),
		// The status comes from the raw headers: for a 404 stylesheet
		// Chrome sends no responseReceived, only the failure and these.
		harness.Listen(rig, ctx, network.ResponseReceivedExtraInfo,
			watch.answer),
	}
	return watch, func() {
		for _, stop := range stops {
			stop()
		}
	}
}

func (watch *sheetWatch) ask(event *network.EventRequestWillBeSent) {
	watch.lock.Lock()
	defer watch.lock.Unlock()
	if event.Type == network.ResourceTypeDocument &&
		event.Request.URL == watch.url {
		watch.top = event.FrameID
	} else if watch.top != "" && event.FrameID == watch.top {
		watch.asked[event.RequestID] = event.Request.URL
	}
}

func (watch *sheetWatch) fail(event *network.EventLoadingFailed) {
	watch.lock.Lock()
	defer watch.lock.Unlock()
	if href, ok := watch.asked[event.RequestID]; ok {
		watch.why[href] = event.ErrorText
	}
}

func (watch *sheetWatch) answer(
	event *network.EventResponseReceivedExtraInfo,
) {
	watch.lock.Lock()
	defer watch.lock.Unlock()
	if href, ok := watch.asked[event.RequestID]; ok {
		watch.answered[href] = event.StatusCode
	}
}

// dropped sorts the stylesheets that did not load into the ones Chrome
// dropped, each with its reason, and an error for any other.
func (watch *sheetWatch) dropped(missing []string) ([]string, error) {
	watch.lock.Lock()
	defer watch.lock.Unlock()
	var dropped []string
	for _, href := range missing {
		reason, ok := watch.why[href]
		if status := watch.answered[href]; status >= http.StatusBadRequest {
			return nil, fmt.Errorf("%s did not load: the tree answered %d",
				href, status)
		}
		if !ok {
			return nil, fmt.Errorf("%s did not load, and Chrome reported"+
				" no failure for it", href)
		}
		dropped = append(dropped, href+": "+reason)
	}
	return dropped, nil
}
