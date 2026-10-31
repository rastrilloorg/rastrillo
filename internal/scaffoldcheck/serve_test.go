package scaffoldcheck

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bookmarksManifest is a resource a person owns, so the JSON API
// exposes it and a token may create one for its owner.
const bookmarksManifest = `name  = "bookmarks"
route = "/bookmarks"
store = "mergeable"
scope = "user"

[list]
columns = [{ field = "Title" }]

[form]
basics = [{ name = "Title", required = true }]
`

// bearerTest is written into the app's test package. The API needs no
// Origin, because the token is its only credential, and refuses a
// request without one.
const bearerTest = `package apiapptest

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/apitoken"
	"amadan.net/rastrillo/rastrillo/db"

	"apiapp/internal/apiapp"
)

func serve(
	handler http.Handler, request *http.Request,
) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestABearerTokenNeedsNoOrigin(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	database, err := db.Open(schema.Path(t), logger)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	handler, err := apiapp.New(database, testOrigin, logger)
	if err != nil {
		t.Fatal(err)
	}
	store, err := apitoken.New(apitoken.Config{DB: database.Writer(),
		Prefix: apitoken.DerivePrefix("apiapp")})
	if err != nil {
		t.Fatal(err)
	}
	secret, _, err := store.Mint(context.Background(), "ada@example.com",
		apitoken.MintInput{Agent: "Claude", Access: rastrillo.Write})
	if err != nil {
		t.Fatal(err)
	}
	create := httptest.NewRequest("POST", "/api/v1/bookmarks",
		strings.NewReader(` + "`" + `{"title": "Read later"}` + "`" + `))
	create.Header.Set("Authorization", "Bearer "+secret)
	create.Header.Set("Content-Type", "application/json")
	if answer := serve(handler, create); answer.Code != http.StatusCreated {
		t.Errorf("POST /api/v1/bookmarks: %d, want 201:\n%s",
			answer.Code, answer.Body)
	}
	bare := serve(handler, httptest.NewRequest("GET", "/api/v1", nil))
	var body struct{ Error struct{ Code string } }
	json.Unmarshal(bare.Body.Bytes(), &body)
	if bare.Code != http.StatusNotFound || body.Error.Code != "not_found" {
		t.Errorf("GET /api/v1: %d, want the API's JSON 404:\n%s",
			bare.Code, bare.Body)
	}
	anonymous := httptest.NewRequest("POST", "/api/v1/bookmarks",
		strings.NewReader(` + "`" + `{"title": "Read later"}` + "`" + `))
	if answer := serve(handler, anonymous); answer.Code != 401 {
		t.Errorf("POST /api/v1/bookmarks with no token: %d, want 401",
			answer.Code)
	}
}
`

// A bearer-token POST reaches the API with no Origin header, so an
// agent's request is not refused by the check that guards the pages; a
// bare /api/v1 is the API's own JSON 404.
func TestTheAPIAnswersABearerTokenWithoutAnOrigin(t *testing.T) {
	t.Parallel()
	directory := buildableApp(t, "apiapp")
	write(t, filepath.Join(directory, "manifest", "bookmarks.toml"),
		bookmarksManifest)
	run(t, directory, rastrilloBinary, "generate")
	bootWithTheEventLog(t, filepath.Join(directory,
		"internal", "apiapp", "migrations.go"))
	write(t, filepath.Join(directory, "internal", "apiapptest",
		"bearer_extra_test.go"), bearerTest)
	runTests(t, directory, "./internal/apiapptest/",
		"TestABearerTokenNeedsNoOrigin")
}

// runTests runs the named tests in the app and fails unless each ran
// and passed: a pattern that matched nothing would pass silently.
func runTests(t *testing.T, directory, packages string, names ...string) {
	t.Helper()
	runTaggedTests(t, directory, "", packages, names...)
}

// runTaggedTests is runTests with build tags, such as browser. A skipped
// test prints no PASS line, so it fails here too.
func runTaggedTests(
	t *testing.T, directory, tags, packages string, names ...string,
) {
	t.Helper()
	pattern := "^(" + strings.Join(names, "|") + ")$"
	output := run(t, directory, "go", "test", "-count=1", "-v",
		"-tags="+tags, "-run", pattern, packages)
	for _, name := range names {
		if !strings.Contains(output, "--- PASS: "+name+" ") {
			t.Errorf("%s did not run and pass:\n%s", name, output)
		}
	}
}

// bootWithTheEventLog adds the mergeable store's tables to BootSchema,
// which the generated resource reads and writes.
func bootWithTheEventLog(t *testing.T, path string) {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const before = "migrate.Merge(apitoken.Schema, Schema)"
	if !strings.Contains(string(source), before) {
		t.Fatalf("%s has no %s to extend:\n%s", path, before, source)
	}
	edited := strings.Replace(string(source), before,
		"migrate.Merge(eventlog.Schema, apitoken.Schema, Schema)", 1)
	edited = strings.Replace(edited, "import (",
		"import (\n\t\"amadan.net/rastrillo/rastrillo/eventlog\"", 1)
	write(t, path, edited)
}

// languageTest is written into the app's test package. It serves the app
// as main.go does, under a base path, and asks for the Spanish home.
const languageTest = `package langapptest

import (
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/basepath"

	genlocales "langapp/gen/locales"
	"langapp/internal/langapp"
)

func TestTheHomePageIsInTheRequestsLanguage(t *testing.T) {
	options := rastrillo.Options{
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Locales: genlocales.Locales, DefaultLocale: genlocales.DefaultLocale,
		BasePath: basepath.Config{Origin: "http://localhost:8080",
			Slug: "acme"},
	}
	langapp.Configure(&options, newApp(t).(*http.ServeMux), time.Now())
	handler, closeAll, err := rastrillo.Handler(options)
	if err != nil {
		t.Fatal(err)
	}
	defer closeAll()
	request := httptest.NewRequest("GET", "/acme/es/", nil)
	request.Host = "localhost:8080"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	skip := rastrillo.BaseCatalogs()["es"]["rastrillo.ui.shell_skip"]
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK ||
		!strings.Contains(body, ` + "`" + `<html lang="es"` + "`" + `) ||
		!strings.Contains(body, template.HTMLEscapeString(skip)) ||
		!strings.Contains(body, "href=\"/acme/static/tokens.") {
		t.Errorf("GET /acme/es/: %d, want the Spanish home:\n%s",
			recorder.Code, body)
	}
}
`

// refusalTest is written into the app's own package, beside the code it
// tests: contextFor is the Ctx every action is given.
const refusalTest = `package langapp

import (
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/action"
	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/op"
)

func TestARefusalRendersTheErrorPage(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	database, err := db.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	app := &App{database: database, logger: logger}
	request := httptest.NewRequest("GET", "/missing", nil)
	request.Header.Set("Accept-Language", "es")
	ctx, err := app.contextFor(request)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	action.NewVisit(recorder, request, ctx).Refused(op.NotFound{})
	title := rastrillo.BaseCatalogs()["es"]["rastrillo.ui.error_404_title"]
	if recorder.Code != http.StatusNotFound || !strings.Contains(
		recorder.Body.String(), template.HTMLEscapeString(title)) {
		t.Errorf("a refused visit: %d, want the Spanish 404 page:\n%s",
			recorder.Code, recorder.Body)
	}
}
`

// Pages render through visit.Render, with the request's language and
// base path, and a refused visit answers with the app's own error page.
func TestPagesRenderInTheRequestsLanguage(t *testing.T) {
	t.Parallel()
	directory := buildableApp(t, "langapp")
	write(t, filepath.Join(directory, "manifest", "app.toml"),
		"locales = [\"en\", \"es\"]\ndefault_locale = \"en\"\n")
	run(t, directory, rastrilloBinary, "generate")
	write(t, filepath.Join(directory, "internal", "langapptest",
		"language_extra_test.go"), languageTest)
	write(t, filepath.Join(directory, "internal", "langapp",
		"refusal_extra_test.go"), refusalTest)
	runTests(t, directory, "./internal/...",
		"TestTheHomePageIsInTheRequestsLanguage",
		"TestARefusalRendersTheErrorPage")
}

// csrfTest is written into the app's test package: a form POST from
// another origin to a page is refused, and one from the app's own is not.
const csrfTest = `package %[1]stest

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestACrossOriginFormIsRefused(t *testing.T) {
	handler := newApp(t)
	for origin, refused := range map[string]bool{
		"http://elsewhere.test": true, testOrigin: false,
	} {
		request := httptest.NewRequest("POST", %[2]q, nil)
		request.Header.Set("Content-Type",
			"application/x-www-form-urlencoded")
		request.Header.Set("Origin", origin)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if (recorder.Code == http.StatusForbidden) != refused {
			t.Errorf("POST %[2]s from %%s: %%d, refused should be %%v",
				origin, recorder.Code, refused)
		}
	}
}
`

// The CSRF guard is a real check, not only a name in the route table: a
// form POST to a page from another origin is refused. A plain app has
// no form yet, so this adds one to its page group, as a person would.
func TestPagesRefuseACrossOriginForm(t *testing.T) {
	t.Parallel()
	for _, variant := range []struct{ name, flag, path string }{
		{"formapp", "--auth=none", "/"},
		{"signoutapp", "--auth=magic", "/signout"},
	} {
		directory := buildableApp(t, variant.name, variant.flag)
		if variant.path == "/" {
			addHomeForm(t, filepath.Join(directory,
				"internal", variant.name, "routes.go"))
		}
		write(t, filepath.Join(directory, "internal", variant.name+"test",
			"csrf_extra_test.go"),
			fmt.Sprintf(csrfTest, variant.name, variant.path))
		runTests(t, directory, "./internal/"+variant.name+"test/",
			"TestACrossOriginFormIsRefused")
	}
}

// addHomeForm answers a POST to the home page inside the page group.
func addHomeForm(t *testing.T, path string) {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const home = "guarded.Get(homePage, app.home)"
	if !strings.Contains(string(source), home) {
		t.Fatalf("%s has no %s to add a form beside:\n%s", path, home, source)
	}
	write(t, path, strings.Replace(string(source), home,
		home+"\n\t\tguarded.Post(homePage, app.home)", 1))
}
