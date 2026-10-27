# 🤖 pdf

`amadan.net/rastrillo/rastrillo/pdf`

Write a multi-page PDF — headings, paragraphs, field lists, tables and
callouts, with pagination and repeating table headers — using only the
standard library. No sidecar, no headless browser, no system font:
every byte is written by this package, onto A4, in the three PDF
base-14 faces every conforming reader already has.

## Building a document

```go
type Builder struct{ /* unexported fields */ }

func New(opt Options) *Builder

func (b *Builder) Heading(level int, text string)
func (b *Builder) Paragraph(text string)
func (b *Builder) Fields(title string, fields []Field)
func (b *Builder) Table(title string, columns []Column, rows [][]Cell, total []Cell)
func (b *Builder) List(title string, items []string)
func (b *Builder) Callout(title, text string, strong bool)
func (b *Builder) PageBreak()
func (b *Builder) KeepTogether(fn func(b *Builder))
func (b *Builder) Produced(by, at, note string)
func (b *Builder) Render() ([]byte, []rune, error)
```

```go
b := pdf.New(pdf.Options{
	Produced: time.Now(),
	Title:    "Statement",
	Author:   "Example Ltd",
})
b.Heading(1, "Account summary")
b.Fields("", []pdf.Field{
	{Label: "Account", Value: "0001234"},
	{Label: "Period", Value: "March 2026"},
})
b.Table("Transactions",
	[]pdf.Column{{Title: "Date"}, {Title: "Amount", Align: "right"}},
	rows, nil)

bs, dropped, err := b.Render()
```

Call a `Builder`'s methods in the order the page should read; each one
flows on from where the one before it left off, starting a fresh page
when the current one runs out of room. Call `Render` once, at the end.

`Fields`, `Table` and `List` take an empty `title` to omit the heading
above them. A `Table` row, or its `total`, may be longer than fits on
one page: both continue onto further pages, with the column headings
repeated at the top of each, and `total` renders apart from the body so
it cannot be misread as another row. `Callout` boxes a statement behind
a left bar; `strong` draws the bar heavier and the text in italic, for
whichever of a caller's own two levels of emphasis this is.

```go
type Column struct {
	Title string
	Align string // "left" (default) or "right"
	Width float64
}

type Cell struct {
	Text string
	Bold bool
}

type Field struct {
	Label  string
	Value  string
	Source string
}
```

A `Column`'s `Width` is its share of the table relative to the other
columns' — a zero takes an equal share of what the rest leave, so a
table with no widths at all still renders sensibly. A negative,
infinite or absurdly large `Width` is treated the same as a zero,
rather than reaching the page: there is no sensible share for a
weight like that to claim, and letting one through would put a
figure outside PDF's own numeric range into the file. `Field.Source`,
left non-empty, prints a small "Recorded by …" line under the value —
for a value that is somebody's own assertion rather than the caller's.

A `Column` with an empty `Title` is blank padding, not a real column —
useful to keep a narrow right-aligned figure from being stretched out
to the margin. A table's own rules (under its header, and under and
double-under its total) stop at the right edge of the last column that
does have a title, rather than running on into that padding; a table
where every column is titled rules to the margin exactly as before.
A `Title` that wraps to more than a few lines is cut short with an
ellipsis: a table's header has to appear whole, above its data, on
every page it repeats on, so there is no "paginate it normally" answer
once one wraps to more lines than a page can hold above even one row.

## Keeping a heading with what follows it

```go
func (b *Builder) KeepTogether(fn func(b *Builder))
```

```go
b.KeepTogether(func(b *pdf.Builder) {
	b.Heading(1, "Balance Sheet")
	b.Paragraph("As at 5 April 2026.")
	b.Table("", columns, rows, total)
})
```

Without this, a heading only ever reserves room for its own height, so
one can land as the last line on a page with its table orphaned onto
the next. `KeepTogether` lays out `fn`'s calls — a heading and what
always follows it, say — so that if they would not fit in the room
left on the current page, a page break happens before any of `fn`'s
own output is drawn.

`fn` runs exactly once, against a throwaway `Builder` starting at the
top of a blank page, never the real one directly. That run is the
answer: if it fit on its one fresh page, its output is copied onto the
real page wherever that currently is; if it did not — either because
the real page has less room left than the fresh one did, or because
`fn` is longer than any single page holds — the real page turns over
and adopts that run's output outright, because turning over puts it in
exactly the state the throwaway one started from. Either way `fn`'s
own pagination is untouched: a long table inside it still breaks
across further pages with its header repeated, exactly as it would
with no `KeepTogether` at all.

`fn` must draw only on the `Builder` it is handed, never the one
`KeepTogether` was called on — easy to get backwards, since the
natural-looking mistake is a closure that closes over the outer one
instead of using its own parameter. That mistake is checked for and
panics naming the parameter, rather than silently measuring nothing
while the real page is drawn on with no room check at all.

## Keeping a line with the end of the document

```go
func (b *Builder) Produced(by, at, note string)
```

```go
b.Produced("Alex Morgan", "24 August 2026", "Draft — not issued")
```

`Produced` records who produced the document and when, as a short line
— "Produced by Alex Morgan on 24 August 2026." — with `note` (left
empty for none) printed beneath it in bold. It draws into the last
page's existing footer area at `Render` time, rather than flowing as an
ordinary block, so it can never end up alone at the top of a trailing
page holding nothing else: the same failure `KeepTogether` exists to
stop a heading having, reached from the tail of the document instead
of partway through it. Call it at most once; a later call replaces the
line rather than adding a second one.

## Determinism and the page footer

```go
type Options struct {
	Produced       time.Time
	Title          string
	Reference      string
	Author         string
	Stamp          *Stamp
	NoRunningTitle bool
	Landscape      bool
	Size           Size
}

type Stamp struct {
	Head string
	Line string
}
```

Two renders built from the same calls with the same `Options` produce
identical bytes: nothing in this package reads the clock or iterates a
map. `Produced` stamps the file's creation and modification dates in
its metadata. `Title` and, if set, `Reference` print on every page as a
running footer — "Title — Reference" beside "Page n of N" — so a reader
holding a loose sheet can tell what it belongs to and whether one is
missing; `NoRunningTitle` leaves that label off while the page numbers
stay. `Stamp`, left nil, draws nothing; set, it lays `Head` (large, red)
and `Line` (small, beneath it) over every page — an approval mark, in
words, with no signature. `Landscape` turns the page on its side —
margins and type stay the same, only the page's own width and height
swap — for a table with more columns than a portrait page can hold at
a readable size.

## Page size

```go
type Size struct{ W, H float64 }

var (
	A4      = Size{595.28, 841.89}
	A3      = Size{841.89, 1190.55}
	A5      = Size{419.53, 595.28}
	Letter  = Size{612, 792}
	Legal   = Size{612, 1008}
	Tabloid = Size{792, 1224}
)
```

```go
b := pdf.New(pdf.Options{Size: pdf.Letter})
```

`Options.Size` is the page size, in points, portrait orientation; the
zero value is `A4`, so a caller that never sets it gets A4 without
naming it. `Landscape` swaps `W` and `H` of whichever `Size` was
chosen, so a landscape Letter page is `Options{Size: pdf.Letter,
Landscape: true}` — there is no separate landscape constant for each
size.

Nothing about this package requires one of the named sizes: a `Size`
is just a rectangle, so a caller can supply their own dimensions for
anything else a document needs to be printed on, such as a pre-printed
form. A `Size` that cannot hold a page falls back to `A4`, turned the
same way if `Landscape` is set. That covers the zero value, a negative
or non-finite side, a side longer than 14,400pt (200in, the most a PDF
reader accepts), and a page too short to hold a four-line table header
and one row once the margins are taken off. The check runs after
`Landscape` turns the page.

## What can't be drawn

```go
func (b *Builder) Render() ([]byte, []rune, error)
```

The three faces this package draws with are PDF base-14 — Helvetica,
Helvetica-Bold, Helvetica-Oblique — encoded as Windows-1252, so nothing
is embedded and the file stays small. A character outside that
encoding cannot be drawn: `Render`'s second return is every rune this
was asked to draw and could not, across the whole document, deduplicated
and sorted. An empty slice means every such character was either drawn
or, for a handful of close lookalikes documented under `Encode` below,
deliberately drawn as something else; a caller that ships the bytes
without checking it is shipping a character the reader cannot see
drawn as a question mark instead.

## Measuring without drawing

```go
type Face int

const (
	Regular Face = Face(faceRegular)
	Bold    Face = Face(faceBold)
	Italic  Face = Face(faceItalic)
)

func Encode(s string) ([]byte, []rune)
func Measure(s string, f Face, size float64) float64
func Wrap(s string, f Face, size, max float64) []string
```

For drawing onto something this package does not lay out itself — a
blank form somebody else published, a fixed-position label — `Measure`
and `Wrap` use the same base-14 metrics `Builder` draws with, so a
value that fits in one place fits in the other: there is exactly one
right answer to "how wide is this word", and this package carries it
once. `Wrap` cuts a single token too long for a line of its own where
it stops fitting, by rune — except a single rune wider than `max`
itself, which still gets a line of its own even though that line does
not fit either, since there is no narrower way to draw it.

`Encode` is the same Windows-1252 conversion `Render` uses internally,
exposed for the same reason `Render` returns its dropped runes: a
caller that measures a string has to measure the bytes it will
actually draw, and has to be able to tell when one of them cannot be.
A handful of characters with no base-14 glyph of their own are drawn as
something else instead, close enough in appearance that this does not
count as a loss and is not reported: a non-breaking space and its
narrower relatives as a plain space, a soft hyphen as nothing, a
non-breaking hyphen or minus sign as a plain hyphen, and the prime
marks as plain quotes. A tab, line feed or carriage return is drawn as
a space, and any other control character below U+0020 is dropped. A
caller comparing drawn text back against its own source has to know
these do not round-trip.
