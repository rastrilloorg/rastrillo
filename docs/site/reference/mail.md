# 🤖 mail

`github.com/carlosframework/rastrillo/mail`

The one outbound-email surface.

## Sender

```go
type Sender interface {
	Send(ctx context.Context, to, subject, body string) error
}
```

Signature-compatible with `signin`'s Mailer on purpose, so
[`auth`](/docs/reference/auth) takes either without an adapter.

## SMTP and FromEnv

```go
func SMTP(host, port, from, user, pass string) Sender
func FromEnv() (Sender, error)
```

`FromEnv` builds an SMTP sender from the environment, which is how a
deployed app is normally configured.

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
