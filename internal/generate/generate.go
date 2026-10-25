// Package generate implements rastrillo's filesystem-routing generator
// (design doc §4): it walks actions/, and for each <name>.<VERB>.go file
// emits gen/router.go wiring a Go 1.22 http.ServeMux.
//
// Implementation note, a real ambiguity the design doc's own example
// glossed over: the convention puts multiple action files in the same
// directory (actions/orders/[id]/cancel.POST.go next to edit.GET.go),
// but Go compiles one package per directory — both files would collide
// on `func Handle`. This generator resolves it by never compiling
// actions/ in place: each file is parsed, its package clause rewritten
// to a name unique to its route, and the result written into its own
// directory under gen/actions/ — one generated package per route,
// normal Go imports, no AST surgery beyond the package clause. A future
// version could instead lift just the Handle function via full AST
// extraction to stay closer to "one arbitrary file, no package
// boilerplate," but that's real complexity deferred rather than rushed.
//
// The same never-compiled-in-place fact is why action files carry a
// `//go:build rastrillo_actions` constraint (BuildTag; friction log F9
// in examples/blog): without it, every `go build ./...` from the app
// root tries to compile actions/ — duplicate Handles in any
// multi-action directory, and a `[id]` directory is a malformed import
// path — so ./... always fails. The constraint makes the go tool skip
// the originals entirely; Rewrite strips it from the gen/ copies, which
// are the form that must compile.
package generate

import (
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"amadan.net/rastrillo/rastrillo/internal/generate/handfile"
)

var actionFileRe = regexp.MustCompile(`^(.+)\.(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\.go$`)

// BuildTag is the build constraint every action file carries
// (handfile.BuildTag): a normal build never satisfies it, so the go
// tool skips actions/ instead of failing on generator input.
const BuildTag = handfile.BuildTag

// Action is one discovered actions/ file.
type Action struct {
	SourcePath  string // relative to actions/, e.g. "orders/[id]/cancel.POST.go"
	Method      string // "GET", "POST", ...
	Route       string // Go 1.22 mux pattern, e.g. "POST /orders/{id}/cancel"
	PackageName string // unique generated package name
	GenDir      string // relative to gen/actions/, e.g. "orders/_id_/cancel_POST"
}

// Collision is a route claimed by more than one action file.
type Collision struct {
	Route   string
	Sources []string
}

// Discover walks actionsDir and returns every action file found, along
// with any route collisions (design doc §4: "build fails loudly on any
// collision"). It does not touch the filesystem otherwise.
func Discover(actionsDir string) ([]Action, []Collision, error) {
	var actions []Action
	err := filepath.WalkDir(actionsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(actionsDir, path)
		if err != nil {
			return err
		}
		base := filepath.Base(rel)
		if strings.HasPrefix(base, "_") {
			return nil // _middleware.go and friends: not an action
		}
		m := actionFileRe.FindStringSubmatch(base)
		if m == nil {
			return nil
		}
		name, method := m[1], m[2]

		route, err := routeFor(filepath.Dir(rel), name, method)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		actions = append(actions, Action{
			SourcePath:  filepath.ToSlash(rel),
			Method:      method,
			Route:       route,
			PackageName: packageNameFor(rel),
			GenDir:      genDirFor(filepath.Dir(rel), name, method),
		})
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	sort.Slice(actions, func(i, j int) bool { return actions[i].SourcePath < actions[j].SourcePath })

	byRoute := map[string][]string{}
	for _, a := range actions {
		byRoute[a.Route] = append(byRoute[a.Route], a.SourcePath)
	}
	var collisions []Collision
	for route, sources := range byRoute {
		if len(sources) > 1 {
			collisions = append(collisions, Collision{Route: route, Sources: sources})
		}
	}
	sort.Slice(collisions, func(i, j int) bool { return collisions[i].Route < collisions[j].Route })

	return actions, collisions, nil
}

// routeFor builds a Go 1.22 mux pattern from an action's directory, its
// filename (minus the .VERB.go suffix), and its HTTP method. "index" as
// the filename contributes no extra path segment (actions/index.GET.go
// -> "GET /{$}"), mirroring the bracket-dir convention's own precedent.
func routeFor(dir, name, method string) (string, error) {
	segs := strings.Split(filepath.ToSlash(dir), "/")
	if dir == "." {
		segs = nil
	}
	if name != "index" {
		segs = append(segs, name)
	}
	var out []string
	for _, s := range segs {
		if s == "" {
			continue
		}
		if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
			param := s[1 : len(s)-1]
			if param == "" {
				return "", fmt.Errorf("empty path param %q", s)
			}
			out = append(out, "{"+param+"}")
		} else {
			out = append(out, s)
		}
	}
	path := "/" + strings.Join(out, "/")
	// A bare "/" is a prefix pattern to Go's mux — it matches every
	// otherwise-unrouted path, so a root index action would swallow
	// all unmatched GETs (friction log F6). "{$}" anchors it to
	// exactly "/"; unmatched paths fall to the mux's own 404.
	if path == "/" {
		path = "/{$}"
	}
	return method + " " + path, nil
}

func genDirFor(dir, name, method string) string {
	segs := strings.Split(filepath.ToSlash(dir), "/")
	if dir == "." {
		segs = nil
	}
	segs = append(segs, name+"_"+method)
	safe := make([]string, len(segs))
	for i, s := range segs {
		s = strings.TrimPrefix(s, "[")
		s = strings.TrimSuffix(s, "]")
		safe[i] = sanitizeIdent(s)
	}
	return strings.Join(safe, "/")
}

func packageNameFor(relPath string) string {
	return "act_" + sanitizeIdent(strings.TrimSuffix(relPath, ".go"))
}

var identRe = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func sanitizeIdent(s string) string {
	s = identRe.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	if s == "" || (s[0] >= '0' && s[0] <= '9') {
		s = "a_" + s
	}
	return strings.ToLower(s)
}

// Rewrite copies the action file at actionsDir/a.SourcePath to
// genDir/actions/a.GenDir/<basename> with its package clause rewritten
// to a.PackageName and its build constraint stripped (handfile.Rewrite
// says why); the rest of the file travels unmodified.
func Rewrite(actionsDir, genDir string, a Action) error {
	src, err := os.ReadFile(filepath.Join(actionsDir, filepath.FromSlash(a.SourcePath)))
	if err != nil {
		return err
	}
	out, err := handfile.Rewrite(src, a.SourcePath, a.PackageName)
	if err != nil {
		return err
	}
	outPath := RewritePath(genDir, a)
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(outPath, out, 0o644)
}

// RewritePath is where Rewrite writes a's copy. Pruning needs it: an
// action copied from generated output keeps the generated header, so
// unless the copy counts as this run's, the next prune deletes it.
func RewritePath(genDir string, a Action) string {
	return filepath.Join(genDir, "actions", filepath.FromSlash(a.GenDir), filepath.Base(filepath.FromSlash(a.SourcePath)))
}

// StaleRewriteMsg is --check's line for a hand action whose copy under
// gen/ is not what Rewrite writes from today's source.
const StaleRewriteMsg = "%s is an older copy of actions/%s; run rastrillo generate"

// StaleRewrites is --check's half of Rewrite: each hand action's copy
// under gen/actions must be the one Rewrite would write now. The copy
// is what the router serves, while the parity lint reads the source
// under actions/, so a copy left stale after the edit that added the
// operation call passed the lint and ran the old handler without
// op.Check. A copy that is not there is left to the build, which fails
// on the router's import of it: --check does not ask gen/ to be whole
// (no router.go or api.go passes too), only that what it holds is
// current. The error names each copy from the app's root, as
// prune.Stale's does.
func StaleRewrites(actionsDir, genDir string, actions []Action) error {
	var lines []string
	for _, a := range actions {
		src, err := os.ReadFile(filepath.Join(actionsDir, filepath.FromSlash(a.SourcePath)))
		if err != nil {
			return err
		}
		want, err := handfile.Rewrite(src, a.SourcePath, a.PackageName)
		if err != nil {
			return err
		}
		copyPath := RewritePath(genDir, a)
		rel, err := filepath.Rel(filepath.Dir(genDir), copyPath)
		if err != nil {
			return err
		}
		got, err := os.ReadFile(copyPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !bytes.Equal(want, got) {
			lines = append(lines, fmt.Sprintf(StaleRewriteMsg, filepath.ToSlash(rel), a.SourcePath))
		}
	}
	if len(lines) == 0 {
		return nil
	}
	return errors.New(strings.Join(lines, "\n"))
}

// UntaggedActions returns the SourcePaths of the given actions whose
// files a default `go build ./...` would try (and fail) to compile.
// `rastrillo generate --check` fails on these with the exact line to
// add.
func UntaggedActions(actionsDir string, actions []Action) ([]string, error) {
	var out []string
	for _, a := range actions {
		src, err := os.ReadFile(filepath.Join(actionsDir, filepath.FromSlash(a.SourcePath)))
		if err != nil {
			return nil, err
		}
		if !SkippedByDefaultBuild(src) {
			out = append(out, a.SourcePath)
		}
	}
	return out, nil
}

// SkippedByDefaultBuild is handfile.SkippedByDefaultBuild: whether a
// default build skips src, by the go tool's own rules.
func SkippedByDefaultBuild(src []byte) bool { return handfile.SkippedByDefaultBuild(src) }

// Router renders gen/router.go: one import per action package, one
// mux.HandleFunc per route, each dispatching into a *rastrillo.Ctx built
// from the request via ctxFactory.
//
// booted is whether the app has manifest resources. Then Router also
// takes gen.Booted, so the app cannot build its mux without calling
// gen.Boot, and refuses a request whose Ctx.DB is not the database Boot
// checked: Boot(ctx, dbA) vouching for contexts on dbB would serve data
// nothing upgraded. A zero Booted written by hand compiles, so it
// panics at startup instead.
func Router(module string, actions []Action, booted bool) ([]byte, error) {
	var b strings.Builder
	b.WriteString("// Code generated by rastrillo generate. DO NOT EDIT.\n\n")
	b.WriteString("package gen\n\n")
	b.WriteString("import (\n")
	if booted {
		b.WriteString("\t\"log/slog\"\n")
	}
	b.WriteString("\t\"net/http\"\n\n")
	b.WriteString("\t\"amadan.net/rastrillo/rastrillo\"\n")
	// Only a router with a handle closure reads the session; a new
	// scaffold's has none, and an unused import would not compile.
	if booted || len(actions) > 0 {
		b.WriteString("\t\"amadan.net/rastrillo/rastrillo/sessions\"\n")
	}
	for _, a := range actions {
		b.WriteString(fmt.Sprintf("\t%s \"%s/gen/actions/%s\"\n", a.PackageName, module, a.GenDir))
	}
	b.WriteString(")\n\n")

	b.WriteString("// Router builds the app's mux. ctxFactory constructs the *rastrillo.Ctx\n")
	b.WriteString("// a request runs with; see cmd/<app>/main.go. Each request gets a copy\n")
	b.WriteString("// of what the factory returns, so a factory may hand every request the\n")
	b.WriteString("// same Ctx. An actor stamped on the request context (rastrillo.WithActor,\n")
	b.WriteString("// the API tree's doing) lands on Ctx.Actor after the factory runs.\n")
	b.WriteString("// Failing that, a factory that left Actor zero gets the signed-in person\n")
	b.WriteString("// (sessions.ActorFrom), so op.Check sees the subject the scoped store\n")
	b.WriteString("// filters by and the caller is attributed even when the factory never\n")
	b.WriteString("// thinks about actors.\n")
	if booted {
		b.WriteString("//\n")
		b.WriteString("// b is gen.Boot's result for the database the app serves. Router\n")
		b.WriteString("// panics if Boot has not run, and a request whose Ctx carries a\n")
		b.WriteString("// different database answers 500.\n")
	}
	dispatch := "\t\t%s.Handle(handle(r), w, r)\n"
	switch {
	case booted:
		b.WriteString(fmt.Sprintf(bootedRouterHead, notBooted, wrongDB))
		dispatch = "\t\tif c := handle(w, r); c != nil {\n\t\t\t%s.Handle(c, w, r)\n\t\t}\n"
	case len(actions) == 0:
		// routerHandle's closure, with no route to call it, is a
		// declared-and-not-used compile error. A new scaffold has no
		// routes, and its app.toml tells the author to run generate as
		// soon as they add a language.
		b.WriteString(routerOpen)
	default:
		b.WriteString(routerOpen + routerHandle)
	}
	for _, a := range actions {
		b.WriteString(fmt.Sprintf("\tmux.HandleFunc(%q, func(w http.ResponseWriter, r *http.Request) {\n", a.Route))
		b.WriteString(fmt.Sprintf(dispatch, a.PackageName))
		b.WriteString("\t})\n")
	}
	b.WriteString("\treturn mux\n")
	b.WriteString("}\n")

	return format.Source([]byte(b.String()))
}

const routerOpen = `func Router(ctxFactory func(*http.Request) *rastrillo.Ctx) *http.ServeMux {
	mux := http.NewServeMux()
`

const routerHandle = `	handle := func(r *http.Request) *rastrillo.Ctx {
` + routerCopy + routerActor + `		return &c
	}
`

// routerCopy is both routers' first line: the request's Ctx is a copy
// of the factory's, as api/tree.go's handle takes one, because an
// app's factory may return the same Ctx every time. Writing the actor
// into a shared one would stamp the first person on every later
// request and race two requests on the field.
const routerCopy = `		c := *ctxFactory(r)
`

// routerActor is how both routers pick the action's actor. A stamped
// actor wins: the API tree verified it. A factory's own actor comes
// next, because an app that sets one knows something the router does
// not. Only then the session: a page request's actor is the signed-in
// person, and without it op.Check would refuse a scoped operation to
// its own owner for having no subject.
const routerActor = `		if a, ok := rastrillo.ActorFromContext(r.Context()); ok {
			c.Actor = a
		} else if c.Actor == (rastrillo.Actor{}) {
			if a, ok := sessions.ActorFrom(r); ok {
				c.Actor = a
			}
		}
`

// bootedRouterHead's 500 is the text recoverPanics already sends, so a
// refused request looks like any other failure to its client; the
// reason goes to the log, where the operator who wired it will look.
const bootedRouterHead = `func Router(b Booted, ctxFactory func(*http.Request) *rastrillo.Ctx) *http.ServeMux {
	if b.db == nil {
		panic(%q)
	}
	mux := http.NewServeMux()
	handle := func(w http.ResponseWriter, r *http.Request) *rastrillo.Ctx {
` + routerCopy + routerActor + `		if c.DB == nil {
			c.DB = b.db
		}
		if c.DB != b.db {
			logger := c.Logger
			if logger == nil {
				logger = slog.Default()
			}
			logger.Error(%q)
			http.Error(w, "Something went wrong.", http.StatusInternalServerError)
			return nil
		}
		return &c
	}
`

// Appendix C.2 op.boot.not_booted and op.boot.wrong_db, as the
// generated router says them.
const (
	notBooted = "gen.Router: gen.Boot has not run for this database; call it after the schema is applied and before serving"
	wrongDB   = "gen.Router: this request's database is not the one gen.Boot checked; pass the same *sql.DB to gen.Boot and to the router's contexts"
)
