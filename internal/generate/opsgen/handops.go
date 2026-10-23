package opsgen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/generate/handfile"
	"amadan.net/rastrillo/rastrillo/internal/generate/shape"
)

// opFiles is the five files a hand-written one can replace, in the
// order Emit writes them. types.go is not among them: it is the API's
// contract, and the parity check compares it.
var opFiles = []string{"list", "get", "create", "update", "delete"}

// Hand is one hand-written operation file, ops/<Resource>/<Op>.go.
type Hand struct {
	Resource string
	Op       string // list, get, create, update or delete
}

// Source is the file's path relative to the app root, as messages name it.
func (h Hand) Source() string { return "ops/" + h.Resource + "/" + h.Op + ".go" }

// The refusals, as the operator reads them. Each names the file and
// says what to do, because the generator stops at the first one.
const (
	msgTypes      = "%s: only the five operation files can be replaced; types are the API contract. Remove it, or put what it declares in one of list.go, get.go, create.go, update.go or delete.go"
	msgNotAnOp    = "%s: only the five operation files can be replaced: list.go, get.go, create.go, update.go and delete.go. Remove it, or put what it declares in one of them"
	msgNoResource = "%s: no manifest resource is named %q. Remove the directory, or declare the resource in manifest/%s.toml"
	msgNotInDir   = "%s: a hand-written operation lives in ops/<resource>/<operation>.go. Move it there"
	msgUntagged   = "operation files missing the build constraint:\n%seach needs `//go:build %s` (then a blank line) above its package clause,\nso `go build ./...` and friends skip generator input instead of failing on it\n(a file that already carries a //go:build line needs it amended, never a second line)"
)

// HandFiles lists every hand-written operation file under appRoot/ops,
// or nothing when there is no such directory. It refuses what Emit
// could not honour: a types.go, which stays generated; any other
// filename, because only the five operations can be replaced; a
// directory naming no resource in rs, which would otherwise sit there
// forever replacing nothing; and a file directly under ops/, for the
// same reason. Entries beginning with a dot are an editor's, not the
// app's, so a swap file does not fail every run while a file is open.
func HandFiles(appRoot string, rs []rastrillo.Resource) ([]Hand, error) {
	dir := filepath.Join(appRoot, "ops")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	names := make(map[string]bool, len(rs))
	for _, r := range rs {
		names[r.Name] = true
	}
	var out []Hand
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if !e.IsDir() {
			return nil, fmt.Errorf(msgNotInDir, "ops/"+e.Name())
		}
		if !names[e.Name()] {
			return nil, fmt.Errorf(msgNoResource, "ops/"+e.Name(), e.Name(), e.Name())
		}
		files, err := os.ReadDir(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if strings.HasPrefix(f.Name(), ".") {
				continue
			}
			op, ok := opOf(f.Name())
			switch {
			case f.Name() == "types.go":
				return nil, fmt.Errorf(msgTypes, "ops/"+e.Name()+"/"+f.Name())
			case f.IsDir(), !ok:
				return nil, fmt.Errorf(msgNotAnOp, "ops/"+e.Name()+"/"+f.Name())
			}
			out = append(out, Hand{Resource: e.Name(), Op: op})
		}
	}
	return out, nil
}

// opOf is the operation a filename replaces, by exact name: a
// "create" or "create.go.bak" is refused rather than passed on, because
// the copy reads create.go, and a file discovery accepted but the copy
// never read would do nothing without a word.
func opOf(name string) (string, bool) {
	for _, op := range opFiles {
		if name == op+".go" {
			return op, true
		}
	}
	return "", false
}

// CheckTagged is `--check`'s half of the build-tag convention for
// operations, the one UntaggedActions is for actions: an untagged
// ops/<resource>/<op>.go does not break generate, which copies it
// anyway, but it breaks every `go build ./...` in the tree with errors
// about types the file never declares, and none of them mention the
// one-line fix. The error here does.
func CheckTagged(appRoot string, hand []Hand) error {
	var untagged strings.Builder
	for _, h := range hand {
		src, err := os.ReadFile(filepath.Join(appRoot, filepath.FromSlash(h.Source())))
		if err != nil {
			return err
		}
		if !handfile.SkippedByDefaultBuild(src) {
			fmt.Fprintf(&untagged, "  %s\n", h.Source())
		}
	}
	if untagged.Len() == 0 {
		return nil
	}
	return fmt.Errorf(msgUntagged, untagged.String(), handfile.BuildTag)
}

// replace copies the hand file for h, if there is one, to path with its
// package clause rewritten to pkg and its build constraint stripped, so
// it shares types.go with the generated files beside it. It reports
// whether a hand file was found.
func replace(appRoot string, h Hand, path, pkg string) (bool, error) {
	src, err := os.ReadFile(filepath.Join(appRoot, filepath.FromSlash(h.Source())))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	out, err := handfile.Rewrite(src, h.Source(), pkg)
	if err != nil {
		return false, err
	}
	if err := shape.WriteFile(path, out); err != nil {
		return false, fmt.Errorf("%s: %w", h.Source(), err)
	}
	return true, nil
}
