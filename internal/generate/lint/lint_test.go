package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/generate"
	"amadan.net/rastrillo/rastrillo/internal/generate/handfile"
)

// bookmarks is user-scoped, so it is on the API by default and its
// doors are checked. posts is unscoped, so it is not, and a hand file
// overriding one of its doors may do as it likes.
var (
	bookmarks = rastrillo.Resource{
		Name:  "bookmarks",
		Route: "/bookmarks",
		Scope: rastrillo.UserScoped,
		List:  rastrillo.List{Columns: []rastrillo.Column{{Field: "Title"}, {Field: "Link"}}, Search: true},
		Form: rastrillo.Form{
			Basics:   []rastrillo.Field{{Name: "Title", Required: true}, {Name: "Link"}},
			Advanced: []rastrillo.Field{{Name: "Notes", Kind: rastrillo.Textarea}},
		},
	}
	posts = rastrillo.Resource{
		Name:  "posts",
		Route: "/admin/posts",
		Store: rastrillo.Exclusive,
		List:  rastrillo.List{Columns: []rastrillo.Column{{Field: "Title"}}},
		Form:  rastrillo.Form{Basics: []rastrillo.Field{{Name: "Title", Required: true}}},
	}
	resources = []rastrillo.Resource{bookmarks, posts}
)

// app writes files under a fresh app root with go.mod naming module
// "app", so an ops import reads app/gen/ops/<resource>.
func app(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	files["go.mod"] = "module app\n\ngo 1.26\n"
	for name, src := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// action wraps body as a bookmarks index.POST override: the header the
// generated door has, then Handle with body as its statements, then
// extra after Handle, for a same-file helper.
func action(leading, body string, extra ...string) string {
	return leading + `package actions

import (
	"net/http"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/form"
	"amadan.net/rastrillo/rastrillo/view"
	ops_bookmarks "app/gen/ops/bookmarks"
)

// Handle is POST /bookmarks.
func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
	p := form.Parse(r, form.Field{Name: "Title"})
	in := ops_bookmarks.CreateInput{}
	if !p.OK() {
		view.Render(ctx, w, "bookmarks/form", http.StatusBadRequest, nil)
		return
	}
` + body + `
}
` + strings.Join(extra, "")
}

// save is a same-file helper that calls the operation the way Handle
// must: at the top level, with the error used and returned.
const save = `
func save(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) error {
	_, err := ops_bookmarks.Create(r.Context(), ctx, ops_bookmarks.CreateInput{})
	return err
}
`

const tagged = "//go:build rastrillo_actions\n\n"

// opFile wraps body as a bookmarks create.go replacement, with decls
// before the function.
func opFile(decls, body string) string {
	return tagged + `package ops

import (
	"context"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/op"
)
` + decls + `
func Create(ctx context.Context, rc *rastrillo.Ctx, in CreateInput) (Bookmark, error) {
` + body + `
}
`
}

const guard = `	if err := op.Check(ctx, rc, CreateOp); err != nil {
		return Bookmark{}, err
	}
	return Bookmark{}, nil`

func TestParity(t *testing.T) {
	const override = "actions/bookmarks/index.POST.go"
	const handOp = "ops/bookmarks/create.go"
	cases := []struct {
		name  string
		files map[string]string
		want  []string // one substring per expected finding, in order; none means a clean run
	}{
		{"compliant override", map[string]string{override: action(tagged, `
	n, err := ops_bookmarks.Create(r.Context(), ctx, in)
	if err != nil {
		view.Refuse(ctx, w, r, "bookmarks: creating", err)
		return
	}
	_ = n`)}, nil},
		{"override calls Get instead of Create", map[string]string{override: action(tagged, `
	n, err := ops_bookmarks.Get(r.Context(), ctx, 1)
	_, _ = n, err`)}, []string{override + ":15: call ops_bookmarks.Create"}},
		{"override calls only ValidateCreate", map[string]string{override: action(tagged, `
	errs := ops_bookmarks.ValidateCreate(in)
	_ = errs`)}, []string{override + ":15: call ops_bookmarks.Create"}},
		{"call inside if", map[string]string{override: action(tagged, `
	if r.FormValue("save") != "" {
		if _, err := ops_bookmarks.Create(r.Context(), ctx, in); err != nil {
			return
		}
	}`)}, []string{override + ":24: ops_bookmarks.Create runs only in a branch"}},
		{"error assigned to blank", map[string]string{override: action(tagged, `
	n, _ := ops_bookmarks.Create(r.Context(), ctx, in)
	_ = n`)}, []string{override + ":23: use the error ops_bookmarks.Create returns"}},
		{"bare call statement", map[string]string{override: action(tagged, `
	ops_bookmarks.Create(r.Context(), ctx, in)`)}, []string{override + ":23: use the error ops_bookmarks.Create returns"}},
		{"helper called in an assignment", map[string]string{override: action(tagged, `
	_ = in
	err := save(ctx, w, r)
	if err != nil {
		view.Refuse(ctx, w, r, "bookmarks: creating", err)
		return
	}`, save)}, nil},
		{"helper called in an if init", map[string]string{override: action(tagged, `
	_ = in
	if err := save(ctx, w, r); err != nil {
		view.Refuse(ctx, w, r, "bookmarks: creating", err)
		return
	}`, save)}, nil},
		{"helper called only in a branch", map[string]string{override: action(tagged, `
	_ = in
	if r.FormValue("save") != "" {
		_ = save(ctx, w, r)
	}`, save)}, []string{override + ":15: call ops_bookmarks.Create"}},
		{"page-only with reason", map[string]string{override: action(tagged+"//rastrillo:page-only the importer takes a file upload; the API has no file transfer yet\n", `
	_ = in`)}, nil},
		{"page-only without reason", map[string]string{override: action(tagged+"//rastrillo:page-only\n", `
	_ = in`)}, []string{override + ":3: say why this action has no API twin"}},
		{"override of an unexposed resource", map[string]string{"actions/admin/posts/index.POST.go": strings.ReplaceAll(action(tagged, `
	_ = in`), "bookmarks", "posts")}, nil},
		{"hand op without op.Check", map[string]string{handOp: opFile("", `
	return Bookmark{}, nil`)}, []string{handOp + ":12: Create: begin with `if err := op.Check(...); err != nil { return ..., err }`"}},
		{"hand op with the exact guard", map[string]string{handOp: opFile("", guard)}, nil},
		{"hand op calls op.Check as a bare statement", map[string]string{handOp: opFile("", `
	op.Check(ctx, rc, CreateOp)
	return Bookmark{}, nil`)}, []string{handOp + ":12: Create: begin with"}},
		{"hand op with a statement before the guard", map[string]string{handOp: opFile("", "\tin.Title.Value = \"x\"\n"+guard)}, []string{handOp + ":12: Create: begin with"}},
		{"hand op whose guard returns nil", map[string]string{handOp: opFile("", `
	if err := op.Check(ctx, rc, CreateOp); err != nil {
		return Bookmark{}, nil
	}
	return Bookmark{}, nil`)}, []string{handOp + ":12: Create: begin with"}},
		{"hand op whose guard binds nil", map[string]string{handOp: opFile("", `
	if nil := op.Check(ctx, rc, CreateOp); nil != nil {
		return Bookmark{}, nil
	}
	return Bookmark{}, nil`)}, []string{handOp + ":12: Create: begin with"}},
		{"package-level var calling a function", map[string]string{handOp: opFile("\nvar cache = load()\n\nfunc load() int { return 1 }\n", guard)}, []string{handOp + ":12: var cache: no work may run before the guard"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := app(t, tc.files)
			got, err := Parity(root, resources)
			if err != nil {
				t.Fatal(err)
			}
			var lines []string
			for _, f := range got {
				lines = append(lines, f.String())
			}
			if len(lines) != len(tc.want) {
				t.Fatalf("want %d finding(s) %q, got %d:\n%s", len(tc.want), tc.want, len(lines), strings.Join(lines, "\n"))
			}
			for i, w := range tc.want {
				if !strings.Contains(lines[i], w) {
					t.Errorf("finding %d: want %q in\n%s", i, w, lines[i])
				}
			}
		})
	}
}

// TestParityAcceptsGeneratedDoors copies every door the generator
// emits into actions/ as an override (tag added, nothing else) and
// requires a clean run: the money edit doors are Handle calling
// handle, and an app that copies one to change a line must not be
// told to rewrite it.
func TestParityAcceptsGeneratedDoors(t *testing.T) {
	ticketTypes := rastrillo.Resource{
		Name:     "ticket_types",
		Route:    "/admin/ticket_types",
		Store:    rastrillo.Exclusive,
		Scope:    rastrillo.UserScoped,
		Currency: "USD",
		List: rastrillo.List{
			Columns: []rastrillo.Column{{Field: "Name"}, {Field: "Price", Kind: rastrillo.Money}, {Field: "Status"}},
			Search:  true,
			Filters: []rastrillo.Filter{{Field: "Status", Values: []string{"draft", "on_sale"}}},
		},
		Form: rastrillo.Form{
			Basics:   []rastrillo.Field{{Name: "Name", Required: true}, {Name: "Price", Kind: rastrillo.Money, Required: true}, {Name: "Status"}},
			Advanced: []rastrillo.Field{{Name: "MaxPerOrder"}, {Name: "Deposit", Kind: rastrillo.Money}},
		},
	}
	rs := []rastrillo.Resource{bookmarks, ticketTypes}
	root := app(t, map[string]string{})
	for _, r := range rs {
		if !r.Exposed() {
			t.Fatalf("%s must be exposed for this test to check anything", r.Name)
		}
		gen := filepath.Join(root, "gen-"+r.Name)
		written, _, err := generate.EmitActions(root, gen, r)
		if err != nil {
			t.Fatal(err)
		}
		doors := generate.Doors(r)
		if len(doors) != len(written) {
			t.Fatalf("%s: %d doors, %d files written", r.Name, len(doors), len(written))
		}
		for i, d := range doors {
			if filepath.Base(written[i]) != filepath.Base(d.Source) {
				t.Fatalf("%s: door %d is %s, file %s", r.Name, i, d.Source, written[i])
			}
			src, err := os.ReadFile(written[i])
			if err != nil {
				t.Fatal(err)
			}
			dst := filepath.Join(root, "actions", filepath.FromSlash(d.Source))
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(dst, append([]byte(tagged), src...), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	got, err := Parity(root, rs)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range got {
		t.Error(f)
	}
}

// The tag the fixtures carry is the one the build-tag check wants, so
// a fixture here is also what --check accepts there.
func TestFixturesCarryTheBuildTag(t *testing.T) {
	if !handfile.SkippedByDefaultBuild([]byte(action(tagged, ""))) {
		t.Fatal("the action fixture is not skipped by a default build")
	}
}
