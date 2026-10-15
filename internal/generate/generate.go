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
	"fmt"
	"go/ast"
	"go/build"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var actionFileRe = regexp.MustCompile(`^(.+)\.(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\.go$`)

// BuildTag is the build constraint every action file carries
// (`//go:build rastrillo_actions`). It is never satisfied by a normal
// build, so `go build ./...`, `go vet ./...` and `go test ./...` skip
// actions/ instead of failing on generator input.
const BuildTag = "rastrillo_actions"

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

// Rewrite parses the action file at actionsDir/a.SourcePath, rewrites
// only its package clause to a.PackageName, and writes the result to
// genDir/actions/a.GenDir/<basename>. The rest of the file — imports,
// the Handle function, any helpers — travels unmodified, with one
// exception: any build-constraint comment (the BuildTag convention, or
// a legacy `// +build` line) is stripped, because the copy is the form
// that must compile and a surviving constraint would exclude it too.
func Rewrite(actionsDir, genDir string, a Action) error {
	src := filepath.Join(actionsDir, filepath.FromSlash(a.SourcePath))
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, src, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parse %s: %w", a.SourcePath, err)
	}
	if file.Name == nil {
		return fmt.Errorf("%s: missing package clause", a.SourcePath)
	}
	file.Name = ast.NewIdent(a.PackageName)

	if file.Doc != nil && isConstraintGroup(file.Doc) {
		file.Doc = nil
	}
	var kept []*ast.CommentGroup
	for _, g := range file.Comments {
		if g.End() < file.Package && isConstraintGroup(g) {
			continue
		}
		kept = append(kept, g)
	}
	file.Comments = kept

	var buf bytes.Buffer
	if err := format.Node(&buf, fset, file); err != nil {
		return fmt.Errorf("format %s: %w", a.SourcePath, err)
	}

	outPath := RewritePath(genDir, a)
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(outPath, buf.Bytes(), 0o644)
}

// RewritePath is where Rewrite writes a's copy. Pruning needs it: an
// action copied from generated output keeps the generated header, so
// unless the copy counts as this run's, the next prune deletes it.
func RewritePath(genDir string, a Action) string {
	return filepath.Join(genDir, "actions", filepath.FromSlash(a.GenDir), filepath.Base(filepath.FromSlash(a.SourcePath)))
}

// isConstraintGroup reports whether any line of g is a build-constraint
// comment: `//go:build ...`, or the legacy `// +build ...` spelling that
// gofmt keeps alongside it in pre-1.17 files.
func isConstraintGroup(g *ast.CommentGroup) bool {
	for _, c := range g.List {
		text := strings.TrimSpace(c.Text)
		if strings.HasPrefix(text, "//go:build ") || strings.HasPrefix(text, "// +build ") {
			return true
		}
	}
	return false
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

// SkippedByDefaultBuild reports whether a default build skips src —
// which is exactly what lets `go build ./...` pass over generator
// input. The scaffolded spelling is `//go:build rastrillo_actions`
// (BuildTag), but the check evaluates the file with the go tool's own
// rules (go/build's MatchFile: constraints, GOOS/GOARCH and release
// tags included) rather than grepping for the tag — a file with
// `//go:build !rastrillo_actions` names the tag yet compiles in every
// default build, and any other never-satisfied constraint earns the
// same skip the scaffolded one does.
func SkippedByDefaultBuild(src []byte) bool {
	ctxt := build.Default
	ctxt.OpenFile = func(string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(src)), nil
	}
	match, err := ctxt.MatchFile(".", "action.go")
	if err != nil {
		// Unparseable header: let the compiler complain, not --check.
		return false
	}
	return !match
}

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
	for _, a := range actions {
		b.WriteString(fmt.Sprintf("\t%s \"%s/gen/actions/%s\"\n", a.PackageName, module, a.GenDir))
	}
	b.WriteString(")\n\n")

	b.WriteString("// Router builds the app's mux. ctxFactory constructs a fresh\n")
	b.WriteString("// *rastrillo.Ctx per request; see cmd/<app>/main.go. An actor stamped\n")
	b.WriteString("// on the request context (rastrillo.WithActor — the tools dispatcher's\n")
	b.WriteString("// doing) lands on Ctx.Actor after the factory runs, so agent calls are\n")
	b.WriteString("// attributed even when the app factory never thinks about actors.\n")
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
		c := ctxFactory(r)
		if a, ok := rastrillo.ActorFromContext(r.Context()); ok {
			c.Actor = a
		}
		return c
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
		c := ctxFactory(r)
		if a, ok := rastrillo.ActorFromContext(r.Context()); ok {
			c.Actor = a
		}
		if c.DB == nil {
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
		return c
	}
`

// Appendix C.2 op.boot.not_booted and op.boot.wrong_db, as the
// generated router says them.
const (
	notBooted = "gen.Router: gen.Boot has not run for this database; call it after the schema is applied and before serving"
	wrongDB   = "gen.Router: this request's database is not the one gen.Boot checked; pass the same *sql.DB to gen.Boot and to the router's contexts"
)
