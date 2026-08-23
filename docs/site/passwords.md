# 🤖 Passwords

An email-and-password identity plugin over the
[sessions](/docs/sessions) core. It verifies a credential and calls
`SignIn`; storage, rendering and CSRF stay yours.

## Wiring it

```go
ph, err := password.New(password.Config{
	Sessions:     sess,
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

These routes sit **outside** the `sess.Require` group — a signed-out
visitor has to be able to reach them.

## The two callbacks that touch your database

```go
Lookup func(ctx context.Context, email string) (id int64, hash string, err error)
Create func(ctx context.Context, email, hash string) (int64, error)
```

`Lookup` receives an email already lowercased and trimmed. Return
`sql.ErrNoRows` for an unknown address; `Signin` then treats it exactly
like a wrong password, verifying against a decoy hash so the timing does
not differ either.

`Create` stores a new user and returns the id. **Any** error it returns
is read as a duplicate email — the only realistic failure mode for a
unique-email store — and re-renders accordingly.

`Create` being nil disables signup entirely: `SignupPage` and `Signup`
both answer 404. Useful for an invite-only app.

`RenderSignup` is **required whenever `Create` is set**, and `New`
returns an error otherwise rather than discovering it at request time.

## Rendering

```go
func renderSignin(w http.ResponseWriter, r *http.Request, d password.PageData)
```

`PageData` carries `Error`, `Email` and `ReturnTo` — enough to
re-render the form with the address still filled in and the problem
stated.

**The callback must not write a status.** `password` has already written
the 422 before calling you. Writing a second one produces a superfluous
`WriteHeader` warning and keeps the first.

## Methods, and their verbs

`SigninPage` and `SignupPage` are GET. `Signin`, `Signup` and `Signout`
are **POST-only** and answer 405 to anything else.

Sign-out being POST-only is deliberate: a GET sign-out can be triggered
by any image tag on any page.

## Rate limiting

Sign-in and sign-up share one per-email budget: **10 failures in 15
minutes** answers 429 until one ages out, and a success resets it.

The budget is shared between the two on purpose. Sign-up leaks the same
fact sign-in does — whether an address is already registered — so
letting an attacker switch endpoints to get a fresh allowance would
defeat the limit.

It is in-memory, and so per-process. IP-level throttling is the
deployment's job, not the framework's.

## Passwords at rest

`password.Hash` and `password.Verify` handle the encoding; the
parameters are pinned and asserted by a test, so they cannot drift
quietly.

`password.NeedsRehash(encoded)` reports whether a stored hash predates
the current parameters, so you can upgrade one transparently at the next
successful sign-in — the only moment the plaintext is available.

## Adding a second factor

```go
SecondFactor func(w http.ResponseWriter, r *http.Request, sess sessions.Session) (done bool, err error)
```

Called at the exact point a verified password would mint the session,
with the session that *would* be minted. Return `done=true` when you
have taken over the response — stored a pending half-session and
redirected — and `done=false` when no second factor applies and sign-in
should proceed unchanged. Nil is exactly the behaviour above.

`passkey.Handlers.Gate` is the shipped implementation.
[Passkeys](/docs/passkeys) covers it.
