package api

import (
	"fmt"
	"net/http"
	"runtime/debug"

	rastrillo "amadan.net/rastrillo/rastrillo"
)

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
