package generate

import (
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"

	rastrillo "github.com/carlosframework/rastrillo"
)

// ResourceSpec is one loaded manifest: the static typed shape plus
// where it came from, which decides how generated code references it —
// a TOML manifest is lowered into gen/manifest and referenced there; a
// .go manifest stays the app's own value (its closures must run) and is
// referenced as <module>/manifest.<VarName>.
type ResourceSpec struct {
	Res        rastrillo.Resource
	VarName    string
	SourceFile string
	FromGo     bool
}

// LoadManifests reads manifestDir's *.toml and *.go files. A missing
// directory is simply "no manifests". Every loaded spec is validated
// here, at generate time — a bad manifest never reaches serving.
func LoadManifests(manifestDir string) ([]ResourceSpec, error) {
	entries, err := os.ReadDir(manifestDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var specs []ResourceSpec
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(manifestDir, e.Name())
		switch {
		case strings.HasSuffix(e.Name(), ".toml"):
			src, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			res, err := decodeManifestTOML(string(src), path)
			if err != nil {
				return nil, err
			}
			specs = append(specs, ResourceSpec{
				Res: res, VarName: upperCamel(res.Name), SourceFile: path,
			})
		case strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go"):
			gs, err := extractGoManifests(path)
			if err != nil {
				return nil, err
			}
			specs = append(specs, gs...)
		}
	}

	sort.Slice(specs, func(i, j int) bool { return specs[i].Res.Name < specs[j].Res.Name })
	seen := map[string]string{}
	for _, s := range specs {
		if err := s.Res.Validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", s.SourceFile, err)
		}
		if prev, ok := seen[s.Res.Name]; ok {
			return nil, fmt.Errorf("resource %q declared in both %s and %s", s.Res.Name, prev, s.SourceFile)
		}
		seen[s.Res.Name] = s.SourceFile
	}
	return specs, nil
}

// upperCamel turns a snake_case resource name into the generated var
// name ("ticket_types" → "TicketTypes").
func upperCamel(name string) string {
	parts := strings.Split(name, "_")
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}

// FindCollisions reports routes claimed more than once across any mix
// of hand-written and manifest-generated actions — one check over the
// union, so a manifest and a hand-written file fighting over a route
// fails as loudly as two files would.
func FindCollisions(actions []Action) []Collision {
	byRoute := map[string][]string{}
	for _, a := range actions {
		byRoute[a.Route] = append(byRoute[a.Route], a.SourcePath)
	}
	var out []Collision
	for route, sources := range byRoute {
		if len(sources) > 1 {
			out = append(out, Collision{Route: route, Sources: sources})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Route < out[j].Route })
	return out
}

// ManifestAction is one generated screen action: the routing Action
// plus the file body to write under gen/actions/<GenDir>/.
type ManifestAction struct {
	Action
	Content []byte
}

// screenDef names one screen in a resource's canonical set.
type screenDef struct {
	fn     string // the screens package function
	method string
	subdir string // relative to the route dir; "" or "[id]"
	name   string // the action filename stem ("index", "new", ...)
}

// screenSet is the canonical screen list for a resource (§3, §9):
// List → Show → Edit/New, the two-action save invariant, and the
// confirm-page delete flow.
func screenSet(res rastrillo.Resource) []screenDef {
	defs := []screenDef{
		{"List", "GET", "", "index"},
		{"NewForm", "GET", "", "new"},
		{"Create", "POST", "", "index"},
		{"Show", "GET", "[id]", "index"},
		{"EditForm", "GET", "[id]", "edit"},
		{"ConfirmDelete", "GET", "[id]", "delete"},
		{"Delete", "POST", "[id]", "delete"},
	}
	if len(res.Form.Advanced) == 0 {
		defs = append(defs, screenDef{"Save", "POST", "[id]", "index"})
	} else {
		defs = append(defs,
			screenDef{"SaveBasics", "POST", "[id]", "edit-basics"},
			screenDef{"SaveAdvanced", "POST", "[id]", "edit-advanced"})
	}
	return defs
}

// routeDir converts a manifest route into the actions/ directory that
// would claim it by hand ("/admin/{slug}/tickets" → "admin/[slug]/tickets").
func routeDir(route string) string {
	segs := strings.Split(strings.TrimPrefix(route, "/"), "/")
	for i, s := range segs {
		if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
			segs[i] = "[" + s[1:len(s)-1] + "]"
		}
	}
	return strings.Join(segs, "/")
}

// ManifestActions computes every generated action for specs, skipping
// each one whose computed path already exists in actionsDir — the
// override-by-existence rule (§3: "a hand-written file at the generated
// path wins, silently, by existence"). It returns the actions to emit
// and the relative paths it skipped (for generate's report).
func ManifestActions(module, actionsDir string, specs []ResourceSpec) ([]ManifestAction, []string, error) {
	var out []ManifestAction
	var skipped []string
	for _, spec := range specs {
		dir := routeDir(spec.Res.Route)
		for _, def := range screenSet(spec.Res) {
			fileDir := dir
			if def.subdir != "" {
				fileDir = dir + "/" + def.subdir
			}
			rel := fileDir + "/" + def.name + "." + def.method + ".go"
			if _, err := os.Stat(filepath.Join(actionsDir, filepath.FromSlash(rel))); err == nil {
				skipped = append(skipped, rel)
				continue
			}
			route, err := routeFor(filepath.FromSlash(fileDir), def.name, def.method)
			if err != nil {
				return nil, nil, fmt.Errorf("%s: %w", spec.SourceFile, err)
			}
			a := Action{
				SourcePath:  "manifest:" + spec.Res.Name + "/" + def.name + "." + def.method,
				Method:      def.method,
				Route:       route,
				PackageName: packageNameFor(rel),
				GenDir:      genDirFor(filepath.FromSlash(fileDir), def.name, def.method),
			}
			content, err := manifestActionFile(module, a, spec, def)
			if err != nil {
				return nil, nil, err
			}
			out = append(out, ManifestAction{Action: a, Content: content})
		}
	}
	return out, skipped, nil
}

// manifestActionFile renders one generated screen action.
func manifestActionFile(module string, a Action, spec ResourceSpec, def screenDef) ([]byte, error) {
	imp := fmt.Sprintf("genmanifest %q", module+"/gen/manifest")
	ref := "genmanifest." + spec.VarName
	if spec.FromGo {
		imp = fmt.Sprintf("manifest %q", module+"/manifest")
		ref = "manifest." + spec.VarName
	}
	src := fmt.Sprintf(`// Code generated by rastrillo generate from manifest %q (%s). DO NOT EDIT.
//
// To take this screen over by hand, write your own file at
// actions/%s (with the //go:build %s constraint) —
// the generator skips this one from then on. Ejecting is copying this
// file there and making it yours.
package %s

import (
	"net/http"

	"github.com/carlosframework/rastrillo"
	"github.com/carlosframework/rastrillo/screens"

	%s
)

// Handle is %s.
func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
	screens.%s(ctx, w, r, %s)
}
`,
		spec.Res.Name, filepath.Base(spec.SourceFile),
		strings.TrimPrefix(a.SourcePath, "manifest:"), BuildTag,
		a.PackageName, imp, a.Route, def.fn, ref)
	return format.Source([]byte(src))
}

// ManifestPackage renders gen/manifest/manifest.go: the TOML manifests
// lowered to typed values, plus Resources() and Migrations() over the
// whole set (Go manifests included, referenced from the app's own
// package so their function values survive).
func ManifestPackage(module string, specs []ResourceSpec) ([]byte, error) {
	anyGo := false
	for _, s := range specs {
		if s.FromGo {
			anyGo = true
		}
	}

	var b strings.Builder
	b.WriteString("// Code generated by rastrillo generate. DO NOT EDIT.\n\n")
	b.WriteString("// Package manifest carries the app's TOML manifests lowered to their\n")
	b.WriteString("// typed-Go form — the identical rastrillo.Resource values a .go\n")
	b.WriteString("// manifest declares by hand (one pipeline, two spellings) — and the\n")
	b.WriteString("// aggregate views over every manifest, both spellings.\n")
	b.WriteString("package manifest\n\n")
	b.WriteString("import (\n")
	b.WriteString("\trastrillo \"github.com/carlosframework/rastrillo\"\n")
	if anyGo {
		fmt.Fprintf(&b, "\n\tappmanifest %q\n", module+"/manifest")
	}
	b.WriteString(")\n\n")

	for _, s := range specs {
		if s.FromGo {
			continue
		}
		fmt.Fprintf(&b, "// %s is lowered from %s.\n", s.VarName, filepath.Base(s.SourceFile))
		fmt.Fprintf(&b, "var %s = %s\n\n", s.VarName, resourceLiteral(s.Res))
	}

	b.WriteString("// Resources returns every manifest resource.\n")
	b.WriteString("func Resources() []rastrillo.Resource {\n\treturn []rastrillo.Resource{\n")
	for _, s := range specs {
		if s.FromGo {
			fmt.Fprintf(&b, "\t\tappmanifest.%s,\n", s.VarName)
		} else {
			fmt.Fprintf(&b, "\t\t%s,\n", s.VarName)
		}
	}
	b.WriteString("\t}\n}\n\n")

	b.WriteString("// Migrations returns the additive migration every Exclusive resource\n")
	b.WriteString("// needs — append to Options.Migrations (Mergeable resources ride\n")
	b.WriteString("// eventlog.Migrations instead).\n")
	b.WriteString("func Migrations() []string {\n")
	b.WriteString("\tvar out []string\n")
	b.WriteString("\tfor _, r := range Resources() {\n")
	b.WriteString("\t\tif m := r.Migration(); m != \"\" {\n\t\t\tout = append(out, m)\n\t\t}\n")
	b.WriteString("\t}\n\treturn out\n}\n")

	return format.Source([]byte(b.String()))
}

// resourceLiteral renders a Resource as Go source. Only the pure-data
// subset exists here — a TOML manifest cannot carry a function value.
func resourceLiteral(r rastrillo.Resource) string {
	var b strings.Builder
	b.WriteString("rastrillo.Resource{\n")
	fmt.Fprintf(&b, "\tName:  %q,\n", r.Name)
	fmt.Fprintf(&b, "\tRoute: %q,\n", r.Route)
	if r.Store == rastrillo.Mergeable {
		b.WriteString("\tStore: rastrillo.Mergeable,\n")
	}
	if len(r.List.Columns) > 0 || r.List.Search || len(r.List.Filter) > 0 {
		b.WriteString("\tList: rastrillo.List{\n")
		if len(r.List.Columns) > 0 {
			b.WriteString("\t\tColumns: []rastrillo.Column{\n")
			for _, c := range r.List.Columns {
				fmt.Fprintf(&b, "\t\t\t{Field: %q, Kind: rastrillo.%s},\n", c.Field, c.Kind.GoName())
			}
			b.WriteString("\t\t},\n")
		}
		if r.List.Search {
			b.WriteString("\t\tSearch: true,\n")
		}
		if len(r.List.Filter) > 0 {
			fmt.Fprintf(&b, "\t\tFilter: %#v,\n", r.List.Filter)
		}
		b.WriteString("\t},\n")
	}
	b.WriteString("\tForm: rastrillo.Form{\n")
	b.WriteString("\t\tBasics: " + fieldsLiteral(r.Form.Basics) + ",\n")
	if len(r.Form.Advanced) > 0 {
		b.WriteString("\t\tAdvanced: " + fieldsLiteral(r.Form.Advanced) + ",\n")
	}
	b.WriteString("\t},\n")
	if r.Delete.Confirm != "" {
		fmt.Fprintf(&b, "\tDelete: rastrillo.Delete{Confirm: %q},\n", r.Delete.Confirm)
	}
	b.WriteString("}")
	return b.String()
}

func fieldsLiteral(fields []rastrillo.Field) string {
	var b strings.Builder
	b.WriteString("[]rastrillo.Field{\n")
	for _, f := range fields {
		fmt.Fprintf(&b, "\t\t\t{Name: %q", f.Name)
		if f.Kind != rastrillo.Text {
			fmt.Fprintf(&b, ", Kind: rastrillo.%s", f.Kind.GoName())
		}
		if f.Required {
			b.WriteString(", Required: true")
		}
		if f.Derived {
			b.WriteString(", Derived: true")
		}
		if len(f.Options) > 0 {
			fmt.Fprintf(&b, ", Options: %#v", f.Options)
		}
		b.WriteString("},\n")
	}
	b.WriteString("\t\t}")
	return b.String()
}
