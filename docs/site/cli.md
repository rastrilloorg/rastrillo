# 🤖 The CLI

One binary, seven commands. Install it with:

```sh
go install github.com/carlosframework/rastrillo/cmd/rastrillo@latest
```

Every command takes an optional directory as its last argument and
defaults to `.`. Flags come before the directory. `rastrillo help`, `-h`
and `--help` all print usage.

## rastrillo new

```sh
rastrillo new [--icons=<set>] [--icon-delivery=<mode>] [--ux=<profile>]
              [--theme=<name>] [--shell=<name>] <name>
```

Scaffolds a complete app in `./<name>` — one that compiles, passes its
own tests, and serves, before you have written anything. It runs
`generate` once on the way out so `go build` works immediately.

| Flag | Values | Default |
|---|---|---|
| `--icons` | `lucide`, `font-awesome` | `lucide` |
| `--icon-delivery` | `inline`, `cdn`, `js` | `inline` |
| `--ux` | `considered`, `standard` | `considered` |
| `--theme` | `day`, `plain`, `signal` | `day` |
| `--shell` | `column`, `topbar`, `sidebar` | `column` |

All six set × delivery combinations scaffold, compile and pass
`generate --check`. The icon set becomes an ordinary app-owned package
under `internal/<app>/icons`, and `--ux` seeds a UX convention profile
into `AGENTS.md`, which is the source of truth from then on — nothing
re-reads the profile name afterwards. [Icons](/docs/icons) explains what
each delivery mode costs, including the one worth repeating: with `js`,
icons do not render at all without JavaScript.

`--theme` picks the colour, type and shape stylesheet written as
`static/theme.css`, and `--shell` picks the page frame written as
`templates/layout.html`. Both are copied in verbatim and are yours from
that moment — the theme like `tokens.css`, the layout like every other
template. Every value is checked before a single file is created, so a
typo fails with your working directory still clean.
[Templates](/docs/templates) describes the three of each.

`--icons=font-awesome` also writes the CC BY 4.0 attribution the licence
requires, because that obligation is the app's and has to travel with
the code.

## rastrillo generate

```sh
rastrillo generate [--check] [--default-locale <code>] [dir]
```

Walks `actions/` and `manifest/` and emits `gen/`: the router on a Go
1.22 `http.ServeMux`, plus every declared resource's store, screens and
locale keys. Fails loudly on route collisions.

`--check` verifies without writing, and is the pre-ship gate. It checks
route collisions, action build tags, icon slugs that nothing answers,
and i18n catalog completeness — and, when the app has a
`cmd/genvectors`, it runs the parity-vectors gate too, so
`vectors --check` never needs a separate CI step. Only `--check` fails on an incomplete
catalog — plain `generate`, and so `dev` and `new`, never does. That
split is deliberate: silent fallback while you iterate, loud failure
before you ship.

`--default-locale` names the catalog every other catalog is compared
against. It defaults to `en` and is **not** read from
`Options.DefaultLocale`, so an app that sets a different default must
pass the matching value here or the check compares against the wrong
catalog.

## rastrillo dev

```sh
rastrillo dev [dir] [-- app args]
```

The watch loop: polls `app/`, `actions/`, `manifest/`, `cmd/`,
`locales/` and `templates/`, and on any change reruns `generate`,
rebuilds `./cmd/<name>` to a temporary binary, and restarts the process
with a graceful SIGTERM. Anything after `--` is passed to your app.

A failed generate or rebuild keeps the previous build serving, and a
failed restart keeps the loop watching — either way the next save
retries. It expects the `rastrillo new` layout: exactly one directory
under `cmd/`.

## rastrillo migration

Schema changes. The group is a noun on purpose: this CLI never applies
migrations, because migrations run at boot — a hibernating route has no
operator moment between a new binary landing and the activator exec'ing
it. `baseline` is the one exception, and it is manual by design.

[Migrations](/docs/migrations) is the guide; these are the commands.

### migration generate

```sh
rastrillo migration generate [--allow-destructive] [dir]
```

Diffs your models against your migrations and writes the delta as a new
numbered migration. Prints `nothing to do` when they already agree.

A change that drops data is refused unless you pass
`--allow-destructive`, and the refusal prints the SQL it would have
written so you can see exactly what it thinks is destructive.

Read the generated SQL before committing it. `generate` may emit a full
table rebuild rather than an `ALTER`, and a rename is indistinguishable
from a drop plus an add to any tool — write those by hand with
`migration new`.

### migration check

```sh
rastrillo migration check [dir]
```

The CI gate: exits non-zero when models and migrations disagree, listing
the SQL that would close the gap. Touches no database, so it runs
anywhere. `make ci` in a scaffolded app runs it for you.

### migration new

```sh
rastrillo migration new <name> [dir]
```

Writes a numbered stub migration for a change you will write by hand —
a rename, a data backfill, anything the differ cannot infer. The name
must be lowercase letters, digits and underscores.

### migration status

```sh
rastrillo migration status --db <path> [dir]
```

What a real database's ledger has applied, plus any pending drift.
Requires `--db` because it reports on a database rather than on source.
A database that has never booted this version gets a plain "no ledger
yet" line rather than an error.

### migration baseline

```sh
rastrillo migration baseline --db <path> [--through <id>] [dir]
```

Stamps a ledger by hand, for the case where boot refuses to adopt an
existing database. It runs no DDL — it only records migrations as
applied.

**Order matters, and getting it wrong loses a migration.** Run
`baseline --through <id>` *first*, stamping only up to the migration the
database already matches, and then apply the missing one by hand; the
next boot runs the rest. Bare `baseline`, with no `--through`, stamps
everything: while the ledger is still empty any boot finds a matching
schema and adopts it, recording every later migration as applied without
running it — which silently skips a pending data migration. That is
what it is for, and it is why it is manual.

[Migrations](/docs/migrations#recovering-an-old-database) walks the
whole recovery.

## rastrillo doctor

```sh
rastrillo doctor [--fix] [--force] [--theme <name>] [dir]
```

Compares the files `rastrillo new` copied into the app's `static/`
directory — `tokens.css`, `theme.css`, `rastrillo.js`, `select.js` and
`datetime.js` — against the copies this binary carries, and says which
ones differ and how.

It is a convenience and an upgrade tool, not the thing standing between
your app and silent drift. That is the `vendored_test.go` the scaffold
writes, which runs in CI on every commit without anyone remembering to.
What `doctor` adds is re-copying rather than telling you to, working on
an app that never had that test, saying *how* a file differs rather than
that it does, and running from outside the app — which is what asking
"is this one safe to upgrade?" about somebody else's repository needs.

| Flag | Purpose |
|---|---|
| `--fix` | Re-copy each drifted file from this binary's library copy |
| `--force` | With `--fix`: re-copy across a version mismatch, and over files recorded as deliberate edits |
| `--theme` | Which theme `static/theme.css` should be, for an app with no pin to read it from |

### The version it compares against

The CLI carries its own compiled-in copy of the library; your app has
its own required version in `go.mod`. These are frequently different,
**and that difference is not drift** — an app deliberately on `v0.19.0`
checked by a `v0.20.0` binary has files that correctly match `v0.19.0`.

So `doctor` reads both, says which one it compared against, and makes a
mismatch the first line of the report rather than a footnote:

```
rastrillo doctor is v0.20.0; this app requires v0.19.0.
Comparing against v0.20.0 — upgrade the module first, or these differences are expected.
```

`--fix` refuses in that state. Copying `v0.20.0` assets into an app that
compiles against `v0.19.0` produces exactly the fault this checks for —
new CSS against old markup — with the difference that `doctor` would
then call it clean. Upgrade the module first, or pass `--force`.

An app with a `replace` directive is not a mismatch: it builds against a
checkout, so there is no second version to disagree with. `doctor` says
which checkout and compares against its own copy, which is right only if
this binary was built from that tree.

### What it will not call drift

A hand-edited theme is a supported thing, not damage. If
`static/theme.css` matches no shipped theme, `doctor` says **custom or
drifted** and compares nothing — it will not pick the closest theme and
report a diff against a guess. Pass `--theme <name>` when you do want it
compared against a shipped one.

A file you edited on purpose is the same: name it in `vendoredIsMine` in
the scaffold's `vendored_test.go` and both that test and `doctor` leave
it alone. Apps scaffolded before that map existed recorded the same
thing by deleting the file's line from the pin, and `doctor` reads that
too — but it reads such a file rather than assuming. The first version
of that pin listed three files, and `theme.css` and `datetime.js` joined
the vendored set afterwards, so a name an old pin never mentions may
simply mean "this app predates it". You delete a pin line to protect an
edit, so the edit is the evidence: a file that is missing is **absent**
and `--fix` delivers it, a file identical to the library is checked
normally, and only a file that is there *and* differs is left alone as
yours. That last rule is also what stops `--fix` from installing a file
and thereby exempting it from every check afterwards.

A file you deleted is the same again. Dropping `select.js` from an app
with no big selects is a supported choice, so an absent file is reported
as **absent**, not as drift, and does not fail the exit code — you get a
line saying what the library ships and how big it is. `--fix` will still
add it, because asking for `--fix` is asking.

### Exit codes

| Code | Meaning |
|---|---|
| `0` | Every compared file matches |
| `1` | An error — not an app, unreadable files |
| `2` | Usage |
| `3` | Drift: files differ from the library copy |
| `4` | The app and the CLI are on different rastrillo versions, so the comparison is not authoritative |

Drift and version mismatch are separate codes because they call for
opposite actions: one means "re-copy these", the other means "do not
re-copy anything yet".

<!-- markup-spelling: old-spelling begin — this section documents the
     migration, so it shows the spelling being migrated away from. -->

## rastrillo markup

```sh
rastrillo markup [--fix] [dir]
```

Rewrites an app's markup from the class spelling of the UI vocabulary to
the attribute spelling: `<div class="rst-box">` becomes `<div rst-box>`,
`class="rst-callout__body"` becomes `rst-callout-body`, `class="rst-btn
rst-btn--primary"` becomes `rst-btn="primary"`, and `data-tone` becomes
`rst-tone`. Seven utility classes stay in `class`, because that is what
`class` is for: `rst-sr-only`, `rst-mono`, `rst-m-hide`, `rst-grow`,
`rst-nm`, `rst-danger` and `rst-cell-mut`.

It reads templates, Go source (markup in a string literal or a doc
comment), Markdown, JavaScript and CSS, and it is the same tool the
framework flipped itself with. It skips `.git`, `node_modules`,
`vendor`, `.design-system` and `.superpowers`.

| Flag | Purpose |
|---|---|
| `--fix` | Write the rewrite. Without it, the command reports what would change and writes nothing |

| Exit | Meaning |
|---|---|
| `0` | Nothing to do, or `--fix` finished with nothing left over |
| `2` | Usage |
| `3` | There is work here: a rewrite waiting for `--fix`, or a class attribute only you can take apart |

Do the upgrade in one sitting, in this order:

```sh
rastrillo doctor --fix     # 1. re-copy tokens.css
rastrillo markup           # 2. read what would change
rastrillo markup --fix     # 3. write it
go test ./...
```

Step 1 is not optional. `tokens.css` is copied into your `static/` at
scaffold time and frozen there while the partials it styles keep
upgrading, so an app on the old, class-only stylesheet whose partials
have started emitting attributes renders unstyled. That is the one
failure this staged migration exists to avoid.

Rewriting is idempotent, so running it twice, or over a tree half of
which is already done, changes nothing the second time. Nothing breaks
if you stop between steps 1 and 3, either: `tokens.css` styles both
spellings until stage 3, and `rastrillo.js` dismisses menus written in
either one. Step 3 is what you owe stage 3, not what you owe today.

### One name changed meaning

`rst-form-foot` used to be the sticky save bar you wrote by hand, and
`rst-form__foot` the plain closing row the `form-foot` partial emits.
BEM's `__` flattens to a hyphen, so both wanted the same attribute, and
one attribute cannot carry two rules. The partial's row took the name
the partial is called:

| Was | Is |
|---|---|
| `class="rst-form__foot"` — the partial's closing row | `rst-form-foot` |
| `class="rst-form-foot"` — your sticky save bar | `rst-form-bar` |
| `class="rst-form-foot__note"` | `rst-form-bar-note` |

**This is the breaking change in the release.** If you hand-wrote a save
bar and do not run the codemod, your `class="rst-form-foot"` now means
the plain row: no border, no background, and it no longer sticks.
`rastrillo markup --fix` applies the rename, and prints a reminder on
every run that changes anything.

### The vendored files

It never touches `static/tokens.css`, `static/theme.css`,
`static/rastrillo.js`, `static/select.js` or `static/datetime.js`. Those
are copies of the library's, and `doctor` is what refreshes them;
rewriting one here would make your copy differ from the library's for
good.

### The one opt-out

A line carrying `markup-spelling: old-spelling begin` starts a region
the tool will not rewrite; `markup-spelling: old-spelling end` closes
it, and an unclosed one runs to the end of the file. Use it for the
paragraph of your own documentation whose subject is the spelling you
are migrating away from — which is what this page is, and why this page
survives its own tool.

### What it reports instead of guessing

It leaves alone — and prints — any class attribute whose shape it cannot
read: markup built by concatenating string literals, a class list a
template assembles in a way it cannot take apart, or markup written as
escaped text in a page of your own documentation. A wrong guess renders
unstyled and looks like markup somebody wrote on purpose. Those keep the
exit code at 3 until you have dealt with them, so a CI gate cannot mark
a half-migrated app done.

### Your own stylesheet

The report ends with the `.rst-` selectors in CSS you own — every
stylesheet except a byte-identical copy of the library's, and every
`<style>` block in a template. Utility classes are left out; they are
still classes and your rules still match them.

A rule you wrote against `.rst-lrow` stops matching the moment your
markup says `rst-lrow`, and no test in your app will notice. Change them
to attribute selectors — `.rst-lrow` becomes `[rst-lrow]` — which weigh
exactly the same, so nothing in your cascade moves.

<!-- markup-spelling: old-spelling end -->

## rastrillo vectors

Go↔JS parity vectors: the app's `cmd/genvectors` enumerates golden cases
from the Go engine, this verb writes them to `test/vectors.json`, and
the app's JS suite must reproduce every one. The derivation engine an
app runs client-side exists twice by necessity, and two engines drifting
is the E2EE bug class where a wrong answer looks fine.

```sh
rastrillo vectors [--init] [--check] [dir]
```

Plain `vectors` runs the app's own generator and writes its stdout to
`test/vectors.json` — a new root-level directory, chosen because the JS
suite is neither a Go package nor a static asset.

| Flag | Purpose |
|---|---|
| `--init` | Scaffold `cmd/genvectors`, the test/ parity suite, and the go-test belt into an existing app (once) |
| `--check` | Pre-ship gate: regenerate, byte-compare, then run the JS parity suite with `node --test test/parity.test.mjs` |

`generate --check` runs this same gate automatically when
`cmd/genvectors` exists — one gate before ship, not two to remember; CI
that already runs `generate --check` needs no extra step.
