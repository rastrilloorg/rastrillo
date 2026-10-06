//go:build browser

// Package sweep holds the gallery's browser sweeps that run over many
// themes, languages and widths with no axe in them. They are a package
// of their own because go test gives each package its own time limit,
// and the design-system package was already 7 to 8 minutes of its 20.
package sweep

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/internal/designsystem"
	"amadan.net/rastrillo/rastrillo/internal/designsystem/galleryrig"
)

// The gallery's hooks and the shared drive helpers, under the names
// these sweeps are written with. One line each: the implementations are
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

func noScripts(t *testing.T, parent context.Context) context.Context {
	t.Helper()
	return galleryrig.NoScripts(t, parent)
}

func requireScriptsOff(t *testing.T, ctx context.Context, where string) {
	t.Helper()
	galleryrig.RequireScriptsOff(t, ctx, where)
}

func requireAnchorPositioning(t *testing.T, ctx context.Context, where, selector string, want bool) {
	t.Helper()
	galleryrig.RequireAnchorPositioning(t, ctx, where, selector, want)
}

const withoutAnchorPositioning = galleryrig.WithoutAnchorPositioning

func phoneRig(t *testing.T, opts ...harness.Option) *harness.Rig {
	t.Helper()
	return galleryrig.PhoneRig(t, func() http.Handler { return treeHandler(t) }, opts...)
}

func requireDrawnScrollbar(t *testing.T, ctx context.Context, where string) {
	t.Helper()
	galleryrig.RequireDrawnScrollbar(t, ctx, where)
}

func requireCoarse(t *testing.T, ctx context.Context) {
	t.Helper()
	galleryrig.RequireCoarse(t, ctx)
}

func until(t *testing.T, ctx context.Context, where, expr string) {
	t.Helper()
	galleryrig.Until(t, ctx, where, expr)
}

func eagerly(t *testing.T, ctx context.Context, where string) {
	t.Helper()
	galleryrig.Eagerly(t, ctx, where)
}

func mobileSettle(t *testing.T, ctx context.Context, where string) {
	t.Helper()
	galleryrig.MobileSettle(t, ctx, where)
}

var clickedMobile = galleryrig.ClickEvery("m")

func heightRows() []galleryrig.HeightRow {
	return galleryrig.HeightRows(designsystem.PageKinds(), designsystem.ExampleCounts())
}

// sweepFull is this package's opt-in for a sweep too large for routine
// CI: every page a theme × locale combination produces, rather than
// the handful routine CI keeps. make browser-sweep sets it; make ci
// and make browser do not, so the full size runs only when a
// maintainer asks for it by hand.
func sweepFull() bool {
	switch v := os.Getenv("RASTRILLO_SWEEP"); v {
	case "":
		return false
	case "full":
		return true
	default:
		// A misspelt opt-in must not quietly run the routine subset and
		// report the full sweep as passed.
		panic(fmt.Sprintf("RASTRILLO_SWEEP=%q: the only value is \"full\"", v))
	}
}

// sweepLocales is the locale set a routine run drives this package's
// heavy sweeps over when sweepFull is false: en for the baseline, ar
// for the one RTL locale, and ru and vi because they measured as
// TestThePinnedBarFitsItsReservation's worst wrappers - the two most
// likely to regress the --ds-bar-h reservation a shorter label would
// not stress. Dropping the other eight locales still exercises every
// theme, width and scripts-on/off combination the full sweep covers.
func sweepLocales() []string {
	if sweepFull() {
		return rastrillo.BaseLocales()
	}
	return []string{"en", "ar", "ru", "vi"}
}
