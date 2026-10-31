package scaffoldcheck

import (
	"slices"
	"strings"
	"testing"
)

// listedRoute is one line of rastrillo routes' table.
type listedRoute struct {
	method, path, handler string
	guards                []string
}

// twoPageShells are the shells whose phone layout is an index and the
// page it opens, so rastrillo new gives them a second page.
var twoPageShells = map[string]bool{"sidebar": true, "console": true}

// The route table shows everything a new app serves, on every shell: the
// static files and the JSON API mounted with no guard, because a bearer
// token is the API's only credential, and every page behind the CSRF
// check and the page middleware.
func TestRoutesListsTheMountsAndGuardsEveryPage(t *testing.T) {
	t.Parallel()
	for _, variant := range everyShell() {
		t.Run(variant.name, func(t *testing.T) {
			t.Parallel()
			directory := buildableApp(t, variant.name, variant.flags...)
			output := run(t, directory, rastrilloBinary, "routes", "--check")
			routes := readRoutes(output)
			for _, mount := range []string{"/static/*", "/api/v1/*"} {
				if !hasUnguarded(routes, mount) {
					t.Errorf("no unguarded mount of %s:\n%s", mount, output)
				}
			}
			checkPages(t, variant, routes)
			checkOverview(t, variant, routes)
		})
	}
}

// checkPages fails unless the table lists pages and each is behind
// csrf and pages, and, in an app that signs people in, signedIn.
func checkPages(t *testing.T, variant variant, routes []listedRoute) {
	t.Helper()
	want := []string{"csrf", "pages"}
	if slices.Contains(variant.flags, "--auth=magic") {
		want = append(want, "signedIn")
	}
	pages := 0
	for _, route := range routes {
		if route.method == "*" {
			continue
		}
		pages++
		for _, guard := range want {
			if !slices.Contains(route.guards, guard) {
				t.Errorf("%s %s is not guarded by %s: %v",
					route.method, route.path, guard, route.guards)
			}
		}
	}
	if pages == 0 {
		t.Errorf("the table lists no pages: %v", routes)
	}
}

// checkOverview fails unless a two-page shell's second page is listed
// with the home page's guards, so it is as public as the home page, and
// the other shells have no such page.
func checkOverview(t *testing.T, variant variant, routes []listedRoute) {
	t.Helper()
	home, _ := findRoute(routes, "GET", "/")
	overview, listed := findRoute(routes, "GET", "/overview")
	switch twoPage := twoPageShells[variant.shellOf()]; {
	case twoPage && !listed:
		t.Errorf("no GET /overview in the table: %v", routes)
	case twoPage && !slices.Equal(overview.guards, home.guards):
		t.Errorf("GET /overview is guarded by %v, the home page by %v",
			overview.guards, home.guards)
	case !twoPage && listed:
		t.Errorf("a one-page shell lists GET /overview: %v", routes)
	}
}

func findRoute(routes []listedRoute, method, path string) (listedRoute, bool) {
	for _, route := range routes {
		if route.method == method && route.path == path {
			return route, true
		}
	}
	return listedRoute{}, false
}

func hasUnguarded(routes []listedRoute, path string) bool {
	for _, route := range routes {
		if route.method == "*" && route.path == path {
			return len(route.guards) == 0
		}
	}
	return false
}

// readRoutes reads the table, which ends at the first blank line.
func readRoutes(output string) []listedRoute {
	var routes []listedRoute
	for line := range strings.Lines(output) {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			break
		}
		if len(fields) < 3 {
			continue
		}
		route := listedRoute{
			method: fields[0], path: fields[1], handler: fields[2],
		}
		for _, guard := range fields[3:] {
			route.guards = append(route.guards, strings.TrimSuffix(guard, ","))
		}
		routes = append(routes, route)
	}
	return routes
}
