// Package manifest owns manifest discovery and the JSON artifact
// (gen/manifest.json) the generator consumes (design doc §3). TOML is
// the pure-data serialization of the same rastrillo.Resource struct a
// Go manifest builds via code; both decode into Resource and are
// merged into one validated, deterministically ordered set.
package manifest

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/currency"
	"amadan.net/rastrillo/rastrillo/internal/manifesterr"
	"amadan.net/rastrillo/rastrillo/internal/opsnames"
	"github.com/BurntSushi/toml"
)

// decodeTOML strictly decodes one manifest file into a Resource. Any
// key the struct doesn't declare is an error naming it — manifests are
// data, not free-form config, and a typo must not silently vanish.
func decodeTOML(path string) (rastrillo.Resource, error) {
	var r rastrillo.Resource
	md, err := toml.DecodeFile(path, &r)
	if err != nil {
		return rastrillo.Resource{}, fmt.Errorf("%s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		return rastrillo.Resource{}, fmt.Errorf("%s: unknown key(s): %s", path, strings.Join(keys, ", "))
	}
	return r, nil
}

// reservedApp is Appendix C.2's op.generate.reserved_app.
const reservedApp = `manifest/app.toml holds the app's locales, so no resource can be named "app"`

// sameGoName refuses two resources whose names become one Go
// identifier: the first name and file, the second, and the identifier.
const sameGoName = "resources %q (%s) and %q (%s) both become %s in generated code; rename one"

// ineligible is Appendix C.2's op.generate.ineligible.
const ineligible = "warning: manifest/%s.toml: %s is still accepted for storage but is no longer issued; choose a current currency for new records"

// IneligibleDefaults returns a warning for each resource whose default
// currency is stored but no longer issued. It is a warning and not a
// Validate error so that a currency's withdrawal never stops an
// unchanged manifest from generating; rs must have passed Load, so each
// Currency is already canonical.
func IneligibleDefaults(rs []rastrillo.Resource) []string {
	var out []string
	for _, r := range rs {
		if r.Currency != "" && !currency.EligibleDefault(r.Currency) {
			out = append(out, fmt.Sprintf(ineligible, r.Name, r.Currency))
		}
	}
	return out
}

// source pairs a decoded resource with the file it came from, so
// validation and duplicate errors can name it.
type source struct {
	resource rastrillo.Resource
	file     string
}

// Load reads every *.toml manifest in dir plus every Go manifest under
// moduleRoot (via goEval), validates each resource, and rejects
// duplicate Names or Routes across the whole set regardless of source
// — naming both files where possible. Results are sorted by Name for
// deterministic downstream output. A missing dir is not an error:
// manifests are optional, per route.
func Load(moduleRoot, dir string) ([]rastrillo.Resource, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.toml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)

	var sources []source
	for _, path := range paths {
		// app.toml is the app's locale declaration (LoadApp), not a
		// resource: decoding it as one fails on its unknown keys.
		if filepath.Base(path) == "app.toml" {
			continue
		}
		r, err := decodeTOML(path)
		if err != nil {
			return nil, err
		}
		sources = append(sources, source{r, path})
	}

	goSources, err := goEval(moduleRoot, dir)
	if err != nil {
		return nil, err
	}
	sources = append(sources, goSources...)

	var resources []rastrillo.Resource
	names := map[string]string{}
	routes := map[string]string{}
	// Keyed by the title-cased name, because that is the identifier the
	// parity package declares for every resource in one package
	// (RunReport2s): report2s and report_2s are distinct names on
	// distinct routes and the same Go name, and the app stops building.
	// The other per-resource packages (gen/ops, gen/store, gen/api) are
	// one per literal name, which the names map already keeps apart.
	titled := map[string]source{}
	for _, s := range sources {
		if err := s.resource.Validate(); err != nil {
			// ErrNoCurrency reads as one sentence with its file,
			// matching Appendix C.2's op.generate.no_currency line:
			// "manifest/foo.toml has a money field...", not
			// "manifest/foo.toml: has a money field...", whose colon
			// would read as introducing a clause.
			if errors.Is(err, manifesterr.ErrNoCurrency) {
				return nil, fmt.Errorf("%s %w", s.file, err)
			}
			return nil, fmt.Errorf("%s: %w", s.file, err)
		}
		// Operator messages send an author to manifest/<name>.toml
		// (op.legacy.undeclared, op.generate.no_currency), so a resource
		// named app would send them to the locales file instead.
		if s.resource.Name == "app" {
			return nil, fmt.Errorf("%s: %s", s.file, reservedApp)
		}
		if prev, ok := names[s.resource.Name]; ok {
			return nil, fmt.Errorf("duplicate resource name %q in %s and %s", s.resource.Name, prev, s.file)
		}
		names[s.resource.Name] = s.file
		if prev, ok := routes[s.resource.Route]; ok {
			return nil, fmt.Errorf("duplicate route %q in %s and %s", s.resource.Route, prev, s.file)
		}
		routes[s.resource.Route] = s.file
		if prev, ok := titled[opsnames.Pascal(s.resource.Name)]; ok {
			return nil, fmt.Errorf(sameGoName, prev.resource.Name, prev.file, s.resource.Name, s.file, opsnames.Pascal(s.resource.Name))
		}
		titled[opsnames.Pascal(s.resource.Name)] = s
		resources = append(resources, s.resource)
	}

	sort.Slice(resources, func(i, j int) bool { return resources[i].Name < resources[j].Name })
	return resources, nil
}
