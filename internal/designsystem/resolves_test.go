package designsystem

import (
	"path"
	"regexp"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/ui"
)

// scriptSource is a script tag's src, in any of the forms a page writes.
var scriptSource = regexp.MustCompile(`<script\b[^>]*\bsrc="([^"]*)"`)

// The gallery renders ui's layouts outside ui.Pages, where they link
// every optional script, and assetURL panics on a name the tree does
// not ship. So the tree ships every script an app vendors, and every
// script a rendered page links is a file in the tree.
func TestTheGalleryResolvesEveryScriptItLinks(t *testing.T) {
	assets := galleryAssets()
	for _, name := range ui.VendoredNames() {
		if _, ok := assets[name]; !ok && path.Ext(name) == ".js" {
			t.Errorf("an app vendors %s and the gallery does not ship it",
				name)
		}
	}
	if t.Failed() {
		t.FailNow()
	}
	files := render(t)
	for name, body := range files {
		if !strings.HasSuffix(name, ".html") {
			continue
		}
		for _, match := range scriptSource.FindAllSubmatch(body, -1) {
			source := string(match[1])
			file, ok := strings.CutPrefix(source, mountPrefix)
			if _, served := files[file]; !ok || !served {
				t.Errorf("%s links %s, which the tree does not serve",
					name, source)
			}
		}
	}
}
