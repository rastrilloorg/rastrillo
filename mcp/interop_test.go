package mcp

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/api"
	"amadan.net/rastrillo/rastrillo/op"
)

// README c4b8be0: testdata/memoria-contract.json is the output of
// `go run ./cmd/memoria-contract` in Memoria at c4b8be0 on
// origin/feat/agent-access. Re-export it at a newer pin to follow
// Memoria; the test reads it, and never edit it by hand.
//
// TestMemoriaInterop shows that Oficina's apps can move onto mcp.New
// without changing their operations. The descriptors are Memoria's own
// notes_create and notes_search, with every name, description, scope,
// flag and schema taken from that file. Their Invoke does what Oficina's
// Registry.Call does around the operation (unique JSON, the team before
// anything else, the schema with exact numbers, the output) over a fake
// store that counts its calls. Rastrillo must not import Oficina, so
// that wrapper is rewritten here.
func TestMemoriaInterop(t *testing.T) {
	checks := map[string]func(*testing.T){
		"lists_the_contract":         checkMemoriaList,
		"create_for_a_granted_team":  checkMemoriaCreate,
		"create_for_an_other_team":   checkMemoriaForbidden,
		"invalid_input_never_stores": checkMemoriaInvalid,
		"duplicate_keys_never_store": checkMemoriaDuplicates,
		"number_past_a_bound":        checkMemoriaBound,
		"output_breaking_its_schema": checkMemoriaBadOutput,
		"exact_number_in_the_result": checkMemoriaExactNumber,
		"search_results_and_notices": checkMemoriaSearch,
		"scope_hides_and_refuses":    checkMemoriaScope,
		"narrowed_teams_at_emission": checkMemoriaNarrowed,
	}
	for name, check := range checks {
		t.Run(name, check)
	}
}

//go:embed testdata/memoria-contract.json
var memoriaContractJSON []byte

// memoriaOperation is one entry of the contract command's output.
type memoriaOperation struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	Scope        string          `json:"scope"`
	ReadOnly     bool            `json:"read_only"`
	Destructive  bool            `json:"destructive"`
	Idempotent   bool            `json:"idempotent"`
	InputSchema  json.RawMessage `json:"input_schema"`
	OutputSchema json.RawMessage `json:"output_schema"`
}

// memoriaContract is the file's operations. A field the test does not
// know is an error, so a re-export that adds one cannot pass unnoticed.
func memoriaContract(t *testing.T) []memoriaOperation {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(memoriaContractJSON))
	decoder.DisallowUnknownFields()
	var operations []memoriaOperation
	if err := decoder.Decode(&operations); err != nil {
		t.Fatalf("memoria-contract.json: %v", err)
	}
	names := []string{}
	for _, operation := range operations {
		names = append(names, operation.Name)
	}
	if !slices.Equal(names, []string{"notes_create", "notes_search"}) {
		t.Fatalf("contract holds %v, want notes_create, notes_search",
			names)
	}
	return operations
}

// operationID is a contract name as a descriptor ID: its first
// separator becomes a dot, so ToolName gives the name back.
func operationID(name string) string {
	return strings.Replace(name, "_", ".", 1)
}

// descriptor is the contract entry as mcp.New takes it.
func (operation memoriaOperation) descriptor(
	t *testing.T, store *memoriaStore,
) op.Descriptor {
	t.Helper()
	access := rastrillo.Write
	if operation.ReadOnly {
		access = rastrillo.Read
	}
	id := operationID(operation.Name)
	resource, _, _ := strings.Cut(id, ".")
	if ToolName(id) != operation.Name {
		t.Fatalf("ToolName(%s) = %s, want %s",
			id, ToolName(id), operation.Name)
	}
	return op.Descriptor{
		Operation: rastrillo.Operation{
			ID: id, Resource: resource, Scoped: true, Access: access,
		},
		Exposed:     true,
		Description: operation.Description,
		Destructive: operation.Destructive,
		Idempotent:  operation.Idempotent,
		Input:       operation.InputSchema,
		Result:      operation.OutputSchema,
		Invoke:      operation.invoke(t, store),
	}
}

// invoke is Registry.Call's order around the store: unique JSON, an
// object, the team and scope before anything else, the input schema,
// the store, then the output schema. Nothing but the first three is
// allowed to run before the team is checked.
func (operation memoriaOperation) invoke(
	t *testing.T, store *memoriaStore,
) func(context.Context, *rastrillo.Ctx, json.RawMessage) (any, error) {
	inputSchema := schemaFor(t, operation.InputSchema)
	outputSchema := schemaFor(t, operation.OutputSchema)
	return func(
		_ context.Context, rc *rastrillo.Ctx, raw json.RawMessage,
	) (any, error) {
		if err := uniqueKeys(raw); err != nil {
			return nil, invalidInput(err)
		}
		var arguments map[string]any
		if err := decodeExact(raw, &arguments); err != nil ||
			arguments == nil {
			return nil, invalidInput(fmt.Errorf("not an object"))
		}
		team, _ := arguments["team"].(string)
		if !allows(rc.Actor, team, operation.Scope) {
			return nil, op.Forbidden{Reason: "team or scope not granted"}
		}
		if err := validateExact(inputSchema, arguments); err != nil {
			return nil, invalidInput(err)
		}
		return checkedOutput(store.run(operation.Name), outputSchema)
	}
}

func invalidInput(cause error) error {
	return op.Invalid{Fields: map[string]string{"input": cause.Error()}}
}

// allows is Principal.Allows: the grant names the team and the scope.
func allows(actor rastrillo.Actor, team, scope string) bool {
	return actor.Grant != nil && team != "" &&
		slices.Contains(actor.Grant.Teams, team) &&
		slices.Contains(actor.Grant.Scopes, scope)
}

// checkedOutput encodes what the store returned, decodes it with exact
// numbers and holds it to the output schema. A plain error is how the
// tool answers internal, as Registry.Call's "internal" does.
func checkedOutput(result any, schema map[string]any) (any, error) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("encode the result: %w", err)
	}
	var value any
	if err := decodeExact(encoded, &value); err != nil {
		return nil, fmt.Errorf("decode the result: %w", err)
	}
	if err := validateExact(schema, value); err != nil {
		return nil, fmt.Errorf("result breaks its schema: %w", err)
	}
	return value, nil
}

// memoriaStore stands in for Memoria's domain code: it counts how often
// anything reaches it, and answers with what a test set.
type memoriaStore struct {
	calls   atomic.Int64
	answers map[string]any
	// afterCall runs once the store has answered, so a test can change
	// the grant between the call and the answer going out.
	afterCall func()
}

func newMemoriaStore() *memoriaStore {
	return &memoriaStore{answers: map[string]any{
		"notes_create": map[string]any{
			"id": json.Number("42"), "indexed": true,
		},
		"notes_search": map[string]any{
			"results": []any{}, "notices": []any{},
		},
	}}
}

func (store *memoriaStore) run(name string) any {
	store.calls.Add(1)
	if store.afterCall != nil {
		store.afterCall()
	}
	return store.answers[name]
}

// connectedToken is the one token the fake connection service knows.
const connectedToken = "connected"

// fakeConnection is a connection service's verifier and rechecker: it
// answers every check with the grant as it stands now.
type fakeConnection struct {
	mutex    sync.Mutex
	teams    []string
	scopes   []string
	rechecks atomic.Int64
}

func (connection *fakeConnection) actor() rastrillo.Actor {
	connection.mutex.Lock()
	defer connection.mutex.Unlock()
	return rastrillo.Actor{
		Subject: "u1", Agent: "Claude", Credential: "tok-connected",
		Access: rastrillo.Write,
		Grant: &rastrillo.Grant{
			ID: "g1", Issuer: "connections", IdentityIssuer: "home",
			Audience: "memoria",
			Teams:    slices.Clone(connection.teams),
			Scopes:   slices.Clone(connection.scopes),
		},
	}
}

func (connection *fakeConnection) Verify(
	_ context.Context, request *http.Request,
) (rastrillo.Actor, error) {
	token, _ := strings.CutPrefix(
		request.Header.Get("Authorization"), "Bearer ")
	if token != connectedToken {
		return rastrillo.Actor{},
			fmt.Errorf("fake: unknown token: %w", api.ErrUnauthorized)
	}
	return connection.actor(), nil
}

func (connection *fakeConnection) Recheck(
	_ context.Context, _ *http.Request, was rastrillo.Actor,
) (rastrillo.Actor, error) {
	connection.rechecks.Add(1)
	fresh := was
	fresh.Grant = connection.actor().Grant
	return fresh, nil
}

func (connection *fakeConnection) narrow(teams ...string) {
	connection.mutex.Lock()
	defer connection.mutex.Unlock()
	connection.teams = teams
}

// scopePolicy is the app's Authorize: the grant must name the scope the
// contract gives the operation.
func scopePolicy(operations []memoriaOperation) func(
	context.Context, rastrillo.Actor, rastrillo.Operation) error {
	scopes := map[string]string{}
	for _, operation := range operations {
		scopes[operationID(operation.Name)] = operation.Scope
	}
	return func(
		_ context.Context, actor rastrillo.Actor,
		operation rastrillo.Operation,
	) error {
		if actor.Grant == nil ||
			!slices.Contains(actor.Grant.Scopes, scopes[operation.ID]) {
			return op.Forbidden{Reason: "scope not granted"}
		}
		return nil
	}
}

// teamGuard is the app's Guard: as an answer goes out, the team the
// input named must still be one the fresh grant names.
func teamGuard(
	_ context.Context, fresh rastrillo.Actor, descriptor *op.Descriptor,
	input json.RawMessage, _ any,
) error {
	if descriptor == nil {
		return nil
	}
	var named struct {
		Team string `json:"team"`
	}
	if err := json.Unmarshal(input, &named); err != nil ||
		fresh.Grant == nil ||
		!slices.Contains(fresh.Grant.Teams, named.Team) {
		return op.Forbidden{Reason: "team no longer granted"}
	}
	return nil
}

// interop is the endpoint under test with its fakes.
type interop struct {
	url        string
	store      *memoriaStore
	connection *fakeConnection
}

// newInterop serves operations through mcp.New, guarded, for a
// connection granted teams and every scope the contract names.
func newInterop(
	t *testing.T, operations []memoriaOperation, teams ...string,
) *interop {
	t.Helper()
	rig := &interop{
		store:      newMemoriaStore(),
		connection: &fakeConnection{teams: teams},
	}
	descriptors := []op.Descriptor{}
	for _, operation := range operations {
		rig.connection.scopes = append(rig.connection.scopes,
			operation.Scope)
		descriptors = append(descriptors, operation.descriptor(t, rig.store))
	}
	config := countedConfig(rig.connection).config
	config.Authorize = scopePolicy(operations)
	config.Guard, config.Guarded = teamGuard, true
	rig.url = serve(t, config, descriptors...)
	return rig
}

// send posts a tools/call with arguments as the exact bytes given.
func (rig *interop) send(t *testing.T, tool, arguments string) posted {
	t.Helper()
	return post(t, http.MethodPost, rig.url, connectedToken,
		"2025-06-18", callBody(tool, arguments))
}

// createArguments are a valid notes_create input for team "a", changed
// by change. Bodies with escapes or duplicate keys are not built here.
func createArguments(t *testing.T, change func(map[string]any)) string {
	t.Helper()
	arguments := map[string]any{
		"team": "a", "title": "Meeting", "body": "Agenda",
		"client_id": strings.Repeat("c", 36),
	}
	if change != nil {
		change(arguments)
	}
	encoded, err := json.Marshal(arguments)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

// refusedAs asserts that a call was refused with code and never
// reached the store.
func (rig *interop) refusedAs(
	t *testing.T, tool, arguments, code string,
) {
	t.Helper()
	before := rig.store.calls.Load()
	refusal := rig.send(t, tool, arguments).call(t).refusal(t)
	if refusal.Code != code {
		t.Errorf("%s %s: code %q, want %q", tool, arguments,
			refusal.Code, code)
	}
	if rig.store.calls.Load() != before {
		t.Errorf("%s %s: the store was called, want it untouched",
			tool, arguments)
	}
}

func checkMemoriaList(t *testing.T) {
	contract := memoriaContract(t)
	rig := newInterop(t, contract, "a")
	session, _ := connect(t, rig.url, connectedToken, "2025-06-18")
	listed, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	want := []string{}
	for _, operation := range contract {
		want = append(want, operation.Name)
	}
	if got := namesOf(listed.Tools); !slices.Equal(got, want) {
		t.Fatalf("listed %v, want the contract's %v", got, want)
	}
	for _, tool := range listed.Tools {
		for _, operation := range contract {
			if operation.Name == tool.Name {
				sameAsContract(t, operation, tool)
			}
		}
	}
}

// sameAsContract holds a listed tool to its contract entry.
func sameAsContract(
	t *testing.T, operation memoriaOperation, tool *sdk.Tool,
) {
	t.Helper()
	if tool.Description != operation.Description {
		t.Errorf("%s description %q, want %q",
			tool.Name, tool.Description, operation.Description)
	}
	hints := tool.Annotations
	if hints == nil || hints.DestructiveHint == nil ||
		hints.ReadOnlyHint != operation.ReadOnly ||
		*hints.DestructiveHint != operation.Destructive ||
		hints.IdempotentHint != operation.Idempotent {
		t.Errorf("%s annotations %+v, want read_only %v, destructive "+
			"%v, idempotent %v", tool.Name, hints, operation.ReadOnly,
			operation.Destructive, operation.Idempotent)
	}
	sameSchema(t, tool.Name+" input schema",
		tool.InputSchema, operation.InputSchema)
	sameSchema(t, tool.Name+" output schema",
		tool.OutputSchema, operation.OutputSchema)
}

func sameSchema(t *testing.T, what string, got any, want []byte) {
	t.Helper()
	var wanted any
	if err := json.Unmarshal(want, &wanted); err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	if !reflect.DeepEqual(got, wanted) {
		t.Errorf("%s is %v, want the contract's %v", what, got, wanted)
	}
}

func checkMemoriaCreate(t *testing.T) {
	rig := newInterop(t, memoriaContract(t), "a")
	session, _ := connect(t, rig.url, connectedToken, "2025-06-18")
	arguments := json.RawMessage(createArguments(t, nil))
	got := structured(t, callTool(t, session, "notes_create", arguments))
	want := map[string]any{"id": float64(42), "indexed": true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("notes_create answered %v, want %v", got, want)
	}
	if rig.store.calls.Load() != 1 {
		t.Errorf("the store was called %d times, want once",
			rig.store.calls.Load())
	}
	if rig.connection.rechecks.Load() == 0 {
		t.Error("the answer went out without a recheck")
	}
}

func checkMemoriaForbidden(t *testing.T) {
	rig := newInterop(t, memoriaContract(t), "a")
	for name, change := range map[string]func(map[string]any){
		"another_team": func(arguments map[string]any) {
			arguments["team"] = "x"
		},
		"no_team": func(arguments map[string]any) {
			delete(arguments, "team")
		},
		// The team comes before the schema, so a caller learns nothing
		// about the schema of a team it may not use.
		"another_team_and_invalid": func(arguments map[string]any) {
			arguments["team"] = "x"
			arguments["client_id"] = "short"
			arguments["extra"] = true
		},
	} {
		t.Run(name, func(t *testing.T) {
			rig.refusedAs(t, "notes_create",
				createArguments(t, change), "forbidden")
		})
	}
	if rig.store.calls.Load() != 0 {
		t.Errorf("the store was called %d times, want 0",
			rig.store.calls.Load())
	}
}

func checkMemoriaInvalid(t *testing.T) {
	rig := newInterop(t, memoriaContract(t), "a")
	for name, change := range map[string]func(map[string]any){
		"extra_field": func(arguments map[string]any) {
			arguments["extra"] = "x"
		},
		"client_id_of_35": func(arguments map[string]any) {
			arguments["client_id"] = strings.Repeat("c", 35)
		},
		"client_id_of_37": func(arguments map[string]any) {
			arguments["client_id"] = strings.Repeat("c", 37)
		},
		"missing_title": func(arguments map[string]any) {
			delete(arguments, "title")
		},
		"title_not_a_string": func(arguments map[string]any) {
			arguments["title"] = 5
		},
		"empty_body": func(arguments map[string]any) {
			arguments["body"] = ""
		},
	} {
		t.Run(name, func(t *testing.T) {
			rig.refusedAs(t, "notes_create",
				createArguments(t, change), "invalid")
		})
	}
	if rig.store.calls.Load() != 0 {
		t.Errorf("the store was called %d times, want 0",
			rig.store.calls.Load())
	}
}

// checkMemoriaDuplicates sends the bodies as bytes: a map cannot hold
// the same key twice, and an escape in source may be written back as
// the character it stands for.
func checkMemoriaDuplicates(t *testing.T) {
	rig := newInterop(t, memoriaContract(t), "a", "b")
	rest := `,"title":"T","body":"B","client_id":"` +
		strings.Repeat("c", 36) + `"}`
	for name, arguments := range map[string]string{
		"plain":   `{"team":"a","team":"b"` + rest,
		"escaped": `{"team":"a","t` + escape("0065") + `am":"b"` + rest,
	} {
		t.Run(name, func(t *testing.T) {
			rig.refusedAs(t, "notes_create", arguments, "invalid")
		})
	}
	if rig.store.calls.Load() != 0 {
		t.Errorf("the store was called %d times, want 0",
			rig.store.calls.Load())
	}
}

// checkMemoriaBound uses a descriptor of its own, because the contract
// has no numeric bound on an input. 2^53 + 1 is one past the bound and
// rounds to the bound as a float64, so only an exact comparison refuses.
func checkMemoriaBound(t *testing.T) {
	window := memoriaOperation{
		Name: "notes_window", Description: "Test only.",
		Scope: "notes:read", ReadOnly: true, Idempotent: true,
		InputSchema: json.RawMessage(`{"type":"object",` +
			`"required":["team","limit"],"additionalProperties":false,` +
			`"properties":{"team":{"type":"string","minLength":1},` +
			`"limit":{"type":"integer","maximum":9007199254740992}}}`),
		OutputSchema: json.RawMessage(`{"type":"object",` +
			`"required":["limit"],"additionalProperties":false,` +
			`"properties":{"limit":{"type":"integer"}}}`),
	}
	rig := newInterop(t, []memoriaOperation{window}, "a")
	rig.store.answers["notes_window"] = map[string]any{
		"limit": json.Number("1"),
	}
	for _, limit := range []string{"9007199254740993", "9007199254740992.5"} {
		rig.refusedAs(t, "notes_window",
			`{"team":"a","limit":`+limit+`}`, "invalid")
	}
	answer := rig.send(t, "notes_window",
		`{"team":"a","limit":9007199254740992}`).call(t)
	if answer.Result.IsError || rig.store.calls.Load() != 1 {
		t.Errorf("the bound itself answered %+v after %d store calls, "+
			"want a result after 1", answer.Result,
			rig.store.calls.Load())
	}
}

func checkMemoriaBadOutput(t *testing.T) {
	rig := newInterop(t, memoriaContract(t), "a")
	for name, answer := range map[string]map[string]any{
		"missing_indexed": {"id": json.Number("1")},
		"id_below_minimum": {
			"id": json.Number("0"), "indexed": true,
		},
		"id_not_an_integer": {
			"id": json.Number("1.5"), "indexed": true,
		},
		"extra_field": {
			"id": json.Number("1"), "indexed": true, "extra": 1,
		},
	} {
		t.Run(name, func(t *testing.T) {
			before := rig.store.calls.Load()
			rig.store.answers["notes_create"] = answer
			refusal := rig.send(t, "notes_create",
				createArguments(t, nil)).call(t).refusal(t)
			if refusal.Code != "internal" || refusal.Ref == "" {
				t.Errorf("answered %+v, want internal with a ref", refusal)
			}
			if rig.store.calls.Load() != before+1 {
				t.Errorf("the store was called %d times, want once",
					rig.store.calls.Load()-before)
			}
		})
	}
}

// numbersOf decodes a JSON text keeping its numbers exact, so a number
// that went out quoted shows up as a string.
func numbersOf(t *testing.T, text string) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := decodeExact([]byte(text), &decoded); err != nil {
		t.Fatalf("%q is not a JSON object: %v", text, err)
	}
	return decoded
}

func checkMemoriaExactNumber(t *testing.T) {
	rig := newInterop(t, memoriaContract(t), "a")
	rig.store.answers["notes_create"] = map[string]any{
		"id": json.Number("9007199254740993"), "indexed": true,
	}
	answer := rig.send(t, "notes_create", createArguments(t, nil)).call(t)
	if answer.Result.IsError {
		t.Fatalf("a result with a large id was refused: %+v", answer.Result)
	}
	for where, text := range map[string]string{
		"structuredContent": string(answer.Result.StructuredContent),
		"text block":        answer.Result.Content[0].Text,
	} {
		if !strings.Contains(text, `"id":9007199254740993`) {
			t.Errorf("%s is %s, want id 9007199254740993 unquoted",
				where, text)
		}
		id := numbersOf(t, text)["id"]
		if id != json.Number("9007199254740993") {
			t.Errorf("%s id is %#v, want json.Number 9007199254740993",
				where, id)
		}
	}
}

func checkMemoriaSearch(t *testing.T) {
	const found = `{"results":[{"id":9007199254740993,"document_id":7,` +
		`"title":"Plan","text":"Ship <it> & café","source":"memoria"}` +
		`],"notices":["A sibling app did not answer."]}`
	rig := newInterop(t, memoriaContract(t), "a")
	rig.store.answers["notes_search"] = json.RawMessage(found)
	answer := rig.send(t, "notes_search",
		`{"team":"a","query":"plan"}`).call(t)
	if answer.Result.IsError {
		t.Fatalf("search was refused: %+v", answer.Result)
	}
	want := numbersOf(t, found)
	for where, text := range map[string]string{
		"structuredContent": string(answer.Result.StructuredContent),
		"text block":        answer.Result.Content[0].Text,
	} {
		if got := numbersOf(t, text); !reflect.DeepEqual(got, want) {
			t.Errorf("%s is %v, want %v", where, got, want)
		}
	}
}

// checkMemoriaScope shows the contract's scopes working through the
// app's Authorize: a grant with only the read scope is shown the read
// tool alone, and a call to the write tool is refused before the store.
func checkMemoriaScope(t *testing.T) {
	rig := newInterop(t, memoriaContract(t), "a")
	rig.connection.scopes = []string{"notes:read"}
	got := toolNames(t, rig.url, connectedToken)
	if !slices.Equal(got, []string{"notes_search"}) {
		t.Errorf("a read-only grant is shown %v, want notes_search", got)
	}
	rig.refusedAs(t, "notes_create", createArguments(t, nil), "forbidden")
}

// checkMemoriaNarrowed changes the grant's teams after the store has
// answered, as a revoked team would between a call and its answer going
// out. The guard sees the input's team no longer granted, and what the
// store answered never goes out.
func checkMemoriaNarrowed(t *testing.T) {
	for tool, test := range map[string]struct{ arguments, secret string }{
		"notes_create": {createArguments(t, nil), `"indexed"`},
		"notes_search": {`{"team":"a","query":"plan"}`, "leaked"},
	} {
		t.Run(tool, func(t *testing.T) {
			rig := newInterop(t, memoriaContract(t), "a")
			rig.store.answers["notes_search"] = map[string]any{
				"results": []any{}, "notices": []any{"leaked"},
			}
			rig.store.afterCall = func() { rig.connection.narrow("z") }
			answer := rig.send(t, tool, test.arguments)
			if answer.status != http.StatusForbidden {
				t.Errorf("answer %d %s, want 403",
					answer.status, answer.body)
			}
			if bytes.Contains(answer.body, []byte(test.secret)) {
				t.Errorf("the refused answer carries %s: %s",
					test.secret, answer.body)
			}
			if rig.store.calls.Load() != 1 {
				t.Errorf("the store was called %d times, want once",
					rig.store.calls.Load())
			}
		})
	}
}

// schemaFor reads a schema with its numbers exact.
func schemaFor(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var schema map[string]any
	if err := decodeExact(raw, &schema); err != nil {
		t.Fatalf("schema %s: %v", raw, err)
	}
	return schema
}

// decodeExact decodes with numbers kept as json.Number, as Oficina's
// DecodeJSON does, so no integer goes through float64.
func decodeExact(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	return decoder.Decode(target)
}

// uniqueKeys is Oficina's UniqueJSON: one value, and no object that
// repeats a key once its escapes are read.
func uniqueKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := walkKeys(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

func walkKeys(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, isContainer := token.(json.Delim)
	if !isContainer {
		return nil
	}
	seen := map[string]bool{}
	for decoder.More() {
		if delimiter == '{' {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, _ := key.(string)
			if seen[name] {
				return fmt.Errorf("duplicate key %q", name)
			}
			seen[name] = true
		}
		if err := walkKeys(decoder); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

// keywords are the schema keywords validateExact understands. Any other
// one is an error, so a re-export that adds a constraint cannot pass
// as though it were checked.
var keywords = []string{
	"type", "required", "additionalProperties", "properties", "items",
	"minLength", "maxLength", "minimum", "maximum",
}

// validateExact holds value, decoded with json.Number, to the keywords
// the contract uses. Numbers are compared as big rationals, so 2^53 + 1
// is above a maximum of 2^53.
func validateExact(schema map[string]any, value any) error {
	for keyword := range schema {
		if !slices.Contains(keywords, keyword) {
			return fmt.Errorf("unsupported schema keyword %q", keyword)
		}
	}
	if err := checkType(schema["type"], value); err != nil {
		return err
	}
	switch typed := value.(type) {
	case map[string]any:
		return validateObject(schema, typed)
	case []any:
		return validateItems(schema, typed)
	case string:
		return validateString(schema, typed)
	case json.Number:
		return validateNumber(schema, typed)
	}
	return nil
}

func checkType(want, value any) error {
	var matches bool
	switch want {
	case nil:
		matches = true
	case "object":
		_, matches = value.(map[string]any)
	case "array":
		_, matches = value.([]any)
	case "string":
		_, matches = value.(string)
	case "boolean":
		_, matches = value.(bool)
	case "integer":
		number, isNumber := value.(json.Number)
		parsed, parses := new(big.Rat).SetString(string(number))
		matches = isNumber && parses && parsed.IsInt()
	default:
		return fmt.Errorf("unsupported schema type %v", want)
	}
	if !matches {
		return fmt.Errorf("%#v is not a %v", value, want)
	}
	return nil
}

func validateObject(schema map[string]any, object map[string]any) error {
	required, _ := schema["required"].([]any)
	for _, name := range required {
		if _, present := object[name.(string)]; !present {
			return fmt.Errorf("%v is required", name)
		}
	}
	properties, _ := schema["properties"].(map[string]any)
	for name, member := range object {
		known, declared := properties[name].(map[string]any)
		if !declared {
			if schema["additionalProperties"] == false {
				return fmt.Errorf("%s is not a known field", name)
			}
			continue
		}
		if err := validateExact(known, member); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func validateItems(schema map[string]any, items []any) error {
	each, _ := schema["items"].(map[string]any)
	for position, item := range items {
		if each == nil {
			break
		}
		if err := validateExact(each, item); err != nil {
			return fmt.Errorf("item %d: %w", position, err)
		}
	}
	return nil
}

func validateString(schema map[string]any, text string) error {
	length := big.NewRat(int64(utf8.RuneCountInString(text)), 1)
	if err := within(schema, length); err != nil {
		return fmt.Errorf("string of %v characters: %w", length, err)
	}
	return nil
}

func validateNumber(schema map[string]any, number json.Number) error {
	value, parses := new(big.Rat).SetString(string(number))
	if !parses {
		return fmt.Errorf("%q is not a number", number)
	}
	return within(schema, value)
}

// within checks value against the bounds a schema sets: minimum and
// maximum for a number, minLength and maxLength for a string's length.
func within(schema map[string]any, value *big.Rat) error {
	for keyword, order := range map[string]int{
		"minimum": -1, "minLength": -1, "maximum": 1, "maxLength": 1,
	} {
		set, has := schema[keyword].(json.Number)
		if !has {
			continue
		}
		bound, _ := new(big.Rat).SetString(string(set))
		if value.Cmp(bound) == order {
			return fmt.Errorf("%v breaks %s %v", value, keyword, set)
		}
	}
	return nil
}
