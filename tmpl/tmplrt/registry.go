package tmplrt

import (
	"bytes"
	"slices"
	"sync"
)

// GeneratorVersion is the version of the code rastrillo generate
// writes. Bump it with any compiler change that alters generated output.
// tmpl takes it into every fingerprint, so code an older generator wrote
// matches nothing after an upgrade: the set runs html/template and logs
// the stale warning, instead of running code that behaves the old way.
const GeneratorVersion = 1

// Exec renders one compiled template into buffer. It is what html/template's
// Execute would have written for the same data, and it fails where
// Execute would.
type Exec func(buffer *bytes.Buffer, data any) error

// Bind connects compiled code to a set's FuncMap. A set calls it once,
// the first time it is used, and keeps the Exec. It type-asserts each
// function the template calls; an error means the functions are not
// the ones the code was compiled against, and the set runs that
// template on html/template instead.
type Bind func(funcs map[string]any) (Exec, error)

var registry struct {
	mu     sync.RWMutex
	binds  map[string]Bind
	owners map[string][]string
}

// Register makes bind the compiled code for templates with this
// fingerprint. Generated code calls it from init. The first registration
// of a fingerprint wins: two packages that use the same partials, from
// the same source, compile the same code for them.
func Register(fingerprint string, bind Bind) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.binds == nil {
		registry.binds = make(map[string]Bind)
	}
	if _, taken := registry.binds[fingerprint]; !taken {
		registry.binds[fingerprint] = bind
	}
}

// Lookup returns the compiled code registered for a fingerprint.
func Lookup(fingerprint string) (Bind, bool) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	bind, ok := registry.binds[fingerprint]
	return bind, ok
}

// RegisterOwner records that the package at pkgPath has compiled code,
// and that rastrillo generate left the templates with the fingerprints
// in unsupported on html/template on purpose. Each generated file calls
// it once from init. A set owned by a registered package that finds no
// code for some other fingerprint knows its compiled code is out of
// date. Fingerprints, not names: two sets of one package can each have a
// template called layout, one compiled and one not, and a name would
// exempt both. A second call for the same package adds to the first.
func RegisterOwner(pkgPath string, unsupported []string) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.owners == nil {
		registry.owners = make(map[string][]string)
	}
	fps := registry.owners[pkgPath]
	for _, fp := range unsupported {
		if !slices.Contains(fps, fp) {
			fps = append(fps, fp)
		}
	}
	if fps == nil {
		fps = []string{}
	}
	registry.owners[pkgPath] = fps
}

// Owner reports whether pkgPath called RegisterOwner, and the
// fingerprints it left uncompiled.
func Owner(pkgPath string) (unsupported []string, ok bool) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	fps, ok := registry.owners[pkgPath]
	return slices.Clone(fps), ok
}
