package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// The words the console gives an instance's health. The console's full set
// (listquery.go:25) also holds Waking, Crashed and Never started, the
// words of its sidecar view, and it adds words over time. A caller that
// shows one to a person should treat a word it does not know as
// HealthUnknown.
const (
	HealthSettingUp     = "Setting up"
	HealthRunning       = "Running"
	HealthServing       = "Serving"
	HealthAsleep        = "Asleep"
	HealthFailed        = "Failed"
	HealthNotResponding = "Not responding"
	HealthNoReadings    = "No readings"
	HealthUnknown       = "Can't tell right now"
)

const (
	// maxListBody is how much of a list answer is read: a page of 499
	// records is more than maxBody, which fits one record.
	maxListBody = 4 << 20
	// maxListLimit is the largest page the console honours; it ignores a
	// limit of 500 or more.
	maxListLimit = 499
)

// ListRequest is one page of the instance list.
type ListRequest struct {
	Query  string // substring of the host, the console's q
	Cursor string
	Limit  int // 1..499; 0 lets the console choose
}

// Listed is one instance in the list: its record, the host people see
// and the console's own reading of its health. An empty PublicHost means
// the host people see is Host itself.
type Listed struct {
	Instance
	PublicHost string `json:"public_host"`
	Health     string `json:"health"`
}

// ListPage is one page of the list.
type ListPage struct {
	Instances []Listed
	Total     int
	Next      string // "" on the last page
	Truncated bool
}

// List reads one page of the app's instances, each with the console's
// health word for it. The console answers from its own readings, so the
// list never touches an instance and never wakes one.
//
// A 429, a 5xx or no answer is a *RetryError, and a 403 is ErrForbidden.
// List never retries by itself: the credential's rate budget is shared
// with Create, so the caller decides when to ask again.
func (console *Console) List(
	ctx context.Context, request ListRequest,
) (ListPage, error) {
	if request.Limit < 0 || request.Limit > maxListLimit {
		return ListPage{}, fmt.Errorf("%w: limit %d is outside 0..%d",
			ErrBadRequest, request.Limit, maxListLimit)
	}
	status, header, text, raw, err := console.callWithin(ctx,
		http.MethodGet, console.listPath(request), nil, maxListBody)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return ListPage{}, contextErr
		}
		// A read that failed after a 429 or 503 still carries its
		// Retry-After.
		failure := retryError(status, header, text)
		failure.Err = err
		return ListPage{}, failure
	}
	switch {
	case status == http.StatusOK:
	case status == http.StatusTooManyRequests || status >= 500:
		return ListPage{}, retryError(status, header, text)
	default:
		return ListPage{}, &APIError{Status: status, Message: text}
	}
	if len(raw) > maxListBody {
		// A cut-off page would read as a shorter fleet.
		return ListPage{}, fmt.Errorf(
			"provision: the list answer is larger than %d bytes", maxListBody)
	}
	return decodeListPage(status, text, raw)
}

// decodeListPage reads a 200 answer. A body that does not decode may be a
// proxy's page or a cut-off one, so it is worth another try.
func decodeListPage(status int, text string, raw []byte) (ListPage, error) {
	var answer struct {
		Instances  []Listed `json:"instances"`
		Truncated  bool     `json:"truncated"`
		Total      int      `json:"total"`
		NextCursor string   `json:"next_cursor"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return ListPage{}, &RetryError{Status: status, Message: text, Err: err}
	}
	return ListPage{
		Instances: answer.Instances, Total: answer.Total,
		Next: answer.NextCursor, Truncated: answer.Truncated,
	}, nil
}

func (console *Console) listPath(request ListRequest) string {
	query := url.Values{}
	if request.Query != "" {
		query.Set("q", request.Query)
	}
	if request.Cursor != "" {
		query.Set("cursor", request.Cursor)
	}
	if request.Limit > 0 {
		query.Set("limit", strconv.Itoa(request.Limit))
	}
	path := console.appPath("instances")
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	return path
}
