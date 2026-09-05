# 🤖 pow

`amadan.net/rastrillo/rastrillo/pow`

The front door for a form anyone on the internet can post to: a proof of
work bound to what the visitor typed, a sealed challenge, a single-use
nonce, and a honeypot. The browser half ships with it.

## Why the browser half is here

The solver in the page and the verifier in Go have to build a
byte-identical string to hash. Nothing about a copied file enforces
that, and a disagreement fails silently — the visitor's solution is
rejected as too short, they see a form that does nothing, and no log
explains it. So both halves ship from this module, and a browser test
runs the shipped solver in a real Chromium and checks its answer against
the Go verifier.

That is also why `pow` does not go through the vendored-assets route
that `tokens.css` and `rastrillo.js` take. A vendored copy can be
edited, or left behind when you upgrade, and then it is a solver that
disagrees with the verifier it is linked against.

## Wiring it up

```go
g, err := pow.New(pow.Config{
	InstanceKey: cfg.InstanceKey,
	Nonces:      pow.SQLNonces(d.Writer()),
})
```

`InstanceKey` is the same one `auth` takes; the seal key is derived from
it under a label of its own, so a token minted by one subsystem never
verifies in the other. `Nonces` has no default, because the default
would have to be "no replay protection" — see below. `Difficulty`,
`MinAge` and `MaxAge` fall back to `DefaultDifficulty`, `DefaultMinAge`
and `DefaultMaxAge`.

Apply the migration with the rest of your set:

```go
migrate.Apply(ctx, d, migrate.Merge(sessions.Schema, pow.Schema, app.Schema))
```

Serve the browser half from wherever you like, behind an `Assets`
registry so the URLs are content-hashed:

```go
powAssets := rastrillo.NewAssets(pow.Assets())
mux.Handle("GET /pow/", http.StripPrefix("/pow/", powAssets.Handler()))
```

## Rendering the form

`Guard.Issue` mints a `Challenge`. It writes nothing, which is the point: an
earlier design persisted a row per challenge, and that made every page
view a serialised write against a single-writer SQLite file — so a
crawler, a link scanner or a prefetcher was a denial of service against
the one resource every submission needs. Nonces are recorded as *spent*
instead, on acceptance, where each row has already cost a solve.

```go
c := g.Issue(time.Now())
```

`Challenge.Fields` renders the four sealed inputs, the empty input the
solver writes into, and the honeypot. `Challenge.FormAttrs` renders the
attributes the module reads off the form:

```html
<form method="post" {{.Challenge.FormAttrs .WorkerURL}}>
  {{.Challenge.Fields}}
  <input type="email" name="email" data-pow-binding required>
  <button type="submit" data-pow-submit disabled>Sign up</button>
  <noscript>Signing up needs JavaScript.</noscript>
</form>
<script type="module" src="{{asset "pow.js"}}"></script>
```

The submit button is rendered **disabled**, and the module enables it
once it has loaded. That ordering is the only one that fails safe:
JavaScript cannot enable a control inside `<noscript>`, and a module
that throws on an old browser leaves the honest disabled state rather
than a form that looks live and does nothing. Say so in the
`<noscript>` — signing up genuinely needs JavaScript here, and a visitor
who can't run it deserves to hear it from you.

Use `Fields` rather than writing the inputs yourself. The honeypot in
particular has an accessibility contract: `aria-hidden` on its wrapper,
`tabindex="-1"` so nothing focusable hides behind that,
`autocomplete="off"`, and a name no browser will autofill. Miss one and
a screen-reader user, or anyone with a password manager, fills the trap
and has a real submission silently discarded behind a cheerful success
page.

The baseline CSP already allows all this: the worker is same-origin, so
`default-src 'self'` covers it, and the honeypot's inline `style`
attribute is covered by `style-src 'self' 'unsafe-inline'`. If you have
replaced the policy wholesale, keep both.

## Checking a submission

```go
r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
if reason, ok := g.Check(r, r.PostFormValue("email")); !ok {
	slog.Info("refused", "reason", reason)
	renderSent(w) // the same page every outcome renders
	return
}
```

`Guard.Check` is the whole front door in one call, and the order it runs in is
the part worth keeping. The honeypot first, because it is free and must
not sit behind anything that writes. The seal before the clock, because
until the signature holds, the timestamp is a number the submitter
chose. The proof of work before the nonce is spent, so a submission that
was never going to pass costs no row. The nonce spent before you do
anything at all, because a solved challenge is otherwise replayable for
its whole `MaxAge` window.

The second argument is the binding — the value the work was tied to,
which is the address in the ordinary case. Pass exactly what the
`[data-pow-binding]` input held; the package ASCII-lowercases and trims
it identically on both sides, so you never have to match Go's case
folding to JavaScript's.

Binding the work to the address is the design's whole economics. An
unbound challenge is solved once and replayed against every address in a
list, so it costs an attacker one solve in total. Bound, it costs one
solve per address — which is the axis a bulk signup attack scales along.
It's also why a form like this needs JavaScript at all: the work can't
begin until the visitor has typed the value it's bound to.

`Trapped` answers the honeypot question on its own, for a handler doing
its own wiring. `Check` already does it, first.

## Reasons

`Check` returns a `Reason` — a closed set, so what you log stays
countable and nothing a submitter typed can reach the log through it.

`ReasonHoneypot`, `ReasonSealInvalid`, `ReasonTooFast`, `ReasonTooOld`,
`ReasonShort`, `ReasonNonceSpent`, `ReasonBounds`, and
`ReasonUnavailable` when the nonce store itself fails. That last one is
still a refusal: letting a submission through when replay protection is
unreachable turns a database outage into unlimited replay.

Mostly, render the same page for every outcome. A form that answers
"you're already signed up" is a form anybody can ask about anybody, one
address at a time.

## Remembering spent nonces

```go
type NonceStore interface {
	Spend(ctx context.Context, nonce string, expires time.Time) (bool, error)
	Sweep(now time.Time) error
}
```

`SQLNonces` is the one to use. `Spend` is a single `INSERT OR IGNORE`,
so there is no window between looking and writing — a `SELECT` then an
`INSERT` lets two concurrent replays both see an empty table, which is
the race single use exists to close.

`MemoryNonces` is honest for a test and for a process that can afford to
forget, but a restart is exactly when an attacker's stockpile of solved
challenges becomes spendable again.

`Guard.Sweep` drops what has expired. Correctness never depends on it —
a challenge past `MaxAge` is refused on age alone — so call it from a
tick you already have, or don't.

## Difficulty

`DefaultDifficulty` is 18 bits, roughly 262k expected hashes. It's an
estimate, not a measurement. Re-set it against real mid-range hardware
before you launch, and record p95 and p99 rather than the mean: solve
time is geometric, so p99 is about 4.6x the average, and calibrating on
the average ships a form that hangs for one visitor in a hundred.

`Verify` is the primitive underneath, for a caller wiring the pieces
themselves. `New` returns `ErrEmptyInstanceKey` or `ErrNoNonceStore`
when a required field is missing.

And keep the ceiling in view: 2^18 SHA-256 is under a millisecond on a
commodity GPU. Proof of work prices out casual and scripted abuse and
nothing more. Against a serious attack the defence is a persisted budget
on whatever the form spends — mail, rows, money — not this.
