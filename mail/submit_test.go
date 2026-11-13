package mail

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

const testFrom = "noreply@app.example"

// fastBackoff makes the 503 retries wait a millisecond each, so the
// retry count can be tested without the real seconds.
func fastBackoff(t *testing.T) {
	previous := submitBackoff
	submitBackoff = []time.Duration{time.Millisecond, time.Millisecond,
		time.Millisecond}
	t.Cleanup(func() { submitBackoff = previous })
}

func TestSubmitPostsTheEnvelopeTheVerbReads(t *testing.T) {
	fake := newFakeControl(t)
	err := fake.sender(testFrom).Send(context.Background(),
		"who@example.com", "Your sign-in link", "Open this.\n")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if fake.requests() != 1 {
		t.Fatalf("requests = %d, want 1", fake.requests())
	}
	env, raw := fake.received[0], fake.raws[0]
	if env.From != testFrom || len(env.To) != 1 ||
		env.To[0] != "who@example.com" {
		t.Errorf("envelope = %+v", env)
	}
	if from, ok := platformOneFrom(raw, env.From); !ok || from != testFrom {
		t.Errorf("From: header %q does not match the envelope", from)
	}
	if !bytes.Contains(raw, []byte("Subject: Your sign-in link\r\n")) {
		t.Errorf("raw message lost its subject:\n%s", raw)
	}
}

func TestSubmitReadsTheTokenOnEverySend(t *testing.T) {
	fake := newFakeControl(t)
	sender := Submit(fake.socket, "", testFrom)
	t.Setenv("CARLOS_ADMIN_TOKEN", "stale")
	err := sender.Send(context.Background(), "who@example.com", "Hi", "x")
	var refusal *SubmitError
	if !errors.As(err, &refusal) ||
		refusal.Status != http.StatusUnauthorized {
		t.Fatalf("with a wrong token: %v, want a 401 SubmitError", err)
	}
	t.Setenv("CARLOS_ADMIN_TOKEN", fake.token)
	err = sender.Send(context.Background(), "who@example.com", "Hi", "x")
	if err != nil {
		t.Fatalf("after the token changed: %v", err)
	}
}

func TestSubmitTurnsEachRefusalIntoATypedError(t *testing.T) {
	fastBackoff(t)
	for _, test := range []struct {
		answer    fakeAnswer
		temporary bool
		wait      time.Duration
		requests  int
	}{
		{fakeAnswer{400, CodeBadRequest, ""}, false, 0, 1},
		{fakeAnswer{400, CodeRejected, ""}, false, 0, 1},
		{fakeAnswer{403, CodeFromNotAllowed, ""}, false, 0, 1},
		{fakeAnswer{403, CodeRouteNotOnBox, ""}, false, 0, 1},
		{fakeAnswer{403, CodeSubmissionOff, ""}, false, 0, 1},
		{fakeAnswer{429, CodeCapReached, "3600"}, true, time.Hour, 1},
		{fakeAnswer{429, CodePaused, "60"}, true, time.Minute, 1},
		{fakeAnswer{429, CodeRate, "1"}, true, time.Second, 1},
		{fakeAnswer{503, CodeMailerUnavailable, ""}, true, 0, 4},
	} {
		t.Run(test.answer.code, func(t *testing.T) {
			fake := newFakeControl(t, test.answer)
			err := fake.sender(testFrom).Send(context.Background(),
				"who@example.com", "Hi", "body")
			var refusal *SubmitError
			if !errors.As(err, &refusal) {
				t.Fatalf("err = %v, want a SubmitError", err)
			}
			if refusal.Status != test.answer.status ||
				refusal.Code != test.answer.code ||
				refusal.RetryAfter != test.wait ||
				refusal.Temporary() != test.temporary {
				t.Errorf("refusal = %+v, temporary %v", refusal,
					refusal.Temporary())
			}
			if fake.requests() != test.requests {
				t.Errorf("requests = %d, want %d", fake.requests(),
					test.requests)
			}
		})
	}
}

func TestSubmitNeverRetriesA429(t *testing.T) {
	fake := newFakeControl(t,
		fakeAnswer{http.StatusTooManyRequests, CodeCapReached, "3600"},
		fakeAnswer{status: http.StatusOK})
	started := time.Now()
	err := fake.sender(testFrom).Send(context.Background(),
		"who@example.com", "Hi", "body")
	var refusal *SubmitError
	if !errors.As(err, &refusal) || refusal.RetryAfter != time.Hour {
		t.Fatalf("err = %v, want 429 with RetryAfter 1h", err)
	}
	if fake.requests() != 1 {
		t.Errorf("requests = %d: a 429 was retried", fake.requests())
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Errorf("Send took %s: it waited on a 429", elapsed)
	}
}

func TestSubmitRetriesA503AtMostThreeTimes(t *testing.T) {
	fastBackoff(t)
	unavailable := fakeAnswer{503, CodeMailerUnavailable, ""}
	fake := newFakeControl(t, unavailable, unavailable, unavailable,
		fakeAnswer{status: http.StatusOK})
	err := fake.sender(testFrom).Send(context.Background(),
		"who@example.com", "Hi", "body")
	if err != nil || fake.requests() != 4 {
		t.Fatalf("three 503s then 200: err %v after %d requests, "+
			"want success after 4", err, fake.requests())
	}
	fake = newFakeControl(t, unavailable)
	err = fake.sender(testFrom).Send(context.Background(),
		"who@example.com", "Hi", "body")
	if fake.requests() != 4 || err == nil {
		t.Fatalf("always 503: err %v after %d requests, want an error "+
			"after 4", err, fake.requests())
	}
}

func TestSubmitDoesNotWaitPastTheDeadlineFor503(t *testing.T) {
	fake := newFakeControl(t,
		fakeAnswer{503, CodeMailerUnavailable, ""},
		fakeAnswer{status: http.StatusOK})
	ctx, cancel := context.WithTimeout(context.Background(),
		300*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := fake.sender(testFrom).Send(ctx, "who@example.com", "Hi", "x")
	var refusal *SubmitError
	if !errors.As(err, &refusal) || refusal.Status != 503 {
		t.Fatalf("err = %v, want the 503", err)
	}
	if fake.requests() != 1 || time.Since(started) > 250*time.Millisecond {
		t.Errorf("%d requests in %s: it waited for a retry that could "+
			"not fit", fake.requests(), time.Since(started))
	}
}

func TestSubmitKeepsAtMostTwoMessagesInFlight(t *testing.T) {
	fake := newFakeControl(t)
	fake.hold = make(chan struct{})
	var group sync.WaitGroup
	errs := make(chan error, 5)
	for range 5 {
		group.Go(func() {
			errs <- fake.sender(testFrom).Send(context.Background(),
				"who@example.com", "Hi", "body")
		})
	}
	time.Sleep(100 * time.Millisecond)
	close(fake.hold)
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("a concurrent send failed: %v", err)
		}
	}
	if fake.peak > 2 || fake.requests() != 5 {
		t.Errorf("peak %d in flight, %d sent; want at most 2 and all 5",
			fake.peak, fake.requests())
	}
}

func TestSubmitRefusesBeforeDialing(t *testing.T) {
	for name, message := range map[string]ListMessage{
		"display name": {To: "Who <who@example.com>", Subject: "Hi"},
		"no address":   {To: "who", Subject: "Hi"},
		"control byte": {To: "who@example.com", Subject: "Hi\x00there"},
		"escape":       {To: "who@example.com", Subject: "\x1b[31mHi"},
		"delete":       {To: "who@example.com", Subject: "Hi\x7f"},
		"line break":   {To: "who@example.com", Subject: "Hi\r\nBcc: x@y"},
		"too big": {To: "who@example.com", Subject: "Hi",
			Body: strings.Repeat("x", maxSubmitRaw)},
		"too big once encoded": {To: "who@example.com", Subject: "Hi",
			Body: strings.Repeat("é", maxSubmitRaw/4)},
		"line break in Reply-To": {To: "who@example.com",
			ReplyTo: "a@b.example\nBcc: x@y"},
		"bare LF":         {To: "who@example.com", Subject: "Hi\nthere"},
		"UTF-8 recipient": {To: "josé@example.com"},
		"UTF-8 list id":   {To: "who@example.com", ListID: "ñ.example.com"},
		"not UTF-8":       {To: "who@example.com", Subject: "caf\xe9"},
		"bad Reply-To":    {To: "who@example.com", ReplyTo: "nobody"},
	} {
		t.Run(name, func(t *testing.T) {
			fake := newFakeControl(t)
			err := SendList(context.Background(), fake.sender(testFrom),
				message)
			var refusal *SubmitError
			if err == nil || errors.As(err, &refusal) {
				t.Errorf("err = %v, want a local refusal", err)
			}
			if fake.requests() != 0 {
				t.Errorf("it reached the platform")
			}
		})
	}
}

func TestSubmittedMessagesPassThePlatformHeaderRules(t *testing.T) {
	list := ListMessage{
		To: "who@example.com", Subject: "Issue 4: café", Body: "hello",
		ListID: "announce.example.com", MessageID: "issue-4@example.com",
		Unsubscribe: "https://example.com/u/abc",
		ReplyTo:     "Editor <editor@example.com>",
	}
	for name, test := range map[string]struct {
		from    string
		message ListMessage
	}{
		"plain":        {testFrom, ListMessage{To: "who@example.com"}},
		"list":         {testFrom, list},
		"display From": {"Oficina Docs <docs@app.example>", list},
		"utf-8 From":   {"Oficina Páginas <docs@app.example>", list},
		"tab": {testFrom,
			ListMessage{To: "a@b.example", Subject: "a\tb"}},
	} {
		t.Run(name, func(t *testing.T) {
			raw := submittedRaw(t, test.from, test.message)
			if _, err := platformHeaderBlock(raw); err != nil {
				t.Errorf("the platform refuses the header: %v\n%s", err, raw)
			}
			if _, ok := platformOneFrom(raw, test.from); !ok {
				t.Errorf("From: does not match %q:\n%s", test.from, raw)
			}
		})
	}
}

func TestSubmittedBodiesEndLinesWithCRLF(t *testing.T) {
	raw := submittedRaw(t, testFrom, ListMessage{To: "who@example.com",
		Body: "one\ntwo\r\nthree\rfour\n"})
	_, body, _ := bytes.Cut(raw, []byte("\r\n\r\n"))
	if string(body) != "one\r\ntwo\r\nthree\r\nfour\r\n" {
		t.Errorf("body = %q", body)
	}
}

// The copy of the platform's rules must itself refuse what the platform
// refuses, or the test above proves nothing.
func TestThePlatformRulesCopyRefusesBadHeaders(t *testing.T) {
	for _, header := range []string{
		"From : a@b.example\r\n", "From\t: a@b.example\r\n",
		"Subject: a\rb\r\n", "Subject: a\x00b\r\n", " folded: x\r\n",
		"Subject: a\x7f\r\n",
	} {
		raw := []byte(header + "To: c@d.example\r\n\r\nbody")
		if _, err := platformHeaderBlock(raw); err == nil {
			t.Errorf("the copy accepts %q", header)
		}
	}
}

func TestSubmitIsConfigured(t *testing.T) {
	if !IsConfigured(Submit("/nowhere.sock", "token", testFrom)) {
		t.Error("IsConfigured(Submit) = false")
	}
	if _, ok := Submit("/s", "t", testFrom).(ListSender); !ok {
		t.Error("Submit is not a ListSender")
	}
}

// submittedRaw is the raw message a submit Sender would post.
func submittedRaw(t *testing.T, from string, message ListMessage) []byte {
	t.Helper()
	sender := Submit("/unused.sock", "token", from).(*submitSender)
	body, err := sender.envelope(message, time.Now())
	if err != nil {
		t.Fatalf("envelope: %v", err)
	}
	var env platformEnvelopeWire
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(env.Raw)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
