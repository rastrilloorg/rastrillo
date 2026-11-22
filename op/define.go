package op

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"

	rastrillo "amadan.net/rastrillo/rastrillo"
)

// Spec is what a hand-written operation says about itself. Define reads
// the rest, its schemas, from the Go types it is called with.
type Spec struct {
	Description string // first sentence is MCP's title
	Confirm     string
	Access      rastrillo.Access
	Destructive bool
	Idempotent  bool
	Scoped      bool   // rows belong to one person: Check needs a subject
	InQuery     bool   // a read the API serves at GET, input in the query
	HumanOnly   string // why only a person may call it
}

// Named is a declared operation. The page calls Call with typed input;
// the API and MCP call Descriptor.Invoke with JSON. Both reach the same
// function through the same Check, so no door can drift from another.
type Named[In, Out any] struct {
	Descriptor Descriptor
	run        func(context.Context, *rastrillo.Ctx, In) (Out, error)
}

// operationID is the name rule every door shares: the API route is
// /api/v1/<area>/<verb> and the tool <area>_<verb>, so one dot only.
var operationID = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`)

// maxToolName is MCP's limit on a tool name.
const maxToolName = 64

// Define declares the operation id, which run carries out. It panics,
// as regexp.MustCompile does, on a declaration a door would refuse or
// describe wrongly, because that must stop the app before it serves
// rather than surface on the first call through one door.
func Define[In, Out any](
	id string, spec Spec,
	run func(context.Context, *rastrillo.Ctx, In) (Out, error),
) *Named[In, Out] {
	inputType, resultType := reflect.TypeFor[In](), reflect.TypeFor[Out]()
	validateSpec(id, spec, inputType)
	if run == nil {
		panic(defineFailure(id, msgNilRun))
	}
	if resultType.Kind() != reflect.Struct {
		panic(defineFailure(id, fmt.Sprintf(msgResultNotStruct, resultType)))
	}
	named := &Named[In, Out]{run: run}
	named.Descriptor = describeSpec(id, spec)
	named.Descriptor.Input = schemaOrPanic(id, inputType, input)
	named.Descriptor.Result = schemaOrPanic(id, resultType, output)
	named.Descriptor.Invoke = named.invoke
	// CheckFlags's message already starts "op: <id>", so it is not
	// prefixed again.
	if err := CheckFlags(named.Descriptor); err != nil {
		panic(err.Error())
	}
	if spec.InQuery {
		checkFlat(id, named.Descriptor.Input)
	}
	return named
}

// Call runs the operation for the page door after Check, so a caller
// who may not run it never reaches run.
func (named *Named[In, Out]) Call(
	ctx context.Context, rc *rastrillo.Ctx, input In,
) (Out, error) {
	if err := Check(ctx, rc, named.Descriptor.Operation); err != nil {
		var zero Out
		return zero, err
	}
	return named.run(ctx, rc, input)
}

// invoke checks the caller, then decodes with the API's rules into a
// fresh input, so the API and MCP refuse the same arguments the same
// way. Check comes first, as at both doors: a caller who may not run
// the operation hears that, not what is wrong with its input, and an
// input type's own decoder never runs for it. A None result is nil,
// which the doors answer as nothing to read.
func (named *Named[In, Out]) invoke(
	ctx context.Context, rc *rastrillo.Ctx, arguments json.RawMessage,
) (any, error) {
	if err := Check(ctx, rc, named.Descriptor.Operation); err != nil {
		return nil, err
	}
	var decoded In
	if err := DecodeInput(arguments, &decoded); err != nil {
		return nil, err
	}
	result, err := named.run(ctx, rc, decoded)
	if err != nil {
		return nil, err
	}
	if _, none := any(result).(None); none {
		return nil, nil
	}
	return result, nil
}

func describeSpec(id string, spec Spec) Descriptor {
	return Descriptor{
		Operation: rastrillo.Operation{
			ID:        id,
			Resource:  id[:strings.Index(id, ".")],
			Scoped:    spec.Scoped,
			Access:    spec.Access,
			HumanOnly: spec.HumanOnly,
		},
		Exposed:     true,
		Description: spec.Description,
		Confirm:     spec.Confirm,
		Destructive: spec.Destructive,
		Idempotent:  spec.Idempotent,
		InQuery:     spec.InQuery,
	}
}

// validateSpec refuses what can be told from the ID, the spec and the
// input type alone, before any schema is derived.
func validateSpec(id string, spec Spec, inputType reflect.Type) {
	if !operationID.MatchString(id) {
		panic("op: " + fmt.Sprintf(msgBadID, id))
	}
	if tool := strings.Replace(id, ".", "_", 1); len(tool) > maxToolName {
		panic(defineFailure(id,
			fmt.Sprintf(msgLongToolName, tool, len(tool))))
	}
	switch {
	case strings.TrimSpace(spec.Description) == "":
		panic(defineFailure(id, msgNoDescription))
	case !spec.Access.Valid():
		panic(defineFailure(id, msgBadAccess))
	case inputType.Kind() != reflect.Struct:
		panic(defineFailure(id, fmt.Sprintf(msgInputNotStruct, inputType)))
	case spec.InQuery && spec.Access != rastrillo.Read:
		panic(defineFailure(id, msgQueryWrite))
	}
}

func schemaOrPanic(id string, goType reflect.Type, way direction) []byte {
	schema, err := schemaFor(goType, way)
	if err != nil {
		panic(defineFailure(id, err.Error()))
	}
	return schema
}

// flatTypes are what one query parameter can carry: the API reads each
// as a JSON scalar of its property's type.
var flatTypes = []string{"string", "integer", "number", "boolean"}

// checkFlat refuses an input the query string cannot carry. It reads the
// schema rather than the Go type, so embedded structs, Opt and Nullable
// are judged as the API's query reader will see them. Names are sorted
// so the refusal is the same on every run.
func checkFlat(id string, inputSchema []byte) {
	var described struct {
		Properties map[string]struct {
			Type any `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(inputSchema, &described); err != nil {
		panic(defineFailure(id, err.Error()))
	}
	names := make([]string, 0, len(described.Properties))
	for name := range described.Properties {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		kind, ok := described.Properties[name].Type.(string)
		if !ok || !slices.Contains(flatTypes, kind) {
			panic(defineFailure(id, fmt.Sprintf(msgQueryNotFlat, name)))
		}
	}
}

func defineFailure(id, message string) string {
	return "op: " + id + ": " + message
}
