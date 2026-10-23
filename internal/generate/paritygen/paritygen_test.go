package paritygen

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
// declare. bookmarks is user-scoped, so exposed by default, with one
// required field; ticket_types is unscoped, so not, unless expose says
// so, and has a required money field.
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

func golden(t *testing.T, name string) string {
	t.Helper()
	want, err := os.ReadFile(filepath.Join("testdata", "paritytest", name+".golden"))
	if err != nil {
		t.Fatal(err)
	}
	return string(want)
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestEmitGoldens pins the package for an app with a scoped resource
// and an exposed money one: the shared helpers, money's included, and
// one Run function per resource.
func TestEmitGoldens(t *testing.T) {
	yes := true
	genDir := filepath.Join(t.TempDir(), "gen")
	written, err := Emit(genDir, []rastrillo.Resource{bookmarks(t), ticketTypes(t, &yes)})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(genDir, "paritytest")
	files := []struct{ file, golden string }{
		{"fixture.go", "fixture.go"},
		{"bookmarks_parity.go", "bookmarks.go"},
		{"ticket_types_parity.go", "ticket_types.go"},
	}
	var want []string
	for _, f := range files {
		want = append(want, filepath.Join(dir, f.file))
	}
	if strings.Join(written, "\n") != strings.Join(want, "\n") {
		t.Fatalf("written = %v; want %v", written, want)
	}
	for _, f := range files {
		if got := read(t, filepath.Join(dir, f.file)); got != golden(t, f.golden) {
			t.Errorf("%s differs from testdata/paritytest/%s.golden:\n%s", f.file, f.golden, got)
		}
	}
}

// TestFixtureWithoutMoney: with no exposed money resource nothing would
// call the money helpers, and an unused function or import fails the
// examples' staticcheck, so they are left out.
func TestFixtureWithoutMoney(t *testing.T) {
	genDir := filepath.Join(t.TempDir(), "gen")
	if _, err := Emit(genDir, []rastrillo.Resource{bookmarks(t), ticketTypes(t, nil)}); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(genDir, "paritytest", "fixture.go"))
	for _, unwanted := range []string{"func money(", "func moneyForm(", `"amadan.net/rastrillo/rastrillo/currency"`, `"amadan.net/rastrillo/rastrillo/form"`, "locales/en"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("fixture.go with no exposed money resource has %q:\n%s", unwanted, got)
		}
	}
	if !strings.Contains(got, "func refused(") {
		t.Errorf("fixture.go lacks refused, which bookmarks' required Title needs:\n%s", got)
	}
}

// TestEmitSkipsAnAppWithNothingExposed: no exposed resource, no
// package, so prune removes the one an earlier run wrote.
func TestEmitSkipsAnAppWithNothingExposed(t *testing.T) {
	genDir := filepath.Join(t.TempDir(), "gen")
	written, err := Emit(genDir, []rastrillo.Resource{ticketTypes(t, nil)})
	if err != nil {
		t.Fatal(err)
	}
	if written != nil {
		t.Fatalf("written = %v; want nothing", written)
	}
	if _, err := os.Stat(filepath.Join(genDir, "paritytest")); !os.IsNotExist(err) {
		t.Fatalf("gen/paritytest exists (%v); want nothing written", err)
	}
}

// TestFixtureWithoutAScopedResource: hidden is the cross-owner check,
// and with no scoped resource nothing would call it.
func TestFixtureWithoutAScopedResource(t *testing.T) {
	yes := true
	genDir := filepath.Join(t.TempDir(), "gen")
	if _, err := Emit(genDir, []rastrillo.Resource{ticketTypes(t, &yes)}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(genDir, "paritytest", "fixture.go")); strings.Contains(got, "func hidden(") {
		t.Errorf("fixture.go with no scoped resource has hidden:\n%s", got)
	}
}

// TestFileNamesAreNeverBuildConstraints: Go reads a file name's last
// _word as a constraint, so a Run file named after orders_test would
// be a test file, and one named after orders_windows would build on
// Windows only. Either way RunOrdersTest or RunOrdersWindows is missing
// where the app's test calls it. fixture is here because its Run file
// must not replace the helpers.
func TestFileNamesAreNeverBuildConstraints(t *testing.T) {
	var rs []rastrillo.Resource
	for _, name := range []string{"orders_test", "orders_windows", "orders_arm64", "fixture"} {
		r := bookmarks(t)
		r.Name, r.Route = name, "/"+name
		rs = append(rs, valid(t, r))
	}
	genDir := filepath.Join(t.TempDir(), "gen")
	written, err := Emit(genDir, rs)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != len(rs)+1 {
		t.Fatalf("written = %v; want fixture.go and one file per resource", written)
	}
	for _, p := range written {
		builtEverywhere(t, p)
	}
}

// builtEverywhere fails unless path is an ordinary source file on every
// platform a test might run on.
func builtEverywhere(t *testing.T, path string) {
	t.Helper()
	if strings.HasSuffix(path, "_test.go") {
		t.Errorf("%s is a test file, so no other package can call it", path)
	}
	for _, p := range [][2]string{{"linux", "amd64"}, {"darwin", "arm64"}, {"windows", "amd64"}} {
		ctx := build.Default
		ctx.GOOS, ctx.GOARCH = p[0], p[1]
		ok, err := ctx.MatchFile(filepath.Dir(path), filepath.Base(path))
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Errorf("%s is left out of the %s/%s build", path, p[0], p[1])
		}
	}
}

const scopedBookmarksTOML = `name  = "bookmarks"
route = "/bookmarks"
scope = "user"

[list]
columns = [{ field = "Title" }, { field = "Link" }]
search  = true

[form]
basics   = [{ name = "Title", required = true }, { name = "Link" }]
advanced = [{ name = "Notes", kind = "textarea" }]
`

// ticketTypesTOMLForRuntime is examples/tickets' ticket_types without
// [legacy]: a fresh scratch database has no old integers to upgrade.
const ticketTypesTOMLForRuntime = `name  = "ticket_types"
route = "/admin/ticket_types"
currency = "USD"

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

// TestParityScenarioRuns generates a scratch app, runs the scenario
// through both doors of both resources, and then gives bookmarks a
// hand-written create page that saves the title in capitals: the same
// operation behind both doors, but a page that changes what it is
// given, which is the drift the test exists to catch.
func TestParityScenarioRuns(t *testing.T) {
	app := parityApp(t)
	if out, err := scratch.GoTest(app, "./paritycheck"); err != nil {
		t.Fatalf("the parity scenario:\n%s", out)
	}

	scratch.Write(t, app, "actions/bookmarks/index.POST.go", shoutingCreate)
	scratch.Generate(t, app)
	out, err := scratch.GoTest(app, "./paritycheck")
	if err == nil {
		t.Fatalf("the parity scenario passed with a page that saves a different title:\n%s", out)
	}
	if want := `row after create: page door {Name:row after create Kind:ok Row:{"link":"Parity link","notes":"Parity notes","title":"PARITY TITLE"}}`; !strings.Contains(out, want) {
		t.Fatalf("the parity scenario failed, but not on the row the page saved; want %q in:\n%s", want, out)
	}
}

// parityApp is a scratch app with both resources exposed, generated,
// and the scratch fixture in paritycheck.
func parityApp(t *testing.T) string {
	t.Helper()
	app := scratch.App(t, map[string]string{
		"manifest/bookmarks.toml":    scopedBookmarksTOML,
		"manifest/ticket_types.toml": ticketTypesTOMLForRuntime + "\n[api]\nexpose = true\n",
	})
	scratch.Generate(t, app)
	scratch.Write(t, app, "paritycheck/parity_test.go", parityScratchTest)
	return app
}

// TestParityFailsWhenBothDoorsShowAnotherPersonsRow: when "b" can read
// "a"'s bookmark through the page and the API alike, the two doors
// agree, and the scenario must still fail, once for each door.
func TestParityFailsWhenBothDoorsShowAnotherPersonsRow(t *testing.T) {
	app := parityApp(t)
	out, err := scratch.RunWith(app, append(scratch.Env(), "PARITY_ONE_PERSON=1"), "go", "test", "-count=1", "-run", "TestBookmarksParity", "./paritycheck")
	if err == nil {
		t.Fatalf("the parity scenario passed with both doors showing another person's row:\n%s", out)
	}
	for _, want := range []string{
		"the page door answered ok for another person's row; want not_found",
		"the API door answered ok for another person's row; want not_found",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the parity scenario's failure lacks %q:\n%s", want, out)
		}
	}
}

// TestParityKeepsLargeNumbersExact runs the helpers against answers
// past 2^53, where a float64 holds only even integers: two adjacent
// amounts must not compare equal, an id must not print as 9.007e+15,
// and an edit must post back the amount as stored.
func TestParityKeepsLargeNumbersExact(t *testing.T) {
	app := parityApp(t)
	scratch.Write(t, app, "gen/paritytest/numbers_test.go", numbersTest)
	if out, err := scratch.GoTest(app, "./gen/paritytest"); err != nil {
		t.Fatalf("the parity helpers and large numbers:\n%s", out)
	}
}

// numbersTest is an internal test of the generated helpers, written
// beside them in the scratch app.
const numbersTest = `package paritytest

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"amadan.net/rastrillo/rastrillo/currency"
)

// priced answers every request with one ticket type of id 2^53+1 whose
// price is amount minor units.
func priced(amount string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, ` + "`" + `{"id": 9007199254740993, "price": {"amount": %s, "currency": "USD"}}` + "`" + `, amount)
	})
}

func TestAdjacentLargeAmountsDiffer(t *testing.T) {
	a := row(t, priced("9007199254740992"), "token", "/api/v1/ticket_types/1")
	b := row(t, priced("9007199254740993"), "token", "/api/v1/ticket_types/1")
	if a == b {
		t.Errorf("rows with amounts 2^53 and 2^53+1 compare equal: %s", a)
	}
}

func TestLargeIDPrintsExactly(t *testing.T) {
	_, body := call(t, priced("1"), "token", "GET", "/api/v1/ticket_types/1", nil)
	if got := fmt.Sprint(body["id"]); got != "9007199254740993" {
		t.Errorf("id 2^53+1 reads %s", got)
	}
}

func TestAnEditPostsTheStoredAmount(t *testing.T) {
	cur := record(t, priced("9007199254740993"), "token", "/api/v1/ticket_types/1")
	f := url.Values{}
	moneyForm(t, f, "Price", cur["price"])
	got, err := currency.ParseNormalized("en", f.Get("Price"), "USD")
	if err != nil {
		t.Fatal(err)
	}
	if got != 9007199254740993 {
		t.Errorf("the edit form posts %q, which is %d minor units; want 9007199254740993", f.Get("Price"), got)
	}
}
`

// TestUnexposingRemovesTheDoorsAndTheParityTest: a resource taken off
// the API loses its doors and its parity file on the next generate, and
// the last one taken off removes gen/paritytest, so nothing keeps
// testing or serving a door the manifest no longer has.
func TestUnexposingRemovesTheDoorsAndTheParityTest(t *testing.T) {
	app := parityApp(t)
	exists := func(rel string, want bool) {
		t.Helper()
		_, err := os.Stat(filepath.Join(app, filepath.FromSlash(rel)))
		if got := err == nil; got != want {
			t.Errorf("%s exists = %v; want %v (%v)", rel, got, want, err)
		}
	}
	for _, rel := range []string{"gen/api/bookmarks/doors.go", "gen/api/ticket_types/doors.go", "gen/paritytest/fixture.go", "gen/paritytest/bookmarks_parity.go", "gen/paritytest/ticket_types_parity.go"} {
		exists(rel, true)
	}

	// Taking operations off the API breaks v1's contract, which is the
	// point here, so it is accepted.
	regenerate := func() {
		t.Helper()
		if out, err := scratch.Run(app, scratch.CLI(t), "generate", "-accept-breaking", app); err != nil {
			t.Fatalf("generate -accept-breaking: %v\n%s", err, out)
		}
		if err := scratch.Check(t, app); err != nil {
			t.Fatalf("generate --check after the change: %v", err)
		}
	}
	scratch.Write(t, app, "manifest/ticket_types.toml", ticketTypesTOMLForRuntime)
	regenerate()
	exists("gen/api/ticket_types", false)
	exists("gen/paritytest/ticket_types_parity.go", false)
	exists("gen/api/bookmarks/doors.go", true)
	exists("gen/paritytest/bookmarks_parity.go", true)

	scratch.Write(t, app, "manifest/bookmarks.toml", scopedBookmarksTOML+"\n[api]\nexpose = false\n")
	regenerate()
	exists("gen/api/bookmarks", false)
	exists("gen/paritytest", false)
}

// shoutingCreate replaces the generated POST /bookmarks with one that
// upper-cases the title before calling the operation.
const shoutingCreate = `//go:build rastrillo_actions

package actions

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/op"
	"amadan.net/rastrillo/rastrillo/view"
	ops_bookmarks "scratch/gen/ops/bookmarks"
)

// Handle is POST /bookmarks.
func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request.", http.StatusBadRequest)
		return
	}
	in := ops_bookmarks.CreateInput{
		Title: op.Opt[string]{Value: strings.ToUpper(r.PostForm.Get("Title")), Set: r.PostForm.Has("Title")},
		Link:  op.Opt[string]{Value: r.PostForm.Get("Link"), Set: r.PostForm.Has("Link")},
		Notes: op.Opt[string]{Value: r.PostForm.Get("Notes"), Set: r.PostForm.Has("Notes")},
	}
	n, err := ops_bookmarks.Create(r.Context(), ctx, in)
	var invalid op.Invalid
	if errors.As(err, &invalid) {
		view.Render(ctx, w, "bookmarks/form", http.StatusBadRequest, nil)
		return
	}
	if err != nil {
		view.Refuse(ctx, w, r, "bookmarks: creating bookmarks", err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/bookmarks/%d", n.ID), http.StatusSeeOther)
}
`

// parityScratchTest is a fixture over the generated router and API
// tree: a header names the signed-in person, as a session would, and a
// token is the person's name. It is the least an app can wire and still
// run both doors as production mounts them.
const parityScratchTest = `package paritycheck

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/api"
	"amadan.net/rastrillo/rastrillo/sessions"

	"scratch/gen"
	"scratch/gen/paritytest"
	bookmarksstore "scratch/gen/store/bookmarks"
	ticket_typesstore "scratch/gen/store/ticket_types"
)

type fixture struct{}

// allow lets tokens write ticket_types, which is unscoped: the pages
// let anyone, and parity needs the API to let the token too.
func allow(context.Context, rastrillo.Actor, rastrillo.Operation) error { return nil }

func (fixture) New(t *testing.T) (http.Handler, *sql.DB) {
	db, err := rastrillo.OpenDB(filepath.Join(t.TempDir(), "app.db"), append(append([]string(nil), bookmarksstore.Migrations...), ticket_typesstore.Migrations...))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	booted, err := gen.Boot(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	pages := gen.Router(booted, func(*http.Request) *rastrillo.Ctx {
		return &rastrillo.Ctx{DB: db, Authorize: allow, Render: func(_ *rastrillo.Ctx, w http.ResponseWriter, _ string, status int, _ any) {
			w.WriteHeader(status)
		}}
	})
	tree, err := api.New(api.Config{
		Verify:    verifier{},
		Ctx:       func(*http.Request) *rastrillo.Ctx { return &rastrillo.Ctx{DB: db} },
		Authorize: allow,
	}, gen.APIRoutes()...)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/api/v1", tree)
	mux.Handle("/api/v1/", tree)
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s := r.Header.Get("X-Parity-Subject"); s != "" {
			r = sessions.WithSession(r, sessions.Session{Subject: s})
		}
		pages.ServeHTTP(w, r)
	}))
	return mux, db
}

type verifier struct{}

func (verifier) Verify(ctx context.Context, r *http.Request) (rastrillo.Actor, error) {
	s := r.Header.Get("Authorization")
	if len(s) < len("Bearer x") {
		return rastrillo.Actor{}, api.ErrUnauthorized
	}
	subject := s[len("Bearer "):]
	return rastrillo.Actor{Subject: subject, Credential: "parity" + subject, Access: rastrillo.Write}, nil
}

// subjectTransport serves every request from h in process, as subject.
type subjectTransport struct {
	h       http.Handler
	subject string
}

func (s subjectTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("X-Parity-Subject", s.subject)
	w := httptest.NewRecorder()
	s.h.ServeHTTP(w, r)
	return w.Result(), nil
}

func (fixture) SignIn(t *testing.T, h http.Handler, db *sql.DB, subject string) *http.Client {
	return &http.Client{Transport: subjectTransport{h, person(subject)}}
}

func (fixture) Token(t *testing.T, db *sql.DB, subject string) string { return person(subject) }

// person is who subject signs in as. PARITY_ONE_PERSON makes "b" the
// same person as "a", so both doors show "a"'s row to "b": the leak a
// scoped resource exists to prevent, agreed on by both doors.
func person(subject string) string {
	if os.Getenv("PARITY_ONE_PERSON") != "" {
		return "a"
	}
	return subject
}

func (fixture) Prepare(t *testing.T, db *sql.DB, subject string) {}

func (fixture) ValidCreate(resource string) map[string]string { return nil }

func TestBookmarksParity(t *testing.T) { paritytest.RunBookmarks(t, fixture{}) }

func TestTicketTypesParity(t *testing.T) { paritytest.RunTicketTypes(t, fixture{}) }
`
