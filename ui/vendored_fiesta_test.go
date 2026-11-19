package ui

import (
	"bytes"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"amadan.net/rastrillo/fiesta"
	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/vendored"
)

// ownScripts are the two vendored files Rastrillo writes itself: the
// fragment shim and the live pages' script. Every other vendored file
// is Fiesta's, byte for byte.
var ownScripts = []string{"rastrillo.js", "live.js"}

// fiestaFile is the file the pinned Fiesta ships under the name an app
// vendors it by, read through Fiesta's own API rather than through ui,
// so a test comparing the two compares two sources. theme.css is the
// named theme.
func fiestaFile(name, theme string) ([]byte, bool) {
	switch name {
	case "tokens.css":
		return fiesta.TokensCSS(), true
	case "shell.css":
		return fiesta.ShellCSS(), true
	case "theme.css":
		return fiesta.ThemeCSS(theme)
	}
	return fiesta.Script(name)
}

// pinDrift names every vendored file in assets that is not the pinned
// Fiesta's file of the same name, for the named theme.
func pinDrift(names []string, assets map[string][]byte, theme string) []string {
	var problems []string
	for _, name := range names {
		if slices.Contains(ownScripts, name) {
			continue
		}
		want, ok := fiestaFile(name, theme)
		switch {
		case !ok:
			problems = append(problems, name+" is vendored, and Fiesta "+
				fiesta.Version+" does not ship it")
		case !bytes.Equal(assets[name], want):
			problems = append(problems, name+" is not Fiesta "+
				fiesta.Version+"'s "+name)
		}
	}
	return problems
}

func TestEveryVendoredFileComesFromThePinnedFiesta(t *testing.T) {
	for _, theme := range ThemeNames() {
		assets, ok := VendoredAssets(theme)
		if !ok {
			t.Fatalf("VendoredAssets(%q) reported an unknown theme", theme)
		}
		for _, problem := range pinDrift(VendoredNames(), assets, theme) {
			t.Errorf("%s: %s", theme, problem)
		}
	}
	for _, script := range fiesta.ScriptNames() {
		if !slices.Contains(VendoredNames(), script) {
			t.Errorf("Fiesta ships %s and Rastrillo does not vendor it",
				script)
		}
	}
	if !slices.Equal(ThemeNames(), fiesta.ThemeNames()) {
		t.Errorf("ThemeNames is %v, Fiesta's is %v",
			ThemeNames(), fiesta.ThemeNames())
	}
}

// A bump to a Fiesta that dropped a file Rastrillo vendors fails, and
// the failure names the file.
func TestAVendoredFileFiestaDropsIsNamed(t *testing.T) {
	assets := map[string][]byte{"gone.js": []byte("x")}
	problems := pinDrift([]string{"gone.js"}, assets, "day")
	if len(problems) != 1 || !strings.Contains(problems[0], "gone.js") {
		t.Errorf("pinDrift = %q, want one problem naming gone.js", problems)
	}
}

// ui keeps no copy of a file Fiesta ships, under any name, and no Node
// harness that drives one of Fiesta's scripts: those are Fiesta's to
// test. A copy left behind would be the next thing someone edits.
func TestNoFiestaFileHasACopyInUI(t *testing.T) {
	shipped := map[string][]byte{
		"tokens.css": fiesta.TokensCSS(), "shell.css": fiesta.ShellCSS(),
	}
	for _, theme := range fiesta.ThemeNames() {
		shipped["themes/"+theme+".css"], _ = fiesta.ThemeCSS(theme)
	}
	for _, script := range fiesta.ScriptNames() {
		shipped[script], _ = fiesta.Script(script)
	}
	err := fs.WalkDir(os.DirFS("."), ".", func(
		name string, entry fs.DirEntry, err error,
	) error {
		if err != nil || entry.IsDir() {
			return err
		}
		if _, ok := shipped[name]; ok {
			t.Errorf("ui/%s is a file Fiesta ships", name)
		}
		body, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		for fiestaName, fiestaBody := range shipped {
			if bytes.Equal(body, fiestaBody) {
				t.Errorf("ui/%s is a copy of Fiesta's %s", name, fiestaName)
			}
		}
		if path.Ext(name) == ".mjs" {
			for _, script := range fiesta.ScriptNames() {
				if bytes.Contains(body, []byte("./"+script)) {
					t.Errorf("ui/%s drives Fiesta's %s", name, script)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// What an app serves is Fiesta's file as Rastrillo's minifier writes
// it: every vendored file, and every theme as theme.css, fetched over
// HTTP from Assets the way a browser fetches it.
func TestServedVendoredBytesEqualFiestas(t *testing.T) {
	for _, theme := range ThemeNames() {
		assets, ok := VendoredAssets(theme)
		if !ok {
			t.Fatalf("VendoredAssets(%q) reported an unknown theme", theme)
		}
		tree := fstest.MapFS{}
		for name, body := range assets {
			tree["static/"+name] = &fstest.MapFile{Data: body}
		}
		served := rastrillo.NewAssets(tree)
		for _, name := range VendoredNames() {
			if slices.Contains(ownScripts, name) {
				continue
			}
			original, ok := fiestaFile(name, theme)
			if !ok {
				t.Errorf("%s: Fiesta does not ship %s", theme, name)
				continue
			}
			want, err := vendored.Minify(name, original)
			if err != nil {
				t.Fatalf("%s: minifying Fiesta's %s: %v", theme, name, err)
			}
			got := servedBody(t, served, "static/"+name)
			if !bytes.Equal(got, want) {
				t.Errorf("%s: static/%s is served as %d bytes, Fiesta's "+
					"minified is %d", theme, name, len(got), len(want))
			}
			if len(got) >= len(original) {
				t.Errorf("%s: static/%s is served unminified", theme, name)
			}
		}
	}
}

// servedBody is the body Assets answers a plain GET for name with.
func servedBody(t *testing.T, assets *rastrillo.Assets, name string) []byte {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, assets.Path(name), nil)
	recorder := httptest.NewRecorder()
	assets.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET %s: %d", name, recorder.Code)
	}
	return recorder.Body.Bytes()
}
