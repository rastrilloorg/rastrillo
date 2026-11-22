# 🤖 lastsignin

`amadan.net/rastrillo/rastrillo/lastsignin`

Remembers, in one browser, which way it last used to sign in (keymail,
an emailed link, a passkey, a federated provider, an authenticator app
or a passphrase) and the address of
the person who signed in, when the caller has one, so the sign-in screen
can offer a one-tap. It is a hint for the screen and
nothing more: it signs nobody in, and a one-tap is checked from scratch.

You rarely build one yourself. `auth.New` builds the jar from its own
configuration, and `auth.RememberJar` hands it to
`passkey.Config.Remember`.

```go
func New(cfg Config) (*Jar, error)
type Config struct {
	Origin, InstanceKey, AttemptCookie string
	Mode                               Mode // Off, Forgetting, On
	Now                                func() time.Time
}
func (j *Jar) Mode() Mode
func (j *Jar) CookieName() string
func (j *Jar) Remember(w http.ResponseWriter, rec Record)
func (j *Jar) Read(r *http.Request) (Record, ReadResult) // Absent, Valid, Invalid
func (j *Jar) Clear(w http.ResponseWriter)
func (j *Jar) EndAttempt(w http.ResponseWriter)
type Record struct{ Method, Address, Provider string }
// MethodKeymail, MethodMagicLink, MethodPasskey, MethodFederated,
// MethodTOTP, MethodPassphrase
const ProviderOficina = "oficina"
func Provider(name string) bool
```

`Remember` writes exactly the record you pass and reads nothing of the
one it replaces. Keymail, a link, `MethodTOTP` and `MethodPassphrase`
always carry an address: the last two are the authenticator app and
the passphrase, whose doors show it filled in. Give a
passkey or federated record an address only when you have resolved it
for the person who just signed in; otherwise leave it empty. A federated
record names its `Provider`, which `Provider` must accept.
`AttemptCookie` is optional: leave it empty when your sign-in keeps no
attempt cookie, and `EndAttempt` does nothing.

`Off` writes, reads and deletes nothing, so an app without the shipped
screen sees no cookie at all. `Forgetting` deletes what was remembered
and never reads it. `On` remembers. `EndAttempt` deletes the screen's
attempt cookie; every sign-in path calls it once a first factor checks
out.

The cookie is sealed, HttpOnly, and lasts 400 days, the longest browsers
allow; each sign-in renews it. Signing out does not delete it. A build
from before `MethodFederated`, `MethodTOTP` or `MethodPassphrase`, or
before a passkey record could carry an address, reads those records as
invalid and clears them.
