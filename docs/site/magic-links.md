# 🤖 Magic links

`rastrillo/auth` signs people in by emailing them a link. No password to
choose, store, or reset.

It is the framework's turnkey option: you get the whole flow — link
minting, single-use redemption, rate limiting, sessions, CSRF — and you
keep your own sign-in page.

If you read nothing else on this page, read
[the trap](#the-trap-do-not-use-sessions-userid-here). It costs people
a working app.

## Wiring it

Build one `*Auth` at boot, merge its migrations, mount four handlers:

```go
a, err := auth.New(auth.Config{
	DB:          writer,
	Origin:      origin,
	InstanceKey: instanceKey,
	Mailer:      mailer,
})
if err != nil {
	return nil, err
}

r.Post("/signin", a.Begin)
r.Get("/auth/callback", a.Callback)
r.Get("/auth/verify", a.Verify)
r.Post("/signout", a.Signout)
```

The migration order is not optional:

```go
var BootSchema = migrate.Merge(sessions.Schema, auth.Schema, Schema)
```

auth's backfill migration reads the sessions table, so `sessions.Schema`
has to apply first. `migrate.Merge`'s argument order is apply order —
see [Migrations](/docs/migrations).

### The sign-in page stays yours

`Begin` and the completion handlers report outcomes by redirecting to
`Config.SigninPath` with a query your page renders:

| Query | Meaning |
|---|---|
| `?sent=1` | a link was emailed |
| `?err=rate` | rate limited |
| `?err=address` | the address was rejected |
| `?err=expired` | the link had expired |

## Configuration worth understanding

`InstanceKey` must not be empty, and `New` refuses an empty one. It
seals the pending blob with an HMAC, and an empty input hashes to one
fixed, publicly computable value — identical across every deployment
that made the same mistake.

`Origin` is the base of emailed links, and what decides the cookie
attributes.

`Mailer` is a `mail.Sender`. Leave it nil and you get `mail.Logged` with
a warning on every send, because an emailed link is a live credential
and logging it is a development-only convenience.

`Authorize func(address string) bool` is the admission gate: given a
verified address, may it have a session? Nil admits every verified
address. Membership tables, roles and admin bootstrap are your policy
layered on this hook, not something the framework models for you.

`SecondFactor` is the same seam the password plugin has.
[Passkeys](/docs/passkeys) covers it.

## The trap: do not use sessions.UserID here

This is the most expensive mistake you can make with this plugin.

The session's `Subject` is the **verified email address**, not a numeric
id. So:

```go
uid, ok := sessions.UserID(r) // (0, false) — always, under this plugin
```

And the ordinary scoping seam drops that `ok`:

```go
func (a *app) owned(r *http.Request) *gorm.DB {
	uid, _ := sessions.UserID(r) // 0
	return scope.Owned(a.db, uid) // WHERE user_id = 0, for everyone
}
```

Every query in your app is now scoped to `user_id = 0`. Nothing errors.
Users just see an empty app — or, if any row ever gets written with
owner zero, each other's data.

Read the viewer with `auth.From(r)` or `sessions.Current(r)` instead
(`RequireSession` stashes both), and map the address to your user row's
id before scoping:

```go
func (a *app) owned(r *http.Request) *gorm.DB {
	id, ok := auth.From(r)
	if !ok {
		return a.db.Where("1 = 0")
	}
	return scope.OwnedBy(a.db, "user_id", a.userIDFor(id.Address))
}
```

## Guarding routes

```go
r.Use(a.RequireSession)
```

and `a.RequireFreshSession(maxAge)` for step-up, matching
`sessions.Require` and `sessions.RequireFresh` — see
[Sessions](/docs/sessions).

## Links are single-use

A link is consumed in one `DELETE ... RETURNING`. A split
`SELECT`-then-`DELETE` would let two concurrent callers both see the row
before either deleted it, even at one writer connection, which would
defeat single use.

An unknown hash, a wrong purpose and an expired row all come back as the
same "not ok"; telling them apart would be an oracle. The row is deleted
even when expired, because a presented token is spent either way.

## Rate limiting

A per-address budget, the same shape the
[password plugin](/docs/passwords) uses: repeated failures answer 429
until one ages out, and a success resets it. In-memory, so per-process.
IP-level throttling is the deployment's job.

## Aside: the keymail upgrade

`rastrillo/auth` wraps `github.com/keymaildev/signin`, and if a
submitted address turns out to have a claimed
[keymail](https://keymail.dev) inbox, sign-in upgrades itself to
keymail's OAuth ceremony instead of emailing a link.

You almost certainly do not need to think about this. Keymail has a
small user base, the upgrade is automatic, and the plugin behaves
identically either way from your app's side — same handlers, same
session, same `Subject`.

Two details if you do care. Classification **fails open**: if the probe
fails, the address gets an ordinary magic link, so nobody is locked out
by a classifier being unreachable. And two more outcome queries can
reach your sign-in page — `?err=keymail` when an approval fails, and
`?force=1` to offer the plain-email escape hatch after one.

`Config.Origin` doubles as the OAuth `client_id` that keymail validates
redirects against.
