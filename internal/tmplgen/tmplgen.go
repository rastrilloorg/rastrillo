// Package tmplgen is the template half of rastrillo generate. It builds
// each of an app's main packages with the rastrillo_capture tag, runs it
// in capture mode so that it records the template sets its startup
// builds, and writes rastrillo_templates_gen.go, the compiled code, into
// every package that built one.
package tmplgen

import (
	"bytes"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"

	"golang.org/x/tools/go/packages"

	"amadan.net/rastrillo/rastrillo/internal/tmplc"
	"amadan.net/rastrillo/rastrillo/tmpl"
)

// GenFile is the name of the generated file in each package.
const GenFile = "rastrillo_templates_gen.go"

// captureVersion is tmpl's: the version of the capture file this
// package reads.
const captureVersion = 1

// buildFlags go every time this package runs go. The tag leaves out the
// generated files, so a stale one that no longer compiles cannot stop
// its own regeneration. VCS stamping is off because the scratch
// directory may sit under someone else's repository, where go build
// fails to stamp.
var buildFlags = []string{"-tags=rastrillo_capture", "-buildvcs=false"}

// runTimeout bounds one capture run. Capture exits as soon as startup
// has built the sets; a binary still running after this is serving.
var runTimeout = 60 * time.Second

// Options says what Run captures and whether it writes.
type Options struct {
	// Mains are package patterns, relative to the module directory. Empty
	// means every main package under cmd/.
	Mains []string
	// Check compares instead of writing: a file that would change, appear
	// or be removed is listed in Stale.
	Check bool
}

// Result is what Run found and did. Paths are relative to the module
// directory, with forward slashes.
type Result struct {
	Written, Stale, Removed []string
	// Unsupported lists the templates left on html/template, and why.
	Unsupported []tmplc.Note
	// CaptureFailures has one note per main package that could not be
	// captured; Owner is its import path. Its sets are not compiled, and
	// no generated file it might have needed is removed.
	CaptureFailures []tmplc.Note
	// Unverified, in check mode, has the notes of CaptureFailures whose
	// main imports a package with a generated file. Check cannot say
	// whether that file is current, so the check is incomplete.
	Unverified []tmplc.Note
}

// Run captures the template sets of the module at moduleDir and writes,
// or with Check compares, each package's generated file. A generated
// file in a package the mains import that captured no sets is removed.
// The error is for a module Run could not process at all; a main that
// fails to capture is a CaptureFailure.
func Run(moduleDir string, options Options) (Result, error) {
	moduleDir, err := filepath.Abs(moduleDir)
	if err != nil {
		return Result{}, err
	}
	mains, err := loadMains(moduleDir, options.Mains)
	if err != nil {
		return Result{}, err
	}
	scratch, err := os.MkdirTemp("", "rastrillo-capture-*")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(scratch)

	work := newJob(moduleDir, scratch, options)
	if err := work.captureAll(mains); err != nil {
		return Result{}, err
	}
	if err := work.compileAll(); err != nil {
		return Result{}, err
	}
	if err := work.removeOrphans(); err != nil {
		return Result{}, err
	}
	return work.result, nil
}

// job is one Run: what its mains captured, and what it has done.
type job struct {
	moduleDir, scratch string
	options            Options
	result             Result
	// sets is by owner import path.
	sets map[string][]tmpl.CapturedSet
	// reached has the directories of module packages the mains import,
	// unsure those of packages a failed main imports, and owned those
	// that got a file.
	reached, unsure, owned map[string]bool
}

func newJob(moduleDir, scratch string, options Options) *job {
	return &job{
		moduleDir: moduleDir,
		scratch:   scratch,
		options:   options,
		sets:      map[string][]tmpl.CapturedSet{},
		reached:   map[string]bool{},
		unsure:    map[string]bool{},
		owned:     map[string]bool{},
	}
}

// captureAll captures each main's sets.
func (work *job) captureAll(mains []*packages.Package) error {
	for index, mainPkg := range mains {
		deps := moduleDeps(mainPkg)
		// Every set is made by tmpl, so a main that does not import it
		// builds none, and starting it would only run its real work: a
		// tool's output written into the scratch directory, reported as
		// a failure to capture.
		if buildsNoSets(mainPkg) {
			work.reach(deps)
			continue
		}
		captured, why, err := capture(
			work.moduleDir,
			work.scratch,
			index,
			mainPkg.PkgPath,
		)
		if err != nil {
			return err
		}
		if why != "" {
			work.failed(tmplc.Note{Owner: mainPkg.PkgPath, Reason: why}, deps)
			continue
		}
		work.reach(deps)
		for _, set := range captured.Sets {
			set = ofMain(set, mainPkg.PkgPath)
			// A set a framework package builds for itself at init has no
			// package of the app to go in.
			if set.Owner == "" {
				continue
			}
			work.sets[set.Owner] = append(work.sets[set.Owner], set)
		}
	}
	return nil
}

// reach records the directories of a main's module packages.
func (work *job) reach(deps map[string]string) {
	for _, dir := range deps {
		work.reached[dir] = true
	}
}

// failed records a main that could not be captured, whose packages
// may still need their generated files. In check mode, one that has a
// file leaves the check incomplete.
func (work *job) failed(note tmplc.Note, deps map[string]string) {
	work.result.CaptureFailures = append(work.result.CaptureFailures, note)
	generated := false
	for _, dir := range deps {
		work.unsure[dir] = true
		if _, err := os.Stat(filepath.Join(dir, GenFile)); err == nil {
			generated = true
		}
	}
	if work.options.Check && generated {
		work.result.Unverified = append(work.result.Unverified, note)
	}
}

// compileAll compiles each owner's sets into its package's file.
func (work *job) compileAll() error {
	pkgs, err := loadTypes(work.moduleDir, work.sets)
	if err != nil {
		return err
	}
	for _, owner := range slices.Sorted(maps.Keys(work.sets)) {
		pkg := pkgs[owner]
		// Sets built by a dependency's own code are its to compile.
		if pkg == nil || pkg.Module == nil || !pkg.Module.Main {
			continue
		}
		in, err := input(pkg, work.sets[owner], pkgs)
		if err != nil {
			return err
		}
		out, err := tmplc.Compile(in)
		if err != nil {
			return err
		}
		work.result.Unsupported = append(work.result.Unsupported,
			out.Unsupported...)
		work.owned[pkg.Dir] = true
		path := filepath.Join(pkg.Dir, GenFile)
		err = work.result.put(work.moduleDir, path, out.Source,
			work.options.Check)
		if err != nil {
			return err
		}
	}
	return nil
}

// removeOrphans removes the generated files no set needs now. Every
// main under cmd/ is every way the module builds sets, so a file
// anywhere else is an orphan. An explicit list of mains is a part of
// the module, and speaks only for the packages it imports.
func (work *job) removeOrphans() error {
	candidates := work.reached
	if len(work.options.Mains) == 0 {
		var err error
		if candidates, err = generatedDirs(work.moduleDir); err != nil {
			return err
		}
	}
	for _, dir := range slices.Sorted(maps.Keys(candidates)) {
		// A failed main's packages may still need theirs.
		if work.owned[dir] || work.unsure[dir] {
			continue
		}
		path := filepath.Join(dir, GenFile)
		err := work.result.remove(work.moduleDir, path, work.options.Check)
		if err != nil {
			return err
		}
	}
	return nil
}

// ofMain is set, captured from the main package at mainPath, with "main"
// replaced by that path. The runtime names a main package's functions
// and types "main", so capture records its sets' owner and the types
// their functions mention under it, and go/packages loads neither.
func ofMain(set tmpl.CapturedSet, mainPath string) tmpl.CapturedSet {
	if set.Owner == "main" {
		set.Owner = mainPath
	}
	funcs := make(map[string]tmpl.FuncSig, len(set.Funcs))
	for name, sig := range set.Funcs {
		refs := make([]tmpl.TypeRef, len(sig.Refs))
		for index, ref := range sig.Refs {
			if ref.PkgPath == "main" {
				ref.PkgPath = mainPath
			}
			refs[index] = ref
		}
		sig.Refs = refs
		funcs[name] = sig
	}
	set.Funcs = funcs
	return set
}

// put writes src to path, or in check mode notes that it would.
func (result *Result) put(
	moduleDir, path string,
	src []byte,
	check bool,
) error {
	old, err := os.ReadFile(path)
	if err == nil && bytes.Equal(old, src) {
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	rel := relative(moduleDir, path)
	if check {
		result.Stale = append(result.Stale, rel)
		return nil
	}
	if err := os.WriteFile(path, src, 0o644); err != nil {
		return err
	}
	result.Written = append(result.Written, rel)
	return nil
}

// remove deletes path if it exists, or in check mode notes that it
// would.
func (result *Result) remove(moduleDir, path string, check bool) error {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	rel := relative(moduleDir, path)
	if check {
		result.Stale = append(result.Stale, rel)
		return nil
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	result.Removed = append(result.Removed, rel)
	return nil
}

func relative(moduleDir, path string) string {
	rel, err := filepath.Rel(moduleDir, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}
