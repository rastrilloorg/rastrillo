package tmpl

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"text/template/parse"

	"amadan.net/rastrillo/rastrillo/internal/capturemode"
	"amadan.net/rastrillo/rastrillo/internal/tmplescape"
	"amadan.net/rastrillo/rastrillo/tmpl/tmplrt"
)

// CaptureEnv names the file rastrillo generate wants the app's template
// sets written to. While it is set, every set the process creates is
// recorded, and rastrillo.Serve writes the record and returns without
// listening. A database opened through rastrillo.OpenDB or db.Open
// opens in the working directory instead of where it is configured.
const CaptureEnv = capturemode.Env

// captureVersion changes when the file's meaning does, so that a
// generate that reads another version can say so.
const captureVersion = 1

// Capture is the file WriteCapture writes: what rastrillo generate needs
// to know about the sets an app built.
type Capture struct {
	Version int
	// GoVersion is the release escaping was forked from, which is the
	// one fingerprints are taken under.
	GoVersion string
	// GeneratorVersion is the app's tmplrt.GeneratorVersion, which its
	// fingerprints take in: only the generator of that version may write
	// code registered under them.
	GeneratorVersion int
	Sets             []CapturedSet
}

// CapturedSet is one set, as it was when capture was written.
type CapturedSet struct {
	// Name is the name of the template the set was created with.
	Name string
	// Owner is the import path of the package that created the set, or
	// "main". Generate maps it to a directory.
	Owner string
	// Templates holds the printed tree of each template, as parsed:
	// escaping has not touched them.
	Templates map[string]string
	// Fingerprints holds the fingerprint the set gives each template at
	// runtime.
	Fingerprints map[string]string
	Funcs        map[string]FuncSig
	// Option is the set's missingkey setting: "default", "zero" or
	// "error".
	Option string
	// MetaContentURLEscape is this process's value for the meta refresh
	// URL escaping, which generate-time escaping has to use.
	MetaContentURLEscape bool
	// Uncompilable maps a template to why it cannot be compiled from
	// what is captured here.
	Uncompilable map[string]string
	// Cloned is whether the app made a set from this one with Clone. A
	// set built only to be cloned, as ui.NewPages builds its base, can
	// hold a template html/template could not run, because the template
	// it calls is defined only in the clones.
	Cloned bool `json:",omitempty"`
}

// recorder holds the sets created while capture is on. Sets are added
// only then, so an app that is not being captured keeps no list.
type recorder struct {
	mu   sync.Mutex
	sets []*set
}

var captured recorder

// testHookCaptured, when set, is called after each set is added.
var testHookCaptured func()

func (recorder *recorder) add(set *set) {
	if os.Getenv(CaptureEnv) == "" {
		return
	}
	recorder.mu.Lock()
	recorder.sets = append(recorder.sets, set)
	recorder.mu.Unlock()
	if testHookCaptured != nil {
		testHookCaptured()
	}
}

func (recorder *recorder) reset() {
	recorder.mu.Lock()
	recorder.sets = nil
	recorder.mu.Unlock()
}

func (recorder *recorder) all() []*set {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return slices.Clone(recorder.sets)
}

// WriteCapture writes, as JSON to path, every set created in this
// process while CaptureEnv was set. It reads the sets without preparing
// or executing them: escaping edits trees in place.
func WriteCapture(path string) error {
	capture := Capture{
		Version:          captureVersion,
		GoVersion:        tmplescape.GoVersion,
		GeneratorVersion: tmplrt.GeneratorVersion,
		Sets:             []CapturedSet{},
	}
	for _, set := range captured.all() {
		capture.Sets = append(capture.Sets, set.capture())
	}
	data, err := json.MarshalIndent(capture, "", "\t")
	if err != nil {
		return fmt.Errorf("tmpl: capture: %w", err)
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func (set *set) capture() CapturedSet {
	set.mu.Lock()
	defer set.mu.Unlock()
	trees, derived := set.treesLocked()
	env := currentEnv(set.missingKey)
	cs := CapturedSet{
		Name:                 set.name,
		Owner:                set.owner,
		Templates:            make(map[string]string, len(trees)),
		Funcs:                make(map[string]FuncSig, len(set.funcs)),
		Option:               set.missingKey,
		Cloned:               set.cloned,
		MetaContentURLEscape: env.metaContentURLEscape,
		Uncompilable:         map[string]string{},
	}
	// Once executed, the trees are escaped and fingerprints can no longer
	// be taken. The ones prepare took before escaping are what the
	// runtime compares with.
	if set.executed {
		cs.Fingerprints = maps.Clone(set.prep.fingerprints)
	} else {
		cs.Fingerprints = fingerprints(trees, set.funcs, env)
	}
	for name, fn := range set.funcs {
		if typ := reflect.TypeOf(fn); typ != nil && typ.Kind() == reflect.Func {
			cs.Funcs[name] = typeExpr(typ, set.owner)
		}
	}

	for name, tree := range trees {
		cs.Templates[name] = tree.Root.String()
	}
	for name, why := range set.reasonsLocked(cs.Templates, derived) {
		cs.Uncompilable[name] = strings.Join(why, "; ")
	}
	return cs
}

// Why capture marks a template uncompilable.
const (
	derivedReason  = "the set defines a name containing " + derivedMarker
	executedReason = "the set executed before capture, " +
		"so its trees are escaped"
	controlFuncReason = "the set has a function called %q, " +
		"which generate cannot tell from {{%s}}"
	unparsedReason = "the source does not survive a round trip " +
		"through the parser: "
	reprintedReason = "the source prints differently after a round trip " +
		"through the parser"
)

// reasonsLocked says why each of the set's templates, whose printed
// sources are sources, cannot be compiled; derived is whether the set
// defines a name html/template's escaping derives.
func (set *set) reasonsLocked(
	sources map[string]string,
	derived bool,
) map[string][]string {
	reasons := map[string][]string{}
	for name, src := range sources {
		if why := roundTripProblem(name, src); why != "" {
			reasons[name] = append(reasons[name], why)
		}
		if set.delims {
			reasons[name] = append(reasons[name], "the set uses Delims")
		}
		if derived {
			reasons[name] = append(reasons[name], derivedReason)
		}
		if set.executed {
			reasons[name] = append(reasons[name], executedReason)
		}
		// The parser reads {{break}} as a call of a function called
		// break, if the set has one, and as the control statement if not.
		// Generate reparses the sources without the set's functions, and
		// both print as {{break}}, so the round trip cannot tell them
		// apart.
		for _, control := range []string{"break", "continue"} {
			if _, ok := set.funcs[control]; ok {
				reasons[name] = append(reasons[name],
					fmt.Sprintf(controlFuncReason, control, control))
			}
		}
	}
	return reasons
}

// roundTripProblem is why src, a printed tree, does not print the same
// after being parsed again, or "" if it does. Generate parses the
// captured sources, so a template that changes in that trip would be
// compiled from a different tree than the app runs.
func roundTripProblem(name, src string) string {
	tree := parse.New(name)
	tree.Mode = parse.SkipFuncCheck
	if _, err := tree.Parse(src, "", "", map[string]*parse.Tree{}); err != nil {
		return unparsedReason + err.Error()
	}
	if tree.Root.String() != src {
		return reprintedReason
	}
	return ""
}
