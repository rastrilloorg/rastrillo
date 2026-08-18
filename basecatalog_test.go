package rastrillo

import "testing"

// TestBaseCatalogResolvesThroughLocales exercises the base layer through
// the real (*Locales).T, per locale.go's doc comment: requested locale's
// app catalog, then the default locale's, then the framework base, then
// the key itself.
func TestBaseCatalogResolvesThroughLocales(t *testing.T) {
	loc, err := NewLocales([]string{"en"}, "en", BaseCatalog(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := loc.T("en", "rastrillo.ui.cancel"); got != "Cancel" {
		t.Errorf("base layer not resolving: %q", got)
	}
	if got := loc.T("en", "no.such.key"); got != "no.such.key" {
		t.Errorf("missing keys still fall back to the key: %q", got)
	}
}

// TestBaseCatalogIsACopy guards BaseCatalog's doc promise: a caller
// mutating its returned map must not corrupt the shared baseCatalog
// every app's Locales resolves against.
func TestBaseCatalogIsACopy(t *testing.T) {
	c := BaseCatalog()
	c["rastrillo.ui.cancel"] = "tampered"
	if baseCatalog["rastrillo.ui.cancel"] != "Cancel" {
		t.Fatalf("BaseCatalog() returned the live map, not a copy: shared catalog is now %q", baseCatalog["rastrillo.ui.cancel"])
	}
	if got := BaseCatalog()["rastrillo.ui.cancel"]; got != "Cancel" {
		t.Errorf("a second BaseCatalog() call = %q, want the untampered default", got)
	}
}
