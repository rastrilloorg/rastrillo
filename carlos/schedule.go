package carlos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// socketEnv is the instance's control channel — a unix socket the agent
// binds BEFORE it spawns the process, so these calls work from the first
// line of main, which is what lets an app re-assert its timers at boot.
const socketEnv = "CARLOS_CONTROL_SOCKET"

// MaxAhead is how far ahead a one-shot timer may be set. The platform
// enforces the same bound and answers 400 past it; ScheduleAt checks it
// first so the caller gets a typed error without a round trip, and so
// the constant is true of this package rather than merely quoted from
// the other end of the socket.
const MaxAhead = 400 * 24 * time.Hour

var (
	// ErrNotOnCarlos means there is no control socket in the
	// environment: this process is not a CARLOS instance. It is the
	// expected error on a dev machine and in tests, and an app that
	// registers timers at boot should treat it as "skip", not "fail".
	ErrNotOnCarlos = errors.New("rastrillo/carlos: not running on CARLOS ($CARLOS_CONTROL_SOCKET is not set)")

	// ErrUnauthorized means the instance has no $CARLOS_ADMIN_TOKEN, or
	// the agent would not accept the one it has. An instance that was
	// already running when the platform gained these verbs sees this
	// until it is restarted, because the token is minted into the
	// process environment at spawn.
	ErrUnauthorized = errors.New("rastrillo/carlos: $CARLOS_ADMIN_TOKEN is missing or the control socket rejected it")

	// ErrDeclaredSchedule means the name belongs to a recurring schedule
	// declared outside the app (`carlos schedule set`). One-shot timers
	// share that namespace and may not overwrite it — pick another name,
	// or drop the declaration.
	ErrDeclaredSchedule = errors.New("rastrillo/carlos: the name belongs to a declared schedule")

	// ErrTooManyTimers means this host is at its ceiling of pending
	// one-shot timers (1000). Delete finished ones, or move recurring
	// work to a declared schedule.
	ErrTooManyTimers = errors.New("rastrillo/carlos: too many one-shot timers on this host")
)

// StatusError is a control-socket reply with no sentinel of its own.
// The status is a field rather than only prose because the caller's
// next move depends on it and the two directions are opposite: the
// agent's 5xx is a transient registry failure and worth retrying, its
// 4xx is a permanent complaint about this request and retrying it
// forever is the bug. A boot pass re-asserting a few dozen timers has
// to be able to tell them apart.
type StatusError struct {
	Status int    // the agent's HTTP status
	Name   string // the timer the call was for
	Body   string // the agent's message, whitespace-collapsed, up to 512 bytes
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("rastrillo/carlos: schedule %q: %d: %s", e.Name, e.Status, e.Body)
}

// nameRE mirrors the platform's own rule for a timer name. Checking it
// here is not politeness about error messages: name goes into the
// request path, so anything with a slash or a ".." in it would address
// some other verb entirely. Refusing at the boundary means no caller has
// to remember to sanitise a name it built from user input.
var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// defaultTimeout bounds a call whose ctx carries no deadline of its own.
// The peer is a local socket held by the agent, so the realistic failure
// is not slowness but an agent wedged mid-write — and an app re-asserting
// its timers from main with context.Background() would then never finish
// booting. A var, not a const, so the test that proves the default is
// applied does not have to wait ten seconds for it.
var defaultTimeout = 10 * time.Second

// ScheduleAt registers a one-shot timer: at the given instant the
// platform wakes this instance and POSTs to path, exactly as it delivers
// a declared schedule. Guard the handler with [Tick].
//
//	err := carlos.ScheduleAt(ctx, "remind-"+id, when, "/jobs/remind")
//
// It is an upsert by name, so re-registering a timer the app already has
// is harmless — which is what makes re-asserting every pending timer at
// boot a sound thing to do. Timers live in the box's registry rather
// than in the process, so a restart loses none; re-asserting is a
// belt-and-braces pass, not a requirement.
//
// name must match ^[a-z0-9][a-z0-9-]{0,31}$ and is the app's own handle
// on the timer — the only way to cancel or replace it. path must be an
// absolute path on this app. at must be set and no more than [MaxAhead]
// ahead; an at in the past is allowed and fires on the platform's next
// sweep.
//
// The errors worth branching on are [ErrNotOnCarlos] (not a CARLOS
// instance at all), [ErrDeclaredSchedule], [ErrTooManyTimers], and
// [StatusError] for anything else the agent refused.
func ScheduleAt(ctx context.Context, name string, at time.Time, path string) error {
	socket, token, err := control()
	if err != nil {
		return err
	}
	if err := validName(name); err != nil {
		return err
	}
	// A zero time is almost always a field that was never filled in, and
	// the platform would accept it: a past `at` is legal by design and
	// fires on the next sweep. Silently reminding someone at boot is a
	// worse answer than an error the developer can see.
	if at.IsZero() {
		return fmt.Errorf("rastrillo/carlos: schedule %q: at is the zero time", name)
	}
	if at.After(time.Now().Add(MaxAhead)) {
		return fmt.Errorf("rastrillo/carlos: schedule %q: at is more than %d days ahead", name, int(MaxAhead.Hours()/24))
	}
	body, err := json.Marshal(struct {
		At   string `json:"at"`
		Path string `json:"path"`
	}{at.UTC().Format(time.RFC3339), path})
	if err != nil {
		return fmt.Errorf("rastrillo/carlos: encode request: %w", err)
	}
	return do(ctx, socket, token, http.MethodPut, name, body)
}

// ScheduleCancel removes a one-shot timer registered by [ScheduleAt].
// It is idempotent: cancelling a name that has already fired, or was
// never registered, succeeds.
//
// It refuses with [ErrDeclaredSchedule] for the name of a recurring
// schedule — those are removed with `carlos schedule rm`, from outside
// the app, so an app cannot cancel work its operator declared.
func ScheduleCancel(ctx context.Context, name string) error {
	socket, token, err := control()
	if err != nil {
		return err
	}
	if err := validName(name); err != nil {
		return err
	}
	return do(ctx, socket, token, http.MethodDelete, name, nil)
}

// control resolves the environment the verbs need. It runs before any
// argument checking so that off CARLOS — a laptop, a test — a caller
// gets ErrNotOnCarlos, which is the fact it can act on, rather than a
// complaint about an argument that was never going to be sent.
func control() (socket, token string, err error) {
	socket = os.Getenv(socketEnv)
	if socket == "" {
		return "", "", ErrNotOnCarlos
	}
	token = os.Getenv(tokenEnv)
	if token == "" {
		return "", "", ErrUnauthorized
	}
	return socket, token, nil
}

func validName(name string) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf("rastrillo/carlos: name %q: want ^[a-z0-9][a-z0-9-]{0,31}$", name)
	}
	return nil
}

// do is the one request path: dial the control socket, present the
// token, map the platform's status codes onto this package's errors.
func do(ctx context.Context, socket, token, method, name string, body []byte) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
	}

	// The host in the URL is a placeholder: the transport dials the
	// socket regardless of it, and the agent fixed the host this socket
	// speaks for when it bound the listener. Nothing an app sends can
	// name a different one.
	req, err := http.NewRequestWithContext(ctx, method, "http://carlos/schedule/"+name, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("rastrillo/carlos: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}}
	// One call, one connection: this is a rare local request, and a
	// pooled idle conn to a socket the agent may replace under us is a
	// stale-socket bug waiting to happen.
	defer client.CloseIdleConnections()

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("rastrillo/carlos: %s %s: %w", method, socket, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusUnauthorized:
		return ErrUnauthorized
	case resp.StatusCode == http.StatusConflict:
		return ErrDeclaredSchedule
	case resp.StatusCode == http.StatusTooManyRequests:
		return ErrTooManyTimers
	}
	return &StatusError{Status: resp.StatusCode, Name: name, Body: reason(resp.Body)}
}

// reason is the agent's plain-text complaint, bounded and tidied — the
// 400s carry the only description of what was wrong with an `at` or a
// path, and dropping it would leave the caller with a bare status code.
func reason(r io.Reader) string {
	b, err := io.ReadAll(io.LimitReader(r, 512))
	if err != nil || len(bytes.TrimSpace(b)) == 0 {
		return "no detail"
	}
	return strings.Join(strings.Fields(string(b)), " ")
}
