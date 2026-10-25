# Sign-in and sessions

Adding sign-in to an app that has none, step by step: skill/add-signin.md.

## The session core

- Viewer: `sessions.UserID(r)` (int64, ok) or `sessions.Current(r)`
  (Subject, Method, AuthTime, At). Past `Require`, `ok` holds only when
  the plugin's Subject is a numeric user id (see magic links below).
- `s.Require`: signed-out GET/HEAD redirects to `SigninPath` with a
  same-site `return_to`; the rest 403. `s.Middleware` only resolves.
- Sign-in redirects go through `sessions.SafeReturn(r, "/")`, never a
  raw `return_to`. `s.Sweep(time.Now())` deletes expired rows.
- `s.RequireFresh(maxAge)` step-up: past maxAge, GET/HEAD adds
  `reauth=1`; re-signing-in, a `passkey` assertion or a `totp` code
  rotates fresh. 2FA: `secondfactor.New` is the gate;
  `Config.SecondFactor` = `g.Hold`; `g.Add(pk, tp)`; a factor completes
  `g.Pending(r)` via `g.Complete`. Merge `secondfactor.Schema` AFTER
  `passkey.Schema` (it adopts passkey's old tables). Lost factor: a
  recovery code redeems at POST `/signin/recovery`
  (`g.SignInRecovery`), minted by `g.RegenerateRecoveryCodes` behind
  `RequireFresh`. docs/site/second-factors.md

## Password plugin traps

`password.New` needs `Sessions`, `Lookup`, `RenderSignin`; nil `Create`
disables signup, and `RenderSignup` is required whenever `Create` is
set. Any `Create` error reads as a duplicate email unless it wraps
`password.ErrRefused` (via `password.Refuse(msg)`). Signin and Signup
share a per-email failure budget answering 429 (IP throttling belongs
to deployment). `New` needs `Proof` (an unbound `*pow.Guard`, else
`ErrProofMode`) or `ProofOff`, else `ErrProofUnset`.
docs/site/passwords.md

## Magic links

`rastrillo/auth`: sign-in by emailed link, upgrading to the keymail
ceremony where the address has one. `auth.New` with
`Begin`/`Callback`/`Verify`/`Signout` and `RequireSession`, same
`sessions` core, `Proof`/`ProofOff` as for `password`. `Begin` checks
the proof before the limiter, refuses to `?err=check&rec=1`, and
`SigninState(r)` carries `.Proof` (render it) and `.ReturnTo` (post it
as `return_to`). **Mount `Verify` on GET *and* POST:** GET draws a Sign
in button and spends nothing, POST redeems, so a mail scanner can't
spend the link; GET alone 405s. **Under `auth`, never
`sessions.UserID`:** the Subject is the verified email, so it returns
`(0, false)` and the owner seam would scope every query to `user_id =
0`. Use `auth.From(r)` or `sessions.Current(r)` and map the address to
your user's id first. docs/site/magic-links.md

**No database (a home or hub):** `auth.Config{Records, Pepper, Signed: &sessions.Signed{Name, Key}}` keeps users and links in `objrec` and signs 15-minute cookies; mount `a.Refresh` at `/_carlos/refresh`. A path-routed tenant only verifies: `sessions.Config{Signed: &sessions.Signed{Name, Keys}}`. `Blind` stores only the email's HMAC. No `SecondFactor`. docs/site/sessions.md

**Sign-in screen:** set `auth.Config.SigninScreen: true` and render
`{{template "signin" (dict "L" $L "State" .Signin "Brand" .Brand)}}`
(title: `signin-title`) in the `stage` shell, from `st :=
a.SigninState(r)` (set `st.Passkey` if passkey discovery is mounted),
then `a.PrepareSigninResponse(w, st)` before rendering; make that
handler `RenderConfirm` too. Mount `POST /signin/forget` → `a.Forget`;
with passkeys, serve `passkey.JS()` beside `webauthn.JS()`, set both
URLs (`ScriptURL`, `ModuleURL`) on `st.Passkey`, and wire
`passkey.Config.Remember: a.RememberJar()`. An admission check in front
of `Begin` answers refusals with `a.AnswerAsSent(w, r)`, never its own
`?sent=1`.

**CSP:** `form-action 'self'` covers a form's whole redirect chain, so
`Begin`'s 303 to a keymail server is refused. With `SigninScreen` on,
keymail continues on your own page and `form-action` stays `'self'`.
Only a hand-built sign-in page with the screen off restates
`Options.CSP` (it replaces the policy wholesale) with the keymail
origin appended: `default-src 'self'; style-src 'self' 'unsafe-hashes'
'sha256-yJxAE4rjdcckohdlnvecSporPcqS9xOaA4hJxi87LMc='; img-src 'self'
data:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'
https://keymail.dev`. `auth.Config.KeymailServers` limits which keymail
servers are trusted; other addresses get a link.
