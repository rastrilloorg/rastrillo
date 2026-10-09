// Package codeview lays out and colours the source a design-system
// gallery shows in its HTML and Template tabs. It knows nothing about
// the gallery: it takes HTML or template source and gives back the same
// text, broken into lines or marked up for colour, so that the bytes a
// reader copies are the bytes the source holds.
package codeview

import "strings"

// piece is one run of markup: a tag, a comment, or the text between.
type piece struct {
	text string
	kind pieceKind
	name string // lower-case element name, for a tag
	end  bool   // a closing tag
	void bool   // opens no element: a void element or a self-closed tag
}

type pieceKind int

const (
	textPiece pieceKind = iota
	tagPiece
	commentPiece
)

// voidElements open nothing, so they never deepen the indent.
var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true,
	"input": true, "link": true, "meta": true, "source": true, "track": true, "wbr": true,
}

// tagEnd returns the index just past the tag that starts at s[i], or -1
// when s[i] does not open one. A '>' inside a quoted attribute value
// does not close the tag: data-tip="a > b" is one tag and not two.
func tagEnd(s string, i int) int {
	j := i + 1
	if j < len(s) && s[j] == '/' {
		j++
	}
	if j >= len(s) || !isLetter(s[j]) {
		return -1
	}
	var quote byte
	for ; j < len(s); j++ {
		c := s[j]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '>':
			return j + 1
		}
	}
	return -1
}

// pieces splits markup into tags, comments and text. A '<' that opens
// no tag ("a < b") stays text.
func pieces(s string) []piece {
	var out []piece
	text := 0
	flush := func(at int) {
		if at > text {
			out = append(out, piece{text: s[text:at]})
		}
	}
	for i := 0; i < len(s); {
		if s[i] != '<' {
			i++
			continue
		}
		if strings.HasPrefix(s[i:], "<!--") {
			end := strings.Index(s[i+4:], "-->")
			if end < 0 {
				break
			}
			flush(i)
			j := i + 4 + end + 3
			out = append(out, piece{text: s[i:j], kind: commentPiece})
			i, text = j, j
			continue
		}
		j := tagEnd(s, i)
		if j < 0 {
			i++
			continue
		}
		flush(i)
		tag := s[i:j]
		closing := strings.HasPrefix(tag, "</")
		n := 1
		if closing {
			n = 2
		}
		k := n
		for k < len(tag) && !isSpace(tag[k]) && tag[k] != '>' && tag[k] != '/' {
			k++
		}
		name := strings.ToLower(tag[n:k])
		out = append(out, piece{
			text: tag, kind: tagPiece, name: name, end: closing,
			void: !closing && (voidElements[name] || strings.HasSuffix(tag, "/>")),
		})
		i, text = j, j
	}
	flush(len(s))
	return out
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }
