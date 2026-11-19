package tmpl

import (
	"context"
	"html/template"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"text/template/parse"

	"amadan.net/rastrillo/rastrillo/tmpl/tmplrt"
)

// The log lines a set writes. They are kept together so they can be
// reviewed as copy.
const (
	staleMessage = "tmpl: templates changed since the last " +
		"rastrillo generate; run rastrillo generate"
	refusedMessage = "tmpl: compiled code does not fit this set's " +
		"functions; using html/template"
)

// set is what every template in one html/template namespace shares:
// Lookup, Templates and New return templates of the same set, and Clone
// starts a new one.
type set struct {
	owner string // import path of the package that created the set
	name  string // the template the set was created with, for capture

	mu         sync.Mutex
	root       *template.Template // any template of the namespace
	funcs      FuncMap
	missingKey string
	// delims is set once any template in the set changes its delimiters.
	// Such a set is never compiled: generate reparses the sources with
	// the default delimiters.
	delims bool
	// wrappers holds the one *Template for each of the namespace's
	// templates that has been handed out. A template html/template's New
	// replaced is no longer in the namespace; adopt moves its wrapper out.
	wrappers map[*template.Template]*Template
	// prep is what prepare worked out. A change to the set before first
	// use drops it, so the next use fingerprints the set as changed.
	prep *prepared
	// cloned is set once Clone has made a set from this one. Capture
	// records it: a set cloned from, such as ui.NewPages's base, may be
	// one the app never executes.
	cloned bool
	// executed is set by the first Execute or ExecuteTemplate. From then
	// on html/template may have escaped the trees in place, so the set is
	// never fingerprinted again.
	executed bool

	// ready is prep once executed is set. Execution reads it without a
	// lock, and never takes html/template's namespace mutex for a compiled
	// template.
	ready atomic.Pointer[prepared]
	// compiledRan is set the first time compiled code renders, standing in
	// for the escaping html/template would have done, so that Parse and
	// Clone fail afterwards as they would there.
	compiledRan atomic.Bool
}

type prepared struct {
	fingerprints map[string]string // every template with a tree
	compiled     map[string]tmplrt.Exec
	// stale names the templates with no code that the owner did not
	// leave out.
	stale   []string
	refused map[string]error // code whose Bind failed
	// shadow is what shadow mode compares compiled code with; nil when
	// shadow mode is off.
	shadow *shadowSets
}

// shadowSets is html/template as compiled code must match it: each
// entry executed first on a fresh set. html/template escapes a set's
// trees in place as each entry first runs, so an entry that runs after
// another on a shared set can write other bytes (see shadow). pristine
// is a clone of the set taken at prepare, before anything executed,
// which is never executed itself; each entry gets a clone of it of its
// own, on which only that entry runs.
type shadowSets struct {
	pristine *template.Template
	err      error // from cloning, which a set that has not run never gets

	mu      sync.Mutex
	entries map[string]*template.Template
}

// entry is the set shadow mode executes name on.
func (shadows *shadowSets) entry(name string) (*template.Template, error) {
	if shadows.err != nil {
		return nil, shadows.err
	}
	shadows.mu.Lock()
	defer shadows.mu.Unlock()
	if template := shadows.entries[name]; template != nil {
		return template, nil
	}
	template, err := shadows.pristine.Clone()
	if err != nil {
		return nil, err
	}
	shadows.entries[name] = template
	return template, nil
}

// testHookPrepare, when set, is called each time a set is prepared.
var testHookPrepare func()

func newSet(root *template.Template, owner string) *set {
	return &set{
		owner:      owner,
		name:       root.Name(),
		root:       root,
		funcs:      FuncMap{},
		missingKey: "default",
		wrappers:   map[*template.Template]*Template{},
	}
}

// clone is a new set for root, the clone of a template of set, with set's
// settings, as html/template's Clone copies them.
func (set *set) clone(root *template.Template) *set {
	set.mu.Lock()
	defer set.mu.Unlock()
	set.cloned = true
	clone := newSet(root, set.owner)
	clone.funcs = maps.Clone(set.funcs)
	clone.missingKey = set.missingKey
	clone.delims = set.delims
	// Last: capture reads the set as soon as the recorder has it.
	captured.add(clone)
	return clone
}

// wrap returns the one *Template for std, a template of this set.
func (set *set) wrap(std *template.Template) *Template {
	if std == nil {
		return nil
	}
	set.mu.Lock()
	defer set.mu.Unlock()
	wrapper := set.wrappers[std]
	if wrapper == nil {
		wrapper = &Template{std: std, set: set}
		set.wrappers[std] = wrapper
	}
	return wrapper
}

// changed records that the set changed. Before first use that only
// means preparing again. After it, the trees may already be escaped, so
// a change that compiled code depends on (the functions it was bound
// to, the missingkey option it was compiled for, a template it renders
// being replaced, Delims) turns compiled code off for the whole set
// instead.
func (set *set) changed(edit func(set *set), compiledDependsOnIt bool) {
	set.mu.Lock()
	defer set.mu.Unlock()
	if edit != nil {
		edit(set)
	}
	if !set.executed {
		set.prep = nil
		return
	}
	if compiledDependsOnIt {
		off := *set.prep
		off.compiled = nil
		set.prep = &off
		set.ready.Store(&off)
	}
}

// use prepares the set if it has not been, and marks it executed. Only
// the first use takes the lock.
func (set *set) use() *prepared {
	if prepared := set.ready.Load(); prepared != nil {
		return prepared
	}
	set.mu.Lock()
	if prepared := set.ready.Load(); prepared != nil {
		set.mu.Unlock()
		return prepared
	}
	prepared := set.prepareLocked()
	set.executed = true
	set.ready.Store(prepared)
	set.mu.Unlock()
	set.report(prepared)
	return prepared
}

// peek prepares the set without marking it executed, for questions
// about it asked before it runs.
func (set *set) peek() *prepared {
	if prepared := set.ready.Load(); prepared != nil {
		return prepared
	}
	set.mu.Lock()
	defer set.mu.Unlock()
	return set.prepareLocked()
}

// compiledExec returns the compiled code for name, or nil to run
// html/template, and what shadow mode compares it with, nil when it is
// off.
func (set *set) compiledExec(name string) (tmplrt.Exec, *shadowSets) {
	prepared := set.use()
	exec := prepared.compiled[name]
	if exec != nil && !set.compiledRan.Load() {
		set.compiledRan.Store(true)
	}
	return exec, prepared.shadow
}

// prepareLocked fingerprints every template, before html/template has
// escaped any of them, and binds the compiled code registered for each.
func (set *set) prepareLocked() *prepared {
	if set.prep != nil {
		return set.prep
	}
	if testHookPrepare != nil {
		testHookPrepare()
	}
	trees, derived := set.treesLocked()
	env := currentEnv(set.missingKey)
	prepared := &prepared{
		compiled: map[string]tmplrt.Exec{},
		refused:  map[string]error{},
	}
	if os.Getenv(ShadowEnv) == "1" {
		pristine, err := set.root.Clone()
		prepared.shadow = &shadowSets{
			pristine: pristine,
			err:      err,
			entries:  map[string]*template.Template{},
		}
	}
	prepared.fingerprints = fingerprints(trees, set.funcs, env)
	set.prep = prepared
	if set.delims || derived {
		return prepared
	}
	set.bindLocked(prepared)
	return prepared
}

// bindLocked binds the compiled code registered for each template of
// prepared. One with no code is stale when its owner registered code
// for the set and did not leave that template out.
func (set *set) bindLocked(prepared *prepared) {
	var unsupported []string
	ownerRegistered := false
	if set.owner != "" {
		unsupported, ownerRegistered = tmplrt.Owner(set.owner)
	}
	for name, fp := range prepared.fingerprints {
		bind, ok := tmplrt.Lookup(fp)
		if !ok {
			if ownerRegistered && !slices.Contains(unsupported, fp) {
				prepared.stale = append(prepared.stale, name)
			}
			continue
		}
		exec, err := bind(maps.Clone(set.funcs))
		if err != nil {
			prepared.refused[name] = err
			continue
		}
		prepared.compiled[name] = exec
	}
	slices.Sort(prepared.stale)
}

// fingerprints is the fingerprint of every template in trees. Prepare and
// capture both take them here, so that a captured one is the one the
// runtime looks up.
func fingerprints(
	trees map[string]*parse.Tree,
	funcs FuncMap,
	env fingerprintEnv,
) map[string]string {
	fps := make(map[string]string, len(trees))
	for name := range trees {
		fps[name] = fingerprint(name, trees, funcs, env)
	}
	return fps
}

// derivedMarker is in every name html/template's escaping derives for a
// template called from another context, such as
// p$htmltemplate_stateJS_elementScript for p inside a <script>.
const derivedMarker = "$htmltemplate_"

// treesLocked is every template in the set that has a tree, by name, and
// whether the set defines a name containing derivedMarker. Escaping
// uses a template of such a name in place of a copy of the one called,
// though no {{template}} names it, so a fingerprint that follows the
// calls would not change when it does. A set like that is never
// compiled.
func (set *set) treesLocked() (trees map[string]*parse.Tree, derived bool) {
	trees = map[string]*parse.Tree{}
	for _, template := range set.root.Templates() {
		if strings.Contains(template.Name(), derivedMarker) {
			derived = true
		}
		if template.Tree != nil && template.Tree.Root != nil {
			trees[template.Name()] = template.Tree
		}
	}
	return trees, derived
}

func (set *set) trees() map[string]*parse.Tree {
	set.mu.Lock()
	defer set.mu.Unlock()
	trees, _ := set.treesLocked()
	return trees
}

// adopt follows html/template's New, which empties a template it
// replaces into a namespace of its own. member is a template still in
// the set, and becomes its root, since the old root may be the one
// replaced. A wrapper for a template that left gets a set of its own,
// so it runs that template's body, as html/template would, and not
// compiled code found under its name here.
func (set *set) adopt(member *template.Template) {
	set.mu.Lock()
	defer set.mu.Unlock()
	set.root = member
	for std, wrapper := range set.wrappers {
		if member.Lookup(std.Name()) == std {
			continue
		}
		delete(set.wrappers, std)
		alone := newSet(std, set.owner)
		alone.wrappers[std] = wrapper
		wrapper.set = alone
	}
}

// report logs, at first use, why templates run on html/template.
// Stale code is a warning, except under rastrillo dev, where a save is
// expected to outrun generate. Refused code is a known state rastrillo
// generate has reported, so it is debug only.
func (set *set) report(prepared *prepared) {
	logger := slog.Default()
	if len(prepared.stale) > 0 {
		level := slog.LevelWarn
		if os.Getenv(DevEnv) == "1" {
			level = slog.LevelDebug
		}
		logger.Log(
			context.Background(),
			level,
			staleMessage,
			"owner",
			set.owner,
			"templates",
			prepared.stale,
		)
	}
	for _, name := range slices.Sorted(maps.Keys(prepared.refused)) {
		logger.Debug(
			refusedMessage,
			"owner",
			set.owner,
			"template",
			name,
			"err",
			prepared.refused[name],
		)
	}
}

// missingKeyOption is the missingkey value opt sets, or current if opt
// is another option. html/template has already rejected a bad one.
func missingKeyOption(opt, current string) string {
	key, value, _ := strings.Cut(opt, "=")
	if key != "missingkey" {
		return current
	}
	if value == "invalid" {
		return "default"
	}
	return value
}
