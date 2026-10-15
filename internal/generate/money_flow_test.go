package generate

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
)

// Money from boot to clear in generated apps, through the real CLI and
// each app's own startup. A step that must be a separate process start
// against the same database file (an old binary, then the new one) is
// a run of the scratch module's cmd/steps tests with STEP and DB set.

// ticketTypesTOMLWithoutCurrency is examples/tickets' manifest as it
// was released, before money had a currency.
func ticketTypesTOMLWithoutCurrency(t *testing.T) string {
	return released(t, "ticket_types.toml")
}

// ticketTypesTOMLWithCurrencyLegacyVenueAndDeposit is that manifest
// after the upgrade: a currency and [legacy], plus a text field and a
// money field added since the release.
func ticketTypesTOMLWithCurrencyLegacyVenueAndDeposit(t *testing.T) string {
	s := ticketTypesTOMLWithoutCurrency(t)
	for old, new := range map[string]string{
		"store = \"exclusive\"\n":                 "store = \"exclusive\"\ncurrency = \"USD\"\n",
		"advanced = [{ name = \"MaxPerOrder\" }]": "advanced = [{ name = \"MaxPerOrder\" }, { name = \"Venue\" }, { name = \"Deposit\", kind = \"money\" }]",
	} {
		if !strings.Contains(s, old) {
			t.Fatalf("the released manifest lacks %q", old)
		}
		s = strings.Replace(s, old, new, 1)
	}
	return s + "\n[legacy]\ncurrency = \"USD\"\nscale    = 2\n"
}

// notesStyleWrapper keeps the generated statements in a migration
// ledger under index IDs, as examples/notes does with its bookmarks.
const notesStyleWrapper = `package app

import (
	"fmt"

	"amadan.net/rastrillo/rastrillo/migrate"
	ticket_typesstore "scratch/gen/store/ticket_types"
)

func Schema() *migrate.Set {
	s := new(migrate.Set)
	for i, stmt := range ticket_typesstore.Migrations {
		s.Add(migrate.Migration{ID: fmt.Sprintf("ticket_types_gen/%04d_init", i+1), SQL: stmt})
	}
	return s
}
`

// stepsHarness is the part of cmd/steps/main_test.go every app with
// generated code shares: start builds the app as main would, through
// rastrillo.Handler (which opens $DB and applies migrations), then in
// its Router before applies anything else, gen.Boot runs, and
// gen.Router serves behind a ui.Pages render of the generated pages.
const stepsHarness = `
const layout = ` + "`" + `{{define "layout"}}{{$L := localizer $}}{{template "content" (withL (.) $L)}}{{end}}` + "`" + `

func start(t *testing.T, migrations []string, before func(*sql.DB) error) (http.Handler, *sql.DB) {
	t.Helper()
	h, db, err := startErr(t, migrations, before)
	if err != nil {
		t.Fatal(err)
	}
	return h, db
}

// startErr is start for a start that may be refused: Handler's error,
// which carries Boot's, is returned rather than failing the test.
func startErr(t *testing.T, migrations []string, before func(*sql.DB) error) (http.Handler, *sql.DB, error) {
	t.Helper()
	cfg, err := locales.CheckConfig(genlocales.DefaultLocale, genlocales.Locales)
	if err != nil {
		t.Fatal(err)
	}
	root := os.DirFS(filepath.Join("..", ".."))
	files, err := fs.Glob(root, "gen/templates/*/*.html")
	if err != nil || len(files) == 0 {
		t.Fatalf("no generated pages: %v", err)
	}
	pageFiles := map[string]ui.PageFile{}
	for _, f := range files {
		pageFiles[strings.TrimSuffix(strings.TrimPrefix(f, "gen/templates/"), ".html")] = ui.PageFile{FS: root, Path: f}
	}
	pages, err := ui.NewPages(ui.PagesConfig{
		App:    fstest.MapFS{"layout.html": {Data: []byte(layout)}},
		Layout: "layout.html", Pages: pageFiles, Locale: cfg, Catalog: genlocales.BaseCatalog,
	})
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	render := func(ctx *rastrillo.Ctx, w http.ResponseWriter, page string, status int, data any) {
		var b bytes.Buffer
		if err := pages.Execute(&b, page, data); err != nil {
			t.Errorf("render %s: %v", page, err)
			http.Error(w, "render", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(status)
		w.Write(b.Bytes())
	}
	var db *sql.DB
	h, closeDB, err := rastrillo.Handler(rastrillo.Options{
		DBPath: os.Getenv("DB"), Migrations: migrations, Logger: logger,
		Locales: genlocales.Locales, DefaultLocale: genlocales.DefaultLocale, BaseCatalog: genlocales.BaseCatalog,
		Router: func(d *sql.DB) (*http.ServeMux, error) {
			d = appDB(d)
			db = d
			if before != nil {
				if err := before(d); err != nil {
					return nil, err
				}
			}
			booted, err := gen.Boot(context.Background(), d)
			if err != nil {
				return nil, err
			}
			app := gen.Router(booted, func(*http.Request) *rastrillo.Ctx {
				return &rastrillo.Ctx{DB: d, Logger: logger, Render: render}
			})
			mux := http.NewServeMux()
			mux.Handle("/", pages.Middleware(app))
			return mux, nil
		},
	})
	if err != nil {
		return nil, nil, err
	}
	t.Cleanup(func() { closeDB() })
	return h, db, nil
}

// appDB is the handle Boot and every request use, given the one
// Handler opened. A test that must pause a request at an exact read
// swaps in a wrapped handle on the same file.
var appDB = func(d *sql.DB) *sql.DB { return d }

func do(t *testing.T, h http.Handler, method, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func want(t *testing.T, rec *httptest.ResponseRecorder, status int, parts ...string) string {
	t.Helper()
	body := rec.Body.String()
	if rec.Code != status {
		t.Fatalf("status %d, want %d:\n%s", rec.Code, status, body)
	}
	for _, p := range parts {
		if !strings.Contains(body, p) {
			t.Errorf("missing %q in:\n%s", p, body)
		}
	}
	return body
}

func exec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}
`

const stepsImports = `
	"bytes"
	"context"
	"database/sql"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/locales"
	"amadan.net/rastrillo/rastrillo/ui"

	"scratch/gen"
	genlocales "scratch/gen/locales"
`

// writeSteps writes cmd/steps/main_test.go: the harness when withHarness,
// the extra imports, and the step code (a TestStep switching on STEP,
// or TestFlows).
func writeSteps(t *testing.T, app string, withHarness bool, imports, code string) {
	t.Helper()
	src := "package steps\n\nimport (\n"
	if withHarness {
		src += stepsImports
	}
	src += imports + ")\n"
	if withHarness {
		src += stepsHarness
	}
	writeFile(t, app, "cmd/steps/main_test.go", src+code)
}

// runInApp runs cmd/steps' TestStep with STEP and DB set: one process
// start against the database file, as a deploy would be.
func runInApp(t *testing.T, app, step, db string) {
	t.Helper()
	if out, err := runSteps(app, "TestStep", "STEP="+step, "DB="+db); err != nil {
		t.Fatalf("step %s: %v\n%s", step, err, out)
	}
}

// runSteps runs one test in cmd/steps. go test exits 0 when -run
// matches nothing, so the test must also be seen passing, or a renamed
// step would pass without running.
func runSteps(app, test string, env ...string) (string, error) {
	out, err := runInEnv(app, env, "go", "test", "-count=1", "-v", "-run", "^"+test+"$", "./cmd/steps")
	if err == nil && !strings.Contains(out, "--- PASS: "+test+" ") {
		err = fmt.Errorf("%s did not run", test)
	}
	return out, err
}

func runInEnv(app string, env []string, name string, args ...string) (string, error) {
	return runInWith(app, append(scratchEnv(), env...), name, args...)
}

func releasedOrFail(t *testing.T, path string) []string {
	t.Helper()
	got, err := releasedMigrations(path)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func openDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := rastrillo.OpenDB(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func ledgerIDs(t *testing.T, path string) []string {
	t.Helper()
	rows, err := openDB(t, path).Query(`SELECT id FROM rastrillo_migrations ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

// assertRow checks the one row q selects, its values joined by spaces.
func assertRow(t *testing.T, path, q, want string) {
	t.Helper()
	rows, err := openDB(t, path).Query(q)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	if !rows.Next() {
		t.Fatalf("%s: no row", q)
	}
	vals := make([]sql.NullString, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(vals))
	for i, v := range vals {
		got[i] = v.String
	}
	if strings.Join(got, " ") != want {
		t.Errorf("%s = %q, want %q", q, strings.Join(got, " "), want)
	}
}

// treeHashes is every file under app, by path, to its SHA-256.
func treeHashes(t *testing.T, app string) map[string][32]byte {
	t.Helper()
	out := map[string][32]byte{}
	err := filepath.WalkDir(app, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[p] = sha256.Sum256(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// An app that keeps generated statements in its migration ledger under
// index IDs (notes' pattern) and was released before money had a
// currency: change the manifest, regenerate, start through migrate.Apply
// and Boot. The released statement's checksum is unchanged, only the
// appended statement is new in the ledger, and Boot finishes the money.
func TestLedgerAppUpgradesThroughApplyAndBoot(t *testing.T) {
	todaysTicketTypesStatement := released(t, "ticket_types_statement.sql")
	app := scratchApp(t, map[string]string{
		"manifest/ticket_types.toml":           ticketTypesTOMLWithoutCurrency(t), // today's examples/tickets manifest
		"gen/store/ticket_types/migrations.go": released(t, "ticket_types_migrations.go.golden"),
		"internal/app/migrations.go":           notesStyleWrapper,
	})
	db := filepath.Join(app, "app.db")
	const applyOld = `
func TestStep(t *testing.T) {
	db, err := rastrillo.OpenDB(os.Getenv("DB"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	switch os.Getenv("STEP") {
	case "apply-old":
		if _, err := migrate.Apply(context.Background(), migrate.Pool(db), app.Schema()); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(` + "`" + `INSERT INTO ticket_types (name, price, status, max_per_order, created_at, updated_at) VALUES ('GA', 1250, 'on_sale', '', 'x', 'x')` + "`" + `); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unknown step %q", os.Getenv("STEP"))
	}
}
`
	writeSteps(t, app, false, "\t\"context\"\n\t\"os\"\n\t\"testing\"\n\n\t\"amadan.net/rastrillo/rastrillo\"\n\t\"amadan.net/rastrillo/rastrillo/migrate\"\n\t\"scratch/internal/app\"\n", applyOld)
	runInApp(t, app, "apply-old", db) // the binary released before money had a currency

	writeFile(t, app, "manifest/ticket_types.toml", ticketTypesTOMLWithCurrencyLegacyVenueAndDeposit(t))
	// generate says what it appended, once: the next run finds it released.
	appended := "ticket_types: appended ALTER TABLE ticket_types ADD COLUMN venue TEXT NOT NULL DEFAULT '' to gen/store/ticket_types/migrations.go; released statements are never edited"
	for run, wantNote := range []bool{true, false} {
		out, err := runIn(app, rastrilloCLI(t), "generate", app)
		if err != nil || strings.Contains(out, appended) != wantNote {
			t.Fatalf("generate %d (want the note: %v): %v\n%s", run+1, wantNote, err, out)
		}
	}
	got := releasedOrFail(t, filepath.Join(app, "gen/store/ticket_types/migrations.go"))
	if got[0] != todaysTicketTypesStatement || len(got) != 2 || !strings.Contains(got[1], "ADD COLUMN venue") {
		t.Fatalf("regenerated migrations: %q", got)
	}

	writeSteps(t, app, true, "\n\t\"amadan.net/rastrillo/rastrillo/migrate\"\n\t\"scratch/internal/app\"\n", `
func TestStep(t *testing.T) {
	if os.Getenv("STEP") != "start" {
		t.Fatalf("unknown step %q", os.Getenv("STEP"))
	}
	h, _ := start(t, nil, func(db *sql.DB) error {
		_, err := migrate.Apply(context.Background(), migrate.Pool(db), app.Schema())
		return err
	})
	want(t, do(t, h, "GET", "/admin/ticket_types/1", nil), http.StatusOK, "$12.50")
}
`)
	runInApp(t, app, "start", db)
	ledger := ledgerIDs(t, db)
	if strings.Join(ledger, " ") != "ticket_types_gen/0001_init ticket_types_gen/0002_init" {
		t.Errorf("ledger %v", ledger)
	}
	assertRow(t, db, `SELECT price_minor, price_currency, venue, deposit_minor IS NULL FROM ticket_types`, "1250 USD  1")
}

// generate --check emits into an empty temp dir. It must read the
// released statements from the real gen/ tree, as generate does, or it
// emits first-release SQL and reports the upgraded tree as stale.
func TestCheckPassesOnAnUpgradedTreeAndChangesNothing(t *testing.T) {
	app := scratchApp(t, map[string]string{
		"manifest/ticket_types.toml":           ticketTypesTOMLWithoutCurrency(t),
		"gen/store/ticket_types/migrations.go": released(t, "ticket_types_migrations.go.golden"),
	})
	writeFile(t, app, "manifest/ticket_types.toml", ticketTypesTOMLWithCurrencyLegacyVenueAndDeposit(t)) // a money field and a non-money field added
	mustGenerate(t, app)
	before := treeHashes(t, app) // path -> SHA-256 of every file under the app
	if err := checkGenerate(t, app); err != nil {
		t.Fatalf("--check on the tree generate just wrote: %v", err)
	}
	if after := treeHashes(t, app); !maps.Equal(before, after) {
		t.Errorf("--check changed the source tree")
	}
	got := releasedOrFail(t, filepath.Join(app, "gen/store/ticket_types/migrations.go"))
	if got[0] != released(t, "ticket_types_statement.sql") {
		t.Errorf("the released statement's bytes changed:\n%q", got[0])
	}
}

// feesTOML is the scratch resource for what tickets cannot show: money
// in both form groups, a three-decimal currency, an optional field.
const feesTOML = `name = "fees"
route = "/fees"
store = "exclusive"
currency = "KWD"

[list]
columns = [{ field = "Name" }, { field = "Price", kind = "money" }]

[form]
basics   = [{ name = "Name", required = true }, { name = "Price", kind = "money", required = true }]
advanced = [{ name = "Note", required = true }, { name = "Deposit", kind = "money" }]
`

const feesAppTOML = `locales = ["en", "de"]
default_locale = "en"
`

// feesFlows are the money flows in two locales, run in the fees app.
// Every edit posts what the browser would: each field's text with its
// locale, currency and seed. A seed is the text the English edit form
// drew, so en "1.500" KWD is 1.5 dinars; read as German it would be
// 1500 dinars, and with no seed en "1.500" reads two ways.
const feesFlows = `
func fee(t *testing.T, db *sql.DB) {
	exec(t, db, "DELETE FROM fees")
	exec(t, db, ` + "`" + `INSERT INTO fees (id, name, price_minor, price_currency, note, deposit_minor, deposit_currency, created_at, updated_at)
		VALUES (1, 'Fee', 1500, 'KWD', 'n', 2500, 'KWD', 'x', 'x')` + "`" + `)
}

func minor(t *testing.T, db *sql.DB, col string) sql.NullInt64 {
	var v sql.NullInt64
	if err := db.QueryRow("SELECT " + col + " FROM fees WHERE id = 1").Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func basics(name, price, locale, seed string) url.Values {
	return url.Values{"Name": {name}, "Price": {price}, "Price.locale": {locale}, "Price.currency": {"KWD"}, "Price.seed": {seed}}
}

func advanced(note, deposit, locale, seed string) url.Values {
	return url.Values{"Note": {note}, "Deposit": {deposit}, "Deposit.locale": {locale}, "Deposit.currency": {"KWD"}, "Deposit.seed": {seed}}
}

func TestFlows(t *testing.T) {
	h, db := start(t, feesstore.Migrations, nil)

	t.Run("en to de, Basics", func(t *testing.T) {
		fee(t, db)
		want(t, do(t, h, "GET", "/fees/1/edit", nil), 200, ` + "`" + `value="1.500"` + "`" + `, ` + "`" + `name="Price.locale" value="en"` + "`" + `)
		want(t, do(t, h, "POST", "/de/fees/1/edit-basics", basics("", "1.500", "en", "1.500")), 400,
			` + "`" + `value="1.500"` + "`" + `, ` + "`" + `name="Price.locale" value="en"` + "`" + `,
			` + "`" + `value="2,500"` + "`" + `, ` + "`" + `name="Deposit.locale" value="de"` + "`" + `)
		want(t, do(t, h, "POST", "/de/fees/1/edit-basics", basics("Fee", "1.500", "en", "1.500")), 303)
		if got := minor(t, db, "price_minor"); got.Int64 != 1500 {
			t.Errorf("price_minor %v, want 1500", got)
		}
	})

	t.Run("en to de, Advanced", func(t *testing.T) {
		fee(t, db)
		want(t, do(t, h, "POST", "/de/fees/1/edit-advanced", advanced("", "2.500", "en", "2.500")), 400,
			` + "`" + `value="2.500"` + "`" + `, ` + "`" + `name="Deposit.locale" value="en"` + "`" + `,
			` + "`" + `value="1,500"` + "`" + `, ` + "`" + `name="Price.locale" value="de"` + "`" + `)
		want(t, do(t, h, "POST", "/de/fees/1/edit-advanced", advanced("x", "2.500", "en", "2.500")), 303)
		if got := minor(t, db, "deposit_minor"); got.Int64 != 2500 {
			t.Errorf("deposit_minor %v, want 2500", got)
		}
	})

	t.Run("seeds, KWD", func(t *testing.T) {
		fee(t, db)
		want(t, do(t, h, "GET", "/fees/1/edit", nil), 200, ` + "`" + `value="1.500"` + "`" + `, ` + "`" + `name="Price.seed" value="1.500"` + "`" + `)
		want(t, do(t, h, "POST", "/fees/1/edit-basics", basics("Fee", "1.500", "en", "1.500")), 303)
		if got := minor(t, db, "price_minor"); got.Int64 != 1500 {
			t.Errorf("an unchanged seed stored %v, want 1500", got)
		}
		create := url.Values{"Name": {"New"}, "Price": {"1.500"}, "Price.locale": {"en"}, "Price.currency": {"KWD"}, "Note": {"n"}, "Deposit.locale": {"en"}, "Deposit.currency": {"KWD"}}
		want(t, do(t, h, "POST", "/fees", create), 400, "1.5 KWD", "1500 KWD")

		// The record changed between the GET and the POST.
		exec(t, db, "UPDATE fees SET price_minor = 2000 WHERE id = 1")
		want(t, do(t, h, "POST", "/fees/1/edit-basics", basics("Fee", "1.500", "en", "1.500")), 400,
			` + "`" + `value="2.000"` + "`" + `, ` + "`" + `name="Price.seed" value="2.000"` + "`" + `)
		if got := minor(t, db, "price_minor"); got.Int64 != 2000 {
			t.Errorf("a stale seed stored %v", got)
		}

		// Two submissions around an error on another field.
		fee(t, db)
		want(t, do(t, h, "POST", "/fees/1/edit-basics", basics("", "1.500", "en", "1.500")), 400,
			` + "`" + `value="1.500"` + "`" + `, ` + "`" + `name="Price.locale" value="en"` + "`" + `, ` + "`" + `name="Price.seed" value="1.500"` + "`" + `)
		want(t, do(t, h, "POST", "/fees/1/edit-basics", basics("Fee", "1.500", "en", "1.500")), 303)
		if got := minor(t, db, "price_minor"); got.Int64 != 1500 {
			t.Errorf("the resubmitted seed stored %v, want 1500", got)
		}
		fee(t, db)
		want(t, do(t, h, "POST", "/fees/1/edit-basics", basics("", "1.500", "en", "1.500")), 400)
		exec(t, db, "UPDATE fees SET price_minor = 2000 WHERE id = 1")
		want(t, do(t, h, "POST", "/fees/1/edit-basics", basics("Fee", "1.500", "en", "1.500")), 400,
			` + "`" + `value="2.000"` + "`" + `, ` + "`" + `name="Price.seed" value="2.000"` + "`" + `)
		if got := minor(t, db, "price_minor"); got.Int64 != 2000 {
			t.Errorf("a seed resubmitted after the record changed stored %v", got)
		}
	})

	t.Run("clearing", func(t *testing.T) {
		fee(t, db)
		want(t, do(t, h, "POST", "/fees/1/edit-advanced", advanced("x", "", "en", "")), 303)
		var minor sql.NullInt64
		var cur sql.NullString
		db.QueryRow("SELECT deposit_minor, deposit_currency FROM fees WHERE id = 1").Scan(&minor, &cur)
		if minor.Valid || cur.Valid {
			t.Errorf("a cleared deposit stored %v %v", minor, cur)
		}
		body := want(t, do(t, h, "GET", "/fees/1", nil), 200, "1.500")
		if strings.Contains(body, "2.500") {
			t.Errorf("the show page still shows the cleared deposit:\n%s", body)
		}
	})
}
`

// Spec §12 items 4, 5 and 15 where tickets cannot show them: money in
// both form groups, a three-decimal currency, two locales, an optional
// field cleared.
func TestMoneyFlowsInTwoLocales(t *testing.T) {
	app := scratchApp(t, map[string]string{"manifest/fees.toml": feesTOML, "manifest/app.toml": feesAppTOML})
	mustGenerate(t, app)
	writeSteps(t, app, true, "\n\tfeesstore \"scratch/gen/store/fees\"\n", feesFlows)
	out, err := runSteps(app, "TestFlows", "DB="+filepath.Join(app, "fees.db"))
	if err != nil {
		t.Fatalf("the money flows failed: %v\n%s", err, out)
	}
	for _, flow := range []string{"en_to_de,_Basics", "en_to_de,_Advanced", "seeds,_KWD", "clearing"} {
		if !strings.Contains(out, "--- PASS: TestFlows/"+flow+" ") {
			t.Errorf("flow %s did not run:\n%s", flow, out)
		}
	}
}

// Spec §1 and §7: a money field added to a resource already released
// needs no hand migration. The generator appends nothing for it; Boot
// adds both columns, and the existing row has no amount.
func TestMoneyFieldAddedLater(t *testing.T) {
	withoutDeposit := strings.Replace(feesTOML, `, { name = "Deposit", kind = "money" }`, "", 1)
	app := scratchApp(t, map[string]string{"manifest/fees.toml": withoutDeposit})
	mustGenerate(t, app)
	db := filepath.Join(app, "fees.db")
	const steps = `
func TestStep(t *testing.T) {
	h, db := start(t, feesstore.Migrations, nil)
	switch os.Getenv("STEP") {
	case "create":
		want(t, do(t, h, "POST", "/fees", url.Values{"Name": {"Fee"}, "Price": {"1.5"}, "Price.locale": {"en"}, "Price.currency": {"KWD"}, "Note": {"n"}}), 303)
	case "after":
		var minor sql.NullInt64
		var cur sql.NullString
		if err := db.QueryRow("SELECT deposit_minor, deposit_currency FROM fees WHERE id = 1").Scan(&minor, &cur); err != nil || minor.Valid || cur.Valid {
			t.Fatalf("the added deposit: %v %v %v", minor, cur, err)
		}
		want(t, do(t, h, "GET", "/fees/1", nil), 200, "1.500")
		want(t, do(t, h, "POST", "/fees/1/edit-advanced", url.Values{"Note": {"n"}, "Deposit": {"5"}, "Deposit.locale": {"en"}, "Deposit.currency": {"KWD"}}), 303)
		if err := db.QueryRow("SELECT deposit_minor, deposit_currency FROM fees WHERE id = 1").Scan(&minor, &cur); err != nil || minor.Int64 != 5000 || cur.String != "KWD" {
			t.Fatalf("the deposit set: %v %v %v", minor, cur, err)
		}
	default:
		t.Fatalf("unknown step %q", os.Getenv("STEP"))
	}
}
`
	writeSteps(t, app, true, "\n\tfeesstore \"scratch/gen/store/fees\"\n", steps)
	runInApp(t, app, "create", db)

	writeFile(t, app, "manifest/fees.toml", feesTOML)
	before := releasedOrFail(t, filepath.Join(app, "gen/store/fees/migrations.go"))
	mustGenerate(t, app)
	if after := releasedOrFail(t, filepath.Join(app, "gen/store/fees/migrations.go")); len(after) != len(before) {
		t.Fatalf("adding a money field appended %q", after[len(before):])
	}
	runInApp(t, app, "after", db)
}

// The edit POST whose own group has no money but can fail draws the
// other group's money inputs from the record; a list-only money column
// has no form field at all. Both shapes must compile.
func TestMoneyOnlyInTheOtherGroupBuilds(t *testing.T) {
	app := scratchApp(t, map[string]string{"manifest/dues.toml": `name = "dues"
route = "/dues"
store = "exclusive"
currency = "JPY"

[list]
columns = [{ field = "Title" }, { field = "Total", kind = "money" }]

[form]
basics   = [{ name = "Title", required = true }]
advanced = [{ name = "Amount", kind = "money" }]
`})
	mustGenerate(t, app)
	if out, err := goBuild(app); err != nil {
		t.Fatalf("the generated app does not build:\n%s", out)
	}
}
