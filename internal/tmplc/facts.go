package tmplc

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// factsName is the variable holding what the function called name
// assumes of the types it reads.
func factsName(
	name string,
) string {
	return "facts_" + strings.TrimPrefix(name, "exec_")
}

// reached is the typed function called name and every typed function
// it calls, directly or not.
func (generated *file) reached(name string) []string {
	seen := map[string]bool{name: true}
	queue := []string{name}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		for _, call := range generated.bodies[name].calls {
			if !seen[call] {
				seen[call] = true
				queue = append(queue, call)
			}
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

// held is the arguments of a tmplrt.Holds of the facts of the
// functions called names, or "" if none assumes anything.
func (generated *file) held(names ...string) string {
	var args []string
	for _, name := range slices.Sorted(slices.Values(names)) {
		if len(generated.bodies[name].facts) > 0 {
			args = append(args, "&"+factsName(name))
		}
	}
	return strings.Join(args, ", ")
}

// writeFacts writes the tmplrt.Facts of the function called name.
func (generated *file) writeFacts(
	builder *strings.Builder,
	name string,
	facts assumed,
) {
	fmt.Fprintf(
		builder,
		"\n// %s is what %s assumes of the types it reads.\n"+
			"var %s = %s.Facts{\n",
		factsName(name),
		name,
		factsName(name),
		generated.rt,
	)
	if facts.dot != "" {
		fmt.Fprintf(builder, "\tDot: %s.TypeFor[%s](),\n",
			generated.reflect, facts.dot)
	}
	if len(facts.funcs) > 0 {
		fmt.Fprintf(builder, "\tFuncs: map[string]%s.Type{\n",
			generated.reflect)
		for _, field := range slices.Sorted(maps.Keys(facts.funcs)) {
			fmt.Fprintf(builder, "\t\t%q: %s.TypeFor[%s](),\n",
				field, generated.reflect, facts.funcs[field])
		}
		builder.WriteString("\t},\n")
	}
	fmt.Fprintf(builder, "\tFacts: []%s.Fact{\n", generated.rt)
	for _, fc := range facts.facts {
		fmt.Fprintf(builder, "\t\t{%s},\n", factFields(fc))
	}
	builder.WriteString("\t},\n}\n")
}

// factFields is the fields of fc's tmplrt.Fact literal.
func factFields(fc fact) string {
	var fields []string
	if fc.path.fn != "" {
		fields = append(fields, "Func: "+strconv.Quote(fc.path.fn))
	}
	if len(fc.path.steps) > 0 {
		fields = append(fields, "Path: "+stringSlice(fc.path.steps))
	}
	fields = append(fields, "Type: "+strconv.Quote(fc.desc))
	return strings.Join(fields, ", ")
}
