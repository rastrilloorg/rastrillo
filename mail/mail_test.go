package mail

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"
)

func TestMessageShape(t *testing.T) {
	msg, err := message("app@example.com", ListMessage{
		To:      "who@example.com",
		Subject: "Hello",
		Body:    "line one\nline two",
	}, time.Date(2026, 9, 5, 11, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("message: %v", err)
	}
	s := string(msg)
	for _, want := range []string{
		"From: app@example.com\r\n",
		"To: who@example.com\r\n",
		"Subject: Hello\r\n",
		"Date: Sat, 05 Sep 2026 11:30:00 +0000\r\n",
		"Content-Type: text/plain; charset=utf-8\r\n",
		"\r\n\r\nline one\nline two",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("message missing %q in:\n%s", want, s)
		}
	}
	// A transactional message must not grow list headers by accident:
	// List-Unsubscribe on a magic link tells a provider the credential
	// is bulk mail.
	for _, unwanted := range []string{"List-", "Message-Id", "Reply-To"} {
		if strings.Contains(s, unwanted) {
			t.Errorf("plain message carries %q:\n%s", unwanted, s)
		}
	}
}

func TestMessageListHeaders(t *testing.T) {
	msg, err := message("app@example.com", ListMessage{
		To:          "who@example.com",
		Subject:     "Issue 4",
		Body:        "hello",
		ListID:      "announce.example.com",
		Unsubscribe: "https://example.com/u/abc",
		MessageID:   "issue-4@example.com",
		ReplyTo:     "editor@example.com",
	}, time.Now())
	if err != nil {
		t.Fatalf("message: %v", err)
	}
	s := string(msg)
	for _, want := range []string{
		"List-Id: <announce.example.com>\r\n",
		"List-Unsubscribe: <https://example.com/u/abc>\r\n",
		"List-Unsubscribe-Post: List-Unsubscribe=One-Click\r\n",
		"Message-Id: <issue-4@example.com>\r\n",
		"Reply-To: editor@example.com\r\n",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("message missing %q in:\n%s", want, s)
		}
	}
	// Every header belongs above the blank line that starts the body.
	head, _, ok := strings.Cut(s, "\r\n\r\n")
	if !ok {
		t.Fatalf("message has no header/body separator:\n%s", s)
	}
	if !strings.Contains(head, "List-Unsubscribe-Post") {
		t.Errorf("list headers landed in the body:\n%s", s)
	}
}

func TestUnsubscribeAndPostAreInseparable(t *testing.T) {
	// One without the other is the misconfiguration the closed set
	// exists to prevent, so there must be no way to ask for it.
	msg, err := message("app@x.com", ListMessage{
		To: "b@x.com", Subject: "s", Body: "b",
		Unsubscribe: "https://x.com/u",
	}, time.Now())
	if err != nil {
		t.Fatalf("message: %v", err)
	}
	s := string(msg)
	if strings.Count(s, "List-Unsubscribe:") != 1 || strings.Count(s, "List-Unsubscribe-Post:") != 1 {
		t.Fatalf("want exactly one of each unsubscribe header:\n%s", s)
	}
}

func TestMessageNormalisesBracketedIDs(t *testing.T) {
	msg, err := message("app@x.com", ListMessage{
		To: "b@x.com", Subject: "s", Body: "b",
		MessageID: "<id@x.com>",
		ListID:    "<news.x.com>",
	}, time.Now())
	if err != nil {
		t.Fatalf("message: %v", err)
	}
	s := string(msg)
	if !strings.Contains(s, "Message-Id: <id@x.com>\r\n") || strings.Contains(s, "<<") {
		t.Errorf("message id not normalised to one pair of brackets:\n%s", s)
	}
	if !strings.Contains(s, "List-Id: <news.x.com>\r\n") {
		t.Errorf("list id not normalised to one pair of brackets:\n%s", s)
	}
}

func TestMessageRefusesBadListValues(t *testing.T) {
	base := ListMessage{To: "b@x.com", Subject: "s", Body: "b"}
	cases := []struct {
		name string
		m    func(ListMessage) ListMessage
		want string
	}{
		{"http unsubscribe", func(m ListMessage) ListMessage {
			m.Unsubscribe = "http://x.com/u"
			return m
		}, "https"},
		{"mailto unsubscribe", func(m ListMessage) ListMessage {
			m.Unsubscribe = "mailto:leave@x.com"
			return m
		}, "https"},
		{"bare-word list id", func(m ListMessage) ListMessage {
			m.ListID = "news"
			return m
		}, "domain-shaped"},
		{"list id with an address", func(m ListMessage) ListMessage {
			m.ListID = "news@x.com"
			return m
		}, "domain-shaped"},
		{"message id without a domain", func(m ListMessage) ListMessage {
			m.MessageID = "just-an-id"
			return m
		}, "id@domain"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := message("app@x.com", c.m(base), time.Now())
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want one mentioning %q", err, c.want)
			}
		})
	}
}

func TestMessageRefusesHeaderInjection(t *testing.T) {
	base := ListMessage{To: "b@x", Subject: "s", Body: "body"}
	cases := []struct {
		name string
		from string
		m    func(ListMessage) ListMessage
	}{
		{"to", "a@x", func(m ListMessage) ListMessage {
			m.To = "b@x\r\nBcc: victim@x"
			return m
		}},
		{"subject", "a@x", func(m ListMessage) ListMessage {
			m.Subject = "s\nX-Forged: 1"
			return m
		}},
		{"from", "a@x\rX: 1", func(m ListMessage) ListMessage { return m }},
		{"reply-to", "a@x", func(m ListMessage) ListMessage {
			m.ReplyTo = "c@x\r\nBcc: victim@x"
			return m
		}},
		{"unsubscribe", "a@x", func(m ListMessage) ListMessage {
			m.Unsubscribe = "https://x.com/u\r\nBcc: victim@x"
			return m
		}},
		{"list id", "a@x", func(m ListMessage) ListMessage {
			m.ListID = "news.x.com\r\nBcc: victim@x"
			return m
		}},
		{"message id", "a@x", func(m ListMessage) ListMessage {
			m.MessageID = "id@x.com\r\nBcc: victim@x"
			return m
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := message(c.from, c.m(base), time.Now()); err == nil {
				t.Fatal("accepted a header line break")
			}
		})
	}
}

func TestSMTPSenderRefusesInjectionBeforeDialing(t *testing.T) {
	// An injection error must surface before any network attempt: the
	// host below is unroutable, so reaching the dial would hang or fail
	// with a network error instead of the injection error.
	s := SMTP("invalid.invalid", "1", "app@example.com", "", "")
	err := s.Send(context.Background(), "b@x\r\nBcc: v@x", "s", "body")
	if err == nil || !strings.Contains(err.Error(), "line break") {
		t.Fatalf("Send with injected recipient: %v, want the injection error", err)
	}
}

func TestLoggedSenderLabelsItself(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	if err := Logged(logger).Send(context.Background(), "who@example.com", "Sub", "Body"); err != nil {
		t.Fatalf("Logged Send: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "NOT SENT") {
		t.Fatalf("Logged output is not unmistakably labelled: %s", out)
	}
	if !strings.Contains(out, "who@example.com") || !strings.Contains(out, "Body") {
		t.Fatalf("Logged output dropped the message: %s", out)
	}
}

func TestLoggedSendListValidatesAnyway(t *testing.T) {
	// A dev instance is where a bad unsubscribe URL gets written;
	// finding out only on the box with a relay is finding out from a
	// mailbox provider.
	var buf bytes.Buffer
	s := Logged(slog.New(slog.NewTextHandler(&buf, nil)))
	err := SendList(context.Background(), s, ListMessage{
		To: "b@x.com", Subject: "s", Body: "b", Unsubscribe: "http://x.com/u",
	})
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("Logged SendList with an http unsubscribe: %v, want the https error", err)
	}
}

func TestIsConfigured(t *testing.T) {
	if IsConfigured(Logged(slog.Default())) {
		t.Error("Logged reports itself configured; a bulk caller would log a whole list")
	}
	if !IsConfigured(SMTP("relay.example.com", "", "app@x.com", "", "")) {
		t.Error("SMTP reports itself unconfigured")
	}
	if !IsConfigured(senderFunc(nil)) {
		t.Error("an unknown Sender should be assumed to deliver")
	}
}

type senderFunc func(ctx context.Context, to, subject, body string) error

func (f senderFunc) Send(ctx context.Context, to, subject, body string) error {
	return f(ctx, to, subject, body)
}

func TestSendListRefusesAPlainSender(t *testing.T) {
	// Falling back to Send would deliver the message stripped of the
	// unsubscribe header the caller reached for SendList to get.
	err := SendList(context.Background(), senderFunc(func(context.Context, string, string, string) error {
		t.Error("SendList fell back to Send and dropped the list headers")
		return nil
	}), ListMessage{To: "b@x.com", Subject: "s", Body: "b", Unsubscribe: "https://x.com/u"})
	if err == nil || !strings.Contains(err.Error(), "ListSender") {
		t.Fatalf("err = %v, want a refusal naming ListSender", err)
	}
}

func TestSMTPSendListPutsTheHeadersOnTheWire(t *testing.T) {
	got, addr := fakeRelay(t)
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	s := SMTP(host, port, "app@example.com", "", "")
	if err := SendList(context.Background(), s, ListMessage{
		To: "who@example.com", Subject: "Issue 4", Body: "hello",
		ListID:      "announce.example.com",
		Unsubscribe: "https://example.com/u/abc",
		MessageID:   "issue-4@example.com",
	}); err != nil {
		t.Fatalf("SendList: %v", err)
	}
	wire := <-got
	for _, want := range []string{
		"MAIL FROM:<app@example.com>",
		"RCPT TO:<who@example.com>",
		"List-Unsubscribe: <https://example.com/u/abc>",
		"List-Unsubscribe-Post: List-Unsubscribe=One-Click",
		"List-Id: <announce.example.com>",
		"Message-Id: <issue-4@example.com>",
	} {
		if !strings.Contains(wire, want) {
			t.Errorf("the relay never saw %q in:\n%s", want, wire)
		}
	}
}

func TestSMTPSendHonoursContextCancellation(t *testing.T) {
	// A relay that accepts the connection and then says nothing is the
	// wedged relay this used to block on forever: net/smtp.SendMail
	// dials with no deadline and cannot be given a context.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { conn.Close() })
		}
	}()

	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- SMTP(host, port, "app@example.com", "", "").
			Send(ctx, "who@example.com", "s", "b")
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Send against a silent relay returned nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Send ignored the context deadline and blocked on a silent relay")
	}
}

// fakeRelay speaks just enough ESMTP for net/smtp to complete a
// delivery, and hands back everything the client said. It advertises
// neither STARTTLS nor AUTH, which is what a local relay on a trusted
// network looks like.
func fakeRelay(t *testing.T) (<-chan string, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	got := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var seen strings.Builder
		r := bufio.NewReader(conn)
		fmt.Fprint(conn, "220 fake ESMTP\r\n")
		inData := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				break
			}
			seen.WriteString(line)
			if inData {
				if strings.TrimRight(line, "\r\n") == "." {
					inData = false
					fmt.Fprint(conn, "250 queued\r\n")
				}
				continue
			}
			cmd := strings.ToUpper(strings.TrimRight(line, "\r\n"))
			switch {
			case strings.HasPrefix(cmd, "EHLO"):
				fmt.Fprint(conn, "250-fake\r\n250 8BITMIME\r\n")
			case strings.HasPrefix(cmd, "HELO"):
				fmt.Fprint(conn, "250 fake\r\n")
			case strings.HasPrefix(cmd, "DATA"):
				inData = true
				fmt.Fprint(conn, "354 go ahead\r\n")
			case strings.HasPrefix(cmd, "QUIT"):
				fmt.Fprint(conn, "221 bye\r\n")
				got <- seen.String()
				return
			default:
				fmt.Fprint(conn, "250 ok\r\n")
			}
		}
		got <- seen.String()
	}()
	return got, ln.Addr().String()
}
