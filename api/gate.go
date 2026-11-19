package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"slices"
	"sync"
	"time"
	"unicode"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/op"
)

// Gate is the request pipeline every agent-facing handler shares. The
// API tree runs it per route, with Admit between the agent headers and
// After; MCP's one endpoint runs it through Wrap and calls Admit per
// tool call, because only then does it know the operation.
type Gate struct {
	// Verify checks the credential on every request. One that is also a
	// Rechecker has authority checked again as the answer goes out.
	Verify Verifier
	// Logger receives refusals and panics. Nil means slog.Default().
	Logger *slog.Logger
	// MaxBody caps a request body. Zero or less means 1 MiB.
	MaxBody int64
	// MaxResponse caps an answer, which is held whole until it has been
	// checked. Zero or less means 2 MiB.
	MaxResponse int64
	// Before runs before the credential check and After once the actor
	// is known and allowed, first element outermost in each.
	Before, After []func(http.Handler) http.Handler
	// RequireAgentSession has Admit refuse an agent's write that came
	// with no Agent-Session header.
	RequireAgentSession bool
	// Redact takes token-shaped text out of what the gate logs.
	Redact func(string) string

	// recheckWindow bounds one recheck and the write after it; zero
	// means five seconds. Tests shorten it.
	recheckWindow time.Duration
}

// Rechecker is a Verifier that can check a credential again without side
// effects: no quota charged, no last-used time written. Verify runs once
// per request; Recheck runs as the answer is emitted.
type Rechecker interface {
	Recheck(
		ctx context.Context, request *http.Request, was rastrillo.Actor,
	) (rastrillo.Actor, error)
}

// The 422 messages for the agent headers. They are not body members,
// so they carry no fields and the message names the header instead.
const (
	msgAgentHeader = "Send %s as 200 bytes or fewer, " +
		"with no control characters."
	msgAgentSession = "Send an Agent-Session header with every write."
)

// maxAgentHeader caps Agent-Session and Agent-Harness. They are the
// client's own claims and go into every log line for the request, so
// an unbounded one would let a client write as much as it likes there.
const maxAgentHeader = 200

// errAgentSession is Admit's refusal of an agent's write with no
// session, which Refusal answers as invalid with msgAgentSession.
var errAgentSession = errors.New(
	"api: an agent's write needs an Agent-Session header")

// Validate reports a gate that would fail on its first request, so a
// door built on it fails the app's start instead. api.New and mcp.New
// call it.
func (gate Gate) Validate() error {
	if gate.Verify == nil {
		return errors.New(
			"api: Verify is required: set Config.Verify or Gate.Verify")
	}
	for index, middleware := range gate.Before {
		if middleware == nil {
			return fmt.Errorf("api: Before[%d] is nil", index)
		}
	}
	for index, middleware := range gate.After {
		if middleware == nil {
			return fmt.Errorf("api: After[%d] is nil", index)
		}
	}
	return nil
}

// Wrap runs handler behind the gate: Before, Verify, the agent headers,
// After, panic recovery, redacted logging and guarded emission, with
// the Actor on the context. A malformed agent header is refused here,
// for every request; handler calls Admit itself once it knows the
// operation. Wrap panics on a Before or After that returns nil, as
// ServeMux.Handle does on a bad pattern.
func (gate Gate) Wrap(handler http.Handler) http.Handler {
	inner, err := gate.compose(nil, handler)
	if err != nil {
		panic(err)
	}
	return http.HandlerFunc(func(
		response http.ResponseWriter, request *http.Request,
	) {
		gate.serve(response, request, inner)
	})
}

// Admit is the gate's per-operation half, in the tree's order:
// RequireAgentSession for a write, then the read-only check. A
// read-only agent's write with no session is refused for the session,
// so both doors give that caller the same answer.
func (gate Gate) Admit(
	actor rastrillo.Actor, operation rastrillo.Operation,
) error {
	// Anything but Read is held to the write rules, so an operation
	// that lost its Access to a bug is refused rather than let through.
	writes := operation.Access != rastrillo.Read
	missingSession := actor.Agent != "" && actor.Session == ""
	if gate.RequireAgentSession && writes && missingSession {
		return errAgentSession
	}
	// Not "== Read": an Access added later would otherwise pass as a
	// writer. Only Write may write.
	if writes && actor.Access != rastrillo.Write {
		return op.Forbidden{Reason: "read-only credential"}
	}
	return nil
}

type disclosuresKey struct{}

// disclosures is what one request's answer must still pass as it goes
// out. The mutex is there because a handler may register from a
// goroutine of its own, as the MCP SDK runs its handlers.
type disclosures struct {
	mutex  sync.Mutex
	checks []func(ctx context.Context, fresh rastrillo.Actor) error
}

// Disclosed registers a check the gate reruns, with the fresh actor,
// before the headers and before each 64 KiB of this request's answer.
// Outside Wrap it does nothing.
func Disclosed(
	ctx context.Context,
	check func(ctx context.Context, fresh rastrillo.Actor) error,
) {
	list, inside := ctx.Value(disclosuresKey{}).(*disclosures)
	if !inside {
		return
	}
	if check == nil {
		panic("api: Disclosed needs a check, not nil")
	}
	list.mutex.Lock()
	defer list.mutex.Unlock()
	list.checks = append(list.checks, check)
}

func (list *disclosures) snapshot() []func(
	ctx context.Context, fresh rastrillo.Actor) error {
	list.mutex.Lock()
	defer list.mutex.Unlock()
	return slices.Clone(list.checks)
}

// compose builds everything after serve: Before, the credential and
// agent headers, admit when the caller knows the operation, emission,
// After and handler. The tree and Wrap both build theirs here, so the
// two doors cannot drift apart.
func (gate Gate) compose(
	admit func(rastrillo.Actor) error, handler http.Handler,
) (http.Handler, error) {
	inner, err := chain(handler, gate.After, "After")
	if err != nil {
		return nil, err
	}
	inner = gate.admitting(admit, gate.emitting(inner))
	return chain(inner, gate.Before, "Before")
}

func chain(
	handler http.Handler,
	middlewares []func(http.Handler) http.Handler, field string,
) (http.Handler, error) {
	for index := len(middlewares) - 1; index >= 0; index-- {
		if handler = middlewares[index](handler); handler == nil {
			return nil, fmt.Errorf(
				"api: %s[%d] returned a nil handler", field, index)
		}
	}
	return handler, nil
}

type startedKey struct{}

// serve is the gate's first step, before anything can refuse, and the
// panic recovery around all the rest. It is outermost, so a panic in a
// Before or After middleware is answered as one in a handler is.
func (gate Gate) serve(
	response http.ResponseWriter, request *http.Request, next http.Handler,
) {
	response.Header().Set("Cache-Control", "no-store")
	// The default, not only what Write sets: a handler or middleware
	// that writes bytes itself would otherwise get net/http's sniffed
	// text/plain, and one that writes only a status would get no type.
	// A handler that answers something else sets its own over this.
	response.Header().Set("Content-Type", "application/json")
	// Stamped before anything can refuse, so Refuse and Decode, and a
	// Before middleware calling WriteError, all see this gate's settings
	// rather than slog.Default and the 1 MiB default.
	request = WithLogger(request, gate.Logger)
	request = WithRedact(request, gate.Redact)
	request = WithMaxBody(request, gate.MaxBody)
	started := &startedWriter{ResponseWriter: response}
	request = request.WithContext(
		context.WithValue(request.Context(), startedKey{}, started))
	// Capped here as well as in Decode, because MCP's SDK reads the body
	// itself with an unbounded io.ReadAll.
	if request.Body != nil {
		request.Body = http.MaxBytesReader(
			response, request.Body, maxBodyFrom(request))
	}
	defer func() {
		if value := recover(); value != nil {
			recovered(started, request, value)
		}
	}()
	next.ServeHTTP(started, request)
}

// admitting is the credential, the agent headers and the operation's
// own checks when admit is set, then the verified actor stamped on the
// request for emission, After and the handler.
func (gate Gate) admitting(
	admit func(rastrillo.Actor) error, next http.Handler,
) http.Handler {
	return http.HandlerFunc(func(
		response http.ResponseWriter, request *http.Request,
	) {
		actor, known := gate.identify(response, request)
		if !known {
			return
		}
		if admit != nil {
			if err := admit(actor); err != nil {
				Refuse(response, request, err)
				return
			}
		}
		next.ServeHTTP(response, rastrillo.WithActor(request, actor))
	})
}

// identify is the credential, then the agent headers, or a refusal
// already answered and false.
func (gate Gate) identify(
	response http.ResponseWriter, request *http.Request,
) (rastrillo.Actor, bool) {
	actor, err := gate.Verify.Verify(request.Context(), request)
	if err != nil {
		Refuse(response, request, err)
		return rastrillo.Actor{}, false
	}
	// The actor a Verifier returns is all the gate knows about who is
	// calling. With no credential, op.Check and DefaultPolicy would
	// treat it as the app itself and allow everything; with no access,
	// the read-only check would have nothing to go on. Either is a
	// Verifier bug, so it is a 500, not a guess.
	if actor.Credential == "" || !actor.Access.Valid() {
		Refuse(response, request, fmt.Errorf("api: Verify returned %s "+
			"with no error but no credential or access", actor))
		return rastrillo.Actor{}, false
	}
	// Agent-Session and Agent-Harness are only an agent's to send. A
	// person's own token could otherwise pose as an agent in the log,
	// so for anyone else they are dropped unread, and whatever the
	// Verifier put in Session and Harness is dropped too: those two
	// come from the request or not at all.
	actor.Session, actor.Harness = "", ""
	if actor.Agent == "" {
		return actor, true
	}
	var valid bool
	actor.Session, valid = agentHeader(response, request, "Agent-Session")
	if !valid {
		return rastrillo.Actor{}, false
	}
	actor.Harness, valid = agentHeader(response, request, "Agent-Harness")
	return actor, valid
}

// agentHeader reads one agent header, or answers 422 and returns false.
// Control characters are refused rather than stripped: these values go
// into log lines, and a newline in one could forge a line of its own.
func agentHeader(
	response http.ResponseWriter, request *http.Request, name string,
) (string, bool) {
	value := request.Header.Get(name)
	refused := len(value) > maxAgentHeader
	for _, character := range value {
		if unicode.IsControl(character) {
			refused = true
			break
		}
	}
	if refused {
		invalid(response, fmt.Sprintf(msgAgentHeader, name), nil)
		return "", false
	}
	return value, true
}

// recovered answers a panic. Before the answer has started, that is a
// JSON 500 with a ref the log line shares. After it, appending JSON
// would corrupt a body the client may already be reading, so the
// connection is aborted and the client sees a truncated response, not
// a wrong one. Either way the value is logged here first: net/http
// logs nothing for http.ErrAbortHandler, so without this line the
// panic would vanish.
func recovered(response *startedWriter, request *http.Request, value any) {
	if value == http.ErrAbortHandler {
		// A handler that aborted on purpose has nothing to report.
		panic(value)
	}
	// Every free-text field goes through the redactor, the path too: a
	// client that pastes its token into a path segment would otherwise
	// put the secret in the log line beside the redacted value.
	redact := redactorFrom(request)
	ref := rastrillo.NewRef()
	loggerFrom(request).Error("api: panic",
		"err", redact(fmt.Sprint(value)), "ref", ref,
		"method", request.Method, "path", redact(request.URL.Path),
		"stack", redact(string(debug.Stack())))
	if response.started {
		panic(http.ErrAbortHandler)
	}
	internal := codes["internal"]
	WriteError(response, internal.status,
		Error{Code: "internal", Message: internal.message, Ref: ref})
}

// startedWriter records whether the answer has started, which decides
// what a panic can still do about it. The emission buffer marks it too,
// when the handler writes: a handler that began an answer and then
// panicked has its connection aborted, as it did before answers were
// buffered.
type startedWriter struct {
	http.ResponseWriter
	started bool
}

// startedFrom is the request's startedWriter, or a spare one for a
// request that did not come through serve, so no caller checks for nil.
func startedFrom(request *http.Request) *startedWriter {
	started, found := request.Context().Value(startedKey{}).(*startedWriter)
	if !found {
		return &startedWriter{}
	}
	return started
}

func (response *startedWriter) WriteHeader(status int) {
	response.started = true
	response.ResponseWriter.WriteHeader(status)
}

func (response *startedWriter) Write(data []byte) (int, error) {
	response.started = true
	return response.ResponseWriter.Write(data)
}

// FlushError is here, not left to Unwrap, because a flush sends the
// status line: a handler that flushed before writing has started the
// response as surely as one that wrote.
func (response *startedWriter) FlushError() error {
	response.started = true
	return http.NewResponseController(response.ResponseWriter).Flush()
}

// Flush keeps the wrapper an http.Flusher for a handler that asserts
// the interface rather than using http.ResponseController.
func (response *startedWriter) Flush() { _ = response.FlushError() }

// Unwrap lets http.ResponseController reach the real writer for
// everything else, such as the emission's write deadlines.
func (response *startedWriter) Unwrap() http.ResponseWriter {
	return response.ResponseWriter
}
