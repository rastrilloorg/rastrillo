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

// MaxAhead is how far ahead the platform accepts an `at`. Past it the
// call fails rather than silently clamping.
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
// booting.
const defaultTimeout = 10 * time.Second

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
// absolute path on this app. at may be up to [MaxAhead] ahead; an at in
// the past fires on the platform's next sweep rather than being refused.
//
// The errors worth branching on are [ErrNotOnCarlos] (not a CARLOS
// instance at all), [ErrDeclaredSchedule] and [ErrTooManyTimers].
func ScheduleAt(ctx context.Context, name string, at time.Time, path string) error {
	body, err := json.Marshal(struct {
		At   string `json:"at"`
		Path string `json:"path"`
	}{at.UTC().Format(time.RFC3339), path})
	if err != nil {
		return fmt.Errorf("rastrillo/carlos: encode request: %w", err)
	}
	return do(ctx, http.MethodPut, name, body)
}

// ScheduleCancel removes a one-shot timer registered by [ScheduleAt].
// It is idempotent: cancelling a name that has already fired, or was
// never registered, succeeds.
//
// It refuses with [ErrDeclaredSchedule] for the name of a recurring
// schedule — those are removed with `carlos schedule rm`, from outside
// the app, so an app cannot cancel work its operator declared.
func ScheduleCancel(ctx context.Context, name string) error {
	return do(ctx, http.MethodDelete, name, nil)
}

// do is the one request path: dial the control socket, present the
// token, map the platform's status codes onto this package's errors.
func do(ctx context.Context, method, name string, body []byte) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf("rastrillo/carlos: name %q: want ^[a-z0-9][a-z0-9-]{0,31}$", name)
	}
	socket := os.Getenv(socketEnv)
	if socket == "" {
		return ErrNotOnCarlos
	}
	token := os.Getenv(tokenEnv)
	if token == "" {
		return ErrUnauthorized
	}
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
	return fmt.Errorf("rastrillo/carlos: schedule %q: %s: %s", name, resp.Status, reason(resp.Body))
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
