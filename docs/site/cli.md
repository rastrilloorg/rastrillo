# 🤖 The CLI

One binary, four commands. Install it with:

```sh
go install github.com/carlosframework/rastrillo/cmd/rastrillo@latest
```

Every command takes an optional directory as its last argument and
defaults to `.`. Flags come before the directory. `rastrillo help`, `-h`
and `--help` all print usage.

## rastrillo new

```sh
rastrillo new [--icons=<set>] [--icon-delivery=<mode>] [--ux=<profile>] <name>
```

Scaffolds a complete app in `./<name>` — one that compiles, passes its
own tests, and serves, before you have written anything. It runs
`generate` once on the way out so `go build` works immediately.

| Flag | Values | Default |
|---|---|---|
| `--icons` | `lucide`, `font-awesome` | `lucide` |
| `--icon-delivery` | `inline`, `cdn`, `js` | `inline` |
| `--ux` | `considered`, `standard` | `considered` |

All six set × delivery combinations scaffold, compile and pass
`generate --check`. The icon set becomes an ordinary app-owned package
under `internal/<app>/icons`, and `--ux` seeds a UX convention profile
into `AGENTS.md`, which is the source of truth from then on — nothing
re-reads the profile name afterwards. [Icons](/docs/icons) explains what
each delivery mode costs, including the one worth repeating: with `js`,
icons do not render at all without JavaScript.

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
and i18n catalog completeness. Only `--check` fails on an incomplete
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
