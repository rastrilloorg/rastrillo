package paritytest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/api"
	"amadan.net/rastrillo/rastrillo/mcp"
	"amadan.net/rastrillo/rastrillo/op"
	"amadan.net/rastrillo/rastrillo/route"
)

// The example app is a notes app with one page per operation, built
// over chi or a ServeMux, whole or broken one way per exampleOptions
// field, so each check is shown passing and failing on a real app.

type createInput struct {
	Title string `json:"title"`
}

type noteResult struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

type listInput struct{}

type listResult struct {
	Notes []noteResult `json:"notes"`
}

type deleteInput struct {
	ID int64 `json:"id"`
}

type tokenInput struct {
	Agent string `json:"agent"`
}

type tokenResult struct {
	Token string `json:"token"`
}

type exportResult struct {
	Text string `json:"text"`
}

// exampleOptions breaks the app. The zero value is the whole app.
type exampleOptions struct {
	// apiLeavesOut, mcpLeavesOut and pageLeavesOut each name an
	// operation that door doesn't get.
	apiLeavesOut, mcpLeavesOut, pageLeavesOut string
	// apiExtra and mcpExtra serve the undeclared write notes.export.
	apiExtra, mcpExtra bool
	// wrongOperation serves notes.create's route at notes.delete's
	// path, and leaves notes.delete out of the API.
	wrongOperation bool
	// catchAll mounts the API at /api/v2/, and answers 401 to
	// everything else under /api/v1/ but MCP.
	catchAll bool
	// humanOnlyCallable gives both doors a tokens.create that isn't
	// human-only and mints without Call.
	humanOnlyCallable bool
	// extraMark registers GET /extra with the mark it returns.
	extraMark func(app *exampleApp) route.Mark
	// pageSkipsValidation stores a POST /notes title without Call.
	pageSkipsValidation bool
	// directRoutes registers GET /direct, and mounts a handler at
	// /files/, on a chi router itself, outside the table.
	directRoutes bool
	// webhookOnRouter registers POST /api/v1/webhooks/stripe on a chi
	// router itself, under the API's prefix but outside both doors.
	webhookOnRouter bool
	// humanOnlyElsewhere also serves the unsafe tokens.create at
	// POST /api/v1/tokens/mint, beside the declared one at its route.
	humanOnlyElsewhere bool
}

type exampleApp struct {
	options exampleOptions
	mutex   sync.Mutex
	notes   map[int64]string
	lastID  int64
	minted  []string
	create  *op.Named[createInput, noteResult]
	list    *op.Named[listInput, listResult]
	remove  *op.Named[deleteInput, op.None]
	tokens  *op.Named[tokenInput, tokenResult]
	// export is declared by no page and listed nowhere: the broken
	// variants serve it.
	export *op.Named[listInput, exportResult]
}

func newExampleApp(options exampleOptions) *exampleApp {
	app := &exampleApp{options: options, notes: map[int64]string{}}
	app.create = op.Define("notes.create", op.Spec{
		Description: "Create a note.", Access: rastrillo.Write,
	}, app.createNote)
	app.list = op.Define("notes.list", op.Spec{
		Description: "List the notes.", Access: rastrillo.Read,
		InQuery: true,
	}, app.listNotes)
	app.remove = op.Define("notes.delete", op.Spec{
		Description: "Delete a note.", Access: rastrillo.Write,
		Destructive: true, Idempotent: true,
	}, app.deleteNote)
	app.tokens = op.Define("tokens.create", op.Spec{
		Description: "Create an API token.", Access: rastrillo.Write,
		HumanOnly: "an agent must not raise its own access",
	}, app.mintToken)
	app.export = op.Define("notes.export", op.Spec{
		Description: "Export the notes as text.", Access: rastrillo.Write,
	}, app.exportNotes)
	return app
}

// operations are the four the app declares.
func (app *exampleApp) operations() []op.Descriptor {
	return []op.Descriptor{
		app.create.Descriptor, app.list.Descriptor,
		app.remove.Descriptor, app.tokens.Descriptor,
	}
}

func (app *exampleApp) createNote(
	_ context.Context, _ *rastrillo.Ctx, input createInput,
) (noteResult, error) {
	if strings.TrimSpace(input.Title) == "" {
		return noteResult{}, op.Invalid{
			Fields: map[string]string{"Title": "Enter a title."},
		}
	}
	return app.store(input.Title), nil
}

func (app *exampleApp) store(title string) noteResult {
	app.mutex.Lock()
	defer app.mutex.Unlock()
	app.lastID++
	app.notes[app.lastID] = title
	return noteResult{ID: app.lastID, Title: title}
}

func (app *exampleApp) listNotes(
	context.Context, *rastrillo.Ctx, listInput,
) (listResult, error) {
	app.mutex.Lock()
	defer app.mutex.Unlock()
	listing := listResult{Notes: []noteResult{}}
	for _, id := range slices.Sorted(maps.Keys(app.notes)) {
		listing.Notes = append(listing.Notes,
			noteResult{ID: id, Title: app.notes[id]})
	}
	return listing, nil
}

func (app *exampleApp) deleteNote(
	_ context.Context, _ *rastrillo.Ctx, input deleteInput,
) (op.None, error) {
	app.mutex.Lock()
	defer app.mutex.Unlock()
	if _, found := app.notes[input.ID]; !found {
		return op.None{}, op.NotFound{}
	}
	delete(app.notes, input.ID)
	return op.None{}, nil
}

func (app *exampleApp) mintToken(
	_ context.Context, _ *rastrillo.Ctx, input tokenInput,
) (tokenResult, error) {
	app.mutex.Lock()
	defer app.mutex.Unlock()
	token := fmt.Sprintf("token-%d-%s", len(app.minted)+1, input.Agent)
	app.minted = append(app.minted, token)
	return tokenResult{Token: token}, nil
}

func (app *exampleApp) exportNotes(
	ctx context.Context, rc *rastrillo.Ctx, input listInput,
) (exportResult, error) {
	listing, err := app.listNotes(ctx, rc, input)
	var titles []string
	for _, each := range listing.Notes {
		titles = append(titles, each.Title)
	}
	return exportResult{Text: strings.Join(titles, "\n")}, err
}

func (app *exampleApp) mintedCount() int {
	app.mutex.Lock()
	defer app.mutex.Unlock()
	return len(app.minted)
}

// authorize is the app's policy: person b is a viewer, refused every
// write.
func authorize(
	_ context.Context, actor rastrillo.Actor, operation rastrillo.Operation,
) error {
	if operation.Access == rastrillo.Write && actor.Subject == "b" {
		return op.Forbidden{Reason: "b is a viewer"}
	}
	return nil
}

// exampleTokens are the tokens the doors accept: agents' write tokens
// for a and b, an agent's read token for a, and a's own token, which
// names no agent.
var exampleTokens = map[string]rastrillo.Actor{
	"writer-a": {Subject: "a", Agent: "Claude", Credential: "tok-writer-a",
		Access: rastrillo.Write},
	"writer-b": {Subject: "b", Agent: "Claude", Credential: "tok-writer-b",
		Access: rastrillo.Write},
	"reader-a": {Subject: "a", Agent: "Claude", Credential: "tok-reader-a",
		Access: rastrillo.Read},
	"person-a": {Subject: "a", Credential: "tok-person-a",
		Access: rastrillo.Write},
}

type mapVerifier map[string]rastrillo.Actor

func (tokens mapVerifier) Verify(
	_ context.Context, request *http.Request,
) (rastrillo.Actor, error) {
	token, _ := strings.CutPrefix(
		request.Header.Get("Authorization"), "Bearer ")
	actor, known := tokens[token]
	if !known {
		return rastrillo.Actor{},
			fmt.Errorf("example: unknown token: %w", api.ErrUnauthorized)
	}
	return actor, nil
}

// The pages. Each reads the form, calls one operation and answers its
// refusal, as skill/code.md's actions do, with a plain-text answer a
// test can read: 422 is one Go field name per line.

// sessionContext is the page's Ctx: the person the test names in
// X-Example-Person, signed in with a browser session.
func sessionContext(request *http.Request) *rastrillo.Ctx {
	return &rastrillo.Ctx{
		Actor: rastrillo.Actor{
			Subject: request.Header.Get("X-Example-Person"),
			Access:  rastrillo.Write,
		},
		Authorize: authorize,
	}
}

// refusedPage answers err, if there is one, and says whether it did.
func refusedPage(response http.ResponseWriter, err error) bool {
	var invalid op.Invalid
	switch {
	case err == nil:
		return false
	case errors.As(err, &invalid):
		response.WriteHeader(http.StatusUnprocessableEntity)
		for _, name := range slices.Sorted(maps.Keys(invalid.Fields)) {
			fmt.Fprintln(response, name)
		}
	case errors.As(err, new(op.Forbidden)):
		response.WriteHeader(http.StatusForbidden)
	case errors.As(err, new(op.NotFound)):
		response.WriteHeader(http.StatusNotFound)
	default:
		http.Error(response, err.Error(), http.StatusInternalServerError)
	}
	return true
}

func (app *exampleApp) createPage(
	response http.ResponseWriter, request *http.Request,
) {
	title := request.PostFormValue("title")
	if app.options.pageSkipsValidation {
		app.store(title)
		http.Redirect(response, request, "/notes", http.StatusSeeOther)
		return
	}
	_, err := app.create.Call(request.Context(), sessionContext(request),
		createInput{Title: title})
	if refusedPage(response, err) {
		return
	}
	http.Redirect(response, request, "/notes", http.StatusSeeOther)
}

func (app *exampleApp) listPage(
	response http.ResponseWriter, request *http.Request,
) {
	listing, err := app.list.Call(request.Context(),
		sessionContext(request), listInput{})
	if refusedPage(response, err) {
		return
	}
	for _, each := range listing.Notes {
		fmt.Fprintln(response, each.Title)
	}
}

func (app *exampleApp) deletePage(
	response http.ResponseWriter, request *http.Request,
) {
	id, err := strconv.ParseInt(request.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(response, request)
		return
	}
	_, err = app.remove.Call(request.Context(), sessionContext(request),
		deleteInput{ID: id})
	if refusedPage(response, err) {
		return
	}
	http.Redirect(response, request, "/notes", http.StatusSeeOther)
}

func (app *exampleApp) tokenPage(
	response http.ResponseWriter, request *http.Request,
) {
	_, err := app.tokens.Call(request.Context(), sessionContext(request),
		tokenInput{Agent: request.PostFormValue("agent")})
	if refusedPage(response, err) {
		return
	}
	http.Redirect(response, request, "/settings/tokens",
		http.StatusSeeOther)
}

func plainPage(response http.ResponseWriter, _ *http.Request) {
	fmt.Fprintln(response, "body {}")
}

// registerPages puts every page on table, and the settings page on a
// sub-router through On, so the check reads a sub-router's full path.
func (app *exampleApp) registerPages(
	table, settings *route.Table,
) {
	pages := []struct {
		pattern   string
		handler   http.HandlerFunc
		operation op.Descriptor
		table     *route.Table
	}{
		{"GET /notes", app.listPage, app.list.Descriptor, table},
		{"POST /notes", app.createPage, app.create.Descriptor, table},
		{"POST /notes/{id}/delete", app.deletePage,
			app.remove.Descriptor, table},
		{"POST /tokens", app.tokenPage, app.tokens.Descriptor, settings},
	}
	for _, page := range pages {
		if page.operation.ID == app.options.pageLeavesOut {
			continue
		}
		page.table.Handle(page.pattern, page.handler,
			route.Operation(page.operation))
	}
	table.HandleFunc("GET /style.css", plainPage,
		route.PageOnly("the stylesheet"))
	if app.options.extraMark != nil {
		table.HandleFunc("GET /extra", plainPage, app.options.extraMark(app))
	}
}

// doors builds the API and MCP endpoints, each with the operations its
// variant gives it.
func (app *exampleApp) doors(
	t testing.TB,
) (apiHandler, mcpHandler http.Handler) {
	t.Helper()
	config := api.Config{
		Verify: mapVerifier(exampleTokens),
		Ctx: func(*http.Request) *rastrillo.Ctx {
			return &rastrillo.Ctx{}
		},
		Authorize:           authorize,
		Logger:              slog.New(slog.DiscardHandler),
		RequireAgentSession: true,
	}
	apiHandler, err := api.New(config, app.apiRoutes()...)
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}
	mcpHandler, err = mcp.New(mcp.Config{
		Gate: config.Gate(), Ctx: config.Ctx, Authorize: authorize,
		Name: "notes", Version: "v1",
	}, app.doorOperations(app.options.mcpLeavesOut, app.options.mcpExtra)...)
	if err != nil {
		t.Fatalf("mcp.New: %v", err)
	}
	return apiHandler, mcpHandler
}

func (app *exampleApp) apiRoutes() []api.Route {
	leavesOut := app.options.apiLeavesOut
	if app.options.wrongOperation {
		leavesOut = app.remove.Descriptor.ID
	}
	routes := api.Operations(
		app.doorOperations(leavesOut, app.options.apiExtra)...)
	if app.options.wrongOperation {
		misplaced := api.Operations(app.create.Descriptor)[0]
		misplaced.Pattern = "/api/v1/notes/delete"
		routes = append(routes, misplaced)
	}
	if app.options.humanOnlyElsewhere {
		minting := api.Operations(app.unsafeTokens())[0]
		minting.Pattern = "/api/v1/tokens/mint"
		routes = append(routes, minting)
	}
	return routes
}

// doorOperations is what one door gets: the declared operations less
// leavesOut, with notes.export when extra is set.
func (app *exampleApp) doorOperations(
	leavesOut string, extra bool,
) []op.Descriptor {
	var descriptors []op.Descriptor
	for _, descriptor := range app.operations() {
		if descriptor.ID == leavesOut {
			continue
		}
		if descriptor.ID == app.tokens.Descriptor.ID &&
			app.options.humanOnlyCallable {
			descriptor = app.unsafeTokens()
		}
		descriptors = append(descriptors, descriptor)
	}
	if extra {
		descriptors = append(descriptors, app.export.Descriptor)
	}
	return descriptors
}

// unsafeTokens is tokens.create as a door must never serve it: not
// human-only, and minting straight from its Invoke.
func (app *exampleApp) unsafeTokens() op.Descriptor {
	unsafe := app.tokens.Descriptor
	unsafe.HumanOnly = ""
	unsafe.Invoke = func(
		ctx context.Context, rc *rastrillo.Ctx, arguments json.RawMessage,
	) (any, error) {
		var input tokenInput
		if err := op.DecodeInput(arguments, &input); err != nil {
			return nil, err
		}
		return app.mintToken(ctx, rc, input)
	}
	return unsafe
}

// catchAllAnswer is an outer handler's 401, in the API's own shape, so
// only the missing Rastrillo-Operation header tells it from the tree.
func catchAllAnswer(response http.ResponseWriter, _ *http.Request) {
	api.WriteError(response, http.StatusUnauthorized, api.Error{
		Code: "unauthorized", Message: "Sign in.",
	})
}

// buildExample builds the app over router, "chi" or "ServeMux".
func buildExample(
	t testing.TB, router string, options exampleOptions,
) (Surface, *exampleApp) {
	t.Helper()
	app := newExampleApp(options)
	apiHandler, mcpHandler := app.doors(t)
	surface := Surface{
		Operations: app.operations(),
		API:        apiHandler,
		MCP:        mcpHandler,
		Token:      "writer-a",
	}
	var table *route.Table
	switch router {
	case "chi":
		table = app.overChi(&surface)
	case "ServeMux":
		table = app.overServeMux(&surface)
	default:
		t.Fatalf("buildExample: no router %q", router)
	}
	surface.Routes = table.List()
	return surface, app
}

func (app *exampleApp) overChi(surface *Surface) *route.Table {
	router := chi.NewRouter()
	table := route.New(route.Chi(router))
	router.Route("/settings", func(sub chi.Router) {
		app.registerPages(table, table.On(route.Chi(sub), "/settings"))
	})
	apiPrefix := "/api/v1/"
	if app.options.catchAll {
		apiPrefix = "/api/v2/"
		router.Mount("/api/v1/", http.HandlerFunc(catchAllAnswer))
	}
	router.Mount(apiPrefix, surface.API)
	router.Mount("/api/v1/mcp", surface.MCP)
	if app.options.directRoutes {
		router.Get("/direct", plainPage)
		router.Mount("/files/", http.NotFoundHandler())
	}
	if app.options.webhookOnRouter {
		router.Post("/api/v1/webhooks/stripe", plainPage)
	}
	surface.Handler, surface.Router = router, router
	return table
}

func (app *exampleApp) overServeMux(surface *Surface) *route.Table {
	mux, settings := http.NewServeMux(), http.NewServeMux()
	mux.Handle("/settings/", http.StripPrefix("/settings", settings))
	table := route.New(route.Mux(mux))
	app.registerPages(table, table.On(route.Mux(settings), "/settings"))
	apiPrefix := "/api/v1/"
	if app.options.catchAll {
		apiPrefix = "/api/v2/"
		mux.HandleFunc("/api/v1/", catchAllAnswer)
	}
	mux.Handle(apiPrefix, surface.API)
	mux.Handle("/api/v1/mcp", surface.MCP)
	surface.Handler = mux
	return table
}
