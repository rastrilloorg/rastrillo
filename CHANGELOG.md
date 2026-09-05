# Changelog

Notable changes to rastrillo, newest first. Anything that can damage an app's
files or silently change its behaviour is called out first in its release,
whether or not it is the largest change.

This file starts at v0.23.0. Earlier releases are in the git history and their
tags; nothing has been reconstructed for them, because a changelog written
backwards from commits is a guess wearing a date.

## v0.26.0

Two new subsystems — a front door for public forms, and mail that can go to a
list — and a UI pass that changes markup your app has a frozen copy of. Read
the first two sections if you have an app scaffolded before this release.

### Changed — re-vendor your CSS, and add calendar.js

`rastrillo new` writes `tokens.css`, `theme.css` and the framework's scripts
into your app's `static/` once, and they are yours from that moment. Nothing
in the framework rewrites them. This release changes all of them, so an app
that does not re-vendor gets the new partials rendering against last
release's stylesheet: buttons at the wrong size, a stat band with no rules, a
calendar that never opens.

`rastrillo doctor` reports the difference. The vendored set is now six files
rather than five — **`calendar.js` is new**, and a date field whose app does
not serve it falls back to the browser's own picker, which is the control
this release replaced.

The examples are apps like any other and got this exact trap during
development: `examples/blog` and `examples/tickets` pin their copies with a
test, and both went red. Worth knowing that the root `go test ./...` says
nothing about it — `examples/` are separate modules with a `replace` back to
the checkout, so they are not in the root gate at all.

### Changed — `--rst-meter-fill` is gone, and two partials changed element

`meter` had been named after an element it did not use since it shipped. It
is a native `<meter>` now, and `job-status` draws a native `<progress>` when
you give it the new optional `Percent`. The span-and-`<i>` markup underneath
both is gone, along with the `--rst-meter-fill` custom property: an app that
set it is now setting nothing, silently, which is why a test asserts the
property is no longer written rather than only that the new markup is. If
your CSS reaches inside either partial, it is reaching at nothing.

What the change buys is narrower than it sounds: machine-readability and the
right element for a scalar measurement, not accessibility. The bar keeps
`aria-hidden` and stays decoration over authoritative text, because a meter
named "412 / 500" beside the text "412 / 500" announces everything twice.

One limit stated rather than smoothed over: styling either element needs
separate `-webkit-` and `-moz-` rules that cannot be merged, because a
selector list containing a pseudo-element an engine does not know is invalid
as a whole in that engine. This project drives Chromium, so the WebKit/Blink
path is measured and **the Firefox path is written from the spec and
unverified**.

### Changed — a form's buttons are bigger, and `mail` now honours its context

`rst-btn` had one size — the size a page-header action wants, which reads as
a skinny bar stretched across a form column. There is a size scale now,
composing with the variant (`rst-btn="primary lg"`), and `form-foot` and
`confirm-form` emit `lg` for their buttons. A test asserting the old markup
will fail; that is what it is for. The one submit that stays at the default
step is the sticky save bar, which is chrome pinned to the viewport.

`mail.Sender.Send` took a `context.Context` and discarded it, because
`net/smtp.SendMail` cannot be given one and dials with no deadline at all. It
is honoured now, and a call whose context carries no deadline of its own is
bounded at 30 seconds. An app that relied on a send blocking indefinitely
will see it stop; that was never a behaviour worth relying on — a wedged
relay blocking a caller is long enough to hold SQLite's single writer.
Messages also carry a `Date` header now, which RFC 5322 requires and nothing
here was sending.

### Added — `rastrillo/pow`, a front door for a public form

A proof of work bound to what the visitor typed, an HMAC-sealed challenge, a
single-use nonce and a honeypot, with the browser half shipped alongside the
Go half that verifies it.

```go
g, _ := pow.New(pow.Config{InstanceKey: key, Nonces: pow.SQLNonces(d.Writer())})
c := g.Issue(time.Now())          // render c.Fields() and c.FormAttrs(workerURL)
reason, ok := g.Check(r, address) // on POST, before anything that writes
```

The binding is the point: an unbound challenge is solved once and replayed
against every address in a list, so it costs an attacker one solve in total.
Bound, it costs one per address, which is the axis a bulk signup attack
scales along.

Unlike the rest of the framework's browser code, `pow`'s modules are **served
from the module rather than vendored** — `rastrillo.NewAssets(pow.Assets())`.
Vendoring is right for a stylesheet somebody should edit and wrong for a file
that must agree byte for byte with the verifier it is checked against. A
browser test runs the shipped solver in a real Chromium and checks its
answers in Go, because a disagreement between the two fails silently, in the
browser, for whichever addresses happen to hit it.

Extracted from `movement` at the point a second app needed the same thing.

### Added — mail to a list

`mail.ListMessage` and `mail.SendList` add a closed, validated header set:
`List-Id`, `List-Unsubscribe` with the `List-Unsubscribe-Post` that makes it
RFC 8058 one-click, `Message-Id` and `Reply-To`. That first pair is the
difference between a reader leaving your list and a reader pressing "report
spam", and the second button damages the sending domain for every other app
on it.

Closed rather than a `map[string]string`, because a header bag hands the
caller a second `From` and a `Bcc`, and turns the injection guard from a
property of the package into something every caller has to remember.
`mail.Sender` is untouched, so signin's Mailer still matches it.

`mail.IsConfigured` answers whether a `Sender` will really deliver.
`mail.Logged` returns `nil` from `Send`, which is right for a magic link on a
dev box and wrong for a mailing — run a list against it and you record
several hundred deliveries that never happened, and write several hundred
subscriber addresses into the log in plaintext on the way.

### Added — the date field draws its own calendar

The button on a date field called `showPicker()`, which cannot be styled to
match the page, has no panel at all on some engines, and — measured — opens
without moving focus, so the pick was swallowed by a focus guard. Choosing a
date did nothing.

`ui/calendar.js` draws a real one: six weeks as a `<table>` under a
`role="grid"`, one day in the tab order, arrows for days and weeks, Page for
months, Shift+Page for years, and a right-to-left page swaps the horizontal
arrows. It knows no month names, weekday names or first-day-of-the-week —
all three come from `Intl` for the page's `lang`. It is a separate script
behind `window.rastrilloCalendar`, so an app serving one and not the other
still has a working field.

Natural-language parsing gained completion in the same pass: "5j" is not a
date, but 5 January, 5 June and 5 July are three, nearest first, each written
out before anyone commits it. The grammar is exactly as strict as it was.

### Added — smaller things

**The primary input.** `field-text` and `field` take `Primary`
(`rst-input="primary"`), so a form's headline field stops looking like
furniture. This needed `DESIGN.md`'s Weight-Not-Size rule changed rather than
worked around: the rule governs chrome, and a title input is the opposite
case.

**The stat band**, with a delta whose sign is not carried by colour alone.

**Semantic elements.** `detail-list` takes an optional `DateTime` beside
`Value`, so a moment renders as `<time>` and an identifier does not. The list
row's name cell is wrapped in `<bdi>` — in an English row, an Arabic name
followed by "· 09:12" drew the time to the *left* of the name it belongs to.

**`form.Error` carries a catalog key.** `form.ParseCents` returned hardcoded
English in a framework shipping twelve locales, and named a currency the
framework does not store. The key sits beside the message, so a caller with a
translator renders the reader's language and one without prints the English.
Recorded rather than fixed: `FormatCents` still writes US dollars and knows
no locales.

**The numbers ruling**, now in `DESIGN.md`, `SKILL.md` and `templates.md`: a
quantity is grouped for the reader's locale, an identifier never is. Order
4471, not order 4,471.

**The design-system gallery** gains a Screens tier (five ways in, with the
password screen carrying a warning, because we do not recommend passwords)
and a Formats page for the four tags no partial will ever hold — `<address>`,
`<abbr>`, `<data>` and `<output>`.

### Fixed — two mobile zooms, and a browser floor that was drifting

Mobile Safari zooms the page when it focuses a control whose text is under
16px, and `tokens.css`'s base type is 14px on purpose. Text-entry controls
come back up to 16px under `@media (pointer: coarse)` and stay 14px under a
mouse. A second tap on a button landed as double-tap-to-zoom rather than a
click; `touch-action: manipulation` drops that gesture on interactive
elements only. `user-scalable=no` would have bought the same quiet by taking
pinch-zoom from everyone, which fails WCAG 1.4.4, and is not used.
`initial-scale=1` is dropped from the four shells and the examples — the 2010
iPhone rotation bug it existed for is long fixed.

The CSS floor is a rolling bar now (a feature is fair game once it has been
in all three engines about a year) with a test that fails nine months after
the recorded review date. It fails rather than warns: a warning on a release
evening is a thing you scroll past.

### Changed — where the work happens

`main` had drifted, because `AGENTS.md` told agents to open a pull request
and squash-merge it, and this project has neither. Work lands on
[amadan](https://amadan.net/rastrillo/rastrillo), where a branch *is* the
review, and GitHub is a mirror. A squash rewrites the commits, so the
ancestry check that decides a branch is merged answers no — which is how
three pieces of `SKILL.md` sat on the mirror for days looking landed while
origin's `main` had never seen one of their commits. Two Makefile targets
make that failure loud; neither is in `make ci`, because a runner must not
push.

`SKILL.md`'s budget moved to 30,000 bytes — a change of regime rather than
another notch, recorded in `skillmd_test.go`. `PRODUCT.md` and `DESIGN.md`
are new: the product record and the incumbent visual system, the latter
derived from `tokens.css` rather than from intentions.

## v0.25.0

Written after the fact. This release was tagged without a prep commit, so it
had no entry here and did not bump the scaffold's fallback version — see the
Fixed note below.

### Changed — the module path moved

rastrillo's repository is now `amadan.net/rastrillo/rastrillo`, and so is its
module path. An app pinning the old `github.com/carlosframework/rastrillo`
keeps building against the version it already has; to move, rewrite the path
in `go.mod` and in every import, then `go mod tidy`.

Note that v0.24.0 was prepared but never tagged, under either path. The
release before this one is v0.23.0.

### Fixed — a dev-built CLI scaffolded an unbuildable app

`rastrillo new` writes the framework version into the app's `go.mod`. A CLI
installed with `go install ...@vX.Y.Z` reads that from its own build info, but
one built from a checkout has no tag to read and falls back to a constant —
and the constant still said `v0.24.0`, a version that was never tagged. So a
scaffold from any local build failed on the first command it prints:

```
go: amadan.net/rastrillo/rastrillo@v0.24.0: reading .../go.mod at revision
  v0.24.0: unknown revision v0.24.0
```

The constant now tracks the newest tag, and two tests hold it there: one
compares it against this file's newest heading, the other against the newest
`v*` tag in the checkout. Between them a release cut without a prep commit —
which is exactly how this happened — turns the gate red.

## v0.24.0

### Fixed — read this one if you have an app scaffolded before v0.24.0

**A freshly scaffolded app failed its own `make ci`**, on the first run, before
a line of app code existed:

```
missing go.sum entry for module providing package github.com/BurntSushi/toml
  (imported by amadan.net/rastrillo/rastrillo/internal/manifest)
```

The `migration-check` target runs the CLI as `go run` from inside the app
module, so the CLI's whole package graph resolves against the app's `go.sum`.
Nothing an app imports reaches `internal/manifest`'s TOML parser, so
`go mod tidy` prunes it: a dependency that is real at `go run` time and
invisible at tidy time. The scaffolded `go.mod` now declares the CLI as a
`tool`, which is what makes tidy record it. One indirect requirement, two
`go.sum` lines — `cmd/rastrillo` does not drag in sqlc.

**Apps scaffolded before this release keep the broken `go.mod`.** Their
`make ci` fails the same way. Add the line to yours:

```
tool amadan.net/rastrillo/rastrillo/cmd/rastrillo
```

then `go mod tidy`. (`go get github.com/BurntSushi/toml` also clears it, but
names a dependency you do not use and will drift again the next time the CLI
grows one.)

Worth recording why this shipped at all. The suite *does* scaffold an app and
run `make migration-check` — but under `GOFLAGS=-mod=mod`, which the sandbox
environment sets so tests can work against a local module cache. Under
`-mod=mod` a missing `go.sum` entry is not an error: `go run` writes the
requirement into `go.mod` and carries on. So the step passed here for exactly
as long as it failed for everyone else, who runs the default `-mod=readonly`.
That step now runs `-mod=readonly` explicitly.

Reported by the Docs team.

### Added

**`auth.Config.SubjectFor`**, for an app that must not store a readable email
address at rest. `rastrillo/auth` minted every session with the verified
address as its subject, with no way to configure it away — so the address
landed in the `sessions` table and in everything keyed off the subject:
passkey credentials, challenges, pending enrollments, recovery codes. An app
whose guarantee is that publishing its database teaches an attacker nothing
could not use the family default for identity, which is the one surface where
it most wanted to.

`SubjectFor` maps the verified address to whatever the app wants the session
keyed by — an HMAC under a pepper it holds, a row id from its own directory.
Nil is exactly the previous behaviour.

Three things to know before setting it. `Authorize` still receives the real
address, because admission answers a question about an address. `Identity`
(`auth.From`, `sessions.Current`) then carries the remapped value in its
`Address` field, which is the trade being made. And an error refuses the
sign-in rather than falling back to the address — writing one is the failure
the hook exists to prevent.

It does not make the plugin server-blind on its own: `auth_links` still holds
the address at rest between sending a magic link and its click, because the
link has to survive a restart. Sealing that store is separate work.

Reported by the Oficina home team.

### Changed

**`SKILL.md` is 1,619 bytes smaller**, and says the same things. Five blocks
whose detail a documentation page already carried — the date and time field
kinds, the password plugin's configuration, the jobs handlers' mount paths,
`doctor`'s exit codes, and the shipped locale list — are now one sentence plus
their page, which is the delegation the file already promised ("rare traps get
one sentence plus a page") and had stopped doing. Every trap stayed inline, by
the rule that a block stays if getting it wrong is silent and moves to its page
if you would look it up anyway. No API changed, and the size budget did not
move.

## v0.23.0

### Fixed — read this one if you ran `rastrillo markup --fix` on v0.22.0

<!-- markup-spelling: old-spelling begin — this entry's subject IS the old
     spelling, so it names it. The gate that would otherwise flag it has the
     same blind spot as the bug the entry describes: prose about markup read
     as markup. -->

**`rastrillo markup --fix` rewrote Markdown as well as markup.** A sentence in
your documentation reading `` `class="rst-box"` `` became `` `rst-box` `` — which
destroys the explanation and leaves a diff that looks correct. If you ran it,
check `git log -p -- '*.md'` from your v0.22.0 bump before trusting those pages.

<!-- markup-spelling: old-spelling end -->

The files most likely to be hit are the ones that *teach* the class-versus-attribute
distinction, so the damage lands where the explanation lives. In this repository the
tool would have rewritten the documentation of the codemod itself, the table
recording the `rst-form-foot` rename, a correct use of `class` for a utility, and
eleven of the fifty-one design documents under `docs/superpowers/`.

`.md` is no longer scanned. A Markdown file has no markup to migrate; it has
discussion of markup, and the costs are asymmetric — an example left unmigrated is
stale, visible and harmless, while a rewritten explanation is destroyed, invisible
and plausible.

Reported by the Sheets team after upgrading.

### Fixed

**Every scaffolded app reported `dev` from `GET /api/version`, forever.** The
scaffolded `make release` built with `-ldflags="-s -w"` and no `-X`, so
`rastrillo.BuildVersion` kept its default in every binary the framework has ever
produced — while `serve.go` said the version was stamped at build time, "see
`cmd/rastrillo`". It now stamps from `git describe --tags --always --dirty`, and
refuses to build rather than shipping an empty or `-dirty` version.

This is worse than a missing version string, because of what `/api/version` is for.
`x-carlos-version` reports the release the platform believes it *adopted*; `/api/version`
reports what the process actually *running* on the instance says it is. Those are two
different facts, and only the second can tell you a process was never recycled onto a new
release. An app answering `dev` cannot supply that fact at all — so the one endpoint that
could detect a stale process is silent on every app this framework has scaffolded.

That failure has been seen: a deploy reported `live` over a process that had never been
recycled, caught only because that app's binary *was* stamped and its two answers
disagreed. A scaffolded app could not have produced that disagreement.

**Apps scaffolded before this release keep the old target.** Re-scaffold, or copy the
`VERSION` line and the `version-check` target into your Makefile. `make build` is
unchanged and still does not stamp: the compile check is not a build.

Reported by the Sheets team.

**`rastrillo markup` exited 3 on a clean tree.** The escaped-markup note ignored
fenced `old-spelling` regions, so a repository that had correctly fenced its
discussion of the old spelling could never get a green run. A report mode that
always reports is one people stop reading — which is how the `--fix` above gets
run without a second look.

**`rastrillo markup` read `<style>` inside Go comments and string literals** as an
app's stylesheet, so roughly a hundred lines of unrelated code could be reported
as selectors to change. It now scans only real string literals, via `go/scanner`.

**The design-system gallery's preview frames were unreadable on a phone.** A
1200px-wide desktop rendering was scaled to fit a phone column, which made most
previews an 18px sliver — present, and invisible. The opening view now follows the
width of the space the preview actually has rather than the width of the window,
and a desktop rendering that would fall below legibility pans instead of shrinking.

### Added

**A colour engine in `ui`**, for apps that need to generate colours rather than
pick them: `Pair` for a fill with matching ink, `Wash` for a fill that keeps the
ink a caller already has, and `Allocate` for assigning distinguishable hues to a
set of keys. Every result clears WCAG 2.2 AA by construction, and the offered set
is proven against every background it can be rendered on. Built with Sheets and
Docs, whose requirements changed the design more than once.
