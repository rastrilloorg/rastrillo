package contract

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/generate/apigen"
	"amadan.net/rastrillo/rastrillo/internal/generate/scratch"
)

func TestMain(m *testing.M) {
	code := m.Run()
	scratch.Cleanup()
	os.Exit(code)
}

// bookmarks and ticketTypes are the shapes examples/notes and
// examples/tickets declare, as apigen's tests build them: one scoped
// and searchable, one with money, a declared filter and a multi-word
// field. edit mutates a copy before Validate, so a test can state the
// one manifest change it is about.
func bookmarks(t *testing.T, edit ...func(*rastrillo.Resource)) rastrillo.Resource {
	t.Helper()
	r := rastrillo.Resource{
		Name:  "bookmarks",
		Route: "/bookmarks",
		Scope: rastrillo.UserScoped,
		List: rastrillo.List{
			Columns: []rastrillo.Column{{Field: "Title"}, {Field: "Link"}},
			Search:  true,
		},
		Form: rastrillo.Form{
			Basics:   []rastrillo.Field{{Name: "Title", Required: true}, {Name: "Link"}},
			Advanced: []rastrillo.Field{{Name: "Notes", Kind: rastrillo.Textarea}},
		},
	}
	return valid(t, r, edit...)
}

func ticketTypes(t *testing.T, edit ...func(*rastrillo.Resource)) rastrillo.Resource {
	t.Helper()
	yes := true
	r := rastrillo.Resource{
		Name:     "ticket_types",
		Route:    "/admin/ticket_types",
		Currency: "USD",
		API:      rastrillo.API{Expose: &yes},
		List: rastrillo.List{
			Columns: []rastrillo.Column{{Field: "Name"}, {Field: "Price", Kind: rastrillo.Money}, {Field: "Status"}},
			Search:  true,
			Filters: []rastrillo.Filter{{Field: "Status", Values: []string{"draft", "on_sale", "sold_out"}}},
		},
		Form: rastrillo.Form{
			Basics:   []rastrillo.Field{{Name: "Name", Required: true}, {Name: "Price", Kind: rastrillo.Money, Required: true}, {Name: "Status"}},
			Advanced: []rastrillo.Field{{Name: "MaxPerOrder"}},
		},
	}
	return valid(t, r, edit...)
}

func valid(t *testing.T, r rastrillo.Resource, edit ...func(*rastrillo.Resource)) rastrillo.Resource {
	t.Helper()
	for _, e := range edit {
		e(&r)
	}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	return r
}

func contract(t *testing.T, rs ...rastrillo.Resource) []byte {
	t.Helper()
	out, err := Contract(rs)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestContractGolden pins the file for the two example shapes: every
// exposed operation with its route, access, arguments and result
// fields, operations sorted by id and fields by name, a money field
// recorded as its kind, and a trailing newline.
func TestContractGolden(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "contract.json"))
	if err != nil {
		t.Fatal(err)
	}
	// ticket_types first: the order resources are declared in must not
	// reach the file.
	got := contract(t, ticketTypes(t), bookmarks(t))
	if !bytes.Equal(got, want) {
		t.Errorf("Contract differs from testdata/contract.json:\n%s", got)
	}
}

// TestContractOmitsAnUnexposedResource: an unscoped resource with no
// [api] table has no doors, so it has no entry; with no exposed
// resource at all the file still exists, with nothing in it, so taking
// the last resource off the API is a diff and never a deleted file.
func TestContractOmitsAnUnexposedResource(t *testing.T) {
	unexposed := ticketTypes(t, func(r *rastrillo.Resource) { r.API.Expose = nil })
	if got := contract(t, bookmarks(t), unexposed); bytes.Contains(got, []byte("ticket_types")) {
		t.Errorf("an unexposed resource is in the contract:\n%s", got)
	}
	empty := "{\n  \"version\": 1,\n  \"operations\": []\n}\n"
	if got := contract(t, unexposed); string(got) != empty {
		t.Errorf("contract with nothing exposed = %q; want %q", got, empty)
	}
	if got := contract(t); string(got) != empty {
		t.Errorf("contract with no resources = %q; want %q", got, empty)
	}
}

// TestBreakingChanges is every rule, one manifest edit each: the
// breaking ones name exactly the break, the additive ones name nothing.
// An additive case also says what the edit put in the new contract, and
// that the old one lacked it: "no breaks" alone holds for a contract
// that records nothing, so without it those cases could not fail.
func TestBreakingChanges(t *testing.T) {
	old := contract(t, bookmarks(t), ticketTypes(t))
	cases := []struct {
		name  string
		edit  func(*rastrillo.Resource)
		want  []string            // every line, in order; nil for an additive change
		shows func(Document) bool // what an additive change adds; nil for a breaking one
	}{
		{"a resource taken off the API", func(r *rastrillo.Resource) { no := false; r.API.Expose = &no }, []string{
			"bookmarks.create: route removed (POST /api/v1/bookmarks)",
			"bookmarks.delete: route removed (DELETE /api/v1/bookmarks/{id})",
			"bookmarks.get: route removed (GET /api/v1/bookmarks/{id})",
			"bookmarks.list: route removed (GET /api/v1/bookmarks)",
			"bookmarks.update: route removed (PATCH /api/v1/bookmarks/{id})",
		}, nil},
		{"a form field removed", func(r *rastrillo.Resource) { r.Form.Advanced = nil }, []string{
			"bookmarks.create: argument notes was removed",
			"bookmarks.create: result field notes was removed",
			"bookmarks.get: result field notes was removed",
			"bookmarks.update: argument notes was removed",
			"bookmarks.update: result field notes was removed",
		}, nil},
		{"an optional field made required", func(r *rastrillo.Resource) { r.Form.Basics[1].Required = true }, []string{
			"bookmarks.create: argument link is now required",
		}, nil},
		{"a new required field", func(r *rastrillo.Resource) {
			r.Form.Basics = append(r.Form.Basics, rastrillo.Field{Name: "Tags", Required: true})
		}, []string{
			"bookmarks.create: new argument tags is required, so a client that does not send it is refused",
		}, nil},
		{"a field's kind changed", func(r *rastrillo.Resource) { r.Form.Advanced[0].Kind = rastrillo.Text }, []string{
			"bookmarks.create: argument notes changed kind from textarea to text",
			"bookmarks.create: result field notes changed kind from textarea to text",
			"bookmarks.get: result field notes changed kind from textarea to text",
			"bookmarks.update: argument notes changed kind from textarea to text",
			"bookmarks.update: result field notes changed kind from textarea to text",
		}, nil},
		{"a new optional field", func(r *rastrillo.Resource) {
			r.Form.Advanced = append(r.Form.Advanced, rastrillo.Field{Name: "Tags"})
		}, nil, func(d Document) bool { return op(d, "bookmarks.create").Input["tags"] == Field{Kind: "text"} }},
		{"a required field made optional", func(r *rastrillo.Resource) { r.Form.Basics[0].Required = false }, nil,
			func(d Document) bool { return op(d, "bookmarks.create").Input["title"] == Field{Kind: "text"} }},
		{"a new list column, so a new result field", func(r *rastrillo.Resource) {
			r.List.Columns = append(r.List.Columns, rastrillo.Column{Field: "Source"})
		}, nil, func(d Document) bool { return op(d, "bookmarks.get").Result["source"] == "text" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			now := contract(t, bookmarks(t, c.edit), ticketTypes(t))
			got, err := BreakingChanges(old, now)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, "\n") != strings.Join(c.want, "\n") {
				t.Errorf("breaking changes = %q; want %q", got, c.want)
			}
			if c.shows != nil {
				shows(t, c.shows, old, now)
			}
		})
	}

	// Money is a kind like any other: making an optional amount required
	// breaks a client that leaves it out, and removing one removes a
	// field; its wire shape being an object changes nothing here.
	t.Run("an optional money field made required", func(t *testing.T) {
		optional := contract(t, bookmarks(t), ticketTypes(t, func(r *rastrillo.Resource) { r.Form.Basics[1].Required = false }))
		got, err := BreakingChanges(optional, old)
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"ticket_types.create: argument price is now required"}; strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("breaking changes = %q; want %q", got, want)
		}
	})

	// A new resource is new routes, and nothing a client relied on.
	t.Run("a new resource", func(t *testing.T) {
		was := contract(t, bookmarks(t))
		got, err := BreakingChanges(was, old)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Errorf("a new resource is breaking: %q", got)
		}
		shows(t, func(d Document) bool { return op(d, "ticket_types.create").Path == "/api/v1/ticket_types" }, was, old)
	})
}

// op is the operation id in d, or the zero Operation.
func op(d Document, id string) Operation {
	for _, o := range d.Operations {
		if o.ID == id {
			return o
		}
	}
	return Operation{}
}

// shows fails unless has holds of the new contract and not the old.
func shows(t *testing.T, has func(Document) bool, old, new []byte) {
	t.Helper()
	was, err := parse(old)
	if err != nil {
		t.Fatal(err)
	}
	now, err := parse(new)
	if err != nil {
		t.Fatal(err)
	}
	if has(was) {
		t.Errorf("the old contract already has what the edit adds:\n%s", old)
	}
	if !has(now) {
		t.Errorf("the new contract lacks what the edit adds:\n%s", new)
	}
}

// TestBreakingChangesReadsTheRouteAndAccess: a route or an access that
// moved under an id is a break a manifest cannot make today, but the
// file records both so a later generator cannot move them silently
// either. Each is one hand-edited line of the old file.
func TestBreakingChangesReadsTheRouteAndAccess(t *testing.T) {
	now := contract(t, bookmarks(t))
	edited := func(old, new string) []byte {
		if !bytes.Contains(now, []byte(old)) {
			t.Fatalf("contract has no %q", old)
		}
		return bytes.Replace(now, []byte(old), []byte(new), 1)
	}
	cases := []struct {
		name string
		old  []byte
		want string
	}{
		{"path", edited(`"path": "/api/v1/bookmarks/{id}",
      "access": "read"`, `"path": "/api/v1/links/{id}",
      "access": "read"`), "bookmarks.get: route changed from GET /api/v1/links/{id} to GET /api/v1/bookmarks/{id}"},
		// get was write and is now read: a read-only token gains it,
		// and no client loses anything.
		{"write to read", edited(`"path": "/api/v1/bookmarks/{id}",
      "access": "read"`, `"path": "/api/v1/bookmarks/{id}",
      "access": "write"`), ""},
		// create was read and is now write.
		{"read to write", edited(`"path": "/api/v1/bookmarks",
      "access": "write"`, `"path": "/api/v1/bookmarks",
      "access": "read"`), "bookmarks.create: now needs write access, so a read-only token loses it"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := BreakingChanges(c.old, now)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, "\n") != c.want {
				t.Errorf("breaking changes = %q; want %q", got, c.want)
			}
		})
	}
	if _, err := BreakingChanges([]byte("{"), now); err == nil {
		t.Error("a file that does not parse must be an error, not a clean comparison")
	}
	if _, err := BreakingChanges([]byte(`{"version": 2, "operations": []}`), now); err == nil {
		t.Error("a version this generator does not write must be an error")
	}
}

func read(t *testing.T, app string) []byte {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(app, Path))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// TestWriteRefusesABreakAndLeavesTheFile: generate compares before it
// writes, so after a breaking edit the committed file is byte for
// byte what it was and the error names the break. -accept-breaking is
// the one way past, and it says what it rewrote over.
func TestWriteRefusesABreakAndLeavesTheFile(t *testing.T) {
	app := t.TempDir()
	notes, err := Write(app, []rastrillo.Resource{bookmarks(t)}, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{Path + ": written"}; strings.Join(notes, "\n") != strings.Join(want, "\n") {
		t.Errorf("first write notes = %q; want %q", notes, want)
	}
	before := read(t, app)

	broken := []rastrillo.Resource{bookmarks(t, func(r *rastrillo.Resource) { r.Form.Basics[1].Required = true })}
	notes, err = Write(app, broken, false)
	var breaking *BreakingError
	if !errors.As(err, &breaking) {
		t.Fatalf("Write after a breaking edit: err = %v; want a *BreakingError", err)
	}
	if want := []string{"bookmarks.create: argument link is now required"}; strings.Join(breaking.Changes, "\n") != strings.Join(want, "\n") {
		t.Errorf("Changes = %q; want %q", breaking.Changes, want)
	}
	for _, want := range []string{"1 breaking change(s) to /api/v1:", "argument link is now required", "left as it was", "-accept-breaking"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q:\n%v", want, err)
		}
	}
	if len(notes) != 0 {
		t.Errorf("notes after a refusal = %q; want none", notes)
	}
	if after := read(t, app); !bytes.Equal(after, before) {
		t.Errorf("api/v1.json changed after a refused generate:\n%s", after)
	}

	notes, err = Write(app, broken, true)
	if err != nil {
		t.Fatalf("Write with acceptBreaking: %v", err)
	}
	want := []string{Path + ": rewritten over 1 breaking change(s):", "  bookmarks.create: argument link is now required"}
	if strings.Join(notes, "\n") != strings.Join(want, "\n") {
		t.Errorf("notes = %q; want %q", notes, want)
	}
	if after := read(t, app); !bytes.Contains(after, []byte(`"link": {"kind": "text", "required": true}`)) {
		t.Errorf("api/v1.json was not rewritten:\n%s", after)
	}
}

// TestWriteUpdatesOnAnAdditiveChange: a new optional field is written
// through, and an unchanged contract is left alone and says nothing.
func TestWriteUpdatesOnAnAdditiveChange(t *testing.T) {
	app := t.TempDir()
	if _, err := Write(app, []rastrillo.Resource{bookmarks(t)}, false); err != nil {
		t.Fatal(err)
	}
	notes, err := Write(app, []rastrillo.Resource{bookmarks(t)}, false)
	if err != nil || len(notes) != 0 {
		t.Errorf("an unchanged contract: notes = %q, err = %v; want neither", notes, err)
	}
	grown := []rastrillo.Resource{bookmarks(t, func(r *rastrillo.Resource) {
		r.Form.Advanced = append(r.Form.Advanced, rastrillo.Field{Name: "Tags"})
	})}
	notes, err = Write(app, grown, false)
	if err != nil {
		t.Fatalf("an additive change must write: %v", err)
	}
	if want := []string{Path + ": updated"}; strings.Join(notes, "\n") != strings.Join(want, "\n") {
		t.Errorf("notes = %q; want %q", notes, want)
	}
	if got := read(t, app); !bytes.Contains(got, []byte(`"tags"`)) {
		t.Errorf("api/v1.json lacks the new field:\n%s", got)
	}
}

// TestCheckNeverWrites: --check fails when the file is missing, when it
// is stale, and names the breaks when the staleness is a break; it
// passes on a current file; and in every case the directory is as it
// was.
func TestCheckNeverWrites(t *testing.T) {
	app := t.TempDir()
	rs := []rastrillo.Resource{bookmarks(t)}
	err := Check(app, rs)
	if err == nil || !strings.Contains(err.Error(), Path+" is missing") {
		t.Errorf("Check with no file: %v; want it named as missing", err)
	}
	if _, err := os.Stat(filepath.Join(app, Path)); !os.IsNotExist(err) {
		t.Fatalf("Check wrote %s (stat err %v)", Path, err)
	}

	if _, err := Write(app, rs, false); err != nil {
		t.Fatal(err)
	}
	if err := Check(app, rs); err != nil {
		t.Errorf("Check on a current file: %v", err)
	}
	before := read(t, app)

	grown := []rastrillo.Resource{bookmarks(t, func(r *rastrillo.Resource) {
		r.Form.Advanced = append(r.Form.Advanced, rastrillo.Field{Name: "Tags"})
	})}
	err = Check(app, grown)
	if err == nil || !strings.Contains(err.Error(), Path+" is out of date. Run rastrillo generate") {
		t.Errorf("Check on an additive edit: %v; want it named as out of date", err)
	}
	broken := []rastrillo.Resource{bookmarks(t, func(r *rastrillo.Resource) { r.Form.Basics[1].Required = true })}
	err = Check(app, broken)
	if err == nil {
		t.Fatal("Check on a breaking edit passed")
	}
	for _, want := range []string{"out of date, and the change breaks /api/v1 clients:", "  bookmarks.create: argument link is now required", "-accept-breaking"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Check on a breaking edit lacks %q:\n%v", want, err)
		}
	}
	if after := read(t, app); !bytes.Equal(after, before) {
		t.Errorf("Check changed %s:\n%s", Path, after)
	}
}

// TestDoorsAgreeWithOpsgen: the contract takes each route's access from
// apigen.Doors, while the running API takes it from the operation
// opsgen emits. The goldens are opsgen's contract for what it emits,
// so a door whose access differs from its operation's is caught here
// rather than in a client that was promised read access to a write.
func TestDoorsAgreeWithOpsgen(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "opsgen", "testdata", "ops", "bookmarks", "types.go.golden"))
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`rastrillo\.Operation\{ID: "bookmarks\.(\w+)", .*Access: rastrillo\.(\w+)\}`)
	emitted := map[string]string{}
	for _, m := range re.FindAllStringSubmatch(string(src), -1) {
		emitted[m[1]] = strings.ToLower(m[2])
	}
	doors := apigen.Doors(bookmarks(t))
	if len(emitted) != len(doors) {
		t.Fatalf("opsgen emits %d operations, apigen has %d doors", len(emitted), len(doors))
	}
	for _, d := range doors {
		if got := emitted[d.Action]; got != d.Access.String() {
			t.Errorf("%s: door access %s, operation access %s", d.Action, d.Access, got)
		}
	}
}

// bookmarksTOML is examples/notes' bookmarks manifest, with its Link
// field on a line of its own so a test can make it required.
const bookmarksTOML = `name  = "bookmarks"
route = "/bookmarks"
scope = "user"

[list]
columns = [{ field = "Title" }, { field = "Link" }]
search  = true

[form]
basics   = [{ name = "Title", required = true }, { name = "Link" }]
advanced = [{ name = "Notes", kind = "textarea" }]
`

// TestGenerateRefusesABreakingEdit drives the real command: the first
// generate writes api/v1.json; a breaking manifest edit makes a plain
// generate exit non-zero with the file byte-identical and gen/ as it
// was; --check fails the same way and writes nothing; and
// -accept-breaking rewrites it.
func TestGenerateRefusesABreakingEdit(t *testing.T) {
	app := scratch.App(t, map[string]string{"manifest/bookmarks.toml": bookmarksTOML})
	scratch.Generate(t, app)
	before := read(t, app)
	if !bytes.Contains(before, []byte(`"id": "bookmarks.create"`)) {
		t.Fatalf("api/v1.json after generate:\n%s", before)
	}
	if err := scratch.Check(t, app); err != nil {
		t.Fatalf("--check on a fresh generate: %v", err)
	}
	typesBefore, err := os.ReadFile(filepath.Join(app, "gen", "ops", "bookmarks", "types.go"))
	if err != nil {
		t.Fatal(err)
	}

	scratch.Write(t, app, "manifest/bookmarks.toml", strings.Replace(bookmarksTOML, `{ name = "Link" }`, `{ name = "Link", required = true }`, 1))
	out, err := scratch.Run(app, scratch.CLI(t), "generate", app)
	if err == nil {
		t.Fatalf("a plain generate after a breaking edit succeeded:\n%s", out)
	}
	for _, want := range []string{"bookmarks.create: argument link is now required", "-accept-breaking"} {
		if !strings.Contains(out, want) {
			t.Errorf("generate output lacks %q:\n%s", want, out)
		}
	}
	if after := read(t, app); !bytes.Equal(after, before) {
		t.Errorf("api/v1.json changed after a refused generate:\n%s", after)
	}
	// Refused before anything was written: gen/ is as it was too, so
	// there is nothing to revert but the manifest.
	if typesAfter, err := os.ReadFile(filepath.Join(app, "gen", "ops", "bookmarks", "types.go")); err != nil || !bytes.Equal(typesAfter, typesBefore) {
		t.Errorf("gen/ops/bookmarks/types.go changed under a refused generate (read err %v)", err)
	}

	err = scratch.Check(t, app)
	if err == nil || !strings.Contains(err.Error(), "argument link is now required") {
		t.Errorf("--check after a breaking edit: %v; want the break named", err)
	}
	if after := read(t, app); !bytes.Equal(after, before) {
		t.Errorf("api/v1.json changed under --check:\n%s", after)
	}

	out, err = scratch.Run(app, scratch.CLI(t), "generate", "-accept-breaking", app)
	if err != nil {
		t.Fatalf("generate -accept-breaking: %v\n%s", err, out)
	}
	if !strings.Contains(out, Path+": rewritten over 1 breaking change(s):") {
		t.Errorf("generate -accept-breaking output lacks the rewrite note:\n%s", out)
	}
	if after := read(t, app); !bytes.Contains(after, []byte(`"link": {"kind": "text", "required": true}`)) {
		t.Errorf("api/v1.json after -accept-breaking:\n%s", after)
	}
	if err := scratch.Check(t, app); err != nil {
		t.Errorf("--check after -accept-breaking: %v", err)
	}
}

// tree is every file under dir by slash path, so a test can say a
// refused run left a directory exactly as it was, deletions included.
func tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		files[filepath.ToSlash(rel)] = string(content)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// TestGenerateRefusesARemovedManifestDir: with manifest/ deleted or
// renamed, a committed contract still binds. No manifests is no
// resources, so every operation the file lists was removed: plain
// generate refuses before it clears gen/, --check names the break and
// writes nothing, and -accept-breaking writes the empty contract.
func TestGenerateRefusesARemovedManifestDir(t *testing.T) {
	app := scratch.App(t, map[string]string{"manifest/bookmarks.toml": bookmarksTOML})
	scratch.Generate(t, app)
	before := read(t, app)
	genBefore := tree(t, filepath.Join(app, "gen"))
	if _, ok := genBefore["ops/bookmarks/types.go"]; !ok {
		t.Fatalf("gen/ after generate has no ops/bookmarks/types.go: %d files", len(genBefore))
	}
	if err := os.Rename(filepath.Join(app, "manifest"), filepath.Join(app, "manifest.old")); err != nil {
		t.Fatal(err)
	}

	unchanged := func(after string) {
		t.Helper()
		if got := read(t, app); !bytes.Equal(got, before) {
			t.Errorf("api/v1.json changed %s:\n%s", after, got)
		}
		genAfter := tree(t, filepath.Join(app, "gen"))
		for path, content := range genBefore {
			if got, ok := genAfter[path]; !ok {
				t.Errorf("gen/%s was deleted %s", path, after)
			} else if got != content {
				t.Errorf("gen/%s changed %s", path, after)
			}
		}
		for path := range genAfter {
			if _, ok := genBefore[path]; !ok {
				t.Errorf("gen/%s was written %s", path, after)
			}
		}
	}

	removed := "bookmarks.create: route removed (POST /api/v1/bookmarks)"
	out, err := scratch.Run(app, scratch.CLI(t), "generate", app)
	if err == nil {
		t.Fatalf("a plain generate with manifest/ removed succeeded:\n%s", out)
	}
	for _, want := range []string{removed, "-accept-breaking"} {
		if !strings.Contains(out, want) {
			t.Errorf("generate output lacks %q:\n%s", want, out)
		}
	}
	unchanged("after a refused generate")

	err = scratch.Check(t, app)
	if err == nil || !strings.Contains(err.Error(), removed) {
		t.Errorf("--check with manifest/ removed: %v; want the break named", err)
	}
	unchanged("under --check")

	out, err = scratch.Run(app, scratch.CLI(t), "generate", "-accept-breaking", app)
	if err != nil {
		t.Fatalf("generate -accept-breaking: %v\n%s", err, out)
	}
	if !strings.Contains(out, Path+": rewritten over 5 breaking change(s):") {
		t.Errorf("generate -accept-breaking output lacks the rewrite note:\n%s", out)
	}
	if got := read(t, app); string(got) != "{\n  \"version\": 1,\n  \"operations\": []\n}\n" {
		t.Errorf("api/v1.json after -accept-breaking:\n%s", got)
	}
	if err := scratch.Check(t, app); err != nil {
		t.Errorf("--check after -accept-breaking: %v", err)
	}
}

// TestGenerateCheckRequiresTheFile: an app with manifests but no
// committed contract fails --check and gains no file from it; its
// first plain generate writes one, empty when nothing is exposed.
func TestGenerateCheckRequiresTheFile(t *testing.T) {
	app := scratch.App(t, map[string]string{"manifest/posts.toml": scratch.ResourceTOML("posts", "exclusive")})
	scratch.Generate(t, app)
	if got := read(t, app); string(got) != "{\n  \"version\": 1,\n  \"operations\": []\n}\n" {
		t.Errorf("api/v1.json with nothing exposed:\n%s", got)
	}
	if err := os.Remove(filepath.Join(app, Path)); err != nil {
		t.Fatal(err)
	}
	err := scratch.Check(t, app)
	if err == nil || !strings.Contains(err.Error(), Path+" is missing") {
		t.Errorf("--check without the file: %v; want it named as missing", err)
	}
	if _, err := os.Stat(filepath.Join(app, Path)); !os.IsNotExist(err) {
		t.Errorf("--check wrote %s (stat err %v)", Path, err)
	}
}
