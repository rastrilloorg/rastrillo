package mcp

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	json "github.com/go-json-experiment/json"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

// jsonSpace is what the SDK's decoder skips before a value, so a batch
// is seen here exactly where the SDK would see one.
const jsonSpace = " \t\r\n"

// methodListen opens a stream whatever JSONResponse says (go-sdk v1.8.0
// mcp/streamable.go:1678-1683).
const methodListen = "subscriptions/listen"

// refusal is a JSON-RPC error answered before the SDK runs. Its id is
// nil when the request has none to give, and goes out as null, as
// JSON-RPC asks, where the SDK's own encoder would leave it out.
type refusal struct {
	status int
	id     any
	code   int64
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
				status: http.StatusMethodNotAllowed,
				code:   jsonrpc.CodeInvalidRequest,
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
			status: http.StatusBadRequest,
			code:   jsonrpc.CodeInvalidRequest,
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
		status: http.StatusNotFound,
		id:     call.ID.Raw(),
		code:   jsonrpc.CodeMethodNotFound,
	}, true
}

func refuse(response http.ResponseWriter, refused refusal) {
	encoded, err := json.Marshal(answer{
		Version: "2.0",
		ID:      refused.id,
		Error:   &jsonrpc.Error{Code: refused.code, Message: msgNoStreams},
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
