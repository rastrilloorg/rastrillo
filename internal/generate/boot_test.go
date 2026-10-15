package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
)

// An app with manifest resources cannot build its router without
// Boot: the wiring every existing app had stops compiling, and the
// new wiring builds.
func TestRouterRequiresBoot(t *testing.T) {
	app := scratchApp(t, map[string]string{"manifest/a.toml": resourceTOML("a", "exclusive")})
	mustGenerate(t, app)
	writeMain(t, app, `mux := gen.Router(func(*http.Request) *rastrillo.Ctx { return &rastrillo.Ctx{DB: db} })`)
	if out, err := goBuild(app); err == nil || !strings.Contains(out, "gen.Router") {
		t.Fatalf("the old wiring still builds:\n%s", out)
	}
	writeMain(t, app, `b, err := gen.Boot(context.Background(), db)
	if err != nil { panic(err) }
	mux := gen.Router(b, func(*http.Request) *rastrillo.Ctx { return &rastrillo.Ctx{DB: db} })`)
	if out, err := goBuild(app); err != nil {
		t.Fatalf("the Boot wiring does not build:\n%s", out)
	}
}

// Review round two: Booted vouches for one database. A router whose
// contexts carry another refuses every request before any action runs,
// and leaves that database untouched.
func TestRouterRefusesADatabaseBootDidNotCheck(t *testing.T) {
	app := scratchApp(t, map[string]string{"manifest/a.toml": resourceTOML("a", "exclusive")})
	mustGenerate(t, app)
	writeFile(t, app, "gen/twodb_test.go", `package gen

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	astore "scratch/gen/store/a"
)

func open(t *testing.T, name string, migrations []string) *sql.DB {
	db, err := rastrillo.OpenDB(filepath.Join(t.TempDir(), name), migrations)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestTwoDatabases(t *testing.T) {
	a, b := open(t, "a.db", astore.Migrations), open(t, "b.db", nil) // a has the app's schema; b is untouched
	noop := func(*rastrillo.Ctx, http.ResponseWriter, string, int, any) {} // the action's render; writing nothing leaves 200
	booted, err := Boot(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	Router(booted, func(*http.Request) *rastrillo.Ctx { return &rastrillo.Ctx{DB: b, Render: noop} }).ServeHTTP(rec, httptest.NewRequest("GET", "/a", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("a router on an unchecked database answered %d", rec.Code)
	}
	var n int
	b.QueryRow("SELECT COUNT(*) FROM sqlite_master").Scan(&n)
	if n != 0 {
		t.Errorf("the unchecked database was touched: %d objects", n)
	}
	rec = httptest.NewRecorder()
	Router(booted, func(*http.Request) *rastrillo.Ctx { return &rastrillo.Ctx{Render: noop} }).ServeHTTP(rec, httptest.NewRequest("GET", "/a", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("a context with no database answered %d; it must get the checked one and list a's rows", rec.Code)
	}
}

// b above has no tables, so its action would fail on its own. c has the
// schema: only the router's check stops it listing c's rows with a 200.
func TestAnotherDatabaseWithTheSchema(t *testing.T) {
	a, c := open(t, "a.db", astore.Migrations), open(t, "c.db", astore.Migrations)
	booted, err := Boot(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	var log strings.Builder
	logger := slog.New(slog.NewTextHandler(&log, nil))
	noop := func(*rastrillo.Ctx, http.ResponseWriter, string, int, any) {}
	rec := httptest.NewRecorder()
	Router(booted, func(*http.Request) *rastrillo.Ctx { return &rastrillo.Ctx{DB: c, Logger: logger, Render: noop} }).ServeHTTP(rec, httptest.NewRequest("GET", "/a", nil))
	if rec.Code != http.StatusInternalServerError || strings.TrimSpace(rec.Body.String()) != "Something went wrong." {
		t.Errorf("a router on an unchecked database answered %d %q", rec.Code, rec.Body)
	}
	if !strings.Contains(log.String(), "not the one gen.Boot checked") {
		t.Errorf("the refusal was not logged: %q", log.String())
	}
}
`)
	if out, err := goTest(app, "./gen/"); err != nil {
		t.Fatalf("%s", out)
	}
}

func TestRouterPanicsOnAForgedBooted(t *testing.T) {
	app := scratchApp(t, map[string]string{"manifest/a.toml": resourceTOML("a", "exclusive")})
	mustGenerate(t, app)
	writeFile(t, app, "gen/forged_test.go", `package gen

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
)

func TestForged(t *testing.T) {
	defer func() {
		if v := recover(); v == nil || !strings.Contains(fmt.Sprint(v), "gen.Boot") {
			t.Errorf("a zero Booted was accepted or the panic does not say what to call: %v", v)
		}
	}()
	Router(Booted{}, func(*http.Request) *rastrillo.Ctx { return nil })
}
`)
	if out, err := goTest(app, "./gen/"); err != nil {
		t.Fatalf("%s", out)
	}
}

// The generator lists every exclusive money store for Boot's SQL
// upgrade, by the column names its queries use, and no other.
func TestBootUpgradesTheMoneyStores(t *testing.T) {
	cases := []struct {
		name string
		rs   []rastrillo.Resource
		want string // the sqlmoney.Table lines, or "" for no SQL step
	}{
		{"tickets", []rastrillo.Resource{ticketTypes(t, nil), mergeableFixtureResource()},
			"\t\t{Name: \"ticket_types\", Fields: []string{\"price\"}, Legacy: &currency.Legacy{Currency: \"USD\", Scale: 2}},\n"},
		{"two money fields, no [legacy]", []rastrillo.Resource{ticketTypes(t, func(r *rastrillo.Resource) {
			r.Legacy = nil
			r.Form.Advanced = append(r.Form.Advanced, rastrillo.Field{Name: "Deposit", Kind: rastrillo.Money})
		})}, "\t\t{Name: \"ticket_types\", Fields: []string{\"price\", \"deposit\"}},\n"},
		{"no money", []rastrillo.Resource{filteredFixtureResource(), scopedFixtureResource()}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			genDir := filepath.Join(t.TempDir(), "gen")
			if _, _, _, err := emitPipeline(newActionsAppRoot(t), genDir, c.rs, nil, false); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filepath.Join(genDir, "boot.go"))
			if err != nil {
				t.Fatal(err)
			}
			if c.want == "" {
				if strings.Contains(string(got), "sqlmoney") {
					t.Errorf("Boot upgrades money for resources without any:\n%s", got)
				}
				return
			}
			if !strings.Contains(string(got), "\tif err := sqlmoney.Upgrade(ctx, db, []sqlmoney.Table{\n"+c.want+"\t}); err != nil {\n") {
				t.Errorf("gen/boot.go lacks\n%s\ngot:\n%s", c.want, got)
			}
		})
	}
}
