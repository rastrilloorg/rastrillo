// Package provision is a client for the Carlos console that a central app
// (a home or a hub) uses to create a tenant instance, read it back, and
// publish the hub's path map.
//
// One Console holds one app-bound service credential. The package writes
// no records: it turns the console's answers into typed outcomes, and the
// app keeps the record those outcomes describe. Its errors are for logs and
// records; an app maps them to its own sentences.
package provision

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// DefaultTimeout bounds one console attempt when Config.Timeout is zero.
const DefaultTimeout = 30 * time.Second

const (
	// maxBody caps what is read of an answer. The console's answers are a
	// record or one sentence; a body larger than this is not one of them,
	// and reading it whole would let a misbehaving proxy hold the memory.
	maxBody = 64 << 10
	// maxMessage caps Exchange.Message and a non-JSON error, so a proxy's
	// HTML error page does not land whole in a log line or a record.
	maxMessage = 512
)

// Config is what a Console needs.
type Config struct {
	// Console is the console's origin, https:// with no path.
	Console string
	// Account is the account's sqid: an app-bound credential's account
	// segment matches as a sqid only.
	Account string
	App     string
	// Token is the app-bound service credential.
	Token string
	// HTTP is the client to use; http.DefaultClient when nil. Its
	// redirects are never followed.
	HTTP *http.Client
	// Timeout bounds one console attempt; DefaultTimeout when zero.
	Timeout time.Duration
	// Observe, when set, sees every console exchange.
	Observe func(Exchange)
}

// Exchange is one console attempt, as Observe sees it. It never carries
// the token.
type Exchange struct {
	Method, Path string
	// Status is 0 when the attempt got no answer.
	Status int
	// Header is the response's.
	Header http.Header
	// Message is the error sentence or the body's first 512 bytes.
	Message string
	Err     error
}

// Console calls the console with one credential.
type Console struct {
	cfg Config
	hc  *http.Client
}

// New checks cfg and returns a Console.
func New(cfg Config) (*Console, error) {
	for _, f := range []struct{ name, v string }{
		{"Console", cfg.Console}, {"Account", cfg.Account}, {"App", cfg.App}, {"Token", cfg.Token},
	} {
		if f.v == "" {
			return nil, fmt.Errorf("provision: Config.%s is empty", f.name)
		}
	}
	u, err := url.Parse(cfg.Console)
	if err != nil || u.Scheme != "https" || u.Host == "" || cfg.Console != "https://"+u.Host {
		// The token rides on every request: plain http would send it in
		// the clear, and a path or query would be pasted in front of
		// every route.
		return nil, fmt.Errorf("provision: Config.Console %q is not an https:// origin with no path", cfg.Console)
	}
	if cfg.Timeout < 0 {
		return nil, fmt.Errorf("provision: Config.Timeout %v is negative", cfg.Timeout)
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultTimeout
	}
	// A redirect is never an answer these calls asked for: following one
	// would read some other server's 200 as the console's, and carry the
	// request somewhere the caller did not name.
	return &Console{cfg: cfg, hc: noRedirect(cfg.HTTP)}, nil
}

// noRedirect returns a copy of hc (http.DefaultClient if nil) that hands
// back a redirect as the answer instead of following it. A copy, so the
// caller's client keeps its own policy.
func noRedirect(hc *http.Client) *http.Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	c := *hc
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &c
}

// The refusals an app branches on. An *APIError is one of them by its
// status; ErrHostTaken comes only from Create.
var (
	// ErrHostTaken: the host is an instance of this app that another
	// creation made. Pick a new label.
	ErrHostTaken = errors.New("provision: the host is another creation's instance")
	// ErrConflict is a 409 that is not a collision this call can see,
	// such as instances not being enabled for the app.
	ErrConflict     = errors.New("provision: conflict")
	ErrNotFound     = errors.New("provision: not found")
	ErrUnauthorized = errors.New("provision: unauthorized")
	ErrForbidden    = errors.New("provision: forbidden")
	ErrBadRequest   = errors.New("provision: bad request")
)

// APIError is a refusal from the console.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("provision: console answered %d", e.Status)
	}
	return fmt.Sprintf("provision: console answered %d: %s", e.Status, e.Message)
}

// Is matches the refusal its status names: 400, 413, 415 and 422 are
// ErrBadRequest, 401 ErrUnauthorized, 403 ErrForbidden, 404 ErrNotFound and
// 409 ErrConflict.
func (e *APIError) Is(target error) bool {
	switch e.Status {
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		return target == ErrBadRequest
	case http.StatusUnauthorized:
		return target == ErrUnauthorized
	case http.StatusForbidden:
		return target == ErrForbidden
	case http.StatusNotFound:
		return target == ErrNotFound
	case http.StatusConflict:
		return target == ErrConflict
	}
	return false
}

// RetryError says the outcome is unknown or the platform said try later.
// Keep the host and call again with Resume: a create may have landed, and
// a record that dropped the host would leave that instance an orphan
// nobody owns.
type RetryError struct {
	// Status is the console's answer, 0 when there was none.
	Status int
	// After is the platform's Retry-After, 0 when it gave none.
	After   time.Duration
	Message string
	// Err is the transport's error, nil when the console answered.
	Err error
}

func (e *RetryError) Error() string {
	var b strings.Builder
	b.WriteString("provision: try again")
	if e.Status != 0 {
		fmt.Fprintf(&b, ": console answered %d", e.Status)
	}
	if e.Message != "" {
		b.WriteString(": " + e.Message)
	}
	if e.Err != nil {
		b.WriteString(": " + e.Err.Error())
	}
	if e.After > 0 {
		fmt.Fprintf(&b, " (after %v)", e.After)
	}
	return b.String()
}

func (e *RetryError) Unwrap() error { return e.Err }

// retryError is the try-later answer, written once so Create and
// PublishPaths read Retry-After the same way. Only the seconds form is
// read; the console sends no other.
func retryError(status int, hdr http.Header, msg string) *RetryError {
	re := &RetryError{Status: status, Message: msg}
	if s, err := strconv.Atoi(strings.TrimSpace(hdr.Get("Retry-After"))); err == nil && s > 0 {
		re.After = time.Duration(s) * time.Second
	}
	return re
}

// appPath is the app's console path with each segment escaped, so an
// account, app or host with a slash in it cannot reach another route.
func (c *Console) appPath(rest ...string) string {
	segs := []string{"/api/cli/apps", url.PathEscape(c.cfg.Account), url.PathEscape(c.cfg.App)}
	for _, s := range rest {
		segs = append(segs, url.PathEscape(s))
	}
	return strings.Join(segs, "/")
}

// call makes one console attempt under its own deadline. err is non-nil
// when there was no answer or its body could not be read; status is then
// whatever arrived, and the caller must treat the outcome as unknown.
func (c *Console) call(ctx context.Context, method, path string, body any) (status int, hdr http.Header, msg string, raw []byte, err error) {
	ex := Exchange{Method: method, Path: path}
	defer func() {
		if c.cfg.Observe != nil {
			ex.Status, ex.Header, ex.Message, ex.Err = status, hdr, msg, err
			c.cfg.Observe(ex)
		}
	}()
	var rd io.Reader
	if body != nil {
		b, merr := json.Marshal(body)
		if merr != nil {
			return 0, nil, "", nil, merr
		}
		rd = bytes.NewReader(b)
	}
	actx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(actx, method, c.cfg.Console+path, rd)
	if err != nil {
		return 0, nil, "", nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		// The console refuses a body without it (415), so a cross-site
		// form cannot drive its doors.
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, nil, "", nil, err
	}
	defer resp.Body.Close()
	raw, err = io.ReadAll(io.LimitReader(resp.Body, maxBody))
	return resp.StatusCode, resp.Header, message(raw), raw, err
}

// message is the console's {"error"} sentence or, for a body without one
// (the text/plain 404s, a proxy's page, a success), the body collapsed to
// one line of at most maxMessage bytes, so it fits a log line.
func message(raw []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &e) == nil && e.Error != "" {
		return e.Error
	}
	s := strings.Join(strings.Fields(string(raw)), " ")
	if len(s) > maxMessage {
		// Cut on a rune boundary, so the cut itself never makes a
		// sentence invalid UTF-8 for whatever stores it.
		n := maxMessage
		for n > 0 && !utf8.RuneStart(s[n]) {
			n--
		}
		s = s[:n]
	}
	return s
}
