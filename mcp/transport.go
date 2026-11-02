package mcp

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"

	json "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

// jsonSpace is what the SDK's decoder skips before a value, so a batch
// is seen here exactly where the SDK would see one.
const jsonSpace = " \t\r\n"

// methodListen opens a stream whatever JSONResponse says (go-sdk v1.8.0
// mcp/streamable.go:1678-1683).
const methodListen = "subscriptions/listen"

// maxExactID is the largest integer a float64 holds exactly, 2^53-1.
// The SDK reads a numeric id through one (go-sdk v1.8.0
// internal/jsonrpc2/messages.go:30), so a larger id would be rounded.
const maxExactID = 1<<53 - 1

// msgInexactID answers such an id. It is a protocol error for client
// developers, not copy an agent or a person reads.
const msgInexactID = "The id must be a string or an integer " +
	"between -9007199254740991 and 9007199254740991."

// refusal is a JSON-RPC error answered before the SDK runs. Its id is
// nil when the request has none to give, and goes out as null, as
// JSON-RPC asks, where the SDK's own encoder would leave it out.
type refusal struct {
	status  int
	id      any
	code    int64
	message string
}

type answer struct {
	Version string         `json:"jsonrpc"`
	ID      any            `json:"id"`
	Error   *jsonrpc.Error `json:"error"`
}

// refusingStreams answers a GET, a batch and subscriptions/listen
// itself. A stream would hold a hibernating instance awake, and a batch
// would put several disclosures in one answer when the gate rechecks
// the one call that ran (spec §10).
func refusingStreams(next http.Handler) http.Handler {
	return http.HandlerFunc(func(
		response http.ResponseWriter, request *http.Request,
	) {
		if request.Method != http.MethodPost {
			response.Header().Set("Allow", http.MethodPost)
			refuse(response, refusal{
				status:  http.StatusMethodNotAllowed,
				code:    jsonrpc.CodeInvalidRequest,
				message: msgNoStreams,
			})
			return
		}
		body, err := readBody(request)
		// The SDK reads the body again, and must meet the same error,
		// so a body over Gate.MaxBody is still its 413.
		request.Body = replay(body, err)
		if err == nil {
			if refused, found := streamed(body); found {
				refuse(response, refused)
				return
			}
			if inexactID(body) {
				refuse(response, refusal{
					status:  http.StatusBadRequest,
					code:    jsonrpc.CodeInvalidRequest,
					message: msgInexactID,
				})
				return
			}
		}
		next.ServeHTTP(response, request)
	})
}

func readBody(request *http.Request) ([]byte, error) {
	if request.Body == nil {
		return nil, nil
	}
	return io.ReadAll(request.Body)
}

// streamed finds a batch, or a subscriptions/listen, with the SDK's own
// decoder: a parser of our own could read a different method from the
// same bytes, and let through what this exists to refuse.
func streamed(body []byte) (refusal, bool) {
	start := bytes.TrimLeft(body, jsonSpace)
	if len(start) > 0 && start[0] == '[' {
		return refusal{
			status:  http.StatusBadRequest,
			code:    jsonrpc.CodeInvalidRequest,
			message: msgNoStreams,
		}, true
	}
	message, err := jsonrpc.DecodeMessage(body)
	call, isRequest := message.(*jsonrpc.Request)
	if err != nil || !isRequest || call.Method != methodListen {
		return refusal{}, false
	}
	// A 404, as the SDK answers any unknown method under the protocol
	// that has subscriptions/listen (SEP-2575).
	return refusal{
		status:  http.StatusNotFound,
		id:      call.ID.Raw(),
		code:    jsonrpc.CodeMethodNotFound,
		message: msgNoStreams,
	}, true
}

// inexactID reports whether the message has a numeric id the SDK would
// round, read from the raw token and never through a float64. Every id
// member counts, as a decoder may keep any one of duplicates; a body
// that does not parse is left to the SDK to refuse.
func inexactID(body []byte) bool {
	decoder := jsontext.NewDecoder(bytes.NewReader(body),
		jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true))
	if token, err := decoder.ReadToken(); err != nil || token.Kind() != '{' {
		return false
	}
	for decoder.PeekKind() == '"' {
		name, err := decoder.ReadToken()
		if err != nil {
			return false
		}
		// Read now: the next read voids the token.
		isID := name.String() == "id"
		value, err := decoder.ReadValue()
		if err != nil {
			return false
		}
		if isID && value.Kind() == '0' && !exactInteger(value) {
			return true
		}
	}
	return false
}

// exactInteger is a number token written as an integer within
// maxExactID either side of zero; 1.0 and 1e3 are not, as written.
func exactInteger(number jsontext.Value) bool {
	integer, err := strconv.ParseInt(string(number), 10, 64)
	return err == nil && integer >= -maxExactID && integer <= maxExactID
}

func refuse(response http.ResponseWriter, refused refusal) {
	encoded, err := json.Marshal(answer{
		Version: "2.0",
		ID:      refused.id,
		Error: &jsonrpc.Error{
			Code: refused.code, Message: refused.message,
		},
	})
	if err != nil {
		// The gate answers a panic as a 500 with a ref.
		panic(fmt.Errorf("mcp: encode a refusal: %w", err))
	}
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(refused.status)
	_, _ = response.Write(encoded)
}

// replay gives back what was read, then the error that stopped it.
func replay(body []byte, err error) io.ReadCloser {
	read := io.Reader(bytes.NewReader(body))
	if err != nil {
		read = io.MultiReader(read, failing{err})
	}
	return io.NopCloser(read)
}

type failing struct{ err error }

func (reader failing) Read([]byte) (int, error) { return 0, reader.err }
