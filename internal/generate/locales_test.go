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

	got, err := MissingKeys(dir, "en")
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

	got, err := MissingKeys(dir, "en")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("MissingKeys = %v, want none", got)
	}
}

func TestMissingKeysWithNoLocalesDirectory(t *testing.T) {
	// A single-locale app ships no catalogs at all; that is not a
	// failure, it is the common case (design doc §10).
	got, err := MissingKeys(filepath.Join(t.TempDir(), "locales"), "en")
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

	_, err := MissingKeys(dir, "en")
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

	_, err := MissingKeys(dir, "en")
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

func TestMissingFrameworkKeysNoLocalesDir(t *testing.T) {
	got, err := MissingFrameworkKeys(filepath.Join(t.TempDir(), "nope"), "en", []string{"k"}, nil)
	if err != nil || got != nil {
		t.Errorf("got %v, %v", got, err)
	}
}
