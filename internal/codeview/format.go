package codeview

import "strings"

// blockElements are the elements whose boxes are blocks: whitespace
// between two tags where one of these is involved renders as nothing,
// so a line break there moves no pixel. Everything else (a, span,
// button, label, input, svg) is inline or inline-block, where a space
// is a space a reader sees, and is never broken.
var blockElements = map[string]bool{
	"address": true, "article": true, "aside": true, "blockquote": true, "details": true,
	"dialog": true, "dd": true, "div": true, "dl": true, "dt": true, "fieldset": true,
	"figcaption": true, "figure": true, "footer": true, "form": true, "h1": true, "h2": true,
	"h3": true, "h4": true, "h5": true, "h6": true, "header": true, "hgroup": true, "hr": true,
	"legend": true, "li": true, "main": true, "nav": true, "ol": true, "p": true, "pre": true,
	"search": true, "section": true, "summary": true, "table": true, "tbody": true, "td": true,
	"tfoot": true, "th": true, "thead": true, "tr": true, "ul": true,
}

// rawElements keep every byte inside them. A <pre> or <textarea>
// renders its whitespace as written, script and style are not markup,
// and an svg's paths are drawing, not structure a reader reads.
var rawElements = map[string]bool{"pre": true, "textarea": true, "svg": true, "script": true, "style": true}

// Format lays rendered HTML out for reading: a line break and two
// spaces of indent per open element are inserted between two tags only
// where at least one is block-level, replacing whatever whitespace-only
// text sat there. Text, inline runs, attribute values and everything
// inside a raw element are left byte for byte. No column limit is
// promised: an inline run stays one line however long it is, and the
// page soft-wraps it instead.
func Format(html string) string {
	return layout(html, func(depth int) string { return "\n" + strings.Repeat("  ", depth) })
}

// layout walks the markup and writes brk(depth) wherever Format may
// break, and the original bytes everywhere else.
func layout(s string, brk func(depth int) string) string {
	ps := pieces(s)
	var b strings.Builder
	depth := 0
	var prev *piece // the last tag written, while only whitespace has followed it
	pending := ""
	for i := 0; i < len(ps); i++ {
		p := ps[i]
		if p.kind != tagPiece {
			if p.kind == textPiece && prev != nil && strings.TrimSpace(p.text) == "" {
				pending += p.text
				continue
			}
			b.WriteString(pending)
			b.WriteString(p.text)
			pending, prev = "", nil
			continue
		}
		if p.end {
			depth = max(depth-1, 0)
		}
		if prev != nil && (blockElements[prev.name] || blockElements[p.name]) {
			b.WriteString(brk(depth))
		} else {
			b.WriteString(pending)
		}
		pending = ""
		b.WriteString(p.text)
		last := p
		if !p.end && !p.void {
			if rawElements[p.name] {
				j := closing(ps, i)
				for k := i + 1; k <= j; k++ {
					b.WriteString(ps[k].text)
				}
				i, last = j, ps[j]
			} else {
				depth++
			}
		}
		prev = &last
	}
	b.WriteString(pending)
	return b.String()
}

// closing is the index of the tag that closes the raw element opened
// at ps[i], counting nested elements of the same name; the last piece
// when the markup never closes it.
func closing(ps []piece, i int) int {
	name, open := ps[i].name, 1
	for j := i + 1; j < len(ps); j++ {
		if ps[j].kind != tagPiece || ps[j].name != name {
			continue
		}
		if ps[j].end {
			open--
			if open == 0 {
				return j
			}
		} else if !ps[j].void {
			open++
		}
	}
	return len(ps) - 1
}
