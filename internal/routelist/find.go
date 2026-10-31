package routelist

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// actionPackage is where Routes, Visit and the recorder come from.
const actionPackage = "amadan.net/rastrillo/rastrillo/action"

// Target is the Routes function to call: where it is and the type of
// the dependency it takes.
type Target struct {
	// ModuleDir is the root of the app's module. The listing program
	// is made to appear inside it, so it may import internal packages.
	ModuleDir  string
	ImportPath string

	// AppType is the dependency's type, declared in ImportPath's
	// package; Pointer is whether Routes takes a pointer to it.
	AppType string
	Pointer bool

	// Output receives what the app prints while its routes are listed.
	// nil is os.Stderr, which keeps the table on stdout clean.
	Output io.Writer
}

// Find looks for Routes in the packages under dir's internal/. chosen,
// an import path, picks one when more than one package has it.
// Cancelling ctx stops the go list it runs.
func Find(ctx context.Context, dir, chosen string) (Target, error) {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return Target{}, err
	}
	// Without the directory, go list's own error names a pattern, not
	// the likely mistake: being in a directory other than the app's.
	internal := filepath.Join(absolute, "internal")
	if info, err := os.Stat(internal); err != nil || !info.IsDir() {
		return Target{}, say(noInternalDir, absolute)
	}
	modFlag, err := goSettings(ctx, absolute)
	if err != nil {
		return Target{}, err
	}
	packages, err := goList(ctx, absolute, modFlag, "./internal/...")
	if err != nil {
		return Target{}, err
	}
	fitting, misfits, err := routesFunctions(packages, chosen)
	if err != nil {
		return Target{}, err
	}
	switch {
	case len(fitting) == 1:
		return fitting[0], nil
	case len(fitting) > 1:
		return Target{}, several(fitting)
	case len(misfits) > 0:
		return Target{}, misfits[0]
	case chosen != "":
		return Target{}, say(chosenHasNone, chosen)
	}
	return Target{}, say(noRoutesFunction, internal)
}

// routesFunctions is every package-level Routes in the packages, split
// into those that can be called and those of another shape. A misfit is
// reported only when nothing fits, so a helper called Routes elsewhere
// does not stop an app whose real one is right. A Routes below a second
// internal directory is a misfit too: the listing program, at the
// module's root, may not import it, and the build would blame the app.
func routesFunctions(
	packages []listedPackage, chosen string,
) ([]Target, []error, error) {
	var fitting []Target
	var misfits []error
	for _, listed := range packages {
		if chosen != "" && listed.ImportPath != chosen {
			continue
		}
		files, err := listed.parse()
		if err != nil {
			return nil, nil, err
		}
		for _, found := range findRoutes(files) {
			if found.misfit == "" && listed.belowSecondInternal() {
				misfits = append(misfits,
					say(nestedInternal, listed.ImportPath))
				continue
			}
			if found.misfit != "" {
				misfits = append(misfits, say(wrongShape,
					listed.ImportPath, found.misfit))
				continue
			}
			found.target.ModuleDir = listed.Module.Dir
			found.target.ImportPath = listed.ImportPath
			fitting = append(fitting, found.target)
		}
	}
	return fitting, misfits, nil
}

func several(fitting []Target) error {
	var lines strings.Builder
	for _, target := range fitting {
		fmt.Fprintf(&lines, "\t%s\n", target.ImportPath)
	}
	return say(severalRoutesFunctions, lines.String(),
		fitting[0].ImportPath)
}

// belowSecondInternal reports whether the package is inside an internal
// directory below the module's top-level internal/. Go lets only the
// tree under that directory's parent import it.
func (listed listedPackage) belowSecondInternal() bool {
	inside := strings.TrimPrefix(listed.ImportPath, listed.Module.Path+"/")
	elements := strings.Split(inside, "/")
	return slices.Contains(elements[1:], "internal")
}
