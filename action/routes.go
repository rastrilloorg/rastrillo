package action

import (
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
	Guards  []string
}

// Routes mounts handlers on a chi router and records each one. Nothing
// is built or called while routes are registered: an app can register
// every route before its stores exist, and a test can read List without
// a database.
type Routes struct {
	router   chi.Router
	ctxFor   CtxFor
	prefix   string
	guards   []string
	recorded *[]Route

	// firstRoute remembers the first thing mounted here, so a Guard
	// that arrives later can name it.
	firstRoute string
}

// NewRoutes mounts on router. ctxFor runs once per request, never at
// registration.
func NewRoutes(router chi.Router, ctxFor CtxFor) *Routes {
	return &Routes{router: router, ctxFor: ctxFor, recorded: &[]Route{}}
}

// Get mounts an action for GET.
func (routes *Routes) Get(path string, handler Handler) {
	routes.record(http.MethodGet, path, handlerName(handler))
	routes.router.Get(path, routes.serve(handler))
}

// Post mounts an action for POST.
func (routes *Routes) Post(path string, handler Handler) {
	routes.record(http.MethodPost, path, handlerName(handler))
	routes.router.Post(path, routes.serve(handler))
}

// Handle mounts an existing net/http handler. It has no function name
// worth reading, so the caller says what to call it in List.
func (routes *Routes) Handle(
	method, path string, handler http.Handler, name string,
) {
	method = strings.ToUpper(method)
	routes.record(method, path, name)
	routes.router.Method(method, path, handler)
}

// Mount hands a whole subtree, such as the API, to its own handler. It
// is one line in List because the subtree lists its own routes.
func (routes *Routes) Mount(
	prefix string, handler http.Handler, name string,
) {
	routes.record("*", withoutSlash(prefix)+"/*", name)
	routes.router.Mount(prefix, handler)
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

// Route mounts routes under a path prefix. List reports each path in
// full, so a reader never has to add the prefixes up.
func (routes *Routes) Route(prefix string, mount func(routes *Routes)) {
	routes.noteRoute(prefix)
	routes.router.Route(prefix, func(inner chi.Router) {
		mount(routes.child(inner, routes.prefix+withoutSlash(prefix)))
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
// "/admin/" and "/admin" at the same place, and joins a route's path
// onto it with its own leading slash, so keeping the prefix's slash
// would record "/admin//x" for a route chi serves at "/admin/x".
func withoutSlash(prefix string) string {
	return strings.TrimSuffix(prefix, "/")
}

// child shares the table but not the guards: a guard added inside a
// group must not leak into the routes around it.
func (routes *Routes) child(inner chi.Router, prefix string) *Routes {
	return &Routes{
		router:   inner,
		ctxFor:   routes.ctxFor,
		prefix:   prefix,
		guards:   append([]string(nil), routes.guards...),
		recorded: routes.recorded,
	}
}

func (routes *Routes) record(method, path, name string) {
	routes.noteRoute(path)
	*routes.recorded = append(*routes.recorded, Route{
		Method:  method,
		Path:    routes.prefix + path,
		Handler: name,
		Guards:  append([]string(nil), routes.guards...),
	})
}

func (routes *Routes) noteRoute(path string) {
	if routes.firstRoute == "" {
		routes.firstRoute = path
	}
}

// serve builds the visit for each request. The context is built here,
// per request, so registering a route needs no database.
func (routes *Routes) serve(handler Handler) http.HandlerFunc {
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

// handlerName is the function's own name: a method value's compiler
// suffix "-fm" and the package path are dropped, so List says
// "tagUpdate" and not "app.(*app).tagUpdate-fm".
func handlerName(handler Handler) string {
	full := runtime.FuncForPC(reflect.ValueOf(handler).Pointer()).Name()
	full = strings.TrimSuffix(full, "-fm")
	return full[strings.LastIndex(full, ".")+1:]
}
