package blogtest

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"blog/internal/blog"
)

func renderPage(t *testing.T, page string, data any) string {
	t.Helper()
	rec := httptest.NewRecorder()
	blog.Render(nil, rec, page, 200, data)
	if rec.Code != 200 {
		t.Fatalf("Render(%q) = %d, want 200:\n%s", page, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// Every page file defines "content" — the same name in every file.
// Parsed into one shared tree they would overwrite each other silently,
// last file wins, and the app would render the wrong screen with no
// error anywhere. This is the test that catches a lost clone.
//
// "posts/form" stands in for the old "admin_new" page here: the
// manifest adoption (task 10) replaced the hand admin_new.html/
// admin_edit.html pages (and their New/Edit actions) with the
// generated form — see genrender.go's genPages tree, keyed by
// "<resource>/<page>" rather than a bare basename. Any struct shaped
// like the generated formView renders it: html/template resolves
// fields by name via reflection, so this local, unexported literal
// works exactly like the real (also unexported, and per-action-file)
// formView the generated actions actually pass.
func TestEachPageRendersItsOwnContent(t *testing.T) {
	index := renderPage(t, "index", blog.HomeView{Head: blog.Head{Title: "The blog"}})
	post := renderPage(t, "post", blog.PostView{
		Head: blog.Head{Title: "Release notes"}, Title: "Release notes",
		Date: "Published 2 August 2026", Paragraphs: []string{"One."},
	})
	form := renderPage(t, "posts/form", struct {
		IsNew  bool
		Fields map[string]string
		Errors map[string]string
	}{IsNew: true, Fields: map[string]string{"Title": "", "Body": ""}})

	wantContains(t, index, "Notes, in the order they were written.")
	wantContains(t, post, `<article class="blog-article">`)
	wantNotContains(t, post, "Notes, in the order they were written.")
	wantContains(t, form, `<form class="rst-form" method="post" action="/admin/posts">`)
	wantNotContains(t, form, `<article class="blog-article">`)
}

func TestLayoutWrapsEveryPage(t *testing.T) {
	html := renderPage(t, "index", blog.HomeView{Head: blog.Head{Title: "The blog"}})

	wantContains(t, html, `<html lang="en">`)
	wantContains(t, html, `<title>The blog · The blog</title>`)
	// Stylesheet hrefs are fingerprinted ({{asset ...}}), so pin the
	// shape, not a literal hash that changes with every CSS edit.
	for _, re := range []string{
		`<link rel="stylesheet" href="/static/tokens\.[0-9a-f]{16}\.css">`,
		`<link rel="stylesheet" href="/static/blog\.[0-9a-f]{16}\.css">`,
	} {
		if !regexp.MustCompile(re).MatchString(html) {
			t.Errorf("missing a stylesheet link matching %s in:\n%s", re, html)
		}
	}
	wantContains(t, html, `<div class="rst-page">`)
	wantContains(t, html, `<footer class="blog-footer">`)
	wantNotContains(t, html, "<script")
}

// A template error is a clean 500, not half a page followed by garbage:
// Render executes into a buffer before it writes anything.
func TestRenderOfAnUnknownPageIs500WithNoPartialOutput(t *testing.T) {
	rec := httptest.NewRecorder()
	blog.Render(nil, rec, "no-such-page", 200, nil)

	if rec.Code != 500 {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	wantNotContains(t, rec.Body.String(), "<html")
}

func TestParagraphsSplitsOnBlankLines(t *testing.T) {
	got := blog.Paragraphs("One.\n\nTwo.\r\n\r\nThree.\n\n\n")
	want := []string{"One.", "Two.", "Three."}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("Paragraphs = %q, want %q", got, want)
	}
	if len(blog.Paragraphs("   ")) != 0 {
		t.Errorf("a blank body should produce no paragraphs")
	}
}

func TestMetaLinesFormatDatesInGo(t *testing.T) {
	when := time.Date(2026, 8, 2, 9, 30, 0, 0, time.UTC)
	if got, want := blog.FormatDate(when), "2 August 2026"; got != want {
		t.Errorf("FormatDate = %q, want %q", got, want)
	}
	if got, want := blog.PublishedLine(when), "Published 2 August 2026"; got != want {
		t.Errorf("PublishedLine = %q, want %q", got, want)
	}
	if got, want := blog.DraftLine(when), "Edited 2 August 2026"; got != want {
		t.Errorf("DraftLine = %q, want %q", got, want)
	}
}

func TestAdminRowsGiveDraftsNoActionPill(t *testing.T) {
	when := time.Date(2026, 8, 2, 9, 30, 0, 0, time.UTC)
	rows := blog.AdminRows([]blog.Post{
		{ID: 1, Title: "Live", Published: true, CreatedAt: when, UpdatedAt: when},
		{ID: 2, Title: "Draft", Published: false, CreatedAt: when, UpdatedAt: when},
	})

	if rows[0].ActionHref != "/posts/1" || rows[0].ActionAria != "View Live" {
		t.Errorf("published row = %+v", rows[0])
	}
	if rows[1].ActionHref != "" {
		t.Errorf("a draft has no public page, so it gets no action pill: %+v", rows[1])
	}
	if rows[1].Href != "/admin/posts/2/edit" {
		t.Errorf("the row goes where the work is: %+v", rows[1])
	}
	// The lead marker stays unused: tinting an aria-hidden circle by
	// publish state would be colour-only status.
	for _, row := range rows {
		if strings.Contains(row.Sub, "aria") {
			t.Errorf("unexpected markup in a Sub line: %q", row.Sub)
		}
	}
}

func TestEditFormResolvesTheStatusPillPair(t *testing.T) {
	draft := blog.EditForm(blog.Post{ID: 7, Title: "Draft"})
	if draft.StatusTone != "neutral" || draft.StatusLabel != "Draft" {
		t.Errorf("draft pill = %q/%q", draft.StatusTone, draft.StatusLabel)
	}
	if draft.Action != "/admin/posts/7" || draft.Sub != "Editing" {
		t.Errorf("edit view = %+v", draft)
	}

	live := blog.EditForm(blog.Post{ID: 7, Title: "Live", Published: true})
	if live.StatusTone != "positive" || live.StatusLabel != "Published" {
		t.Errorf("published pill = %q/%q", live.StatusTone, live.StatusLabel)
	}
}

func TestPaginationIsHiddenAtOnePageAndShownBeyondIt(t *testing.T) {
	if got := blog.BuildPagination("/admin/posts", "", "", 1, 10); got.Show {
		t.Errorf("ten posts is one page; the strip must stay away")
	}
	got := blog.BuildPagination("/admin/posts", "", "", 1, 11)
	if !got.Show {
		t.Fatalf("eleven posts is two pages; the strip must appear")
	}
	want := []blog.PageItem{
		{Label: "Previous", Disabled: true},
		{Label: "1", Current: true},
		{Label: "2", Href: "/admin/posts?page=2"},
		{Label: "Next", Href: "/admin/posts?page=2"},
	}
	if len(got.Items) != len(want) {
		t.Fatalf("items = %+v, want %+v", got.Items, want)
	}
	for i := range want {
		if got.Items[i] != want[i] {
			t.Errorf("item %d = %+v, want %+v", i, got.Items[i], want[i])
		}
	}
}

func TestPaginationCarriesTheQueryAndDisablesNextOnTheLastPage(t *testing.T) {
	got := blog.BuildPagination("/admin/posts", "go", "", 2, 11)
	if got.Items[0] != (blog.PageItem{Label: "Previous", Href: "/admin/posts?q=go&page=1"}) {
		t.Errorf("Previous = %+v", got.Items[0])
	}
	last := got.Items[len(got.Items)-1]
	if last != (blog.PageItem{Label: "Next", Disabled: true}) {
		t.Errorf("Next on the last page = %+v", last)
	}
}

// A non-empty status must flow through BuildPagination too, in order
// after q and before page — the admin list's own filter dropdown href
// also contains "q=Note" and "status=draft", so an assertion on the
// full page string couldn't tell the two apart; this one reaches
// BuildPagination directly.
func TestPaginationCarriesTheStatusFilterAfterTheQuery(t *testing.T) {
	got := blog.BuildPagination("/admin/posts", "Note", "draft", 1, blog.PageSize+1)
	next := got.Items[len(got.Items)-1]
	if next != (blog.PageItem{Label: "Next", Href: "/admin/posts?q=Note&status=draft&page=2"}) {
		t.Errorf("Next = %+v", next)
	}
}

// The admin list's integration tests only ever exercise a plain search
// (q, no status) or a plain filter (status, no q); this test pins the
// combined wording directly, plus the filter-only case's curly quotes.
func TestNoMatchNotePinsTheCombinedQueryAndStatusWording(t *testing.T) {
	if got, want := blog.NoMatchNote("x", "draft"), "No drafts match “x”."; got != want {
		t.Errorf("NoMatchNote(%q, %q) = %q, want %q", "x", "draft", got, want)
	}
	if got, want := blog.NoMatchNote("", "published"), "No published posts yet."; got != want {
		t.Errorf("NoMatchNote(%q, %q) = %q, want %q", "", "published", got, want)
	}
}

// A gap needs 71 posts to appear, so the app builds it correctly and
// never renders it — the library's own fixtures cover that item kind.
func TestPaginationWindowsWithGapsPastSevenPages(t *testing.T) {
	got := blog.BuildPagination("/", "", "", 5, 100) // ten pages
	var labels []string
	gaps := 0
	for _, item := range got.Items {
		if item.Gap {
			gaps++
			labels = append(labels, "…")
			continue
		}
		labels = append(labels, item.Label)
	}
	if gaps != 2 {
		t.Errorf("items = %v, want two gaps", labels)
	}
	if strings.Join(labels, " ") != "Previous 1 … 4 5 6 … 10 Next" {
		t.Errorf("items = %q", strings.Join(labels, " "))
	}
}
