# Scaffold shapes: `rastrillo new --scaffold <shape>`

**Date:** 2026-10-25
**Status:** Draft for Paul's approval.
**Inputs:** Till's feedback on building a dashboard with Rastrillo
(2026-10-23); a survey of the 15 Rastrillo apps on docs-t1 plus the four
examples; Tito Go (`~/github.com/tito/titogo`) and Eleven
(`amadan.net/eleven/messenger`); the user-models roadmap
(`carlos/pegamento`, branch `design-user-models`,
`docs/superpowers/plans/2026-09-28-user-models-roadmap.md`).

## 1. Why

`rastrillo new` writes one app shape for everyone: about 40 files with
SQLite, GORM, migrations, a chi router, CSRF, the JSON API and its token
page, a browser lane, a perf lane, budgets, gitleaks, a ten-step Makefile
gate, a manifest directory and AGENTS.md. Till built a dashboard that
needs none of the data side and found it too much: chi is too much setup,
the SQLite requirement could not be got past (an in-memory database did
not help), and the structure did not read as Go.

The framework already allows a smaller app. `Serve` takes a stdlib
`*http.ServeMux`, runs with `DBPath` empty and hands `Router` a nil
database (`serve.go:71`, `serve.go:90`). Only one framework package
imports chi (`perf/perftest/screens.go`). The weight is in the scaffold.

The survey also showed the one shape fits few real apps. Most run one
instance per team or person behind a central home; the two most mature
(Tito Go, Eleven) do not use Rastrillo's scaffold at all, and Tito Go
uses Rastrillo only as a library of parts (`xlsx`, `csrf`, `background`,
`money`, `dbtest`, `clientip`, `table`).

## 2. Goals

1. Someone building a small app (Till's dashboard) gets only what it
   takes to put a styled page on screen, and can read all of it in a few
   minutes.
2. Every app picks a shape from a short menu, and the shape decides what
   gets written. The skill helps an agent pick.
3. The menu is the one place app shapes live. The user-models roadmap's
   planned `--model home|hub` (Plans 6 and 7) becomes two more entries on
   it, not a second flag.

## 3. Non-goals and anti-goals

- **Not** building `home` or `hub` here. They are roadmap Plans 6 and 7,
  and depend on Plans 2, 3 and 5, which have not landed.
- **Not** retiring manifest codegen. That is a separate project, after
  this one (§9).
- **Not** changing the platform's start-up contract (`<binary> serve`,
  `<binary> sidecar run`, the flags, `RASTRILLO_CHECK_CONFIG`). Requests
  to carlosframework, not workarounds, if a shape needs something new.
- **Not** adding operator subcommands to app binaries (`./app check`,
  `./app perf`). Till suggested it and Tito Go does it; it belongs with
  the coalescing work (§9).
- **Anti-goal:** a combinatorial matrix. A new axis that multiplies the
  existing tested combinations (icons × delivery × UX × theme × shell) is
  a failure even if each combination works. Shapes are few and named.
- **Anti-goal:** a shape whose fresh scaffold does not compile, pass its
  own tests and serve, exactly as today's does.

## 4. The menu

One flag, `--scaffold <shape>`.

| Shape | What it is | Real apps that fit |
|---|---|---|
| `bare` | A page and a route. No database, no sign-in. Design system on by default, easy to drop. | Till's dashboard, `examples/helloworld` |
| `single` | One instance whose data is the app's: SQLite, GORM, migrations, CSRF, the JSON API. No sign-in until the app adds it. **This is today's scaffold**, on a stdlib mux. | patrix, pullup, rutline's tenant, `examples/blog`, `examples/tickets` |
| `accounts` | Many users in the app's own SQLite: sessions, an identity plugin, owner-scoped queries, public and signed-in route groups. | `examples/notes`, the TodoMVC bake-off app |
| `home`, `hub` | Reserved. Accepted by the flag parser, refused with a message naming the roadmap plan that will build them. | oficina, correomona, comercio, ppl, Tito Go, Eleven |

**Default:** `single` (my call). It keeps `rastrillo new notes`, every doc
and every test that runs it doing what they do today, apart from the
router change.

### 4.1 `bare`

Files (the builder may adjust names to match the existing scaffold):

- `go.mod`: Rastrillo only. No chi, no GORM.
- `cmd/<name>/main.go`: `rastrillo.Run` with `Options.Mux`, no `DBPath`.
- `internal/<app>/app.go`: the routes and their handlers on an
  `http.ServeMux`, starting with `GET /`.
- `internal/<app>/app_test.go`: one test, `package <app>_test`, that
  proves the index renders.
- The design system: layout and index templates, the vendored static
  files, the icons package and the renderer that joins them, all
  delivered once and app-owned as today.
- `.gitignore` and a short README that says how to run it and how to
  grow it into `single`.

Not written: models, migrations, `schema.sql`, the JSON API and token
page, sessions, CSRF (there is nothing to change state), the browser and
perf lanes, the budgets file, gitleaks, the Makefile, AGENTS.md and
CLAUDE.md, the manifest directory. `go test ./...` is the whole gate.

**Design-system opt-out (my call):** a boolean `--design-system`,
default true. `--design-system=false` writes no templates, static files,
icons or renderer, and the handler writes plain HTML or JSON. With it
false, passing `--icons`, `--icon-delivery`, `--theme` or `--shell` is an
error, not a silent no-op. The flag applies to `bare` only (amended
while planning, 2026-10-25): `single` and `accounts` render their API
token page, sign-in pages and error page with the design system, so
without it they would need a second, plain copy of each. Passing
`--design-system=false` with them is an error that names `bare`.

Whatever `bare` needs to boot and deploy on Carloku must stay, and only
that. If boot needs `manifest/app.toml` for locales, the builder either
keeps a minimal one or makes boot accept its absence with the default
locale; either is fine as long as the result is the smaller app.

### 4.2 `single`

Today's scaffold, with three changes that apply to every shape:

1. **stdlib mux, not chi (my call).** `single` has no sign-in, so it has
   no route groups; a whole-app guard added later wraps the whole mux.
2. **Tests beside the code.** `internal/<app>test/` goes; its files move
   into `internal/<app>/` as `package <app>_test`, which is Go's own way
   to test a package from outside. The harness, browser, perf and
   vendored tests keep what they do.
3. **The migration check is a test.** A test in the app calls
   `modeldiff.Generate` (`migrate/modeldiff/modeldiff.go:79`) with the
   app's migrations and models and fails on any change, with the same
   message `rastrillo migration check` prints. The Makefile's
   `migration-check` step goes. The CLI command stays for people who
   want it; it only builds a loader and `go run`s it because it runs
   outside the app.

The perf lane needs `perftest.Screens` to stop requiring `chi.Routes`
(§6).

### 4.3 `accounts`

`single` plus what `examples/notes` wires: the sessions core, an
identity plugin, `scope.Owned` queries, and a chi router so public and
signed-in routes sit in separate groups behind `sessions.Require`. This
is where the August known-libraries decision still applies. Chi stays
for convenience, not necessity: `sessions.Require` wraps any
`http.Handler` (`sessions/sessions.go:345`), but a chi group puts a whole
set of signed-in routes behind it in one line, and the bake-off's winning
arms used it.

Which identity plugin by default (keymail `auth/` or `password/`) is
left to the builder, following what `examples/notes` and
`skill/add-signin.md` already recommend.

### 4.4 `home` and `hub` (reserved)

The flag accepts the names and refuses them with one line, for example:
`--scaffold home is not built yet: it is Plan 6 of the user-models
roadmap`. Nothing else here depends on them.

## 5. The skill

`SKILL.md`'s "Start here" becomes "Pick a shape": the menu table, a
sentence on how to choose (who the users are, where they live), and the
`rastrillo new --scaffold <shape>` line for each. The frontmatter
description stops saying every app is multi-user with chi routes.
`skill/add-signin.md` gains a short path from `single` to `accounts`.
`skillmd_test.go`'s size budget still holds.

When Plans 8 and 9 land, the roadmap's user-models chapter extends this
menu rather than writing its own.

## 6. Framework changes

- **`perftest.Screens` without chi.** `Routes` takes a small interface,
  or a list of GET paths, that chi's router can still satisfy, so the
  perf lane works on any mux. Existing chi apps must keep compiling with
  at most a one-line change, recorded in CHANGELOG.
- **`rastrillo new`:** the `--scaffold` and `--design-system` flags, the
  three shape templates, and the reserved names.
- **Docs:** `docs/site/getting-started.md`, `cli.md`, `app-shape.md`,
  `testing.md` and `compare.md` ("scaffolded apps use chi") updated to
  the shapes.

## 7. Constraints

- Every shape × design-system setting scaffolds, compiles, passes its
  own `go test ./...` and serves, tested the way
  `TestScaffoldMigratesAndPassesCheck` and the existing scaffold tests
  do now. The existing icon/theme/shell combinations are tested on one
  shape (`single`), not on all three.
- The framework gate is sharded (`.amadan/ci.d/50-suite@33`, 33 suite
  roles in the Makefile); a new suite needs a role and a bumped count.
- Locally the gate needs `GOFLAGS=-mod=mod` for three scratch-module
  packages.
- Branch `stateless-signin` (roadmap Plan 2) touches three
  `cmd/rastrillo` files. Whichever lands second rebases.
- Branch `gorm-measured-cost` records Tito Go's measured GORM costs. If
  it changes the advice on GORM, `single` and `accounts` follow it; this
  design does not decide GORM's place.

## 8. Left to the builder

File names inside each shape; the exact error texts; whether the
reserved names are listed in `--help`; how the README explains growing
from one shape to the next; the `perftest` interface's shape; the order
the work lands in.

## 9. Later, separately

**Coalescing with Tito Go and Eleven.** Rastrillo was extracted from
them, and the direction agreed on 2026-10-25 is that Rastrillo extracts
what they run and they adopt it over time. Roughly: TLS, the host proxy
and instance creation are the platform's (Tito Go's router came from
Eleven's and was kept close so it could move onto Carlos); root-domain
identity in an object store, one identity across several accounts, the
router passing an authorized person to the instance, and the single
binary with operator subcommands come to Rastrillo as the `home` and
`hub` shapes, on `objrec`; Eleven's connect broker and push relay are
the prior art for a glue shape. This means rewriting roadmap Plans 2, 3,
6 and 7 from their code, weighing Tito Go's forwarded-headers model
against the roadmap's assertion handoff.

**Retiring manifest codegen.** No real app uses it; only the four
examples do. It lost the bake-off and was frozen in August. Retiring it
means re-scaffolding the examples from shapes first. A small-model
bake-off can settle whether it helps weaker agents before anything is
deleted.
