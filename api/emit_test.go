package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	json "github.com/go-json-experiment/json"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/op"
)

// fakeRechecker is the fake verifier with a Recheck whose answer a test
// changes between calls. By default it answers the original actor with
// no Session or Harness, as a real store would: those come from the
// request's headers, never from the credential.
type fakeRechecker struct {
	fakeVerifier
	mutex    sync.Mutex
	rechecks int
	answer   func(call int, was rastrillo.Actor) (rastrillo.Actor, error)
}

func newRechecker(verify fakeVerifier) *fakeRechecker {
	return &fakeRechecker{fakeVerifier: verify}
}

func (checker *fakeRechecker) Recheck(
	_ context.Context, _ *http.Request, was rastrillo.Actor,
) (rastrillo.Actor, error) {
	checker.mutex.Lock()
	checker.rechecks++
	call, answer := checker.rechecks, checker.answer
	checker.mutex.Unlock()
	if answer != nil {
		return answer(call, was)
	}
	fresh := was
	fresh.Session, fresh.Harness = "", ""
	return fresh, nil
}

func (checker *fakeRechecker) answerWith(
	answer func(call int, was rastrillo.Actor) (rastrillo.Actor, error),
) {
	checker.mutex.Lock()
	defer checker.mutex.Unlock()
	checker.answer = answer
}

func (checker *fakeRechecker) calls() int {
	checker.mutex.Lock()
	defer checker.mutex.Unlock()
	return checker.rechecks
}

func revoked(int, rastrillo.Actor) (rastrillo.Actor, error) {
	return rastrillo.Actor{}, fmt.Errorf("fake: revoked: %w", ErrUnauthorized)
}

// fetched is one answer read off a real connection.
type fetched struct {
	status  int
	header  http.Header
	body    []byte
	readErr error
}

// fetch goes through a real server: the emission sets write deadlines,
// which a ResponseRecorder cannot take.
func fetch(
	t *testing.T, handler http.Handler,
	method, target, token string, header ...string,
) fetched {
	t.Helper()
	server := httptest.NewUnstartedServer(handler)
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.Start()
	t.Cleanup(server.Close)
	request, err := http.NewRequest(method, server.URL+target,
		strings.NewReader(`{"title":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	for index := 0; index+1 < len(header); index += 2 {
		request.Header.Set(header[index], header[index+1])
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(response.Body)
	return fetched{response.StatusCode, response.Header, body, readErr}
}

func (answer fetched) refusal(t *testing.T) Error {
	t.Helper()
	var envelope struct {
		Error Error `json:"error"`
	}
	if err := json.Unmarshal(answer.body, &envelope); err != nil {
		t.Fatalf("body %q is not an error envelope: %v", answer.body, err)
	}
	return envelope.Error
}

func (answer fetched) assertRefused(t *testing.T, status int, code string) {
	t.Helper()
	if answer.status != status {
		t.Fatalf("status = %d, want %d; body %s",
			answer.status, status, answer.body)
	}
	if got := answer.refusal(t).Code; got != code {
		t.Fatalf("code = %q, want %q", got, code)
	}
	if strings.Contains(string(answer.body), secretAnswer) {
		t.Fatalf("the handler's answer went out: %s", answer.body)
	}
}

// secretAnswer is what a handler discloses; a refused answer must never
// carry it.
const secretAnswer = "the handler's answer"

func writesSecret(response http.ResponseWriter, _ *http.Request) {
	Write(response, http.StatusOK, map[string]string{"note": secretAnswer})
}

// writesBytes answers size bytes of x, written raw, so no Content-Length
// comes from Write.
func writesBytes(size int) http.Handler {
	return http.HandlerFunc(func(
		response http.ResponseWriter, _ *http.Request,
	) {
		_, _ = response.Write([]byte(strings.Repeat("x", size)))
	})
}

func gateWith(verify Verifier) Gate {
	return Gate{Verify: verify, Logger: slog.New(slog.DiscardHandler)}
}

func treeWith(t *testing.T, verify Verifier, routes ...Route) http.Handler {
	configuration := config()
	configuration.Verify = verify
	return mustNew(t, configuration, routes...)
}

func TestEmitRecheckBeforeHeaders(t *testing.T) {
	checker := newRechecker(verifier())
	tree := treeWith(t, checker, getThing(func(
		_ context.Context, _ *rastrillo.Ctx,
		response http.ResponseWriter, request *http.Request,
	) {
		writesSecret(response, request)
		checker.answerWith(revoked)
	}))
	answer := fetch(t, tree, http.MethodGet, "/api/v1/things/1",
		"person-read")
	answer.assertRefused(t, http.StatusUnauthorized, "unauthorized")
	if got := answer.header.Get("WWW-Authenticate"); got != "Bearer" {
		t.Errorf("WWW-Authenticate = %q, want Bearer", got)
	}
}

func TestEmitRecheckMidBody(t *testing.T) {
	checker := newRechecker(verifier())
	checker.answerWith(func(call int, was rastrillo.Actor) (
		rastrillo.Actor, error,
	) {
		if call == 1 {
			return was, nil
		}
		return revoked(call, was)
	})
	size := 200 << 10
	wrapped := gateWith(checker).Wrap(writesBytes(size))
	answer := fetch(t, wrapped, http.MethodGet, "/", "person-read")
	if answer.status != http.StatusOK {
		t.Fatalf("status = %d, want 200 before the revocation",
			answer.status)
	}
	if got := answer.header.Get("Content-Length"); got != strconv.Itoa(size) {
		t.Errorf("Content-Length = %q, want %d", got, size)
	}
	if !errors.Is(answer.readErr, io.ErrUnexpectedEOF) {
		t.Fatalf("read error = %v, want an unexpected EOF", answer.readErr)
	}
	if len(answer.body) > 64<<10 {
		t.Fatalf("the client read %d bytes, more than one 64 KiB chunk",
			len(answer.body))
	}
}

func TestEmitNarrowedAccess(t *testing.T) {
	checker := newRechecker(verifier())
	checker.answerWith(func(_ int, was rastrillo.Actor) (
		rastrillo.Actor, error,
	) {
		was.Access = rastrillo.Read
		return was, nil
	})
	tree := treeWith(t, checker, createThing(func(
		_ context.Context, _ *rastrillo.Ctx,
		response http.ResponseWriter, request *http.Request,
	) {
		writesSecret(response, request)
	}))
	answer := fetch(t, tree, http.MethodPost, "/api/v1/things",
		"person-write")
	answer.assertRefused(t, http.StatusForbidden, "forbidden")
}

// The tree reruns the app's policy for the route's operation with the
// fresh actor, not only the read-only check.
func TestEmitRerunsTheAppPolicy(t *testing.T) {
	checker := newRechecker(verifier())
	configuration := config()
	configuration.Verify = checker
	var refuse bool
	var mutex sync.Mutex
	configuration.Authorize = func(
		context.Context, rastrillo.Actor, rastrillo.Operation,
	) error {
		mutex.Lock()
		defer mutex.Unlock()
		if refuse {
			return op.Forbidden{Reason: "moved to another team"}
		}
		return nil
	}
	tree := mustNew(t, configuration, getThing(func(
		_ context.Context, _ *rastrillo.Ctx,
		response http.ResponseWriter, request *http.Request,
	) {
		writesSecret(response, request)
		mutex.Lock()
		defer mutex.Unlock()
		refuse = true
	}))
	answer := fetch(t, tree, http.MethodGet, "/api/v1/things/1",
		"person-read")
	answer.assertRefused(t, http.StatusForbidden, "forbidden")
}

func TestEmitSlowCheck(t *testing.T) {
	gate := gateWith(newRechecker(verifier()))
	gate.recheckWindow = 50 * time.Millisecond
	wrapped := gate.Wrap(http.HandlerFunc(func(
		response http.ResponseWriter, request *http.Request,
	) {
		Disclosed(request.Context(), func(context.Context,
			rastrillo.Actor) error {
			time.Sleep(150 * time.Millisecond)
			return nil
		})
		writesSecret(response, request)
	}))
	answer := fetch(t, wrapped, http.MethodGet, "/", "person-read")
	answer.assertRefused(t, http.StatusServiceUnavailable, "unavailable")
}

// A Recheck returns no Session, because the store never had one. Without
// the gate copying it back, an agent's write that passed
// RequireAgentSession on the way in would fail it on the way out.
func TestEmitRestoresSession(t *testing.T) {
	checker := newRechecker(verifier())
	configuration := config()
	configuration.Verify = checker
	configuration.RequireAgentSession = true
	tree := mustNew(t, configuration, createThing(ok))
	answer := fetch(t, tree, http.MethodPost, "/api/v1/things",
		"agent-write", "Agent-Session", "s-1")
	if answer.status != http.StatusOK || answer.readErr != nil {
		t.Fatalf("status = %d, read error %v, want a whole 200; body %s",
			answer.status, answer.readErr, answer.body)
	}
	if want := `{"harness":"","session":"s-1"}`; string(answer.body) != want {
		t.Fatalf("body = %s, want %s", answer.body, want)
	}
	if checker.calls() == 0 {
		t.Fatal("the answer went out with no recheck")
	}
}

func TestEmitRunsDisclosedChecksInOrder(t *testing.T) {
	var ran, sessions []string
	var mutex sync.Mutex
	check := func(name string, err error) func(
		context.Context, rastrillo.Actor) error {
		return func(_ context.Context, fresh rastrillo.Actor) error {
			mutex.Lock()
			defer mutex.Unlock()
			ran = append(ran, name)
			sessions = append(sessions, fresh.Session)
			return err
		}
	}
	cases := []struct {
		name   string
		second error
		status int
		ran    []string
	}{
		{"all pass", nil, http.StatusOK, []string{"one", "two", "three"}},
		{"the second refuses", op.Forbidden{}, http.StatusForbidden,
			[]string{"one", "two"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ran, sessions = nil, nil
			gate := gateWith(newRechecker(verifier()))
			wrapped := gate.Wrap(http.HandlerFunc(func(
				response http.ResponseWriter, request *http.Request,
			) {
				Disclosed(request.Context(), check("one", nil))
				Disclosed(request.Context(), check("two", tc.second))
				Disclosed(request.Context(), check("three", nil))
				writesSecret(response, request)
			}))
			answer := fetch(t, wrapped, http.MethodGet, "/", "agent-read",
				"Agent-Session", "s-1")
			mutex.Lock()
			defer mutex.Unlock()
			if answer.status != tc.status {
				t.Fatalf("status = %d, want %d", answer.status, tc.status)
			}
			if !slices.Equal(ran, tc.ran) {
				t.Fatalf("checks ran %q, want %q", ran, tc.ran)
			}
			for _, session := range sessions {
				if session != "s-1" {
					t.Fatalf("a check saw Session %q, want s-1", session)
				}
			}
		})
	}
}

// The holder is who the credential names; any change to it is a 401,
// while a change to what the grant allows is the policy's to judge.
func TestEmitChecksTheHolder(t *testing.T) {
	granted := agentWrite
	granted.Grant = &rastrillo.Grant{
		ID: "g-1", Issuer: "https://connect.example",
		IdentityIssuer: "https://id.example", Audience: "tickets",
		Teams: []string{"a"}, Scopes: []string{"read"},
	}
	cases := []struct {
		name   string
		change func(fresh *rastrillo.Actor)
		status int
	}{
		{"nothing", func(*rastrillo.Actor) {}, 200},
		{"Subject", func(fresh *rastrillo.Actor) {
			fresh.Subject = "eve"
		}, 401},
		{"Agent", func(fresh *rastrillo.Actor) { fresh.Agent = "Other" }, 401},
		{"Credential", func(fresh *rastrillo.Actor) {
			fresh.Credential = "aw000000000b"
		}, 401},
		{"Grant gone", func(fresh *rastrillo.Actor) { fresh.Grant = nil }, 401},
		{"Grant.ID", grantChange(func(grant *rastrillo.Grant) {
			grant.ID = "g-2"
		}), 401},
		{"Grant.Issuer", grantChange(func(grant *rastrillo.Grant) {
			grant.Issuer = "https://other.example"
		}), 401},
		{"Grant.IdentityIssuer", grantChange(func(grant *rastrillo.Grant) {
			grant.IdentityIssuer = "https://other.example"
		}), 401},
		{"Grant.Audience", grantChange(func(grant *rastrillo.Grant) {
			grant.Audience = "notes"
		}), 401},
		{"Grant.Teams", grantChange(func(grant *rastrillo.Grant) {
			grant.Teams = []string{"b"}
		}), 200},
		{"Grant.Scopes", grantChange(func(grant *rastrillo.Grant) {
			grant.Scopes = nil
		}), 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checker := newRechecker(fakeVerifier{
				tokens: map[string]rastrillo.Actor{"granted": granted},
			})
			checker.answerWith(func(_ int, was rastrillo.Actor) (
				rastrillo.Actor, error,
			) {
				tc.change(&was)
				return was, nil
			})
			wrapped := gateWith(checker).Wrap(http.HandlerFunc(writesSecret))
			answer := fetch(t, wrapped, http.MethodGet, "/", "granted")
			if tc.status == http.StatusOK {
				if answer.status != http.StatusOK {
					t.Fatalf("status = %d, want 200; body %s",
						answer.status, answer.body)
				}
				return
			}
			answer.assertRefused(t, http.StatusUnauthorized, "unauthorized")
		})
	}
}

// grantChange copies the grant before changing it, so the verifier's
// own actor, which shares the pointer, keeps the original.
func grantChange(change func(grant *rastrillo.Grant)) func(
	fresh *rastrillo.Actor) {
	return func(fresh *rastrillo.Actor) {
		grant := *fresh.Grant
		change(&grant)
		fresh.Grant = &grant
	}
}

func TestEmitNoStore(t *testing.T) {
	checker := newRechecker(verifier())
	gate := gateWith(checker)
	gate.MaxResponse = 100
	wrapped := gate.Wrap(http.HandlerFunc(func(
		response http.ResponseWriter, request *http.Request,
	) {
		response.Header().Set("Cache-Control", "no-cache, no-transform")
		size, _ := strconv.Atoi(request.URL.Query().Get("size"))
		_, _ = response.Write([]byte(strings.Repeat("x", size)))
	}))
	cases := []struct {
		name, target string
		answer       func(int, rastrillo.Actor) (rastrillo.Actor, error)
		status       int
	}{
		{"200", "/?size=10", nil, http.StatusOK},
		{"401", "/?size=10", revoked, http.StatusUnauthorized},
		{"422", "/?size=101", nil, http.StatusUnprocessableEntity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checker.answerWith(tc.answer)
			answer := fetch(t, wrapped, http.MethodGet, tc.target,
				"person-read")
			if answer.status != tc.status {
				t.Fatalf("status = %d, want %d", answer.status, tc.status)
			}
			if got := answer.header.Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", got)
			}
		})
	}
}

func TestEmitTooLarge(t *testing.T) {
	gate := gateWith(verifier())
	gate.MaxResponse = 1000
	wrapped := gate.Wrap(http.HandlerFunc(func(
		response http.ResponseWriter, request *http.Request,
	) {
		size, _ := strconv.Atoi(request.URL.Query().Get("size"))
		_, _ = response.Write([]byte(strings.Repeat("x", size/2)))
		_, _ = response.Write([]byte(strings.Repeat("x", size-size/2)))
	}))
	defaultCap := treeWith(t, verifier(), getThing(func(
		_ context.Context, _ *rastrillo.Ctx,
		response http.ResponseWriter, request *http.Request,
	) {
		size, _ := strconv.Atoi(request.URL.Query().Get("size"))
		_, _ = response.Write([]byte(strings.Repeat("x", size)))
	}))
	cases := []struct {
		name    string
		handler http.Handler
		target  string
		size    int
	}{
		{"MaxResponse", wrapped, "/", 1000},
		{"the 2 MiB default", defaultCap, "/api/v1/things/1", 2 << 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			whole := serve(tc.handler, http.MethodGet,
				tc.target+"?size="+strconv.Itoa(tc.size), "person-read", "")
			if whole.Code != http.StatusOK || whole.Body.Len() != tc.size {
				t.Fatalf("at the cap: %d with %d bytes, want 200 with %d",
					whole.Code, whole.Body.Len(), tc.size)
			}
			over := serve(tc.handler, http.MethodGet,
				tc.target+"?size="+strconv.Itoa(tc.size+1), "person-read", "")
			assertCode(t, over, http.StatusUnprocessableEntity,
				"response_too_large")
			if got := decodeError(t, over).Message; got != msgResponseTooLarge {
				t.Fatalf("message = %q, want %q", got, msgResponseTooLarge)
			}
		})
	}
}

func TestEmitWithoutRechecker(t *testing.T) {
	var checks int
	wrapped := gateWith(verifier()).Wrap(http.HandlerFunc(func(
		response http.ResponseWriter, request *http.Request,
	) {
		Disclosed(request.Context(), func(context.Context,
			rastrillo.Actor) error {
			checks++
			return nil
		})
		_, _ = response.Write([]byte(`{"id":1}`))
	}))
	recorder := serve(wrapped, http.MethodGet, "/", "person-read", "")
	if recorder.Code != http.StatusOK || recorder.Body.String() != `{"id":1}` {
		t.Fatalf("answered %d %s, want 200 {\"id\":1}",
			recorder.Code, recorder.Body)
	}
	if got := recorder.Header().Get("Content-Length"); got != "8" {
		t.Errorf("Content-Length = %q, want 8", got)
	}
	if checks != 0 {
		t.Errorf("a Disclosed check ran %d times with no Rechecker", checks)
	}
}

// A ResponseRecorder cannot take a write deadline, so with a Rechecker
// nothing the handler wrote may go out through it.
func TestEmitWithoutWriteDeadlineIsUnavailable(t *testing.T) {
	gate := gateWith(newRechecker(verifier()))
	wrapped := gate.Wrap(http.HandlerFunc(writesSecret))
	recorder := serve(wrapped, http.MethodGet, "/", "person-read", "")
	assertCode(t, recorder, http.StatusServiceUnavailable, "unavailable")
	if strings.Contains(recorder.Body.String(), secretAnswer) {
		t.Fatalf("the handler's answer went out: %s", recorder.Body)
	}
}

func TestEmitPanicMidBodyAborts(t *testing.T) {
	var logs syncBuffer
	gate := gateWith(newRechecker(verifier()))
	gate.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	var calls int
	wrapped := gate.Wrap(http.HandlerFunc(func(
		response http.ResponseWriter, request *http.Request,
	) {
		Disclosed(request.Context(), func(context.Context,
			rastrillo.Actor) error {
			if calls++; calls > 1 {
				panic("boom in a check")
			}
			return nil
		})
		_, _ = response.Write([]byte(strings.Repeat("x", 200<<10)))
	}))
	answer := fetch(t, wrapped, http.MethodGet, "/", "person-read")
	if !errors.Is(answer.readErr, io.ErrUnexpectedEOF) {
		t.Fatalf("read error = %v, want an unexpected EOF", answer.readErr)
	}
	if !strings.Contains(logs.String(), "boom in a check") {
		t.Fatalf("the panic is not in the gate's log: %q", logs.String())
	}
}

func TestWrapCapsBody(t *testing.T) {
	cases := []struct {
		name    string
		maxBody int64
		limit   int
	}{
		{"the 1 MiB default", 0, 1 << 20},
		{"a custom MaxBody", 10, 10},
	}
	for _, tc := range cases {
		for _, chunked := range []bool{false, true} {
			name := fmt.Sprintf("%s, chunked %v", tc.name, chunked)
			t.Run(name, func(t *testing.T) {
				gate := gateWith(verifier())
				gate.MaxBody = tc.maxBody
				var readErr error
				wrapped := gate.Wrap(http.HandlerFunc(func(
					response http.ResponseWriter, request *http.Request,
				) {
					_, readErr = io.ReadAll(request.Body)
					Write(response, http.StatusOK, struct{}{})
				}))
				read := func(size int) error {
					readErr = nil
					request := bodyOf(size, chunked)
					wrapped.ServeHTTP(httptest.NewRecorder(), request)
					return readErr
				}
				if err := read(tc.limit); err != nil {
					t.Fatalf("at the cap: %v", err)
				}
				var tooLarge *http.MaxBytesError
				if err := read(tc.limit + 1); !errors.As(err, &tooLarge) {
					t.Fatalf("past the cap: %v, want *http.MaxBytesError", err)
				}
			})
		}
	}
}

// bodyOf is a request with size bytes of body. Chunked, the reader hides
// its length, so nothing but the cap stops the read.
func bodyOf(size int, chunked bool) *http.Request {
	body := strings.Repeat("x", size)
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	request.Body = io.NopCloser(io.MultiReader(strings.NewReader(body)))
	request.ContentLength = int64(size)
	if chunked {
		request.ContentLength = -1
		request.TransferEncoding = []string{"chunked"}
	}
	request.Header.Set("Authorization", "Bearer person-read")
	return request
}

// plainWrapper is the hand-rolled writer wrapper a Before middleware
// often uses: it embeds the writer and has no Unwrap.
type plainWrapper struct{ http.ResponseWriter }

// A Before that wraps the writer must not hide the transport's write
// deadline from the gate: if it did, every rechecked answer would be a
// 503 the moment the verifier became a Rechecker.
func TestEmitPastBeforesWriterWrapper(t *testing.T) {
	checker := newRechecker(verifier())
	gate := gateWith(checker)
	gate.Before = []func(http.Handler) http.Handler{
		func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(
				response http.ResponseWriter, request *http.Request,
			) {
				next.ServeHTTP(plainWrapper{response}, request)
			})
		},
	}
	wrapped := gate.Wrap(http.HandlerFunc(writesSecret))

	answer := fetch(t, wrapped, http.MethodGet, "/", "person-read")
	if answer.status != http.StatusOK || answer.readErr != nil ||
		!strings.Contains(string(answer.body), secretAnswer) {
		t.Fatalf("answered %d, read error %v, body %s; want a whole 200",
			answer.status, answer.readErr, answer.body)
	}
	if checker.calls() == 0 {
		t.Fatal("the answer went out with no recheck")
	}

	checker.answerWith(revoked)
	answer = fetch(t, wrapped, http.MethodGet, "/", "person-read")
	answer.assertRefused(t, http.StatusUnauthorized, "unauthorized")
}
