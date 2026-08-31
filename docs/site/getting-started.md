# 🤖 Getting started

This page takes you from nothing to an app serving on your machine.

## Install the CLI

```sh
go install github.com/carlosframework/rastrillo/cmd/rastrillo@latest
```

You need Go 1.25 or newer. [The CLI reference](/docs/cli) has every
command; you will mostly use three of them.

## Scaffold an app

```sh
rastrillo new notes
cd notes
```

You get a complete app, not a stub. It compiles, its tests pass, and it
serves before you have written anything:

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

Five flags shape what gets written: `--icons`, `--icon-delivery`,
`--ux`, `--theme` (`day`, `plain`, `signal`) and `--shell` (`column`,
`topbar`, `sidebar`, `console`). The theme lands as `static/theme.css` and the
shell as `templates/layout.html`, both yours to edit from that moment.
Take the defaults your first time through. [Icons](/docs/icons) and
[Templates](/docs/templates) explain what each one costs when you care.

The templates include an `errors.html`, wired to `Options.ErrorPage`, so
a panicking handler lands on a styled page inside your shell rather than
a blank 500. Wire the same function to `Ctx.ErrorPage` and your own
failures land there too — until you do, `view.Fail` answers plain text.

## Run it

```sh
rastrillo dev
```

This watches your source and regenerates, rebuilds and restarts on every
save. A failed build keeps the previous one serving, so a typo does not
take your app down mid-thought.

Visit the address it prints. You have a working app with sign-in,
sessions, CSRF, a database and a migration ledger.

## Where to go next

Read [The shape of an app](/docs/app-shape) before you edit anything.
It walks through the five files `new` wrote and explains why `main.go`
calls `Resolve` and `Serve` instead of `Run`.

After that the guides follow roughly the order you will need them:
[Data](/docs/data) for models and the database handle,
[Migrations](/docs/migrations) for changing the schema,
[Scoping](/docs/scoping) for keeping one user's rows away from
another's, [Forms](/docs/forms) for reading input safely, and
[Sessions](/docs/sessions) for signed-in state.

## What Rastrillo is not

It is a middle layer, not a full-stack framework. There is no ORM of its
own, no template language of its own, and no router of its own. When you
need to do something it has no opinion about, you write Go and nothing
gets in the way.
