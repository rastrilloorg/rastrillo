//go:build browser

package designsystem

import (
	"context"
	"net/http"
	"testing"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
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

func addInit(js string) chromedp.Action[chromedp.Void] {
	return galleryrig.AddInit(js)
}

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

// disableJavaScript is a step that switches the tab's page scripts off
// at the engine.
func disableJavaScript() chromedp.Action[chromedp.Void] {
	return chromedp.Func(func(ctx context.Context, tab *chromedp.Target) error {
		_, err := cdp.Call(ctx, tab, emulation.SetScriptExecutionDisabled,
			emulation.SetScriptExecutionDisabledParams{Value: true})
		return err
	})
}

// prefersColorScheme is a step that has the tab report scheme, "light"
// or "dark", as the OS's colour scheme.
func prefersColorScheme(scheme string) chromedp.Action[chromedp.Void] {
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

// ptr points at a copy of value, for cdproto's optional fields. Go
// 1.26's new(value) does the same, and the module still builds on Go 1.25.
func ptr[T any](value T) *T { return &value }
