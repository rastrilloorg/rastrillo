// Package designsystem renders Rastrillo components, the gallery at
// rastrillo.org/components: one static page per theme × locale
// showing every partial and icon the framework ships, plus a full-page
// demo of each of the five shells and one of the modal route. The
// tokens, the markup with no Go call and each stylesheet and script are
// Fiesta's, and the pages link Fiesta's design system for them
// (fiesta_links.go).
//
// It exists because a component library nobody can look at is a
// specification, not a library. The page is the only place the whole
// vocabulary is on screen at once — every tone beside every other tone,
// every field in its error state, the RTL and CJK renderings of the same
// markup — and it is the only place the two enhanced controls
// (select.js's filterable combobox, datetime.js's natural-language date
// field and the month grid calendar.js opens for it
// field) actually run.
//
// Render returns the whole tree in memory, keyed by path relative to the
// tree root. Nothing in this repository holds a copy of it: cmd/dsgen
// writes it, and the website that publishes it runs cmd/dsgen at build
// time against a pinned version. `go generate ./...` writes a copy into
// a git-ignored directory for looking at locally, and that copy is
// disposable — the render is the artifact.
//
// Determinism is a contract, not a nicety: every gate here compares two
// renders, and a site that rebuilds the gallery on every deploy should
// get the same bytes from the same version.
// Every map in here — Styleguide's samples, BaseCatalogs, the parsed
// token blocks — is sorted before anything reaches output.
package designsystem

import (
	_ "embed"
	"fmt"
	"html/template"
	"path"
	"sort"
	"strings"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/ui"
)

// galleryJS is the gallery's own script, embedded from the file beside
// this one so it is edited as JavaScript — with a syntax highlighter
// and a linter — rather than as a Go string constant.
//
// It lives here and not in ui/ because it is not part of the framework:
// no scaffold writes it, no app receives it, and the only page that
// loads it is the one this package renders. Its header comment is its
// contract; TestGalleryScriptStaysInertAndFirstParty holds the two
// honest.
//
//go:embed gallery.js
var galleryJS []byte

// GalleryJS returns gallery.js's raw bytes. Exported for the same
// reason ui.ShimJS is: the gates read it, and a caller embedding this
// tree somewhere else needs the asset as well as the pages.
func GalleryJS() []byte { return append([]byte(nil), galleryJS...) }

// galleryCSS is the gallery's own stylesheet, embedded beside the
// script for the same two reasons: it is edited as CSS rather than as a
// Go string constant, and it is not part of the framework — no scaffold
// writes it and no app receives it.
//
// It was inlined into every page's <style> until v2.1, which cost most
// of eight kilobytes on each of them and roughly two megabytes over the
// tree. It is one asset at the tree root now, and its own header
// comment is the rest of the story.
//
//go:embed gallery.css
var galleryCSS []byte

// GalleryCSS returns gallery.css's raw bytes, for GalleryJS's reason.
//
// The bytes a page links are these bytes. That is the change v2.1 made
// and it is worth saying out loud: the inline version went through
// html/template's CSS sanitiser, which stripped the comments explaining
// the preview widget's scaling on the way to the page. A served file
// keeps them, so the stylesheet a reader can open is the stylesheet
// this package holds.
func GalleryCSS() []byte { return append([]byte(nil), galleryCSS...) }

// DefaultMount is where rastrillo.org serves this tree, and the mount
// path Render uses when a caller does not name one.
//
// Every URL the renderer emits — stylesheet, script, iframe, switcher,
// shell demo, back-link — is an absolute path under the mount it was
// rendered at.
//
// Absolute rather than relative because the CARLOS static edge serves a
// directory index at its slash-less URL as a 200 with no redirect:
// /components and /components/ both return the same document, but
// a relative href in it resolves against a different base on each, so
// the slash-less visit loaded no stylesheet and every link pointed one
// directory too high. That was the live bug. The cost is that a rendered
// tree only works at the mount path it was rendered for, which is why
// the mount is an argument rather than a constant: the site that
// publishes the tree says where it is publishing it.
//
// rastrillo.org published the tree at /design-system until the gallery
// became Rastrillo components; Redirects sends those addresses here.
const DefaultMount = "/components"

// Render builds the whole gallery tree in memory: path relative to
// the tree root → file content.
//
//	index.html                            day, en, assets at the tree root
//	<theme>/<locale>/index.html           the Overview, 36 times
//	<theme>/<locale>/icons.html           and one page per section beside
//	<theme>/<locale>/form.html            it: see pageKinds() in page.go,
//	<theme>/<locale>/shells.html          which is the whole of that list.
//	                                      One of those sections per family
//	                                      of components, so form.html has
//	                                      the text fields on it,
//	                                      choices-and-actions.html the
//	                                      choosers and closing rows, and
//	                                      display.html the display ones.
//	<theme>/<locale>/modal.html           36 modal demos, one per gallery
//	<theme>/<locale>/demo.html            the demo app's index, and
//	<theme>/<locale>/demo-dashboard.html  its three section pages beside it
//	<theme>/<locale>/demo-requests.html
//	<theme>/<locale>/demo-request.html
//	<theme>/<locale>/<page>/<group>.html  one preview document per example
//	<theme>/<locale>/shells/<shell>.html  one full-page demo per shell, and
//	<theme>/<locale>/shells/<shell>-page.html  a section page for the sidebar
//	                                      and console
//	<theme>/<locale>/shells/topbar-menu.html  the topbar's Menu page
//	tokens.<hash>.css theme-<theme>.<hash>.css shell.<hash>.css
//	                                      the stylesheets, once each
//	rastrillo.<hash>.js busy.<hash>.js shell.<hash>.js
//	                                      the framework's scripts
//	select.<hash>.js datetime.<hash>.js calendar.<hash>.js
//	                                      (calendar.js draws the month grid
//	                                      datetime.js opens)
//	gallery.<hash>.js gallery.<hash>.css  the gallery's own furniture,
//	                                      once each
//
// The assets are shared by every page rather than copied per theme, so
// the tree's size is the documents plus the library, twice. Each is
// written under its content-hashed name (see hashedNames), which every
// page links, so a cache can never hand a new page an old asset; and
// under its plain name, linked by no page, for hotlinks and the site's
// checks.
//
// mount is the URL path the tree will be served from — DefaultMount for
// rastrillo.org. Every link and asset URL in the output is an absolute
// path under it, so a tree rendered for one mount cannot be served at
// another.
func Render(mount string) (map[string][]byte, error) {
	mount, err := CleanMount(mount)
	if err != nil {
		return nil, err
	}
	for _, theme := range ui.ThemeNames() {
		if _, ok := ui.ThemeCSS(theme); !ok {
			return nil, fmt.Errorf("designsystem: no theme %q", theme)
		}
	}
	// Every asset twice. The hashed name is the only one a page links,
	// so a deploy can never pair new HTML with an old cached stylesheet
	// or script: a page asks for exactly the bytes it was rendered
	// against. The plain name is the same bytes, linked by no page, kept
	// for what is not a page of this tree: a reader hotlinking tokens.css
	// as the Getting Started page invites, and the website's own checks,
	// which look for tokens.css and theme-<theme>.css by name.
	out := map[string][]byte{}
	for name, body := range galleryAssets() {
		out[hashedNames()[name]] = body
		out[name] = body
	}

	for _, theme := range ui.ThemeNames() {
		for _, locale := range rastrillo.BaseLocales() {
			dir := theme + "/" + locale + "/"
			pages, err := renderGallery(mount, theme, locale)
			if err != nil {
				return nil, fmt.Errorf("designsystem: %s: %w", dir, err)
			}
			for file, page := range pages {
				if err := put(out, dir+file, page); err != nil {
					return nil, err
				}
			}
			modal, err := renderModal(mount, theme, locale)
			if err != nil {
				return nil, fmt.Errorf("designsystem: %s: %w", dir+"modal.html", err)
			}
			if err := put(out, dir+"modal.html", modal); err != nil {
				return nil, err
			}
			docs, err := renderDemo(mount, theme, locale)
			if err != nil {
				return nil, fmt.Errorf("designsystem: %s: %w", dir+"demo.html", err)
			}
			for name, doc := range docs {
				if err := put(out, dir+name, doc); err != nil {
					return nil, err
				}
			}
			for _, shell := range ui.LayoutNames() {
				docs, err := renderShell(mount, theme, locale, shell)
				if err != nil {
					return nil, fmt.Errorf("designsystem: %s: %w", dir+"shells/"+shell+".html", err)
				}
				for name, doc := range docs {
					if err := put(out, dir+"shells/"+name, doc); err != nil {
						return nil, err
					}
				}
			}
		}
	}

	// The tree root is the default theme in English again — day/en since
	// v2, and named through ui.ThemeNames() rather than spelled out, so
	// renaming the default theme moves the root with it. It used to be a
	// second render at a different depth, because every path on it needed
	// rewriting for the shallower directory; with absolute paths there is
	// nothing left to rewrite, so the two files are the same bytes and
	// saying so is more honest than a second call that could only ever
	// return them. Copied, not aliased: callers get one slice per path.
	rootPage := RootTheme() + "/en/index.html"
	nested, ok := out[rootPage]
	if !ok {
		return nil, fmt.Errorf("designsystem: index.html: no %s page to serve as the tree root", rootPage)
	}
	out["index.html"] = append([]byte(nil), nested...)
	return out, nil
}

// CleanMount validates and normalises a mount path: a rooted, cleaned,
// slash-less-at-the-end URL path. "/" is rejected rather than special-
// cased — the tree writes its assets beside its directories, so a tree
// mounted at the site root would put tokens.css at /tokens.css and
// collide with whatever the site already keeps there.
//
// It exists because dsgen is published surface and the mount reaches it
// from a command line: a caller who passes "components/" or
// "/components/" gets the tree they meant rather than 189 pages of
// broken links. It refuses a mount Redirects could not write a rule
// for (refuseRuleSyntax).
func CleanMount(mount string) (string, error) {
	if mount == "" {
		return "", fmt.Errorf("designsystem: empty mount path (want something like %q)", DefaultMount)
	}
	if !strings.HasPrefix(mount, "/") {
		mount = "/" + mount
	}
	if err := refuseRuleSyntax(mount); err != nil {
		return "", err
	}
	// path.Clean on a rooted path never leaves a "..": a climb past the
	// root collapses to "/", which the next line refuses. So there is no
	// separate check for one, and a check that cannot fire would only
	// suggest this function does more than it does.
	clean := path.Clean(mount)
	if clean == "/" {
		return "", fmt.Errorf("designsystem: mount path %q is the site root; the tree needs a directory of its own", mount)
	}
	return clean, nil
}

// put adds one file to the tree and refuses a second writer. The
// preview files share their directories with the pages and the shell
// demos, and a name one of them took from another would replace a
// document with no gate noticing.
func put(out map[string][]byte, name string, body []byte) error {
	if _, dup := out[name]; dup {
		return fmt.Errorf("designsystem: %s is produced twice", name)
	}
	out[name] = body
	return nil
}

// RootTheme is the theme the tree root serves: the first name
// ui.ThemeNames() reports, which is the same theme rastrillo new
// scaffolds by default. Exported so the gates can say "the root index is
// this theme's English page" without repeating the name.
func RootTheme() string { return ui.ThemeNames()[0] }

// translator binds T (and Tf, which ui.WithT sets to the same function)
// to one locale's framework catalog: the locale's own value, English if
// the locale is missing the key, and the key itself if English is too —
// the same three-step fallback rastrillo.Locales.T walks, minus the app
// layers a static page has none of.
func translator(locale string) func(string, ...any) string {
	catalogs := rastrillo.BaseCatalogs()
	own, en := catalogs[locale], catalogs["en"]
	return func(key string, args ...any) string {
		s, ok := own[key]
		if !ok {
			if s, ok = en[key]; !ok {
				return key
			}
		}
		return interpolate(s, args)
	}
}

// interpolate substitutes {name} placeholders from alternating
// name/value arguments. Reimplemented here rather than reached for in
// the root package, where the equivalent walk is unexported and takes a
// Locales receiver this package has no reason to build.
//
// A placeholder with no matching argument is left verbatim, for the
// reason locale.go gives: a translator's typo should show up in the page
// rather than silently delete half a sentence.
func interpolate(s string, args []any) string {
	if len(args) == 0 || !strings.Contains(s, "{") {
		return s
	}
	values := make(map[string]string, len(args)/2)
	for i := 0; i+1 < len(args); i += 2 {
		if name, ok := args[i].(string); ok {
			values[name] = fmt.Sprint(args[i+1])
		}
	}
	var b strings.Builder
	for {
		open := strings.Index(s, "{")
		if open < 0 {
			b.WriteString(s)
			return b.String()
		}
		shut := strings.Index(s[open:], "}")
		if shut < 0 {
			b.WriteString(s)
			return b.String()
		}
		shut += open
		if v, ok := values[s[open+1:shut]]; ok {
			b.WriteString(s[:open])
			b.WriteString(v)
		} else {
			b.WriteString(s[:shut+1])
		}
		s = s[shut+1:]
	}
}

// partialTree parses ui's whole partial set with T bound to one locale.
// A fresh tree per locale rather than one tree cloned per request: this
// runs 72 times at generate time — an index page and a modal demo each
// — so the Clone discipline ui.FuncsWith documents buys nothing here.
func partialTree(locale string) (*template.Template, error) {
	return template.New("designsystem").
		Funcs(galleryFuncs(locale)).
		ParseFS(ui.Templates(), "*.html")
}

// galleryFuncs is ui's own func map plus P, the gallery's own
// translator. T resolves the framework's catalog — the strings a
// component emits — and P resolves prose.go, the strings this page says
// in its own voice. Two functions rather than one table because the two
// sets have different owners: an app can override a rastrillo.ui.* key,
// and nothing outside this package has any business in prose.go.
func galleryFuncs(locale string) template.FuncMap {
	funcs := ui.Funcs(ui.WithT(translator(locale)), ui.WithLocale(locale))
	funcs["P"] = func(key string, args ...any) string { return proseIn(locale, key, args...) }
	// sample is localMarkup for a run of sample markup written into a
	// page template, where a sentence carries an element P would escape:
	// the shell demo's count line, with its two numbers in <strong>.
	funcs["sample"] = func(src string) template.HTML { return template.HTML(localMarkup(locale, src)) }
	return funcs
}

// partialNames lists the partials ui defines, sorted: the templates that
// carry a {{define}}, not the per-file templates ParseFS also creates.
// It is how the page discovers a partial nobody wrote a sample for — see
// buildFamilies' orphan sweep.
//
// Deliberately read off a tree holding nothing but ui's partials, not
// off the page's own tree. The page adds templates of its own (the page
// body, one per hand-written sample), and an orphan sweep over that tree
// would treat the page as a partial with no sample data and render it
// inside itself. It did, once.
func partialNames() ([]string, error) {
	tmpl, err := template.New("partials").Funcs(ui.Funcs()).ParseFS(ui.Templates(), "*.html")
	if err != nil {
		return nil, fmt.Errorf("parsing partials: %w", err)
	}
	var names []string
	for _, t := range tmpl.Templates() {
		name := t.Name()
		if name == "" || name == "partials" || strings.HasSuffix(name, ".html") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}
