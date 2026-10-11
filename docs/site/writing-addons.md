# 🤖 Writing an addon

You built something in one Rastrillo app, and other apps need it too. It goes either in the framework (core) or in an addon: a separate Go module an app adds when it wants it. Decide which first, because the two paths are different work.

## Core or addon

Core holds what every app needs and what is hard to get right twice. An addon holds what many apps want but not every app should carry.

Put a change in core when it fills a gap in something core already ships. For example, extending a partial in `ui/partials/`, or adding a hook inside a core package such as the `db` handle's write pool. An addon could only override or wrap the core code, which is the copying you set out to end.

Make an addon when the feature brings its own tables and its own policy, and that policy will keep changing on its own schedule. Roles, data retention and notification rules are policy. idear (accounts, roles, invitations) is an addon. So is aviso (Web Push).

"Not every app needs it" is not enough on its own. Passkeys, background jobs and manifests are core, and not every app uses them.

Core also costs more to add to. Every exported name in a core package needs a reference page, and `internal/docsite/symbols_test.go` fails the build when one is missing.

If you are unsure, ask before you write code. Open a discussion on the Rastrillo repository, as an Idea or a Feature Request, and say what each app built and where:

```sh
amadan discuss new rastrillo/rastrillo -title "..." -body -
```

## What an addon costs

An addon is built against one Rastrillo version, and Go uses a single Rastrillo for the whole build. When a core package the addon uses changes in a way that breaks it, such as `sessions` or `migrate`, apps cannot upgrade Rastrillo until the addon catches up.

An addon also needs its own CI, its own frozen migration checksums and its own releases.

## The four rules

[Addons](/docs/addons) sets out four rules. The addon imports Rastrillo, and Rastrillo never imports the addon. The addon ships its own namespaced migrations, which the app merges into `BootSchema`. The addon ships its own `SKILL.md` inside the module. The addon does not re-implement sessions, CSRF, migrations, forms or flash.

## Ask core for a hook first

If the addon needs a hook that core does not have, propose it in a discussion. Land the hook in core on its own branch, then build the addon on it.

Use an existing hook when one fits. idear fills `auth.Config.Authorize`, which core leaves open for membership rules.

A hook must be useful to any addon, not a feature for one. idear needed sign-up to be refusable, so `password.ErrRefused` landed in core first, with tests and a line on the [password reference page](/docs/reference/password).

## Copy an existing repository

Start from aviso, the smaller of the two, and look at idear if your addon has more wiring. Two parts are not in aviso: take the frozen checksum test from idear's `schema_test.go`, and add your own `LICENSE`.

```text
go.mod            requires amadan.net/rastrillo/rastrillo at a released tag
<name>.go ...     the package
migrations/       0001_init.sql, embedded
migrations.go     var Schema = migrate.MustFromFS(migrationFS, "<name>")
schema_test.go    frozen migration checksums
example/          a working app
SKILL.md          for agents; read instead of the source
skillmd_test.go   byte budget, and the exported names it must mention
Makefile          one ci target
.amadan/ci        runs make ci
.amadan/ci.d/     one step per make target
README.md, LICENSE
```

Embed the migrations and use the addon's name as their namespace, so they never collide with the app's:

```go
//go:embed migrations/*.sql
var migrationFS embed.FS

// Schema is the addon's migration set. Merge it into the app's
// BootSchema, never its Schema: `rastrillo migration check` diffs
// Schema against Models and would propose dropping a table Models
// does not know about.
var Schema = migrate.MustFromFS(migrationFS, "aviso")
```

In the app, merge the addon's set into `BootSchema`. The arguments are applied in order:

```go
var BootSchema = migrate.Merge(sessions.Schema, aviso.Schema, Schema)
```

[Migrations](/docs/migrations) explains why it goes in `BootSchema` and not `Schema`.

Choose the addon's own licence. It does not inherit Rastrillo's.

## Run the gate

`make ci` is the one definition of the addon's checks. Each step in `.amadan/ci.d/` calls a make target and never keeps its own copy of a command, so change the Makefile and the step together.

The usual targets are `go vet`, a `gofmt` check, `go test ./... -count=1`, and a `-race` run with `CGO_ENABLED=1` set for that target only. `-count=1` stops a cached pass from standing in for a fresh run of a database test. If the addon has a browser half, drive it in a real browser with [harness](/docs/reference/harness). Leave `RASTRILLO_BROWSER_OPTIONAL` unset in the gate, so a run with no browser fails and does not skip.

If the example is its own module, as aviso's is, `go test ./...` at the root does not reach it. Test it from its own directory. [Testing](/docs/testing) has the patterns.

## Write SKILL.md

An agent reads `SKILL.md` instead of the source, so an inaccurate line is worse than a missing one. Cover how to install the addon, how to wire it into an app, its routes or API, the mistakes people make, and a checklist for a finished mount.

Keep it under a byte budget that a test enforces. aviso's is 9,000 bytes. If it must grow, raise the budget in the test and say why there. Do not cut a fact that prevents a mistake to make it fit.

`SKILL.md` ships inside the module, so the copy an agent reads matches the version the app builds against:

```sh
go get amadan.net/rastrillo/aviso
cat "$(go list -m -f '{{.Dir}}' amadan.net/rastrillo/aviso)/SKILL.md"
```

## Choose a module path

The path must work with `go get`. Addons the project maintains live at `amadan.net/rastrillo/<name>`, and amadan serves the `go-import` tag for them. To host yours there, ask in a discussion. Anywhere else `go get` reaches works too.

Before anything depends on the path, check it from a throwaway module, with your path in place of aviso's:

```sh
cd "$(mktemp -d)"
go mod init example.com/check
go get amadan.net/rastrillo/aviso
```

## Release it

Tag semantic versions, `v0.x.y` while the API is settling. Pin Rastrillo to a released tag in `go.mod`, not a commit.

When Rastrillo releases a breaking change to a package your addon uses, update the addon and tag a release soon. Apps cannot upgrade Rastrillo until you do.

## Get listed

Open a branch on `rastrillo/rastrillo` that adds an entry to [Addons](/docs/addons) (`docs/site/addons.md`). Follow the existing entries: status and version, module path and source link, the `go get` and `cat SKILL.md` commands, what the addon does, and what it deliberately does not do.

Describe the branch on amadan. It is reviewed and squash-merged like any other change. `AGENTS.md` in the repository has the procedure.

## Before you ask to be listed

- You checked it does not belong in core.
- Any hook it needed has landed in core.
- Its migrations are namespaced and merged into `BootSchema`.
- `SKILL.md` is under a tested byte budget.
- The example runs and its tests pass.
- `make ci` passes.
- `go get` works from a throwaway module.
- It has a released version and pins a released Rastrillo.
- It has its own licence.
