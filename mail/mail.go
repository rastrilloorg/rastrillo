// Package mail is the framework's one outbound-email surface — the
// third extraction of a shape vitogo (internal/vito/mail), kass
// (internal/mail) and seapointish (smtpMailer) each hand-rolled: a
// one-method Sender interface, a stdlib net/smtp implementation, a
// loudly-labelled log fallback for instances with no relay configured,
// and the header-injection guard all three carried.
//
// Sender's signature deliberately matches keymaildev/signin's Mailer
// interface, so any Sender drops straight into the auth package's
// magic-link flow without an adapter.
//
// Plain text only, one recipient per call. Recording attempts in an
// emails table (vitogo's pattern) is app policy, not framework — wrap a
// Sender if you want a ledger.
//
// Mail to a list needs headers a magic link does not — an unsubscribe
// a mailbox provider will honour, an identity a reader can filter on.
// That is ListMessage and SendList, a closed set of validated headers
// rather than a map: a caller that could add arbitrary headers could
// add a second From or a Bcc, and the injection guard would stop being
// a property of this package.
package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"os"
	"strings"
	"time"
)

// Sender delivers one plain-text email. Implementations must be safe
// for concurrent use.
type Sender interface {
	Send(ctx context.Context, to, subject, body string) error
}

// ListSender additionally delivers a ListMessage. Both senders this
// package returns implement it; a wrapper around a Sender does not
// unless it says so, which is why SendList exists to ask.
type ListSender interface {
	Sender
	SendList(ctx context.Context, m ListMessage) error
}

// Deliverer reports whether a Sender will really put a message on the
// wire. Logged says no. It exists so bulk mail can refuse to start on
// an instance with no relay: a transactional caller shrugs at a magic
// link that was only logged, but a mailing run against Logged records
// several hundred deliveries that never happened, and writes several
// hundred subscriber addresses into the log in plaintext on the way.
//
// A wrapper around a Sender (a ledger, a retry, a throttle) should
// forward Delivers, or IsConfigured stops seeing through it.
type Deliverer interface {
	Sender
	Delivers() bool
}

// IsConfigured reports whether s will really send. A Sender that does
// not implement Deliverer is assumed to deliver — an unknown
// implementation is far more likely to be a real relay than a second
// log fallback, and guessing the other way would have bulk callers
// refuse to run against senders that work.
func IsConfigured(s Sender) bool {
	d, ok := s.(Deliverer)
	return !ok || d.Delivers()
}

// ListMessage is one message to one recipient carrying the headers a
// mailing list needs. Every field is optional except To, Subject and
// Body; an empty field emits no header. There is deliberately no From,
// To-list or Bcc: the envelope belongs to the Sender.
type ListMessage struct {
	To      string
	Subject string
	Body    string

	// ListID identifies the list this message belongs to (RFC 2919),
	// so a reader can filter it into a folder and a provider can see
	// successive mailings as one stream. Domain-shaped and globally
	// unique: "announce.example.com". Angle brackets are added.
	ListID string

	// Unsubscribe is an https URL that unsubscribes the recipient.
	// It emits List-Unsubscribe together with the
	// List-Unsubscribe-Post that makes it RFC 8058 one-click, which is
	// the difference between a reader leaving the list and a reader
	// pressing "report spam" — and that second button damages the
	// sending domain for every other app on it. Gmail and Yahoo
	// require the pair above 5,000 messages a day.
	//
	// The relay must DKIM-sign these two headers for one-click to be
	// honoured; nothing here can check that it does.
	Unsubscribe string

	// MessageID is the message's stable identity ("id@example.com"),
	// so a retried delivery is recognisably the same message rather
	// than a second one. Angle brackets are optional and normalised.
	MessageID string

	// ReplyTo is where replies go when the envelope From is a
	// no-reply address.
	ReplyTo string
}

// SendList sends m through s, which must be a ListSender. It refuses
// rather than quietly falling back to Send: dropping to Send would
// deliver the message without its List-Unsubscribe, which is the one
// header the caller reached for this method to get.
func SendList(ctx context.Context, s Sender, m ListMessage) error {
	ls, ok := s.(ListSender)
	if !ok {
		return fmt.Errorf("rastrillo/mail: %T is a Sender but not a ListSender, so it cannot send list headers", s)
	}
	return ls.SendList(ctx, m)
}

// defaultPort is used when a host is configured without a port — 587
// (submission) rather than 25, since a relay an app points this at is
// far more likely to expect authenticated submission than
// MTA-to-MTA delivery.
const defaultPort = "587"

// defaultTimeout bounds a whole SMTP conversation when the caller's
// context carries no deadline of its own — which is the common case,
// since auth hands its mailer a request context that may well outlive
// the relay's silence, and a scheduled tick often passes
// context.Background. Without a ceiling a wedged relay blocks the
// caller forever: that is what makes a hung dial hold SQLite's single
// writer, and what makes bounded work in a tick impossible to promise.
const defaultTimeout = 30 * time.Second

// SMTP returns a Sender that dials host:port. Auth is PlainAuth when
// user is non-empty, none otherwise (a relay accepting anonymous
// submission from its own network). Port defaults to 587 when empty.
func SMTP(host, port, from, user, pass string) Sender {
	if port == "" {
		port = defaultPort
	}
	return &smtpSender{host: host, port: port, from: from, user: user, pass: pass}
}

type smtpSender struct {
	host, port, from, user, pass string
}

func (s *smtpSender) Send(ctx context.Context, to, subject, body string) error {
	return s.SendList(ctx, ListMessage{To: to, Subject: subject, Body: body})
}

func (s *smtpSender) SendList(ctx context.Context, m ListMessage) error {
	msg, err := message(s.from, m, time.Now())
	if err != nil {
		return err
	}
	if err := s.deliver(ctx, m.To, msg); err != nil {
		return fmt.Errorf("rastrillo/mail: send to %s: %w", m.To, err)
	}
	return nil
}

func (s *smtpSender) Delivers() bool { return true }

// deliver runs the SMTP conversation net/smtp.SendMail runs, with one
// difference: it dials through the context and holds the connection
// itself. SendMail cannot be given a context and dials with no
// deadline at all, so the only way to honour one is to own every step.
// The STARTTLS-if-offered and refuse-AUTH-if-unoffered rules below are
// SendMail's, kept identical on purpose — PlainAuth's own refusal to
// hand credentials to an unencrypted, non-localhost connection still
// applies, since it lives in the auth mechanism rather than here.
func (s *smtpSender) deliver(ctx context.Context, to string, msg []byte) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(s.host, s.port))
	if err != nil {
		return err
	}
	// Every step past the dial is a blocking read or write that
	// net/smtp performs on conn, and there is no way to pass a context
	// into any of them. A deadline covers a relay that goes silent;
	// closing the connection when ctx ends covers a caller that gives
	// up first. Both turn a hang into an error the caller can see.
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	defer context.AfterFunc(ctx, func() { conn.Close() })()

	c, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return err
	}
	defer c.Close()
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: s.host}); err != nil {
			return err
		}
	}
	if s.user != "" {
		if ok, _ := c.Extension("AUTH"); !ok {
			return errors.New("a user is configured but the relay offers no AUTH")
		}
		if err := c.Auth(smtp.PlainAuth("", s.user, s.pass, s.host)); err != nil {
			return err
		}
	}
	if err := c.Mail(s.from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// Logged returns a Sender that writes the message to the log instead of
// sending it — the dev/unconfigured fallback. The log line carries the
// full body: for the magic-link flow that is a live credential, which is
// exactly why every Logged line is labelled unmistakably and FromEnv
// warns when it falls back to this. It answers Delivers false, so bulk
// mail can refuse to run against it rather than log a list.
func Logged(logger *slog.Logger) Sender {
	if logger == nil {
		logger = slog.Default()
	}
	return loggedSender{logger}
}

type loggedSender struct{ logger *slog.Logger }

func (l loggedSender) Send(_ context.Context, to, subject, body string) error {
	l.logger.Warn("rastrillo/mail: NOT SENT (no SMTP configured) — logging instead",
		"to", to, "subject", subject, "body", body)
	return nil
}

func (l loggedSender) SendList(_ context.Context, m ListMessage) error {
	// Validate anyway. A dev instance is where a bad List-Unsubscribe
	// gets written, and finding out only on the box that has a relay
	// is finding out from a mailbox provider.
	if _, err := message("logged@invalid", m, time.Now()); err != nil {
		return err
	}
	l.logger.Warn("rastrillo/mail: NOT SENT (no SMTP configured) — logging instead",
		"to", m.To, "subject", m.Subject, "body", m.Body,
		"list_id", m.ListID, "unsubscribe", m.Unsubscribe, "message_id", m.MessageID)
	return nil
}

func (l loggedSender) Delivers() bool { return false }

// FromEnv builds a Sender from <prefix>_SMTP_{HOST,PORT,FROM,USER,PASS}.
// HOST and FROM are the two values with no sane default: with either
// missing the instance has no relay, and FromEnv returns Logged with a
// loud warning rather than an error — an unconfigured standalone
// instance is the expected common case, not a misconfiguration
// (vitogo's rule). Ask IsConfigured if a caller needs to know which it
// got. These arrive as ordinary env vars, which on the platform means
// `carlos env` / sealed secrets.
func FromEnv(prefix string, logger *slog.Logger) Sender {
	if logger == nil {
		logger = slog.Default()
	}
	host := strings.TrimSpace(os.Getenv(prefix + "_SMTP_HOST"))
	from := strings.TrimSpace(os.Getenv(prefix + "_SMTP_FROM"))
	if host == "" || from == "" {
		logger.Warn("rastrillo/mail: SMTP not configured — outbound email will be logged, not sent",
			"want_env", prefix+"_SMTP_HOST and "+prefix+"_SMTP_FROM")
		return Logged(logger)
	}
	return SMTP(host,
		strings.TrimSpace(os.Getenv(prefix+"_SMTP_PORT")),
		from,
		os.Getenv(prefix+"_SMTP_USER"),
		os.Getenv(prefix+"_SMTP_PASS"))
}

// message builds a minimal RFC 5322 plain-text email. Every header
// value passes through the injection check: a bare CR or LF in a header
// value is header injection's entire attack surface (it starts a new
// header line, or the blank line that starts the body). The three
// source implementations silently stripped the bytes; refusing loudly
// is stricter and surfaces the caller bug instead of mailing a
// mangled header.
func message(from string, m ListMessage, now time.Time) ([]byte, error) {
	var b strings.Builder
	write := func(name, value string) error {
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("rastrillo/mail: %s value %q contains a line break", name, value)
		}
		fmt.Fprintf(&b, "%s: %s\r\n", name, value)
		return nil
	}
	for _, h := range []struct{ name, value string }{
		{"From", from},
		{"To", m.To},
		{"Subject", m.Subject},
	} {
		if err := write(h.name, h.value); err != nil {
			return nil, err
		}
	}
	// RFC 5322 requires a Date, and a message without one reads to a
	// spam filter as machine-generated bulk. Every relay would add its
	// own; ours is the honest one, and the only one under our control.
	if err := write("Date", now.Format(time.RFC1123Z)); err != nil {
		return nil, err
	}
	if m.MessageID != "" {
		id, err := messageID(m.MessageID)
		if err != nil {
			return nil, err
		}
		if err := write("Message-Id", "<"+id+">"); err != nil {
			return nil, err
		}
	}
	if m.ReplyTo != "" {
		if err := write("Reply-To", m.ReplyTo); err != nil {
			return nil, err
		}
	}
	if m.ListID != "" {
		id, err := listID(m.ListID)
		if err != nil {
			return nil, err
		}
		if err := write("List-Id", "<"+id+">"); err != nil {
			return nil, err
		}
	}
	if m.Unsubscribe != "" {
		u, err := unsubscribeURL(m.Unsubscribe)
		if err != nil {
			return nil, err
		}
		if err := write("List-Unsubscribe", "<"+u+">"); err != nil {
			return nil, err
		}
		// The pair is the point: List-Unsubscribe alone is a link a
		// provider may or may not surface, and the Post header is what
		// makes it the one-click button (RFC 8058). Emitting one
		// without the other is the misconfiguration this package
		// exists to make impossible, so they are written together.
		if err := write("List-Unsubscribe-Post", "List-Unsubscribe=One-Click"); err != nil {
			return nil, err
		}
	}
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(m.Body)
	return []byte(b.String()), nil
}

// messageID normalises "id@host" or "<id@host>" to the bare form. The
// brackets are structure rather than value, so accepting both spellings
// and emitting exactly one pair is the only way a caller cannot end up
// with "<<id@host>>" — which is not a message id, and is accepted
// silently by every relay that will then fail to match a retry to it.
func messageID(v string) (string, error) {
	id := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(v), "<"), ">")
	local, domain, ok := strings.Cut(id, "@")
	if !ok || local == "" || domain == "" || strings.ContainsAny(id, "<> \t") {
		return "", fmt.Errorf("rastrillo/mail: message id %q is not id@domain", v)
	}
	return id, nil
}

// listID normalises RFC 2919's identifier the same way, and insists on
// a dot: the header's whole job is to be globally unique across every
// list a mailbox has ever seen, and a bare word ("news") is not.
func listID(v string) (string, error) {
	id := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(v), "<"), ">")
	if !strings.Contains(id, ".") || strings.ContainsAny(id, "<>@ \t") {
		return "", fmt.Errorf("rastrillo/mail: list id %q is not domain-shaped (want announce.example.com)", v)
	}
	return id, nil
}

// unsubscribeURL insists on https. One-click unsubscribe is defined
// over an HTTPS POST (RFC 8058), so a mailto: or http: value would emit
// a List-Unsubscribe-Post that no provider can act on — a header pair
// that looks compliant and is not, which is worse than neither.
func unsubscribeURL(v string) (string, error) {
	u := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(v), "<"), ">")
	if !strings.HasPrefix(strings.ToLower(u), "https://") || strings.ContainsAny(u, "<> \t") {
		return "", fmt.Errorf("rastrillo/mail: unsubscribe %q is not an https URL", v)
	}
	return u, nil
}
