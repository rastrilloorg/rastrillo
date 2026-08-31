package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
)

// repoRoot returns this repo's absolute root, computed from this
// file's own location (internal/manifest/goeval_test.go) rather than
// the test's working directory, so it's stable regardless of how `go
// test` is invoked.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// newScratchModule builds a tiny standalone module in t.TempDir() that
// requires and replaces amadan.net/rastrillo/rastrillo with this
// repo, so evalGo's `go run` driver has a real module context to build
// in. Returns the module's root directory.
func newScratchModule(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	goMod := "module scratch\n\ngo 1.25.0\n\nrequire amadan.net/rastrillo/rastrillo v0.0.0\n\nreplace amadan.net/rastrillo/rastrillo => " + repoRoot(t) + "\n"
	writeFile(t, root, "go.mod", goMod)
	return root
}

func TestEvalGoFindsExportedResources(t *testing.T) {
	root := newScratchModule(t)
	dir := filepath.Join(root, "manifest")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "notes.go", `package manifest

import "amadan.net/rastrillo/rastrillo"

var Notes = rastrillo.Resource{
	Name:  "notes",
	Route: "/admin/notes",
	Store: rastrillo.Exclusive,
	List: rastrillo.List{
		Columns: []rastrillo.Column{
			{Field: "Title", Kind: rastrillo.Text},
			{Field: "Price", Kind: rastrillo.Money},
		},
		Search: true,
		Filter: []string{"Title"},
	},
	Form: rastrillo.Form{
		Basics: []rastrillo.Field{
			{Name: "Title"},
			{Name: "Body", Kind: rastrillo.Textarea},
		},
		Advanced: []rastrillo.Field{
			{Name: "Price", Kind: rastrillo.Money},
		},
	},
}
`)

	got, err := goEval(root, dir)
	if err != nil {
		t.Fatalf("goEval: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("goEval returned %d source(s), want 1: %#v", len(got), got)
	}
	if !reflect.DeepEqual(got[0].resource, fixtureResource()) {
		t.Errorf("goEval resource = %#v, want %#v", got[0].resource, fixtureResource())
	}
	wantFile := filepath.Join(dir, "notes.go")
	if got[0].file != wantFile {
		t.Errorf("goEval file = %q, want %q", got[0].file, wantFile)
	}
}

func TestEvalGoCompileErrorSurfaces(t *testing.T) {
	root := newScratchModule(t)
	dir := filepath.Join(root, "manifest")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "broken.go", `package manifest

import "amadan.net/rastrillo/rastrillo"

var Broken = rastrillo.Resource{
	Name:  "broken",
	Route: "/admin/broken",
	List: rastrillo.List{
		Columns: []rastrillo.Column{{Field: "Title"}},
		Serach:  true,
	},
}
`)

	_, err := goEval(root, dir)
	if err == nil {
		t.Fatal("goEval accepted a manifest with a compile error")
	}
	if !strings.Contains(err.Error(), "Serach") {
		t.Errorf("error %q missing %q (the compiler's message, verbatim)", err, "Serach")
	}
}

func TestEvalGoIgnoresUnexportedAndOtherTypes(t *testing.T) {
	root := newScratchModule(t)
	dir := filepath.Join(root, "manifest")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "other.go", `package manifest

import "amadan.net/rastrillo/rastrillo"

var notExported = rastrillo.Resource{
	Name:  "not_exported",
	Route: "/admin/not-exported",
	List:  rastrillo.List{Columns: []rastrillo.Column{{Field: "Title"}}},
}

var Count = 42

var Filters = rastrillo.List{
	Columns: []rastrillo.Column{{Field: "Title"}},
}
`)

	got, err := goEval(root, dir)
	if err != nil {
		t.Fatalf("goEval: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("goEval = %#v, want none (unexported var, int var, and a rastrillo.List var should all be ignored)", got)
	}
}

func TestGoAndTOMLSameManifestSameArtifact(t *testing.T) {
	tomlDir := t.TempDir()
	writeFile(t, tomlDir, "notes.toml", fixtureTOML)

	tomlResources, err := Load("", tomlDir)
	if err != nil {
		t.Fatalf("Load (TOML): %v", err)
	}

	root := newScratchModule(t)
	goDir := filepath.Join(root, "manifest")
	if err := os.MkdirAll(goDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, goDir, "notes.go", `package manifest

import "amadan.net/rastrillo/rastrillo"

var Notes = rastrillo.Resource{
	Name:  "notes",
	Route: "/admin/notes",
	Store: rastrillo.Exclusive,
	List: rastrillo.List{
		Columns: []rastrillo.Column{
			{Field: "Title", Kind: rastrillo.Text},
			{Field: "Price", Kind: rastrillo.Money},
		},
		Search: true,
		Filter: []string{"Title"},
	},
	Form: rastrillo.Form{
		Basics: []rastrillo.Field{
			{Name: "Title"},
			{Name: "Body", Kind: rastrillo.Textarea},
		},
		Advanced: []rastrillo.Field{
			{Name: "Price", Kind: rastrillo.Money},
		},
	},
}
`)

	goResources, err := Load(root, goDir)
	if err != nil {
		t.Fatalf("Load (Go): %v", err)
	}

	tomlArtifact := Artifact(tomlResources)
	goArtifact := Artifact(goResources)
	if string(tomlArtifact) != string(goArtifact) {
		t.Errorf("Artifact(TOML) = %s\nArtifact(Go) = %s\nwant identical bytes", tomlArtifact, goArtifact)
	}
}

func TestArtifactIsStableAndSorted(t *testing.T) {
	first := fixtureResource()
	if err := (&first).Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	second := fixtureResource()
	second.Name = "zzz_second"
	second.Route = "/admin/zzz-second"
	if err := (&second).Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	// Given in reverse order, Artifact must sort by Name.
	got := Artifact([]rastrillo.Resource{second, first})
	var decoded []rastrillo.Resource
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("Unmarshal(Artifact output): %v", err)
	}
	if len(decoded) != 2 || decoded[0].Name != "notes" || decoded[1].Name != "zzz_second" {
		t.Fatalf("Artifact not sorted by name, got names %v", []string{decoded[0].Name, decoded[1].Name})
	}

	// The golden artifact string for the fixture alone, pinned exactly
	// — Task 1's JSON tags, two-space indent via json.MarshalIndent
	// (which fully expands nested objects/arrays, one field per line;
	// this golden's whitespace was taken from the marshaler's actual
	// output, not typed by hand), plus a trailing newline.
	want := `[
  {
    "name": "notes",
    "route": "/admin/notes",
    "store": "exclusive",
    "list": {
      "columns": [
        {
          "field": "Title",
          "kind": "text"
        },
        {
          "field": "Price",
          "kind": "money"
        }
      ],
      "search": true,
      "filter": [
        "Title"
      ],
      "filters": null
    },
    "form": {
      "basics": [
        {
          "name": "Title",
          "kind": "text",
          "required": false
        },
        {
          "name": "Body",
          "kind": "textarea",
          "required": false
        }
      ],
      "advanced": [
        {
          "name": "Price",
          "kind": "money",
          "required": false
        }
      ]
    }
  }
]
`

	singleGot := Artifact([]rastrillo.Resource{first})
	if string(singleGot) != want {
		t.Errorf("Artifact golden mismatch:\ngot:\n%s\nwant:\n%s", singleGot, want)
	}
}
