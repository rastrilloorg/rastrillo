//go:build browser

package galleryrig

import (
	"context"
	"errors"
	"testing"

	"github.com/chromedp/cdproto"
	"github.com/chromedp/chromedp"
)

// navigatedAway is Chrome's reply to a command whose document went away
// before it answered.
const navigatedAway = "Inspected target navigated or closed"

// Leave runs script, which starts a navigation away from the page: a
// tap on a link the page's own script handles, or history.back(). The
// evaluate is answered by the document being left, and a page that comes
// from the back/forward or HTTP cache can commit before that answer is
// sent, so Chrome fails the command with navigatedAway. That is the
// navigation asked for, not a failure; the leg's next Until says where
// it landed, and fails by name if it did not.
func Leave(t *testing.T, ctx context.Context, where, script string) {
	t.Helper()
	err := chromedp.Do(ctx, chromedp.Evaluate[chromedp.Void](script))
	var protocol *cdproto.Error
	if errors.As(err, &protocol) && protocol.Code == -32000 &&
		protocol.Message == navigatedAway {
		return
	}
	if err != nil {
		t.Fatalf("%s: %v", where, err)
	}
}
