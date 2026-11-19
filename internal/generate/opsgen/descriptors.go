package opsgen

import (
	"fmt"
	"strings"
)

// invokeHead opens every descriptor's Invoke, whose signature is
// op.Descriptor's own.
const invokeHead = "Invoke: func(ctx context.Context, rc *rastrillo.Ctx, " +
	"args json.RawMessage) (any, error) {\n"

// descriptor is the part of one op.Descriptor literal that differs
// between the five operations. The flags are not among them: they
// come from Effects, which the API contract reads too.
type descriptor struct {
	operation   string // ListOp
	action      string // list
	description string
	confirm     string // empty when the operation asks nothing first
	input       string // the schema constant Input reads
	result      string
	body        string // Invoke's statements
}

// render writes the literal. Destructive and Idempotent come after
// Confirm, in op.Descriptor's own field order.
func (parts descriptor) render(res resource) string {
	destructive, idempotent := Effects(parts.action, res.replaced[parts.action])
	var out strings.Builder
	fmt.Fprintf(&out, "{\nOperation: %s,\nExposed: %t,\nDescription: %q,\n",
		parts.operation, res.Exposed(), parts.description)
	if parts.confirm != "" {
		fmt.Fprintf(&out, "Confirm: %q,\n", parts.confirm)
	}
	fmt.Fprintf(&out, "Destructive: %t,\nIdempotent: %t,\n",
		destructive, idempotent)
	fmt.Fprintf(&out, "Input: json.RawMessage(%s),\n", parts.input)
	fmt.Fprintf(&out, "Result: json.RawMessage(%s),\n", parts.result)
	out.WriteString(invokeHead + parts.body + "},\n},\n")
	return out.String()
}

// descriptors renders Descriptors. Each Invoke decodes with
// op.DecodeInput, the API door's rules, so the two cannot drift.
func descriptors(res resource) string {
	var out strings.Builder
	out.WriteString(comment(fmt.Sprintf("Descriptors names every %s "+
		"operation, for the API tree now and MCP later. Each Invoke "+
		"decodes its arguments with the API door's rules and calls "+
		"the operation.", res.Name)))
	out.WriteString("func Descriptors() []op.Descriptor {\n")
	out.WriteString("return []op.Descriptor{\n")
	for _, parts := range []descriptor{
		listDescriptor(res), getDescriptor(res), createDescriptor(res),
		updateDescriptor(res), deleteDescriptor(res),
	} {
		out.WriteString(parts.render(res))
	}
	out.WriteString("}\n}\n")
	return out.String()
}

// decodeInto is the start of an Invoke: the arguments decoded into a
// value of the named type.
func decodeInto(typeName string) string {
	return "var a " + typeName + "\n" +
		"if err := op.DecodeInput(args, &a); err != nil {\n" +
		"return nil, err\n}\n"
}

func listDescriptor(res resource) descriptor {
	description := fmt.Sprintf("List %s, newest first.", res.HumanMany)
	if res.Scoped {
		description = fmt.Sprintf("List your %s, newest first.", res.HumanMany)
	}
	var inputs []string
	if res.SearchBind {
		inputs = append(inputs, "Search: a.Q")
	}
	for _, filter := range res.Filters {
		inputs = append(inputs, fmt.Sprintf("%[1]s: a.%[1]s", filter.Param))
	}
	inputs = append(inputs, "After: a.After", "Limit: a.Limit")
	return descriptor{
		operation: "ListOp", action: "list", description: description,
		input: "listArgsSchema", result: "listResultSchema",
		body: decodeInto("ListArgs") +
			"p, err := List(ctx, rc, ListInput{" +
			strings.Join(inputs, ", ") + "})\n" +
			"if err != nil {\nreturn nil, err\n}\n" +
			"return p.Result(), nil\n",
	}
}

func getDescriptor(res resource) descriptor {
	return descriptor{
		operation: "GetOp", action: "get",
		description: fmt.Sprintf("Get one %s by id.", res.HumanOne),
		input:       "idArgsSchema", result: "resultSchema",
		body: decodeInto("GetArgs") + "return Get(ctx, rc, a.ID)\n",
	}
}

func createDescriptor(res resource) descriptor {
	return descriptor{
		operation: "CreateOp", action: "create",
		description: fmt.Sprintf("Create %s %s.", res.Article, res.HumanOne),
		input:       "createArgsSchema", result: "resultSchema",
		body: decodeInto("CreateInput") + "return Create(ctx, rc, a)\n",
	}
}

func updateDescriptor(res resource) descriptor {
	return descriptor{
		operation: "UpdateOp", action: "update",
		description: fmt.Sprintf(
			"Change %s %s. Send only the fields to change.",
			res.Article, res.HumanOne),
		confirm: fmt.Sprintf("Change %s {id}?", res.HumanOne),
		input:   "updateArgsSchema", result: "resultSchema",
		body: decodeInto("UpdateArgs") +
			"return Update(ctx, rc, a.ID, a.UpdateInput)\n",
	}
}

func deleteDescriptor(res resource) descriptor {
	return descriptor{
		operation: "DeleteOp", action: "delete",
		description: fmt.Sprintf("Delete %s %s.", res.Article, res.HumanOne),
		confirm:     fmt.Sprintf("Delete %s {id}?", res.HumanOne),
		input:       "idArgsSchema", result: "noResultSchema",
		body: decodeInto("DeleteArgs") + "return nil, Delete(ctx, rc, a.ID)\n",
	}
}
