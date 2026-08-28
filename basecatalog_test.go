package rastrillo

import (
	"reflect"
	"strings"
	"testing"
)

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
	if baseCatalogs["en"]["rastrillo.ui.cancel"] != "Cancel" {
		t.Fatalf("BaseCatalog() returned the live map, not a copy: shared catalog is now %q", baseCatalogs["en"]["rastrillo.ui.cancel"])
	}
	if got := BaseCatalog()["rastrillo.ui.cancel"]; got != "Cancel" {
		t.Errorf("a second BaseCatalog() call = %q, want the untampered default", got)
	}
}

// TestBaseCatalogsShareOneKeySet is spec §3.2's gate: every shipped
// catalog holds exactly the en key set, so a locale can never be
// silently missing a string that en has.
func TestBaseCatalogsShareOneKeySet(t *testing.T) {
	want := []string{"en", "ga", "zh-Hans", "es", "hi", "pt", "bn", "ru", "ja", "yue", "vi", "ar"}
	if got := BaseLocales(); !reflect.DeepEqual(got, want) {
		t.Fatalf("BaseLocales = %v, want %v", got, want)
	}
	all := BaseCatalogs()
	en := all["en"]
	if len(en) == 0 {
		t.Fatal("en catalog is empty")
	}
	for _, code := range want {
		c, ok := all[code]
		if !ok {
			t.Errorf("no catalog for %s", code)
			continue
		}
		for k := range en {
			if v, ok := c[k]; !ok || strings.TrimSpace(v) == "" {
				t.Errorf("%s.toml: missing or empty %s", code, k)
			}
		}
		for k := range c {
			if _, ok := en[k]; !ok {
				t.Errorf("%s.toml: key %s is not in en", code, k)
			}
		}
		if code != "en" && c["rastrillo.ui.locale_name"] == en["rastrillo.ui.locale_name"] {
			t.Errorf("%s.toml: locale_name is still English", code)
		}
	}
	for _, k := range BaseKeys() {
		if !strings.HasPrefix(k, "rastrillo.ui.") {
			t.Errorf("key %s is not namespaced rastrillo.ui.*", k)
		}
	}
}

// TestBaseCatalogsAreCopies mirrors TestBaseCatalogIsACopy for the map.
func TestBaseCatalogsAreCopies(t *testing.T) {
	BaseCatalogs()["ga"]["rastrillo.ui.cancel"] = "tampered"
	if got := BaseCatalogs()["ga"]["rastrillo.ui.cancel"]; got == "tampered" {
		t.Fatal("BaseCatalogs returned live maps")
	}
}

// TestIsBaseKey covers the predicate `rastrillo generate --check` uses to
// tell a framework key from an app's own: only keys en actually ships,
// and only under the rastrillo.ui.* namespace.
func TestIsBaseKey(t *testing.T) {
	if !IsBaseKey("rastrillo.ui.error_404_title") || !IsBaseKey("rastrillo.ui.cancel") {
		t.Error("shipped keys must report true")
	}
	if IsBaseKey("rastrillo.ui.nope") || IsBaseKey("app.title") {
		t.Error("unshipped keys must report false")
	}
}
