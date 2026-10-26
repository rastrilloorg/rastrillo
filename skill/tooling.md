# The CLI and the gate

`rastrillo new --theme=day|plain|signal
--shell=column|topbar|sidebar|console|stage <name>` (also `--icons`,
`--icon-delivery`, `--ux`, and `--auth=magic` for sign-in by emailed
link) writes the whole app shape plus `go.mod`,
migrations, templates, static assets, app-owned icons, a test harness
including the browser drive, a `Makefile` whose `ci` target is the
gate, and `.amadan/ci.d/` steps. It compiles, passes its tests and
serves before you write a line; never hand-write any of it.
docs/site/templates.md

`rastrillo generate` writes `gen/` and `api/v1.json` from `manifest/`:
commit both, never hand-edit; `make ci` runs `generate --check`
(an app made before it: add a `generate-check` target to `ci`). `make ci` also runs
staticcheck, govulncheck and gitleaks, pinned in the Makefile along with
the Go release (`GOTOOLCHAIN`); raise the tool pins with the go
directive, and `GOTOOLCHAIN` with each Go security release. `rastrillo
dev` watches, regenerates, rebuilds and restarts.
docs/site/getting-started.md

`rastrillo doctor [--fix]` compares `static/`'s vendored files with the
CLI's own copies. The scaffolded pin test, not doctor, is the standing
gate. An app importing `github.com/carlosframework/rastrillo` (the
pre-move path) is frozen at v0.23.0 and its pin test cannot see it: a
current doctor exits 5 and prints the migration. docs/site/cli.md

Manifests: a `manifest/*.toml` resource generates CRUD screens — field
kinds text, textarea, money; no relations; `scope = "user"`
owner-filters by session subject. `gen.Router` takes `gen.Boot`'s
result: Boot upgrades and checks money. A hand-written action file at a
generated path (carrying `//go:build rastrillo_actions`) takes that one
screen over; on a resource the API exposes it must call the screen's
operation (`skill/api.md`). docs/site/manifests.md

Optional clients: `amadan.net/rastrillo/pwa` adds installation and a
public offline fallback; `amadan.net/rastrillo/native` ships Swift
components and an Apple companion scaffold, with optional Go Mobile
guidance. Prefer native UI with platform conventions; use a native
shell around selected webviews when complexity warrants it. Load the
chosen kit's own `SKILL.md`. Neither supplies offline data
synchronisation. Discovery and installation: docs/site/addons.md

**Never import `github.com/glebarez/*` or `gorm.io/driver/sqlite`:**
glebarez re-registers modernc's `sqlite` driver name, so the binary
panics at init; the gorm.io one is cgo. `db.Open` already wires
`gormlite` over modernc.

**Your app's components stay in your app.** `ui` is rastrillo's
vocabulary, not yours. The day one app copies another's, extract it to
a shared module that week and delete the copy. A copy is the trigger —
"a second consumer appears" never fires.
