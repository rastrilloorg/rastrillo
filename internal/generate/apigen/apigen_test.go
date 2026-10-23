package apigen

import (
	"go/build"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/generate/scratch"
)

func TestMain(m *testing.M) {
	code := m.Run()
	scratch.Cleanup()
	os.Exit(code)
}

// bookmarks and ticketTypes are internal/generate's scopedFixtureResource
// and ticketTypes: the shapes examples/notes and examples/tickets
// declare. bookmarks is user-scoped, so exposed by default; ticket_types
// is unscoped, so not, unless its manifest says [api] expose = true.
func bookmarks(t *testing.T) rastrillo.Resource {
	return valid(t, rastrillo.Resource{
		Name:  "bookmarks",
		Route: "/bookmarks",
		Store: rastrillo.Exclusive,
		Scope: rastrillo.UserScoped,
		List: rastrillo.List{
			Columns: []rastrillo.Column{{Field: "Title"}, {Field: "Link"}},
			Search:  true,
		},
		Form: rastrillo.Form{
			Basics:   []rastrillo.Field{{Name: "Title", Required: true}, {Name: "Link"}},
			Advanced: []rastrillo.Field{{Name: "Notes", Kind: rastrillo.Textarea}},
		},
	})
}

func ticketTypes(t *testing.T, expose *bool) rastrillo.Resource {
	scale := 2
	return valid(t, rastrillo.Resource{
		Name:     "ticket_types",
		Route:    "/admin/ticket_types",
		Store:    rastrillo.Exclusive,
		Currency: "USD",
		Legacy:   &rastrillo.Legacy{Currency: "USD", Scale: &scale},
		API:      rastrillo.API{Expose: expose},
		List: rastrillo.List{
			Columns: []rastrillo.Column{{Field: "Name"}, {Field: "Price", Kind: rastrillo.Money}, {Field: "Status"}},
			Search:  true,
			Filters: []rastrillo.Filter{{Field: "Status", Values: []string{"draft", "on_sale", "sold_out"}}},
		},
		Form: rastrillo.Form{
			Basics:   []rastrillo.Field{{Name: "Name", Required: true}, {Name: "Price", Kind: rastrillo.Money, Required: true}, {Name: "Status"}},
			Advanced: []rastrillo.Field{{Name: "MaxPerOrder"}},
		},
	})
}

func valid(t *testing.T, r rastrillo.Resource) rastrillo.Resource {
	t.Helper()
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	return r
}

func scratchModule(t *testing.T) (root, genDir string) {
	t.Helper()
	root = t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module scratch\n\ngo 1.25.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, filepath.Join(root, "gen")
}

func golden(t *testing.T, name string) string {
	t.Helper()
	want, err := os.ReadFile(filepath.Join("testdata", "api", name+".golden"))
	if err != nil {
		t.Fatal(err)
	}
	return string(want)
}

// TestEmitGoldens pins the two door shapes: a searchable scoped
// resource, and one with a declared filter, which adds one line to the
// list's input. Everything else differs only by name.
func TestEmitGoldens(t *testing.T) {
	yes := true
	for _, c := range []struct {
		name string
		r    rastrillo.Resource
	}{
		{"bookmarks", bookmarks(t)},
		{"ticket_types", ticketTypes(t, &yes)},
	} {
		t.Run(c.name, func(t *testing.T) {
			root, genDir := scratchModule(t)
			written, err := Emit(root, genDir, c.r)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(genDir, "api", c.name, "doors.go")
			if len(written) != 1 || written[0] != path {
				t.Fatalf("written = %v; want exactly %s", written, path)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if want := golden(t, c.name+".go"); string(got) != want {
				t.Errorf("%s's doors.go differs from testdata/api/%s.go.golden:\n%s", c.name, c.name, got)
			}
		})
	}
}

// TestFileNameIsNeverABuildConstraint: Go reads a file name's last
// _word as a constraint, so doors named after orders_test would be a
// test file and ones named after orders_windows would build on Windows
// only; either way gen/api.go's call to Routes would not compile.
func TestFileNameIsNeverABuildConstraint(t *testing.T) {
	for _, name := range []string{"orders_test", "orders_windows", "orders_arm64"} {
		r := bookmarks(t)
		r.Name, r.Route = name, "/"+name
		root, genDir := scratchModule(t)
		written, err := Emit(root, genDir, valid(t, r))
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range written {
			if strings.HasSuffix(p, "_test.go") {
				t.Errorf("%s is a test file, so gen/api.go cannot call its Routes", p)
			}
			for _, plat := range [][2]string{{"linux", "amd64"}, {"darwin", "arm64"}, {"windows", "amd64"}} {
				ctx := build.Default
				ctx.GOOS, ctx.GOARCH = plat[0], plat[1]
				ok, err := ctx.MatchFile(filepath.Dir(p), filepath.Base(p))
				if err != nil {
					t.Fatal(err)
				}
				if !ok {
					t.Errorf("%s is left out of the %s/%s build", p, plat[0], plat[1])
				}
			}
		}
	}
}

// TestEmitSkipsAnUnexposedResource: an unscoped resource with no [api]
// table gets no doors and claims no path, so nothing under gen/api/
// names it and prune has nothing of it to keep.
func TestEmitSkipsAnUnexposedResource(t *testing.T) {
	root, genDir := scratchModule(t)
	written, err := Emit(root, genDir, ticketTypes(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if written != nil {
		t.Fatalf("written = %v; want nothing", written)
	}
	if _, err := os.Stat(filepath.Join(genDir, "api")); !os.IsNotExist(err) {
		t.Fatalf("gen/api exists (%v); want nothing written", err)
	}
}

// TestFile pins gen/api.go for a module with one exposed resource and
// one not, and for a module with no resources at all: an app's main.go
// calls both functions either way.
func TestFile(t *testing.T) {
	got, err := File("scratch", []rastrillo.Resource{ticketTypes(t, nil), bookmarks(t)})
	if err != nil {
		t.Fatal(err)
	}
	if want := golden(t, "api.go"); string(got) != want {
		t.Errorf("api.go differs from testdata/api/api.go.golden:\n%s", got)
	}

	got, err = File("scratch", nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := golden(t, "api_none.go"); string(got) != want {
		t.Errorf("api.go with no resources differs from testdata/api/api_none.go.golden:\n%s", got)
	}

	// Resources but no doors: APIRoutes is still there to call, and
	// Operations still lists them, each with Exposed false.
	got, err = File("scratch", []rastrillo.Resource{ticketTypes(t, nil)})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"func APIRoutes() []api.Route { return nil }",
		"ops = append(ops, ops_ticket_types.Descriptors()...)",
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("api.go with no exposed resource lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(string(got), "gen/api/") {
		t.Errorf("api.go with no exposed resource imports a doors package:\n%s", got)
	}
}

// feesTOML is a user-scoped resource, so exposed by default, with a
// required amount and an optional one: every money shape on the wire.
const feesTOML = `name     = "fees"
route    = "/fees"
scope    = "user"
currency = "EUR"

[list]
columns = [{ field = "Title" }, { field = "Price", kind = "money" }]

[form]
basics   = [{ name = "Title", required = true }, { name = "Price", kind = "money", required = true }]
advanced = [{ name = "Deposit", kind = "money" }]
`

// ticketTypesTOML is examples/tickets' ticket_types without [legacy]
// (a fresh database has nothing to upgrade) and with [api] expose =
// true, which the example does not set: it is the one shape with a
// filter and a multi-word field, so it is what pins the wire names.
const ticketTypesTOML = `name  = "ticket_types"
route = "/admin/ticket_types"
currency = "USD"

[api]
expose = true

[list]
columns = [{ field = "Name" }, { field = "Price", kind = "money" }, { field = "Status" }]
search  = true

[[list.filters]]
field  = "Status"
values = ["draft", "on_sale", "sold_out"]

[form]
basics   = [{ name = "Name", required = true }, { name = "Price", kind = "money", required = true }, { name = "Status" }]
advanced = [{ name = "MaxPerOrder" }]
`

// TestRuntime generates a scratch app and drives its doors through
// api.New over HTTP: money in and out, the error precedence, the list
// parameters, and the wire field names. The test it runs is
// testdata/api/wire_test.go.src, carried as data because it compiles
// against the scratch app's generated packages, not this one.
func TestRuntime(t *testing.T) {
	app := scratch.App(t, map[string]string{
		"manifest/fees.toml":         feesTOML,
		"manifest/ticket_types.toml": ticketTypesTOML,
	})
	scratch.Generate(t, app)
	src, err := os.ReadFile(filepath.Join("testdata", "api", "wire_test.go.src"))
	if err != nil {
		t.Fatal(err)
	}
	scratch.Write(t, app, "apitest/wire_test.go", string(src))
	if out, err := scratch.GoTest(app, "./apitest"); err != nil {
		t.Fatalf("the generated API doors:\n%s", out)
	}
}

// TestLocalNeverShadowsAHandlerParameter: a door names the row after
// the singular's first letter, and r and w are the request and the
// writer, so a resource whose singular starts with either would make
// api.Refuse(w, r, err) pass the row as the request and not compile.
func TestLocalNeverShadowsAHandlerParameter(t *testing.T) {
	for name, want := range map[string]string{"bookmarks": "b", "remotes": "row", "widgets": "row", "ticket_types": "t"} {
		if got := local(rastrillo.Resource{Name: name}); got != want {
			t.Errorf("local(%s) = %q, want %q", name, got, want)
		}
	}
}
