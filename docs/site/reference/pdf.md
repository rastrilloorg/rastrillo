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
table with no widths at all still renders sensibly. `Field.Source`,
left non-empty, prints a small "Recorded by …" line under the value —
for a value that is somebody's own assertion rather than the caller's.

A `Column` with an empty `Title` is blank padding, not a real column —
useful to keep a narrow right-aligned figure from being stretched out
to the margin. A table's own rules (under its header, and under and
double-under its total) stop at the right edge of the last column that
does have a title, rather than running on into that padding; a table
where every column is titled rules to the margin exactly as before.

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
left on the current page, a page break happens FIRST, before any of
`fn`'s own output is drawn.

How much room they need is measured, not guessed: `fn` runs once
already, against a throwaway page, to find out — which is also why
`fn` must be a pure function of the `Builder` it is handed (no state
outside that parameter, no side effect that should not happen twice).
A group longer than one page costs exactly one page's worth, which is
the only question left once there is no page it would entirely fit on;
its own pagination — a long table's repeating header, say — carries on
exactly as it would with no `KeepTogether` at all.

## Keeping a line with the end of the document

```go
func (b *Builder) Produced(by, at, note string)
```

```go
b.Produced("A Practitioner", "24 August 2026", "Draft — not issued")
```

`Produced` records who produced the document and when, as a short line
— "Produced by A Practitioner on 24 August 2026." — with `note` (left
empty for none) printed beneath it in bold. It draws into the LAST
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

## What can't be drawn

```go
func (b *Builder) Render() ([]byte, []rune, error)
```

The three faces this package draws with are PDF base-14 — Helvetica,
Helvetica-Bold, Helvetica-Oblique — encoded as Windows-1252, so nothing
is embedded and the file stays small. A character outside that
encoding cannot be drawn: `Render`'s second return is every rune this
was asked to draw and could not, across the whole document, deduplicated
and sorted. An empty slice means every character drew; a caller that
ships the bytes without checking it is shipping a silent substitution.

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
it stops fitting, by rune, so every line it returns really does fit.
`Encode` is the same Windows-1252 conversion `Render` uses internally,
exposed for the same reason `Render` returns its dropped runes: a
caller that measures a string has to measure the bytes it will
actually draw, and has to be able to tell when one of them cannot be.
