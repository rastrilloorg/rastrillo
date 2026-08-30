// Command dsgen writes rastrillo's design-system gallery — every
// partial, every class idiom, every token, in three themes and twelve
// languages — to a directory of static files.
//
// The gallery is not committed to the rastrillo repository. It is 20 MB
// of machine output that changes whole on every change to ui, and the
// site that publishes it builds it instead:
//
//	go run github.com/carlosframework/rastrillo/cmd/dsgen@<version> \
//	    -out ./src/design-system -mount /design-system
//
// Pin a version. The gallery documents the framework at that version,
// so the sha or tag you generate from should be the one your prose was
// vendored from; anything else documents a library your reader is not
// using.
//
// # The two arguments, and why there are only two
//
// -out is where the files go. -mount is the URL path they will be
// served from, and it has to be right: every link, stylesheet and frame
// in the output is an absolute path under it, because the static edge
// this was built for serves a directory index at its slash-less URL
// without redirecting, and a relative href resolves against a different
// base on those two URLs. A tree generated for one mount cannot be
// served at another.
//
// There is deliberately nothing else. This command is published surface
// — an outside caller runs it with `go run …@version` and expects it to
// keep working — so every flag is a promise. A flag that encoded one
// site's layout, file naming or theme choice would be a promise made on
// behalf of everyone else's.
//
// # What it deletes
//
// dsgen removes the paths it is about to write, so a page that stops
// being rendered stops existing, and it removes nothing else: the
// top-level names in the render are the whole of what it will unlink
// inside -out. The earlier version of this command deleted its output
// root outright and, pointed at the wrong directory once, wiped 152
// files that were not the gallery's. A command people run with a
// directory on the command line does not get to do that.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/carlosframework/rastrillo/internal/designsystem"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "dsgen:", err)
		os.Exit(1)
	}
}

func run() error {
	out := flag.String("out", "", "directory to write the gallery into (required; created if missing)")
	mount := flag.String("mount", designsystem.DefaultMount, "URL path the gallery will be served from")
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q: dsgen takes flags only", flag.Arg(0))
	}
	if *out == "" {
		return fmt.Errorf("-out is required (a directory to write the gallery into)")
	}

	files, err := designsystem.Render(*mount)
	if err != nil {
		return err
	}

	root, err := filepath.Abs(*out)
	if err != nil {
		return fmt.Errorf("resolving -out: %w", err)
	}
	if err := clear(root, files); err != nil {
		return err
	}

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	var total int
	for _, name := range names {
		dest := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("mkdir for %s: %w", name, err)
		}
		if err := os.WriteFile(dest, files[name], 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", name, err)
		}
		total += len(files[name])
	}

	fmt.Printf("dsgen: wrote %d files, %d bytes, to %s (mounted at %s)\n", len(files), total, root, *mount)
	return nil
}

// clear removes the previous run's output and nothing else: for each
// distinct first path segment the render produces — index.html, the
// shared assets, one directory per theme — the matching entry under
// root goes, and anything else in root is left where it is.
//
// So dsgen is safe to point at a directory that holds other things, and
// a stale page from a render that no longer produces it still cannot
// survive into the output.
func clear(root string, files map[string][]byte) error {
	tops := map[string]bool{}
	for name := range files {
		tops[strings.SplitN(name, "/", 2)[0]] = true
	}
	names := make([]string, 0, len(tops))
	for name := range tops {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := os.RemoveAll(filepath.Join(root, name)); err != nil {
			return fmt.Errorf("removing the previous %s: %w", name, err)
		}
	}
	return nil
}

func usage() {
	fmt.Fprint(flag.CommandLine.Output(), `dsgen writes rastrillo's design-system gallery to a directory.

Usage:
	dsgen -out DIR [-mount PATH]

Every URL in the output is an absolute path under -mount, so generate
with the path the site will serve the files from. The default is the
path rastrillo.org uses.

Flags:
`)
	flag.PrintDefaults()
}
