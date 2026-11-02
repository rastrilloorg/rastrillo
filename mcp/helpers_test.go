package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/api"
	"amadan.net/rastrillo/rastrillo/op"
)

// The tokens the fake verifier knows: a writer, a read-only token, and a
// connection service's token whose grant names only the read scope.
const (
	writerToken  = "writer"
	readerToken  = "reader"
	grantedToken = "granted"
)

var actors = map[string]rastrillo.Actor{
	writerToken: {
		Subject: "u1", Agent: "Claude", Credential: "tok-writer",
		Access: rastrillo.Write,
	},
	readerToken: {
		Subject: "u2", Agent: "Claude", Credential: "tok-reader",
		Access: rastrillo.Read,
	},
	grantedToken: {
		Subject: "u3", Agent: "Claude", Credential: "tok-granted",
		Access: rastrillo.Write,
		Grant: &rastrillo.Grant{
			ID: "g1", Scopes: []string{"bookmarks:read"},
		},
	},
}

// fakeVerifier knows the tokens above. Each Verify builds a new Grant,
// as a real store's lookup would, so no two requests share one.
type fakeVerifier struct{}

func (fakeVerifier) Verify(
	_ context.Context, request *http.Request,
) (rastrillo.Actor, error) {
	token, _ := strings.CutPrefix(
		request.Header.Get("Authorization"), "Bearer ")
	actor, known := actors[token]
	if !known {
		return rastrillo.Actor{},
			fmt.Errorf("fake: unknown token: %w", api.ErrUnauthorized)
	}
	if actor.Grant != nil {
		grant := *actor.Grant
		grant.Scopes = slices.Clone(grant.Scopes)
		actor.Grant = &grant
	}
	return actor, nil
}

// fakeRechecker answers Recheck with the original actor, or with it made
// read-only once narrowed is set, or not at all once revoked is, and
// counts its calls.
type fakeRechecker struct {
	fakeVerifier
	narrowed atomic.Bool
	revoked  atomic.Bool
	rechecks atomic.Int64
}

func (checker *fakeRechecker) Recheck(
	_ context.Context, _ *http.Request, was rastrillo.Actor,
) (rastrillo.Actor, error) {
	checker.rechecks.Add(1)
	if checker.revoked.Load() {
		return rastrillo.Actor{},
			fmt.Errorf("fake: revoked: %w", api.ErrUnauthorized)
	}
	fresh := was
	fresh.Session, fresh.Harness = "", ""
	if checker.narrowed.Load() {
		fresh.Access = rastrillo.Read
	}
	return fresh, nil
}

// counted is a Config whose Ctx factory and Authorize count their calls,
// so a test can tell whether a request reached the SDK's handlers.
type counted struct {
	config         Config
	contexts       atomic.Int64
	authorizations atomic.Int64
}

func countedConfig(verifier api.Verifier) *counted {
	shared := &rastrillo.Ctx{}
	counts := &counted{}
	counts.config = Config{
		Gate: api.Gate{
			Verify: verifier,
			Logger: slog.New(slog.DiscardHandler),
		},
		Ctx: func(*http.Request) *rastrillo.Ctx {
			counts.contexts.Add(1)
			return shared
		},
		Authorize: func(
			context.Context, rastrillo.Actor, rastrillo.Operation,
		) error {
			counts.authorizations.Add(1)
			return nil
		},
		Name:    "bookmarks",
		Version: "v1.2.3",
	}
	return counts
}

func testConfig() Config { return countedConfig(fakeVerifier{}).config }

// serve mounts New's handler on a real server, because an answer that
// goes through a Rechecker needs a transport that sets write deadlines.
func serve(
	t *testing.T, config Config, descriptors ...op.Descriptor,
) string {
	t.Helper()
	handler, err := New(config, descriptors...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server.URL
}

// bearer adds a token to every request the SDK client sends, and
// records the JSON-RPC method of each so a test can see the handshake.
type bearer struct {
	token   string
	mutex   sync.Mutex
	methods []string
}

func (transport *bearer) RoundTrip(
	request *http.Request,
) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set("Authorization", "Bearer "+transport.token)
	if request.Body != nil {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		request.Body = io.NopCloser(bytes.NewReader(body))
		var message struct{ Method string }
		_ = json.Unmarshal(body, &message)
		transport.mutex.Lock()
		transport.methods = append(transport.methods, message.Method)
		transport.mutex.Unlock()
	}
	return http.DefaultTransport.RoundTrip(request)
}

func (transport *bearer) sent() []string {
	transport.mutex.Lock()
	defer transport.mutex.Unlock()
	return slices.Clone(transport.methods)
}

// connect opens an SDK client session as token's holder. An empty
// version is the SDK's newest.
func connect(
	t *testing.T, url, token, version string,
) (*sdk.ClientSession, *bearer) {
	t.Helper()
	transport := &bearer{token: token}
	client := sdk.NewClient(
		&sdk.Implementation{Name: "test", Version: "v0"}, nil)
	session, err := client.Connect(t.Context(),
		&sdk.StreamableClientTransport{
			Endpoint:   url,
			HTTPClient: &http.Client{Transport: transport},
		},
		&sdk.ClientSessionOptions{ProtocolVersion: version})
	if err != nil {
		t.Fatalf("connect at %q as %s: %v", version, token, err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session, transport
}

// toolNames lists the tools token's holder is shown, sorted.
func toolNames(t *testing.T, url, token string) []string {
	t.Helper()
	session, _ := connect(t, url, token, "2025-06-18")
	listed, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("tools/list as %s: %v", token, err)
	}
	return namesOf(listed.Tools)
}

func namesOf(tools []*sdk.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	return names
}

// bookmarks is a generated resource's descriptors, with the flags the
// generator sets: create adds, update overwrites, delete removes and
// can be repeated, and list and get only read.
func bookmarks() []op.Descriptor {
	return []op.Descriptor{
		bookmark("create", rastrillo.Write, false, false,
			"Create a bookmark. Send its URL and title.", ""),
		bookmark("list", rastrillo.Read, false, true,
			"List your bookmarks, newest first.", ""),
		bookmark("get", rastrillo.Read, false, true,
			"Get one bookmark by its ID.", ""),
		bookmark("update", rastrillo.Write, true, false,
			"Change a bookmark. Send only the fields to change.",
			"Change a bookmark {id}?"),
		bookmark("delete", rastrillo.Write, true, true,
			"Delete a bookmark. It cannot be brought back.",
			"Delete a bookmark {id}?"),
	}
}

const bookmarkSchema = `{"type":"object","properties":{` +
	`"id":{"type":"string"},"url":{"type":"string"}}}`

func bookmark(
	action string, access rastrillo.Access, destructive, idempotent bool,
	description, confirm string,
) op.Descriptor {
	result := json.RawMessage(bookmarkSchema)
	if action == "delete" {
		result = json.RawMessage(`{"type":"null"}`)
	}
	return op.Descriptor{
		Operation: rastrillo.Operation{
			ID: "bookmarks." + action, Resource: "bookmarks",
			Scoped: true, Access: access,
		},
		Exposed:     true,
		Description: description,
		Confirm:     confirm,
		Destructive: destructive,
		Idempotent:  idempotent,
		Input: json.RawMessage(
			`{"type":"object","properties":{"limit":{"type":"integer",` +
				`"minimum":1,"maximum":200}}}`),
		Result: result,
		Invoke: func(
			context.Context, *rastrillo.Ctx, json.RawMessage,
		) (any, error) {
			return nil, nil
		},
	}
}

// posted is one raw answer, read whole.
type posted struct {
	status int
	header http.Header
	body   []byte
}

// post sends one body straight over HTTP, as a client the SDK does not
// model would, with the headers a streamable HTTP client sends.
func post(
	t *testing.T, method, url, token, version, body string,
) posted {
	t.Helper()
	request, err := http.NewRequestWithContext(
		t.Context(), method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	if version != "" {
		request.Header.Set("MCP-Protocol-Version", version)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer response.Body.Close()
	read, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read the answer: %v", err)
	}
	return posted{response.StatusCode, response.Header, read}
}

// rpcError is a JSON-RPC error answer, with the id it names kept raw so
// a test can tell null from absent.
type rpcError struct {
	Version string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Error   struct {
		Code    int64           `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	} `json:"error"`
}

func (answer posted) rpcError(t *testing.T) rpcError {
	t.Helper()
	var decoded rpcError
	if err := json.Unmarshal(answer.body, &decoded); err != nil {
		t.Fatalf("answer %d is not JSON-RPC: %v: %s",
			answer.status, err, answer.body)
	}
	return decoded
}

const listBody = `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`

// storedBookmark is one row of the in-memory store, and the result a
// create, get or update answers with. The owner never goes out.
type storedBookmark struct {
	ID    string `json:"id"`
	URL   string `json:"url"`
	Title string `json:"title"`
	owner string
}

// bookmarkInput is every member the five bookmark tools accept.
type bookmarkInput struct {
	ID    op.Opt[string] `json:"id"`
	URL   op.Opt[string] `json:"url"`
	Title op.Opt[string] `json:"title"`
	Limit op.Opt[int]    `json:"limit"`
}

// bookmarkStore is the owner-scoped table behind bookmarks(), standing
// in for a generated resource until Task 9 runs the generated ones:
// each Invoke runs op.Check first and decodes with op.DecodeInput, and
// another person's row is not found.
type bookmarkStore struct {
	mutex   sync.Mutex
	rows    map[string]storedBookmark
	last    int
	writes  atomic.Int64
	invokes atomic.Int64
	// afterWrite runs once a write is stored, so a test can change the
	// token between Invoke and the answer.
	afterWrite func()
}

func newBookmarkStore() *bookmarkStore {
	return &bookmarkStore{rows: map[string]storedBookmark{}}
}

// descriptors are bookmarks(), with the generator's flags, over store.
func (store *bookmarkStore) descriptors() []op.Descriptor {
	runs := map[string]func(string, bookmarkInput) (any, error){
		"bookmarks.create": store.create,
		"bookmarks.list":   store.list,
		"bookmarks.get":    store.get,
		"bookmarks.update": store.update,
		"bookmarks.delete": store.remove,
	}
	descriptors := bookmarks()
	for index := range descriptors {
		operation := descriptors[index].Operation
		run := runs[operation.ID]
		descriptors[index].Invoke = func(
			ctx context.Context, rc *rastrillo.Ctx, raw json.RawMessage,
		) (any, error) {
			store.invokes.Add(1)
			if err := op.Check(ctx, rc, operation); err != nil {
				return nil, err
			}
			var input bookmarkInput
			if err := op.DecodeInput(raw, &input); err != nil {
				return nil, err
			}
			return run(rc.Actor.Subject, input)
		}
	}
	return descriptors
}

// add stores a row directly, as owner, and returns its ID.
func (store *bookmarkStore) add(owner, url, title string) string {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.last++
	id := strconv.Itoa(store.last)
	store.rows[id] = storedBookmark{
		ID: id, URL: url, Title: title, owner: owner,
	}
	return id
}

func (store *bookmarkStore) row(id string) (storedBookmark, bool) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	row, found := store.rows[id]
	return row, found
}

func (store *bookmarkStore) wrote() {
	store.writes.Add(1)
	if store.afterWrite != nil {
		store.afterWrite()
	}
}

func (store *bookmarkStore) create(
	owner string, input bookmarkInput,
) (any, error) {
	if input.Title.Value == "" {
		return nil, op.Invalid{Fields: map[string]string{
			"Title": "Enter a title.",
		}}
	}
	id := store.add(owner, input.URL.Value, input.Title.Value)
	store.wrote()
	row, _ := store.row(id)
	return row, nil
}

func (store *bookmarkStore) list(
	owner string, _ bookmarkInput,
) (any, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	items := []storedBookmark{}
	for _, row := range store.rows {
		if row.owner == owner {
			items = append(items, row)
		}
	}
	slices.SortFunc(items, func(first, second storedBookmark) int {
		return strings.Compare(first.ID, second.ID)
	})
	return map[string]any{"items": items, "next_after": nil}, nil
}

// owned is owner's row, or NotFound for a row that is someone else's.
func (store *bookmarkStore) owned(
	owner string, input bookmarkInput,
) (storedBookmark, error) {
	row, found := store.row(input.ID.Value)
	if !found || row.owner != owner {
		return storedBookmark{}, op.NotFound{}
	}
	return row, nil
}

func (store *bookmarkStore) get(
	owner string, input bookmarkInput,
) (any, error) {
	return store.owned(owner, input)
}

func (store *bookmarkStore) update(
	owner string, input bookmarkInput,
) (any, error) {
	row, err := store.owned(owner, input)
	if err != nil {
		return nil, err
	}
	if input.Title.Set {
		row.Title = input.Title.Value
	}
	if input.URL.Set {
		row.URL = input.URL.Value
	}
	store.mutex.Lock()
	store.rows[row.ID] = row
	store.mutex.Unlock()
	store.wrote()
	return row, nil
}

func (store *bookmarkStore) remove(
	owner string, input bookmarkInput,
) (any, error) {
	row, err := store.owned(owner, input)
	if err != nil {
		return nil, err
	}
	store.mutex.Lock()
	delete(store.rows, row.ID)
	store.mutex.Unlock()
	store.wrote()
	return nil, nil
}

// withInvoke is descriptors with id's Invoke replaced.
func withInvoke(
	descriptors []op.Descriptor, id string,
	invoke func(context.Context, *rastrillo.Ctx, json.RawMessage) (
		any, error),
) []op.Descriptor {
	for index := range descriptors {
		if descriptors[index].ID == id {
			descriptors[index].Invoke = invoke
		}
	}
	return descriptors
}

// callTool calls a tool through the SDK client and fails the test on a
// protocol error: every answer under test here is a tool result.
func callTool(
	t *testing.T, session *sdk.ClientSession, name string, arguments any,
) *sdk.CallToolResult {
	t.Helper()
	result, err := session.CallTool(t.Context(),
		&sdk.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("tools/call %s: %v, want a tool result", name, err)
	}
	return result
}

// textOf is a result's one text block.
func textOf(t *testing.T, result *sdk.CallToolResult) string {
	t.Helper()
	if len(result.Content) != 1 {
		t.Fatalf("result has %d content blocks, want 1: %+v",
			len(result.Content), result.Content)
	}
	text, isText := result.Content[0].(*sdk.TextContent)
	if !isText {
		t.Fatalf("result block is a %T, want text", result.Content[0])
	}
	return text.Text
}

// structured is a successful result's structuredContent, after checking
// that it is the text block's own value.
func structured(t *testing.T, result *sdk.CallToolResult) map[string]any {
	t.Helper()
	text := textOf(t, result)
	if result.IsError {
		t.Fatalf("result is a tool error: %s", text)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("text block %q is not a JSON object: %v", text, err)
	}
	if !reflect.DeepEqual(result.StructuredContent, any(parsed)) {
		t.Errorf("structuredContent %#v, text block %s",
			result.StructuredContent, text)
	}
	return parsed
}

// refusalOf is a tool error's envelope, the API's {"error": ...}.
func refusalOf(t *testing.T, result *sdk.CallToolResult) api.Error {
	t.Helper()
	text := textOf(t, result)
	if !result.IsError {
		t.Fatalf("result %s is not a tool error", text)
	}
	if result.StructuredContent != nil {
		t.Errorf("a tool error carries structuredContent %#v",
			result.StructuredContent)
	}
	var envelope struct {
		Error api.Error `json:"error"`
	}
	if err := json.Unmarshal([]byte(text), &envelope); err != nil {
		t.Fatalf("tool error %q is not the API's envelope: %v", text, err)
	}
	return envelope.Error
}

// callBody is a raw tools/call, with arguments as the exact bytes sent.
func callBody(name, arguments string) string {
	return `{"jsonrpc":"2.0","id":1,"method":"tools/call",` +
		`"params":{"name":"` + name + `","arguments":` + arguments + `}}`
}

// rawCall is a tools/call answer read with its values kept as bytes.
type rawCall struct {
	Result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StructuredContent json.RawMessage `json:"structuredContent"`
		IsError           bool            `json:"isError"`
	} `json:"result"`
}

func (answer posted) call(t *testing.T) rawCall {
	t.Helper()
	var decoded rawCall
	if err := json.Unmarshal(answer.body, &decoded); err != nil ||
		answer.status != http.StatusOK ||
		len(decoded.Result.Content) != 1 {
		t.Fatalf("answer %d is not one tool result: %v: %s",
			answer.status, err, answer.body)
	}
	return decoded
}

// refusal is the API envelope in a raw tool error.
func (call rawCall) refusal(t *testing.T) api.Error {
	t.Helper()
	var envelope struct {
		Error api.Error `json:"error"`
	}
	text := call.Result.Content[0].Text
	if !call.Result.IsError ||
		json.Unmarshal([]byte(text), &envelope) != nil {
		t.Fatalf("answer %s is not a tool error", text)
	}
	return envelope.Error
}
