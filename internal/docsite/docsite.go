// Package docsite loads the documentation corpus under docs/site and
// exposes it in the shape the gates need.
//
// The corpus is markdown plus one nav.json holding the table of
// contents. Everything here is parsing: the tests beside this file are
// the point, and they are what stop the docs and the code drifting
// apart. Nothing in this package ships in a binary — docs/site is
// rendered by the website (carlosframework/rastrillo-website), which
// vendors a copy. This package exists so the framework's own CI can
// fail a pull request whose docs no longer describe it.
//
// The markdown parsing is deliberately small and deliberately
// fence-aware. It is not a markdown implementation and must never grow
// into one: it finds ATX headings, fenced code blocks, internal links
// and docs:ignore markers, and it knows that a "#" inside a shell fence
// is a comment rather than a heading. Anything more belongs to the
// renderer.
//
// 🤖 written for the web-docs change, 2026-08-23.
package docsite

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Heading is one ATX heading, with the anchor the renderer will give
// it. Anchor equivalence with the website's markdown-it-anchor
// configuration is pinned from the website side; see Anchor.
type Heading struct {
	Level  int
	Text   string
	Anchor string
}

// Link is one internal /docs link found in a page's source, with the
// line it sits on so a failure can name it.
type Link struct {
	Target   string // page slug, e.g. "migrations" or "reference/db"
	Fragment string // heading anchor, or "" for a whole-page link
	Line     int
}

// Fence is one fenced code block and the language on its info string.
type Fence struct {
	Lang string
	Body string
	Line int
}

// Page is one documentation page: its nav metadata, its source, and
// everything the gates read out of it.
type Page struct {
	Slug  string // "migrations", "reference/db"
	Label string // nav label, usually shorter than Title
	Blurb string // one line, rendered on the /docs index
	Title string // the page's own "# " heading
	Path  string // path on disk

	Body     string
	Headings []Heading
	Links    []Link
	Fences   []Fence

	// Ignores maps a symbol name to the reason its page gives for not
	// documenting it, from a "<!-- docs:ignore Name reason -->" marker.
	Ignores map[string]string
}

// Section is one group in the sidebar.
type Section struct {
	Title string
	Pages []*Page
}

// Site is the whole corpus, in reading order.
type Site struct {
	// Index is docs/site/index.md, the /docs overview. It sits outside
	// Sections because it is the page the sidebar's sections are listed
	// on: an entry for itself would render as a card linking to the page
	// you are already reading.
	Index    *Page
	Sections []Section
	BySlug   map[string]*Page
}

// Pages returns every page in reading order, the index first.
func (s *Site) Pages() []*Page {
	var out []*Page
	if s.Index != nil {
		out = append(out, s.Index)
	}
	for _, sec := range s.Sections {
		out = append(out, sec.Pages...)
	}
	return out
}

// navFile is nav.json's shape.
type navFile struct {
	Sections []struct {
		Title string `json:"title"`
		Pages []struct {
			Slug  string `json:"slug"`
			Label string `json:"label"`
			Blurb string `json:"blurb"`
		} `json:"pages"`
	} `json:"sections"`
}

// Load reads nav.json and every page it names. A page named by nav.json
// that does not exist is an error here; a page that exists and is not
// named is not — that asymmetry is deliberate, because the nav test
// reports it with a better message than a load failure could.
func Load(root string) (*Site, error) {
	raw, err := os.ReadFile(filepath.Join(root, "nav.json"))
	if err != nil {
		return nil, fmt.Errorf("read nav.json: %w", err)
	}
	var nav navFile
	if err := json.Unmarshal(raw, &nav); err != nil {
		return nil, fmt.Errorf("parse nav.json: %w", err)
	}

	site := &Site{BySlug: map[string]*Page{}}
	index, err := loadPage(root, "index", "Documentation", "")
	if err != nil {
		return nil, err
	}
	site.Index = index
	site.BySlug[index.Slug] = index

	for _, sec := range nav.Sections {
		out := Section{Title: sec.Title}
		for _, entry := range sec.Pages {
			p, err := loadPage(root, entry.Slug, entry.Label, entry.Blurb)
			if err != nil {
				return nil, err
			}
			out.Pages = append(out.Pages, p)
			site.BySlug[p.Slug] = p
		}
		site.Sections = append(site.Sections, out)
	}
	return site, nil
}

// loadPage reads one page off disk and parses it.
func loadPage(root, slug, label, blurb string) (*Page, error) {
	p := &Page{
		Slug:  slug,
		Label: label,
		Blurb: blurb,
		Path:  filepath.Join(root, filepath.FromSlash(slug)+".md"),
	}
	body, err := os.ReadFile(p.Path)
	if err != nil {
		return nil, fmt.Errorf("nav.json names %q: %w", slug, err)
	}
	p.Body = string(body)
	parsePage(p)
	return p, nil
}

// parsePage fills in everything derived from a page's source.
func parsePage(p *Page) {
	p.Ignores = map[string]string{}
	lines := strings.Split(p.Body, "\n")

	inFence := false
	fenceMark := ""
	var fence Fence

	for i, line := range lines {
		lineNo := i + 1
		trimmed := strings.TrimSpace(line)

		// Fence open/close. The marker must match in kind and be at
		// least as long as the opener, so a ``` inside a ~~~~ block is
		// content rather than a close.
		if mark, info, ok := fenceDelim(trimmed); ok {
			switch {
			case !inFence:
				inFence, fenceMark = true, mark
				fence = Fence{Lang: info, Line: lineNo}
				continue
			case sameFence(mark, fenceMark):
				inFence = false
				p.Fences = append(p.Fences, fence)
				continue
			}
		}
		if inFence {
			fence.Body += line + "\n"
			continue
		}

		if level, text, ok := heading(trimmed); ok {
			h := Heading{Level: level, Text: text, Anchor: Anchor(text)}
			p.Headings = append(p.Headings, h)
			if level == 1 && p.Title == "" {
				p.Title = text
			}
		}

		for _, l := range docsLinks(line) {
			l.Line = lineNo
			p.Links = append(p.Links, l)
		}

		if name, reason, ok := ignoreMarker(trimmed); ok {
			p.Ignores[name] = reason
		}
	}
}

// fenceDelim reports whether a trimmed line opens or closes a fence,
// returning the marker run and the info string.
func fenceDelim(line string) (mark, info string, ok bool) {
	var r byte
	switch {
	case strings.HasPrefix(line, "```"):
		r = '`'
	case strings.HasPrefix(line, "~~~"):
		r = '~'
	default:
		return "", "", false
	}
	n := 0
	for n < len(line) && line[n] == r {
		n++
	}
	return line[:n], strings.TrimSpace(line[n:]), true
}

// sameFence reports whether a closing marker can close an opener: same
// character, and at least as long.
func sameFence(closing, opening string) bool {
	if closing == "" || opening == "" || closing[0] != opening[0] {
		return false
	}
	return len(closing) >= len(opening)
}

// heading parses an ATX heading.
func heading(line string) (level int, text string, ok bool) {
	n := 0
	for n < len(line) && line[n] == '#' {
		n++
	}
	if n == 0 || n > 6 || n >= len(line) || line[n] != ' ' {
		return 0, "", false
	}
	return n, strings.TrimSpace(strings.TrimRight(line[n+1:], " #")), true
}

// docsLinks finds every markdown link whose target starts with /docs.
func docsLinks(line string) []Link {
	var out []Link
	rest := line
	for {
		i := strings.Index(rest, "](/docs")
		if i < 0 {
			return out
		}
		rest = rest[i+2:]
		end := strings.IndexByte(rest, ')')
		if end < 0 {
			return out
		}
		out = append(out, parseDocsHref(rest[:end]))
		rest = rest[end:]
	}
}

// parseDocsHref turns "/docs/reference/db#open" into its slug and
// fragment. A bare "/docs" is the index, whose slug is "index".
func parseDocsHref(href string) Link {
	var l Link
	if i := strings.IndexByte(href, '#'); i >= 0 {
		l.Fragment, href = href[i+1:], href[:i]
	}
	href = strings.TrimSuffix(href, "/")
	slug := strings.TrimPrefix(href, "/docs")
	slug = strings.TrimPrefix(slug, "/")
	if slug == "" {
		slug = "index"
	}
	l.Target = path.Clean(slug)
	return l
}

// ignoreMarker parses "<!-- docs:ignore Name reason -->".
func ignoreMarker(line string) (name, reason string, ok bool) {
	const open = "<!-- docs:ignore"
	if !strings.HasPrefix(line, open) {
		return "", "", false
	}
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, open), "-->"))
	name, reason, _ = strings.Cut(body, " ")
	return strings.TrimSpace(name), strings.TrimSpace(reason), name != ""
}

// Anchor is the heading anchor the rendered page will carry.
//
// The website configures markdown-it-anchor with a slugify that matches
// this function exactly, and check-anchors.mjs compares the two against
// a committed fixture. Keeping both sides deliberately plain — lowercase
// alphanumerics, everything else a separator — is what makes that
// equivalence checkable rather than hopeful: markdown-it-anchor's own
// default URI-encodes punctuation, so a heading naming `db.Open` would
// anchor differently on each side.
func Anchor(text string) string {
	var b strings.Builder
	pendingSep := false
	for _, r := range strings.ToLower(text) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			if pendingSep && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingSep = false
			b.WriteRune(r)
		default:
			pendingSep = true
		}
	}
	return b.String()
}

// MarkdownFiles walks root and returns every page slug on disk, with
// "reference/" prefixes preserved and ".md" stripped.
func MarkdownFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		out = append(out, strings.TrimSuffix(filepath.ToSlash(rel), ".md"))
		return nil
	})
	return out, err
}
