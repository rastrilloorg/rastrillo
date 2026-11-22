package api

import (
	"context"
	encodingjson "encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/op"
)

type noteInput struct {
	Title string `json:"title"`
}

type note struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

type noteCount struct {
	Count int `json:"count"`
}

type noteSearch struct {
	Search op.Opt[string] `json:"search"`
	Limit  op.Opt[int]    `json:"limit"`
	Pinned op.Opt[bool]   `json:"pinned"`
}

// noteSearchEcho is noteSearch as a result, which may not hold an Opt.
type noteSearchEcho struct {
	Search string `json:"search"`
	Limit  int    `json:"limit"`
	Pinned bool   `json:"pinned"`
}

type noteID struct {
	ID int64 `json:"id"`
}

// msgNoTitle is the fixture operation's own refusal, so a test can tell
// that the operation ran and refused rather than the door.
const msgNoTitle = "Give the note a title."

// noteOperations are the fixtures, declared as an app declares them.
type noteOperations struct {
	create *op.Named[noteInput, note]
	count  *op.Named[op.None, noteCount]
	list   *op.Named[noteSearch, noteSearchEcho]
	delete *op.Named[noteID, op.None]
}

func notes() noteOperations {
	return noteOperations{
		create: op.Define("notes.create", op.Spec{
			Description: "Create a note.", Access: rastrillo.Write,
		}, createNote),
		count: op.Define("notes.count", op.Spec{
			Description: "Count the notes.", Access: rastrillo.Read,
		}, countNotes),
		list: op.Define("notes.list", op.Spec{
			Description: "List the notes.", Access: rastrillo.Read,
			InQuery: true,
		}, listNotes),
		delete: op.Define("notes.delete", op.Spec{
			Description: "Delete a note.", Access: rastrillo.Write,
			Destructive: true,
		}, deleteNote),
	}
}

func (operations noteOperations) descriptors() []op.Descriptor {
	return []op.Descriptor{
		operations.create.Descriptor, operations.count.Descriptor,
		operations.list.Descriptor, operations.delete.Descriptor,
	}
}

func createNote(
	_ context.Context, _ *rastrillo.Ctx, input noteInput,
) (note, error) {
	if input.Title == "" {
		return note{}, op.Invalid{Fields: map[string]string{
			"Title": msgNoTitle,
		}}
	}
	return note{ID: 1, Title: input.Title}, nil
}

func countNotes(
	context.Context, *rastrillo.Ctx, op.None,
) (noteCount, error) {
	return noteCount{Count: 2}, nil
}

func listNotes(
	_ context.Context, _ *rastrillo.Ctx, input noteSearch,
) (noteSearchEcho, error) {
	return noteSearchEcho{
		Search: input.Search.Value, Limit: input.Limit.Value,
		Pinned: input.Pinned.Value,
	}, nil
}

func deleteNote(context.Context, *rastrillo.Ctx, noteID) (op.None, error) {
	return op.None{}, nil
}

// handPurge is notes.purge written by hand, human-only, with an Invoke
// that counts its calls and never calls op.Check: what the door must
// still refuse for a caller the operation is not for.
func handPurge(calls *int) op.Descriptor {
	return op.Descriptor{
		Operation: rastrillo.Operation{
			ID: "notes.purge", Resource: "notes", Access: rastrillo.Write,
			HumanOnly: "Purging deletes every note for good.",
		},
		Exposed:     true,
		Description: "Delete every note for good.",
		Destructive: true,
		Input:       []byte(`{"type":"object"}`),
		Result:      []byte(`{"type":"null"}`),
		Invoke: func(
			context.Context, *rastrillo.Ctx, encodingjson.RawMessage,
		) (any, error) {
			*calls++
			return nil, nil
		},
	}
}

func notesTree(t *testing.T) http.Handler {
	t.Helper()
	return mustNew(t, config(), Operations(notes().descriptors()...)...)
}

func assertBody(
	t *testing.T, recorder *httptest.ResponseRecorder,
	status int, body string,
) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("answered %d, want %d; body %s",
			recorder.Code, status, recorder.Body)
	}
	if got := strings.TrimSpace(recorder.Body.String()); got != body {
		t.Fatalf("body = %s, want %s", got, body)
	}
}

func TestOperationsRoutesEachOperationByItsName(t *testing.T) {
	operations := notes()
	routes := Operations(
		operations.create.Descriptor, operations.list.Descriptor)
	if len(routes) != 2 {
		t.Fatalf("Operations = %v, want 2 routes", routes)
	}
	want := []string{
		"POST /api/v1/notes/create notes.create",
		"GET /api/v1/notes/list notes.list",
	}
	for index, route := range routes {
		got := route.Method + " " + route.Pattern + " " + route.Op.ID
		if got != want[index] {
			t.Errorf("route %d = %s, want %s", index, got, want[index])
		}
	}
}

func TestAPostedOperationAnswersItsResult(t *testing.T) {
	recorder := serve(notesTree(t), http.MethodPost,
		"/api/v1/notes/create", "person-write", `{"title":"Hello"}`)
	assertBody(t, recorder, http.StatusOK, `{"id":1,"title":"Hello"}`)
}

// A read with a body is served by POST, and the declaration's Read
// lets a read-only token through, where a hand-built POST route must
// be Write (TestNewRefuses, "POST with Read").
func TestAReadServedByPostTakesAReadOnlyToken(t *testing.T) {
	tree := notesTree(t)
	for _, token := range []string{"person-read", "agent-read"} {
		t.Run(token, func(t *testing.T) {
			recorder := serve(tree, http.MethodPost,
				"/api/v1/notes/count", token, `{}`)
			assertBody(t, recorder, http.StatusOK, `{"count":2}`)
		})
	}
}

func TestAnOperationWithNoResultAnswers204(t *testing.T) {
	recorder := serve(notesTree(t), http.MethodPost,
		"/api/v1/notes/delete", "person-write", `{"id":1}`)
	assertBody(t, recorder, http.StatusNoContent, "")
}

// No body and no Content-Type is {}, as MCP treats absent arguments, so
// the operation itself decides whether that is enough.
func TestAnEmptyPostIsAnEmptyObject(t *testing.T) {
	tree := notesTree(t)
	t.Run("all optional", func(t *testing.T) {
		recorder := serve(tree, http.MethodPost,
			"/api/v1/notes/count", "person-read", "")
		assertBody(t, recorder, http.StatusOK, `{"count":2}`)
	})
	t.Run("a required title", func(t *testing.T) {
		recorder := serve(tree, http.MethodPost,
			"/api/v1/notes/create", "person-write", "")
		assertCode(t, recorder, http.StatusUnprocessableEntity, "invalid")
		fields := decodeError(t, recorder).Fields
		want := map[string]string{"title": msgNoTitle}
		if !maps.Equal(fields, want) {
			t.Fatalf("fields = %v, want %v", fields, want)
		}
	})
}

func TestABodyWithoutJSONContentTypeIs415(t *testing.T) {
	recorder := serve(notesTree(t), http.MethodPost,
		"/api/v1/notes/create", "person-write", `{"title":"Hello"}`,
		"Content-Type", "text/plain")
	assertCode(t, recorder, http.StatusUnsupportedMediaType,
		"unsupported_media_type")
}

// The API must answer a malformed body exactly as MCP does, which is
// Refusal of Invoke's error: one classifier, op.DecodeInput, for both.
func TestMalformedBodiesAreClassifiedByDecodeInput(t *testing.T) {
	create := notes().create
	tree := mustNew(t, config(), Operations(create.Descriptor)...)
	bodies := []string{
		`[]`, `{"title":"a","title":"b"}`, `{"title":null}`,
		`{"colour":1}`,
	}
	for _, body := range bodies {
		t.Run(body, func(t *testing.T) {
			caller := &rastrillo.Ctx{Actor: personWrite, Authorize: allow}
			_, err := create.Descriptor.Invoke(
				context.Background(), caller, []byte(body))
			if err == nil {
				t.Fatalf("Invoke(%s) returned no error", body)
			}
			request := httptest.NewRequest(http.MethodPost,
				"/api/v1/notes/create", nil)
			status, want := Refusal(request, err)

			recorder := serve(tree, http.MethodPost,
				"/api/v1/notes/create", "person-write", body)
			if recorder.Code != status {
				t.Fatalf("answered %d, want %d; body %s",
					recorder.Code, status, recorder.Body)
			}
			// maps.Equal, because an absent fields member decodes as
			// nil where Refusal builds an empty map.
			got := decodeError(t, recorder)
			if got.Code != want.Code || got.Message != want.Message ||
				!maps.Equal(got.Fields, want.Fields) {
				t.Fatalf("answered %+v, want %+v", got, want)
			}
		})
	}
}

func TestQueryArgumentsAreRefusedByTheBodyRules(t *testing.T) {
	tree := notesTree(t)
	list := func(query string) *httptest.ResponseRecorder {
		return serve(tree, http.MethodGet,
			"/api/v1/notes/list?"+query, "agent-read", "")
	}
	t.Run("typed values", func(t *testing.T) {
		assertBody(t, list("search=hi&limit=5&pinned=true"),
			http.StatusOK, `{"search":"hi","limit":5,"pinned":true}`)
	})
	refused := []struct{ query, key string }{
		{"limit=five", "limit"},
		{"limit=1&limit=2", "limit"},
		{"pinned=yes", "pinned"},
		{"colour=red", "colour"},
	}
	for _, each := range refused {
		t.Run(each.query, func(t *testing.T) {
			recorder := list(each.query)
			assertCode(t, recorder,
				http.StatusUnprocessableEntity, "invalid")
			fields := decodeError(t, recorder).Fields
			if _, named := fields[each.key]; !named || len(fields) != 1 {
				t.Fatalf("fields = %v, want only %q", fields, each.key)
			}
		})
	}
	// A body with invalid UTF-8 is refused whole, so a query is too,
	// rather than the operation seeing a string the client never sent.
	t.Run("invalid UTF-8", func(t *testing.T) {
		recorder := list("search=caf%FF")
		assertCode(t, recorder, http.StatusUnprocessableEntity, "invalid")
		if fields := decodeError(t, recorder).Fields; len(fields) != 0 {
			t.Fatalf("fields = %v, want none", fields)
		}
	})
}

// A pair url.Values would drop must not reach the operation as a call
// without that argument.
func TestAnUnreadableQueryIsRefusedBeforeInvoke(t *testing.T) {
	var calls int
	counted := notes().list.Descriptor
	invoke := counted.Invoke
	counted.Invoke = func(
		ctx context.Context, rc *rastrillo.Ctx,
		arguments encodingjson.RawMessage,
	) (any, error) {
		calls++
		return invoke(ctx, rc, arguments)
	}
	tree := mustNew(t, config(), Operations(counted)...)
	for _, query := range []string{"search=100%", "search=a;b"} {
		t.Run(query, func(t *testing.T) {
			recorder := serve(tree, http.MethodGet,
				"/api/v1/notes/list?"+query, "agent-read", "")
			assertCode(t, recorder,
				http.StatusUnprocessableEntity, "invalid")
			refused := decodeError(t, recorder)
			if refused.Message != msgBadQuery || len(refused.Fields) != 0 {
				t.Fatalf("answered %+v, want %q and no fields",
					refused, msgBadQuery)
			}
		})
	}
	if calls != 0 {
		t.Fatalf("Invoke ran %d times, want 0", calls)
	}
}

func TestQueryArgumentsBuildOneObjectInKeyOrder(t *testing.T) {
	schema := notes().list.Descriptor.Input
	query := url.Values{
		"search": {`say "hi"`, "5"},
		"limit":  {"5", "-1.5e3", "05"},
		"pinned": {"true", "1"},
		"colour": {"7"},
	}
	got := string(queryArguments(query, schema))
	want := `{"colour":"7",` +
		`"limit":5,"limit":-1.5e3,"limit":"05",` +
		`"pinned":true,"pinned":"1",` +
		`"search":"say \"hi\"","search":"5"}`
	if got != want {
		t.Fatalf("queryArguments = %s, want %s", got, want)
	}
}

// The ruling: whatever a descriptor's Invoke does, the door runs
// op.Check first, so a hand-written Invoke that skips it still never
// runs for a caller the operation refuses.
func TestTheDoorChecksBeforeAnUncheckedInvoke(t *testing.T) {
	var calls int
	gated := config()
	gated.RequireAgentSession = true
	tree := mustNew(t, gated, Operations(handPurge(&calls))...)
	for _, token := range []string{"agent-write", "person-write"} {
		t.Run(token, func(t *testing.T) {
			recorder := serve(tree, http.MethodPost,
				"/api/v1/notes/purge", token, `{}`)
			assertCode(t, recorder, http.StatusForbidden, "human_only")
		})
	}
	t.Run("a gate Admit can't see", func(t *testing.T) {
		unmarked := handPurge(&calls)
		unmarked.HumanOnly = ""
		refusing := config()
		refusing.Authorize = func(
			_ context.Context, _ rastrillo.Actor,
			operation rastrillo.Operation,
		) error {
			if operation.ID == "notes.purge" {
				return op.Forbidden{Reason: "purging is for people"}
			}
			return nil
		}
		tree := mustNew(t, refusing, Operations(unmarked)...)
		recorder := serve(tree, http.MethodPost,
			"/api/v1/notes/purge", "agent-write", `{}`)
		assertCode(t, recorder, http.StatusForbidden, "forbidden")
	})
	if calls != 0 {
		t.Fatalf("notes.purge's Invoke ran %d times, want 0", calls)
	}
}
