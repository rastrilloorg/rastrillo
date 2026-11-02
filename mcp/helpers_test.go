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
	"slices"
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
// read-only once narrowed is set, and counts its calls.
type fakeRechecker struct {
	fakeVerifier
	narrowed atomic.Bool
	rechecks atomic.Int64
}

func (checker *fakeRechecker) Recheck(
	_ context.Context, _ *http.Request, was rastrillo.Actor,
) (rastrillo.Actor, error) {
	checker.rechecks.Add(1)
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
