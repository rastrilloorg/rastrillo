package manifest

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/carlosframework/rastrillo"
)

// fixtureHeader/fixtureBody split the plan's fixture manifest so tests
// can splice content in at the top level (before any [table]).
const fixtureHeader = `name  = "notes"
route = "/admin/notes"
store = "exclusive"
`

const fixtureBody = `
[list]
columns = [{ field = "Title", kind = "text" }, { field = "Price", kind = "money" }]
search  = true
filter  = ["Title"]

[form]
basics   = [{ name = "Title" }, { name = "Body", kind = "textarea" }]
advanced = [{ name = "Price", kind = "money" }]
`

const fixtureTOML = fixtureHeader + fixtureBody

func fixtureResource() rastrillo.Resource {
	return rastrillo.Resource{
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
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDecodeTOMLFixture(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "notes.toml", fixtureTOML)

	got, err := decodeTOML(path)
	if err != nil {
		t.Fatalf("decodeTOML: %v", err)
	}
	want := fixtureResource()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("decodeTOML = %#v, want %#v", got, want)
	}
}

func TestDecodeTOMLUnknownKeyErrors(t *testing.T) {
	dir := t.TempDir()
	src := fixtureHeader + "colour = \"red\"\n" + fixtureBody
	path := writeFile(t, dir, "notes.toml", src)

	_, err := decodeTOML(path)
	if err == nil {
		t.Fatal("decodeTOML accepted an unknown top-level key")
	}
	if !strings.Contains(err.Error(), "colour") {
		t.Errorf("error %q missing %q", err, "colour")
	}
}

func TestLoadRejectsDuplicateNames(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.toml", `name  = "notes"
route = "/admin/notes"

[list]
columns = [{ field = "Title" }]
`)
	writeFile(t, dir, "b.toml", `name  = "notes"
route = "/admin/other-notes"

[list]
columns = [{ field = "Title" }]
`)

	_, err := Load("", dir)
	if err == nil {
		t.Fatal("Load accepted duplicate names")
	}
	if !strings.Contains(err.Error(), "a.toml") || !strings.Contains(err.Error(), "b.toml") {
		t.Errorf("error %q missing both filenames", err)
	}
}

func TestLoadRejectsDuplicateRoutes(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.toml", `name  = "notes"
route = "/admin/shared"

[list]
columns = [{ field = "Title" }]
`)
	writeFile(t, dir, "b.toml", `name  = "posts"
route = "/admin/shared"

[list]
columns = [{ field = "Title" }]
`)

	_, err := Load("", dir)
	if err == nil {
		t.Fatal("Load accepted duplicate routes")
	}
	if !strings.Contains(err.Error(), "route") {
		t.Errorf("error %q missing %q", err, "route")
	}
}

func TestLoadValidates(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "notes.toml", `name  = "notes"
route = "/admin/notes"
store = "weird"

[list]
columns = [{ field = "Title" }]
`)

	_, err := Load("", dir)
	if err == nil {
		t.Fatal("Load accepted an unknown store")
	}
	if !strings.Contains(err.Error(), "store") {
		t.Errorf("error %q missing %q", err, "store")
	}
}

// TestLoadAcceptsMergeable: store = "mergeable" loads now (the
// 2026-08-22 mergeable-manifests spec removed the Validate refusal).
func TestLoadAcceptsMergeable(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "notes.toml", `name  = "notes"
route = "/admin/notes"
store = "mergeable"

[list]
columns = [{ field = "Title" }]
`)

	rs, err := Load("", dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(rs) != 1 || rs[0].Store != rastrillo.Mergeable {
		t.Fatalf("Load = %+v, want one mergeable resource", rs)
	}
}

func TestLoadNoManifestDirIsEmpty(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")

	got, err := Load("", dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != nil {
		t.Errorf("Load = %#v, want nil", got)
	}
}
