// api_test.go proves the JSON API's default for a resource with no
// owner (JSON API spec §2 and §4). ticket_types and announcements are
// admin tables: tickets guards their pages with nothing, and an app
// guards them with middleware the API cannot see. So they stay off the
// API unless a manifest exposes them, and once one does, a token is
// still refused there until the app's Authorize allows it.
package ticketstest

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"tickets/gen"
)

// Neither resource declares an owner or an [api] table, so neither is
// on the API: no doors, no operations in the committed contract, and
// every descriptor says so for anything else that reads them.
func TestUnscopedResourcesStayOffTheAPI(t *testing.T) {
	t.Parallel()
	if routes := gen.APIRoutes(); len(routes) != 0 {
		t.Errorf("gen.APIRoutes() = %d routes, want none: neither resource has an owner or an [api] table", len(routes))
	}
	ops := gen.Operations()
	if len(ops) == 0 {
		t.Fatal("gen.Operations() is empty: the operations exist whether or not they are exposed")
	}
	for _, d := range ops {
		if d.Exposed || d.Scoped {
			t.Errorf("%s: Exposed %v, Scoped %v; want both false", d.ID, d.Exposed, d.Scoped)
		}
	}
	b, err := os.ReadFile(filepath.Join(appRoot(t), "api", "v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct{ Operations []json.RawMessage }
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Operations) != 0 {
		t.Errorf("api/v1.json lists %d operations, want none:\n%s", len(doc.Operations), b)
	}
}

// Exposing an unscoped resource puts its doors on the API, but
// op.DefaultPolicy still refuses a token there: 403. Only an Authorize
// that allows it lets the token create: 201. This runs on a copy of the
// app whose ticket_types manifest adds [api] expose = true, generated
// for real, so it proves the generated doors rather than a stand-in.
// tickets' own manifest is left alone, keeping the default in force.
func TestExposingAnUnscopedResourceNeedsAPolicy(t *testing.T) {
	if testing.Short() {
		t.Skip("copies, generates and tests a whole app")
	}
	t.Parallel()
	src := appRoot(t)
	root, err := filepath.Abs(filepath.Join(src, "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "tickets")
	copyApp(t, src, dst)

	toml := filepath.Join(dst, "manifest", "ticket_types.toml")
	b, err := os.ReadFile(toml)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(toml, append(b, "\n[api]\nexpose = true\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dst, "internal", "exposedtest"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "internal", "exposedtest", "exposed_test.go"), []byte(exposedTest), 0o644); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("go", args...)
		cmd.Dir = dst
		// -buildvcs=false: the copy has no repository, and one above
		// the temp directory would fail go run's VCS stamping.
		cmd.Env = append(withModMode(os.Environ()), "GOFLAGS=-mod=mod -buildvcs=false")
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Run(); err != nil {
			t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out.String())
		}
		return out.String()
	}
	run("mod", "edit", "-replace", "amadan.net/rastrillo/rastrillo="+root)
	run("run", "amadan.net/rastrillo/rastrillo/cmd/rastrillo", "generate", ".")
	run("test", "-count=1", "./internal/exposedtest/")
}

// copyApp copies the app's source to dst, leaving out what .gitignore
// names: the built binary and a local database.
func copyApp(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		if rel == "tickets" || strings.HasPrefix(rel, "tickets.db") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// exposedTest runs inside the copy, against its generated gen package.
// The token is minted at run time, never written here, so the
// repository's gitleaks rule for the token shape has nothing to find.
const exposedTest = `package exposedtest

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/api"
	"amadan.net/rastrillo/rastrillo/apitoken"
	"amadan.net/rastrillo/rastrillo/basepath"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/op"

	"tickets/gen"
	announcementsstore "tickets/gen/store/announcements"
	ticket_typesstore "tickets/gen/store/ticket_types"
)

type writer struct{ *sql.DB }

func (w writer) Writer() *sql.DB { return w.DB }

func TestExposedUnscopedNeedsAPolicy(t *testing.T) {
	ctx := context.Background()
	d, err := rastrillo.OpenDB(filepath.Join(t.TempDir(), "tickets.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	// One set from an empty database: the ledger refuses to adopt one
	// that already has tables it did not record. Both stores, because
	// gen.Boot reads announcements' event log too.
	set := migrate.Merge(apitoken.Schema)
	for i, stmt := range slices.Concat(ticket_typesstore.Migrations, announcementsstore.Migrations) {
		set.Add(migrate.Migration{ID: fmt.Sprintf("gen/%04d", i+1), SQL: stmt})
	}
	if _, err := migrate.Apply(ctx, writer{d}, set); err != nil {
		t.Fatal(err)
	}
	if _, err := gen.Boot(ctx, d); err != nil {
		t.Fatal(err)
	}
	tokens, err := apitoken.New(apitoken.Config{DB: d, Prefix: "tickets"})
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := tokens.Mint(ctx, "1", apitoken.MintInput{Agent: "Claude", Access: rastrillo.Write})
	if err != nil {
		t.Fatal(err)
	}
	handler := func(authorize func(context.Context, rastrillo.Actor, rastrillo.Operation) error) http.Handler {
		h, err := api.New(api.Config{
			Verify:    tokens,
			Ctx:       func(*http.Request) *rastrillo.Ctx { return &rastrillo.Ctx{DB: d} },
			Authorize: authorize,
		}, gen.APIRoutes()...)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	create := func(h http.Handler, target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(` + "`" + `{"name": "General", "price": {"amount": 2500, "currency": "USD"}}` + "`" + `))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	// api.New refuses a nil Authorize, so the default is named: it is
	// what an app that says nothing more gets.
	if rec := create(handler(op.DefaultPolicy), "/api/v1/ticket_types"); rec.Code != http.StatusForbidden {
		t.Errorf("POST /api/v1/ticket_types under op.DefaultPolicy: %d, want 403\n%s", rec.Code, rec.Body)
	}
	allow := handler(func(context.Context, rastrillo.Actor, rastrillo.Operation) error { return nil })
	if rec := create(allow, "/api/v1/ticket_types"); rec.Code != http.StatusCreated {
		t.Errorf("POST /api/v1/ticket_types under an Authorize that allows it: %d, want 201\n%s", rec.Code, rec.Body)
	}

	// Under a base path the create's Location is a header, which the
	// middleware prefixes once: the client finds the record at /acme,
	// not at the hub's /api/v1, and not at /acme/acme.
	acme := basepath.Config{Origin: "https://example.com", Slug: "acme"}.Handler(allow)
	rec := create(acme, "/acme/api/v1/ticket_types")
	if loc := rec.Header().Get("Location"); rec.Code != http.StatusCreated || loc != "/acme/api/v1/ticket_types/2" {
		t.Errorf("POST /acme/api/v1/ticket_types: %d, Location %q; want 201, /acme/api/v1/ticket_types/2\n%s", rec.Code, loc, rec.Body)
	}
}
`
