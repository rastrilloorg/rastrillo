//go:build browser

package galleryrig

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// aborted is Chrome's answer to a navigation it dropped before the
// response committed.
const aborted = "net::ERR_ABORTED"

// Load sizes the tab to width × height, opens url and runs ready, and
// fails the test naming where if that does not work.
//
// On a loaded fleet runner Chrome now and then drops one of a sweep's
// hundreds of navigations with net::ERR_ABORTED, though nothing on the
// page asked to go anywhere (rastrillo#233; it never happens on an idle
// machine). Load tries such a navigation once more, but only when the
// page's top frame asked for no navigation of its own: a page that sends
// itself somewhere else is a bug, and its failure names where it went.
func Load(
	t *testing.T, ctx context.Context, where string,
	width, height int64, url string, ready chromedp.Action[chromedp.Void],
) {
	t.Helper()
	if err := loadErr(ctx, width, height, url, ready); err != nil {
		t.Fatalf("%s: loading: %v", where, err)
	}
}

// loadErr is Load without the test, so a test can check its failures.
func loadErr(
	ctx context.Context, width, height int64, url string,
	ready chromedp.Action[chromedp.Void],
) error {
	err := loadOnce(ctx, width, height, url, ready)
	if worthAnotherTry(err) {
		return loadOnce(ctx, width, height, url, ready)
	}
	return err
}

// worthAnotherTry is true only of an abort Chrome made on its own: an
// error a page's own navigation caused is wrapped in pageNavigated,
// which hides the abort underneath it.
func worthAnotherTry(err error) bool {
	var dropped *chromedp.LoadError
	return errors.As(err, &dropped) && dropped.ErrorText == aborted
}

// loadOnce is one attempt. Any navigation the top frame asked for while
// it ran is named in its error, and makes an abort final.
func loadOnce(
	ctx context.Context, width, height int64, url string,
	ready chromedp.Action[chromedp.Void],
) error {
	top, err := topFrame(ctx)
	if err != nil {
		return err
	}
	// chromedp subscribes when Events is called, so the watch is in
	// place before the navigation starts.
	watching, stop := context.WithCancel(ctx)
	requests := chromedp.Events(watching, page.FrameRequestedNavigation)
	var lock sync.Mutex
	var asked []string
	var done sync.WaitGroup
	done.Go(func() {
		for request, err := range requests {
			if err != nil {
				return
			}
			if request.FrameID == top {
				lock.Lock()
				asked = append(asked, request.URL)
				lock.Unlock()
			}
		}
	})
	err = chromedp.Do(ctx, chromedp.EmulateViewport(width, height),
		chromedp.Navigate(url), ready)
	// One more round trip before the watch stops: Chrome sends events
	// before the answer to a later command, so a request the page made
	// during the attempt is queued by then rather than lost to the stop.
	_, _ = topFrame(ctx)
	stop()
	done.Wait()
	if err != nil && len(asked) > 0 {
		return fmt.Errorf("%w (the page asked for a navigation of its own"+
			" to %s)", pageNavigated{err}, strings.Join(asked, ", "))
	}
	return err
}

// pageNavigated wraps an error the page's own navigation caused, so it
// no longer reads as an abort worth trying again.
type pageNavigated struct{ cause error }

func (failure pageNavigated) Error() string { return failure.cause.Error() }

// topFrame is the ID of the tab's top frame, the one a navigation the
// test asks for loads into.
func topFrame(ctx context.Context) (cdp.FrameID, error) {
	var top cdp.FrameID
	err := chromedp.Do(ctx, chromedp.Func(
		func(ctx context.Context, target *chromedp.Target) error {
			tree, err := cdp.Call(ctx, target, page.GetFrameTree, cdp.Empty{})
			if err != nil {
				return err
			}
			top = tree.FrameTree.Frame.ID
			return nil
		}))
	return top, err
}
