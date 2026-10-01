# 🤖 lastsignin

`amadan.net/rastrillo/rastrillo/lastsignin`

Remembers, in one browser, which way it last used to sign in (keymail,
an emailed link or a passkey) and the address for the first two, so the
sign-in screen can offer a one-tap. It is a hint for the screen and
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
type Record struct{ Method, Address string } // MethodKeymail, MethodMagicLink, MethodPasskey
```

`Off` writes, reads and deletes nothing, so an app without the shipped
screen sees no cookie at all. `Forgetting` deletes what was remembered
and never reads it. `On` remembers. `EndAttempt` deletes the screen's
attempt cookie; every sign-in path calls it once a first factor checks
out.

The cookie is sealed, HttpOnly, and lasts 400 days, the longest browsers
allow; each sign-in renews it. Signing out does not delete it.
