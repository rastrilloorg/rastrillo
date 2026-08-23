# 🤖 mail

`github.com/carlosframework/rastrillo/mail`

The one outbound-email surface. Four symbols.

## Sender

```go
type Sender interface {
	Send(ctx context.Context, to, subject, body string) error
}
```

Deliberately signature-compatible with `signin`'s Mailer, so
[`auth`](/docs/reference/auth) can take either without an adapter.

## SMTP and FromEnv

```go
func SMTP(host, port, from, user, pass string) Sender
func FromEnv() (Sender, error)
```

`FromEnv` builds an SMTP sender from the environment, which is how a
deployed app is normally configured.

**Header injection is refused.** A `to` or `subject` containing a
newline is rejected rather than sent — that is the whole of the CRLF
injection class, and refusing at the boundary means no caller has to
remember to sanitise.

## Logged

```go
func Logged(logger *slog.Logger) Sender
```

Writes the message to the log instead of sending it, with a warning on
every send.

This is the fallback [`auth`](/docs/reference/auth) uses when no mailer
is configured, and the warning is not noise: a magic link is a live
credential, so an app logging them is one misconfiguration away from
putting credentials in a log aggregator. Development only, and it says
so every time.
