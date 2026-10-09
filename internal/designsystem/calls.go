package designsystem

import (
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"sync"

	"amadan.net/rastrillo/rastrillo/internal/codeview"
	"amadan.net/rastrillo/rastrillo/ui"
)

// call is one sample's template call: the source the Template tab shows,
// and the value . must be when that source runs, which is empty unless
// the sample binds a placeholder.
type call struct {
	Source string
	Dot    map[string]any
}

// callFor is the call that renders one sample, generated from the same
// value the preview was rendered from. Every partial sample has a
// Template tab because the rendered HTML alone taught the hand-rolling
// the framework forbids, and lost the partial's own logic: meter clamps
// Percent 140 to 100, and HTML copied out of it carries the wrong
// number. An error names the partial, the state and the value's type,
// and fails the build.
func callFor(partial string, s sample, locale string) (call, error) {
	data := s.Data
	if s.Build != nil {
		data = s.Build(locale)
	}
	src, dot, err := codeview.Call(partial, data, partialKeys()[partial], s.Bind)
	if err != nil {
		return call{}, fmt.Errorf("%s (%s): %w", partial, s.State, err)
	}
	return call{Source: src, Dot: dot}, nil
}

var (
	keysOnce      sync.Once
	keysByPartial map[string][]string
)

// partialKeys is each partial's keys in the order its own Keys: comment
// block lists them, read off ui's templates once. A call written in
// that order reads like the partial's documentation; a key the block
// does not list follows alphabetically, so either way the order is the
// same on every render.
func partialKeys() map[string][]string {
	keysOnce.Do(func() {
		keysByPartial = map[string][]string{}
		files, err := fs.Glob(ui.Templates(), "*.html")
		if err != nil {
			panic("designsystem: listing ui's templates: " + err.Error())
		}
		for _, name := range files {
			src, err := fs.ReadFile(ui.Templates(), name)
			if err != nil {
				panic("designsystem: reading " + name + ": " + err.Error())
			}
			for partial, keys := range parseKeys(string(src)) {
				keysByPartial[partial] = keys
			}
		}
	})
	return keysByPartial
}

var (
	keysDefine = regexp.MustCompile(`\{\{define "([^"]+)"\}\}`)
	// A key line: one or more capitalised names, comma-separated, then
	// at least two spaces and the type. Continuation lines and prose in
	// the block do not match: they start lower-case, or have one space
	// after their first word.
	keysLine = regexp.MustCompile(`^(\s+)([A-Z][A-Za-z]*(?:, [A-Z][A-Za-z]*)*)\s{2,}\S`)
)

// parseKeys reads one template file's Keys: blocks and gives each to
// the {{define}} that follows it. Only lines at the block's own indent
// count, so row-menu's nested item keys (Label, Href, …) are not taken
// for the partial's.
func parseKeys(src string) map[string][]string {
	out := map[string][]string{}
	var pending []string
	in, indent := false, ""
	for _, line := range strings.Split(src, "\n") {
		if m := keysDefine.FindStringSubmatch(line); m != nil {
			out[m[1]] = pending
			pending, in = nil, false
			continue
		}
		if strings.TrimSpace(line) == "Keys:" {
			pending, in, indent = nil, true, ""
			continue
		}
		if !in {
			continue
		}
		m := keysLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if indent == "" {
			indent = m[1]
		}
		if m[1] != indent {
			continue
		}
		pending = append(pending, strings.Split(m[2], ", ")...)
	}
	return out
}
