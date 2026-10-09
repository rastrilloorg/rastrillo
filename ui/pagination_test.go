package ui

import (
	"fmt"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
)

// stripItems builds Items the way the blog example does: Previous, the
// listed pages with a Gap between any two that are not adjacent, Next,
// the two steps marked with their Rel.
func stripItems(current int, shown ...int) []any {
	items := []any{map[string]any{"Label": "Previous", "Href": "/p", "Rel": "prev"}}
	if current == shown[0] {
		items[0] = map[string]any{"Label": "Previous", "Disabled": true, "Rel": "prev"}
	}
	for i, n := range shown {
		if i > 0 && n != shown[i-1]+1 {
			items = append(items, map[string]any{"Gap": true})
		}
		items = append(items, map[string]any{"Label": fmt.Sprint(n), "Href": fmt.Sprintf("/p?page=%d", n), "Current": n == current})
	}
	if current == shown[len(shown)-1] {
		return append(items, map[string]any{"Label": "Next", "Disabled": true, "Rel": "next"})
	}
	return append(items, map[string]any{"Label": "Next", "Href": "/n", "Rel": "next"})
}

// spell writes a strip as one screen shows it: narrow skips what only a
// wide strip shows and draws a step as a chevron; wide skips what only a
// narrow strip shows.
func spell(items []paginationItem, narrow bool) string {
	var parts []string
	for _, it := range items {
		switch {
		case narrow && it.Wide, !narrow && it.Narrow:
		case it.Gap:
			parts = append(parts, "…")
		case narrow && it.Step == "prev":
			parts = append(parts, "‹")
		case narrow && it.Step == "next":
			parts = append(parts, "›")
		default:
			parts = append(parts, fmt.Sprint(it.Label))
		}
	}
	return strings.Join(parts, " ")
}

// TestPaginationItemsMarkWhatAPhoneShows: a phone shows the first, the
// current and the last page between the two chevrons, with one
// ellipsis wherever pages are skipped, and a wide strip shows exactly
// what the app passed. The current page next to either end is where a
// rule written from the gaps alone draws an ellipsis between adjacent
// pages, or two in a row; a strip with no gaps at all (the generated
// list actions list every page) is where it draws none.
func TestPaginationItemsMarkWhatAPhoneShows(t *testing.T) {
	for _, c := range []struct {
		name         string
		items        []any
		wide, narrow string
	}{
		{"middle", stripItems(4, 1, 3, 4, 5, 9), "Previous 1 … 3 4 5 … 9 Next", "‹ 1 … 4 … 9 ›"},
		{"first", stripItems(1, 1, 2), "Previous 1 2 Next", "‹ 1 2 ›"},
		{"second", stripItems(2, 1, 2, 3, 9), "Previous 1 2 3 … 9 Next", "‹ 1 2 … 9 ›"},
		{"third", stripItems(3, 1, 2, 3, 4, 9), "Previous 1 2 3 4 … 9 Next", "‹ 1 … 3 … 9 ›"},
		{"next to last", stripItems(8, 1, 7, 8, 9), "Previous 1 … 7 8 9 Next", "‹ 1 … 8 9 ›"},
		{"last", stripItems(9, 1, 8, 9), "Previous 1 … 8 9 Next", "‹ 1 … 9 ›"},
		{"no gaps", stripItems(4, 1, 2, 3, 4, 5, 6, 7), "Previous 1 2 3 4 5 6 7 Next", "‹ 1 … 4 … 7 ›"},
		{"one page", stripItems(1, 1), "Previous 1 Next", "‹ 1 ›"},
		{"no first or last page", stripItems(5, 4, 5, 6), "Previous 4 5 6 Next", "‹ 4 5 6 ›"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := paginationItems(map[string]any{"Items": c.items})
			if w := spell(got, false); w != c.wide {
				t.Errorf("a wide strip shows %q, want %q", w, c.wide)
			}
			if n := spell(got, true); n != c.narrow {
				t.Errorf("a phone shows %q, want %q", n, c.narrow)
			}
		})
	}
}

// pageItem is the struct shape the blog example passes, Rel and all;
// oldItem is the same struct from before Rel existed.
type pageItem struct {
	Label, Href, Rel       string
	Current, Disabled, Gap bool
}

type oldItem struct {
	Label, Href            string
	Current, Disabled, Gap bool
}

// TestPaginationItemsReadEveryShape: Items arrive as dicts and as an
// app's own structs, with or without a Rel field. Only Rel makes a
// step: an item labelled "First" at the start, or "Previous" with no
// Rel, is not a chevron. A strip with no Rel at all renders on a phone
// as it always has, because nothing else tells its steps from its
// pages, and with no current page there is nothing to keep between the
// ends.
func TestPaginationItemsReadEveryShape(t *testing.T) {
	structs := []pageItem{{Label: "Back", Href: "/b", Rel: "prev"}, {Label: "1", Current: true}, {Label: "2", Href: "/2"}, {Label: "3", Href: "/3"}, {Label: "Forward", Href: "/f", Rel: "next"}}
	old := []oldItem{{Label: "Previous", Href: "/b"}, {Label: "1", Current: true}, {Label: "2", Href: "/2"}, {Label: "3", Href: "/3"}, {Label: "Next", Href: "/f"}}
	for _, c := range []struct {
		name         string
		data         any
		wide, narrow string
	}{
		{"structs", map[string]any{"Items": structs}, "Back 1 2 3 Forward", "‹ 1 … 3 ›"},
		{"struct data", struct{ Items []pageItem }{structs}, "Back 1 2 3 Forward", "‹ 1 … 3 ›"},
		{"structs without Rel", map[string]any{"Items": old}, "Previous 1 2 3 Next", "Previous 1 2 3 Next"},
		{"dicts without Rel", map[string]any{"Items": []any{
			map[string]any{"Label": "Previous", "Href": "/p"}, map[string]any{"Label": "1", "Href": "/1"}, map[string]any{"Gap": true},
			map[string]any{"Label": "4", "Current": true}, map[string]any{"Gap": true}, map[string]any{"Label": "9", "Href": "/9"}, map[string]any{"Label": "Next", "Href": "/n"},
		}}, "Previous 1 … 4 … 9 Next", "Previous 1 … 4 … 9 Next"},
		{"First is not a step", map[string]any{"Items": []any{
			map[string]any{"Label": "First", "Href": "/1"}, map[string]any{"Label": "Previous", "Href": "/3", "Rel": "prev"},
			map[string]any{"Label": "3", "Href": "/3"}, map[string]any{"Label": "4", "Current": true}, map[string]any{"Label": "5", "Href": "/5"},
			map[string]any{"Label": "Next", "Href": "/5", "Rel": "next"}, map[string]any{"Label": "Last", "Href": "/9"},
		}}, "First Previous 3 4 5 Next Last", "First ‹ … 4 … › Last"},
		{"an unknown Rel", map[string]any{"Items": []any{
			map[string]any{"Label": "First", "Href": "/1", "Rel": "first"}, map[string]any{"Label": "1", "Current": true}, map[string]any{"Label": "2", "Href": "/2"},
		}}, "First 1 2", "First 1 2"},
		{"no current page", map[string]any{"Items": stripItems(0, 1, 2, 3)}, "Previous 1 2 3 Next", "‹ 1 2 3 ›"},
		{"no items", map[string]any{}, "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := paginationItems(c.data)
			if w := spell(got, false); w != c.wide {
				t.Errorf("a wide strip shows %q, want %q", w, c.wide)
			}
			if n := spell(got, true); n != c.narrow {
				t.Errorf("a phone shows %q, want %q", n, c.narrow)
			}
		})
	}
}

// TestPaginationKeepsTheWordsOfAChevron: on a phone Previous and Next
// are drawn as chevrons, and the word stays in the markup for a screen
// reader, so no new string is needed and an app's translation keeps
// working. The links a phone hides and the ellipsis only it shows are
// marked, and a desktop renders every link it did before.
func TestPaginationKeepsTheWordsOfAChevron(t *testing.T) {
	got := render(t, "pagination", map[string]any{"Items": stripItems(4, 1, 2, 3, 4, 5, 6, 7)})
	for _, want := range []string{
		`<a href="/p" rst-pagination-step="prev">` + string(rastrillo.Icon("chevron-down")) + `<span rst-pagination-word>Previous</span></a>`,
		`<a href="/n" rst-pagination-step="next">` + string(rastrillo.Icon("chevron-down")) + `<span rst-pagination-word>Next</span></a>`,
		`<a href="/p?page=1">1</a>`,
		`<a href="/p?page=2" rst-pagination-wide>2</a>`,
		`<span aria-current="page">4</span>`,
		`<a href="/p?page=7">7</a>`,
		`<span rst-pagination-gap="narrow" aria-hidden="true">…</span>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	if n := strings.Count(got, `rst-pagination-gap="narrow"`); n != 2 {
		t.Errorf("%d ellipses for the phone, want 2: %s", n, got)
	}
	disabled := render(t, "pagination", map[string]any{"Items": stripItems(1, 1, 2)})
	if want := `<span rst-pagination-disabled rst-pagination-step="prev">` + string(rastrillo.Icon("chevron-down")) + `<span rst-pagination-word>Previous</span></span>`; !strings.Contains(disabled, want) {
		t.Errorf("missing %s in %s", want, disabled)
	}
}
