# Sign-in-time passkey 2FA — design

**Goal:** the second of the site's Pending items: a true second-factor
gate at first sign-in — "a pending half-session between factors" —
completing the passkey story whose step-up half shipped in v0.9.0.

## The shape

The first factor (magic link, keymail, password) verifies as today.
If the account has a passkey enrolled and the app opted in, the plugin
does NOT mint a session: it hands the would-be session to a **gate**,
which stores a pending half-session and redirects to the app's
"confirm with your passkey" page. Only a successful assertion turns
the half-session into a real one.

## The seam: a plugin hook, not a passkey import

`auth.Config` and `password.Config` each gain one optional hook:

    SecondFactor func(w http.ResponseWriter, r *http.Request,
        sess sessions.Session) (done bool, err error)

Called at the exact point the plugin would call `sessions.SignIn`.
`done=true` means the gate took over (pending stored, redirect sent) —
the plugin stops. `done=false` means no second factor applies (nothing
enrolled) — the plugin signs in exactly as before. The hook keeps
auth/password ignorant of passkeys specifically: any future factor
(TOTP, say) implements the same signature. Zero hook = today's
behavior, unchanged.

## The passkey half

`passkey.Config` gains `ConfirmPath` (default `/passkey/confirm` —
the app renders that page). `passkey.Handlers` gains:

- `Gate` — the SecondFactor implementation: not enrolled → (false,
  nil); enrolled → mint a pending half-session (token cookie + hashed
  row in `passkey_pending`: subject, method, safe return_to, 5-minute
  TTL), 303 to ConfirmPath, (true, nil).
- `SignInBegin` / `SignInFinish` — the ceremony pair on the pending
  cookie (purpose "signin", same single-use challenge machinery as
  step-up). Finish verifies the assertion against the pending
  subject's credentials, consumes the pending row
  (DELETE…RETURNING — single use), clears the cookie, and calls
  `sessions.SignIn` with the ORIGINAL first-factor method plus
  "+passkey" (e.g. "magiclink+passkey") and AuthTime now. The JSON
  answer carries `to` (the stored return_to) for the page's JS.

The trust boundary holds: the pending cookie alone opens no door — it
only names who must still assert. A stolen pending token without the
authenticator is a dead end; an expired or already-used pending row
answers 403; challenge subject/purpose binding is unchanged from
step-up. Step-up (upgrade an existing session) and sign-in gate
(mint from a half-session) stay distinct purposes — an assertion
minted for one can never finish the other.

`passkey.Sweep` also clears expired pending rows.

## Proof

Full-ceremony tests via webauthn/authtest: gate passes through when
nothing is enrolled; gate + begin/finish mints a session whose method
carries both factors; pending rows are single-use and expire; a
challenge minted for step-up cannot finish sign-in; finish without a
pending cookie is refused. Plus one test per plugin (auth, password)
that the hook intercepts SignIn and a nil hook changes nothing.

## Out of scope

Recovery codes (an account with a lost authenticator falls back to
the operator today), remembering devices, and requiring 2FA per-app
policy knobs beyond "hook set or not".
