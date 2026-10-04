# 🤖 pow

`amadan.net/rastrillo/rastrillo/pow`

`pow` is the front door for a form anyone on the internet can post to. Each form carries a sealed, single-use challenge and a honeypot, and, unless you turn it off, a proof of work the visitor's browser solves while they fill the form in.

`auth` and `password` put sign-in and sign-up behind it by default. This page is about wiring it in front of a form of your own; [Magic links](/docs/magic-links#the-front-door) and [Passwords](/docs/passwords#the-front-door) cover what those two need from you.

## What it is

A challenge is minted when the page renders, and minting writes nothing. A row per render would make every crawler and prefetcher a stream of writes against SQLite's one writer. Instead the challenge is sealed with an HMAC over its scope, nonce, issue time, expiry, difficulty and flags, so the server can trust what comes back without having stored it. When a submission is accepted, its nonce is recorded as spent, so each challenge buys exactly one submission.

The honeypot is an input placed off-screen that people never fill in. It catches the laziest scripts for free, but it is not the control. The proof of work and single use are.

The proof of work makes every submission cost the sender some CPU time. A person doesn't notice, because the solve starts when the page loads and finishes while they type. A script sending thousands of posts pays for every one.

The browser half ships from this module alongside the Go half. The solver in the page and the verifier in Go must hash byte-identical input, and when they disagree the visitor's proof is refused with nothing on the page or in the log to explain it. A browser test runs the shipped solver in Chromium against the Go verifier, and serving both from one module means they can't be different versions of each other.

What it doesn't do: proof of work prices out scripted abuse and nothing more. Even 18 bits is under a millisecond on a commodity GPU, so an attacker who pays for the solves gets through. The defence against that is a persisted budget on whatever the form spends (mail per address, rows per hour, money), and `pow` has none. Its own counters live in memory and reset on restart.

## Wiring it up

Apply the migration first, then build the Guard. `New` checks that the `pow_spent_nonces` table is there and returns `ErrNoSchema` if it isn't, so a forgotten migration fails at boot instead of refusing the first visitor.

```go
if _, err := migrate.Apply(ctx, d, migrate.Merge(sessions.Schema, pow.Schema, app.Schema)); err != nil {
	return err
}

powAssets := rastrillo.NewAssets(pow.Assets())
mux.Handle("GET /pow/", http.StripPrefix("/pow/", powAssets.Handler()))

guard, err := pow.New(pow.Config{
	InstanceKey: instanceKey,
	Nonces:      pow.SQLNonces(writer),
	ScriptURL:   "/pow" + powAssets.Path("pow.js"),
	WorkerURL:   "/pow" + powAssets.Path("pow-worker.js"),
})
if err != nil {
	return err
}
```

Serve the scripts from `pow.Assets()`; don't vendor them. A vendored copy can be edited, or left behind when you upgrade, and then the solver in the page disagrees with the verifier it's checked against. `NewAssets` gives the two entry points content-hashed URLs, and the handler also serves the modules they import by relative name.

`InstanceKey` is the same stored key `auth` takes. `pow` derives its own key from it under a label of its own, so a token minted by another subsystem never verifies here. Keep it stable across restarts: a new key refuses every form already open in a browser (the visitor gets a recovery form and succeeds on the second try), and a key made up per process does that on every deploy.

`Nonces` has no default, because the only possible default is no replay protection. Use `SQLNonces` on your writer. `MemoryNonces` is for tests: it forgets on restart, and it ignores the caller's transaction, so a spend inside a transaction that rolls back stays spent.

The rest of `Config`:

| Field | Default | |
|---|---|---|
| `Difficulty` | `DefaultDifficulty` (18) | Leading zero bits a solution needs. `NoProof` for the token and honeypot without the work. See [Choosing a difficulty](#choosing-a-difficulty). |
| `MinAge` | 3s | A post sooner than this after the challenge was issued is refused as `too_fast`. |
| `MaxAge` | 2h | Sealed into the token when it's issued, so raising it later never revives an old token. |
| `Bind` | off | See below. |
| `Attempts` | 20 | How many times `Admit` accepts one token. |
| `Tracked` | 100,000 | How many tokens `Admit` counts at once. |
| `ScriptURL`, `WorkerURL` | none | Required unless `NoProof`; `New` returns `ErrNoAssets` without them. |

Three seconds of `MinAge` suits a form somebody writes sentences into. A sign-in form is quicker than that, and the module holds a submit until the minimum age has passed, so a returning visitor tapping a one-tap button would sit and wait. The notes example uses 500ms for sign-in and sign-up.

`NoProof` keeps the sealed single-use token and the honeypot and drops the work. The form then needs no JavaScript at all: `Attrs` and `Script` render nothing, `NeedsScript` is false, and the submit is rendered enabled. Use it where a visitor must be able to submit the moment the page renders, like a checkout confirm screen.

`Bind` ties the work to the value of the form's `[data-pow-binding]` input. Leave it off. With single-use tokens one solve already buys one submission, whatever that submission carries, and binding means the work can't start until the visitor has typed the value, so they wait out the whole solve after pressing the button. A bound Guard needs `Want.Binding` on every check. `auth` and `password` refuse a bound Guard at boot (`ErrProofMode`), and `New` refuses `Bind` with `NoProof` (`ErrBindNeedsProof`).

Your app owns the Guard. One Guard can serve every form you have, because each form's scope keeps its tokens apart, and `auth` and `password` take the one you built. Sweep it from a background loop so spent rows don't pile up:

```go
bg := &background.Group{}
bg.Loop(ctx, 10*time.Minute, func() {
	if err := guard.Sweep(time.Now()); err != nil {
		logger.Warn("sweep spent challenges", "err", err)
	}
})
opts.Background = bg // Serve stops the loop before your database closes
```

`Sweep` deletes at most 500 rows a call, and only rows ten minutes past their sealed expiry on the database's clock, so a sweep never holds the writer for long. Every ten minutes keeps up with 3,000 accepted posts an hour; sweep more often if you take more. Nothing depends on it for correctness: an expired token is refused whether or not its row is still there.

## Rendering a form

Mint a `Form` when you render the page, under a scope that names the form:

```go
f := guard.Form(time.Now(), "myapp/contact")
```

The scope is sealed into the token and compared when it comes back, so a token minted for one form is refused by another. Namespace it the way `auth` (`rastrillo/auth/begin`) and `password` (`rastrillo/password/signin` and `.../signup`) do. If the form exists once per event or per account, put that in the scope too.

Then render it:

```html
<form rst-form method="post" action="/contact" {{.Proof.Attrs}}>
  {{.Proof.Fields}}
  <!-- your fields -->
  {{template "form-foot" dict "Submit" "Send" "Proof" .Proof}}
</form>
{{.Proof.Script}}
```

`Fields` is the sealed hidden inputs, the empty input the solver writes its answer into, and the honeypot. Use it rather than writing them yourself. The honeypot has an accessibility contract (`aria-hidden` on its wrapper, `tabindex="-1"`, `autocomplete="off"`, a name no browser autofills), and if any part of it is missing, a screen-reader user or a password manager fills the trap. `Attrs` goes on the `<form>` and tells the module the nonce, the difficulty, the worker's URL and how much of the minimum age is left. `Script` is the module's `<script type="module">` tag: render it once per page, however many forms the page has.

When the form needs a script, `form-foot`'s `Proof` key does the rest: the submit is rendered disabled with `data-pow-submit`, followed by the status line and a `<noscript>` line saying the form needs JavaScript. If you write your own button:

```html
<button type="submit"{{if .Proof.NeedsScript}} disabled data-pow-submit{{end}}>Send</button>
{{.Proof.StatusLine (T "rastrillo.ui.pow_status")}}
{{if .Proof.NeedsScript}}<noscript><p>{{T "rastrillo.ui.pow_noscript"}}</p></noscript>{{end}}
```

A disabled submit is the only starting point that fails safe. JavaScript can't enable a control from inside `<noscript>`, and a module that's blocked by a CSP, missing, or throws leaves an honest disabled button instead of one that looks live and does nothing. The status line ("If this form doesn't respond, reload the page.") shows from the first paint, and the module hides it once the form is ready, so if the module never runs, the line is still there and still true. If the worker fails, the module shows it again. Render it inside the form. A submit button outside the form, attached with `form="..."`, works as long as it carries `data-pow-submit`.

If the visitor submits before the solve or the minimum age is done, the module holds the submit, shows `busy.js`'s busy state on the button (`aria-busy`, the spinner, `data-busy-label`), and sends it with the button they pressed once both are done. A button rendered with `autofocus` gets its focus back when the module enables it, so a one-tap sign-in still answers Enter.

The baseline CSP allows all of this: the worker is same-origin, and the honeypot's inline style is allowed by its hash. If you replace the policy, keep `'unsafe-hashes'` and `pow.HoneypotStyleHash` in `style-src`, or the trap field shows on your form.

**Never cache a page that carries a challenge.** One token on a shared cached page is one token for every visitor, and the first to submit spends it for everyone else. `pow` can't set headers from a template, so set `Cache-Control: no-store` on every such response yourself. `auth`'s `PrepareSigninResponse` and `password`'s handlers already do.

### Forms with nothing left to type

`FollowOn` issues a token that is already past its minimum age. Use it for a form the visitor reaches with nothing left to type, like a prefilled details form or a confirm screen, and for the re-render of either after a validation error. With `Form` instead, a visitor who presses the button straight away would be held for the minimum age. A `FollowOn` form keeps the honeypot.

### After a refusal

Re-render a refused form with `Recovery`. A recovery form never has a honeypot and is always issued past its minimum age, whatever the reason for the refusal: a password manager that filled the trap once will fill it again, and the visitor has already waited once. (An earlier design varied recovery by reason, and the variants undid each other.) Anyone can ask for a recovery form, and that's fine. It costs the same proof of work, and the honeypot was never the control.

Recovery is sticky. Once a visitor has a recovery token, every later re-render of that form for them should be a recovery form too, or a wrong password on the trapless form brings the trap back and they're refused again. `Recovered` tells you the posted token was a recovery token:

```go
func (a *app) contactForm(adm pow.Admission) pow.Form {
	if adm.Recovered() {
		return a.guard.Recovery(time.Now(), contactScope)
	}
	return a.guard.Form(time.Now(), contactScope)
}
```

`pow` recovers the challenge, not the submission. A recovery form is a fresh, empty challenge, and whether you refill it with what the visitor typed is up to you. It's only safe if the write behind the form is idempotent, because the review of this design found several ways that refilling repeats a submission that already succeeded:

- After `spent`, the first submission may well have gone through. Refill the form and the visitor sends it again.
- A refusal can hide an earlier success. The first post was accepted; a second post of the same token with the honeypot filled is refused as `honeypot` before anything checks whether the token was spent, and the refilled recovery form sends the same thing again.
- A request still carrying the original token can succeed after the recovery form was issued, and then both succeed.
- Two refusals give two recovery forms, and each can succeed once.

Guarding against all of these inside `pow` took a sealed submission id, a deadline for it, and a day of retained rows: the form's own idempotency under another name. So make the write idempotent (a submission id with a unique constraint, say), or don't refill after a refusal that could follow a success. Refilling is harmless where repeating the submission is. `auth` and `password` keep the typed email, because a second sign-in only sends another link, or tries the same password under the same rate limit.

## Checking a submission

`Admit` decides and writes nothing. `Commit` spends the token inside your transaction, alongside the write the form is for:

```go
r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
adm := a.guard.Admit(r, pow.Want{Scope: contactScope})
if !adm.OK {
	a.log.Info("contact form refused", "reason", adm.Reason, "also", adm.Also)
	a.renderContact(w, a.guard.Recovery(time.Now(), contactScope), nil)
	return
}
msg, problems := parseContact(r)
if len(problems) > 0 {
	a.renderContact(w, a.contactForm(adm), problems) // nothing spent yet
	return
}

tx, err := a.writer.BeginTx(ctx, nil)
if err != nil {
	// ...
}
defer tx.Rollback()
if err := adm.Commit(ctx, tx); errors.Is(err, pow.ErrSpent) {
	a.renderMaybeSent(w, a.guard.Recovery(time.Now(), contactScope))
	return
} else if err != nil {
	// ...
}
// the business write on tx, then tx.Commit()
```

A validation error never reaches `Commit`, so the visitor's solved token isn't burned, and if the transaction rolls back, so does the spend.

Call `Admit` before you begin the transaction. Its one database read, the spent check, goes through the store's own handle, and rastrillo's writer pool has a single connection: a read queued behind your open transaction waits for ever. `Commit` never reads, for the same reason.

`ErrSpent` means the token was spent by an identical request, or expired before it could be spent; the insert checks expiry on the database's own clock. Refuse it, and never answer 500. Tell the visitor the form may already have been sent and give them a fresh one. `Commit` doesn't tell the two cases apart, because that would take a second read while your transaction holds the connection. `Commit` on a refused admission returns `ErrNotAdmitted`.

`Commit` takes anything with `ExecContext`: `*sql.Tx`, `*sql.DB` or `*sql.Conn`. Inside a GORM transaction, pass `tx.Statement.ConnPool`, not `tx` (`pow` doesn't import GORM). `nil` spends on the store's own handle.

`Admit` runs its checks cheapest first: the honeypot, then the seal before anything the seal vouches for, then the clock and the proof, and only if all of those passed, the spent check and the attempt count. `Reason` is the first failure. `Also` lists the other cheap checks that failed, for your log.

`Check` is `Admit` and `Commit` at once, on the store's own handle. Use it in a handler that redirects after every POST or has no transaction of its own: the next GET mints a new challenge anyway, and the spend lands before the handler sends mail or makes a request. `auth` and `password` use it. Like `Admit`, don't call it while you hold a transaction.

In an HTTP test, `powtest.Fill(t, page, form)` copies the first challenge out of a rendered page into your form values and solves it the way the browser would. Wait out `MinAge` before you post. It contains a solver, which is why it lives in a package whose name says test.

### The attempt allowance

A token that `Admit` accepted but that keeps failing your validation would otherwise be free handler work for its whole `MaxAge`. So `Admit` counts acceptances per token, in memory, and after `Attempts` (20) refuses that token as `attempts`. The count lasts until the token expires. A successful `Commit` doesn't clear it, because the insert succeeding says nothing about whether your transaction will, and clearing it there let one solve repeat admit, spend, refuse, roll back for ever.

`Admit` counts at most `Tracked` tokens per Guard (100,000, a few megabytes) and never forgets a live one to make room. At capacity, a new token is refused as `busy` until an entry expires; tokens already counted keep their allowance. Evicting the oldest instead would let an attacker cycling one token more than capacity reset every allowance without solving again.

The trade-off: an attacker holding `Tracked` accepted tokens can make every form that uses `Admit` on that Guard answer `busy` until those tokens expire. That's 100,000 solves with proof of work on, or 100,000 page loads under `NoProof`. And because committed tokens also stay counted until they expire, an honest form taking more than `Tracked` submissions within `MaxAge` meets the same ceiling, so raise `Tracked` for a form like that. `Check` never counts, so a Guard whose count is full never makes `Check` answer `busy`.

The count resets on restart. That's fine: it bounds handler work, and replay is the spent table's job.

## Requests on a form's behalf

Some requests run on a form's behalf before it's submitted, like an email-availability check or a file upload. They can carry the form's proof instead of needing a challenge of their own.

In the browser, `whenSolved` waits for the form's solve and gives you the fields to send:

```js
import { whenSolved } from "/pow/pow.<hash>.js"; // exactly your Config.ScriptURL

try {
  const { fields } = await whenSolved(form, { timeout: 10000 });
  const body = new URLSearchParams(fields);
  body.set("email", input.value);
  await fetch("/check-email", { method: "POST", body });
} catch (err) {
  // show an error the visitor can retry from
}
```

Import it from the same URL `Script` loads. A module at a different URL is a second copy, which wires every form on the page a second time.

`whenSolved` never stays pending:

- It resolves with `{fields}`, the `pow_*` names and values to send, once the form is solved, or straight away if it already is.
- It resolves straight away for a form with no `data-pow-form`, such as a `NoProof` form, with the fields it has.
- It rejects straight away for a bound form, whose proof can't be checked without its value, and for a form the module hasn't wired.
- It rejects if the worker fails, when the visitor leaves the page, and after `timeout` (10 seconds by default).

So the code that calls it has to handle a rejection: show an error the visitor can retry from, and clean up in `finally`. The form also fires `pow:solved` (with `detail.fields`) and `pow:failed` (with `detail.error`).

If your page replaces its own document with `document.open` and `document.write`, call `init()` from the same module afterwards. A module URL that has already run is never evaluated again, so the new page's forms would otherwise stay disabled.

On the server, `Guard.Verify` checks the parent token without counting it or spending it:

```go
parent, reason, ok := a.guard.Verify(r)
if !ok {
	a.log.Info("email check refused", "reason", reason)
	w.WriteHeader(http.StatusForbidden)
	return
}
if parent.Scope != applyScope || parent.Difficulty < 16 {
	w.WriteHeader(http.StatusForbidden)
	return
}
// key this endpoint's own allowance by parent.Nonce
```

It checks the seal, the expiry, the proof at the difficulty sealed into the token, and that the token isn't spent. It doesn't check the scope. It returns it, authenticated, for you to decide whether this endpoint serves that form, along with the token's `Difficulty` (a `NoProof` parent has 0, so refuse what you don't accept) and its `Nonce`, the one canonical string to key your own allowance by. Any Guard with the same instance key verifies any of the app's unbound tokens, so the endpoint doesn't need the Guard that minted the parent. A bound token is refused as `seal_invalid`.

## Reasons

A `Reason` is one of a closed set, so your refusal log stays countable and nothing a submitter typed reaches it. Keep reasons off the page: telling a script which check it failed helps the script. `auth` and `password` show "Your browser couldn't finish a security check. Try again." with a recovery form for every reason, and that's a good default for your own forms, with the exceptions below.

| Reason | Value | What happened | Show the visitor |
|---|---|---|---|
| `ReasonMissing` | `missing` | No challenge was posted. The form was never wired, or was rendered before you upgraded. Look for this one first after an upgrade. | A recovery form |
| `ReasonHoneypot` | `honeypot` | The trap field was filled, by a script or a password manager. | A recovery form, which has no trap |
| `ReasonSealInvalid` | `seal_invalid` | The challenge was altered, malformed, for another scope, sealed with another key, easier than this Guard requires, or bound when the Guard isn't (or the reverse). One reason for all of them, so a prober learns nothing. | A recovery form |
| `ReasonTooFast` | `too_fast` | Posted before `MinAge`. | A recovery form, which is past its minimum age |
| `ReasonTooOld` | `too_old` | Past its expiry: the page sat open longer than `MaxAge`. | A recovery form |
| `ReasonShort` | `pow_short` | The proof of work is missing or wrong. | A recovery form |
| `ReasonSpent` | `spent` | Already used, usually a double submit or a resend after Back. `Commit`'s `ErrSpent` is the same case. | That the form may already have been sent, and a fresh, empty form |
| `ReasonBounds` | `bounds` | The body didn't parse as a form, or was over your `MaxBytesReader` limit. | A recovery form |
| `ReasonUnavailable` | `unavailable` | The spent-nonce store failed. Still a refusal: accepting posts while replay protection is down would turn an outage into unlimited replay. | That something went wrong on your side, and to try again shortly |
| `ReasonAttempts` | `attempts` | `Admit` has accepted this token `Attempts` times. | A recovery form |
| `ReasonBusy` | `busy` | `Admit` is already counting `Tracked` tokens. | To try again in a few minutes |

## Choosing a difficulty

Solve time is geometric, so choose on p95 and p99, never the average. p99 is about 4.6 times the average, and a difficulty chosen on the average hangs for one visitor in a hundred.

Most visitors never wait, because the solve starts when the page loads and runs while they type. The visitor who waits is the one with nothing to type: someone coming back and tapping a one-tap sign-in button waits for the whole solve. So for sign-in, take the largest difficulty whose p95 stays at or under one second with Chromium's CPU slowed six times, which stands in for a mid-range phone. That's 16 bits, and it's what the notes example uses. `DefaultDifficulty` is still 18.

Measured on 2026-10-04 on an AMD Ryzen 7 5800X in headless Chromium at 6x CPU throttle, 60 solves per row (30 at 17 and 18 bits):

| Bits | p50 | p95 | p99 |
|---|---|---|---|
| 12 | 11ms | 30ms | 50ms |
| 13 | 25ms | 72ms | 172ms |
| 14 | 39ms | 194ms | 352ms |
| 15 | 88ms | 366ms | 532ms |
| 16 | 232ms | 796ms | 879ms |
| 17 | 306ms | 1139ms | 1257ms |
| 18 | 592ms | 1494ms | 1855ms |

Samples this small make the tails noisy (the same run put 18 bits' p95 at 2586ms at only 4x throttle), and a throttled desktop is not a phone, so treat the table as a guide. To measure again, on your own hardware or after a change to the solver:

```sh
POW_MEASURE=1 TMPDIR=/var/tmp go test -tags browser -run Measure -v -timeout 60m ./pow/
```

It prints p50, p95 and p99 for 12 to 18 bits at 1x, 4x and 6x throttle.

## Declarations

```go
func New(cfg Config) (*Guard, error)
func Assets() fs.FS
var Schema *migrate.Set

const (
	DefaultDifficulty = 18
	DefaultMinAge     = 3 * time.Second
	DefaultMaxAge     = 2 * time.Hour
	DefaultAttempts   = 20
	DefaultTracked    = 100_000
	NoProof           = -1
)

var (
	ErrEmptyInstanceKey, ErrNoNonceStore, ErrNoAssets, ErrBindNeedsProof error // from New
	ErrNoSchema                                                          error // from New, via NonceStore.Ready
	ErrSpent, ErrNotAdmitted                                             error // from Admission.Commit
)

func (g *Guard) Form(now time.Time, scope string) Form
func (g *Guard) FollowOn(now time.Time, scope string) Form
func (g *Guard) Recovery(now time.Time, scope string) Form
func (g *Guard) Issue(now time.Time, scope string) Challenge
func (g *Guard) Admit(r *http.Request, w Want) Admission
func (g *Guard) Check(r *http.Request, w Want) Admission
func (g *Guard) Verify(r *http.Request) (Parent, Reason, bool)
func (g *Guard) Sweep(now time.Time) error
func (g *Guard) Bound() bool

type Want struct{ Scope, Binding string }

type Admission struct {
	OK     bool
	Reason Reason
	Also   []Reason
}
func (a Admission) Commit(ctx context.Context, ex Execer) error
func (a Admission) Recovered() bool

type Parent struct {
	Scope      string
	Nonce      string
	Difficulty int
	Issued     time.Time
}

type Form struct{ Challenge }
func (f Form) Attrs() template.HTMLAttr
func (f Form) Script() template.HTML
func (f Form) StatusLine(text string) template.HTML
func (f Form) NeedsScript() bool

type Challenge struct {
	Scope, Nonce    string
	Issued, Expires time.Time
	Difficulty      int
	Flags           uint8
	Seal            string
}
func (c Challenge) Fields() template.HTML

type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type NonceStore interface {
	Spend(ctx context.Context, ex Execer, nonce string, expires time.Time) (bool, error)
	Spent(ctx context.Context, nonce string) (bool, error)
	Ready(ctx context.Context) error
	Sweep(now time.Time) error
}
func SQLNonces(db *sql.DB) NonceStore
func MemoryNonces() NonceStore

func Trapped(r *http.Request) bool
func Verify(nonce, binding, counter string, difficulty int) bool
const HoneypotStyleHash = "'sha256-yJxAE4rjdcckohdlnvecSporPcqS9xOaA4hJxi87LMc='"
```

`Issue` mints a bare `Challenge` for a page wiring the browser side itself. `Trapped` reports the honeypot on its own. `Verify` checks one candidate solution, and is what `powtest` solves against. `Bound` reports `Config.Bind`, so `auth` and `password` can refuse a bound Guard.

The `pow/powtest` package fills a rendered challenge for HTTP tests:

```go
func Fill(t testing.TB, page []byte, form url.Values) url.Values
func FillBound(t testing.TB, page []byte, form url.Values, binding string) url.Values
```
