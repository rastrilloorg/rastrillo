package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/op"
)

// msgAgentSessionCopy is the API's agent-session message, which both
// doors must answer with.
const msgAgentSessionCopy = "Send an Agent-Session header with every write."

func TestCallCRUD(t *testing.T) {
	store := newBookmarkStore()
	url := serve(t, testConfig(), store.descriptors()...)
	session, _ := connect(t, url, writerToken, "")
	created := structured(t, callTool(t, session, "bookmarks_create",
		map[string]any{"url": "https://example.com", "title": "Example"}))
	want := map[string]any{
		"id": "1", "url": "https://example.com", "title": "Example",
	}
	if !reflect.DeepEqual(created, want) {
		t.Errorf("create answered %v, want %v", created, want)
	}
	got := structured(t, callTool(t, session, "bookmarks_get",
		map[string]any{"id": "1"}))
	if !reflect.DeepEqual(got, want) {
		t.Errorf("get answered %v, want %v", got, want)
	}
	updated := structured(t, callTool(t, session, "bookmarks_update",
		map[string]any{"id": "1", "title": "Renamed"}))
	want["title"] = "Renamed"
	if !reflect.DeepEqual(updated, want) {
		t.Errorf("update answered %v, want %v", updated, want)
	}
	deleted := callTool(t, session, "bookmarks_delete",
		map[string]any{"id": "1"})
	if text := textOf(t, deleted); deleted.IsError || text != "null" ||
		deleted.StructuredContent != nil {
		t.Errorf("delete answered %q, error %v, structured %#v; "+
			"want null and no structured content",
			text, deleted.IsError, deleted.StructuredContent)
	}
	if _, found := store.row("1"); found || store.writes.Load() != 3 {
		t.Errorf("the row is still there after delete, or writes = %d",
			store.writes.Load())
	}
}

func TestCallRefusals(t *testing.T) {
	tests := []struct {
		name      string
		session   bool // the gate requires an agent session
		token     string
		tool      string
		arguments map[string]any
		code      string
		message   string
		field     string
	}{
		{"another_persons_id", false, readerToken, "bookmarks_get",
			map[string]any{"id": "1"}, "not_found", "", ""},
		{"read_only_create", false, readerToken, "bookmarks_create",
			map[string]any{"title": "Mine"}, "forbidden", "", ""},
		{"missing_title", false, writerToken, "bookmarks_create",
			map[string]any{"url": "https://example.com"}, "invalid",
			"", "title"},
		{"write_without_session", true, writerToken, "bookmarks_create",
			map[string]any{"title": "Mine"}, "invalid",
			msgAgentSessionCopy, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newBookmarkStore()
			store.add("u1", "https://example.com", "The writer's")
			config := testConfig()
			config.Gate.RequireAgentSession = test.session
			url := serve(t, config, store.descriptors()...)
			session, _ := connect(t, url, test.token, "")
			refused := refusalOf(t,
				callTool(t, session, test.tool, test.arguments))
			if refused.Code != test.code {
				t.Errorf("code %q, want %q: %+v",
					refused.Code, test.code, refused)
			}
			if test.message != "" && refused.Message != test.message {
				t.Errorf("message %q, want %q",
					refused.Message, test.message)
			}
			if _, named := refused.Fields[test.field]; test.field != "" &&
				!named {
				t.Errorf("fields %v do not name %s",
					refused.Fields, test.field)
			}
			if store.writes.Load() != 0 {
				t.Errorf("a refused call wrote %d rows",
					store.writes.Load())
			}
		})
	}
	t.Run("hidden_tool_is_still_refused_as_forbidden", func(t *testing.T) {
		url := serve(t, testConfig(), newBookmarkStore().descriptors()...)
		if slices.Contains(toolNames(t, url, readerToken),
			"bookmarks_create") {
			t.Error("a read-only token is shown bookmarks_create")
		}
	})
}

func TestCallWithoutArguments(t *testing.T) {
	store := newBookmarkStore()
	store.add("u1", "https://one.example", "One")
	store.add("u1", "https://two.example", "Two")
	store.add("u2", "https://other.example", "Not the writer's")
	url := serve(t, testConfig(), store.descriptors()...)
	session, _ := connect(t, url, writerToken, "")
	listed := structured(t, callTool(t, session, "bookmarks_list", nil))
	if items, _ := listed["items"].([]any); len(items) != 2 {
		t.Errorf("list with no arguments answered %v, want two items",
			listed)
	}
	// The SDK's client sends {} for nil, so these two go raw.
	for name, body := range map[string]string{
		"absent": `{"jsonrpc":"2.0","id":1,"method":"tools/call",` +
			`"params":{"name":"bookmarks_list"}}`,
		"null": callBody("bookmarks_list", "null"),
	} {
		answer := post(t, http.MethodPost, url, writerToken, "2025-06-18",
			body)
		call := answer.call(t)
		var page struct{ Items []storedBookmark }
		err := json.Unmarshal(call.Result.StructuredContent, &page)
		if call.Result.IsError || err != nil || len(page.Items) != 2 {
			t.Errorf("list with %s arguments answered %s",
				name, answer.body)
		}
	}
}

// escape is a JSON escape per UTF-16 unit, a backslash, u and the four
// hex digits given. It is built at run time because an editor or tool
// that meets an escape in source may write the character instead, and
// a test of escapes would then test none.
func escape(units ...string) string {
	var built strings.Builder
	for _, unit := range units {
		built.WriteString(`\` + "u" + unit)
	}
	return built.String()
}

// requireEscape fails the test unless arguments still hold an escape.
func requireEscape(t *testing.T, arguments string) {
	t.Helper()
	if !bytes.Contains([]byte(arguments), []byte{'\\', 'u'}) {
		t.Fatalf("%q holds no escape to test", arguments)
	}
}

// emoji is U+1F600, which the escapes d83d and de00 name.
var emoji = string(rune(0x1F600))

func TestArgumentBytes(t *testing.T) {
	accepted := []struct{ name, arguments, title string }{
		{"escaped_pair",
			`{"title":"` + escape("d83d", "de00") + `"}`, emoji},
		{"escaped_backslash",
			`{"title":"\\` + `ud83d"}`, `\` + "ud83d"},
	}
	for _, test := range accepted {
		t.Run(test.name, func(t *testing.T) {
			requireEscape(t, test.arguments)
			store := newBookmarkStore()
			url := serve(t, testConfig(), store.descriptors()...)
			answer := post(t, http.MethodPost, url, writerToken,
				"2025-06-18",
				callBody("bookmarks_create", test.arguments))
			if call := answer.call(t); call.Result.IsError {
				t.Fatalf("%s refused: %s", test.arguments, answer.body)
			}
			row, found := store.row("1")
			if !found || row.Title != test.title {
				t.Errorf("stored %q, want %q", row.Title, test.title)
			}
		})
	}
	refused := []struct {
		name, arguments string
		escapes         bool
	}{
		{"lone_high", `{"title":"` + escape("d83d") + `"}`, true},
		{"lone_low", `{"title":"` + escape("de00") + `x"}`, true},
		{"invalid_byte", "{\"title\":\"a\xffb\"}", false},
	}
	for _, test := range refused {
		t.Run(test.name, func(t *testing.T) {
			if test.escapes {
				requireEscape(t, test.arguments)
			}
			store := newBookmarkStore()
			url := serve(t, testConfig(), store.descriptors()...)
			answer := post(t, http.MethodPost, url, writerToken,
				"2025-06-18",
				callBody("bookmarks_create", test.arguments))
			refusal := answer.call(t).refusal(t)
			if refusal.Code != "invalid" ||
				refusal.Message != msgArgumentBytes {
				t.Errorf("%q answered %+v, want invalid with %q",
					test.arguments, refusal, msgArgumentBytes)
			}
			if store.invokes.Load() != 0 {
				t.Error("Invoke ran on arguments that were refused")
			}
		})
	}
}

// TestArgumentScanner holds the byte check to JSON's string escapes,
// where a backslash can itself be escaped.
func TestArgumentScanner(t *testing.T) {
	pair, high, low := escape("d83d", "de00"), escape("d83d"),
		escape("de00")
	tests := []struct {
		arguments string
		valid     bool
		escapes   bool
	}{
		{`{"a":"` + pair + `"}`, true, true},
		{`{"a":"` + escape("D83D", "DE00") + `"}`, true, true},
		{`{"a":"\\` + `ud83d"}`, true, true},
		{`{"a":"\\` + pair + `"}`, true, true},
		{`{"a":"\"","b":"` + escape("00e9") + `\n"}`, true, true},
		{`{"a":1,"b":["` + pair + `", "x"]}`, true, true},
		{`{"a":"` + emoji + string(rune(0xE9)) + `"}`, true, false},
		{`{"a":"` + high + `"}`, false, true},
		{`{"a":"` + high + `x"}`, false, true},
		{`{"a":"` + escape("d83d", "0041") + `"}`, false, true},
		{`{"a":"` + escape("d83d", "d83d", "de00") + `"}`, false, true},
		{`{"a":"` + low + `"}`, false, true},
		{`{"a":"` + escape("DE00") + `"}`, false, true},
		{`{"a":"\\` + high + `"}`, false, true},
		{`{"a":"\"` + high + `"}`, false, true},
		{`{"` + high + `":1}`, false, true},
		{`{"a":"` + high, false, true},
		{"{\"a\":\"\xff\"}", false, false},
		{"{\"a\":\"\xed\xa0\xbd\"}", false, false},
	}
	for _, test := range tests {
		if test.escapes {
			requireEscape(t, test.arguments)
		}
		if got := argumentBytesValid([]byte(test.arguments)); got !=
			test.valid {
			t.Errorf("argumentBytesValid(%q) = %v, want %v",
				test.arguments, got, test.valid)
		}
	}
}

func TestExactNumbers(t *testing.T) {
	var received atomic.Value
	descriptors := withInvoke(bookmarks(), "bookmarks.get", func(
		_ context.Context, _ *rastrillo.Ctx, raw json.RawMessage,
	) (any, error) {
		received.Store(string(raw))
		return map[string]any{"id": json.Number("9007199254740993")}, nil
	})
	url := serve(t, testConfig(), descriptors...)
	sent := `{"n":9007199254740993}`
	answer := post(t, http.MethodPost, url, writerToken, "2025-06-18",
		callBody("bookmarks_get", sent))
	call := answer.call(t)
	want := `{"id":9007199254740993}`
	if got := string(call.Result.StructuredContent); got != want {
		t.Errorf("structuredContent %s, want %s", got, want)
	}
	if got := call.Result.Content[0].Text; got != want {
		t.Errorf("text block %s, want %s", got, want)
	}
	if got, _ := received.Load().(string); got != sent {
		t.Errorf("Invoke received %s, want %s", got, sent)
	}
}

// TestMalformedNumbers: a json.Number is written as its own text, so a
// bad one would put invalid JSON on the wire unless it is refused.
func TestMalformedNumbers(t *testing.T) {
	for _, number := range []string{"12abc", "", " 12", `"12"`, "01", "{}"} {
		descriptors := withInvoke(bookmarks(), "bookmarks.get", func(
			context.Context, *rastrillo.Ctx, json.RawMessage,
		) (any, error) {
			return map[string]any{"id": json.Number(number)}, nil
		})
		url := serve(t, testConfig(), descriptors...)
		session, _ := connect(t, url, writerToken, "")
		refused := refusalOf(t, callTool(t, session, "bookmarks_get", nil))
		if refused.Code != "internal" || refused.Ref == "" {
			t.Errorf("json.Number(%q) answered %+v, want internal",
				number, refused)
		}
	}
}

// lineSeparator is U+2028, which the SDK's encoding/json escapes.
var lineSeparator = string(rune(0x2028))

func resultOfText(text string) func(
	context.Context, *rastrillo.Ctx, json.RawMessage) (any, error) {
	return func(
		context.Context, *rastrillo.Ctx, json.RawMessage,
	) (any, error) {
		return map[string]any{"q": text}, nil
	}
}

func TestResultTooLarge(t *testing.T) {
	// The second is 208 bytes encoded, but 1208 once the SDK escapes
	// each < in it as six bytes.
	for name, text := range map[string]string{
		"long":           strings.Repeat("a", 2048),
		"escaped_by_sdk": strings.Repeat("<", 200),
	} {
		t.Run(name, func(t *testing.T) {
			config := testConfig()
			config.MaxResult = 1024
			url := serve(t, config, withInvoke(bookmarks(),
				"bookmarks.list", resultOfText(text))...)
			session, _ := connect(t, url, writerToken, "")
			refused := refusalOf(t,
				callTool(t, session, "bookmarks_list", nil))
			if refused.Code != "response_too_large" ||
				refused.Message != msgResultTooLarge {
				t.Errorf("answered %+v, want response_too_large with %q",
					refused, msgResultTooLarge)
			}
		})
	}
}

// TestResultJustUnderCap sends results whose size, counted as the SDK
// will escape them, is MaxResult - 64 bytes: " doubles again in the
// text block, and the SDK writes < and U+2028 as six bytes each. The
// agent's text block holds < and U+2028 as they are, as the API's does.
func TestResultJustUnderCap(t *testing.T) {
	// {"q":"..."} is eight bytes around the characters.
	const budget = defaultMaxResult - 64 - 8
	for _, test := range []struct {
		name, character, encoded string
		counted                  int
	}{
		{"quotes", `"`, `\"`, 2},
		{"angle_brackets", "<", "<", 6},
		{"line_separators", lineSeparator, lineSeparator, 6},
	} {
		t.Run(test.name, func(t *testing.T) {
			count := budget / test.counted
			padding := strings.Repeat("a", budget-count*test.counted)
			text := strings.Repeat(test.character, count) + padding
			url := serve(t, testConfig(), withInvoke(bookmarks(),
				"bookmarks.list", resultOfText(text))...)
			session, _ := connect(t, url, writerToken, "")
			result := callTool(t, session, "bookmarks_list", nil)
			want := `{"q":"` + strings.Repeat(test.encoded, count) +
				padding + `"}`
			if got := textOf(t, result); got != want {
				t.Fatalf("text block is %d bytes, want %d: %.200s",
					len(got), len(want), got)
			}
			if got, _ := structured(t, result)["q"].(string); got != text {
				t.Errorf("structuredContent q is %d bytes, want %d",
					len(got), len(text))
			}
		})
	}
}

// panickingResult panics when encoded.
type panickingResult struct{}

func (panickingResult) MarshalJSON() ([]byte, error) {
	panic("result encoder bug")
}

func TestCallPanics(t *testing.T) {
	store := newBookmarkStore()
	config := testConfig()
	logged := &lockedBuffer{}
	config.Gate.Logger = slog.New(slog.NewTextHandler(logged, nil))
	config.Authorize = func(
		_ context.Context, _ rastrillo.Actor, operation rastrillo.Operation,
	) error {
		if operation.ID == "bookmarks.get" {
			panic("policy bug")
		}
		return nil
	}
	descriptors := withInvoke(store.descriptors(), "bookmarks.update",
		func(context.Context, *rastrillo.Ctx, json.RawMessage) (any, error) {
			panic("operation bug")
		})
	descriptors = withInvoke(descriptors, "bookmarks.list",
		func(context.Context, *rastrillo.Ctx, json.RawMessage) (any, error) {
			return panickingResult{}, nil
		})
	url := serve(t, config, descriptors...)
	session, _ := connect(t, url, writerToken, "")
	for _, test := range []struct{ tool, cause string }{
		{"bookmarks_get", "policy bug"},
		{"bookmarks_update", "operation bug"},
		{"bookmarks_list", "result encoder bug"},
	} {
		refused := refusalOf(t, callTool(t, session, test.tool,
			map[string]any{"id": "1"}))
		if refused.Code != "internal" || refused.Ref == "" {
			t.Errorf("%s answered %+v, want internal with a ref",
				test.tool, refused)
		}
		if line := logged.String(); !strings.Contains(line, refused.Ref) ||
			!strings.Contains(line, test.cause) {
			t.Errorf("no log line names %q under ref %s",
				test.cause, refused.Ref)
		}
	}
	structured(t, callTool(t, session, "bookmarks_create",
		map[string]any{"title": "After the panics"}))
}

// onceResult encodes once and panics from its second encoding on, as
// the SDK's own marshaling would make it if the result reached it.
type onceResult struct{ calls *atomic.Int64 }

func (result onceResult) MarshalJSON() ([]byte, error) {
	if result.calls.Add(1) > 1 {
		panic("encoded twice")
	}
	return []byte(`{"id":"1"}`), nil
}

func TestResultEncodedOnce(t *testing.T) {
	calls := &atomic.Int64{}
	descriptors := withInvoke(bookmarks(), "bookmarks.get", func(
		context.Context, *rastrillo.Ctx, json.RawMessage,
	) (any, error) {
		return onceResult{calls: calls}, nil
	})
	url := serve(t, testConfig(), descriptors...)
	session, _ := connect(t, url, writerToken, "")
	got := structured(t, callTool(t, session, "bookmarks_get", nil))
	if got["id"] != "1" || calls.Load() != 1 {
		t.Errorf("answered %v after %d encodings, want id 1 after one",
			got, calls.Load())
	}
}

// guardSeen is what a test's Guard was handed.
type guardSeen struct {
	mutex      sync.Mutex
	calls      int
	descriptor string
	input      string
	result     any
}

func TestCallRecheck(t *testing.T) {
	const sent = `{"title":"Kept","url":"https://example.com"}`
	type setup func(*bookmarkStore, *fakeRechecker, *Config, *guardSeen)
	tests := []struct {
		name   string
		setup  setup
		status int
	}{
		{"revoked_after_invoke", func(store *bookmarkStore,
			checker *fakeRechecker, _ *Config, _ *guardSeen) {
			store.afterWrite = func() { checker.revoked.Store(true) }
		}, http.StatusUnauthorized},
		{"narrowed_after_invoke", func(store *bookmarkStore,
			checker *fakeRechecker, _ *Config, _ *guardSeen) {
			store.afterWrite = func() { checker.narrowed.Store(true) }
		}, http.StatusForbidden},
		{"guard_refuses", func(_ *bookmarkStore, _ *fakeRechecker,
			config *Config, _ *guardSeen) {
			config.Guard = func(context.Context, rastrillo.Actor,
				*op.Descriptor, json.RawMessage, any) error {
				return op.Forbidden{Reason: "no longer theirs"}
			}
		}, http.StatusForbidden},
		{"guard_sees_raw_and_result", recordingGuard, http.StatusOK},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, checker := newBookmarkStore(), &fakeRechecker{}
			config := countedConfig(checker).config
			seen := &guardSeen{}
			test.setup(store, checker, &config, seen)
			descriptors := scribbling(store.descriptors())
			url := serve(t, config, descriptors...)
			answer := post(t, http.MethodPost, url, writerToken,
				"2025-06-18", callBody("bookmarks_create", sent))
			if answer.status != test.status {
				t.Errorf("answer %d %s, want %d",
					answer.status, answer.body, test.status)
			}
			if store.invokes.Load() != 1 || store.writes.Load() != 1 {
				t.Errorf("Invoke ran %d times and wrote %d rows, "+
					"want once each",
					store.invokes.Load(), store.writes.Load())
			}
			if test.status == http.StatusOK {
				seen.check(t, sent, store)
			}
		})
	}
}

// scribbling has create overwrite its input once it has run, so a test
// can tell whether the guard sees the arguments as they were sent.
func scribbling(descriptors []op.Descriptor) []op.Descriptor {
	for index := range descriptors {
		if descriptors[index].ID != "bookmarks.create" {
			continue
		}
		invoke := descriptors[index].Invoke
		descriptors[index].Invoke = func(
			ctx context.Context, rc *rastrillo.Ctx, raw json.RawMessage,
		) (any, error) {
			result, err := invoke(ctx, rc, raw)
			for position := range raw {
				raw[position] = ' '
			}
			return result, err
		}
	}
	return descriptors
}

func recordingGuard(
	_ *bookmarkStore, _ *fakeRechecker, config *Config, seen *guardSeen,
) {
	config.Guard = func(
		_ context.Context, actor rastrillo.Actor,
		descriptor *op.Descriptor, input json.RawMessage, result any,
	) error {
		seen.mutex.Lock()
		defer seen.mutex.Unlock()
		seen.calls++
		seen.descriptor = descriptor.ID
		seen.input = string(input)
		seen.result = result
		if actor.Credential != "tok-writer" {
			return op.Forbidden{Reason: "the guard saw another actor"}
		}
		return nil
	}
}

func (seen *guardSeen) check(
	t *testing.T, sent string, store *bookmarkStore,
) {
	t.Helper()
	seen.mutex.Lock()
	defer seen.mutex.Unlock()
	row, _ := store.row("1")
	if seen.calls == 0 || seen.descriptor != "bookmarks.create" ||
		seen.input != sent || !reflect.DeepEqual(seen.result, row) {
		t.Errorf("the guard ran %d times for %s with %s and %#v; "+
			"want bookmarks.create with %s and %#v", seen.calls,
			seen.descriptor, seen.input, seen.result, sent, row)
	}
}

// TestCallLogsOneLine: the tree's "api: request" line, for a tool, so a
// revoked token's last call can still be traced.
func TestCallLogsOneLine(t *testing.T) {
	config := testConfig()
	logged := &lockedBuffer{}
	config.Gate.Logger = slog.New(slog.NewTextHandler(logged, nil))
	config.Gate.Redact = func(text string) string {
		return strings.ReplaceAll(text, "tok-writer", "tok-[redacted]")
	}
	url := serve(t, config, newBookmarkStore().descriptors()...)
	session, _ := connect(t, url, writerToken, "")
	structured(t, callTool(t, session, "bookmarks_list", nil))
	var lines []string
	for line := range strings.Lines(logged.String()) {
		if strings.Contains(line, `msg="mcp: call"`) {
			lines = append(lines, line)
		}
	}
	if len(lines) != 1 {
		t.Fatalf("%d mcp: call lines, want 1: %s",
			len(lines), logged.String())
	}
	if !strings.Contains(lines[0], "tool=bookmarks_list") ||
		!strings.Contains(lines[0], "tok-[redacted]") ||
		strings.Contains(lines[0], "tok-writer") {
		t.Errorf("the line does not name the tool and the redacted "+
			"actor: %s", lines[0])
	}
}
