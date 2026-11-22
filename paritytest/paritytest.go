// Package paritytest checks that an app's pages, its API and its MCP
// endpoint offer the same named operations, so an agent can do what a
// person can, and no door serves what the app never declared.
package paritytest

import (
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/go-chi/chi/v5"

	"amadan.net/rastrillo/rastrillo/op"
	"amadan.net/rastrillo/rastrillo/route"
)

// Surface is everything Routes compares: the page table, the declared
// operations, both doors as their constructors returned them, and the
// whole app with both mounted.
type Surface struct {
	Routes     []route.Route   // Table.List()
	Operations []op.Descriptor // every operation the app declares
	// Generated is gen.Operations(). Checking those is gen/paritytest's
	// job, so they are exempt from the page check here.
	Generated []op.Descriptor
	API       http.Handler // exactly what api.New returned
	MCP       http.Handler // exactly what mcp.New returned
	Handler   http.Handler // the whole app, both mounted
	// Token is an agent's write token for a person whom Authorize
	// allows everything.
	Token string
	// Set Router to the app's chi router so Routes can find routes
	// registered outside the table. Leave it nil for a ServeMux, which
	// Routes can't walk.
	Router chi.Routes
}

// Routes reports, with t.Errorf, every way surface's page table and
// doors differ from its declared operations, and never stops early:
//   - each route names declared operations or a page-only reason, and
//     each declared operation has a page;
//   - on chi, every route the router serves is in the table;
//   - the API and MCP each serve every declared operation as declared,
//     and nothing undeclared;
//   - the API answers each declared operation's route at /api/v1/;
//   - MCP lists every operation an agent may call, and no human-only
//     one, which both doors refuse.
func Routes(t testing.TB, surface Surface) {
	t.Helper()
	declared := uniqueOperations(t, surface.Operations)
	known := map[string]bool{}
	for _, descriptor := range slices.Concat(declared, surface.Generated) {
		known[descriptor.ID] = true
	}
	checkPages(t, surface.Routes, declared, known)
	checkRecorded(t, surface)
	checkAPI(t, surface.API, declared, known)
	checkMCP(t, surface.MCP, declared, known)
	checkMounted(t, surface, declared)
	checkListed(t, surface, declared)
	checkHumanOnly(t, surface, declared)
}

// uniqueOperations is operations without repeats, each reported once.
func uniqueOperations(
	t testing.TB, operations []op.Descriptor,
) []op.Descriptor {
	t.Helper()
	seen := map[string]int{}
	var unique []op.Descriptor
	for _, descriptor := range operations {
		seen[descriptor.ID]++
		switch seen[descriptor.ID] {
		case 1:
			unique = append(unique, descriptor)
		case 2:
			t.Errorf(msgListedTwice, descriptor.ID)
		}
	}
	return unique
}

// checkPages holds the page table to the declarations: every route is
// marked, names only known operations, and every declared operation
// has a route. Generated operations have their pages checked by
// gen/paritytest, so only their names count here.
func checkPages(
	t testing.TB, routes []route.Route, declared []op.Descriptor,
	known map[string]bool,
) {
	t.Helper()
	paged := map[string]bool{}
	for _, each := range routes {
		if len(each.Operations) == 0 && each.PageOnly == "" {
			t.Errorf(msgUnmarked, each.Method, each.Path)
		}
		for _, id := range each.Operations {
			paged[id] = true
			if !known[id] {
				t.Errorf(msgUndeclared, each.Method, each.Path, id)
			}
		}
	}
	for _, descriptor := range declared {
		if !paged[descriptor.ID] {
			t.Errorf(msgNoPage, descriptor.ID)
		}
	}
}

// doorMounts are the paths chi's walk gives the two doors' mounts: the
// API tree at /api/v1/ and MCP at /api/v1/mcp. Both are mounted on the
// router directly, and their own checks cover them. Only these paths
// are skipped, so a route registered directly anywhere else under
// /api/v1/, such as a webhook, is still found.
var doorMounts = map[string]bool{
	"/api/v1/*":     true,
	"/api/v1/mcp":   true,
	"/api/v1/mcp/":  true,
	"/api/v1/mcp/*": true,
}

// checkRecorded walks a chi router for routes registered on it
// directly, which the table never saw and so never marked. The walk
// visits chi's map of methods in no fixed order, so the failures are
// sorted to read the same on every run.
func checkRecorded(t testing.TB, surface Surface) {
	t.Helper()
	if surface.Router == nil {
		return
	}
	recorded := map[string]bool{}
	for _, each := range surface.Routes {
		recorded[each.Method+" "+each.Path] = true
	}
	var failures []string
	err := chi.Walk(surface.Router, func(method, path string,
		_ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !doorMounts[path] && !recorded[method+" "+path] {
			failures = append(failures,
				fmt.Sprintf(msgUnrecorded, method, path))
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}
	slices.Sort(failures)
	for _, failure := range failures {
		t.Error(failure)
	}
}
