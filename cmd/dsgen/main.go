// Command dsgen writes rastrillo's design-system gallery — every
// partial, every class idiom, every token, in three themes and twelve
// languages — to a directory of static files.
//
// The gallery is not committed to the rastrillo repository. It is 20 MB
// of machine output that changes whole on every change to ui, and the
// site that publishes it builds it instead:
//
//	go run amadan.net/rastrillo/rastrillo/cmd/dsgen@<version> \
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
// # What it deletes, and what it refuses to
//
// dsgen owns -out. It empties the directory before it writes, so what is
// there afterwards is the render and only the render: a page, an asset
// or a whole theme that a previous version produced and this one does
// not cannot survive into the output. That matters most where it is
// least visible — a build directory that persists between runs, and a
// framework version that renamed a theme.
//
// It will only take ownership of a directory that is empty, absent, or
// already its own. dsgen leaves a stamp file (.dsgen) naming itself in
// every directory it writes, and refuses, changing nothing, when -out
// holds anything else. The earlier version of this command emptied its
// output root with no such check and, pointed at the wrong directory
// once, wiped 152 files that were not the gallery's; a version after
// that removed only the paths it was about to write, which was safe and
// left a renamed theme's files published for as long as the build
// directory lived. The stamp is how it gets both.
//
// The stamp is written before the pages, so a run interrupted halfway —
// a full disk — leaves a directory dsgen still owns and will clean out
// on the next run. It is not atomic, though: a failed run leaves a
// partial tree behind, and the only thing saying so is the non-zero
// exit status. Treat it as fatal.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"amadan.net/rastrillo/rastrillo/internal/designsystem"
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

	root, n, total, err := write(*out, *mount)
	if err != nil {
		return err
	}

	fmt.Printf("dsgen: wrote %d files, %d bytes, to %s (mounted at %s)\n", n, total, root, *mount)
	return nil
}

// write renders the gallery at mount and puts it under out, returning
// the absolute directory it wrote and what it wrote there. Separate from
// run so the flags and the work can be tested apart.
func write(out, mount string) (root string, files, bytes int, err error) {
	rendered, err := designsystem.Render(mount)
	if err != nil {
		return "", 0, 0, err
	}

	root, err = filepath.Abs(out)
	if err != nil {
		return "", 0, 0, fmt.Errorf("resolving -out: %w", err)
	}
	if err := claim(root); err != nil {
		return "", 0, 0, err
	}

	names := make([]string, 0, len(rendered))
	for name := range rendered {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		dest := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return "", 0, 0, fmt.Errorf("mkdir for %s: %w", name, err)
		}
		if err := os.WriteFile(dest, rendered[name], 0o644); err != nil {
			return "", 0, 0, fmt.Errorf("writing %s: %w", name, err)
		}
		bytes += len(rendered[name])
	}
	return root, len(rendered), bytes, nil
}

// stampName is the file dsgen leaves in a directory to say the
// directory is its own. Dotted so it stays out of the way of whatever
// serves the tree.
const stampName = ".dsgen"

const stampBody = `This directory is written by rastrillo's dsgen command, which EMPTIES it
on every run. Do not keep anything here. Delete this file and dsgen will
refuse the directory instead.

	https://pkg.go.dev/amadan.net/rastrillo/rastrillo/cmd/dsgen
`

// claim makes root a directory dsgen owns and empties it, or fails
// without touching anything.
//
// Ownership is the whole of the safety story, and it is deliberately
// conservative: an absent directory is created, an empty one is adopted,
// one holding dsgen's own stamp is emptied and reused, and anything else
// is refused with an explanation. So dsgen can be pointed at the wrong
// directory — the thing that once cost 152 files — and the wrong
// directory is almost never empty.
//
// Emptying rather than removing named paths is the other half. Removing
// only what the current render produces looked safer and quietly wasn't:
// a theme renamed between versions leaves its whole directory and its
// stylesheet behind, published, linked, and invisible to a site guard
// that checks which files are present rather than which are not. That is
// the deleted freshness gate's failure mode reappearing one directory
// over, and the answer is that there is never a second copy to drift:
// the output IS the render, every time.
func claim(root string) error {
	entries, err := os.ReadDir(root)
	switch {
	case os.IsNotExist(err):
		if err := os.MkdirAll(root, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", root, err)
		}
	case err != nil:
		return fmt.Errorf("reading %s: %w", root, err)
	case len(entries) > 0:
		if _, err := os.Stat(filepath.Join(root, stampName)); err != nil {
			return fmt.Errorf("refusing to empty %s: it is not empty and holds no %s stamp, "+
				"so dsgen has not written it before. Point -out at a new or empty directory, "+
				"or delete that one yourself", root, stampName)
		}
		for _, e := range entries {
			if err := os.RemoveAll(filepath.Join(root, e.Name())); err != nil {
				return fmt.Errorf("emptying %s: %w", root, err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(root, stampName), []byte(stampBody), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", filepath.Join(root, stampName), err)
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
