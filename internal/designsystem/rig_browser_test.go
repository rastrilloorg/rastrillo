//go:build browser

package designsystem

import (
	"context"
	"net/http"
	"testing"

	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/internal/designsystem/galleryrig"
)

// The shared drive helpers, under the names this package's tests are
// written with. One line each: the implementation is galleryrig's, which
// the sweep package uses too, so the two cannot drift.

func treeHandler(t *testing.T) http.Handler {
	t.Helper()
	files, err := Render(mountPath)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return galleryrig.Tree(t, files, mountPrefix)
}

func eagerly(t *testing.T, ctx context.Context, where string) {
	t.Helper()
	galleryrig.Eagerly(t, ctx, where)
}

func mobileSettle(t *testing.T, ctx context.Context, where string) {
	t.Helper()
	galleryrig.MobileSettle(t, ctx, where)
}

func addInit(js string) chromedp.Action { return galleryrig.AddInit(js) }

func until(t *testing.T, ctx context.Context, where, expr string) {
	t.Helper()
	galleryrig.Until(t, ctx, where, expr)
}

func phoneRig(t *testing.T) *harness.Rig {
	t.Helper()
	return galleryrig.PhoneRig(t, func() http.Handler { return treeHandler(t) })
}

func requireDrawnScrollbar(t *testing.T, ctx context.Context, where string) {
	t.Helper()
	galleryrig.RequireDrawnScrollbar(t, ctx, where)
}

func requireCoarse(t *testing.T, ctx context.Context) {
	t.Helper()
	galleryrig.RequireCoarse(t, ctx)
}

func settleMotion(t *testing.T, ctx context.Context, where string) {
	t.Helper()
	galleryrig.SettleMotion(t, ctx, where)
}

func noScripts(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	return galleryrig.NoScripts(t, ctx)
}

func requireScriptsOff(t *testing.T, ctx context.Context, where string) {
	t.Helper()
	galleryrig.RequireScriptsOff(t, ctx, where)
}

func heightRows() []galleryrig.HeightRow { return galleryrig.HeightRows(PageKinds(), ExampleCounts()) }
