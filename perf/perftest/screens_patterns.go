package perftest

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
)

// routeSourceProblem says what is wrong when ScreenConfig has no source
// of routes, or two. Without exactly one, every record would also be
// reported stale, burying the one problem that matters.
func routeSourceProblem(cfg ScreenConfig) string {
	switch {
	case cfg.Routes == nil && cfg.Patterns == nil:
		return "ScreenConfig: set Routes (a chi router) or Patterns " +
			"(a stdlib mux's GET routes)"
	case cfg.Routes != nil && cfg.Patterns != nil:
		return "ScreenConfig: set Routes or Patterns, not both"
	}
	return ""
}

// listRoutes is every GET route, sorted, from whichever source cfg has.
// walked holds the routes as served, and mounts the stdlib subtree
// patterns, which like chi's "/*" have screens no table lists.
func listRoutes(cfg ScreenConfig) (
	routes []string, walked, mounts map[string]bool, err error,
) {
	walked, mounts = map[string]bool{}, map[string]bool{}
	add := func(route string) {
		if !walked[route] {
			walked[route] = true
			routes = append(routes, route)
		}
	}
	if cfg.Patterns != nil {
		for _, pattern := range cfg.Patterns {
			if isSubtree(pattern) {
				mounts[pattern] = true
			}
			// "/{$}" matches only the root, but requested literally it
			// is a 404 (the wildcard is not a path), so the screen is "/".
			add(strings.TrimSuffix(pattern, "{$}"))
		}
	} else {
		err = chi.Walk(cfg.Routes, func(method, route string,
			_ http.Handler, _ ...func(http.Handler) http.Handler) error {
			if method == http.MethodGet {
				add(route)
			}
			return nil
		})
	}
	sort.Strings(routes)
	return routes, walked, mounts, err
}

// A ServeMux hands a whole subtree to "/", "/admin/" and a terminal
// wildcard such as "/files/{rest...}", so the handler behind one serves
// screens no table lists. "/{$}" and "/admin/{$}" are one page each.
func isSubtree(pattern string) bool {
	return strings.HasSuffix(pattern, "/") ||
		strings.HasSuffix(pattern, "...}")
}

// unseenMount is the problem for a mount with no Opaque group and no
// skip record. tableMount says whether the route table named it, or chi.
func unseenMount(screen string, tableMount bool) string {
	who := "chi"
	if tableMount {
		who = "the route table"
	}
	return fmt.Sprintf("%s is a mounted handler %s cannot see inside: "+
		"list its screens in an Opaque group, or record "+
		"`perf %s skip <reason>`", screen, who, screen)
}
