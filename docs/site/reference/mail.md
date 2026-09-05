# 🤖 mail

`amadan.net/rastrillo/rastrillo/mail`

The one outbound-email surface.

## Sender

```go
type Sender interface {
	Send(ctx context.Context, to, subject, body string) error
}
```

Signature-compatible with `signin`'s Mailer on purpose, so
[`auth`](/docs/reference/auth) takes either without an adapter.

The context is honoured. A whole SMTP conversation is bounded by your
deadline, or by 30 seconds if you pass one without. Before that, a
wedged relay could block the caller indefinitely — long enough to hold
SQLite's single writer, which is why apps ended up with rules about
never sending inside a transaction.

## SMTP and FromEnv

```go
func SMTP(host, port, from, user, pass string) Sender
func FromEnv(prefix string, logger *slog.Logger) Sender
```

`FromEnv` reads `<prefix>_SMTP_{HOST,PORT,FROM,USER,PASS}`, which is how
a deployed app is normally configured. With HOST or FROM missing it
warns and gives you `Logged` instead — an unconfigured standalone
instance is expected, not an error.

Header injection is refused: a `to` or `subject` containing a newline is
rejected instead of sent. That is the whole CRLF injection class, and
refusing at the boundary means no caller has to remember to sanitise.

## Logged

```go
func Logged(logger *slog.Logger) Sender
```

Writes the message to the log instead of sending it, with a warning on
every send.

This is what [`auth`](/docs/reference/auth) falls back to when you
configure no mailer, and the warning matters: a magic link is a live
credential, so an app logging them is one misconfiguration away from
putting credentials in a log aggregator. Development only, and it says
so every time.

## Mail to a list

A magic link needs nothing beyond From, To and Subject. Anything sent to
more than one person needs more than that, so there is a second method:

```go
type ListMessage struct {
	To, Subject, Body string
	ListID            string // announce.example.com
	Unsubscribe       string // https URL
	MessageID         string // id@example.com
	ReplyTo           string
}

func SendList(ctx context.Context, s Sender, m ListMessage) error
```

`Unsubscribe` is the one that matters most. It emits `List-Unsubscribe`
together with the `List-Unsubscribe-Post` that makes it RFC 8058
one-click, and that pair is the difference between a reader leaving your
list and a reader pressing "report spam" — which damages the sending
domain for every other app on it. Gmail and Yahoo require it above 5,000
messages a day and reward it well below that. It must be an `https` URL,
because one-click is defined over an HTTPS POST; a `mailto:` would give
you a header pair that looks compliant and isn't.

`ListID` lets a reader file your mail into a folder and lets a provider
see successive mailings as one stream. `MessageID` makes a retried
delivery recognisably the same message rather than a second one. Both
take angle brackets or not, as you prefer.

The fields are a closed set on purpose. A `map[string]string` of headers
would hand callers a second `From` and a `Bcc`, and would turn the
injection guard from a property of this package into something every
caller has to remember. If you need a header that isn't here, add it
here.

One thing this can't check: your relay has to DKIM-sign the two
unsubscribe headers, or providers won't honour the one-click.

`SendList` takes a `Sender` and refuses if it isn't a `ListSender` —
both senders in this package are. It won't quietly fall back to `Send`,
because that would deliver the message stripped of the unsubscribe
header you called it for. If you wrap a `Sender`, implement
`SendList` to pass it through.

## Before a mailing run

```go
func IsConfigured(s Sender) bool
```

`Logged` returns `nil` from `Send`, which is the right answer for a
magic link on a dev box and the wrong one for a mailing. Run a list
against it and you record several hundred deliveries that never
happened, and write several hundred subscriber addresses into the log
in plaintext on the way.

So check before you start:

```go
if !mail.IsConfigured(sender) {
	return errors.New("no relay configured; refusing to run the mailing")
}
```

A `Sender` this package didn't make is assumed to deliver. If yours
wraps another, implement `Deliverer` and forward `Delivers() bool`, or
`IsConfigured` stops seeing through it.
