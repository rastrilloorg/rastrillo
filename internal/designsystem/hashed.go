package designsystem

import (
	"fmt"
	"strings"
	"sync"
	"testing/fstest"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/ui"
)

// galleryAssets is every stylesheet and script the tree ships, by its
// plain name: the framework's, one stylesheet per theme, and the
// gallery's own two. Render writes each once, under its hashed name.
func galleryAssets() map[string][]byte {
	out := map[string][]byte{
		"tokens.css":   ui.TokensCSS(),
		"rastrillo.js": ui.ShimJS(),
		"busy.js":      ui.BusyJS(),
		"shell.js":     ui.ShellJS(),
		"shell.css":    ui.ShellCSS(),
		"select.js":    ui.SelectJS(),
		"datetime.js":  ui.DatetimeJS(),
		"calendar.js":  ui.CalendarJS(),
		"gallery.js":   GalleryJS(),
		"gallery.css":  GalleryCSS(),
	}
	for _, theme := range ui.ThemeNames() {
		if css, ok := ui.ThemeCSS(theme); ok {
			out["theme-"+theme+".css"] = css
		}
	}
	return out
}

// hashedNames maps each asset's plain name to the name it is written
// under: name.<16 hex of its sha256>.ext. The tree is served by a static
// edge that caches assets, and with plain names a deploy could pair a
// new page with yesterday's cached tokens.css, a page of new markup
// drawn by old rules. A name that changes whenever the bytes do cannot
// be served stale. The name is rastrillo.Assets' own, read off its Path
// rather than computed again here, so the gallery and every scaffolded
// app spell a hashed asset one way. The bytes are embedded, so this is
// worked out once per process and every render of the tree agrees.
var hashedNames = sync.OnceValue(func() map[string]string {
	assets := galleryAssets()
	fsys := fstest.MapFS{}
	for name, body := range assets {
		fsys[name] = &fstest.MapFile{Data: body}
	}
	paths := rastrillo.NewAssets(fsys)
	out := make(map[string]string, len(assets))
	for name := range assets {
		out[name] = strings.TrimPrefix(paths.Path(name), "/")
	}
	return out
})

// assetURL is the address a page links an asset by: the hashed name
// under the mount. A name the tree does not ship panics, because it is
// a typo in this package's own templates, and a page linking a file
// that is not there renders unstyled with no other sign of it.
func assetURL(mount, name string) string {
	hashed, ok := hashedNames()[name]
	if !ok {
		panic(fmt.Sprintf("designsystem: no asset %q in the tree", name))
	}
	return mount + "/" + hashed
}

// assetFunc is the asset template func for one theme's pages. It takes
// a name as the gallery's own templates write it ("tokens.css") or as
// ui's layouts do ("static/tokens.css"), and theme.css, which the
// layouts link and an app vendors as one file, is this theme's own
// stylesheet.
func assetFunc(mount, theme string) func(string) string {
	return func(p string) string {
		name := strings.TrimPrefix(p, "static/")
		if name == "theme.css" {
			name = "theme-" + theme + ".css"
		}
		return assetURL(mount, name)
	}
}
