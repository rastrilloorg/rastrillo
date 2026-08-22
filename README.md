# 🤖 Rastrillo

The CARLOS web framework — the shape of a CARLOS app, the way the platform
(`carlosframework/platform`) is the shape of the deployment substrate it
runs on. Read the full design at
[`carlosframework/platform`'s spec library](https://github.com/carlosframework/platform/blob/main/docs/superpowers/specs/2026-08-01-carlos-framework-design.md)
(approved and merged 2026-08-01).

## Status

v1 was a walking skeleton, built overnight to prove the core loop end
to end. Two overnight passes since then split the remaining design
between them: one built the manifest system, the ui vocabulary,
fingerprinted assets and the scaffolded harness on main; the other
(`docs/superpowers/specs/2026-08-17-completion-design.md`) built the
subsystem packages against what the family's apps had hand-rolled in
the meantime. This list is their union. **Built:**

- **`rastrillo new [flags] <name>`** — scaffolds a Go app: `go.mod`, one
  starter action, a `main.go` wiring `rastrillo.Run`. Runs generate once
  so `go build` works immediately. Takes `--icons`, `--icon-delivery` and
  `--ux` — see **Icons and UX conventions** below.

- **Icons and UX conventions** — `--icons` (`lucide`, the default, or
  `font-awesome`), `--icon-delivery` (`inline`, the default, or `cdn` or
  `js`), and `--ux` (`considered`, the default, or `standard`). All six
  set × delivery combinations scaffold, compile and pass `--check`.

  The chosen set is written into the app as an ordinary app-owned
  `internal/<app>/icons` package — the same terms as `tokens.css` and
  `rastrillo.js`: delivered once, yours from then on. Its map is keyed by
  **rastrillo's slugs whatever the set**, so `{{icon "search"}}` means the
  same thing in every app and the shipped `ui/` partials never change when
  the set does. The scaffold wires both seams into the generated
  `render.go` and puts `{{iconAssets}}` in the layout's `<head>`; that
  renders empty for the inline default, so switching delivery later needs
  no template edit.

  Those slugs are rastrillo's own vocabulary, not a vendor's, and that is
  load-bearing rather than pedantic: five of the eleven differ from
  Lucide's canonical names (`kebab` is Lucide's `ellipsis-vertical`, and
  v1 renamed `check-circle`, `alert-triangle`, `x-circle` and
  `help-circle`), so even the Lucide set carries a translation table.
  `rastrillo.IconSlugs` is the list, and `internal/iconsets` asserts every
  scaffoldable set covers all of it — an icon added to `icons.go` that a
  set cannot answer would otherwise vanish the moment someone passed
  `--icons`.

  Inline vendoring stays the default and the recommendation — no build
  step, no second origin, works offline, which is what `icons.go` has
  always been for. `cdn` and `js` are fully supported rather than
  discouraged: each prints its specific cost once at scaffold time,
  records it as a comment in the generated package, and is never mentioned
  again. The cost worth repeating is `js`'s — icons do not render at all
  without JavaScript. Both remote modes pin exact versions with real SRI
  hashes.

  `--icons=font-awesome` means Font Awesome **Free**. Pro is a paid
  product rastrillo cannot vendor or link on your behalf, so Pro-only
  icons will not resolve; a Pro licensee wires their own kit through the
  same seam, since the icons package is app-owned source. Choosing
  `font-awesome` also writes the CC BY 4.0 attribution its licence
  requires — that obligation is the app's, so it travels with the code.

  Versions are pinned (`lucide@1.33.0`, `lucide-static@1.33.0`,
  `@fortawesome/fontawesome-free@7.3.1`) and nothing re-pins them
  automatically. A version changed without its hash fails as an unstyled
  page rather than an error, so check both together with:

  ```
  go test -tags pins ./internal/iconsets/
  ```

  That verifies every pinned URL still hashes to the integrity value
  shipped beside it — a mismatch means the bytes changed under a version
  that is supposed to be immutable, which is serious — and separately
  reports whether a newer release exists, which is only informational.
  Build-tagged, so the ordinary suite and CI never depend on jsdelivr or
  the npm registry being up: a check that fails when someone else's CDN
  has a bad afternoon teaches people to ignore it. Run it at release.

  `rastrillo generate --check` fails when a template names an icon nothing
  answers — both `{{icon "x"}}` and the commoner form where the slug
  reaches a partial as data (`dict "ActionIcon" "plus"`). At run time an
  unknown slug still renders nothing rather than crashing a response; this
  is the pre-ship gate, the same posture as the i18n catalog check. A slug
  computed at run time cannot be checked, as with any static gate.

  `--ux` seeds a UX convention profile into the app's `AGENTS.md`, which
  carries the app's instructions and is the source of truth from then on;
  `CLAUDE.md` is a one-line `@AGENTS.md` import so the instructions reach
  whatever agent someone uses rather than one of them. **The profile is a
  seed, not a live binding.** The resolved list is written once, an
  explicit flag beats the profile's default so the file never lies about
  what the app does, and nothing re-reads the profile name afterwards —
  which is what makes editing a line as valid as picking a profile, and
  what stops a rastrillo upgrade changing a shipped app's UX. Conventions
  marked `[x]` are enforced by a vendored component; `[ ]` ones an agent
  applies by hand, and the gap between the two is kept visible rather than
  blurred.

  The conventions in `considered` are rastrillo's own, and the profile is
  named for what it does rather than after anyone else's work. For wider
  reading on interface quality,
  [impeccable.style](https://impeccable.style/), the
  [WAI-ARIA Authoring Practices](https://www.w3.org/WAI/ARIA/apg/) and
  [Inclusive Components](https://inclusive-components.design/) are all
  worth your time — offered as reading, not as anything this framework
  endorses or claims to implement.
- **`rastrillo generate [dir]`** — the filesystem-routing generator
  (design doc §4): walks `actions/`, emits `gen/router.go` on a Go 1.22
  `http.ServeMux`. Fails loudly on route collisions. Action files carry
  `//go:build rastrillo_actions` (scaffolded for you; stripped from the
  compiled copies under `gen/`) so `go build ./...`, `go vet ./...` and
  `go test ./...` skip generator input instead of failing on it —
  `generate --check` names any file missing the constraint.
- **`rastrillo dev [dir] [-- app args]`** — the development watch loop
  (design doc §11): watches `app/`, `actions/`, `manifest/`, `cmd/`,
  `internal/`, `locales/`, `templates/`, and `static/` by polling. On any change, reruns `rastrillo
  generate`, builds the app's `./cmd/<name>` package to a temporary binary
  (cleaned up on exit), and restarts the running process (graceful
  SIGTERM). A failed generate or rebuild keeps the previous build serving;
  a failed restart keeps the loop watching too — either way, the next save
  retries. Expects the `rastrillo new` layout: exactly one directory under
  `cmd/`. Useful for rapid iteration: edits to `actions/` require
  regeneration (the binary uses generated code under `gen/`), and `dev`
  does that automatically. It also warns — never generates — when an
  app's models have outrun its migrations.
- **`rastrillo migration <cmd> [dir]`** — schema changes on the GORM
  path: `generate` diffs `models.go` against the migrations and writes
  the delta, `check` is the CI gate that fails when the two disagree,
  `new` stubs a hand-written migration, `status --db` reports what a
  real database has applied, and `baseline --db` is the operator's
  escape hatch for a deployed database `Apply` refused to adopt.
  `generate` and `check` need no database at all — both sides of the
  diff are computed in memory.
- **`rastrillo.Run`** — the process entrypoint the scaffold wires up: it
  resolves whichever of the platform's two activation argv shapes the
  binary was invoked with — `-socket`/`-addr`/`-db` flags for an agent
  exec child (hibernate routes), or a bare `serve` subcommand with no
  flags for a `carlos-app@.service` unit tenant — then calls `Serve`. A
  relative `-db`/`Options.DBPath` is resolved inside `$STATE_DIRECTORY`
  when systemd provides one, since a unit tenant's cwd isn't its state
  dir. Hibernation requires nothing else from the app: the activator
  owns the restore/replicate cycle, and `Serve`'s SIGTERM drain fits
  inside its SIGKILL budget. `rastrillo.Resolve` is the same resolution
  without the serving, for apps that need the resolved invocation before
  doing anything else with it — e.g. one that wants the resolved
  `DBPath` without going through `Options.Router`.
- **`rastrillo.Serve`** — the bootstrap (design doc §5): the SQLite
  pragma-ordering fix, `SetMaxOpenConns(1)`, additive migrations; the
  platform's activation contract (`Options.Socket`/`Options.Addr`/systemd
  `LISTEN_FDS`, matching `carlosframework/platform`'s `testdata/echoapp`
  exactly); `GET /healthz` and `GET /api/version` answered automatically.
  An app that keeps its database in `Ctx` sets `Options.Router` instead
  of `Options.Mux` and is handed the `*sql.DB` Serve opened;
  `rastrillo.OpenDB` is the same corrected opener exported for tests.
  `Options.Wrap` is the app-middleware seam: it wraps the app's mux
  (sessions, CSRF, panic pages, authorization) inside the framework's
  chrome — `GET /healthz`, `GET /api/version`, and locale-prefix stripping
  stay outside it, so probes never traverse app middleware and
  middleware sees the same paths routes match on.
  Between `Serve` and `Run`, the activation contract is covered end to
  end: every route kind the platform runs — always-on instance,
  hibernating exec child, unit tenant — boots the same scaffolded app.
- **Localization** (design doc §10) — `Options.Locales`/`DefaultLocale`/
  `LocaleFS` declare an app's locale set and supply its catalogs from an
  `embed.FS` carrying `locales/<code>.toml` (flat `key = "value"` TOML).
  Each request resolves a locale in order: URL path prefix (stripped
  before the app's mux sees it, so `/fr/orders` and `/orders` reach the
  same route), then `Accept-Language` (q-ordered, so a browser sending
  `fr-CA` matches a declared `fr`), then the `rastrillo_locale` cookie,
  then the default. Actions call request-scoped `rastrillo.T(r, key)` /
  `Tf(r, key, args...)` (`{name}` interpolation) for translated strings;
  lookup falls back through the requested locale's catalog, the default
  locale's catalog, the framework's base catalog, and finally the key
  itself — a missing translation stays visible on the page, never blank.
  The framework base catalog (`rastrillo.BaseCatalog()`, wired into every
  `Serve`d app's `Locales` automatically) carries `rastrillo/ui`'s own
  `rastrillo.ui.*` strings, so a single-locale app gets correctly-worded
  built-in components without writing a catalog of its own; an app
  catalog entry for the same key still wins. `rastrillo generate --check [--default-locale
  <code>]` fails loudly when a non-default catalog is missing keys the
  default has (§10's "silent fallback while iterating, loud failure
  before ship"); that gate runs under `--check` only — plain `rastrillo
  generate` (and so `rastrillo dev` and `rastrillo new`) never fails on an
  incomplete catalog. `--check`'s `--default-locale` flag defaults to
  `en` and is not read from `Options.DefaultLocale` — if an app sets a
  different `DefaultLocale`, pass the matching `--default-locale` by hand
  or the check compares against the wrong catalog. Nothing writes the
  `rastrillo_locale` cookie yet; persisting a user's locale choice across
  requests is the app's job for now. Two honest caveats: an app that
  declares locale `en` can't serve an app route whose first path segment
  is also `en` — inherent to prefix routing, not a bug to fix; and a
  `ServeMux` trailing-slash redirect issued under a locale prefix
  currently emits the unprefixed path, dropping the locale on that one
  redirect (known limitation).
- **`rastrillo/ui`** — the component/UI vocabulary (design doc's List
  screens plus the display, form, and route families): badges, meters,
  person cells, callouts, fields, choice cards, toggle blocks, seg-tabs,
  confirm forms, bulk select, modal shells, and the rest of the List
  screen set — with framework strings resolved through the §10 locale
  chain. An app registers `ui.Funcs()` (`dict`, `list`, `icon`, `T`) on
  its own template tree and `ParseFS`s `ui.Templates()` alongside its own
  templates; `ui.TokensCSS()` is the design-token stylesheet
  `rastrillo new` writes once into a new app's `static/` directory, app-
  owned from then on. `T` resolves a partial's own hardcoded-English
  default (e.g. `pagination`'s "Pagination", `confirm-form`'s "Cancel")
  through the framework base catalog — a caller-supplied value always
  wins over it — and `ui.FuncsWith` lets an app rebind `T` to a
  request-scoped `rastrillo.T` lookup so those defaults resolve in the
  request's locale instead. See `ui`'s package doc for the full class
  idiom vocabulary (list grid, dropdown, filter tokens, help tooltip,
  selection checkbox) that isn't a Go template partial.
- **`examples/helloworld`** — a real scaffolded app, checked in, proven
  to ship/promote/serve through the actual `carlos` binary — see
  [`hack/local-deploy-demo.sh`](hack/local-deploy-demo.sh).
- **Manifests** (design doc §9) — declare a `rastrillo.Resource` and
  `rastrillo generate` builds its store, its four screens' worth of
  actions and templates, and their locale keys, with `sqlc` query
  colocation for the store. See "Manifests" below.
- **`examples/blog`** — a whole app built from stock parts: `ui`'s
  partials, no JavaScript, and (new on this branch) a manifest-declared
  resource adopted alongside hand-written actions and ejected
  templates — see [`examples/blog/README.md`](examples/blog/README.md).
- **`examples/tickets`** — the fully generated proof: one manifest
  resource, zero hand actions, zero ejected templates — see
  [`examples/tickets/README.md`](examples/tickets/README.md).

- **`rastrillo/crypto`** — the family envelope: ECDH P-256 ephemeral →
  HKDF-SHA256 → AES-256-GCM sealing (`ephPub(65) ‖ iv(12) ‖ ct`), ECDSA
  raw r‖s signing over `SHA-256(context ‖ 0x00 ‖ msg)`, the symmetric
  half (`Derive`/`SealSym`/`OpenSym`), keypair marshalling, and a
  WebCrypto JS twin (`crypto.JS()`), all proven against amadan's pinned
  golden vectors — the compatibility contract that lets amadan,
  seapointish and keymail delete their local copies.
- **`rastrillo/gormlite`** — a GORM SQLite dialector over
  `modernc.org/sqlite`, a minimal fork of `glebarez/sqlite` that keeps
  Rastrillo on current modernc without a double driver registration.
- **`rastrillo/db`** — opens the app's SQLite database as one
  `*gorm.DB` with the pragma order `OpenDB` already got right, split
  into a writer pool capped at one connection and a multi-connection
  reader pool, routed transparently by `dbresolver`.
- **`rastrillo/migrate`** — one ledgered schema mechanism for the GORM
  path, replacing `AutoMigrate` plus per-package raw SQL: ordered,
  namespaced `Set`s applied once each at boot, each inside its own
  `BEGIN IMMEDIATE` with its ledger row, on a pinned connection so a
  table rebuild can bracket `PRAGMA foreign_keys`. A database that
  predates the ledger is adopted — stamped, zero DDL run — or refused
  loudly with a structural diff; it is never silently migrated.
  Migrations are immutable once applied and forward-only: production
  rollback here is the platform restoring the SQLite file, not a `Down`
  function.
- **`rastrillo/sessions`** — the SQLite-backed session core: signed-in
  sessions as real rows (sign-out and admin revocation both work),
  `__Host-` cookies on https origins, and the request-context surface (`Current`,
  `UserID`) every identity plugin calls `SignIn` into.
- **`rastrillo/csrf`** — `Protect`, a same-origin middleware for
  state-changing requests, checked in order of evidence quality:
  `Sec-Fetch-Site`, then `Origin`, then `Referer`.
- **`rastrillo/flash`** — one-shot notice messages carried in an HTTP
  cookie and cleared once read; display state, not a record.
- **`rastrillo/form`** — the framework-independent form helpers a
  generated handler needs: money parsing/formatting and a field error
  map, shared by generated and hand-written handlers alike.
- **`rastrillo/view`** — the plain HTTP-response helpers a generated
  action needs against a `*rastrillo.Ctx`: `Render`, `Fail` (safe
  500s, logged), and `ParseID` for the `{id}` path value.
- **`rastrillo/scope`** — `Owned`/`OwnedBy`, the GORM query scopes that
  make per-user ownership the short query: a row that isn't yours is a
  row that doesn't exist, so handlers answer 404, never 403.
- **`rastrillo/jobs`** — background work you can watch: `Start` runs a
  function in a goroutine and hands back an id, and `NewHandlers` turns
  that id into two routes the app mounts behind `sessions.Require` and
  renders with its own callbacks — a status page at `/jobs/{id}` and the
  fragment it polls at `/jobs/{id}/fragment`. Ownership is the session
  subject, so someone else's job, like someone else's row, 404s. The
  status page works with scripts off: while the job runs it carries a
  `<noscript>` meta refresh, and a finished job with somewhere to go
  answers 303 (the fragment's equivalent is 204 plus a
  `Rastrillo-Location` header). The registry is in-memory on purpose — a
  restart kills the goroutine, so a stored row would only persist a lie;
  work that must survive one belongs in `eventlog`. It is bounded, too:
  an owner holds at most four running jobs (`Start` answers
  `ErrOwnerBusy` past that), and a job still running after fifteen
  minutes is marked failed, its context expired. The only JavaScript
  in the framework is `static/rastrillo.js`, a ~130-line app-owned shim
  `rastrillo new` writes beside `tokens.css`: it replaces an element
  carrying `data-poll` with the HTML fragment it fetches and stops when
  the new fragment stops asking, and marks a submitting `data-busy` form
  busy. Status pages poll, or ride Server-Sent Events where the browser
  supports them: `Events` streams at `/jobs/{id}/events` (heartbeats,
  per-write deadlines, a bounded stream lifetime — the serve.go
  streaming recipe), and the shim's `data-poll-push` upgrades to an
  EventSource that falls back to plain polling on its own. htmx remains
  a choice, not a dependency — `examples/notes`
  demonstrates the whole loop with an Export flow.
- **`rastrillo/password`** — an email+password identity plugin on the
  sessions core, the same one-call `SignIn` contract auth's keymail
  flow honors, leaving storage, rendering, and CSRF to the app.
- **`rastrillo/auth`** — the family-default identity plugin on the
  sessions core: magic-link email sign-in that auto-upgrades to the
  keymail ceremony when the address has a claimed inbox (classification
  fails open, so every address always works), wrapping
  `keymaildev/signin` the way seapointish's reviewed integration does —
  explicit rate limiter, single-use links via `DELETE … RETURNING`,
  real session revocation, same-origin CSRF on every state-changing
  handler, an `Authorize` admission hook, and `RequireFreshSession`
  for step-up on sensitive routes.
- **`rastrillo/passkey`** — the WebAuthn second factor, both places it
  belongs: on the step-up seam (a signed-in user enrolls, and a
  valid-but-stale session is made fresh again by an assertion instead
  of a full re-sign-in) and at first sign-in (`Gate`, wired into a
  plugin's `SecondFactor` hook: a verified first factor becomes a
  pending half-session that only an assertion completes — the session
  it mints names both factors, `"magiclink+passkey"`). Single-use
  server-side challenges and half-sessions, subject-bound, over
  `rastrillo/webauthn`'s ceremonies and browser module. And the escape
  hatch: ten single-use recovery codes, minted behind `RequireFresh`
  and shown once, redeem at the gate by plain form POST — no
  JavaScript, because a lost passkey is exactly when WebAuthn isn't
  working — minting `"magiclink+recovery"` so the app can nudge
  re-enrollment. Sign-in only: step-up still takes a real assertion.
- **`rastrillo/webauthn`** — the passkey identity half, lifted from
  kass tests-and-all: ES256 only, no attestation checking, the CBOR
  subset reader, `LegacyRPID` for hostname moves, plus the `authtest`
  fake authenticator as a public sub-package and the browser half as an
  embedded ES module (`webauthn.JS()`).
- **`rastrillo/eventlog`** — the `Mergeable` store shape: append-only
  per-resource streams (many single-writer streams), a pure generic
  `Derive` fold, idempotent `Ingest` as the platform transport's seam,
  and a deterministic default merge order pinned by JSON vectors.
- **`rastrillo/blobs`** — content-addressed bytes: `S3FromEnv()` over
  the platform's object-storage primitive (`CARLOS_STORE_*`), a
  hand-rolled SigV4 signer + presigned GET/PUT pinned against the
  official AWS vectors, `Dir` and `Inline` backends (with the 4 KiB
  rule: bigger belongs in the object store), and `Sealed()` for E2EE.
- **`rastrillo/mail`** — the one outbound-email surface (SMTP or
  loudly-logged fallback, header-injection refused), signature-
  compatible with signin's Mailer.
- **Agents** (design doc §8) — actions opt in as tools (`var Tool =
  rastrillo.Tool{...}`), the generator emits the registry
  (`gen.Tools()`), the `tools` package renders schemas and dispatches
  registry-validated, consent-gated, actor-attributed calls through the
  same mux; `Options.Sidecar` + the `sidecar run` argv speak the
  platform's sidecar contract, and `Options.NextDue` answers the
  activator's `GET /api/next-due` scheduled-wake poll.
- **Serve seams** — `Options.Wrap` (the one middleware seam), exported
  `rastrillo.Handler` (Serve minus the listener, for test harnesses),
  and the scaffold's host awareness: a Makefile `ci` gate, executable
  `.amadan/ci` + `.amadan/ci.d/` steps delegating to it, an empty
  `manifest/` with a README, and a `CLAUDE.md` preload (§12).

**Not built yet**, honestly: the mergeable store's transport (edge
sync is the platform's designed territory — `eventlog.Ingest` is the
seam it will call, and until it lands, generated mergeable ids stay
writer-local and every generated event's actor is `"app"`); automatic
manifest-diff ALTER emission (generated stores emit only the initial
`CREATE`; evolving a declared resource's schema is the app's own
`rastrillo migration new` — and on the GORM path, reshaping a manifest
after an app's first boot trips the ledger's immutability guard, which
`migration baseline` is the way out of); richer manifest kinds beyond text/textarea/money (Bool,
Time, Select and Blob arrive as manifest slices); and any LLM client
(§8 leaves the provider per app).

## A known implementation decision worth flagging

The design doc's routing example puts multiple action files in the same
directory (`actions/orders/[id]/cancel.POST.go` next to `edit.GET.go`) —
but Go compiles one package per directory, so both files sharing a bare
`func Handle` would collide. The generator resolves this by never
compiling `actions/` in place: each file is parsed, its package clause
rewritten to a name unique to its route, and the result written into its
own directory under `gen/actions/`. Normal Go imports, no AST surgery
beyond the package clause. A future version could instead lift just the
`Handle` function via full AST extraction to get closer to "one file, no
package boilerplate," but that's real complexity, deliberately deferred
rather than rushed — see `internal/generate/generate.go`'s package doc.

## Manifests

Manifests are the declarative path — an optional, equal alternative to
hand-written handlers, not a requirement and not a legacy mode. The
two paths live side by side in one app, per resource: declare the
screens that are pure CRUD, hand-write the ones that aren't, and move
a resource between the paths whenever its needs change (eject one
generated file, or delete your hand files and re-declare). Design doc
§9's `Resource` sugar declares an entity once and `rastrillo generate`
builds its store, its screens, and their locale keys — a CRUD surface
for a fraction of the cost of writing each of those by hand, as
readable committed code that composes the same `form`/`view` helpers a
hand-written app uses.

Its vocabulary today is honestly scoped: one flat resource, three
field kinds (text, textarea, money), no relations. It reaches where
the code path does on ownership: `scope = "user"` makes every
generated query owner-filtered by the session subject — someone
else's row answers 404, the `scope` package's discipline, declared
instead of hand-written (`examples/notes` runs both halves side by
side and proves them with one two-user suite). Relations and custom
flows still take the code path; that boundary is where the generator
currently stops, not where it is fated to stop. Drop a manifest in
`manifest/posts.toml` when a resource fits the declared shape:

```toml
name  = "posts"
route = "/admin/posts"
store = "exclusive"

[list]
columns = [{ field = "Title" }, { field = "Status" }]
search  = true

[[list.filters]]
field  = "Status"
values = ["draft", "published"]

[form]
basics = [{ name = "Title", required = true }, { name = "Body", kind = "textarea" }]
```

(or build the same `rastrillo.Resource` value in `manifest/*.go` — a
typed alternative for a shape TOML can't express, evaluated with `go
run` against the app's own module, for when a resource's shape wants to
be computed rather than declared literally) and `rastrillo generate`
produces, per resource:

- **A store** — `gen/store/<name>/`: `schema.sql`/`queries.sql` (sqlc's
  own input, colocated per resource) plus `migrations.go` (the same
  table as `CREATE TABLE IF NOT EXISTS`, for `Options.Migrations`).
  Generation runs `go tool sqlc generate` against that input, so an
  app adopting a manifest must add the tool directive once:
  `go get -tool github.com/sqlc-dev/sqlc/cmd/sqlc`.
- **Actions** for the four canonical states plus the delete flow —
  list, show, new+create, edit (basics, plus advanced when the manifest
  declares `[form] advanced` fields), and delete as its own confirm-page
  URL: `GET <route>/{id}/delete` renders the question (a GET never
  mutates), only the sibling POST deletes — written straight into
  `gen/actions/`, compiled normally: unlike a hand action under
  `actions/`, a manifest's action files never pass through the
  filesystem router's own Discover/Rewrite step, so they carry no
  `//go:build` tag. Each hands its page to the app's own template tree
  through `Ctx.Render` — the one seam generated code needs, since it
  cannot call an app-private helper like a hand-rolled `blog.Render`.
  Page names are always `<resource>/list`, `<resource>/show`,
  `<resource>/form` or `<resource>/confirm`, regardless of which of the
  (up to) nine action files is rendering.
- **Templates** — `gen/templates/<name>/{list,show,form,confirm}.html`,
  composed entirely from the `ui` package's partials. `list.html` is
  gated on `search` at generation time: a resource with `search =
  false` gets no search box at all. A `[[list.filters]]` entry declares
  a filterable field and a set of enumerable values (e.g.
  `field = "Status"` with `values = ["draft", "published"]`): the
  generated list renders a dropdown control that filters by value and
  composes with search and pagination. Each filter value becomes a
  translation key `resource.<name>.filter.<field>.<value>`, plus
  `ui.all` for the all-items state. The bare `filter` field (superseded)
  validates but generates no control.
- **Locale keys** — `gen/locales/<default>.toml` (for humans/
  translators) and `gen/locales/locales.go` (a generated `BaseCatalog`
  var, wired as `Options.BaseCatalog`) carry a title-cased fallback
  label for every field and screen a resource declares, from one source
  map so the two files cannot drift — layered underneath whatever
  catalog the app supplies.
- **`gen/manifest.json`** — the whole resource set as one stable JSON
  artifact (sorted by name, two-space indent, evolution additive-only),
  for any future renderer or tool that wants a resource's shape without
  parsing TOML or running Go.

**Eject a template or action file, or skip generating one at all.** A
hand-written file already sitting at the exact path generation would
compute — `templates/<name>/list.html`, or `actions/<route
path>/index.GET.go` — is left alone: the generator writes nothing
there. That is the whole ejection story: copy a generated file's own
content out to its hand path (each file's header names the exact path
to copy to), and generation of that one file stops there; every other
file for that resource keeps regenerating normally. A route claimed by
two sources — hand and generated, or two resources whose computed
paths collide — fails the build loudly, the same as a filesystem-router
collision. `rastrillo generate --check` runs the whole pipeline into a
scratch directory and diffs it against the committed `gen/`, catching
both a stale/hand-edited generated file (idempotency) and a collision,
without writing anything.

**Filters** — at most one `[[list.filters]]` entry per resource. Field
values are validated at generation time (must name a declared list
column); each declared value must be non-empty, match `^[a-z0-9_-]+$`,
and appear only once — they travel in URLs and double as translation
keys, so they can't be arbitrary text. A filter's selection persists
across search and pagination (carried in the generated hrefs); the
dropdown's own open/closed `<details>` state does not survive
navigation.

**Required fields** — `required = true` on a form field marker adds a
client-side `required` attribute via the field partial AND generates
server-side validation: a blank submission re-renders the form with a
400 status and the field's own error message (e.g. "Title is required").
A `Money` field marked `required = true` still accepts `"0"` as valid —
the field must be present and parseable, not necessarily non-zero.

**Manifest-only apps** — a resource need not coexist with hand actions.
An app with *only* declared resources (and no `actions/` or
`templates/` directory at all) is legal: `rastrillo generate` produces
the whole store, all seven actions, and every template, compiled
normally, and the app runs without any hand-written route or screen
handlers.

**Migrations** — the generated `migrations.go` emits `CREATE TABLE IF
NOT EXISTS`. A fresh database runs the generated migration and works
out of the box. An existing database that predates a manifest field
addition needs an app-owned additive migration (e.g. `ALTER TABLE posts
ADD COLUMN status TEXT`): manifest edits regenerate code and
migrations, but the generated migration stays idempotent
(`IF NOT EXISTS`) — schema evolution is the app's own work (roadmap:
automatic manifest-diff ALTER emission). `examples/blog` shows the
pattern: the generated `CREATE TABLE IF NOT EXISTS posts` runs first,
then the app's own `ALTER TABLE posts ADD COLUMN published BOOLEAN`
runs after. This is all specific to the legacy `Options.Migrations` +
`OpenDB` path; an app on the GORM path (`db.Open`) manages schema
through the `migrate` package instead — see SKILL.md.

`store = "mergeable"` generates too (v0.16.0): the same screens over
an `eventlog`-backed store — each record one stream, deletes appended
as tombstones, reads derived by replaying the merged history —
`examples/tickets`' `announcements` resource is the generated proof,
tombstone test included. `examples/blog` shows what an app adds by hand
to cover what a manifest doesn't generate; `examples/tickets` is the
fully generated proof (one manifest resource, no hand actions or
templates).

## Release builds

A scaffolded app's `Makefile` has two build targets, doing different jobs:

- **`make build`** — the compile check. `go build ./...` with more than
  one package matched discards its output, so this catches a broken
  package without producing an artifact.
- **`make release`** — what ships: `-ldflags="-s -w"`, dropping the
  symbol table and DWARF, into `releases/<app>-<goos>-<goarch>`.

`release` cross-compiles for **linux/arm64 by default**, not for your own
machine, because that is what `carlos ship -target` defaults to. Building
a release on an amd64 laptop and shipping it is a silent architecture
mismatch — the upload succeeds and the binary fails to exec on the
instance. Override for a one-off with `make release RELEASE_GOARCH=amd64`.
The artifact is named for its architecture, and `make release` prints the
matching `carlos ship` command.

`releases/` is in the scaffolded `.gitignore`, along with the local
SQLite database the app creates when you run it and its write-ahead log.

Stripping is worth having because the compressed artifact is what gets
transferred. Measured across this family's own apps (titogo, amadan,
platform, slopbox, keymail, and a fresh scaffold): **23-31% off the raw
binary, and 45-53% off it after `zstd -19`**. A fresh scaffold goes
21.3MB → 14.7MB raw, 10.6MB → 5.1MB compressed.

What survives stripping, checked rather than assumed:

- **Panic traces are byte-identical**, function names and line numbers
  included — Go's `pclntab` is not touched by these flags.
- `debug.ReadBuildInfo()` works.
- `go version -m` still reports module metadata.

What you lose is source-level debugging with delve or gdb. Build without
the flags when you need that.

**`rastrillo dev` never strips**, deliberately: a debuggable binary is
what the dev loop is for. `carlos` itself is built the same way — see
`carlosframework/platform`'s own `Makefile` — so this closes a gap rather
than setting a new policy.

## Browser tests

Almost everything here is covered by ordinary Go tests. One thing is not:
`field-select`'s searchable enhancement needs a real JS engine, real
focus and real keyboard handling, so it gets a single chromedp drive:

```
go test -tags browser ./ui/
```

Build-tagged, so a plain `go test ./...` never half-runs it and chromedp
stays out of the ordinary build graph (`go list -deps ./...` pulls none
of it). It finds a Chromium on `PATH`, via `RASTRILLO_CHROME`, or in a
Playwright cache. **A skip is not a pass:** with no browser it fails,
unless `RASTRILLO_BROWSER_OPTIONAL=1` makes the skip a deliberate,
visible choice.

It is one test on purpose. A browser drive is expensive, so that test
drives the whole journey — render, enhance, filter, keyboard-select,
mirror back, submit — and asserts the server received the value a user
picked. It is loud about console errors and junk-scans the rendered text
for `undefined`, `[object Object]` and `NaN`: the bug class that renders
perfectly and says nothing.

Its assertions are written as the bug classes they catch, and each was
verified by breaking the script on purpose and watching the test fail —
including the one that found a real bug during development, where the
filter box kept the committed label so typing appended to it, matched
nothing, and silently committed the pre-existing value.

`chromedp` is pinned to v0.14.2 rather than the latest: newer releases
require Go 1.26, and a test-only dependency should not raise the module's
Go floor for everyone who imports rastrillo.

## Try it

```
go install github.com/carlosframework/rastrillo/cmd/rastrillo@latest
rastrillo new myapp
cd myapp && go mod tidy && rastrillo dev
```

Then edit an action, save, refresh — `rastrillo dev` regenerates,
rebuilds, and restarts for you. For a one-off build without the watch
loop: `go build ./cmd/myapp && ./myapp -addr :8080`.

Or via Homebrew: `brew install carlosframework/tap/rastrillo`.

To see it actually deployed through the real platform binary (local
directory store + local registry + `carlos edge -dev`, no AWS/SSH
required):

```
PLATFORM_REPO=/path/to/carlosframework/platform hack/local-deploy-demo.sh
```

## Live

[`https://hello.bdf.oncarlos.com`](https://hello.bdf.oncarlos.com) —
`examples/helloworld`, deployed for real on the CARLOS flagship: a
real S3-backed deployment bucket, a real `carlos edge`, a real Let's
Encrypt certificate — not the local-directory demo above. It runs as a
hibernating instance (`rastrillo.Run` speaking the activation
contract), wakes on the first request, and `/api/version` reports the
exact rastrillo commit it was built from. The old
`helloworld.dev.oncarlos.com` host belonged to the retired platform-dev
environment and no longer resolves to a registered route. App
hostnames live under `oncarlos.com`; `carlosframework.com` is reserved
for platform surfaces.

## See also

[carlosframework.com](https://carlosframework.com) for the architecture
rastrillo builds apps on top of, and
[`carlosframework/skills`](https://github.com/carlosframework/skills) for
the Claude Code skill capturing the family's conventions — including,
after this framework's first pieces landed, which of those conventions
rastrillo now enforces mechanically rather than asks you to remember.
