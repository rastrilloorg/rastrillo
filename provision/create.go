package provision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// MaxRelabels is how many new labels an app tries after ErrHostTaken
// before it gives up. With 30 random bits a second collision means
// something other than chance, and a counter the app persists stops a
// refresh loop from relabelling for ever.
const MaxRelabels = 2

// maxCreateMessage is the platform's activity message limit, in runes
// (activity.go:27-44).
const maxCreateMessage = 240

// CreateRequest is one create. Host is the whole instance host: the label,
// a dot and the app's instance domain.
type CreateRequest struct {
	Host, Region, Message string
	// Resume says an earlier call for this host may have landed: reconcile
	// with a GET before posting. Every call but the signup's first passes
	// it.
	Resume bool
}

// Created is a create's outcome.
type Created struct {
	Host string
	// Landed: an earlier attempt for this host had created it.
	Landed bool
	// Accepted: the platform took the create and is finishing it; the
	// instance is not serving yet.
	Accepted         bool
	Operation, Phase string
}

// Box is one box serving an instance.
type Box struct {
	Box    string `json:"box"`
	Region string `json:"region"`
}

// Instance is the platform's record of an instance.
type Instance struct {
	Host      string    `json:"host"`
	App       string    `json:"app"`
	Account   string    `json:"account"`
	Region    string    `json:"region"`
	Channel   string    `json:"channel"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	ServedBy  []Box     `json:"served_by"`
	Gated     bool      `json:"gated"`
}

var dnsLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// check refuses what the platform would refuse with a 400, before it is
// sent: a bad request costs one of the credential's 60 calls a minute and
// teaches nothing a local check cannot.
func (req CreateRequest) check() error {
	label, rest, ok := strings.Cut(req.Host, ".")
	if !ok || rest == "" || !dnsLabel.MatchString(label) {
		return fmt.Errorf("%w: host %q is not a DNS label followed by a domain", ErrBadRequest, req.Host)
	}
	if !utf8.ValidString(req.Message) {
		return fmt.Errorf("%w: the message is not valid UTF-8", ErrBadRequest)
	}
	if n := utf8.RuneCountInString(req.Message); n > maxCreateMessage {
		return fmt.Errorf("%w: the message is %d characters, more than %d", ErrBadRequest, n, maxCreateMessage)
	}
	for _, r := range req.Message {
		if unicode.IsControl(r) {
			return fmt.Errorf("%w: the message has a control character", ErrBadRequest)
		}
	}
	return nil
}

// Create creates the instance req.Host names.
//
// It posts at most twice. An attempt with no answer, or a 5xx other than
// 503, may have landed, so before posting again it asks the platform with a GET.
// When the caller's own ctx ends, Create returns ctx's error, and a post
// may have been sent: keep the host and call again with Resume.
func (c *Console) Create(ctx context.Context, req CreateRequest) (Created, error) {
	if err := req.check(); err != nil {
		return Created{}, err
	}
	cr := &creation{c: c, req: req}
	if req.Resume {
		return cr.reconcile(ctx)
	}
	return cr.post(ctx)
}

// creation is one Create call's state.
type creation struct {
	c     *Console
	req   CreateRequest
	posts int
	// unknown: an earlier post in this call may have landed, so a later
	// "already exists" is this call's own instance.
	unknown bool
}

// reconcile asks whether the host exists before anything is posted.
//
// Only an unknown outcome (no answer, a 5xx, a 429) becomes a RetryError.
// An auth failure or a bad request passes through as the *APIError it is:
// spec §7.2 step 3's table has no retry for it, and a revoked token
// reported as RetryError shows the person a retry button for ever.
func (cr *creation) reconcile(ctx context.Context) (Created, error) {
	_, err := cr.c.Instance(ctx, cr.req.Host)
	switch {
	case err == nil:
		return Created{Host: cr.req.Host, Landed: true}, nil
	case errors.Is(err, ErrNotFound):
		return cr.post(ctx)
	}
	return Created{}, err
}

func (cr *creation) post(ctx context.Context) (Created, error) {
	cr.posts++
	body := struct {
		Host    string `json:"host"`
		Region  string `json:"region,omitempty"`
		Message string `json:"message,omitempty"`
	}{cr.req.Host, cr.req.Region, cr.req.Message}
	status, hdr, msg, raw, err := cr.c.call(ctx, http.MethodPost, cr.c.appPath("instances"), body)

	if err != nil || unknownOutcome(status) {
		// The caller gave up: its deadline, not the attempt's, so no
		// retry is earned, and the post may still have landed.
		if cerr := ctx.Err(); cerr != nil {
			return Created{}, cerr
		}
		// The platform writes before it answers, so a lost answer, a late
		// one or a 5xx after the write has often landed. Posting again
		// blind would meet "already exists" and read our own instance as
		// someone else's, and giving up would drop a host that may be live.
		cr.unknown = true
		if cr.posts == 1 {
			return cr.reconcile(ctx)
		}
		if err != nil {
			return Created{}, &RetryError{Status: status, Message: msg, Err: err}
		}
		return Created{}, retryError(status, hdr, msg)
	}

	switch status {
	case http.StatusCreated:
		return Created{Host: cr.req.Host}, nil
	case http.StatusAccepted:
		// The 202 itself is the answer; its body only names the operation,
		// so a body that does not decode still leaves the create accepted.
		var a struct {
			Operation string `json:"operation_id"`
			Phase     string `json:"phase"`
		}
		_ = json.Unmarshal(raw, &a)
		return Created{Host: cr.req.Host, Accepted: true, Operation: a.Operation, Phase: a.Phase}, nil
	case http.StatusConflict:
		return cr.conflict(ctx, hdr, msg)
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		return Created{}, retryError(status, hdr, msg)
	}
	return Created{}, &APIError{Status: status, Message: msg}
}

// unknownOutcome is a create answer that does not say whether the write
// happened: every 5xx but 503. The console gives 500 and 502 after a
// failed check or write, and a proxy in front of it can give 504 or any
// other after the write went through; refusing for good on those would
// strand an instance that may exist. 503 is the platform's "no box has
// room", sent before anything is written, so it stays a try-later.
func unknownOutcome(status int) bool {
	return status >= 500 && status != http.StatusServiceUnavailable
}

// conflict decides a 409 (spec §7.2 step 3). Every 409 has one shape
// (platform gap 1): "already exists", "instances are not enabled", "held by
// another app", a deleted instance's leftover data. A GET can see only the
// first, so it decides: found means the host is an instance of this app.
// An existing host behind a busy claim answers the busy 409, with
// Retry-After, instead (createleftovers.go:43-50), so that one is a
// try-later and never a collision.
//
// A 409 the GET cannot explain is refused for good rather than relabelled:
// "not enabled" would fail under every label.
func (cr *creation) conflict(ctx context.Context, hdr http.Header, msg string) (Created, error) {
	if hdr.Get("Retry-After") != "" {
		return Created{}, retryError(http.StatusConflict, hdr, msg)
	}
	apiErr := &APIError{Status: http.StatusConflict, Message: msg}
	_, err := cr.c.Instance(ctx, cr.req.Host)
	switch {
	case err == nil:
		if cr.req.Resume || cr.unknown {
			// The host is the operation's identity: an earlier attempt
			// for it made this instance.
			return Created{Host: cr.req.Host, Landed: true}, nil
		}
		return Created{}, fmt.Errorf("%w: %w", ErrHostTaken, apiErr)
	case errors.Is(err, ErrNotFound):
		return Created{}, apiErr
	}
	return Created{}, err
}

// Instance reads the platform's record of host. A 404 is ErrNotFound. No
// answer, a 5xx, a 429, or a 200 that is not a record of host is a
// *RetryError, so a caller deciding whether a create landed never reads
// "could not ask" as "not there", nor "could not tell" as "there". Both
// reconciles in Create go through it.
func (c *Console) Instance(ctx context.Context, host string) (Instance, error) {
	status, hdr, msg, raw, err := c.call(ctx, http.MethodGet, c.appPath("instances", host), nil)
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return Instance{}, cerr
		}
		return Instance{}, &RetryError{Status: status, Message: msg, Err: err}
	}
	switch {
	case status == http.StatusOK:
		var in Instance
		if err := json.Unmarshal(raw, &in); err != nil {
			// A 200 that is not a record did not come from the console's
			// handler; reading it as "found" or "not found" would both be
			// guesses.
			return Instance{}, &RetryError{Status: status, Message: msg, Err: err}
		}
		if !strings.EqualFold(in.Host, host) {
			// json.Unmarshal takes null and {} without error, and a proxy
			// or a stale cache can answer with another host's record. Each
			// would let a reconcile report Landed for an instance that does
			// not exist, and the app would wait for its readiness for ever.
			return Instance{}, &RetryError{Status: status, Message: msg,
				Err: fmt.Errorf("provision: the record names host %q, not %q", in.Host, host)}
		}
		return in, nil
	case status == http.StatusTooManyRequests || status >= 500:
		return Instance{}, retryError(status, hdr, msg)
	}
	return Instance{}, &APIError{Status: status, Message: msg}
}
