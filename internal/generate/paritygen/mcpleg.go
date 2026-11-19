package paritygen

import (
	"fmt"
	"strings"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/generate/shape"
	"amadan.net/rastrillo/rastrillo/internal/wirename"
)

// The MCP half of a Run file: the API door's scenario again through the
// MCP endpoint, which must end every step as the API did, and the list
// limits through both doors. The tool names are the operation ids with
// the dot made an underscore, as mcp.ToolName writes them; the generator
// does not import mcp, which would pull the SDK into the CLI.

const legParameters = "t *testing.T, f Fixture, values map[string]string"

// legSetup opens a leg of the scenario on a fresh app.
const legSetup = "\th, db := f.New(t)\n" +
	"\tf.Prepare(t, db, \"a\")\n" +
	"\ttoken := f.Token(t, db, \"a\")\n"

// legHead is what Run does with the legs: each pair is compared, and the
// first pair is the page door against the API door.
const legHead = "\tapi := %[1]sAPI(t, f, values)\n" +
	"\tcompare(t, %[1]sPages(t, f, values), api)\n" +
	"\tcompareDoors(t, \"API\", \"MCP\", api, %[1]sMCP(t, f, values))\n" +
	"\tcompareDoors(t, \"API\", \"MCP\",\n" +
	"\t\t%[1]sAPILimits(t, f, values), %[1]sMCPLimits(t, f, values))\n}\n\n"

// runCompares ends a Run function with its comparisons.
func runCompares(function string) string {
	return fmt.Sprintf(legHead, function)
}

// stepLine is one appended step, its kind and row as Go expressions.
func stepLine(name, kind, row string) string {
	return fmt.Sprintf(
		"\tsteps = append(steps, step{%q, %s, %s})\n", name, kind, row)
}

// toolLine is one tool call, assigning as given ("k, _ =").
func toolLine(assign, token, tool, arguments string) string {
	return fmt.Sprintf(
		"\t%s tool(t, h, %s, %q, %s)\n", assign, token, tool, arguments)
}

// resourceTools is one resource's names in the generated Go.
type resourceTools struct {
	function string // "ticketTypes"
	resource string // "ticket_types"
}

func toolsOf(resource rastrillo.Resource) resourceTools {
	return resourceTools{
		function: shape.LowerFirst(shape.PascalCase(resource.Name)),
		resource: resource.Name,
	}
}

// tool is the tool for verb, such as "ticket_types_get".
func (tools resourceTools) tool(verb string) string {
	return tools.resource + "_" + verb
}

// mcpDoors is everything after the API door in a Run file: the MCP leg,
// the create body both doors share, and the two list-limit legs.
func mcpDoors(resource rastrillo.Resource) string {
	tools := toolsOf(resource)
	api := "/api/v1/" + resource.Name
	body := tools.function + "Body(t, values)"
	apiCreate := fmt.Sprintf("call(t, h, token, \"POST\", %q, %s)", api, body)
	apiList := fmt.Sprintf(
		"call(t, h, token, \"GET\", fmt.Sprintf(%q, limit), nil)",
		api+"?limit=%d")
	mcpList := toolCall(tools.tool("list"), "map[string]any{\"limit\": limit}")
	return mcpLeg(resource) + createBody(resource) +
		limitLeg(tools, "API", apiCreate, apiList) +
		limitLeg(tools, "MCP", toolCall(tools.tool("create"), body), mcpList)
}

// apiCreateLine is the API door's create, with the body both doors share.
func apiCreateLine(path, function string) string {
	return fmt.Sprintf("\tk, body := call(t, h, token, \"POST\", "+
		"%q, %sBody(t, values))\n", path, function)
}

// toolCall is a tool call by the person in token.
func toolCall(tool, arguments string) string {
	return fmt.Sprintf("tool(t, h, token, %q, %s)", tool, arguments)
}

// createBody is a valid create as both doors take it, so a door cannot
// be tested with a body the other would not accept.
func createBody(resource rastrillo.Resource) string {
	tools := toolsOf(resource)
	var text strings.Builder
	fmt.Fprintf(&text, "\n// %sBody is a valid create as the API and MCP "+
		"both take it.\n", tools.function)
	fmt.Fprintf(&text, "func %sBody(t *testing.T, "+
		"values map[string]string) map[string]any {\n", tools.function)
	text.WriteString("\tt.Helper()\n\treturn map[string]any{\n")
	for _, field := range shape.FormFields(resource) {
		wire := wirename.Name(field.Name)
		if field.Kind == rastrillo.Money {
			fmt.Fprintf(&text, "\t\t%q: money(t, values[%q], %q),\n",
				wire, field.Name, resource.Currency)
			continue
		}
		fmt.Fprintf(&text, "\t\t%q: values[%q],\n", wire, field.Name)
	}
	text.WriteString("\t}\n}\n")
	return text.String()
}

// limitLeg is the scenario's list-limit steps through one door: a
// fresh app with one row, listed with limit 0 and 201, which the doors
// must treat alike. create and list are calls returning the kind and
// the decoded body.
func limitLeg(
	tools resourceTools, door, create, list string,
) string {
	var text strings.Builder
	fmt.Fprintf(&text, "\nfunc %s%sLimits(%s) []step {\n",
		tools.function, door, legParameters)
	text.WriteString(legSetup)
	fmt.Fprintf(&text, "\tif k, _ := %s; k != \"ok\" {\n", create)
	fmt.Fprintf(&text, "\t\tt.Fatalf(\"the %s door answered %%s to "+
		"a valid create\", k)\n\t}\n", door)
	text.WriteString("\tvar steps []step\n")
	text.WriteString("\tfor _, limit := range []int{0, 201} {\n")
	fmt.Fprintf(&text, "\t\tk, body := %s\n", list)
	text.WriteString("\t\tsteps = append(steps, step{" +
		"fmt.Sprintf(\"list with limit %d\", limit), k, ")
	fmt.Fprintf(&text, "items(body, %q)})\n\t}\n", tools.resource)
	text.WriteString("\treturn steps\n}\n")
	return text.String()
}

// mcpLeg mirrors the API door's steps one for one through the tools.
func mcpLeg(resource rastrillo.Resource) string {
	tools := toolsOf(resource)
	var text strings.Builder
	fmt.Fprintf(&text, "\nfunc %sMCP(%s) []step {\n",
		tools.function, legParameters)
	text.WriteString(legSetup + "\tvar steps []step\n\n")
	text.WriteString(toolLine("k, body :=", "token", tools.tool("create"),
		tools.function+"Body(t, values)"))
	text.WriteString("\tid := fmt.Sprint(body[\"id\"])\n")
	text.WriteString(stepLine("create", "k", `""`))
	text.WriteString(stepLine("row after create", `"ok"`, tools.rowLine()))
	text.WriteString("\n")
	text.WriteString(tools.invalidCreate(resource))
	text.WriteString(tools.update(resource))
	if shape.Scoped(resource) {
		text.WriteString(tools.hidden())
	}
	text.WriteString(tools.remove())
	text.WriteString("\treturn steps\n}\n")
	return text.String()
}

// rowLine reads the row through the get tool.
func (tools resourceTools) rowLine() string {
	return fmt.Sprintf("toolRow(t, h, token, %q, id)", tools.tool("get"))
}

// invalidCreate sends no fields and expects the API's refusal.
func (tools resourceTools) invalidCreate(resource rastrillo.Resource) string {
	names := required(resource)
	if len(names) == 0 {
		return ""
	}
	quoted := make([]string, len(names))
	for index, name := range names {
		quoted[index] = fmt.Sprintf("%q", name)
	}
	return toolLine("k, body =", "token", tools.tool("create"),
		"map[string]any{}") +
		fmt.Sprintf("\trefused(t, body, %s)\n", strings.Join(quoted, ", ")) +
		stepLine("invalid create", "k", `""`) + "\n"
}

// update changes the one field the API door's PATCH changes.
func (tools resourceTools) update(resource rastrillo.Resource) string {
	edit, ok := editOf(resource)
	if !ok {
		return ""
	}
	changes := fmt.Sprintf("withID(id, map[string]any{%q: %q})",
		wirename.Name(edit.field.Name), changed)
	return toolLine("k, _ =", "token", tools.tool("update"), changes) +
		stepLine("update", "k", `""`) +
		stepLine("row after update", `"ok"`, tools.rowLine()) + "\n"
}

// hidden is another person asking for a row that is not theirs.
func (tools resourceTools) hidden() string {
	return "\tother := f.Token(t, db, \"b\")\n" +
		toolLine("k, _ =", "other", tools.tool("get"), "withID(id, nil)") +
		"\thidden(t, \"MCP\", k)\n" +
		stepLine("another person's row", "k", `""`) + "\n"
}

// remove deletes the row, then asks for it and for one never made.
func (tools resourceTools) remove() string {
	get := tools.tool("get")
	return toolLine("k, _ =", "token", tools.tool("delete"),
		"withID(id, nil)") + stepLine("delete", "k", `""`) +
		toolLine("k, _ =", "token", get, "withID(id, nil)") +
		stepLine("row after delete", "k", `""`) +
		toolLine("k, _ =", "token", get, `withID("999999", nil)`) +
		stepLine("missing row", "k", `""`)
}
