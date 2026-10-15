package generate

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeCatalog(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMissingKeys(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "locales")
	writeCatalog(t, dir, "en.toml", "a = \"A\"\nb = \"B\"\nc = \"C\"\n")
	writeCatalog(t, dir, "fr.toml", "a = \"A\"\n")
	writeCatalog(t, dir, "de.toml", "a = \"A\"\nb = \"B\"\nc = \"C\"\n")

	got, err := MissingKeys(dir, "en", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"fr": {"b", "c"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MissingKeys = %v, want %v (de is complete and must not appear)", got, want)
	}
}

func TestMissingKeysIgnoresExtraKeysInANonDefaultLocale(t *testing.T) {
	// A locale carrying a key the default does not have is not a build
	// failure: design doc §10 only fails on the other direction.
	dir := filepath.Join(t.TempDir(), "locales")
	writeCatalog(t, dir, "en.toml", "a = \"A\"\n")
	writeCatalog(t, dir, "fr.toml", "a = \"A\"\nextra = \"E\"\n")

	got, err := MissingKeys(dir, "en", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("MissingKeys = %v, want none", got)
	}
}

// rastrillo.NewLocales serves an offered pt-PT from locales/pt.toml
// with locales/pt-PT.toml layered over it, so the check has to compare
// that same layered catalog. Comparing the files one by one failed a
// Portuguese-default app that kept its pt.toml ("no pt-PT.toml"), and
// reported a pt-PT.toml holding only overrides as missing every key
// pt.toml supplies.
func TestMissingKeysReadsARetaggedCatalogUnderItsTarget(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "locales")
	writeCatalog(t, dir, "pt.toml", "a = \"A\"\nb = \"B\"\n")
	writeCatalog(t, dir, "en.toml", "a = \"A\"\n")
	got, err := MissingKeys(dir, "pt-PT", nil)
	if err != nil {
		t.Fatalf("a pt-PT default served from pt.toml: %v", err)
	}
	if want := map[string][]string{"en": {"b"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("pt-PT default from pt.toml: %v, want %v", got, want)
	}

	// pt.toml is a layer of the pt-PT default here, not a language of
	// its own to check against it.
	writeCatalog(t, dir, "pt-PT.toml", "c = \"C\"\n")
	writeCatalog(t, dir, "en.toml", "a = \"A\"\nb = \"B\"\nc = \"C\"\n")
	if got, err := MissingKeys(dir, "pt-PT", nil); err != nil || len(got) != 0 {
		t.Errorf("pt-PT default over pt.toml: %v, %v; want none", got, err)
	}

	// An English default: the override file is complete once pt.toml
	// is under it.
	if got, err := MissingKeys(dir, "en", nil); err != nil || len(got) != 0 {
		t.Errorf("a partial pt-PT.toml over a complete pt.toml: %v, %v; want none", got, err)
	}
}

func TestMissingKeysWithNoLocalesDirectory(t *testing.T) {
	// A single-locale app ships no catalogs at all; that is not a
	// failure, it is the common case (design doc §10).
	got, err := MissingKeys(filepath.Join(t.TempDir(), "locales"), "en", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("MissingKeys = %v, want none", got)
	}
}

func TestMissingKeysRequiresTheDefaultCatalog(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "locales")
	writeCatalog(t, dir, "fr.toml", "a = \"A\"\n")

	_, err := MissingKeys(dir, "en", nil)
	if err == nil {
		t.Fatal("want an error when other locales exist but the default's catalog does not")
	}
	if !strings.Contains(err.Error(), "en.toml") {
		t.Errorf("error should name the missing file: %v", err)
	}
}

func TestMissingKeysReportsAnUndecodableCatalog(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "locales")
	writeCatalog(t, dir, "en.toml", "a = \"A\"\n")
	writeCatalog(t, dir, "fr.toml", "[table]\n")

	_, err := MissingKeys(dir, "en", nil)
	if err == nil {
		t.Fatal("want an error for an undecodable catalog")
	}
	if !strings.Contains(err.Error(), "fr.toml") {
		t.Errorf("error should name the offending file: %v", err)
	}
}

func TestMissingFrameworkKeys(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("en.toml", "app.title = \"Orders\"\n")
	write("fr.toml", "app.title = \"Commandes\"\nrastrillo.ui.cancel = \"Annuler\"\n")
	write("ga.toml", "app.title = \"Orduithe\"\n") // shipped by the framework: exempt
	keys := []string{"rastrillo.ui.cancel", "rastrillo.ui.done"}
	got, err := MissingFrameworkKeys(dir, "en", keys, []string{"en", "ga"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"fr": {"rastrillo.ui.done"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// The framework's Portuguese ships as pt-PT, and an app's pt.toml is
// read under both codes (rastrillo.NewLocales), so the framework covers
// it: demanding every rastrillo.ui.* key there would fail a catalog
// that worked the day before the retag.
func TestMissingFrameworkKeysCoversARetaggedCode(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{"en.toml": "app.title = \"Orders\"\n", "pt.toml": "app.title = \"Encomendas\"\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := MissingFrameworkKeys(dir, "en", []string{"rastrillo.ui.cancel"}, []string{"en", "pt-PT"})
	if err != nil || len(got) != 0 {
		t.Errorf("got %v, %v; want pt covered by the shipped pt-PT", got, err)
	}
}

func TestMissingFrameworkKeysNoLocalesDir(t *testing.T) {
	got, err := MissingFrameworkKeys(filepath.Join(t.TempDir(), "nope"), "en", []string{"k"}, nil)
	if err != nil || got != nil {
		t.Errorf("got %v, %v", got, err)
	}
}
