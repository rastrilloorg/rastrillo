# 🤖 Passwords

An email-and-password identity plugin over the
[sessions](/docs/sessions) core. It verifies a credential and calls
`SignIn`; storage, rendering and CSRF stay yours.

## Wiring it

```go
ph, err := password.New(password.Config{
	Sessions:     sess,
	Proof:        guard, // see "The front door" below
	Lookup:       lookupUser(d.G),
	Create:       createUser(d.G),
	RenderSignin: renderSignin,
	RenderSignup: renderSignup,
})
if err != nil {
	return nil, err
}

r.Get("/signin", ph.SigninPage)
r.Post("/signin", ph.Signin)
r.Get("/signup", ph.SignupPage)
r.Post("/signup", ph.Signup)
r.Post("/signout", ph.Signout)
```

These routes sit outside the `sess.Require` group, since a signed-out
visitor has to reach them.

## The two callbacks that touch your database

```go
Lookup func(ctx context.Context, email string) (id int64, hash string, err error)
Create func(ctx context.Context, email, hash string) (int64, error)
```

`Lookup` gets an email already lowercased and trimmed. Return
`sql.ErrNoRows` for an unknown address; `Signin` then treats it exactly
like a wrong password, verifying against a decoy hash so the timing does
not give it away either.

`Create` stores a new user and returns the id. Any error it returns is
read as a duplicate email, the only realistic failure for a
unique-email store — unless it wraps `password.ErrRefused`, in which
case `Signup` renders the refusal's own message verbatim at 403
instead.

Leave `Create` nil and signup is disabled entirely: `SignupPage` and
`Signup` both answer 404. `password.Refuse` is the finer-grained tool
for the same job: an invite-only app can keep `Create` wired up and
refuse only the addresses that never got an invitation, rather than
closing signup outright.

The refusal is there because the duplicate-email answer would be a
*lie* to someone who never had an account. It is not free: a 403 and a
422 can be told apart, so a refused address and a registered one no
longer look the same to a prober, where the duplicate copy made them
identical. Saying a true thing that is distinguishable is the trade,
and it is worth making — but write one refusal message that fits every
refused address, and never interpolate the submitted address into it,
or the 403 becomes a finer oracle than the outcome alone.

`RenderSignup` is required whenever `Create` is set, and `New` returns
an error rather than letting you discover it at request time.

## Rendering

```go
func renderSignin(w http.ResponseWriter, r *http.Request, d password.PageData)
```

`PageData` carries `Error`, `Email`, `ReturnTo` and `Proof` — enough to
re-render the form with the address still filled in and the problem
stated.

Your callback must not write a status. `password` has already written
it before calling you — 422, 403 or 429, depending on the outcome.

## The front door

`password.New` won't start until you decide whether sign-in and sign-up sit behind [pow](/docs/reference/pow). Pass a Guard as `Proof`, or set `ProofOff`. With neither, `New` returns `ErrProofUnset`, which names both fixes. A Guard with `Bind` on is `ErrProofMode`, because these forms have nothing to bind to until the visitor has typed it. If you use [magic links](/docs/magic-links#the-front-door) too, give both the same Guard; their tokens are kept apart by scope (`password.ScopeSignin`, `password.ScopeSignup`).

`Signin` and `Signup` check the challenge before the rate limit. A post that hasn't solved it costs no password hashing, no lookup and no new row, and it spends nobody's budget. A refusal re-renders the form at 422 with "Your browser couldn't finish a security check. Try again.", the email kept and the password cleared, as for any other failure.

Your render callbacks get the challenge in `PageData.Proof`. Render it on the form and hand it to `form-foot`:

```html
<form rst-form method="post" action="/signin"{{with .Proof}} {{.Attrs}}{{end}}>
{{with .Proof}}{{.Fields}}{{end}}
<!-- your fields -->
{{template "form-foot" dict "Submit" "Sign in" "Proof" .Proof}}
</form>
{{with .Proof}}{{.Script}}{{end}}
```

`.Proof` is nil with `ProofOff`, hence the `with`s. With proof of work on, the forms need JavaScript, and `form-foot` renders the `<noscript>` line that says so.

`password` picks the kind of challenge for you. After a refusal it's a recovery challenge, with no honeypot, and it stays one on every later re-render for a visitor who posted one, so a password manager that filled the trap once can't trap them again after a wrong password. Render whatever it hands you.

Every page and re-render goes out with `Cache-Control: no-store`, set before your callback runs. The page carries a single-use token, and a cached copy would be one token for everyone who loaded it.

## Methods and their verbs

`SigninPage` and `SignupPage` are GET. `Signin`, `Signup` and `Signout`
are POST-only and answer 405 to anything else.

Sign-out being POST-only matters: a GET sign-out can be triggered by any
image tag on any page.

## Rate limiting

There are two per-email budgets. Each allows ten failures in fifteen
minutes, then answers 429 until one ages out.

Sign-in and sign-up share the first. Wrong credentials and the
duplicate-email answer both spend it, and either can block both doors.
They share it on purpose: sign-up leaks the same fact sign-in does —
whether an address is already registered — so letting an attacker switch
endpoints for a fresh allowance would defeat the limit.

A `Refuse` refusal spends the second, which gates sign-up alone. A
refusal has to cost something, since `password.Hash` runs before
`Create` and an unmetered refusal path is a way to burn CPU. It must
not cost the shared budget, though: a refused address is one nobody has
an account for yet, so charging sign-in for it would let a stranger who
knows an invitee's address post ten refused signups and hold that
address at 429 on both doors, indefinitely, for about one request every
ninety seconds. Sign-in never looks at it.

A successful sign-up clears both; a successful sign-in clears the
shared one. Both are in-memory, and so per-process. IP-level throttling
is the deployment's job.

## Passwords at rest

`password.Hash` and `password.Verify` handle the encoding, and the
parameters are pinned by a test so they cannot drift quietly.

`password.NeedsRehash(encoded)` tells you whether a stored hash predates
the current parameters, so you can upgrade it transparently at the next
successful sign-in — the only moment you have the plaintext.

## Adding a second factor

```go
SecondFactor func(w http.ResponseWriter, r *http.Request, sess sessions.Session) (done bool, err error)
```

Called at the exact point a verified password would mint the session,
with the session that would be minted. Return `done=true` when you have
taken over the response — stored a pending half-session and redirected —
and `done=false` when no second factor applies and sign-in should carry
on unchanged. Nil is exactly the behaviour above.

`passkey.Handlers.Gate` is the shipped implementation;
[Passkeys](/docs/passkeys) covers it.
