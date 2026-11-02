package api

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"net/http"
	"strconv"
	"time"

	rastrillo "amadan.net/rastrillo/rastrillo"
)

// emitChunk is how much of an answer goes out per recheck, so a token
// revoked mid-answer stops it within one chunk.
const emitChunk = 64 << 10

const (
	defaultMaxResponse   int64 = 2 << 20
	defaultRecheckWindow       = 5 * time.Second
)

func (gate Gate) maxResponse() int64 {
	if gate.MaxResponse <= 0 {
		return defaultMaxResponse
	}
	return gate.MaxResponse
}

func (gate Gate) window() time.Duration {
	if gate.recheckWindow <= 0 {
		return defaultRecheckWindow
	}
	return gate.recheckWindow
}

// emission is one answer on its way out: who it was for, what it says,
// and what it must still pass.
type emission struct {
	response  http.ResponseWriter
	request   *http.Request
	was       rastrillo.Actor
	buffered  *answerBuffer
	checks    *disclosures
	rechecker Rechecker
	window    time.Duration
}

// emitting runs next into a buffer and sends the answer only once it is
// whole and, with a Rechecker, checked again. It sits outside After, so
// what After writes is held to the same checks as the handler's answer.
func (gate Gate) emitting(next http.Handler) http.Handler {
	return http.HandlerFunc(func(
		response http.ResponseWriter, request *http.Request,
	) {
		was, _ := rastrillo.ActorFromContext(request.Context())
		checks := &disclosures{}
		request = request.WithContext(
			context.WithValue(request.Context(), disclosuresKey{}, checks))
		buffered := &answerBuffer{
			header:  response.Header().Clone(),
			limit:   gate.maxResponse(),
			started: startedFrom(request),
		}
		next.ServeHTTP(buffered, request)
		out := emission{
			response: response, request: request, was: was,
			buffered: buffered, checks: checks, window: gate.window(),
		}
		if buffered.tooLarge {
			Refuse(response, request, errResponseTooLarge)
			return
		}
		rechecker, rechecks := gate.Verify.(Rechecker)
		if !rechecks {
			buffered.commit(response)
			_, _ = response.Write(buffered.sendable())
			return
		}
		out.rechecker = rechecker
		out.guarded()
	})
}

// guarded sends the answer a chunk at a time, each behind a recheck
// whose deadline also bounds the write after it (spec §3, steps 2-5).
func (out emission) guarded() {
	// Over the gate's own wrapper of the transport, not out.response: a
	// Before middleware's writer wrapper seldom has Unwrap, and through
	// it every rechecked answer would find no deadline and be a 503.
	controller := http.NewResponseController(startedFrom(out.request))
	// Without a write deadline a slow client could hold a checked chunk
	// open long after the check that allowed it.
	if err := controller.SetWriteDeadline(time.Time{}); err != nil {
		Refuse(out.response, out.request,
			fmt.Errorf("api: no write deadline: %v: %w", err, ErrUnavailable))
		return
	}
	// The connection may go on to serve another request, which must not
	// inherit this answer's deadline.
	defer func() { _ = controller.SetWriteDeadline(time.Time{}) }()
	body := out.buffered.sendable()
	for offset, first := 0, true; first || offset < len(body); {
		end := min(offset+emitChunk, len(body))
		err := out.recheck(controller)
		if err != nil && first {
			Refuse(out.response, out.request, err)
			return
		}
		if err != nil {
			out.abort(err)
		}
		if first {
			out.buffered.commit(out.response)
			first = false
		}
		if err := send(out.response, controller, body[offset:end]); err != nil {
			out.abort(err)
		}
		offset = end
	}
}

// recheck is one cycle: the credential, the holder, then every
// Disclosed check in order, under one deadline that it then sets as the
// write deadline for the chunk that follows.
func (out emission) recheck(controller *http.ResponseController) error {
	deadline := time.Now().Add(out.window)
	own, bounded := out.request.Context().Deadline()
	if bounded && own.Before(deadline) {
		deadline = own
	}
	ctx, cancel := context.WithDeadline(out.request.Context(), deadline)
	defer cancel()
	fresh, err := out.rechecker.Recheck(ctx, out.request, out.was)
	if err = settled(ctx, deadline, err); err != nil {
		return err
	}
	if err := sameHolder(out.was, fresh); err != nil {
		return err
	}
	// Session and Harness come from the request's headers, never from a
	// verifier, so a Recheck returns them empty; without the copy an
	// agent's write that passed RequireAgentSession would fail it now.
	fresh.Session, fresh.Harness = out.was.Session, out.was.Harness
	for _, check := range out.checks.snapshot() {
		if err := settled(ctx, deadline, check(ctx, fresh)); err != nil {
			return err
		}
	}
	if err := controller.SetWriteDeadline(deadline); err != nil {
		return fmt.Errorf("api: set a write deadline: %v: %w",
			err, ErrUnavailable)
	}
	return nil
}

// settled is a callback's error, or the deadline's when the callback
// returned after it: a slow check that passes must not let the
// credential check before it go stale.
func settled(ctx context.Context, deadline time.Time, err error) error {
	if ctx.Err() != nil || !time.Now().Before(deadline) {
		return fmt.Errorf("api: rechecking authority took past its "+
			"deadline: %w", ErrUnavailable)
	}
	return err
}

// sameHolder refuses a fresh actor that is someone else, or the same
// person under another credential or grant, as the token gone. What
// the grant allows, its Teams and Scopes, may change: the app's policy
// judges that in the Disclosed checks.
func sameHolder(was, fresh rastrillo.Actor) error {
	if was.Subject != fresh.Subject || was.Agent != fresh.Agent ||
		was.Credential != fresh.Credential ||
		!sameGrant(was.Grant, fresh.Grant) {
		return fmt.Errorf("api: the credential now names %s, not %s: %w",
			fresh, was, ErrUnauthorized)
	}
	// As Verify's: with no access, the read-only check has nothing to go
	// on, and that is a Rechecker bug, so a 500.
	if !fresh.Access.Valid() {
		return fmt.Errorf("api: Recheck returned %s with no valid access",
			fresh)
	}
	return nil
}

// sameGrant compares field by field, because each lookup builds a new
// Grant and the pointers never match.
func sameGrant(was, fresh *rastrillo.Grant) bool {
	if was == nil || fresh == nil {
		return was == nil && fresh == nil
	}
	return was.ID == fresh.ID && was.Issuer == fresh.Issuer &&
		was.IdentityIssuer == fresh.IdentityIssuer &&
		was.Audience == fresh.Audience
}

func send(
	response http.ResponseWriter, controller *http.ResponseController,
	chunk []byte,
) error {
	if _, err := response.Write(chunk); err != nil {
		return err
	}
	return controller.Flush()
}

// abort cuts the connection after the headers have gone. The answer
// carries a Content-Length, so the client sees a short body rather
// than a whole-looking one. The cause is logged here, because net/http
// logs nothing for http.ErrAbortHandler.
func (out emission) abort(err error) {
	loggerFrom(out.request).Warn("api: answer cut off",
		"err", redactorFrom(out.request)(err.Error()),
		"method", method(out.request), "path", path(out.request))
	panic(http.ErrAbortHandler)
}

// answerBuffer holds an answer until the gate has checked it may go.
// It has no Unwrap, deliberately: through http.ResponseController a
// handler could otherwise hijack the connection or write past the gate.
type answerBuffer struct {
	header   http.Header
	status   int
	body     bytes.Buffer
	limit    int64
	tooLarge bool
	started  *startedWriter
}

func (buffered *answerBuffer) Header() http.Header { return buffered.header }

func (buffered *answerBuffer) WriteHeader(status int) {
	// net/http's own rule, checked here because the real WriteHeader
	// runs only at commit, far from the handler that made the mistake.
	if status < 100 || status > 999 {
		panic(fmt.Sprintf("invalid WriteHeader code %v", status))
	}
	buffered.started.started = true
	// An informational status would go out at once, ahead of every
	// check, so it is dropped.
	if status >= 200 && buffered.status == 0 {
		buffered.status = status
	}
}

func (buffered *answerBuffer) Write(data []byte) (int, error) {
	buffered.started.started = true
	if buffered.status == 0 {
		buffered.status = http.StatusOK
	}
	if buffered.tooLarge {
		return 0, errResponseTooLarge
	}
	if int64(buffered.body.Len())+int64(len(data)) > buffered.limit {
		buffered.tooLarge = true
		buffered.body = bytes.Buffer{}
		return 0, errResponseTooLarge
	}
	return buffered.body.Write(data)
}

// FlushError sends nothing, since the answer goes out only once it has
// been checked, but it starts the answer as a real flush would.
func (buffered *answerBuffer) FlushError() error {
	buffered.started.started = true
	if buffered.status == 0 {
		buffered.status = http.StatusOK
	}
	return nil
}

// Flush keeps the buffer an http.Flusher for a handler that asserts
// the interface rather than using http.ResponseController.
func (buffered *answerBuffer) Flush() { _ = buffered.FlushError() }

// commit sends the handler's status and headers, with no-store and the
// buffered length over whatever it set: an answer is scoped to the token
// that asked, and the length lets a client tell a cut-off answer.
func (buffered *answerBuffer) commit(response http.ResponseWriter) {
	header := response.Header()
	clear(header)
	maps.Copy(header, buffered.header)
	header.Set("Cache-Control", "no-store")
	status := buffered.statusOrOK()
	header.Del("Content-Length")
	if bodyAllowed(status) {
		header.Set("Content-Length", strconv.Itoa(buffered.body.Len()))
	}
	response.WriteHeader(status)
}

func (buffered *answerBuffer) statusOrOK() int {
	if buffered.status == 0 {
		return http.StatusOK
	}
	return buffered.status
}

// sendable is the body to send, none for a status that carries none:
// net/http refuses such a write, which would abort a valid answer.
func (buffered *answerBuffer) sendable() []byte {
	if !bodyAllowed(buffered.statusOrOK()) {
		return nil
	}
	return buffered.body.Bytes()
}

func bodyAllowed(status int) bool {
	return status >= 200 && status != http.StatusNoContent &&
		status != http.StatusNotModified
}
