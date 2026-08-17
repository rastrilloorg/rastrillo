package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	rastrillo "github.com/carlosframework/rastrillo"
)

// designDocTOML is the design doc §3 example, verbatim shape.
const designDocTOML = `# app/manifest/ticket_types.toml
name  = "ticket_types"
route = "/admin/ticket_types"
store = "exclusive"

[list]
columns = [{ field = "Name", kind = "text" }, { field = "Price", kind = "money" }]
search  = true
filter  = ["Status"]

[form]
basics   = [{ name = "Name", required = true }, { name = "Price", kind = "money" }, { name = "Status", kind = "select", options = ["draft", "live"] }]
advanced = [{ name = "MaxPerOrder", kind = "money" }]
`

func TestDecodeManifestTOML(t *testing.T) {
	res, err := decodeManifestTOML(designDocTOML, "ticket_types.toml")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Name != "ticket_types" || res.Route != "/admin/ticket_types" || res.Store != rastrillo.Exclusive {
		t.Fatalf("header: %+v", res)
	}
	if len(res.List.Columns) != 2 || res.List.Columns[1].Kind != rastrillo.Money {
		t.Fatalf("columns: %+v", res.List.Columns)
	}
	if !res.List.Search || len(res.List.Filter) != 1 {
		t.Fatalf("search/filter: %+v", res.List)
	}
	if len(res.Form.Basics) != 3 || !res.Form.Basics[0].Required {
		t.Fatalf("basics: %+v", res.Form.Basics)
	}
	if res.Form.Basics[2].Kind != rastrillo.Select || len(res.Form.Basics[2].Options) != 2 {
		t.Fatalf("select field: %+v", res.Form.Basics[2])
	}
	if len(res.Form.Advanced) != 1 {
		t.Fatalf("advanced: %+v", res.Form.Advanced)
	}
	if err := res.Validate(); err != nil {
		t.Fatalf("the design-doc example must validate: %v", err)
	}
}

func TestDecodeManifestTOMLErrors(t *testing.T) {
	cases := []string{
		"name = \"x\"\nroute = /nope\n",              // unquoted value
		"[mystery]\n",                                // unknown section
		"name = \"x\"\nbogus = \"y\"\n",              // unknown key
		"[list]\ncolumns = [{ kind = \"text\" }]\n",  // column without field
		"[form]\nbasics = [{ name = \"A\" } extra\n", // malformed array
		"store = \"wat\"\n",                          // unknown store
	}
	for _, src := range cases {
		if _, err := decodeManifestTOML(src, "bad.toml"); err == nil {
			t.Errorf("decoded %q without error", src)
		}
	}
}

const goManifestSrc = `package manifest

import (
	"html/template"

	"github.com/carlosframework/rastrillo"
)

var Gauges = rastrillo.Resource{
	Name:  "gauges",
	Route: "/admin/gauges",
	Store: rastrillo.Mergeable,
	List: rastrillo.List{
		Columns: []rastrillo.Column{
			{Field: "Label", Kind: rastrillo.Text},
			{Field: "Level", Kind: rastrillo.Meter, Render: levelBar},
		},
	},
	Form: rastrillo.Form{
		Basics: []rastrillo.Field{{Name: "Label", Required: true}},
	},
	Delete: rastrillo.Delete{Confirm: "Drop this gauge?"},
}

func levelBar(row map[string]any) template.HTML { return "" }
`

func TestExtractGoManifests(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gauges.go")
	if err := os.WriteFile(path, []byte(goManifestSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	specs, err := extractGoManifests(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("specs = %d", len(specs))
	}
	s := specs[0]
	if s.VarName != "Gauges" || !s.FromGo {
		t.Fatalf("spec: %+v", s)
	}
	if s.Res.Store != rastrillo.Mergeable || s.Res.Delete.Confirm != "Drop this gauge?" {
		t.Fatalf("resource: %+v", s.Res)
	}
	if s.Res.List.Columns[1].Render == nil {
		t.Fatal("the Render function's presence must survive extraction (validation depends on it)")
	}
	if err := s.Res.Validate(); err != nil {
		t.Fatalf("extracted resource must validate: %v", err)
	}
}

func TestLoadManifestsRejectsDuplicatesAndInvalid(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.toml"), []byte("name = \"things\"\nroute = \"/things\"\n[form]\nbasics = [{ name = \"Name\" }]\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.toml"), []byte("name = \"things\"\nroute = \"/other\"\n[form]\nbasics = [{ name = \"Name\" }]\n"), 0o644)
	if _, err := LoadManifests(dir); err == nil || !strings.Contains(err.Error(), "declared in both") {
		t.Fatalf("duplicate names: %v", err)
	}

	dir2 := t.TempDir()
	os.WriteFile(filepath.Join(dir2, "bad.toml"), []byte("name = \"BadName\"\nroute = \"/x\"\n[form]\nbasics = [{ name = \"Name\" }]\n"), 0o644)
	if _, err := LoadManifests(dir2); err == nil {
		t.Fatal("an invalid manifest must fail at generate time")
	}

	if specs, err := LoadManifests(filepath.Join(dir2, "missing")); err != nil || specs != nil {
		t.Fatalf("a missing manifest dir is just 'no manifests': %v, %v", specs, err)
	}
}

func TestManifestActions(t *testing.T) {
	manifestDir := t.TempDir()
	os.WriteFile(filepath.Join(manifestDir, "ticket_types.toml"), []byte(designDocTOML), 0o644)
	specs, err := LoadManifests(manifestDir)
	if err != nil {
		t.Fatal(err)
	}

	actionsDir := t.TempDir()
	mas, skipped, err := ManifestActions("demo", actionsDir, specs)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v", skipped)
	}
	routes := map[string]bool{}
	for _, ma := range mas {
		routes[ma.Route] = true
		if !strings.Contains(string(ma.Content), "genmanifest.TicketTypes") {
			t.Fatalf("action content must reference the lowered value:\n%s", ma.Content)
		}
	}
	// Advanced section present → two independent save actions (§3).
	for _, want := range []string{
		"GET /admin/ticket_types",
		"GET /admin/ticket_types/new",
		"POST /admin/ticket_types",
		"GET /admin/ticket_types/{id}",
		"GET /admin/ticket_types/{id}/edit",
		"POST /admin/ticket_types/{id}/edit-basics",
		"POST /admin/ticket_types/{id}/edit-advanced",
		"GET /admin/ticket_types/{id}/delete",
		"POST /admin/ticket_types/{id}/delete",
	} {
		if !routes[want] {
			t.Errorf("missing route %s (have %v)", want, routes)
		}
	}
	if routes["POST /admin/ticket_types/{id}"] {
		t.Error("a manifest with Advanced must not also emit the single-save action")
	}

	// Override-by-existence: a hand-written file at the computed path
	// silently claims its screen.
	handPath := filepath.Join(actionsDir, "admin", "ticket_types", "index.GET.go")
	os.MkdirAll(filepath.Dir(handPath), 0o755)
	os.WriteFile(handPath, []byte("//go:build rastrillo_actions\n\npackage actions\n"), 0o644)
	mas2, skipped2, err := ManifestActions("demo", actionsDir, specs)
	if err != nil {
		t.Fatal(err)
	}
	if len(mas2) != len(mas)-1 || len(skipped2) != 1 || skipped2[0] != "admin/ticket_types/index.GET.go" {
		t.Fatalf("override-by-existence: %d actions, skipped %v", len(mas2), skipped2)
	}
}

func TestManifestPackage(t *testing.T) {
	manifestDir := t.TempDir()
	os.WriteFile(filepath.Join(manifestDir, "ticket_types.toml"), []byte(designDocTOML), 0o644)
	os.WriteFile(filepath.Join(manifestDir, "gauges.go"), []byte(goManifestSrc), 0o644)
	specs, err := LoadManifests(manifestDir)
	if err != nil {
		t.Fatal(err)
	}
	src, err := ManifestPackage("demo", specs)
	if err != nil {
		t.Fatal(err)
	}
	out := string(src)
	for _, want := range []string{
		"var TicketTypes = rastrillo.Resource{",
		`appmanifest "demo/manifest"`,
		"appmanifest.Gauges,",
		"func Resources() []rastrillo.Resource",
		"func Migrations() []string",
		`Options: []string{"draft", "live"}`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("gen/manifest missing %q:\n%s", want, out)
		}
	}
}

func TestResourceMigration(t *testing.T) {
	res, _ := decodeManifestTOML(designDocTOML, "t.toml")
	m := res.Migration()
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS ticket_types",
		"name TEXT NOT NULL DEFAULT ''",
		"price INTEGER NOT NULL DEFAULT 0",
		"max_per_order INTEGER NOT NULL DEFAULT 0",
		"created_at TEXT NOT NULL",
	} {
		if !strings.Contains(m, want) {
			t.Errorf("migration missing %q:\n%s", want, m)
		}
	}
	res.Store = rastrillo.Mergeable
	if res.Migration() != "" {
		t.Error("a Mergeable resource has no table of its own")
	}
}
