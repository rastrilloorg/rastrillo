// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// Ported from html/template/url.go in Go 1.26.10 for Rastrillo's compiled
// templates; see LICENSE-go. Behaviour is unchanged.

//rastrillo:vendored go1.26.10 src/html/template/url.go

package tmplrt

import (
	"fmt"
	"strings"

	"amadan.net/rastrillo/rastrillo/internal/htmlctx"
)

// URLFilter returns its input unless it contains an unsafe scheme in which
// case it defangs the entire URL.
//
// Schemes that cause unintended side effects that are irreversible without user
// interaction are considered unsafe. For example, clicking on a "javascript:"
// link can immediately trigger JavaScript code execution.
//
// This filter conservatively assumes that all schemes other than the following
// are unsafe:
//   - http:   Navigates to a new website, and may open a new window or tab.
//     These side effects can be reversed by navigating back to the
//     previous website, or closing the window or tab. No irreversible
//     changes will take place without further user interaction with
//     the new website.
//   - https:  Same as http.
//   - mailto: Opens an email program and starts a new draft. This side effect
//     is not irreversible until the user explicitly clicks send; it
//     can be undone by closing the email program.
//
// To allow URLs containing other schemes to bypass this filter, developers must
// explicitly indicate that such a URL is expected and safe by encapsulating it
// in a template.URL value.
func URLFilter(args ...any) string {
	s, t := stringify(args...)
	if t == htmlctx.ContentTypeURL {
		return s
	}
	if !isSafeURL(s) {
		return "#" + filterFailsafe
	}
	return s
}

// isSafeURL is true if s is a relative URL or if URL has a protocol in
// (http, https, mailto).
func isSafeURL(s string) bool {
	if protocol, _, ok := strings.Cut(s, ":"); ok && !strings.Contains(protocol, "/") {
		if !strings.EqualFold(protocol, "http") && !strings.EqualFold(protocol, "https") && !strings.EqualFold(protocol, "mailto") {
			return false
		}
	}
	return true
}

// URLEscaper produces an output that can be embedded in a URL query.
// The output can be embedded in an HTML attribute without further escaping.
func URLEscaper(args ...any) string {
	return urlProcessor(false, args...)
}

// URLNormalizer normalizes URL content so it can be embedded in a quote-delimited
// string or parenthesis delimited url(...).
// The normalizer does not encode all HTML specials. Specifically, it does not
// encode '&' so correct embedding in an HTML attribute requires escaping of
// '&' to '&amp;'.
func URLNormalizer(args ...any) string {
	return urlProcessor(true, args...)
}

// urlProcessor normalizes (when norm is true) or escapes its input to produce
// a valid hierarchical or opaque URL part.
func urlProcessor(norm bool, args ...any) string {
	s, t := stringify(args...)
	if t == htmlctx.ContentTypeURL {
		norm = true
	}
	var b strings.Builder
	if processURLOnto(s, norm, &b) {
		return b.String()
	}
	return s
}

// urlByteEncoded reports whether processURLOnto encodes s[i]. This is the
// loop's switch lifted out unchanged so that fast.go asks the same question
// instead of keeping a copy; it is the one deviation from a verbatim port.
func urlByteEncoded(s string, i int, norm bool) bool {
	c := s[i]
	switch c {
	// Single quote and parens are sub-delims in RFC 3986, but we
	// escape them so the output can be embedded in single
	// quoted attributes and unquoted CSS url(...) constructs.
	// Single quotes are reserved in URLs, but are only used in
	// the obsolete "mark" rule in an appendix in RFC 3986
	// so can be safely encoded.
	case '!', '#', '$', '&', '*', '+', ',', '/', ':', ';', '=', '?', '@', '[', ']':
		if norm {
			return false
		}
	// Unreserved according to RFC 3986 sec 2.3
	// "For consistency, percent-encoded octets in the ranges of
	// ALPHA (%41-%5A and %61-%7A), DIGIT (%30-%39), hyphen (%2D),
	// period (%2E), underscore (%5F), or tilde (%7E) should not be
	// created by URI producers
	case '-', '.', '_', '~':
		return false
	case '%':
		// When normalizing do not re-encode valid escapes.
		if norm && i+2 < len(s) && htmlctx.IsHex(s[i+1]) && htmlctx.IsHex(s[i+2]) {
			return false
		}
	default:
		// Unreserved according to RFC 3986 sec 2.3
		if 'a' <= c && c <= 'z' {
			return false
		}
		if 'A' <= c && c <= 'Z' {
			return false
		}
		if '0' <= c && c <= '9' {
			return false
		}
	}
	return true
}

// processURLOnto appends a normalized URL corresponding to its input to b
// and reports whether the appended content differs from s.
func processURLOnto(s string, norm bool, b *strings.Builder) bool {
	b.Grow(len(s) + 16)
	written := 0
	// The byte loop below assumes that all URLs use UTF-8 as the
	// content-encoding. This is similar to the URI to IRI encoding scheme
	// defined in section 3.1 of  RFC 3987, and behaves the same as the
	// EcmaScript builtin encodeURIComponent.
	// It should not cause any misencoding of URLs in pages with
	// Content-type: text/html;charset=UTF-8.
	for i, n := 0, len(s); i < n; i++ {
		c := s[i]
		if !urlByteEncoded(s, i, norm) {
			continue
		}
		b.WriteString(s[written:i])
		fmt.Fprintf(b, "%%%02x", c)
		written = i + 1
	}
	b.WriteString(s[written:])
	return written != 0
}

// Filters and normalizes srcset values which are comma separated
// URLs followed by metadata.
func SrcsetEscaper(args ...any) string {
	s, t := stringify(args...)
	switch t {
	case htmlctx.ContentTypeSrcset:
		return s
	case htmlctx.ContentTypeURL:
		// Normalizing gets rid of all HTML whitespace
		// which separate the image URL from its metadata.
		var b strings.Builder
		if processURLOnto(s, true, &b) {
			s = b.String()
		}
		// Additionally, commas separate one source from another.
		return strings.ReplaceAll(s, ",", "%2c")
	}

	var b strings.Builder
	written := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			filterSrcsetElement(s, written, i, &b)
			b.WriteString(",")
			written = i + 1
		}
	}
	filterSrcsetElement(s, written, len(s), &b)
	return b.String()
}

// Derived from https://play.golang.org/p/Dhmj7FORT5
const htmlSpaceAndASCIIAlnumBytes = "\x00\x36\x00\x00\x01\x00\xff\x03\xfe\xff\xff\x07\xfe\xff\xff\x07"

// isHTMLSpace is true iff c is a whitespace character per
// https://infra.spec.whatwg.org/#ascii-whitespace
func isHTMLSpace(c byte) bool {
	return (c <= 0x20) && (htmlSpaceAndASCIIAlnumBytes[c>>3]&(1<<uint(c&0x7))) != 0
}

func isHTMLSpaceOrASCIIAlnum(c byte) bool {
	return (c < 0x80) && (htmlSpaceAndASCIIAlnumBytes[c>>3]&(1<<uint(c&0x7))) != 0
}

func filterSrcsetElement(s string, left int, right int, b *strings.Builder) {
	start := left
	for start < right && isHTMLSpace(s[start]) {
		start++
	}
	end := right
	for i := start; i < right; i++ {
		if isHTMLSpace(s[i]) {
			end = i
			break
		}
	}
	if url := s[start:end]; isSafeURL(url) {
		// If image metadata is only spaces or alnums then
		// we don't need to URL normalize it.
		metadataOk := true
		for i := end; i < right; i++ {
			if !isHTMLSpaceOrASCIIAlnum(s[i]) {
				metadataOk = false
				break
			}
		}
		if metadataOk {
			b.WriteString(s[left:start])
			processURLOnto(url, true, b)
			b.WriteString(s[end:right])
			return
		}
	}
	b.WriteString("#")
	b.WriteString(filterFailsafe)
}
