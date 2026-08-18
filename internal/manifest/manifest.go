// Package manifest owns manifest discovery and the JSON artifact
// (gen/manifest.json) the generator consumes (design doc §3). TOML is
// the pure-data serialization of the same rastrillo.Resource struct a
// Go manifest builds via code; both decode into Resource and are
// merged into one validated, deterministically ordered set.
package manifest

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/carlosframework/rastrillo"
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
	for _, s := range sources {
		if err := s.resource.Validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", s.file, err)
		}
		if prev, ok := names[s.resource.Name]; ok {
			return nil, fmt.Errorf("duplicate resource name %q in %s and %s", s.resource.Name, prev, s.file)
		}
		names[s.resource.Name] = s.file
		if prev, ok := routes[s.resource.Route]; ok {
			return nil, fmt.Errorf("duplicate route %q in %s and %s", s.resource.Route, prev, s.file)
		}
		routes[s.resource.Route] = s.file
		resources = append(resources, s.resource)
	}

	sort.Slice(resources, func(i, j int) bool { return resources[i].Name < resources[j].Name })
	return resources, nil
}
