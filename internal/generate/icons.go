package generate

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"amadan.net/rastrillo/rastrillo"
)

// IconRef is one {{icon "slug"}} call whose slug no icon answers.
type IconRef struct {
	File string // relative to the app directory
	Line int
	Slug string
}

// frameworkSlugs is the built-in set, used when an app has no icons
// package of its own — it predates the scaffold that writes one, or it
// deleted the package and calls rastrillo.Icon directly. Either way the
// check still runs rather than silently passing.
//
// Read from rastrillo.IconSlugs rather than copied, so an icon added to
// icons.go cannot leave this list quietly stale.
var frameworkSlugs = rastrillo.IconSlugs()

// KnownSlugs reports every icon slug the app can render, read from its
// own internal/icons package when it has one.
//
// The package is parsed rather than pattern-matched because it is
// app-owned source a developer is explicitly invited to edit: an icon
// added by hand must count, and a regex over Go source would miss or
// misread it.
func KnownSlugs(appDir string) (map[string]bool, error) {
	out := map[string]bool{}
	// The scaffold writes internal/<pkg>/icons/icons.go, and <pkg> is
	// derived from the app name, so the path is discovered rather than
	// assumed. Any icons package under internal/ counts: an app is free
	// to move or rename it, and this is a gate, not a layout police.
	matches, err := filepath.Glob(filepath.Join(appDir, "internal", "*", "icons", "icons.go"))
	if err != nil {
		return nil, err
	}
	if legacy := filepath.Join(appDir, "internal", "icons", "icons.go"); fileExists(legacy) {
		matches = append(matches, legacy)
	}
	if len(matches) == 0 {
		for _, s := range frameworkSlugs {
			out[s] = true
		}
		return out, nil
	}
	for _, path := range matches {
		if err := collectSlugs(path, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// collectSlugs adds every string key of every map literal in one
// app-owned icons package to out.
func collectSlugs(path string, out map[string]bool) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, src, parser.AllErrors)
	if err != nil {
		return err
	}
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		if _, ok := lit.Type.(*ast.MapType); !ok {
			return true
		}
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.BasicLit)
			if !ok || key.Kind != token.STRING {
				continue
			}
			if slug, err := strconv.Unquote(key.Value); err == nil {
				out[slug] = true
			}
		}
		return true
	})
	return nil
}

// iconCall matches {{icon "slug"}} in all its whitespace and trim-marker
// spellings: {{icon "x"}}, {{ icon "x" }}, {{- icon "x" -}}.
var iconCall = regexp.MustCompile(`\{\{-?\s*icon\s+"([^"]*)"\s*-?\}\}`)

// iconArg matches a literal slug passed to a partial as data — the way
// the shipped partials are actually driven:
//
//	{{template "page-header" dict "ActionIcon" "plus"}}
//
// Without this the gate would miss the idiom this very repository uses
// everywhere and only catch bare {{icon "x"}} calls, which are the rarer
// form. Any key ending in Icon counts.
//
// A key ending in Icon whose value is deliberately not a slug would be a
// false positive; rename the key, or use "" for "no icon", which is
// skipped.
var iconArg = regexp.MustCompile(`"[A-Za-z]*Icon"\s+"([^"]+)"`)

// UnknownIconSlugs reports every {{icon "slug"}} in the app's HTML
// templates whose slug nothing answers.
//
// Every .html file under the app is scanned, not just a templates/
// directory at the root: an app is free to colocate its templates with
// the package that renders them (examples/blog keeps them under
// internal/blog/templates), and a gate that only looked in one
// conventional place would silently pass those apps.
//
// gen/ is skipped because it is generated output, rewritten on every
// generate — a finding there would name a file the developer must not
// edit.
//
// At run time an unknown slug renders nothing rather than panicking a
// page mid-response — that is the right behaviour and it stays. This is
// the pre-ship gate, matching the i18n catalog check's posture: silent
// fallback while iterating, loud failure before ship.
func UnknownIconSlugs(appDir string) ([]IconRef, error) {
	known, err := KnownSlugs(appDir)
	if err != nil {
		return nil, err
	}

	var out []IconRef
	err = filepath.WalkDir(appDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "gen" || d.Name() == "node_modules" || strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".html") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(appDir, path)
		if err != nil {
			rel = path
		}
		for i, line := range strings.Split(string(src), "\n") {
			for _, re := range []*regexp.Regexp{iconCall, iconArg} {
				for _, m := range re.FindAllStringSubmatch(line, -1) {
					if m[1] != "" && !known[m[1]] {
						out = append(out, IconRef{File: rel, Line: i + 1, Slug: m[1]})
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
