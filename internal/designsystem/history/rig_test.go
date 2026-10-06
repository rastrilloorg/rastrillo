//go:build browser

// Package history holds the gallery's browser legs that need a real
// back/forward cache restore or a real new-tab click: the headless
// shell the cloud runner links as chromium never gives either, so
// these legs need full Chromium (Makefile's FULL_CHROMIUM), and they
// are a package of their own so that cost falls on only them, not on
// internal/designsystem or internal/designsystem/sweep.
package history

import (
	"context"
	"net/http"
	"testing"

	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/internal/designsystem"
	"amadan.net/rastrillo/rastrillo/internal/designsystem/galleryrig"
)

// The gallery's hooks and the shared drive helpers, under the names
// these legs are written with. One line each: the implementations are
// the gallery's and galleryrig's.

const (
	mountPath   = designsystem.DefaultMount
	mountPrefix = mountPath + "/"
)

func pageHref(mount, theme, locale, file string) string {
	return designsystem.PageHref(mount, theme, locale, file)
}

func fileOf(kind string) string { return designsystem.PageFile(kind) }

// indexHref is the overview page's address: the one page kind every
// index drive navigates to by name rather than by walking pageKinds().
func indexHref(mount, theme, locale string) string {
	return pageHref(mount, theme, locale, fileOf("overview"))
}

func treeHandler(t *testing.T) http.Handler {
	t.Helper()
	files, err := designsystem.Render(mountPath)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return galleryrig.Tree(t, files, mountPrefix)
}

func addInit(js string) chromedp.Action { return galleryrig.AddInit(js) }

func phoneRig(t *testing.T) *harness.Rig {
	t.Helper()
	return galleryrig.PhoneRig(t, func() http.Handler { return treeHandler(t) })
}

func requireCoarse(t *testing.T, ctx context.Context) {
	t.Helper()
	galleryrig.RequireCoarse(t, ctx)
}

func until(t *testing.T, ctx context.Context, where, expr string) {
	t.Helper()
	galleryrig.Until(t, ctx, where, expr)
}
