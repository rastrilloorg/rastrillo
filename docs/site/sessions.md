# 🤖 Sessions

`sessions` owns signed-in state. It does not know how a session is
earned: an identity plugin verifies a credential and calls `SignIn`, and
that call is the whole plugin contract.

Three plugins ship — [passwords](/docs/passwords),
[magic links and keymail](/docs/magic-links), and
[passkeys](/docs/passkeys) as a second factor — and you can write
another without changing anything here.

## What a session is

A row in SQLite, not a signed cookie. Sign-out and administrative
revocation are therefore real: a deleted row is dead even if the cookie
lives on. Only the SHA-256 hash of a token is stored; the token itself
exists in the cookie and nowhere else.

```go
writer, err := d.G.DB()
if err != nil {
	return err
}
sess, err := sessions.New(sessions.Config{
	DB:     writer,
	Origin: origin,
	Logger: logger,
})
```

`Origin` is your external origin with the scheme —
`https://app.example.com`. It decides the `Secure` and `__Host-` cookie
attributes and nothing else; `sessions` never redirects off it. `TTL`
defaults to 30 days and `SigninPath` to `/signin`.

Build one `*Sessions` per process and share it. Its migrations go into
`BootSchema` — see [Migrations](/docs/migrations).

## CSRF

```go
r.Use(csrf.Protect(origin))
```

Mount it app-wide, above the route groups, so a route you add later is
protected without anyone remembering.

It refuses cross-origin `POST`, `PUT`, `PATCH` and `DELETE`, checking
`Sec-Fetch-Site` first, then `Origin`, then `Referer`. There are no
tokens to mint, nothing to thread through a template, and nothing to get
wrong in a form you wrote in a hurry.

## Guarding routes

Three of them, and the difference matters.

### Require

```go
r.Group(func(r chi.Router) {
	r.Use(sess.Require)
	r.Get("/", a.listNotes)
})
```

A signed-out `GET` or `HEAD` redirects to `SigninPath` with a same-site
`return_to`. Anything else answers 403, since a signed-out `POST` has no
sensible page to be sent to.

### Middleware

Softer: it resolves a session when there is one and blocks nothing. Use
it for a page that renders differently when signed in but is public
either way.

### RequireFresh

```go
r.Use(sess.RequireFresh(15 * time.Minute))
```

`Require` plus step-up. The session must exist and its credential must
have been verified within `maxAge`; past that, a `GET` or `HEAD` goes to
`SigninPath?reauth=1` so your sign-in page can say "confirm it's you",
and anything else answers 403.

Re-signing in calls `SignIn`, which rotates the session with a fresh
timestamp and satisfies the gate. So does a passkey assertion —
[Passkeys](/docs/passkeys) covers using one for step-up instead of a
full re-sign-in.

Freshness is measured from `AuthTime` when the plugin records one, and
from the session row's creation time otherwise. A session is only ever
minted at credential verification, so that fallback is an honest lower
bound, and it is what stops a plugin that never sets `AuthTime` — the
magic link, for one — from redirect-looping forever.

`sessions.Fresh(sess, maxAge, now)` answers the same question directly,
if you want to step up in a handler rather than per route.

## Reading the viewer

```go
uid, ok := sessions.UserID(r)     // int64
sess, ok := sessions.Current(r)   // the whole Session
```

`Session` carries `Subject`, `Method`, `AuthTime` and `At`. `Subject` is
your own identifier for who is signed in, kept as a string so plugins
never have to agree on a numeric type. `Method` names how the credential
was verified, in the plugin's vocabulary: `"password"`, `"keymail"`,
`"magiclink+passkey"`.

`UserID` parses `Subject` as an `int64`. Past `Require` its `ok` holds
only for a plugin whose subject really is a numeric user id. It is not
for keymail, whose subject is an email address — see
[Magic links](/docs/magic-links) for what follows from assuming
otherwise.

## Redirecting after sign-in

```go
http.Redirect(w, r, sessions.SafeReturn(r, "/"), http.StatusSeeOther)
```

Never use a raw `return_to`. `SafeReturn` accepts only a same-site
absolute path — exactly one leading `/`, no scheme, no backslash, no
control characters — and gives you the fallback otherwise.

The control-character rule is not there for show. Browsers strip tab, CR
and LF from a URL before parsing it, so `/\t/evil.example` passes a bare
`//` check and still navigates scheme-relative off-site.

## Housekeeping

```go
sess.Sweep(time.Now())
```

Deletes expired rows. Expired sessions are already refused on read, so
this is hygiene rather than enforcement — call it from a periodic job or
at boot.
