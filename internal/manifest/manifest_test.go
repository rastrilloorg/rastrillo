package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
)

// fixtureHeader/fixtureBody split the plan's fixture manifest so tests
// can splice content in at the top level (before any [table]).
const fixtureHeader = `name  = "notes"
route = "/admin/notes"
store = "exclusive"
currency = "USD"
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
		Name:     "notes",
		Route:    "/admin/notes",
		Store:    rastrillo.Exclusive,
		Currency: "USD",
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

// TestLoadMoneyCurrency decodes the money keys end to end. The one
// that matters most is [legacy] without scale: decoded into a plain
// int it would read as 0, and every stored 1250 cents would be
// upgraded to 125000.
func TestLoadMoneyCurrency(t *testing.T) {
	for name, c := range map[string]struct {
		extra string // TOML appended after the fixture
		top   string // TOML spliced in at the top level
		err   string
		sep   string // between the file name and the message
	}{
		// No colon: op.generate.no_currency is one sentence, "manifest/
		// <file>.toml has a money field...", not "...toml: has a money
		// field...", which would read as a label introducing a clause.
		"no currency":     {top: "", err: `has a money field but no currency; add currency = "EUR"`, sep: " "},
		"scale omitted":   {top: `currency = "USD"` + "\n", extra: "\n[legacy]\ncurrency = \"USD\"\n", err: "[legacy] needs scale", sep: ": "},
		"currency missed": {top: `currency = "USD"` + "\n", extra: "\n[legacy]\nscale = 2\n", err: "[legacy] needs currency", sep: ": "},
		"bad legacy code": {top: `currency = "USD"` + "\n", extra: "\n[legacy]\ncurrency = \"DEM\"\nscale = 2\n", err: `[legacy] currency "DEM"`, sep: ": "},
	} {
		dir := t.TempDir()
		path := writeFile(t, dir, "notes.toml", `name  = "notes"
route = "/admin/notes"
`+c.top+fixtureBody+c.extra)
		_, err := Load("", dir)
		if err == nil || !strings.Contains(err.Error(), c.err) {
			t.Errorf("%s: Load error %v, want %q", name, err, c.err)
			continue
		}
		// Load names the file once; Validate must not name it again.
		if !strings.HasPrefix(err.Error(), path+c.sep) || strings.Count(err.Error(), ".toml") != 1 {
			t.Errorf("%s: %q should name %s exactly once, separated by %q", name, err, path, c.sep)
		}
	}

	dir := t.TempDir()
	writeFile(t, dir, "notes.toml", fixtureHeader+fixtureBody+"\n[legacy]\ncurrency = \"jpy\"\nscale = 0\n")
	writeFile(t, dir, "posts.toml", `name = "posts"
route = "/admin/posts"
currency = " eur "
`+fixtureBody+"\n[legacy]\ncurrency = \"USD\"\nscale = 2\n")
	rs, err := Load("", dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if rs[0].Legacy == nil || rs[0].Legacy.Scale == nil || *rs[0].Legacy.Scale != 0 || rs[0].Legacy.Currency != "JPY" {
		t.Fatalf("notes [legacy] = %+v, want JPY at an explicit scale 0", rs[0].Legacy)
	}
	if rs[1].Currency != "EUR" {
		t.Errorf("posts currency = %q, want the canonical EUR", rs[1].Currency)
	}

	// gen/manifest.json must keep an explicit 0 distinct from omission,
	// or a renderer reading the artifact would refuse a valid manifest.
	art := Artifact(rs)
	if !strings.Contains(string(art), `"scale": 0`) || !strings.Contains(string(art), `"scale": 2`) {
		t.Fatalf("artifact lost a scale:\n%s", art)
	}
	var back []rastrillo.Resource
	if err := json.Unmarshal(art, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, rs) {
		t.Errorf("artifact round trip = %#v, want %#v", back, rs)
	}
}

// TestLoadNoCurrencyMatchesOperatorMessage pins the exact line
// rastrillo generate prints for a resource with a money field but no
// currency against Appendix C.2's op.generate.no_currency: one
// sentence, "manifest/<file>.toml has a money field...", with no colon
// between the file name and the message.
func TestLoadNoCurrencyMatchesOperatorMessage(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("manifest", 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, "manifest", "notes.toml", `name  = "notes"
route = "/admin/notes"
`+fixtureBody)

	_, err := Load("", "manifest")
	want := `manifest/notes.toml has a money field but no currency; add currency = "EUR" (or another ISO 4217 code), the default for new records`
	if err == nil || err.Error() != want {
		t.Errorf("Load error = %v, want %v", err, want)
	}
}

func TestIneligibleDefaults(t *testing.T) {
	got := IneligibleDefaults([]rastrillo.Resource{
		{Name: "fees", Currency: "SLL"},
		{Name: "notes", Currency: "USD"},
		{Name: "posts"},
	})
	want := []string{"warning: manifest/fees.toml: SLL is still accepted for storage but is no longer issued; choose a current currency for new records"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("IneligibleDefaults = %q, want %q", got, want)
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
