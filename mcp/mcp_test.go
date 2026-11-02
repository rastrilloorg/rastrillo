package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/api"
	"amadan.net/rastrillo/rastrillo/op"
)

func TestNewRefuses(t *testing.T) {
	guard := func(
		context.Context, rastrillo.Actor, *op.Descriptor,
		json.RawMessage, any,
	) error {
		return nil
	}
	tests := []struct {
		name        string
		config      func(config *Config)
		change      func(descriptor *op.Descriptor)
		descriptors []op.Descriptor
		want        []string
	}{
		{name: "nil_ctx", config: func(config *Config) { config.Ctx = nil },
			want: []string{"Config.Ctx"}},
		{name: "nil_authorize",
			config: func(config *Config) { config.Authorize = nil },
			want:   []string{"Config.Authorize"}},
		{name: "gate_validate",
			config: func(config *Config) { config.Gate.Verify = nil },
			want:   []string{"Verify is required"}},
		{name: "nil_before", config: func(config *Config) {
			config.Gate.Before = []func(http.Handler) http.Handler{nil}
		}, want: []string{"Before[0] is nil"}},
		{name: "empty_id",
			change: func(descriptor *op.Descriptor) { descriptor.ID = "" },
			want:   []string{"empty ID"}},
		{name: "invalid_access",
			change: func(descriptor *op.Descriptor) { descriptor.Access = 0 },
			want:   []string{"bookmarks.create", "Access"}},
		{name: "input_not_object", change: func(descriptor *op.Descriptor) {
			descriptor.Input = json.RawMessage(`{"type":"array"}`)
		}, want: []string{"bookmarks.create", "Input"}},
		{name: "input_not_json", change: func(descriptor *op.Descriptor) {
			descriptor.Input = json.RawMessage(`{"type":`)
		}, want: []string{"bookmarks.create", "Input"}},
		{name: "input_empty",
			change: func(descriptor *op.Descriptor) { descriptor.Input = nil },
			want:   []string{"bookmarks.create", "Input"}},
		{name: "result_neither", change: func(descriptor *op.Descriptor) {
			descriptor.Result = json.RawMessage(`{"type":"string"}`)
		}, want: []string{"bookmarks.create", "Result"}},
		{name: "result_null_with_more",
			change: func(descriptor *op.Descriptor) {
				descriptor.Result = json.RawMessage(
					`{"type":"null","description":"x"}`)
			}, want: []string{"bookmarks.create", "Result"}},
		{name: "nil_invoke",
			change: func(descriptor *op.Descriptor) { descriptor.Invoke = nil },
			want:   []string{"bookmarks.create", "Invoke"}},
		{name: "destructive_read", change: func(descriptor *op.Descriptor) {
			descriptor.Access = rastrillo.Read
			descriptor.Destructive = true
		}, want: []string{"bookmarks.create", "Destructive"}},
		{name: "fence", change: func(descriptor *op.Descriptor) {
			descriptor.ID = "bookmarks.purge_all"
		}, want: []string{"bookmarks.purge_all", "Destructive"}},
		{name: "tool_name", change: func(descriptor *op.Descriptor) {
			descriptor.ID = "Bookmarks.create"
		}, want: []string{"Bookmarks.create", "Bookmarks_create"}},
		{name: "tool_name_too_long", change: func(descriptor *op.Descriptor) {
			descriptor.ID = "bookmarks." + strings.Repeat("x", 60)
		}, want: []string{"bookmarks.xxx", "^[a-z][a-z0-9_]{0,63}$"}},
		{name: "bad_header_annotation",
			change: func(descriptor *op.Descriptor) {
				descriptor.Input = json.RawMessage(`{"type":"object",` +
					`"properties":{"tags":{"type":"array",` +
					`"x-mcp-header":"Tags"}}}`)
			}, want: []string{"bookmarks.create", "x-mcp-header"}},
		{name: "duplicate_tool_name", descriptors: []op.Descriptor{
			renamed(bookmarks()[0], "a_b.create"),
			renamed(bookmarks()[0], "a.b_create"),
		}, want: []string{"a_b.create", "a.b_create", "a_b_create"}},
		{name: "guard_without_rechecker",
			config: func(config *Config) { config.Guard = guard },
			want:   []string{"Guard", "Rechecker"}},
		{name: "guarded_without_guard", config: func(config *Config) {
			config.Gate.Verify = &fakeRechecker{}
			config.Guarded = true
		}, want: []string{"Guarded", "Guard"}},
		{name: "guarded_without_rechecker", config: func(config *Config) {
			config.Guarded, config.Guard = true, guard
		}, want: []string{"Guarded", "Rechecker"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := testConfig()
			if test.config != nil {
				test.config(&config)
			}
			descriptors := test.descriptors
			if test.change != nil {
				descriptors = bookmarks()
				test.change(&descriptors[0])
			}
			handler, err := New(config, descriptors...)
			if err == nil {
				t.Fatalf("New = %v, nil; want an error naming %q",
					handler, test.want)
			}
			for _, want := range test.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("New's error %q does not name %q", err, want)
				}
			}
		})
	}
}

func renamed(descriptor op.Descriptor, id string) op.Descriptor {
	descriptor.ID = id
	return descriptor
}

func TestNewAcceptsGuardedWithBoth(t *testing.T) {
	config := testConfig()
	config.Gate.Verify = &fakeRechecker{}
	config.Guarded = true
	config.Guard = func(
		context.Context, rastrillo.Actor, *op.Descriptor,
		json.RawMessage, any,
	) error {
		return nil
	}
	if _, err := New(config, bookmarks()...); err != nil {
		t.Fatalf("New with a Rechecker and a Guard: %v", err)
	}
}

func TestToolName(t *testing.T) {
	if got := ToolName("bookmarks.create"); got != "bookmarks_create" {
		t.Errorf(`ToolName("bookmarks.create") = %q`, got)
	}
	if got := ToolName("a.b.c"); got != "a_b_c" {
		t.Errorf(`ToolName("a.b.c") = %q`, got)
	}
}

// TestProtocolVersions pins what go-sdk v1.8.0 answers, so a bump that
// changes the negotiation fails here rather than in a client.
func TestProtocolVersions(t *testing.T) {
	url := serve(t, testConfig(), bookmarks()...)
	tests := []struct {
		name, version, firstMethod, answered string
	}{
		{"legacy_2025_06_18", "2025-06-18", "initialize", "2025-06-18"},
		{"legacy_2025_03_26", "2025-03-26", "initialize", "2025-03-26"},
		{"newest", "", "server/discover", "2026-07-28"},
		// Not refused: the legacy handshake answers its newest version,
		// and the client goes on at that one.
		{"unsupported", "1999-01-01", "initialize", "2025-11-25"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			session, transport := connect(t, url, writerToken, test.version)
			sent := transport.sent()
			if len(sent) == 0 || sent[0] != test.firstMethod {
				t.Errorf("the client sent %q first, want %q",
					sent, test.firstMethod)
			}
			answered := session.InitializeResult().ProtocolVersion
			if answered != test.answered {
				t.Errorf("answered %q, want %q", answered, test.answered)
			}
			listed, err := session.ListTools(t.Context(), nil)
			if err != nil {
				t.Fatalf("tools/list at %s: %v", answered, err)
			}
			if len(listed.Tools) != 5 {
				t.Errorf("listed %v at %s, want 5 tools",
					namesOf(listed.Tools), answered)
			}
		})
	}
	t.Run("unsupported_header", func(t *testing.T) {
		answer := post(t, http.MethodPost, url, writerToken,
			"1999-01-01", listBody)
		const want = "Bad Request: Unsupported protocol version " +
			"(supported versions: 2026-07-28,2025-11-25,2025-06-18," +
			"2025-03-26,2024-11-05)\n"
		if answer.status != http.StatusBadRequest ||
			string(answer.body) != want {
			t.Errorf("answer = %d %q, want 400 %q",
				answer.status, answer.body, want)
		}
	})
}

func TestInitialize(t *testing.T) {
	const appInstructions = "Bookmarks belong to one person."
	tests := []struct{ name, instructions, want string }{
		{"framework_only", "", instructionsText},
		{"with_the_apps", appInstructions,
			instructionsText + "\n\n" + appInstructions},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := testConfig()
			config.Instructions = test.instructions
			url := serve(t, config, bookmarks()...)
			session, _ := connect(t, url, writerToken, "2025-06-18")
			result := session.InitializeResult()
			if result.Instructions != test.want {
				t.Errorf("Instructions = %q, want %q",
					result.Instructions, test.want)
			}
			tools := result.Capabilities.Tools
			if tools == nil || tools.ListChanged {
				t.Errorf("Capabilities.Tools = %+v, want listChanged false",
					tools)
			}
			info := result.ServerInfo
			if info == nil || info.Name != "bookmarks" ||
				info.Version != "v1.2.3" {
				t.Errorf("ServerInfo = %+v, want bookmarks v1.2.3", info)
			}
		})
	}
}

func TestToolShape(t *testing.T) {
	url := serve(t, testConfig(), bookmarks()...)
	session, _ := connect(t, url, writerToken, "2025-06-18")
	listed, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]*sdk.Tool{}
	for _, tool := range listed.Tools {
		tools[tool.Name] = tool
		if tool.Annotations == nil {
			t.Fatalf("%s has no annotations", tool.Name)
		}
		if tool.Annotations.OpenWorldHint != nil {
			t.Errorf("%s sets openWorldHint", tool.Name)
		}
	}
	remove := tools["bookmarks_delete"]
	if remove == nil {
		t.Fatalf("no bookmarks_delete in %v", namesOf(listed.Tools))
	}
	hints := remove.Annotations
	if hints.DestructiveHint == nil || !*hints.DestructiveHint ||
		!hints.IdempotentHint || hints.ReadOnlyHint {
		t.Errorf("bookmarks_delete hints = %+v", hints)
	}
	if remove.OutputSchema != nil {
		t.Errorf("bookmarks_delete OutputSchema = %v, want none",
			remove.OutputSchema)
	}
	wantDescription := "Delete a bookmark. It cannot be brought back." +
		"\n\n" + confirmLine + "Delete a bookmark {id}?"
	if remove.Description != wantDescription {
		t.Errorf("bookmarks_delete description = %q, want %q",
			remove.Description, wantDescription)
	}
	if remove.Title != "Delete a bookmark." {
		t.Errorf("bookmarks_delete title = %q", remove.Title)
	}
	list := tools["bookmarks_list"]
	if list == nil || !list.Annotations.ReadOnlyHint ||
		list.Annotations.DestructiveHint == nil ||
		*list.Annotations.DestructiveHint ||
		!list.Annotations.IdempotentHint {
		t.Errorf("bookmarks_list = %+v, want a read-only, idempotent "+
			"tool that says it is not destructive", list)
	}
	if list.Title != "List your bookmarks, newest first." ||
		list.Description != "List your bookmarks, newest first." {
		t.Errorf("bookmarks_list title %q and description %q, want the "+
			"whole description in each", list.Title, list.Description)
	}
	create := tools["bookmarks_create"]
	if create == nil || create.Annotations.IdempotentHint ||
		*create.Annotations.DestructiveHint {
		t.Errorf("bookmarks_create hints = %+v", create.Annotations)
	}
	sameJSON(t, "create's input", create.InputSchema, bookmarks()[0].Input)
	sameJSON(t, "create's output", create.OutputSchema, bookmarkSchema)
}

func sameJSON(t *testing.T, what string, got any, want any) {
	t.Helper()
	encode := func(value any) string {
		if raw, isRaw := value.(json.RawMessage); isRaw {
			value = string(raw)
		}
		if text, isText := value.(string); isText {
			var decoded any
			if err := json.Unmarshal([]byte(text), &decoded); err != nil {
				t.Fatalf("%s: %v", what, err)
			}
			value = decoded
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		return string(encoded)
	}
	if encode(got) != encode(want) {
		t.Errorf("%s = %s, want %s", what, encode(got), encode(want))
	}
}

func TestEmptyServer(t *testing.T) {
	url := serve(t, testConfig())
	session, _ := connect(t, url, writerToken, "2025-06-18")
	if session.InitializeResult() == nil {
		t.Fatal("no initialize result")
	}
	listed, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if len(listed.Tools) != 0 {
		t.Errorf("listed %v, want none", namesOf(listed.Tools))
	}
	_, err = session.CallTool(t.Context(), &sdk.CallToolParams{Name: "x"})
	var rpc *jsonrpc.Error
	if !errors.As(err, &rpc) || rpc.Code != -32602 ||
		!strings.Contains(rpc.Message, `unknown tool "x"`) {
		t.Errorf("tools/call x = %v, want the unknown-tool protocol error",
			err)
	}
}

func TestNoStreams(t *testing.T) {
	counts := countedConfig(fakeVerifier{})
	url := serve(t, counts.config, bookmarks()...)
	tests := []struct {
		name, method, body string
		status             int
		code               int64
		id                 string
	}{
		{"get", http.MethodGet, "", http.StatusMethodNotAllowed,
			-32600, "null"},
		{"batch", http.MethodPost, "[" + listBody + "]",
			http.StatusBadRequest, -32600, "null"},
		{"batch_after_space", http.MethodPost, " \r\n\t[" + listBody + "]",
			http.StatusBadRequest, -32600, "null"},
		{"listen", http.MethodPost, `{"jsonrpc":"2.0","id":7,` +
			`"method":"subscriptions/listen","params":{}}`,
			http.StatusNotFound, -32601, "7"},
		{"listen_escaped", http.MethodPost, `{"jsonrpc":"2.0","id":"a",` +
			`"method":"subscriptions\/listen","params":{}}`,
			http.StatusNotFound, -32601, `"a"`},
		{"listen_trailing", http.MethodPost, `{"jsonrpc":"2.0","id":8,` +
			`"method":"subscriptions/listen"} {"method":"ping"}`,
			http.StatusNotFound, -32601, "8"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			answer := post(t, test.method, url, writerToken, "", test.body)
			if answer.status != test.status {
				t.Errorf("status %d, want %d: %s",
					answer.status, test.status, answer.body)
			}
			if cache := answer.header.Get("Cache-Control"); cache !=
				"no-store" {
				t.Errorf("Cache-Control %q, want no-store", cache)
			}
			refused := answer.rpcError(t)
			if refused.Version != "2.0" || refused.Error.Code != test.code ||
				refused.Error.Message != msgNoStreams ||
				string(refused.ID) != test.id {
				t.Errorf("answer %s, want code %d, id %s and msgNoStreams",
					answer.body, test.code, test.id)
			}
		})
	}
	if allow := post(t, http.MethodGet, url, writerToken, "", "").
		header.Get("Allow"); allow != "POST" {
		t.Errorf("a GET's Allow is %q, want POST", allow)
	}
	if contexts, authorizations := counts.contexts.Load(),
		counts.authorizations.Load(); contexts != 0 || authorizations != 0 {
		t.Errorf("the refusals reached the server: %d Ctx and %d Authorize"+
			" calls", contexts, authorizations)
	}
	post(t, http.MethodPost, url, writerToken, "2025-06-18", listBody)
	if counts.authorizations.Load() == 0 {
		t.Error("a plain tools/list made no Authorize call, so the counts" +
			" above prove nothing")
	}
}

func TestNoStreamsAfterTheGate(t *testing.T) {
	url := serve(t, testConfig(), bookmarks()...)
	answer := post(t, http.MethodGet, url, "nobody", "", "")
	if answer.status != http.StatusUnauthorized {
		t.Errorf("an unknown token's GET is %d, want the gate's 401: %s",
			answer.status, answer.body)
	}
}

func TestMaxResponseRaised(t *testing.T) {
	config := testConfig()
	config.Gate.MaxResponse = 256
	url := serve(t, config, bookmarks()...)
	if config.Gate.MaxResponse != 256 {
		t.Errorf("the caller's Gate.MaxResponse is now %d",
			config.Gate.MaxResponse)
	}
	answer := post(t, http.MethodPost, url, writerToken, "2025-06-18",
		listBody)
	if answer.status != http.StatusOK || len(answer.body) <= 256 {
		t.Errorf("a %d-byte list answered %d over a 256-byte gate, want "+
			"it raised to fit three results", len(answer.body),
			answer.status)
	}
}

// TestMaxResponseNeverLowered holds the raise to raising: a small
// MaxResult must not cut the gate below its own 2 MiB default.
func TestMaxResponseNeverLowered(t *testing.T) {
	tests := []struct {
		gateMax   int64
		maxResult int
		want      int64
	}{
		{0, 1 << 10, 2 << 20},
		{-1, 1 << 10, 2 << 20},
		{256, 2 << 20, 3*(2<<20) + 64<<10},
		{0, 2 << 20, 3*(2<<20) + 64<<10},
		{10 << 20, 1 << 10, 10 << 20},
	}
	for _, test := range tests {
		got := maxResponse(test.gateMax, test.maxResult)
		if got != test.want {
			t.Errorf("maxResponse(%d, %d) = %d, want %d",
				test.gateMax, test.maxResult, got, test.want)
		}
	}
}

// TestServedBehindALoopbackProxy is an app behind CARLOS or tailscale
// serve: the request arrives on a loopback address with the public
// Host, and the SDK's rebinding check would refuse every one.
func TestServedBehindALoopbackProxy(t *testing.T) {
	url := serve(t, testConfig(), bookmarks()...)
	request, err := http.NewRequestWithContext(t.Context(),
		http.MethodPost, url, strings.NewReader(listBody))
	if err != nil {
		t.Fatal(err)
	}
	request.Host = "app.example.com"
	request.Header.Set("Authorization", "Bearer "+writerToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("MCP-Protocol-Version", "2025-06-18")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		t.Errorf("Host app.example.com over loopback: %d %s, want 200",
			response.StatusCode, body)
	}
}

// TestPanicOutsideTheListIsInternal panics inside tools/call, through
// an app's Redact that fails once. The SDK runs each method on a bare
// goroutine, so without the middleware's recovery the process dies.
func TestPanicOutsideTheListIsInternal(t *testing.T) {
	config := testConfig()
	var redactions atomic.Int64
	config.Gate.Redact = func(text string) string {
		if redactions.Add(1) == 1 {
			panic("redactor bug")
		}
		return text
	}
	url := serve(t, config, bookmarks()...)
	session, _ := connect(t, url, writerToken, "2025-06-18")
	_, err := session.CallTool(t.Context(),
		&sdk.CallToolParams{Name: "bookmarks_get"})
	var rpc *jsonrpc.Error
	if !errors.As(err, &rpc) || rpc.Code != jsonrpc.CodeInternalError {
		t.Errorf("tools/call = %v, want an internal JSON-RPC error", err)
	}
	if got := toolNames(t, url, writerToken); len(got) != 5 {
		t.Errorf("after the panic the list is %v", got)
	}
}

func TestNilCtxIsInternal(t *testing.T) {
	config := testConfig()
	config.Ctx = func(*http.Request) *rastrillo.Ctx { return nil }
	url := serve(t, config, bookmarks()...)
	answer := post(t, http.MethodPost, url, writerToken, "2025-06-18",
		listBody)
	var refused struct{ Error api.Error }
	_ = json.Unmarshal(answer.body, &refused)
	if answer.status != http.StatusInternalServerError ||
		refused.Error.Code != "internal" || refused.Error.Ref == "" {
		t.Errorf("answer %d %s, want a 500 with a ref",
			answer.status, answer.body)
	}
}

// TestCopyIsApproved holds the strings to the approved copy, so a
// transcription slip fails here rather than in front of an agent.
func TestCopyIsApproved(t *testing.T) {
	document, err := os.ReadFile(
		"../docs/superpowers/plans/2026-10-25-mcp-copy.md")
	if err != nil {
		t.Fatal(err)
	}
	approved := map[string]string{}
	section := regexp.MustCompile("(?s)## (\\w+)[^\n]*\n\n```text\n" +
		"(.*?)\n```")
	for _, match := range section.FindAllStringSubmatch(
		string(document), -1) {
		approved[match[1]] = match[2]
	}
	ours := map[string]string{
		"instructionsText": instructionsText,
		"confirmLine":      confirmLine,
		"msgNoStreams":     msgNoStreams,
	}
	for name, got := range ours {
		want, found := approved[name]
		if !found || got != want {
			t.Errorf("%s = %q, approved %q", name, got, want)
		}
	}
}
