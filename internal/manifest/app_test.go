package manifest

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLoadApp(t *testing.T) {
	dir := t.TempDir()
	app, err := LoadApp(dir)
	if err != nil || !slices.Equal(app.Locales, []string{"en"}) || app.DefaultLocale != "en" {
		t.Fatalf("no app.toml: %+v, %v; want en only", app, err)
	}
	write := func(s string) { os.WriteFile(filepath.Join(dir, "app.toml"), []byte(s), 0o644) }
	write("locales = [\"es\", \"en\"]\n")
	if app, err = LoadApp(dir); err != nil || app.DefaultLocale != "es" {
		t.Errorf("default from locales[0]: %+v, %v", app, err)
	}
	write("locales = [\"en\"]\ndefault_lang = \"en\"\n")
	if _, err = LoadApp(dir); err == nil || !strings.Contains(err.Error(), "default_lang") {
		t.Errorf("a misspelled key must be an error naming it, got %v", err)
	}
}

func TestLoadSkipsAppTOMLAndRefusesResourceApp(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "app.toml"), []byte("locales = [\"en\"]\n"), 0o644)
	if rs, err := Load(dir, dir); err != nil || len(rs) != 0 {
		t.Fatalf("Load read app.toml as a resource: %v, %v", rs, err)
	}
	os.WriteFile(filepath.Join(dir, "things.toml"), []byte("name = \"app\"\nroute = \"/app\"\n[form]\nbasics = [{ name = \"Title\" }]\n"), 0o644)
	if _, err := Load(dir, dir); err == nil || !strings.Contains(err.Error(), `"app"`) {
		t.Errorf("a resource named app was accepted: %v", err)
	}
}
