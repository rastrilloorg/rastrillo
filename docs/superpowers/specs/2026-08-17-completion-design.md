# 🤖 Rastrillo completion: the pending list, designed against what the family actually built

> 🤖 Written by Claude in the overnight rastrillo session, 2026-08-17,
> from a survey of every active repo in ~/github.com. Approved-by-default
> under Paul's overnight instruction; every decision below is traceable to
> a source named in §Provenance.

**Goal:** clear rastrillo.org's Pending list — the mergeable event-log
store, blobs, the crypto core, WebAuthn, agents, the rest of the manifest
surface — plus magic-link/keymail signup, CARLOS-platform awareness, and
amadan.net awareness, extracting from the apps that hand-rolled each
piece rather than inventing fresh.

**Parent design:** `carlosframework/platform`
`docs/superpowers/specs/2026-08-01-carlos-framework-design.md` (§ numbers
below refer to it). Where the platform moved since 2026-08-01, the
platform as-built wins over the design doc as-written; each such
divergence is called out.

---

## 0. Corrections to the brief, up front

Three things the research established that change how the pending items
land:

1. **The platform did not ship a native mergeable store.** The platform's
   `mergeable` storage class is still "designed, not built"
   (`platform docs/superpowers/plans/2026-08-01-roadmap.md:47`; zero code
   hits). What the platform *did* ship natively is (a) **Open Ledgers** —
   an append-only, hash-chained, *publication* primitive with no merge
   and no Derive (`internal/ledger/`), and (b) the **object-storage
   primitive** — a real per-app S3 bucket delivered as `CARLOS_STORE_*`
   env vars (`2026-08-10-object-storage-primitive-design.md`). So
   `rastrillo.Mergeable` is built here, in the framework, on SQLite —
   shaped so the platform's designed many-streams/one-merge contract can
   adopt it later — and blobs ride the native object store.

2. **The object store is control-plane only.** No presigned-URL service,
   no conditional writes, no versioning (spec §13). The framework mints
   its own presigned URLs with the delivered credentials, and blob
   dedup is best-effort (content addressing makes double-PUT idempotent
   in effect: same key, same bytes).

3. **In the signup flow, the magic link *is* the email fallback.**
   keymail-OAuth is the primary path; the magic link is what every
   non-keymail address — and every classification failure — gets.
   `github.com/keymaildev/signin` v0.1.0 (stdlib-only, tagged, shipped in
   seapointish) already owns that ceremony; rastrillo wraps it rather
   than writing a fourth implementation.

---

## 1. `rastrillo/crypto` — the envelope, to amadan's pinned contract

Amadan wrote rastrillo's contract for this package
(`amadan docs/superpowers/specs/2026-08-03-rastrillo-crypto-prompt.md`)
and pinned golden vectors at `internal/envelope/testdata/golden.json`.
Three apps carry copies (amadan `internal/envelope`+`internal/repokey`,
seapointish `internal/seal` — self-labelled "candidate to graduate
upstream into rastrillo" — and keymail's `crypto.go`). We implement the
amadan contract exactly, keeping its API shapes verbatim:

```go
type Keypair struct {
    SignPriv *ecdsa.PrivateKey // P-256
    BoxPriv  *ecdh.PrivateKey  // P-256
}
func Generate() (*Keypair, error)
func (kp *Keypair) SignPub() []byte  // 65-byte uncompressed point
func (kp *Keypair) BoxPub() []byte
func Seal(recipientBoxPub []byte, context string, plaintext []byte) ([]byte, error)
func Open(kp *Keypair, context string, sealed []byte) ([]byte, error)
func Sign(kp *Keypair, context string, msg []byte) ([]byte, error)   // SHA-256(context‖0x00‖msg), raw r‖s
func Verify(signPub []byte, context string, msg, sig []byte) bool
func MarshalKeypair(kp *Keypair) ([]byte, error)
func UnmarshalKeypair(b []byte) (*Keypair, error)
// symmetric half (amadan internal/repokey, keymail usage):
func Derive(key []byte, context string) []byte      // HKDF-SHA256, salt=nil, info=context, 32 bytes
func SealSym(key, plaintext []byte) ([]byte, error) // iv(12) ‖ AES-256-GCM ct
func OpenSym(key, sealed []byte) ([]byte, error)
```

Wire shape `ephPub(65) ‖ iv(12) ‖ ciphertext` (design doc §6). Amadan's
`golden.json` is copied into `crypto/testdata/` as the
cross-implementation fixture and the test suite must pass it. A JS twin
(`crypto/js/crypto.mjs`, WebCrypto, ES module, zero deps) is verified
against the same vectors by a node test run from Go when `node` is on
PATH, skipped otherwise.

**Deferred from §6:** `WrapKey`/`UnwrapKey`/`DeriveInvite`. Eleven's
real invite shape is unconfirmed against the doc's sketch; inventing it
here risks a wire format three apps would then have to migrate off.
The package doc says so.

## 2. Serve seams — the friction log, closed

Small API additions that three consumers asked for by name:

- **`Options.Wrap func(http.Handler) http.Handler`** — applied around
  the app's mux (inside the framework's `/healthz`, `/api/version`
  wrapping, inside locale middleware, same position amadan's outer-mux
  workaround produces today). Kills the workaround at
  `amadan internal/hub/server.go:471-481`.
- **`rastrillo.Handler(opts Options) (http.Handler, func() error, error)`**
  — everything `Serve` builds short of the listener and signals: opens
  the DB, migrates, resolves Mux/Router, wraps. Returns the handler and
  a close func. `Serve` now uses it; test harnesses stop duplicating
  `/healthz`+`/api/version` wiring and the DSN (the vitogo
  `internal/vitotest/harness_test.go:40-48` complaint, and seapointish's
  copied `apptest`).
- **`GET /api/next-due`** — the platform's scheduled-wake endpoint
  (`platform internal/activator/backend_exec.go:420-454`): when
  `Options.NextDue func() time.Time` is set, Serve answers
  `{"due": <unix secs>}`, gated by `Authorization: Bearer
  $CARLOS_ADMIN_TOKEN` (403 without; zero-time → `{"due": 0}`). Unset →
  404 as today.
- **Sidecar activation** (§8, platform contract): the activator spawns
  `<live binary> sidecar run` when `<host>-sidecar.env` exists
  (`backend_exec.go:726-773`). `rastrillo.Run` recognizes argv
  `sidecar run` and calls `Options.Sidecar` (see §7) instead of serving;
  a nil `Options.Sidecar` is a loud error, not a silent serve.

## 3. `rastrillo/mail` — outbound email, third copy retired

Extraction of the shape vitogo (`internal/vito/mail/mail.go`), kass
(`internal/mail`), and seapointish (`internal/app/signinflow.go:136-168`)
each hand-rolled:

```go
type Sender interface{ Send(ctx context.Context, to, subject, body string) error }
func SMTP(host, port, from, user, pass string) Sender  // net/smtp, STARTTLS via PlainAuth when user set
func Logged(logger *slog.Logger) Sender                // dev fallback, loudly labelled
func FromEnv(prefix string, logger *slog.Logger) Sender // <PREFIX>_SMTP_{HOST,PORT,FROM,USER,PASS}; missing HOST/FROM → Logged + warning
```

Every implementation guards header injection (CR/LF in to/subject —
seapointish's `errHeaderInjection`, vitogo's `headerSafe`). Plain text
only, v1.

## 4. `rastrillo/auth` — keymail sign-in with magic-link fallback

A turnkey wrapper around `github.com/keymaildev/signin` v0.1.0 (the
framework's second dependency; stdlib-only itself), following
seapointish's reviewed integration (`internal/app/signinflow.go`,
`actions/signin/`, `actions/auth/`) — the deliberate holes `signin`
leaves are exactly what a framework should fill once:

- **`auth.New(cfg auth.Config) (*auth.Auth, error)`** — builds **one**
  long-lived `*signin.Flow` (the README's trap #1: a Flow per request
  silently gets no rate limiting), with an explicit
  `signin.NewMemoryLimiter`, an HMAC pending key derived
  `sha256("rastrillo/auth/pending\x00" + cfg.InstanceKey)` refusing an
  empty instance key (seapointish's reasoning verbatim), and a
  `Classifier` whose HTTP client carries a RoundTripper rewriting
  `/api/lookup` → `/api/federation/lookup` — the signin↔keymail contract
  bug; the workaround is documented and removable when signin fixes it.
- **Storage** — `auth.Migrations` (additive): `auth_links` (hash PK,
  address, purpose, expires_at) with `TakeLink` as one
  `DELETE … RETURNING` (concurrency-correct even at MaxOpenConns(1)),
  and `auth_sessions` (token-hash PK, address, method, auth_time,
  created_at, expires_at).
- **Sessions** — crypto/rand 32-byte token, SHA-256 hash stored, cookie
  `__Host-rastrillo_session` on HTTPS origins / `rastrillo_session`
  otherwise (the vitogo TODO resolved: pick by origin scheme), HttpOnly,
  SameSite=Lax, real revocation on signout.
- **Handlers** the app mounts as actions or lets the manifest mount:
  `Begin` (POST; CSRF-checked, sets the 10-min pending cookie, redirects
  to keymail or "check your email"), `Callback` (GET /auth/callback;
  clears pending cookie the moment it's read; failed keymail approval
  redirects to retry-with-force, never dead-ends), `Verify` (GET
  /auth/verify?token=; magic-link landing), `Signout` (POST).
- **CSRF** — baked in, not left to each app (vitogo shipped same-origin
  checking broken twice): `Sec-Fetch-Site` when present
  (`same-origin`/`none` pass), else Origin/Referer host match, applied
  to every state-changing auth handler.
- **Admission** — `Config.Authorize func(address string) bool`; nil
  means any verified address gets a session (a members table is app
  policy, not framework).
- **Middleware** — `RequireSession(next)` + `auth.From(r)` returning the
  session's `Identity{Address, Method, AuthTime}`.

Mailer comes from `rastrillo/mail`. Step-up (`prompt=login`, gesture
windows) is deferred: `signin` doesn't expose it and only the console
needs it today; the session row already stores `auth_time` so step-up
can land without a schema change.

## 5. `rastrillo/webauthn` — the identity half, extracted from kass

Design doc §7 names the extraction source and kass carries it:
`kass/internal/webauthn` (stdlib-only, ES256/COSE −7 only, no
attestation checking, no third-party CBOR — an ~80-line CBOR subset
reader, tests included). We lift it as `rastrillo/webauthn` with
provenance noted, keeping its narrow-by-design surface (register:
parse+verify attestation object client data, extract COSE key; assert:
verify signature over authData‖SHA-256(clientData), counter, origin, RP
ID hash) and its package-doc stance: this proves *identity*; content
keys are `rastrillo/crypto`'s job and PRF stays client-side.

The acknowledged JS dependency ships in the package as an embedded,
reusable ES module (`webauthn/js/webauthn.mjs`): base64url helpers +
`register()`/`authenticate()` wrappers over `navigator.credentials`,
served by the app however it serves static files. Ceremony state
(challenge) sealing uses `crypto.SealSym` with an app key — the seam
seapointish and messenger each built privately.

## 6. `rastrillo/eventlog` — the Mergeable store shape (§5)

The Eleven shape, designed fresh in §5 since no app has extracted it;
woodstar's signed event log (`internal/core/event.go`) and messenger's
vectors-as-spec discipline inform the discipline, not the wire.

- **Schema** (`eventlog.Migrations`, additive):
  `events(stream TEXT, writer TEXT, seq INTEGER, lamport INTEGER,
  ts TEXT, actor TEXT, kind TEXT, payload TEXT(JSON),
  PRIMARY KEY(stream, writer, seq))` — one row per immutable event;
  `writer` is this instance's stream identity (many single-writer
  streams, one merge — the platform's designed `mergeable` contract,
  `2026-08-01-carlos-platform-design.md:184-195`).
- **API:**
  ```go
  type Event struct{ Stream, Writer string; Seq, Lamport int64; TS time.Time; Actor, Kind string; Payload json.RawMessage }
  func Open(db *sql.DB, writer string) (*Log, error)   // writer = instance identity
  func (l *Log) Append(ctx context.Context, stream, kind string, actor rastrillo.Actor, payload any) (Event, error)
  func (l *Log) Events(ctx context.Context, stream string) ([]Event, error) // merged order
  func Derive[S any](events []Event, reduce func(S, Event) S) S             // pure fold; replay = truncate & refold
  func (l *Log) Ingest(ctx context.Context, ev []Event) error               // idempotent import of another writer's stream
  ```
- **Merge order** — §15 open question 2 resolved as the doc's own lean:
  a **generic lamport default with an app-suppliable comparator**.
  Total order: `(lamport, ts, writer, seq)`; `Append` stamps
  `lamport = max(local max, seen max)+1`. Deterministic across
  edges by construction; a `Resource` can override the comparator when
  Eleven's real merge rule is finally confirmed (the seam exists, the
  default is honest).
- **`Ctx.Append`** (§5's `ctx.Append(stream, event)`) is *not* added to
  `Ctx` — Ctx stays the app-owned extension point; apps put a `*Log` in
  Scope or their own Ctx wrapper. The manifest's Mergeable store wiring
  passes the Log explicitly.
- Replay, convergence (same events any ingest order → same state), and
  idempotent ingest are property-tested; JSON event vectors live in
  `eventlog/testdata/` in the messenger vectors-as-spec style.

**Not built:** transport (edge sync) — that is the platform's designed
territory; `Ingest` is the seam it will call. Open Ledgers publication
is orthogonal (a *public* hash chain, no merge) and out of scope.

## 7. Agents — tools from actions, sidecars on the platform contract (§8)

- **`rastrillo.Tool`** — opt-in marker in an action file:
  `var Tool = rastrillo.Tool{Description: "...", Access: rastrillo.Read,
  Args: map[string]string{...}, Confirm: "Cancel order {id}?"}`.
  The generator (AST, same pass that rewrites package clauses) collects
  them into `gen/tools.go`: `func Tools() []rastrillo.ToolDef` with ID
  (route-derived), method, path, access, args, confirm. `generate
  --check` fails a write-access tool with an empty Confirm (§13's
  agent-gate check, the buildable half).
- **`tools` package** — `tools.Schemas(defs)` renders the registry as
  LLM tool schemas (JSON; MCP does not exist anywhere in the platform —
  schema JSON is the honest export), and
  `tools.Dispatch(mux, defs, call, actor)` re-validates a proposed call
  against the registry (unknown tool, undeclared arg → refused), builds
  the synthetic request, and invokes the same mux — "a tool call and an
  HTTP POST reach the identical Handle function" (§8). `Ctx.Actor` is
  set via the ctxFactory seam; consent for write tools is the app's
  confirm-page redirect (§8's model), enforced by Dispatch refusing
  write tools unless `call.Confirmed` is set.
- **`rastrillo.RunSidecar` → `Options.Sidecar`** — the thin harness §8
  promises: `Options.Sidecar func(ctx context.Context) (time.Time, error)`
  — one pass of the wake → read since bookmark → decide → act loop; the
  returned time is the next due wake (what `/api/next-due` reports on
  the serving instance). `Run` invokes it on `sidecar run` argv in a
  loop with backoff until SIGTERM. The LLM client itself stays app-side
  (§8 leaves the provider case-by-case); the framework ships the
  registry, schemas, dispatch, consent gate, and harness.

## 8. `rastrillo/blobs` — content-addressed bytes on the native object store

Messenger's `blobstore.go` seam ("the blobs table IS the journal —
write-once makes this embarrassingly simple") + the platform's
delivered contract:

- **`blobs.Store` interface** — `Put(ctx, r io.Reader) (Ref, error)`,
  `Get(ctx, hash)`, `Delete`, `URL(ctx, hash, ttl) (string, error)`;
  `Ref{Hash, Size, ContentType}`. Rows hold metadata only (§5).
- **Backends:** `blobs.Local(dir)` (dev; content-addressed files) and
  `blobs.S3(cfg)` with a stdlib-only SigV4 client (extracted from the
  family: kass `internal/carlos/objects.go` / messenger `home/sigv4.go`)
  including presigned GET/PUT — the platform provides no signing
  service, so the framework signs with the delivered credentials.
  `blobs.FromEnv()` reads the platform's `CARLOS_STORE_{BUCKET, REGION,
  ENDPOINT, ACCESS_KEY, SECRET_KEY}` and returns nil cleanly when absent.
  Keys: `blobs/<sha256>` in the app's own bucket (the design doc's
  deployment-bucket prefix is superseded — apps get their own bucket).
- **The 4 KiB rule:** `rastrillo.Blob` manifest Kind stores metadata in
  the row always; bytes ≤ 4 KiB *may* inline in SQLite
  (`blobs.Inline(db)` backend) but every doc string, the manifest
  generator's comments, and `generate --check` recommend the object
  store above 4 KiB — stated as guidance ("always recommend"), enforced
  nowhere, per the brief.
- E2EE apps wrap any backend in `blobs.Sealed(store, key)` —
  `crypto.SealSym` before Put, `OpenSym` after Get (§5's "sealed before
  they leave the process"), at the cost of content addressing the
  ciphertext.

## 9. The manifest surface (§3) — Resource, TOML sugar, codegen-with-skip

The headline: a `Resource{List, Form}` yields working screens without
the author naming them. Architecture — **static shape at generate time,
function values at run time**:

- **Types in the root package** (§3's spelling): `Resource{Name, Route,
  Store, List, Form, Delete}`, `List{Columns, Search, Filter}`,
  `Column{Field, Kind, Render}`, `Form{Basics, Advanced}`,
  `Field{Name, Kind, Required, Derived}`, `Delete{Confirm string}`.
- **Kinds** — the doc's four (`Text`, `Money`, `Meter`, `Blob`) plus the
  richer set the blog/apps actually need: `LongText`, `Bool`, `Time`,
  `Select{Options}`. `Money` is integer cents (§3). Each kind knows its
  SQLite column type, form control partial, and list rendering.
- **Two forms, one pipeline:** `manifest/<name>.toml` is lowered by
  `rastrillo generate` into a typed-Go `rastrillo.Resource` in
  `gen/manifest.go`; a hand-written `manifest/<name>.go` (package
  `manifest`, `var X = rastrillo.Resource{...}`) is read by AST for its
  static shape (Name, Route, fields, kinds — literal fields only) and
  referenced *by value* at runtime, so `Render` closures and validators
  work — the doc's "the need for code is the natural signal to switch
  files".
- **Codegen-with-skip (§3:196-201):** for each resource the generator
  computes the standard action set — `index.GET` (List), `new.GET`,
  `index.POST` (create), `[id]/edit.GET`, `[id]/edit-basics.POST` +
  `[id]/edit-advanced.POST` (two independent saves, §3's invariant, only
  when Advanced is non-empty; a plain Form gets one `[id]/index.POST`),
  `[id]/delete.GET` (the confirm page — §9's "destructive actions as
  their own confirm-page URL") and `[id]/delete.POST` — and emits each
  into `gen/actions/` **unless a hand-written file exists at the same
  path in `actions/`** — override-by-existence, silently, the one
  mechanism (§2). Ejecting is copying the file out.
- **Runtime**: emitted actions are thin calls into a new `screens`
  package — `screens.List(ctx, w, r, res)`, `screens.Form(...)`,
  `screens.Confirm(...)`, `screens.Save(...)`, `screens.Delete(...)` —
  which compose the existing `ui` partials (list-bar, pagination,
  status-pill, empty-state…) and plain generated SQL (blog-store shape:
  parameterized, LIKE-escaped search, COUNT+page). **sqlc is deferred**:
  the survey shows zero family apps using sqlc; generating the blog's
  proven hand-SQL shape delivers the same colocation promise without a
  new toolchain tonight. The portability lint (§5) defers with it.
  Delete for a Mergeable resource appends a tombstone event rather than
  `DELETE` (derive skips dead ids); for Exclusive it is a real `DELETE`.
- **Derived fields**, both senses: `Column.Render func(row map[string]any)
  template.HTML` (the Go-manifest-only escape hatch, §3:120) and
  `Field{Derived: true}` — shown, never editable, never in a save's
  column list (the §3:155 "computed field", read side).
- **Labels are translation keys** (§10): the generator emits
  `resource.<name>.field.<field>` keys with title-cased fallback —
  lookup already falls back to the key path today.
- `generate --check` grows: manifest/route collision, write-tool-
  without-Confirm, blob-inline-over-4KiB advice.

Proven end-to-end by a manifest-built resource in an example app plus
generator golden tests.

## 10. Scaffold: CARLOS golden target, amadan awareness (`rastrillo new`)

- **Makefile** with `build`, `test`, `vet`, `fmt-check`, and a `ci`
  target chaining them — the documented amadan fallback.
- **`.amadan/ci`** (executable, `exec make ci`) and **`.amadan/ci.d/`**
  steps (`10-vet`, `20-gofmt`, `30-test`), each `exec make <target>` —
  mirroring amadan's own live recipe; scaffold `chmod +x`es them because
  a non-executable step is the known silent-skip failure mode. No badges
  — amadan has no badge API; the README stub links the run page shape
  instead.
- **CLAUDE.md preload** (§12, the buildable core): the app-side
  conventions — actions can't hold shared code, additive migrations,
  the activation contract, where the manifest lives — so the next
  Claude session starts warm.
- CARLOS remains the golden target, not required: `Run`/`Serve` already
  speak the whole activation contract; scaffold README says both
  "`./app -addr :8080` anywhere" and the `carlos deploy` path.

## 11. Out of scope tonight, named honestly

sqlc + portability lint (§5); step-up auth; `WrapKey`/`DeriveInvite`
(§6); an LLM client (§8); mergeable transport/edge sync (platform's);
Open Ledgers client sugar; the `carlos vet` gates beyond `--check`
(§13); rastrillo-website updates (its accuracy contract says the list
moves when work *lands* — i.e., after this branch merges).

## Provenance

Every load-bearing source, by subsystem: **crypto** — amadan
`docs/superpowers/specs/2026-08-03-rastrillo-crypto-prompt.md`,
`internal/envelope` (+`testdata/golden.json`), seapointish
`internal/seal/seal.go`; **serve seams** — amadan
`internal/hub/server.go:471-481`, vitogo
`internal/vitotest/harness_test.go:40-48`, platform
`internal/activator/backend_exec.go:420-454,726-773`; **mail** — vitogo
`internal/vito/mail/mail.go`, kass `internal/mail/mail.go`, seapointish
`internal/app/signinflow.go:136-168`; **auth** —
`github.com/keymaildev/signin` v0.1.0 (+README caveats §), seapointish
`internal/app/signinflow.go`, `actions/{signin,auth}/`, keymail
`oauth.go`, `docs/apps.md`; **webauthn** — kass `internal/webauthn/`
(design doc §7 names the extraction); **eventlog** — framework doc
§5:283-306 + §15 Q2, platform doc §:184-195, woodstar
`internal/core/event.go`, messenger `.claude/memory/event-contract.md`;
**blobs** — messenger `blobstore.go`/`blobmirror.go`, kass
`internal/carlos/objects.go`, messenger `home/sigv4.go`, platform
`2026-08-10-object-storage-primitive-design.md`; **manifest** —
framework doc §3 (all sub-ranges), §9:545-549 (confirm pages), blog
example spec + `examples/blog`; **agents** — framework doc §8, platform
sidecar contract above; **scaffold/amadan** — amadan
`.amadan/ci.d/`, `internal/cli/runnerjob.go:181,274-334`,
`docs/superpowers/specs/2026-08-09-runner-ci-design.md`.
