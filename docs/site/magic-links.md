# 🤖 Magic links and keymail

`rastrillo/auth` is the framework's turnkey sign-in and the family
default: a magic-link email that works for every address, which
auto-upgrades to the keymail ceremony when the address resolves to a
claimed keymail inbox.

It wraps `github.com/keymaildev/signin`, filling in the deliberate holes
that package leaves — link storage, mailer, cookies, sessions, CSRF,
admission — once here, instead of once per app.

## The decision tree

Every submitted address is classified. A claimed keymail inbox gets the
keymail OAuth ceremony; every other address gets a signed link by email.

Classification **fails open** by design. If the probe fails, the address
gets a magic link. Nobody is ever locked out by a classifier being
unreachable.

## Wiring it

Build one `*Auth` at boot, merge its migrations, and mount four
handlers:

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

`auth`'s backfill migration reads the sessions table, so
`sessions.Schema` must apply first. `migrate.Merge`'s argument order is
apply order — see [Migrations](/docs/migrations).

### The sign-in page stays yours

`Begin` and the completion handlers report outcomes by redirecting to
`Config.SigninPath` with a query your page renders:

| Query | Meaning |
|---|---|
| `?sent=1` | a link was emailed |
| `?err=rate` | rate limited |
| `?err=address` | the address was rejected |
| `?err=expired` | the link had expired |
| `?err=keymail` | the keymail approval failed |
| `?force=1` | offer the plain-email escape hatch after a failed keymail approval |

## Configuration worth understanding

**`InstanceKey` must not be empty**, and `New` refuses an empty one. It
seals the pending blob with an HMAC. An empty input hashes to one fixed,
publicly computable value — identical across every deployment that made
the same mistake, and known to anyone reading the source — which would
let an attacker forge a pending blob naming their own keymail server.

**`Origin`** is the OAuth `client_id` that keymail validates redirects
against, the base of emailed links, and what decides the `Secure` and
`__Host-` cookie attributes.

**`Mailer`** is a `mail.Sender`. Nil falls back to `mail.Logged` with a
warning on every send, because an emailed link is a live credential and
logging it is a development-only convenience.

**`Authorize func(address string) bool`** is the admission gate: given a
verified address, may it have a session? Nil admits every verified
address. Membership tables, roles and admin bootstrap are app policy
layered on this hook — not something the framework models for you.

**`SecondFactor`** is the same seam the password plugin has, called at
the point a verified first factor would mint the session.
[Passkeys](/docs/passkeys) covers it.

## The trap: do not use sessions.UserID here

This is the single most expensive mistake to make with this plugin.

Under keymail the session's `Subject` is the **verified email address**,
not a numeric id. So:

```go
uid, ok := sessions.UserID(r) // (0, false) — always, under keymail
```

And the ordinary scoping seam drops that `ok`:

```go
func (a *app) owned(r *http.Request) *gorm.DB {
	uid, _ := sessions.UserID(r) // 0
	return scope.Owned(a.db, uid) // WHERE user_id = 0, for everyone
}
```

Every query in the app is then scoped to `user_id = 0`. Nothing errors.
Users simply see an empty app, or — if any row ever gets written with
owner zero — each other's data.

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

A link is consumed in one `DELETE ... RETURNING` statement. A split
`SELECT`-then-`DELETE` would let two concurrent callers both observe the
row before either deleted it — even at one writer connection — which
would defeat single use.

An unknown hash, a wrong purpose and an expired row are all the same
"not ok". Telling them apart would be an oracle. The row is deleted even
when expired: a presented token is spent either way.
