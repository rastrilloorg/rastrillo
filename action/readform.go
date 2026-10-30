package action

import (
	"mime"
	"net/http"

	"amadan.net/rastrillo/rastrillo/form"
	"amadan.net/rastrillo/rastrillo/op"
)

// maxBodyBytes caps a form post at 1 MiB, so one request cannot make
// the server hold an unbounded body in memory.
const maxBodyBytes = 1 << 20

// BadRequest refuses a request whose body could not be read. Refused
// answers it with Message at Status and nothing else, because a broken
// body has no form worth showing back.
type BadRequest struct {
	Status  int
	Message string
}

// Error returns the message, which is also what the person is shown.
func (refusal BadRequest) Error() string { return refusal.Message }

// ReadBody caps the body at 1 MiB and parses it, as a urlencoded form
// or as multipart/form-data. An action calls it first, before any path
// or database work, so a broken body is a 400 whatever else is wrong
// with the request. Calling it again returns the same answer.
func (visit *Visit) ReadBody() error {
	if visit.bodyRead {
		return visit.bodyErr
	}
	visit.bodyRead = true
	multipart := hasMultipartBody(visit.request)
	visit.request.Body = http.MaxBytesReader(
		visit.response, visit.request.Body, maxBodyBytes,
	)
	if err := visit.parseBody(multipart); err != nil {
		visit.bodyErr = BadRequest{
			Status: http.StatusBadRequest, Message: "Bad request.",
		}
	}
	return visit.bodyErr
}

// parseBody asks for the multipart parse by name, because ParseForm
// leaves a multipart body unread and marks the form as parsed, which
// would turn every field of a valid upload into nothing and let a broken
// one through. The memory limit equals the cap, so no part can spill to
// a temporary file that nothing would remove.
func (visit *Visit) parseBody(multipart bool) error {
	if multipart {
		return visit.request.ParseMultipartForm(maxBodyBytes)
	}
	return visit.request.ParseForm()
}

// hasMultipartBody is true for a multipart/form-data post that has a
// body. A request without one, such as a GET or an empty POST, keeps
// the answer ParseForm gave it: no fields and no refusal.
func hasMultipartBody(request *http.Request) bool {
	switch request.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
	default:
		return false
	}
	empty := request.Body == nil || request.Body == http.NoBody
	if empty || request.ContentLength == 0 {
		return false
	}
	kind, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	return err == nil && kind == "multipart/form-data"
}

// ReadForm reads the body, then the fields. A field that does not parse
// comes back as op.Invalid beside the parsed form, because showing the
// form again needs what the person typed (Echo) as well as the errors.
func (visit *Visit) ReadForm(fields ...form.Field) (*form.Parsed, error) {
	if err := visit.ReadBody(); err != nil {
		return nil, err
	}
	parsed := form.Parse(visit.request, fields...)
	if !parsed.OK() {
		return parsed, op.Invalid{Fields: parsed.Errors()}
	}
	return parsed, nil
}
