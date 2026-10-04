# 🤖 Magic links

`rastrillo/auth` signs people in by emailing them a link. No password to
choose, store, or reset.

It is the framework's turnkey option. You get the whole flow (link
minting, single-use redemption, rate limiting, sessions and CSRF) and a
sign-in screen to use as it is or replace with your own.

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
	Proof:       guard, // see "The front door" below
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

### Use the shipped screen, or your own

Turn on `SigninScreen` and render ui's `signin` partial, and your app has
a sign-in page on its first day: one field for an address, the right
thing happening next whether that address gets a link or Keymail, and a
one-tap for someone coming back.

```go
a, err := auth.New(auth.Config{
	DB:           writer,
	Origin:       origin,
	InstanceKey:  instanceKey,
	Proof:        guard,
	Mailer:       mailer,
	SigninScreen: true,
})

r.Get("/signin", func(w http.ResponseWriter, r *http.Request) {
	st := a.SigninState(r)
	st.Passkey = &auth.PasskeyDoor{ // only if you mounted passkey discovery
		BeginPath:  "/passkey/discover/begin",
		FinishPath: "/passkey/discover/finish",
		ModuleURL:  "/passkey/webauthn.mjs", // serves webauthn.JS()
		ScriptURL:  "/passkey/signin.mjs",   // serves passkey.JS()
	}
	a.PrepareSigninResponse(w, st)
	render(w, "signin", map[string]any{"Signin": st, "Brand": map[string]any{"Name": "Harbour"}})
})
r.Post("/signin/forget", a.Forget)
r.Get("/passkey/webauthn.mjs", serveJS(webauthn.JS()))
r.Get("/passkey/signin.mjs", serveJS(passkey.JS()))
```

```go
// serveJS serves an embedded module with the type a browser requires.
func serveJS(b []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Write(b)
	}
}
```

`templates/signin.html`, in a page set built on the `stage` shell instead of `layout.html`:

```html
{{define "title"}}{{template "signin-title" (dict "State" .Signin "Brand" .Brand)}}{{end}}
{{define "content"}}{{template "signin" (dict "State" .Signin "Brand" .Brand)}}{{end}}
```

```go
stage, _ := ui.Layout("stage")
t := template.Must(template.New("layout").
	Funcs(ui.Funcs(ui.WithIcons(icons.Icon, icons.Assets))).
	Funcs(template.FuncMap{"asset": assets.Path}).
	ParseFS(ui.Templates(), "*.html"))
t = template.Must(t.Parse(string(stage)))
pages["signin"] = template.Must(t.ParseFS(appFS, "templates/signin.html"))
```

`SigninScreen` turns on everything the screen needs from `auth`, together: a short-lived cookie that remembers what was typed, so the page can refill the field after a problem and name the address a link went to; a keymail sign-in that stays on your page instead of leaving the form, so the default CSP's `form-action 'self'` needs no widening; and a cookie that remembers which way this browser got in last. Leave it off and nothing changes: no new cookie, and `Begin` and `Callback` answer exactly as before.

Set `Remember` to a pointer to `false` to keep the screen and stop remembering, for a kiosk or a shared computer. It also deletes anything remembered before. The remembered cookie is a hint for the screen and nothing else: it signs nobody in, and a one-tap is checked from scratch like a typed address.

If you use passkeys, serve `passkey.JS()` beside `webauthn.JS()` and set both addresses on the page's `Passkey` door, as above; the screen loads the passkey script only when it shows the door. Pass `a.RememberJar()` as `passkey.Config.Remember`: a passkey sign-in then clears an address typed earlier in the same browser, and the next visit offers the passkey first.

The `stage` shell is made for this page: one card over a generated backdrop. Redefine its `backdrop` block for a pattern or a picture of your own.

`Begin` and the completion handlers report outcomes by redirecting to `Config.SigninPath`. The shipped screen reads these for you; a page of your own renders them:

| Query | Meaning |
|---|---|
| `?sent=1` | a link was emailed, or the address was refused; the page cannot tell which |
| `?sent=1&attempt=<id>` | the same, with the screen on: the id lets the page name the address |
| `?continue=<id>` | a keymail sign-in is under way; the screen moves on by itself |
| `?err=rate` | rate limited |
| `?err=address` | the address was rejected |
| `?err=expired` | the link had expired or was already used |
| `?err=1` | something else went wrong |
| `?err=check&rec=1` | the front door refused the post; the page shows a recovery challenge, which `SigninState` mints for `rec=1` |
| `?reauth=1` | a fresh sign-in is needed to continue |

#### An admission check in front of Begin

If you put an admission check in front of `Begin`, refusing addresses that are not members before any mail goes out, answer a refusal with `a.AnswerAsSent(w, r)` rather than a redirect of your own. With the screen on, a sent link leaves a cookie and an `attempt=` behind; a plain `?sent=1` for a refusal would look different on the very first try, and anyone could learn who is a member. `AnswerAsSent` checks the challenge first when `Proof` is set, as `Begin` does, then answers exactly as a sent link does and sends nothing, so the page and the cookies give nothing away. It does not stop the per-address rate limit or keymail classification from revealing something about an address; that is separate work, and this does not do it.

## The front door

`auth.New` won't start until you decide whether sign-in sits behind [pow](/docs/reference/pow). Build a Guard and pass it as `Proof`, or set `ProofOff`:

```go
guard, err := pow.New(pow.Config{
	InstanceKey: instanceKey,
	Nonces:      pow.SQLNonces(writer),
	Difficulty:  16,
	MinAge:      500 * time.Millisecond,
	ScriptURL:   "/pow" + powAssets.Path("pow.js"),
	WorkerURL:   "/pow" + powAssets.Path("pow-worker.js"),
})
```

[The pow reference](/docs/reference/pow#wiring-it-up) covers the rest of building it: apply `pow.Schema` before `pow.New`, serve `pow.Assets()`, and sweep the Guard from a background loop. 16 bits and 500ms are what sign-in needs; [Choosing a difficulty](/docs/reference/pow#choosing-a-difficulty) says why. If you use [passwords](/docs/passwords) too, give both the same Guard.

With neither set, `New` returns `ErrProofUnset`, which names both fixes; setting both is an error too. A Guard with `Bind` on is `ErrProofMode`, because the sign-in forms carry the address in different shapes and none of them is bound.

With `Proof` set, `Begin` checks the challenge first: after the same-origin check, and before the rate limit, classifying the address, or any mail. Someone who hasn't solved it can't make your server look up a domain they chose, send a link, or spend another person's rate budget. `Begin` uses `Check` under `auth.ProofScope` (`rastrillo/auth/begin`), so the token is spent before anything goes out.

So the sign-in form needs JavaScript. That's deliberate: if a post without a proof were let through, every bot would take that path. A `NoProof` Guard is accepted, and its button renders enabled, if you want the token and honeypot without the work.

A refusal redirects to `?err=check&rec=1`. The shipped screen shows "Your browser couldn't finish a security check. Try again." on the ask step with the address filled in, and `rec=1` makes `SigninState` mint a recovery challenge, one without the honeypot, so a visitor whose password manager filled the trap gets through next time. Every other error redirect from `Begin` keeps `rec=1` when the post was itself a recovery, so the trap doesn't come back after a mistyped address. And every redirect keeps `force=1` when the post had it: a visitor who chose "send a link instead" after a failed keymail approval, and was then refused, still gets their link instead of being sent back to the provider that just failed them.

`SigninState(r).Proof` is the page's challenge. It's set whenever `Proof` is, with the screen on or off. The `signin` partial renders it on whichever Begin form the state shows (the fields, a disabled button the module enables, the status line, a `<noscript>` line, and the script once), and the Forget form carries none of it. A sign-in page of your own renders `.Proof` as [the pow reference](/docs/reference/pow#rendering-a-form) shows, renders the hidden `force` input when `.Force` is true, and must not be cached: call `PrepareSigninResponse`, which sets `Cache-Control: no-store`, or set the header yourself.

`Begin` logs each refusal at debug level with its reason. Right after an upgrade, `missing` is a sign-in page that was open before the deploy, and the visitor gets in on their second try.

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

Every post to `Begin` that gets past the front door counts against two budgets, whether or not it succeeds: 20 per client IP and 5 per address, each in a fixed 15-minute window that starts with the first attempt. A success doesn't reset either one. Over budget, `Begin` redirects to `?err=rate`.

The IP budget is checked first, so a script hammering the endpoint uses up its own budget before any DNS lookup. The address budget is checked once the address parses, so a typo never spends a real person's allowance. Both are in memory, so they're per process and reset on restart. Behind a proxy, [`TrustedProxyHops`](/docs/reference/clientip) decides which address is the client's.

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

By default any keymail server an address's own domain names is trusted. That is keymail's design: whoever controls a domain chooses its server, the same person who controls its mail and could receive a link anyway, and a server cannot vouch for anyone else's address, because the address it returns must match the one that was typed. To trust a closed set instead, list them in `KeymailServers` (host or host:port). An address whose server is not listed gets an ordinary link, and its server is never contacted, either to check the address or to finish a sign-in. The list applies with the screen on or off.
