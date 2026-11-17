package mail

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"mime"
	"mime/quotedprintable"
	"net/http"
	netmail "net/mail"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

func TestQuotedLocalPartsSurviveTheEnvelope(t *testing.T) {
	for _, to := range []string{
		`"a b"@example.com`, `"a@b"@example.com`, `"a..b"@example.com`,
		`who@example.com`, `<who@example.com>`, `" "@example.com`,
	} {
		t.Run(to, func(t *testing.T) {
			fake := newFakeControl(t)
			err := fake.sender(testFrom).Send(context.Background(), to,
				"Hi", "body")
			if err != nil {
				t.Fatalf("Send: %v", err)
			}
			want, _ := netmail.ParseAddress(to)
			got, err := netmail.ParseAddress(fake.received[0].To[0])
			if err != nil || got.Address != want.Address {
				t.Errorf("envelope to %q parses to %v, %v; want %q",
					fake.received[0].To[0], got, err, want.Address)
			}
		})
	}
}

func TestQuotedLocalPartsSurviveTheFrom(t *testing.T) {
	from := `"no reply"@app.example`
	fake := newFakeControl(t)
	err := fake.sender(from).Send(context.Background(), "who@example.com",
		"Hi", "body")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	got, err := netmail.ParseAddress(fake.received[0].From)
	if err != nil || got.Address != "no reply@app.example" {
		t.Errorf("envelope from %q parses to %v, %v",
			fake.received[0].From, got, err)
	}
}

func TestNonASCIIRoundTripsThroughMIME(t *testing.T) {
	from := "Oficina Páginas <docs@app.example>"
	list := ListMessage{
		To: "who@example.com", Subject: "Olá: café, ñandú — 日本語",
		ReplyTo: "Équipe <team@app.example>",
		Body:    "Olá\nlinha dois é longa " + strings.Repeat("ç", 200) + "\n",
	}
	raw := submittedRaw(t, from, list)
	header, _, _ := bytes.Cut(raw, []byte("\r\n\r\n"))
	if !ascii(string(header)) {
		t.Errorf("the header block is not ASCII:\n%s", header)
	}
	if _, err := platformHeaderBlock(raw); err != nil {
		t.Errorf("the platform refuses the encoded header: %v", err)
	}
	if _, ok := platformOneFrom(raw, testFromOf(t, from)); !ok {
		t.Errorf("the platform's From check fails:\n%s", header)
	}
	parsed, err := netmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	var decoder mime.WordDecoder
	subject, err := decoder.DecodeHeader(parsed.Header.Get("Subject"))
	if err != nil || subject != list.Subject {
		t.Errorf("Subject decodes to %q, %v", subject, err)
	}
	for field, want := range map[string]string{
		"From": "Oficina Páginas", "Reply-To": "Équipe",
	} {
		address, err := parsed.Header.AddressList(field)
		if err != nil || len(address) != 1 || address[0].Name != want {
			t.Errorf("%s decodes to %v, %v; want name %q", field, address,
				err, want)
		}
	}
	if encoding := parsed.Header.Get("Content-Transfer-Encoding"); encoding !=
		"quoted-printable" {
		t.Errorf("Content-Transfer-Encoding = %q", encoding)
	}
	assertQuotedPrintableBody(t, parsed.Body, list.Body)
}

// assertQuotedPrintableBody decodes body and wants the original with
// CRLF line endings, from lines no longer than RFC 5322's 78 plus CRLF.
func assertQuotedPrintableBody(t *testing.T, body io.Reader, want string) {
	t.Helper()
	encoded, _ := io.ReadAll(body)
	for _, line := range strings.Split(string(encoded), "\r\n") {
		if len(line) > 78 || !ascii(line) || strings.Contains(line, "\n") {
			t.Errorf("encoded line %q is not a short ASCII CRLF line", line)
		}
	}
	decoded, err := io.ReadAll(quotedprintable.NewReader(
		bytes.NewReader(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != string(crlf([]byte(want))) {
		t.Errorf("body decodes to %q, want %q", decoded, crlf([]byte(want)))
	}
}

func testFromOf(t *testing.T, from string) string {
	t.Helper()
	address, err := netmail.ParseAddress(from)
	if err != nil {
		t.Fatal(err)
	}
	return address.Address
}

func TestAMalformed200IsUnconfirmedAndNotRetried(t *testing.T) {
	fastBackoff(t)
	for _, body := range []string{
		"", "not JSON", "{}", `{"message_id":""}`, `{"message_id":"  "}`,
		`{"code":"MAILER_UNAVAILABLE"}`,
		`{"message_id":"` + strings.Repeat("x", maxAnswer) + `"}`,
		`{"message_id":"cut"}` + strings.Repeat(" ", maxAnswer),
	} {
		t.Run(body[:min(len(body), 30)], func(t *testing.T) {
			fake := newFakeControl(t)
			fake.verbatim = &body
			err := fake.sender(testFrom).Send(context.Background(),
				"who@example.com", "Hi", "body")
			if !errors.Is(err, ErrUnconfirmed) {
				t.Errorf("err = %v, want ErrUnconfirmed", err)
			}
			if fake.requests() != 1 {
				t.Errorf("requests = %d: an unconfirmed send was retried",
					fake.requests())
			}
		})
	}
}

func TestAWellFormed200IsSent(t *testing.T) {
	body := `{"message_id":"0100018f-ses"}` + "\n"
	fake := newFakeControl(t)
	fake.verbatim = &body
	err := fake.sender(testFrom).Send(context.Background(),
		"who@example.com", "Hi", "body")
	if err != nil {
		t.Errorf("err = %v", err)
	}
}

func TestRetryAfterSaturatesInsteadOfWrapping(t *testing.T) {
	longest := time.Duration(math.MaxInt64/int64(time.Second)) * time.Second
	for value, want := range map[string]time.Duration{
		"":                     0,
		"0":                    0,
		"1":                    time.Second,
		" 3600 ":               time.Hour,
		"-5":                   0,
		"9223372036":           longest,
		"9223372037":           longest,
		"99999999999999999999": longest,
		"-9999999999999999999": 0,
		"soon":                 0,
		time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat): 0,
	} {
		if got := retryAfter(value); got != want {
			t.Errorf("retryAfter(%q) = %s, want %s", value, got, want)
		}
	}
	future := time.Now().Add(2 * time.Hour).UTC().Format(http.TimeFormat)
	if got := retryAfter(future); got < time.Hour || got > 2*time.Hour {
		t.Errorf("retryAfter(%q) = %s, want about 2h", future, got)
	}
}

func TestA200CutOffMidReadIsUnconfirmed(t *testing.T) {
	response := &http.Response{StatusCode: http.StatusOK,
		Body: io.NopCloser(io.MultiReader(
			strings.NewReader(`{"message_id":"0100018f-ses"}`),
			iotest.ErrReader(errors.New("connection reset"))))}
	if err := submitAnswer(response); !errors.Is(err, ErrUnconfirmed) {
		t.Errorf("err = %v, want ErrUnconfirmed", err)
	}
}
