package mcp

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	json "github.com/go-json-experiment/json"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/op"
)

// toolNamePattern is MCP's name rule narrowed to what a descriptor ID
// turns into, so every tool name reads the same in every client.
var toolNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// ToolName is the tool a descriptor ID is served as: "bookmarks.create"
// is "bookmarks_create".
func ToolName(id string) string { return strings.ReplaceAll(id, ".", "_") }

// toolsFor checks every descriptor and builds its tool, with the
// descriptors by tool name for the list to look up.
func toolsFor(
	descriptors []op.Descriptor,
) ([]*sdk.Tool, map[string]op.Descriptor, error) {
	tools := make([]*sdk.Tool, 0, len(descriptors))
	byName := make(map[string]op.Descriptor, len(descriptors))
	for _, descriptor := range descriptors {
		if err := checkDescriptor(descriptor); err != nil {
			return nil, nil, err
		}
		name := ToolName(descriptor.ID)
		if other, taken := byName[name]; taken {
			return nil, nil, fmt.Errorf(
				"mcp: operations %s and %s are both the tool %s; "+
					"rename one", other.ID, descriptor.ID, name)
		}
		// Cloned, so an app that reuses its schema bytes cannot change
		// a tool the SDK was told would not change.
		descriptor.Input = slices.Clone(descriptor.Input)
		descriptor.Result = slices.Clone(descriptor.Result)
		byName[name] = descriptor
		tools = append(tools, toolFor(name, descriptor))
	}
	return tools, byName, nil
}

func checkDescriptor(descriptor op.Descriptor) error {
	if descriptor.ID == "" {
		return errors.New("mcp: a descriptor has an empty ID; " +
			"set it to the operation's ID, such as bookmarks.create")
	}
	id, name := descriptor.ID, ToolName(descriptor.ID)
	switch {
	case !descriptor.Access.Valid():
		return fmt.Errorf("mcp: operation %s: Access is not Read or Write",
			id)
	case !isObjectSchema(descriptor.Input):
		return fmt.Errorf("mcp: operation %s: Input must be an object "+
			`schema, {"type":"object",...}`, id)
	case !isObjectSchema(descriptor.Result) &&
		!isNullSchema(descriptor.Result):
		return fmt.Errorf("mcp: operation %s: Result must be an object "+
			`schema or exactly {"type":"null"}`, id)
	case descriptor.Invoke == nil:
		return fmt.Errorf("mcp: operation %s: Invoke is nil", id)
	}
	if err := op.CheckFlags(descriptor); err != nil {
		return fmt.Errorf("mcp: %w", err)
	}
	if !toolNamePattern.MatchString(name) {
		return fmt.Errorf("mcp: operation %s: its tool name %s must "+
			"match %s; use lowercase letters, digits, _ and .",
			id, name, toolNamePattern)
	}
	return nil
}

func schemaOf(schema []byte) map[string]any {
	var decoded map[string]any
	if err := json.Unmarshal(schema, &decoded); err != nil {
		return nil
	}
	return decoded
}

func isObjectSchema(schema []byte) bool {
	return schemaOf(schema)["type"] == "object"
}

// isNullSchema is the one schema a result with nothing in it may have,
// and the one that leaves a tool without an output schema.
func isNullSchema(schema []byte) bool {
	decoded := schemaOf(schema)
	return len(decoded) == 1 && decoded["type"] == "null"
}

func toolFor(name string, descriptor op.Descriptor) *sdk.Tool {
	destructive := descriptor.Destructive
	tool := &sdk.Tool{
		Name:        name,
		Title:       title(descriptor.Description),
		Description: describe(descriptor),
		InputSchema: descriptor.Input,
		// Every hint on every tool, as Oficina's adapter sends them;
		// openWorldHint stays unset, so clients read its default, true.
		Annotations: &sdk.ToolAnnotations{
			ReadOnlyHint:    descriptor.Access == rastrillo.Read,
			DestructiveHint: &destructive,
			IdempotentHint:  descriptor.Idempotent,
		},
	}
	if !isNullSchema(descriptor.Result) {
		tool.OutputSchema = descriptor.Result
	}
	return tool
}

// title is the description's first sentence: up to and including the
// first ". ", without the space, or all of it.
func title(description string) string {
	end := strings.Index(description, ". ")
	if end < 0 {
		return description
	}
	return description[:end+1]
}

// describe adds the consent sentence with its placeholders left as
// written, for the agent to fill in when it asks.
func describe(descriptor op.Descriptor) string {
	if descriptor.Confirm == "" {
		return descriptor.Description
	}
	return descriptor.Description + "\n\n" + confirmLine + descriptor.Confirm
}
