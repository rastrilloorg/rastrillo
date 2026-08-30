package blogtest

import (
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
)

// populatedScreens renders every screen with content on it: eleven
// published posts (so the guarded pagination strip appears on both list
// screens, and /?page=2 is a real second page rather than an empty
// state), plus one draft so the edit screen has a neutral pill to show.
//
// The admin list/new/edit/show targets all render through the
// manifest system's own template tree now (task 10's adoption):
// posts/show stays fully generated, posts/list and posts/form are
// ejected app-owned files (task 11) rather than the old hand
// admin_list.html/admin_new.html/admin_edit.html.
func populatedScreens(t *testing.T) map[string]string {
	t.Helper()
	app, db := newApp(t)
	var published int64
	for i := 1; i <= 11; i++ {
		published = seed(t, db, fmt.Sprintf("Go post %02d", i), "First paragraph.\n\nSecond paragraph.", true)
	}
	draft := seed(t, db, "A draft to edit", "Body.", false)

	out := map[string]string{}
	for _, target := range []string{
		"/",
		"/?page=2",
		fmt.Sprintf("/posts/%d", published),
		"/admin/posts",
		"/admin/posts?q=go",
		"/admin/posts?q=zzz",
		"/admin/posts/new",
		fmt.Sprintf("/admin/posts/%d", published),
		fmt.Sprintf("/admin/posts/%d/edit", published),
		fmt.Sprintf("/admin/posts/%d/edit", draft),
	} {
		rec := get(t, app, target)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", target, rec.Code)
		}
		out[target] = rec.Body.String()
	}
	return out
}

// emptyScreens renders the two blank states.
func emptyScreens(t *testing.T) map[string]string {
	t.Helper()
	app, _ := newApp(t)
	out := map[string]string{}
	for _, target := range []string{"/", "/admin/posts"} {
		rec := get(t, app, target)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", target, rec.Code)
		}
		out["empty "+target] = rec.Body.String()
	}
	return out
}

func allScreens(t *testing.T) map[string]string {
	t.Helper()
	out := populatedScreens(t)
	for name, html := range emptyScreens(t) {
		out[name] = html
	}
	return out
}

// The acceptance criterion, made executable: all stock partials are used
// by the running app, not by a fixture. The marker strings are the ones
// ui_test.go's own smoke test uses.
func TestAllStockPartialsAppearAcrossTheApp(t *testing.T) {
	var combined strings.Builder
	for _, html := range allScreens(t) {
		combined.WriteString(html)
	}
	out := combined.String()

	markers := map[string]string{
		"page-header":        `<header rst-page-header>`,
		"list-bar":           `<div rst-lbar>`,
		"list-bar-search":    `<form rst-search`,
		"list-search-submit": `<button class="rst-sr-only" type="submit">`,
		"list-row-action":    `<div rst-row>`,
		"status-pill":        `<span rst-status`,
		"empty-state":        `<div rst-empty>`,
		"pagination":         `<nav rst-pagination`,
		"rst-form":           `<form rst-form`,
		"field-text":         `<input rst-input`,
		"field-textarea":     `<textarea rst-textarea`,
		"form-foot":          `<div rst-form-foot>`,
	}
	for name, marker := range markers {
		if !strings.Contains(out, marker) {
			t.Errorf("no screen rendered %s (%q)", name, marker)
		}
	}
}

func TestEveryScreenHasAPageHeaderAndATitle(t *testing.T) {
	titleRe := regexp.MustCompile(`<title>([^<]+)</title>`)
	for name, html := range allScreens(t) {
		// Every screen carries a page-header now, generated and
		// ejected alike: task 11 ejected posts/list and posts/form
		// (templates/posts/{list,form}.html) and added one to both
		// the New and Edit screens the (single) ejected form.html
		// covers — the exclusion this test used to need for them is
		// gone along with the reason for it.
		if !strings.Contains(html, `<header rst-page-header>`) {
			t.Errorf("%s has no page header", name)
		}
		m := titleRe.FindStringSubmatch(html)
		if m == nil || strings.TrimSpace(m[1]) == "" {
			t.Errorf("%s has no non-empty <title>", name)
		}
	}
}

func TestAdminListScreenCarriesItsStockComponents(t *testing.T) {
	screens := populatedScreens(t)
	html := screens["/admin/posts"]
	for _, want := range []string{
		`<header rst-page-header>`,
		`<div rst-lbar>`,
		`<form rst-search`,
		`<button class="rst-sr-only" type="submit">`,
		`<div rst-row>`,
		`<nav rst-pagination`,
	} {
		wantContains(t, html, want)
	}
}

// Zero JavaScript, and nothing the browser fetches from another origin.
// The layout's two stylesheet links are the app's only asset references,
// so unlike the library's own sweep this one permits <link and checks
// the target instead.
func TestNoScreenContainsScriptOrAnOffOriginReference(t *testing.T) {
	for name, html := range allScreens(t) {
		for _, bad := range []string{"<script", "<iframe", "onload=", "onclick=", "http://", "https://", "//cdn", "srcset", "@import"} {
			if strings.Contains(html, bad) {
				t.Errorf("%s reaches outside the page (%q)", name, bad)
			}
		}
		for _, attr := range []string{`href="`, `action="`} {
			for _, value := range attributeValues(html, attr) {
				if value == "" || !strings.ContainsAny(value[:1], "/?#") {
					t.Errorf("%s: %s%s…\" is not a same-origin reference", name, attr, value)
				}
			}
		}
	}
}

// attributeValues returns every value of one attribute in the HTML.
func attributeValues(html, attr string) []string {
	var out []string
	rest := html
	for {
		i := strings.Index(rest, attr)
		if i < 0 {
			return out
		}
		rest = rest[i+len(attr):]
		j := strings.Index(rest, `"`)
		if j < 0 {
			return out
		}
		out = append(out, rest[:j])
		rest = rest[j+1:]
	}
}

func TestBlogCSSIsSelfContained(t *testing.T) {
	css := readBlogCSS(t)
	for _, bad := range []string{"@import", "url(", "http://", "https://", "src:"} {
		if strings.Contains(css, bad) {
			t.Errorf("blog.css reaches outside the page (%q)", bad)
		}
	}
}

// blog.css never styles a library class: an example that shipped
// .rst-row { padding: … } would be teaching every reader to fork the
// design system on day one. Comments are stripped first, because the
// file's own header explains the rule by naming the prefix.
func TestBlogCSSStylesNoLibraryClass(t *testing.T) {
	css := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(readBlogCSS(t), "")
	if strings.Contains(css, ".rst-") {
		t.Errorf("blog.css contains a .rst- selector:\n%s", css)
	}
}

// Colours come from tokens, never literals — that is the whole mechanism
// by which these controls track the dark theme.
func TestBlogCSSUsesTokensNotLiterals(t *testing.T) {
	css := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(readBlogCSS(t), "")
	if m := regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`).FindString(css); m != "" {
		t.Errorf("blog.css contains a literal colour %q", m)
	}
}

func readBlogCSS(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../static/blog.css")
	if err != nil {
		t.Fatalf("read blog.css: %v", err)
	}
	return string(b)
}
