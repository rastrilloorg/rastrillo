// Package pdf is a deliberately minimal PDF writer, on the standard
// library alone: flow paragraphs, headings, field lists, callouts and
// tables (with pagination and repeating headers) onto A4 pages, and get
// back deterministic bytes.
//
// It exists because rastrillo apps are shipped as `carlos ship -kind
// binary|static` — one file — and the sidecar is the same binary
// re-invoked with different argv (sidecar.go). There is nowhere on this
// platform to run a headless browser, a headless office suite or a
// Python renderer, so an app that wants a PDF either writes one in
// process or does not have one. Writing the file by hand (see emit.go)
// means depending on nothing whose own dependency set includes a PDF
// *parser* — the usual shape of a Go PDF library, because most of them
// also read.
//
// # What makes this tractable
//
// The input is the small vocabulary of blocks a Builder exposes:
// headings, paragraphs, field lists, tables and callouts. There is no
// style cascade, no arbitrary nesting, no float and no text wrap around
// anything. That is deliberate and it is the whole difference between
// this and rendering a page description language built for arbitrary
// documents, which is an engineer-years problem. Reach for a real
// library — or a different design entirely — if an app needs more than
// this vocabulary gives it; this package covers what a report, an
// export or a confirmation needs and nothing fancier, the same bet
// package xlsx already makes for spreadsheets.
//
// # No embedded font, deliberately
//
// The three faces are PDF base-14: Helvetica, Helvetica-Bold and
// Helvetica-Oblique. Every conforming reader already has them, so
// nothing is embedded, there is no font licence to hold, and the file
// is small. The cost is honest: the output looks like Helvetica, and an
// app that wants a house typeface is asking for font embedding, which
// is further work and not a setting.
//
// Text is encoded as Windows-1252, which is what the base-14 faces are
// declared with here. A character outside that repertoire cannot be
// drawn, so [Encode] replaces it rather than emitting a byte that would
// render as something else — and it says which characters it dropped,
// so a caller can tell somebody rather than shipping a silently altered
// document.
//
// # Determinism
//
// Two renders of the same sequence of Builder calls with the same
// [Options] produce identical bytes. Nothing here reads the clock —
// the creation date comes from Options.Produced — and nothing iterates
// a map. That is what makes "render it again and compare" a check an
// app can actually run against a document it keeps for years.
//
// # Lineage
//
// Ported from ips's internal/report/pdf, where it renders a statutory
// report's own block tree — headings, field lists, tables, callouts —
// built against a page-at-a-time flow and the same base-14 metrics
// this package carries. That report-specific tree (its citation and
// verification model in particular) stayed behind, along with a UK/
// Canada cheque-printing layer built on top of the same engine: both are
// product- and jurisdiction-specific, not framework material. What
// moved is the part with no caller-specific meaning in it at all: the
// metrics, the text measuring and wrapping, the page-flow and
// pagination, and the hand-written PDF file format underneath it.
package pdf
