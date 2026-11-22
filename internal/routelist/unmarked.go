package routelist

import (
	"context"

	"amadan.net/rastrillo/rastrillo/action"
)

// Check is what rastrillo routes --check reports once the table is
// printed: the actions no route mounts, or else the routes with no
// mark, though only in an app that calls op.Define, so an app that has
// not adopted named operations passes as it did before the rule. It is
// "" when there is nothing to fix.
func Check(
	ctx context.Context, target Target, routes []action.Route,
) (string, error) {
	unmounted, err := Unmounted(ctx, target, routes)
	if err != nil {
		return "", err
	}
	if len(unmounted) > 0 {
		return FormatUnmounted(unmounted), nil
	}
	declares, err := DeclaresOperations(ctx, target)
	if err != nil {
		return "", err
	}
	if unmarked := Unmarked(routes); declares && len(unmarked) > 0 {
		return FormatUnmarked(unmarked), nil
	}
	return "", nil
}

// Unmarked is every route that names no operation and gives no
// page-only reason. A Mount is left out: it hands a subtree, such as
// the API, to a handler of its own, and is not a page.
func Unmarked(routes []action.Route) []action.Route {
	var unmarked []action.Route
	for _, route := range routes {
		if route.Method != "*" && len(route.Operations) == 0 &&
			route.PageOnly == "" {
			unmarked = append(unmarked, route)
		}
	}
	return unmarked
}
