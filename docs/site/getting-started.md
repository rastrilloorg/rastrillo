# 🤖 Getting started

Rastrillo is the CARLOS web framework. This page takes you from nothing
to an app serving on your machine, and points at the page that explains
each piece properly.

## Install the CLI

```sh
go install github.com/carlosframework/rastrillo/cmd/rastrillo@latest
```

Rastrillo needs Go 1.25 or newer. The CLI is one binary with four
commands — [the CLI reference](/docs/cli) has all of them.

## Scaffold an app

```sh
rastrillo new notes
cd notes
```

`new` writes a complete app, not a stub. It compiles, its tests pass,
and it serves — before you have written anything:

```text
cmd/notes/main.go       Resolve -> db.Open -> App -> Serve
internal/notes/         models, migrations, app, handlers, render, templates, static
internal/notestest/     harness + example tests, passing out of the box
manifest/               the declarative path: drop a <name>.toml here
Makefile                make ci = vet + fmt + test + migration check
.amadan/ci, ci.d/       amadan runner CI, delegating to make
internal/notes/icons/   app-owned icons, edit freely
AGENTS.md               instructions + UX conventions, the source of truth
CLAUDE.md               an @AGENTS.md import, nothing more
```

Three flags shape what it writes: `--icons`, `--icon-delivery` and
`--ux`. All three have defaults worth keeping the first time through;
[Icons](/docs/icons) explains what each one costs.

## Run it

```sh
rastrillo dev
```

`dev` watches your source, regenerates, rebuilds, and restarts the
process on every save. A failed build keeps the previous one serving, so
a typo does not take your app down mid-thought — the next save retries.

Visit the address it prints. You have a working app with sign-in,
sessions, CSRF, a database and a migration ledger.

## Make the first change

The app you just scaffolded is five files plus `migrations.go`, and they
are worth reading in order before you edit them:
[The shape of an app](/docs/app-shape) walks through each one and
explains why `main.go` calls `Resolve` and `Serve` rather than `Run`.

From there the guides follow the order you will actually need them:

- [Data](/docs/data) — models, and the database handle `db.Open` returns.
- [Migrations](/docs/migrations) — how a schema change reaches a running app.
- [Scoping](/docs/scoping) — the one seam that keeps one user's rows out of another's.
- [Forms](/docs/forms) — reading input without ever binding a request onto a model.
- [Sessions](/docs/sessions) — signed-in state, CSRF, and step-up.

## What Rastrillo is not

It is a middle layer, not a full-stack framework. You write GORM models,
`net/http` handlers on a chi router, and `html/template` pages — all
ordinary Go, all yours. Rastrillo supplies the parts that are hard to
get right twice: the database opener, the session store, identity
plugins, CSRF, owner scoping, form helpers, background jobs.

There is no ORM of its own, no template language of its own, and no
router of its own. When you need to do something the framework has no
opinion about, you write Go, and nothing gets in the way.
