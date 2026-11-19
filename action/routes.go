package action

import (
	"cmp"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"runtime"
	"slices"
	"strings"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/sessions"
	"github.com/go-chi/chi/v5"
)

// Handler is an action: one function of the visit it is given.
type Handler func(visit *Visit)

// CtxFor builds the per-request context. An error answers a logged 500,
// so a context that cannot be built never reaches an action. It is the
// place to fill a nil database with the booted one and to refuse a
// different one, which the generated router checks the same way.
type CtxFor func(request *http.Request) (*rastrillo.Ctx, error)

// Route is one line of the route table: what List reports so that a
// test, a doc or a check can read the app's routes without serving them.
type Route struct {
	Method  string
	Path    string
	Handler string

	// Action is a Get or Post handler's full identity, as
	// runtime.FuncForPC names it with the compiler's "-fm" dropped:
	// "example.com/app/internal/app.(*App).tagEdit". It is empty for
	// Handle and Mount, whose Handler is only a name the caller chose.
	// Two methods called Show on different types differ here and not
	// in Handler, so a check compares Action.
	Action string

	Guards []string
}

// Routes mounts handlers on a chi router and records each one. Nothing
// is built or called while routes are registered: an app can register
// every route before its stores exist, and a test can read List without
// a database.
type Routes struct {
	router   chi.Router
	ctxFor   CtxFor
	guards   []string
	recorded *[]Route

	// recording is true in a recorder: chi is given stand-ins for every
	// guard and handler, so nothing of the app is built or called.
	recording bool

	// prefix is the innermost Route's path without its trailing slash,
	// "" at the root. A path registered here must be it or start with
	// it and a slash, so that the full path written is the one served.
	prefix string

	// firstRoute remembers the first thing mounted here, so a Guard
	// that arrives later can name it.
	firstRoute string

	// registered is shared by the whole table, groups and Routes
	// included: each method and pattern chi has, as refuseTwice keys
	// it, and the path it was first written as.
	registered map[string]string
}

// NewRoutes mounts on router. ctxFor runs once per request, never at
// registration.
func NewRoutes(router chi.Router, ctxFor CtxFor) *Routes {
	return &Routes{
		router:     router,
		ctxFor:     ctxFor,
		recorded:   &[]Route{},
		registered: map[string]string{},
	}
}

// NewRecorder is a route table that records every registration and
// serves nothing. It registers on a private chi router of its own, so
// chi refuses what it would refuse a real table (a repeated prefix, a
// pattern it cannot parse) and an app that lists cleanly also starts.
// What chi is given are stand-ins: guards are named but never built,
// and handlers and mounted handlers are never wrapped, so an app's
// Routes function can run on it with an empty dependency. List reports
// what a real table would for the same registrations.
func NewRecorder() *Routes {
	return &Routes{
		router:     chi.NewRouter(),
		recorded:   &[]Route{},
		recording:  true,
		registered: map[string]string{},
	}
}

// Get mounts an action for GET at path, the full path from the root.
func (routes *Routes) Get(path Path, handler Handler) {
	routes.action(http.MethodGet, path, handler)
}

// Post mounts an action for POST at path, the full path from the root.
func (routes *Routes) Post(path Path, handler Handler) {
	routes.action(http.MethodPost, path, handler)
}

func (routes *Routes) action(method string, path Path, handler Handler) {
	inner := routes.within(string(path))
	identity := actionIdentity(handler)
	routes.record(Route{
		Method:  method,
		Path:    string(path),
		Handler: identity[strings.LastIndex(identity, ".")+1:],
		Action:  identity,
	})
	routes.router.Method(method, inner, routes.serve(handler))
	routes.refuseTwice(method, string(path), inner)
}

// Handle mounts an existing net/http handler. It has no function name
// worth reading, so the caller says what to call it in List.
func (routes *Routes) Handle(
	method string, path Path, handler http.Handler, name string,
) {
	method = strings.ToUpper(method)
	inner := routes.within(string(path))
	routes.record(Route{Method: method, Path: string(path), Handler: name})
	routes.router.Method(method, inner, routes.standIn(handler))
	routes.refuseTwice(method, string(path), inner)
}

// Mount hands a whole subtree, such as the API, to its own handler. It
// is one line in List because the subtree lists its own routes.
func (routes *Routes) Mount(
	prefix Path, handler http.Handler, name string,
) {
	full := withoutSlash(string(prefix))
	inner := routes.within(cmp.Or(full, "/"))
	routes.record(Route{Method: "*", Path: full + "/*", Handler: name})
	routes.router.Mount(inner, routes.standIn(handler))
}

// Guard runs middleware before every route mounted after it in this
// group. It panics if a route came first, because chi would leave that
// route unguarded without a word.
func (routes *Routes) Guard(
	name string, middleware func(http.Handler) http.Handler,
) {
	if routes.firstRoute != "" {
		panic(fmt.Sprintf("action: guard %q comes after the route %q; "+
			"put the guard first, because a route mounted before it "+
			"is not guarded", name, routes.firstRoute))
	}
	routes.guards = append(routes.guards, name)
	if routes.recording {
		middleware = func(next http.Handler) http.Handler { return next }
	}
	routes.router.Use(middleware)
}

// Group mounts routes that share guards without sharing a path prefix.
// Guards added inside it do not reach the routes outside it.
func (routes *Routes) Group(mount func(routes *Routes)) {
	routes.noteRoute("(a group)")
	routes.router.Group(func(inner chi.Router) {
		mount(routes.child(inner, routes.prefix))
	})
}

// Route mounts routes under a path prefix. Every path inside it is
// written in full, prefix included, so the path a reader sees in the
// code is the URL, with no prefixes to add up.
func (routes *Routes) Route(prefix Path, mount func(routes *Routes)) {
	full := withoutSlash(string(prefix))
	inner := routes.within(cmp.Or(full, "/"))
	routes.noteRoute(string(prefix))
	routes.router.Route(inner, func(router chi.Router) {
		mount(routes.child(router, full))
	})
}

// List is every route mounted so far, in the order it was mounted. Each
// call returns its own copy, so a caller that edits a route, guards
// included, cannot change what the next call reports.
func (routes *Routes) List() []Route {
	listing := make([]Route, len(*routes.recorded))
	for position, route := range *routes.recorded {
		route.Guards = slices.Clone(route.Guards)
		listing[position] = route
	}
	return listing
}

// withoutSlash drops one trailing slash from a prefix. chi mounts
// "/admin/" and "/admin" at the same place, so both must accept the
// same full paths inside them, and a Mount at "/api/" must list as
// "/api/*", not "/api//*".
func withoutSlash(prefix string) string {
	return strings.TrimSuffix(prefix, "/")
}

// within is the part of a full path that chi is given: chi joins a
// route's path onto the prefixes it was mounted under. A path outside
// the innermost Route panics, because chi would serve it at that
// prefix joined to it, a URL nobody wrote.
func (routes *Routes) within(path string) string {
	if routes.prefix != "" && path == routes.prefix {
		return "/"
	}
	if strings.HasPrefix(path, routes.prefix+"/") {
		return strings.TrimPrefix(path, routes.prefix)
	}
	full := routes.prefix + "/" + strings.TrimPrefix(path, "/")
	if strings.TrimPrefix(path, "/") == "" {
		full = cmp.Or(routes.prefix, "/")
	}
	if routes.prefix == "" {
		panic(fmt.Sprintf("action: the path %q does not start with /. "+
			"Write the full path from the root: %q.", path, full))
	}
	panic(fmt.Sprintf("action: the path %q is inside Route(%q), so it "+
		"must be %q or start with %q. Write the full path from the "+
		"root: %q.", path, routes.prefix, routes.prefix,
		routes.prefix+"/", full))
}

// child shares the table but not the guards: a guard added inside a
// group must not leak into the routes around it.
func (routes *Routes) child(inner chi.Router, prefix string) *Routes {
	return &Routes{
		router:     inner,
		ctxFor:     routes.ctxFor,
		prefix:     prefix,
		guards:     append([]string(nil), routes.guards...),
		recorded:   routes.recorded,
		recording:  routes.recording,
		registered: routes.registered,
	}
}

// refuseTwice panics on a method and pattern this table has already
// given chi. chi keeps the later handler without a word, so the earlier
// action would be listed, pass --check, and never run. It runs after
// chi has the route, so a pattern chi cannot parse gets chi's message.
// The key is what chi serves: the prefix joined to what chi was given,
// so a Route's "/admin" and "/admin/" are one, and parameter names are
// left out, because chi matches /tags/{id} and /tags/{name} alike.
func (routes *Routes) refuseTwice(method, path, inner string) {
	key := method + " " + Path(routes.prefix+inner).shape()
	earlier, found := routes.registered[key]
	if !found {
		routes.registered[key] = path
		return
	}
	panic(fmt.Sprintf("action: %s %q is registered twice; it was "+
		"registered before as %q. chi serves only the later one, so "+
		"the first would never run. Register each method and path once.",
		method, path, earlier))
}

func (routes *Routes) record(route Route) {
	routes.noteRoute(route.Path)
	route.Guards = append([]string(nil), routes.guards...)
	*routes.recorded = append(*routes.recorded, route)
}

func (routes *Routes) noteRoute(path string) {
	if routes.firstRoute == "" {
		routes.firstRoute = path
	}
}

// serve builds the visit for each request. The context is built here,
// per request, so registering a route needs no database.
func (routes *Routes) serve(handler Handler) http.HandlerFunc {
	if routes.recording {
		return doNothing
	}
	return func(response http.ResponseWriter, request *http.Request) {
		ctx, err := routes.contextFor(request)
		if err != nil {
			slog.Error("action: the request's context could not be built",
				"method", request.Method, "path", request.URL.Path,
				"err", err)
			http.Error(response, "Something went wrong.",
				http.StatusInternalServerError)
			return
		}
		handler(NewVisit(response, request, ctx))
	}
}

// contextFor copies what ctxFor returns before it sets the actor, as
// the generated router does. The factory may hand every request the
// same Ctx, and without the copy one person's actor would stay on it
// for the next request.
func (routes *Routes) contextFor(
	request *http.Request,
) (*rastrillo.Ctx, error) {
	built, err := routes.ctxFor(request)
	if err != nil {
		return nil, err
	}
	if built == nil {
		return nil, errors.New("action: CtxFor returned no context and " +
			"no error")
	}
	ctx := *built
	if actor, ok := rastrillo.ActorFromContext(request.Context()); ok {
		ctx.Actor = actor
	} else if ctx.Actor == (rastrillo.Actor{}) {
		if actor, ok := sessions.ActorFrom(request); ok {
			ctx.Actor = actor
		}
	}
	return &ctx, nil
}

// actionIdentity is the handler's full name, package path and receiver
// included. A method value's compiler suffix "-fm" is dropped, so it is
// "app.(*app).tagUpdate" and matches the function a check finds in the
// source; List's Handler is the part after the last dot.
func actionIdentity(handler Handler) string {
	full := runtime.FuncForPC(reflect.ValueOf(handler).Pointer()).Name()
	return strings.TrimSuffix(full, "-fm")
}

// doNothing is what a recorder gives chi for a handler: chi needs one
// to register a route, and nothing is ever served.
func doNothing(http.ResponseWriter, *http.Request) {}

// standIn is the handler, or in a recorder doNothing, so that a nil
// handler or one built from a zero app is never touched.
func (routes *Routes) standIn(handler http.Handler) http.Handler {
	if routes.recording {
		return http.HandlerFunc(doNothing)
	}
	return handler
}
