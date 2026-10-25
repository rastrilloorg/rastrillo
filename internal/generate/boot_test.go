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

// routerActorTest is the same four requests against either router: who
// the action sees, as "<actor> <access>". A page request with a session
// must reach op.Check as that person, or a scoped operation behind a
// signed-in page would refuse its own owner for having no subject.
// Then one Ctx for every request, which an app's factory may well
// return: the second person must not be served as the first, and two
// at once must not race on the actor (final review, finding 1).
const routerActorTest = `package gen

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/sessions"
)

func TestActor(t *testing.T) {
	as := func(subject string) *http.Request {
		return sessions.WithSession(httptest.NewRequest("GET", ROUTE, nil), sessions.Session{Subject: subject})
	}
	signedIn := func() *http.Request { return as("42") }
	cases := []struct {
		name    string
		r       *http.Request
		factory rastrillo.Actor
		want    string
	}{
		{"a session, factory left Actor zero", signedIn(), rastrillo.Actor{}, "user:42 write"},
		{"a stamped actor beats the session", rastrillo.WithActor(signedIn(), rastrillo.Actor{Subject: "7", Credential: "x", Access: rastrillo.Read}), rastrillo.Actor{}, "user:7 token:x read"},
		{"the factory's actor beats the session", signedIn(), rastrillo.Actor{Subject: "f", Access: rastrillo.Write}, "user:f write"},
		{"no session, no actor", httptest.NewRequest("GET", ROUTE, nil), rastrillo.Actor{}, "app invalid"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		router(t, c.factory).ServeHTTP(rec, c.r)
		if got := rec.Body.String(); got != c.want {
			t.Errorf("%s: the action saw %q, want %q", c.name, got, c.want)
		}
	}

	saw := func(shared http.Handler, subject string) {
		rec := httptest.NewRecorder()
		shared.ServeHTTP(rec, as(subject))
		if got, want := rec.Body.String(), "user:"+subject+" write"; got != want {
			t.Errorf("one Ctx for every request: session %s saw %q, want %q", subject, got, want)
		}
	}
	shared := router(t, rastrillo.Actor{})
	saw(shared, "42")
	saw(shared, "43")
	// A fresh one for the race: the first request's write and the
	// others' reads, which the sequential pair above has already settled.
	shared = router(t, rastrillo.Actor{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			saw(shared, "42")
			saw(shared, "43")
		}()
	}
	wg.Wait()
}
`

func TestRouterTakesAPageActorFromTheSession(t *testing.T) {
	t.Run("no resources", func(t *testing.T) {
		if testing.Short() {
			t.Skip("builds a scratch app")
		}
		app := newScratchModule(t, false)
		writeFile(t, app, "manifest/app.toml", "locales = [\"en\"]\ndefault_locale = \"en\"\n")
		writeFile(t, app, "actions/index.GET.go", `//go:build `+BuildTag+`

package actions

import (
	"fmt"
	"net/http"

	"amadan.net/rastrillo/rastrillo"
)

func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, ctx.Actor, " ", ctx.Actor.Access)
}
`)
		mustGenerate(t, app)
		writeFile(t, app, "gen/actor_test.go", strings.ReplaceAll(routerActorTest, "ROUTE", `"/"`))
		writeFile(t, app, "gen/router_test.go", `package gen

import (
	"net/http"
	"testing"

	"amadan.net/rastrillo/rastrillo"
)

// One Ctx per router, handed to every request, as an app's factory may.
func router(t *testing.T, a rastrillo.Actor) http.Handler {
	c := &rastrillo.Ctx{Actor: a}
	return Router(func(*http.Request) *rastrillo.Ctx { return c })
}
`)
		if out, err := goTest(app, "./gen/"); err != nil {
			t.Fatalf("%s", out)
		}
	})
	t.Run("booted", func(t *testing.T) {
		app := scratchApp(t, map[string]string{"manifest/a.toml": resourceTOML("a", "exclusive")})
		mustGenerate(t, app)
		writeFile(t, app, "gen/actor_test.go", strings.ReplaceAll(routerActorTest, "ROUTE", `"/a"`))
		writeFile(t, app, "gen/router_test.go", `package gen

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	astore "scratch/gen/store/a"
)

// The list action answers through Render, so Render is where the
// booted router's action shows who it saw. The action calls its
// operation first, whose default policy refuses a token on an
// unscoped resource; allowing everything here keeps the question
// about the actor the router hands over, not about the policy. One
// Ctx per router, handed to every request, as an app's factory may.
func router(t *testing.T, a rastrillo.Actor) http.Handler {
	db, err := rastrillo.OpenDB(filepath.Join(t.TempDir(), "a.db"), astore.Migrations)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	b, err := Boot(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	show := func(c *rastrillo.Ctx, w http.ResponseWriter, _ string, _ int, _ any) {
		fmt.Fprint(w, c.Actor, " ", c.Actor.Access)
	}
	allow := func(context.Context, rastrillo.Actor, rastrillo.Operation) error { return nil }
	c := &rastrillo.Ctx{Actor: a, Render: show, Authorize: allow}
	return Router(b, func(*http.Request) *rastrillo.Ctx { return c })
}
`)
		if out, err := goTest(app, "./gen/"); err != nil {
			t.Fatalf("%s", out)
		}
	})
}
