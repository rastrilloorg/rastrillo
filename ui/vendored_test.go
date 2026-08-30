package ui

import (
	"bytes"
	"sort"
	"testing"
)

// TestVendoredNamesMatchVendoredAssets holds the ordered name list and
// the byte map to the same set. They are two views of one list, and the
// whole reason this file exists is that two views of one list drift.
func TestVendoredNamesMatchVendoredAssets(t *testing.T) {
	assets, ok := VendoredAssets("day")
	if !ok {
		t.Fatal(`VendoredAssets("day") reported an unknown theme`)
	}
	names := VendoredNames()
	if len(names) != len(assets) {
		t.Fatalf("VendoredNames has %d entries, VendoredAssets has %d", len(names), len(assets))
	}
	got := append([]string(nil), names...)
	sort.Strings(got)
	var want []string
	for name := range assets {
		want = append(want, name)
	}
	sort.Strings(want)
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("VendoredNames %v does not match VendoredAssets keys %v", got, want)
		}
	}
	for _, name := range names {
		if len(assets[name]) == 0 {
			t.Errorf("vendored %s is empty", name)
		}
	}
}

// TestVendoredAssetsCarryTheLibraryBytes pins each entry to the
// accessor it must equal: a caller that re-copies from VendoredAssets
// must land the same bytes as one that called TokensCSS itself.
func TestVendoredAssetsCarryTheLibraryBytes(t *testing.T) {
	assets, ok := VendoredAssets("plain")
	if !ok {
		t.Fatal(`VendoredAssets("plain") reported an unknown theme`)
	}
	plain, _ := ThemeCSS("plain")
	for name, want := range map[string][]byte{
		"tokens.css":   TokensCSS(),
		"theme.css":    plain,
		"rastrillo.js": ShimJS(),
		"select.js":    SelectJS(),
		"datetime.js":  DatetimeJS(),
	} {
		if !bytes.Equal(assets[name], want) {
			t.Errorf("VendoredAssets[%q] is not the library copy", name)
		}
	}
}

// TestVendoredAssetsRejectsAnUnknownTheme: the theme is the one
// parameter, and a typo must fail rather than deliver a themeless set.
func TestVendoredAssetsRejectsAnUnknownTheme(t *testing.T) {
	if _, ok := VendoredAssets("nosuchtheme"); ok {
		t.Fatal("VendoredAssets accepted a theme that is not shipped")
	}
}

// TestEveryShippedThemeHasAVendoredSet: --theme picks from
// ThemeNames(), so every name there must produce a set.
func TestEveryShippedThemeHasAVendoredSet(t *testing.T) {
	for _, name := range ThemeNames() {
		if _, ok := VendoredAssets(name); !ok {
			t.Errorf("VendoredAssets(%q) reported an unknown theme, but it is in ThemeNames()", name)
		}
	}
}
