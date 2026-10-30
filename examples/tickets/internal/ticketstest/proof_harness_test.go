// proof_harness_test.go builds the app proof_test.go sends requests
// to, and compares its answers with the generated route's.
package ticketstest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/action"
	"amadan.net/rastrillo/rastrillo/locales"
	"github.com/go-chi/chi/v5"

	"tickets/gen"
	genlocales "tickets/gen/locales"
	ops_ticket_types "tickets/gen/ops/ticket_types"
	announcementsstore "tickets/gen/store/announcements"
	ticket_typesstore "tickets/gen/store/ticket_types"
	"tickets/internal/proof"
	"tickets/internal/tickets"
)

const editBasics = "/admin/ticket_types/1/edit-basics"

// updateFunc is the generated Update's signature. A test swaps it in
// to count saves, or to move the record's currency in the middle of
// one, which no request can do.
type updateFunc = func(
	ctx context.Context, rc *rastrillo.Ctx, id int64,
	in ops_ticket_types.UpdateInput,
) (ops_ticket_types.TicketType, error)

// proofSetup is what a test changes in the app newProofApp builds.
// The zero value is that app as newAppWithAt wires the generated one.
type proofSetup struct {
	update   updateFunc                   // nil: the generated Update
	database func(booted *sql.DB) *sql.DB // nil: the booted database
}

func newProofApp(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	return newProofAppWith(t, proofSetup{})
}

// newProofAppWith builds the tickets app as newAppWithAt does, with
// the proof's chi router in front of gen.Router for the one route it
// rewrites. Both sit inside the same app.Middleware, because the form
// the proof shows again needs the localizer that middleware sets.
func newProofAppWith(
	t *testing.T, setup proofSetup,
) (http.Handler, *sql.DB) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	update := setup.update
	if update == nil {
		update = ops_ticket_types.Update
	}
	var database *sql.DB
	handler, closeApp, err := rastrillo.Handler(rastrillo.Options{
		DBPath: filepath.Join(t.TempDir(), "tickets.db"),
		Migrations: slices.Concat(
			ticket_typesstore.Migrations, announcementsstore.Migrations,
		),
		BaseCatalog:   genlocales.BaseCatalog,
		Logger:        logger,
		Locales:       genlocales.Locales,
		DefaultLocale: genlocales.DefaultLocale,
		Router: func(opened *sql.DB) (*http.ServeMux, error) {
			database = opened
			booted, err := gen.Boot(context.Background(), opened)
			if err != nil {
				return nil, err
			}
			config, err := locales.CheckConfig(
				genlocales.DefaultLocale, genlocales.Locales,
			)
			if err != nil {
				return nil, err
			}
			app, err := tickets.NewApp(config)
			if err != nil {
				return nil, err
			}
			factory := func(*http.Request) *rastrillo.Ctx {
				ctx := &rastrillo.Ctx{
					DB: opened, Logger: logger,
					Actor: rastrillo.Actor{}, Render: app.Render,
				}
				if setup.database != nil {
					ctx.DB = setup.database(opened)
				}
				return ctx
			}
			router := chi.NewRouter()
			ticketTypes := &proof.TicketTypes{Update: update}
			ticketTypes.Mount(
				action.NewRoutes(
					router, bootChecked(opened, factory),
				),
			)
			outer := http.NewServeMux()
			outer.Handle("POST /admin/ticket_types/{id}/edit-basics",
				app.Middleware(router))
			outer.Handle("/", app.Middleware(gen.Router(booted, factory)))
			return outer, nil
		},
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	t.Cleanup(func() { closeApp() })
	return handler, database
}

// bootChecked is gen.Router's database check in front of factory: a
// nil DB is the booted one, and any other refuses the request, so the
// proof cannot serve a database gen.Boot never upgraded.
func bootChecked(
	booted *sql.DB, factory func(*http.Request) *rastrillo.Ctx,
) action.CtxFor {
	return func(request *http.Request) (*rastrillo.Ctx, error) {
		ctx := factory(request)
		if ctx.DB == nil {
			ctx.DB = booted
		}
		if ctx.DB != booted {
			return nil, errors.New("this request's database is not " +
				"the one gen.Boot checked")
		}
		return ctx, nil
	}
}

// parityCase is one request sent to both routers.
type parityCase struct {
	name   string
	path   string     // editBasics by default
	form   url.Values // nil when body is set
	body   string     // a raw body instead of form
	then   url.Values // non-nil: submit the re-shown form again
	status int        // what the generated route answers
	shows  []string   // HTML both answers must contain
}

// answered is one router's answer in the terms the parity test
// compares.
type answered struct {
	status   int
	location string
	body     string
	row      string
}

// ask sends example to a fresh app from build, with the same row
// seeded, and resubmits the form it shows again when example says so.
func ask(
	t *testing.T,
	build func(*testing.T) (http.Handler, *sql.DB),
	example parityCase,
) answered {
	t.Helper()
	app, database := build(t)
	seed(t, database, "VIP", 1250, "draft", "4")
	path := example.path
	if path == "" {
		path = editBasics
	}
	recorder := send(t, app, path, example)
	if example.then != nil {
		values := formInputs(t, recorder.Body.String())
		for name, value := range example.then {
			values[name] = value
		}
		recorder = post(t, app, path, values)
	}
	return answered{
		status:   recorder.Code,
		location: recorder.Header().Get("Location"),
		body:     recorder.Body.String(),
		row:      storedRow(t, database),
	}
}

func send(
	t *testing.T, app http.Handler, path string, example parityCase,
) *httptest.ResponseRecorder {
	t.Helper()
	if example.body == "" {
		return post(t, app, path, example.form)
	}
	request := httptest.NewRequest(http.MethodPost, path,
		strings.NewReader(example.body))
	request.Header.Set("Content-Type",
		"application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)
	return recorder
}

// compareAnswers holds the proof to the generated answer. A re-shown
// form is compared whole, so its field values, its messages with
// their arguments and its hidden money inputs all have to match.
func compareAnswers(t *testing.T, generated, proofed answered) {
	t.Helper()
	want := generated.status
	if want == http.StatusBadRequest && showsForm(generated.body) {
		want = http.StatusUnprocessableEntity
	}
	if proofed.status != want {
		t.Errorf("the proof answered %d, want %d:\n%s",
			proofed.status, want, proofed.body)
	}
	if proofed.location != generated.location {
		t.Errorf("the proof redirected to %q, the generated route "+
			"to %q", proofed.location, generated.location)
	}
	if proofed.row != generated.row {
		t.Errorf("the proof stored %q, the generated route %q",
			proofed.row, generated.row)
	}
	if proofed.body != generated.body {
		t.Errorf("the bodies differ: %s",
			firstDifference(generated.body, proofed.body))
	}
}

func showsForm(body string) bool {
	return strings.Contains(body, `name="Price.currency"`)
}

// firstDifference quotes both bodies around where they part, which is
// what a reader needs from two pages of HTML.
func firstDifference(generated, proofed string) string {
	at := 0
	for at < len(generated) && at < len(proofed) &&
		generated[at] == proofed[at] {
		at++
	}
	start := max(at-80, 0)
	return fmt.Sprintf("at byte %d\ngenerated: %q\nproof:     %q", at,
		generated[start:min(at+80, len(generated))],
		proofed[start:min(at+80, len(proofed))])
}

var (
	basicsForm = regexp.MustCompile(
		`(?s)<form[^>]*action="[^"]*/edit-basics".*?</form>`)
	inputTag   = regexp.MustCompile(`<input[^>]*>`)
	inputName  = regexp.MustCompile(`\bname="([^"]*)"`)
	inputValue = regexp.MustCompile(`\bvalue="([^"]*)"`)
)

// formInputs is what a browser would post from the re-shown basics
// form: every named input with its value, hidden ones included.
func formInputs(t *testing.T, body string) url.Values {
	t.Helper()
	basics := basicsForm.FindString(body)
	if basics == "" {
		t.Fatalf("no basics form to submit again in:\n%s", body)
	}
	values := url.Values{}
	for _, tag := range inputTag.FindAllString(basics, -1) {
		name := inputName.FindStringSubmatch(tag)
		if name == nil {
			continue
		}
		value := ""
		if found := inputValue.FindStringSubmatch(tag); found != nil {
			value = html.UnescapeString(found[1])
		}
		values.Add(html.UnescapeString(name[1]), value)
	}
	return values
}

func storedRow(t *testing.T, database *sql.DB) string {
	t.Helper()
	var name, code string
	var minor int64
	err := database.QueryRow(`SELECT name, price_minor, price_currency
		FROM ticket_types WHERE id = 1`).Scan(&name, &minor, &code)
	if err != nil {
		t.Fatalf("reading the stored row: %v", err)
	}
	return fmt.Sprintf("%s %d %s", name, minor, code)
}

func wantRow(t *testing.T, database *sql.DB, want string) {
	t.Helper()
	if got := storedRow(t, database); got != want {
		t.Errorf("stored %q, want %q", got, want)
	}
}

// withValues sets pairs of name and value on a copy of values.
func withValues(values url.Values, pairs ...string) url.Values {
	changed := url.Values{}
	for name, list := range values {
		changed[name] = slices.Clone(list)
	}
	for index := 0; index+1 < len(pairs); index += 2 {
		changed.Set(pairs[index], pairs[index+1])
	}
	return changed
}

func withoutValues(values url.Values, names ...string) url.Values {
	changed := withValues(values)
	for _, name := range names {
		changed.Del(name)
	}
	return changed
}
