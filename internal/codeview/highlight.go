package codeview

import (
	"html/template"
	"strings"
)

// Highlight escapes source for an HTML text context and wraps five
// kinds of token in two-letter custom elements that the gallery's
// stylesheet colours: <ds-t> a tag's name with its opening bracket,
// <ds-a> an attribute name, <ds-v> an attribute value with its quotes,
// <ds-x> a template action's delimiters, keywords and fields, and <ds-s>
// a string literal inside an action.
//
// Custom elements rather than <span class>: a token costs 13 bytes this
// way and 29 the other, and on the heaviest page that difference was
// the page budget. An undefined element is inline with no role, so it
// changes nothing for assistive technology or for textContent.
//
// Only &, < and > are escaped. Quotes stay literal, which is valid in
// text and spares four bytes on every attribute a sample writes.
func Highlight(src string) template.HTML {
	var b strings.Builder
	for i := 0; i < len(src); {
		switch {
		case strings.HasPrefix(src[i:], "{{"):
			end := actionEnd(src, i)
			action(&b, src[i:end])
			i = end
		case src[i] == '<' && tagEnd(src, i) > 0:
			end := tagEnd(src, i)
			tag(&b, src[i:end])
			i = end
		default:
			j := i + 1
			for j < len(src) && !strings.HasPrefix(src[j:], "{{") && !(src[j] == '<' && tagEnd(src, j) > 0) {
				j++
			}
			escape(&b, src[i:j])
			i = j
		}
	}
	return template.HTML(b.String())
}

func escape(b *strings.Builder, s string) {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		default:
			b.WriteByte(s[i])
		}
	}
}

func wrapped(b *strings.Builder, kind, s string) {
	b.WriteString("<ds-" + kind + ">")
	escape(b, s)
	b.WriteString("</ds-" + kind + ">")
}

// tag writes one tag: its name, each attribute's name and value, and
// the whitespace and closing bracket between them as plain text.
func tag(b *strings.Builder, t string) {
	n := 1
	if strings.HasPrefix(t, "</") {
		n = 2
	}
	for n < len(t) && !isSpace(t[n]) && t[n] != '>' && t[n] != '/' {
		n++
	}
	wrapped(b, "t", t[:n])
	for i := n; i < len(t); {
		c := t[i]
		switch {
		case isSpace(c) || c == '/' || c == '>':
			escape(b, t[i:i+1])
			i++
		case c == '=':
			b.WriteByte('=')
			i++
			j := i
			if j < len(t) && (t[j] == '"' || t[j] == '\'') {
				q := t[j]
				j++
				for j < len(t) && t[j] != q {
					j++
				}
				if j < len(t) {
					j++
				}
			} else {
				for j < len(t) && !isSpace(t[j]) && t[j] != '>' {
					j++
				}
			}
			wrapped(b, "v", t[i:j])
			i = j
		default:
			j := i
			for j < len(t) && !isSpace(t[j]) && t[j] != '=' && t[j] != '>' && !(t[j] == '/' && j+1 < len(t) && t[j+1] == '>') {
				j++
			}
			wrapped(b, "a", t[i:j])
			i = j
		}
	}
}

// actionEnd returns the index just past the action starting at s[i],
// treating "}}" inside a string literal as part of the string.
func actionEnd(s string, i int) int {
	for j := i + 2; j < len(s); j++ {
		switch s[j] {
		case '"':
			for j++; j < len(s) && s[j] != '"'; j++ {
				if s[j] == '\\' {
					j++
				}
			}
		case '`':
			for j++; j < len(s) && s[j] != '`'; j++ {
			}
		case '}':
			if j+1 < len(s) && s[j+1] == '}' {
				return j + 2
			}
		}
	}
	return len(s)
}

// action writes one {{…}}: the delimiters, identifiers and .Fields as
// <ds-x>, string literals as <ds-s>, and everything else as text.
func action(b *strings.Builder, a string) {
	body, closed := strings.CutSuffix(a[2:], "}}")
	wrapped(b, "x", "{{")
	for i := 0; i < len(body); {
		c := body[i]
		switch {
		case c == '"' || c == '`':
			j := i + 1
			for j < len(body) && body[j] != c {
				if c == '"' && body[j] == '\\' {
					j++
				}
				j++
			}
			j = min(j+1, len(body))
			wrapped(b, "s", body[i:j])
			i = j
		case isLetter(c) || c == '_' || c == '.':
			j := i + 1
			for j < len(body) && (isLetter(body[j]) || body[j] == '_' || body[j] == '.' || body[j] >= '0' && body[j] <= '9') {
				j++
			}
			wrapped(b, "x", body[i:j])
			i = j
		default:
			escape(b, body[i:i+1])
			i++
		}
	}
	if closed {
		wrapped(b, "x", "}}")
	}
}
