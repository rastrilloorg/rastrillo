package mail

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// The submit transport is CARLOS platform mail submission: the app holds
// no mail credential, and hands each message to POST /v1/mail on its own
// control socket. The platform's design is
// docs/superpowers/specs/2026-11-12-platform-mail-submission-design.md
// in the CARLOS repository, §4 for the verb.
const (
	controlSocketEnv = "CARLOS_CONTROL_SOCKET"
	adminTokenEnv    = "CARLOS_ADMIN_TOKEN"
	submitURL        = "http://carlos/v1/mail"
	// maxSubmitRaw is the verb's limit on the decoded message. Refusing
	// here gives the caller the reason instead of a bare 400.
	maxSubmitRaw = 10 << 20
	// maxAnswer bounds what is read of the platform's answer, which is a
	// one-field JSON object.
	maxAnswer = 4096
)

// ErrUnconfirmed is a 200 from the platform that does not say which
// message it sent. The message may have gone, so nothing retries it:
// sending it again could deliver it twice.
var ErrUnconfirmed = errors.New("rastrillo/mail: the platform answered " +
	"200 without a message id, so the message may or may not have been " +
	"sent; check the mail log before sending it again")

// The codes the verb answers with, in SubmitError.Code.
const (
	CodeFromNotAllowed    = "FROM_NOT_ALLOWED"
	CodeRouteNotOnBox     = "ROUTE_NOT_ON_BOX"
	CodeSubmissionOff     = "SUBMISSION_OFF"
	CodeCapReached        = "CAP_REACHED"
	CodePaused            = "PAUSED"
	CodeRate              = "RATE"
	CodeMailerUnavailable = "MAILER_UNAVAILABLE"
	CodeBadRequest        = "BAD_REQUEST"
	CodeRejected          = "REJECTED"
)

// submitBackoff is the wait before each retry of a 503, so a 503 is
// tried at most len+1 times. A variable so tests need not sleep.
var submitBackoff = []time.Duration{time.Second, 2 * time.Second,
	2 * time.Second}

// submitSlots holds this process to the verb's two messages in flight
// per control socket. A third would be answered 429 RATE, which nothing
// here retries, so a burst of three sign-ins would lose one link.
var submitSlots = make(chan struct{}, 2)

// SubmitError is a refusal from the platform's mail verb. RetryAfter is
// the platform's Retry-After, zero when it sent none. Nothing in this
// package retries a 429: the platform is saying to slow down, and an
// app that wants to try later keeps its own queue.
type SubmitError struct {
	Status     int
	Code       string
	RetryAfter time.Duration
}

func (refusal *SubmitError) Error() string {
	code := refusal.Code
	if code == "" {
		code = http.StatusText(refusal.Status)
	}
	text := fmt.Sprintf("rastrillo/mail: the platform refused the message: "+
		"%d %s", refusal.Status, code)
	if refusal.RetryAfter > 0 {
		text += fmt.Sprintf(" (retry after %s)", refusal.RetryAfter)
	}
	return text
}

// Temporary reports whether the same message may succeed later: a 429
// (after RetryAfter) or a 503. A 400 or 403 never will.
func (refusal *SubmitError) Temporary() bool {
	return refusal.Status == http.StatusTooManyRequests ||
		refusal.Status == http.StatusServiceUnavailable
}

// Submit returns a Sender that hands each message to the platform's
// mail verb over the control socket at socket. An empty token is read
// from $CARLOS_ADMIN_TOKEN on every send. from is the From address,
// which the platform checks against the domains the app was granted.
func Submit(socket, token, from string) Sender {
	return &submitSender{socket: socket, token: token, from: from}
}

type submitSender struct {
	socket, token, from string
}

func (sender *submitSender) Send(
	ctx context.Context, to, subject, body string,
) error {
	return sender.SendList(ctx,
		ListMessage{To: to, Subject: subject, Body: body})
}

func (sender *submitSender) Delivers() bool { return true }

func (sender *submitSender) SendList(
	ctx context.Context, message ListMessage,
) error {
	envelope, err := sender.envelope(message, time.Now())
	if err != nil {
		return err
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
	}
	if err := sender.submit(ctx, envelope); err != nil {
		return fmt.Errorf("rastrillo/mail: send to %s: %w", message.To, err)
	}
	return nil
}

// submitEnvelope is the verb's body. The field names are the wire.
type submitEnvelope struct {
	From string   `json:"from"`
	To   []string `json:"to"`
	Raw  string   `json:"raw"`
}

// envelope builds the message and refuses, before any round trip, what
// the platform would answer 400 to.
func (sender *submitSender) envelope(
	message ListMessage, now time.Time,
) ([]byte, error) {
	recipient, err := envelopeRecipient(message.To)
	if err != nil {
		return nil, err
	}
	from, err := envelopeFrom(sender.from)
	if err != nil {
		return nil, err
	}
	raw, err := submitMessage(sender.from, message, now)
	if err != nil {
		return nil, err
	}
	return json.Marshal(submitEnvelope{
		From: from,
		To:   []string{recipient},
		Raw:  base64.StdEncoding.EncodeToString(raw),
	})
}

// submit posts the envelope, retrying a 503 after each submitBackoff
// wait that fits in ctx's deadline. Every other answer is final.
func (sender *submitSender) submit(ctx context.Context, body []byte) error {
	for attempt := 0; ; attempt++ {
		err := sender.post(ctx, body)
		var refusal *SubmitError
		if !errors.As(err, &refusal) ||
			refusal.Status != http.StatusServiceUnavailable ||
			attempt >= len(submitBackoff) {
			return err
		}
		wait := submitBackoff[attempt]
		if deadline, _ := ctx.Deadline(); time.Until(deadline) <= wait {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(wait):
		}
	}
}

// post is one request. The URL's host is a placeholder: the transport
// dials the socket, and the agent stamps the message with the route
// that owns it, never anything the request names. This repeats
// rastrillo/carlos's dial rather than sharing it, because that one maps
// statuses onto the schedule verb's errors.
func (sender *submitSender) post(ctx context.Context, body []byte) error {
	select {
	case submitSlots <- struct{}{}:
		defer func() { <-submitSlots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		submitURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+sender.bearer())
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", sender.socket)
		},
	}}
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("POST /v1/mail on %s: %w", sender.socket, err)
	}
	defer response.Body.Close()
	return submitAnswer(response)
}

func (sender *submitSender) bearer() string {
	if sender.token != "" {
		return sender.token
	}
	return os.Getenv(adminTokenEnv)
}

// submitAnswer is nil for a 200 that names the message, ErrUnconfirmed
// for any other 200, and a SubmitError for anything else. A 401 carries
// a plain-text body, not the JSON code, so its Code is empty.
func submitAnswer(response *http.Response) error {
	var answer struct {
		MessageID string `json:"message_id"`
		Code      string `json:"code"`
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, maxAnswer+1))
	decodeErr := json.Unmarshal(raw, &answer)
	if response.StatusCode == http.StatusOK {
		if readErr != nil || len(raw) > maxAnswer || decodeErr != nil ||
			strings.TrimSpace(answer.MessageID) == "" {
			return fmt.Errorf("%w: %q", ErrUnconfirmed, raw[:min(len(raw),
				200)])
		}
		return nil
	}
	return &SubmitError{
		Status:     response.StatusCode,
		Code:       answer.Code,
		RetryAfter: retryAfter(response.Header.Get("Retry-After")),
	}
}

// maxRetryAfter is the longest Retry-After this package will hold, in
// seconds: the largest a time.Duration can carry. A larger value would
// overflow into a negative wait, a retry in the past.
const maxRetryAfter = math.MaxInt64 / int64(time.Second)

// retryAfter reads Retry-After as seconds or as an HTTP date (RFC 9110
// allows both), and is zero when it is neither. Seconds past what a
// Duration holds saturate rather than wrap.
func retryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	seconds, err := strconv.ParseInt(value, 10, 64)
	switch {
	case err == nil:
		return time.Duration(min(max(seconds, 0), maxRetryAfter)) *
			time.Second
	case errors.Is(err, strconv.ErrRange) && !strings.HasPrefix(value, "-"):
		return time.Duration(maxRetryAfter) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		return max(time.Until(when), 0)
	}
	return 0
}
