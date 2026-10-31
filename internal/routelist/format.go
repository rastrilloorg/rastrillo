package routelist

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"amadan.net/rastrillo/rastrillo/action"
)

// Format is the route table as rastrillo routes prints it: method,
// path, handler and guards, one route per line in the order they were
// registered, then the note on what guards do not decide.
func Format(routes []action.Route) string {
	var aligned strings.Builder
	columns := tabwriter.NewWriter(&aligned, 0, 4, 2, ' ', 0)
	for _, route := range routes {
		fmt.Fprintf(columns, "%s\t%s\t%s\t%s\n", route.Method, route.Path,
			handlerName(route), strings.Join(route.Guards, ", "))
	}
	columns.Flush()
	var table strings.Builder
	if len(routes) == 0 {
		fmt.Fprintln(&table, noRoutesRegistered)
	}
	// A route with no guards ends in an empty, padded column; the
	// padding is trimmed so no line ends in spaces.
	for line := range strings.Lines(aligned.String()) {
		fmt.Fprintln(&table, strings.TrimRight(line, " \n"))
	}
	fmt.Fprintf(&table, "\n%s\n", tableNote)
	return table.String()
}

// handlerName is an action's identity from its package's name on, so
// that two methods called Show read as (*Tags).Show and (*Tickets).Show,
// and a Handle or Mount's name as its caller gave it.
func handlerName(route action.Route) string {
	if route.Action == "" {
		return route.Handler
	}
	return route.Action[strings.LastIndex(route.Action, "/")+1:]
}

// FormatUnmounted is what --check says when actions are not mounted.
func FormatUnmounted(unmounted []string) string {
	var names strings.Builder
	for _, name := range unmounted {
		fmt.Fprintf(&names, "\t%s\n", name)
	}
	return fmt.Sprintf(unmountedActions, names.String())
}
