package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/api"
	"amadan.net/rastrillo/rastrillo/op"
)

var (
	everyTool = []string{
		"bookmarks_create", "bookmarks_delete", "bookmarks_get",
		"bookmarks_list", "bookmarks_update",
	}
	readTools = []string{"bookmarks_get", "bookmarks_list"}
)

func TestListPerCaller(t *testing.T) {
	refusing := func(id string) func(
		context.Context, rastrillo.Actor, rastrillo.Operation) error {
		return func(
			_ context.Context, _ rastrillo.Actor, operation rastrillo.Operation,
		) error {
			if operation.ID == id {
				return op.Forbidden{Reason: "test policy"}
			}
			return nil
		}
	}
	hidden := func(id string) func(
		context.Context, rastrillo.Actor, rastrillo.Operation) error {
		return func(
			_ context.Context, _ rastrillo.Actor, operation rastrillo.Operation,
		) error {
			if operation.ID == id {
				return op.NotFound{}
			}
			return nil
		}
	}
	// A connection service's grant whose scopes leave out writing: the
	// app's policy, not the framework, reads what a scope means.
	scoped := func(
		_ context.Context, actor rastrillo.Actor,
		operation rastrillo.Operation,
	) error {
		writes := operation.Access == rastrillo.Write
		if writes && actor.Grant != nil &&
			!slices.Contains(actor.Grant.Scopes, "bookmarks:write") {
			return op.Forbidden{Reason: "no bookmarks:write scope"}
		}
		return nil
	}
	tests := []struct {
		name      string
		authorize func(context.Context, rastrillo.Actor,
			rastrillo.Operation) error
		token string
		want  []string
	}{
		{"writer", nil, writerToken, everyTool},
		{"read_only", nil, readerToken, readTools},
		{"policy_refuses_update", refusing("bookmarks.update"),
			writerToken, []string{
				"bookmarks_create", "bookmarks_delete", "bookmarks_get",
				"bookmarks_list",
			}},
		{"policy_not_found", hidden("bookmarks.get"), readerToken,
			[]string{"bookmarks_list"}},
		{"grant_without_write_scope", scoped, grantedToken, readTools},
		{"grant_policy_for_a_writer", scoped, writerToken, everyTool},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := testConfig()
			if test.authorize != nil {
				config.Authorize = test.authorize
			}
			url := serve(t, config, bookmarks()...)
			got := toolNames(t, url, test.token)
			if !slices.Equal(got, test.want) {
				t.Errorf("%s lists %v, want %v", test.token, got, test.want)
			}
		})
	}
	t.Run("private", func(t *testing.T) {
		url := serve(t, testConfig(), bookmarks()...)
		session, _ := connect(t, url, writerToken, "2025-06-18")
		listed, err := session.ListTools(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if listed.CacheScope != "private" {
			t.Errorf("CacheScope = %q, want private", listed.CacheScope)
		}
	})
}

// TestListPerCallerConcurrent has a writer and a read-only token list at
// once through one Ctx that the factory hands every request, which is
// where one caller's actor would leak into the other's list.
func TestListPerCallerConcurrent(t *testing.T) {
	url := serve(t, testConfig(), bookmarks()...)
	var group sync.WaitGroup
	for index := range 20 {
		token, want := writerToken, everyTool
		if index%2 == 1 {
			token, want = readerToken, readTools
		}
		group.Go(func() {
			session, _ := connect(t, url, token, "2025-06-18")
			listed, err := session.ListTools(t.Context(), nil)
			if err != nil {
				t.Errorf("tools/list as %s: %v", token, err)
				return
			}
			if got := namesOf(listed.Tools); !slices.Equal(got, want) {
				t.Errorf("%s lists %v, want %v", token, got, want)
			}
		})
	}
	group.Wait()
}

// TestListFrozenOnRecheck narrows the token between building the list
// and sending it. Recomputing the list then would quietly drop the
// write tools and pass; the frozen list refuses instead.
func TestListFrozenOnRecheck(t *testing.T) {
	checker := &fakeRechecker{}
	config := countedConfig(checker).config
	url := serve(t, config, bookmarks()...)
	checker.narrowed.Store(true)
	answer := post(t, http.MethodPost, url, writerToken, "2025-06-18",
		listBody)
	var refused struct{ Error api.Error }
	_ = json.Unmarshal(answer.body, &refused)
	if answer.status != http.StatusForbidden ||
		refused.Error.Code != "forbidden" {
		t.Errorf("answer %d %s, want 403 forbidden",
			answer.status, answer.body)
	}
	if checker.rechecks.Load() == 0 {
		t.Error("the answer was never rechecked")
	}
}

// TestListDisclosureRuns pins that the list's Disclosed check runs at
// all. api.Disclosed does nothing outside the gate's request, so a
// registration on the wrong context would pass every other test.
func TestListDisclosureRuns(t *testing.T) {
	checker := &fakeRechecker{}
	counts := countedConfig(checker)
	var guarded struct {
		mutex         sync.Mutex
		calls         int
		actor         rastrillo.Actor
		result        any
		nilDescriptor bool
	}
	counts.config.Guard = func(
		_ context.Context, actor rastrillo.Actor,
		descriptor *op.Descriptor, input json.RawMessage, result any,
	) error {
		guarded.mutex.Lock()
		defer guarded.mutex.Unlock()
		guarded.calls++
		guarded.actor, guarded.result = actor, result
		guarded.nilDescriptor = descriptor == nil && input == nil
		return nil
	}
	url := serve(t, counts.config, bookmarks()...)
	answer := post(t, http.MethodPost, url, readerToken, "2025-06-18",
		listBody)
	if answer.status != http.StatusOK {
		t.Fatalf("answer %d %s", answer.status, answer.body)
	}
	// Two read tools pass Authorize as the list is built and once more
	// as it goes out; the three writes stop at Admit both times.
	if calls := counts.authorizations.Load(); calls != 4 {
		t.Errorf("Authorize ran %d times, want 2 to build and 2 to recheck",
			calls)
	}
	guarded.mutex.Lock()
	defer guarded.mutex.Unlock()
	if guarded.calls != 1 || !guarded.nilDescriptor ||
		guarded.actor.Credential != "tok-reader" {
		t.Errorf("Guard ran %d times, for %v, with no descriptor: %v",
			guarded.calls, guarded.actor, guarded.nilDescriptor)
	}
	listed, isDescriptors := guarded.result.([]op.Descriptor)
	var ids []string
	for _, descriptor := range listed {
		ids = append(ids, descriptor.ID)
	}
	if !isDescriptors ||
		!slices.Equal(ids, []string{"bookmarks.get", "bookmarks.list"}) {
		t.Errorf("Guard saw %#v, want the listed descriptors",
			guarded.result)
	}
}

func TestListGuardRefuses(t *testing.T) {
	checker := &fakeRechecker{}
	config := countedConfig(checker).config
	config.Guard = func(
		context.Context, rastrillo.Actor, *op.Descriptor,
		json.RawMessage, any,
	) error {
		return op.Forbidden{Reason: "a listed tool is no longer yours"}
	}
	url := serve(t, config, bookmarks()...)
	answer := post(t, http.MethodPost, url, writerToken, "2025-06-18",
		listBody)
	if answer.status != http.StatusForbidden {
		t.Errorf("answer %d %s, want the guard's 403",
			answer.status, answer.body)
	}
}

func TestListPanicIsInternal(t *testing.T) {
	config := testConfig()
	var panics atomic.Int64
	config.Authorize = func(
		_ context.Context, actor rastrillo.Actor, _ rastrillo.Operation,
	) error {
		if actor.Access == rastrillo.Read {
			panics.Add(1)
			panic("policy bug")
		}
		return nil
	}
	url := serve(t, config, bookmarks()...)
	session, _ := connect(t, url, readerToken, "2025-06-18")
	_, err := session.ListTools(t.Context(), nil)
	var rpc *jsonrpc.Error
	if !errors.As(err, &rpc) || rpc.Code != jsonrpc.CodeInternalError {
		t.Fatalf("tools/list = %v, want an internal JSON-RPC error", err)
	}
	var data api.Error
	if err := json.Unmarshal(rpc.Data, &data); err != nil ||
		data.Code != "internal" || data.Ref == "" ||
		rpc.Message != data.Message {
		t.Errorf("error %+v, data %s: want the API's internal error "+
			"with a ref", rpc, rpc.Data)
	}
	if panics.Load() == 0 {
		t.Error("Authorize never ran")
	}
	// The process is still up, and the next caller's list is answered.
	if got := toolNames(t, url, writerToken); !slices.Equal(got, everyTool) {
		t.Errorf("after a panic the writer's list is %v", got)
	}
}

// TestListFailsOnAPolicyError has the policy fail rather than refuse.
// Hiding the tool would show a shorter list for the whole connection
// and log nothing, so the list fails, under a ref the log shares.
func TestListFailsOnAPolicyError(t *testing.T) {
	config := testConfig()
	logged := &lockedBuffer{}
	config.Gate.Logger = slog.New(slog.NewTextHandler(logged, nil))
	config.Authorize = func(
		_ context.Context, _ rastrillo.Actor, operation rastrillo.Operation,
	) error {
		if operation.ID == "bookmarks.get" {
			return errors.New("the policy database is down")
		}
		return nil
	}
	url := serve(t, config, bookmarks()...)
	session, _ := connect(t, url, writerToken, "2025-06-18")
	listed, err := session.ListTools(t.Context(), nil)
	var rpc *jsonrpc.Error
	if !errors.As(err, &rpc) || rpc.Code != jsonrpc.CodeInternalError {
		t.Fatalf("tools/list = %v, %v; want an internal JSON-RPC error",
			listed, err)
	}
	var data api.Error
	if err := json.Unmarshal(rpc.Data, &data); err != nil ||
		data.Ref == "" {
		t.Fatalf("error data %s has no ref", rpc.Data)
	}
	line := logged.String()
	if !strings.Contains(line, data.Ref) ||
		!strings.Contains(line, "the policy database is down") {
		t.Errorf("no log line names the cause under ref %s: %s",
			data.Ref, line)
	}
}

// TestListShowsWritesWithoutASession: tools/list never needs a session
// (spec §2), and a hidden write would never show the agent the message
// that tells it to send Agent-Session. The recheck holds to the same.
func TestListShowsWritesWithoutASession(t *testing.T) {
	checker := &fakeRechecker{}
	config := countedConfig(checker).config
	config.Gate.RequireAgentSession = true
	url := serve(t, config, bookmarks()...)
	if got := toolNames(t, url, writerToken); !slices.Equal(got, everyTool) {
		t.Errorf("an agent with no session lists %v, want %v",
			got, everyTool)
	}
	if got := toolNames(t, url, readerToken); !slices.Equal(got, readTools) {
		t.Errorf("a read-only agent with no session lists %v, want %v",
			got, readTools)
	}
	if checker.rechecks.Load() == 0 {
		t.Error("the lists were never rechecked")
	}
}

// lockedBuffer is a log destination the server's goroutines share.
type lockedBuffer struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

func (locked *lockedBuffer) Write(data []byte) (int, error) {
	locked.mutex.Lock()
	defer locked.mutex.Unlock()
	return locked.buffer.Write(data)
}

func (locked *lockedBuffer) String() string {
	locked.mutex.Lock()
	defer locked.mutex.Unlock()
	return locked.buffer.String()
}
