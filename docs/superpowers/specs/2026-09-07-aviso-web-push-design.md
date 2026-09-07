# Aviso — Web Push as a rastrillo addon, extracted from Eleven

**Date:** 2026-09-07 · **Status:** DRAFT for review ·
**Source:** Eleven messenger (`github.com/elevenmessenger/messenger`):
`push.go`, `web/static/push.js`, the notification half of
`web/static/sw.js`, and their Go tests. **First consumer:**
birthday-alarm (reminders go out by mail today). **Second, when it asks:**
oficina/calendar.

Brainstormed by Claude and Codex in two rounds on 2026-09-07; every
ruling below was agreed by both, and the two points they argued are
recorded in §14 with the position that won.

## 0. What this is, and the answer to the question

The question was whether rastrillo should have a "PWA library with push
notifications" extracted from the Eleven PWA. The answer is **yes to
push, as an addon, and no to a PWA library.**

Rastrillo today has no web app manifest, no service worker support and no
Web Push (`manifest.go` is the *resource* manifest, CRUD sugar; `icons.go`
is an icon vocabulary). Eleven is the only app in the family that has
push, and its code splits cleanly: a transport half — SSRF-guarded client,
endpoint validation, subscription CRUD, VAPID keys, a bounded fan-out that
prunes dead endpoints — that is generic; and a policy half — who gets
told, what the payload means, how the worker decrypts and routes it —
that is Eleven's message model and nothing else's. The transport half is
about 230 lines of Go, 180 of browser JS and 40 of worker JS. That is
what this spec extracts.

There is no "PWA library" to extract. Eleven's service worker does no
caching and has no offline page; its manifest handler is twenty lines of
app identity. Installability is a recipe an app follows (§11), not code a
package can own — and it is needed, because iOS only delivers push to an
installed app.

Two rulings bind everything, both inherited from the addons doctrine
(`docs/site/addons.md`): **the arrow points one way** — aviso depends on
rastrillo, rastrillo never learns aviso exists; and **the app owns
policy** — aviso moves bytes to devices a subject enrolled, and never
decides who should be told what.

## 1. Rulings

Each one line, with the failure it prevents.

- **Addon, not core.** Push is real for many apps and wrong for every app
  to carry; a dependency (`webpush-go`) in core would be paid by apps
  that never push.
- **Named `aviso`** ("notice"): it names the user outcome, in the family's
  Spanish-word style, and collides with nothing in the family.
- **Keep `github.com/SherClockHolmes/webpush-go` v1.4.0, behind hidden
  types.** Extraction must not become a cryptography rewrite; RFC 8291's
  aes128gcm record format and RFC 8292's VAPID JWT do not fall out of
  rastrillo/crypto's envelope (which is P-256 ECDH/ECDSA, AES-256-GCM,
  domain-separated — the right curve, the wrong construction). A stdlib
  implementation is separately reviewed work, inside the addon, later or
  never.
- **The session subject owns a subscription.** Ownership is
  `sessions.Current(r).Subject`, never a client-supplied field and never
  `rastrillo.Actor` (which says human-or-agent, not who). Eleven's
  per-subscription bearer token (`token_hash`) does not carry over: it
  existed because Eleven has no server session.
- **Reject cross-owner endpoint upserts.** Eleven's unconditional
  `ON CONFLICT(endpoint) DO UPDATE` (`push.go:88`) would let a second
  account on the same browser silently take over the first account's
  device. Aviso answers 409.
- **Revision-conditional pruning.** A 404/410 deletes a row only if the
  revision the send captured is still current, so a slow send cannot
  delete a subscription the browser refreshed meanwhile.
- **Schema merges into `BootSchema`**, namespaced `aviso`, and has no
  foreign key to any users table: apps own their identity schema.
- **No boot-time key generation.** The VAPID private key is provisioned
  like every other family secret (one env var, refused when empty, §8).
  Eleven's mint-if-missing sidecar is the thing this rules out.
- **`enable` needs a gesture; `reconcile` never prompts; the worker helper
  never calls `skipWaiting` or `clients.claim`.** Permission prompts
  outside a click are denied by browsers and resented by people; lifecycle
  is the app's worker's business.
- **Every push shows a notification.** WebKit revokes push for a worker
  that receives without displaying; silent suppression is done
  server-side by not sending.

## 2. Package layout

A separate repository and module, `amadan.net/rastrillo/aviso`, on the
idear pattern (`docs/site/addons.md`; source `github.com/rastrillo/idear`):

```
aviso.go        Config, New, Service, errors
store.go        List, DeleteSubject, Sweep, the CRUD the handlers use
send.go         Send, SendTo, fan-out, pruning
http.go         PublicKey, Subscribe, Unsubscribe
vapid.go        GenerateKey, key parsing, key id
ssrf.go         dial guard and endpoint validation (from Eleven's
                push.go:25-70 and unfurl.go:160)
migrations.go   Schema = migrate.MustFromFS(migrationFS, "aviso")
migrations/0001_init.sql
js.go           JS() and WorkerJS()
js/push.mjs     browser module (from push.js)
js/aviso-sw.js  classic worker helper, exposes AvisoSW
js/*.test.mjs   node tests, the vault/js precedent
cmd/aviso-key   prints one private key
SKILL.md        byte-budgeted, tested, like the framework's
docs/installable.md  the §11 recipe
```

Nothing in `github.com/carlosframework/rastrillo` imports aviso. The
directory page in `docs/site/addons.md` gains one entry.

## 3. Go API

```go
package aviso

type Config struct {
    DB          *sql.DB
    PrivateKey  string // unpadded base64url, 32-byte P-256 scalar; required
    Contact     string // VAPID "sub": a mailto: or https: the push service may contact
    Origin      string // the app's origin, for csrf.SameOrigin and click-URL checks
    Concurrency int    // in-flight sends across the Service; 0 means 32
    Logger      *slog.Logger
}

type Subscription struct{ Endpoint, P256dh, Auth string }

type Stored struct {
    ID, Subject, VAPIDKeyID string
    Revision                int64
    Subscription
}

type Options struct {
    TTL     time.Duration // whole seconds, >= 0; 0 means the push service's default
    Urgency string        // "very-low" | "low" | "normal" | "high"; "" means normal
    Topic   string        // RFC 8030 topic, <= 32 URL-safe chars; "" means none
}

type Result struct {
    ID         string
    Status     int           // push-service status; 0 when Err is transport-level
    RetryAfter time.Duration // from a 429/503, else 0
    Err        error
}

var (
    Schema               *migrate.Set
    ErrEmptyPrivateKey   error
    ErrInvalidPrivateKey error
)

func GenerateKey() (string, error)
func New(cfg Config) (*Service, error)

func (s *Service) List(ctx context.Context, subject string) ([]Stored, error)
func (s *Service) DeleteSubject(ctx context.Context, subject string) error
func (s *Service) Sweep(ctx context.Context, notConfirmedSince time.Time) error

func (s *Service) Send(ctx context.Context, to []Stored, payload []byte, o Options) ([]Result, error)
func (s *Service) SendTo(ctx context.Context, subject string, payload []byte, o Options) ([]Result, error)

func (s *Service) PublicKey(w http.ResponseWriter, r *http.Request)
func (s *Service) Subscribe(w http.ResponseWriter, r *http.Request)
func (s *Service) Unsubscribe(w http.ResponseWriter, r *http.Request)

func JS() []byte
func WorkerJS() []byte
```

`SendTo` is the common case — every device one subject enrolled — so
birthday-alarm never touches `Stored`. `Send` is for apps that select
devices. Both return a batch `error` for what stops the batch (selection
query failed, payload over bound, invalid options, context cancelled) and
a `Result` per attempted device; a database failure must not look like
"zero devices". A `Result` reports the push service's *acceptance*, not
delivery, and aviso never retries: `RetryAfter` is for the app's own
scheduler. Rows whose `VAPIDKeyID` is not the Service's are skipped with
a Result error, not sent with a key that cannot sign for them.

Concurrency is bounded across the Service, not per call, and the
semaphore wait honours `ctx`. Each request has a 30 s timeout.

## 4. HTTP routes and their gating

Handlers, not a router: the app mounts them where it likes (the SKILL.md
suggests `/aviso/…`). Each enforces its method.

| Route | Body | Reply |
|---|---|---|
| `GET  …/public-key` | — | `{"publicKey": "<base64url>"}`; `Cache-Control: no-cache` |
| `POST …/subscribe` | `{"subscription": {endpoint, keys:{p256dh, auth}}, "publicKey": "…", "previousEndpoint"?: "…"}` | 204 |
| `POST …/unsubscribe` | `{"endpoint": "…"}` | 204, idempotent |

The two mutations require the app's session middleware to have run, a
non-empty `sessions.Current(r).Subject`, and `csrf.SameOrigin(r,
cfg.Origin)`: 401 without a session, 403 on origin failure. `publicKey`
must equal the Service's, else 409 — a browser subscribed under a rotated
key must re-enrol, not be stored unsendable. An endpoint already owned by
another subject is 409. `previousEndpoint` is deleted in the same
transaction, and only if the same subject owns it; the subscribe body is
capped at 8 KiB and the endpoint at 2048 bytes (Eleven's bound).
Unsubscribe deletes only the caller's own row and answers 204 either way.

A re-subscribe of an endpoint the same subject already owns replaces the
keys, bumps `revision`, and sets `last_confirmed_at`.

## 5. Schema

`migrations/0001_init.sql`, immutable once released:

```sql
CREATE TABLE aviso_subscriptions (
    id                TEXT    NOT NULL PRIMARY KEY,
    endpoint          TEXT    NOT NULL UNIQUE,
    subject           TEXT    NOT NULL CHECK (length(subject) > 0),
    p256dh            TEXT    NOT NULL,
    auth              TEXT    NOT NULL,
    vapid_key_id      TEXT    NOT NULL,
    revision          INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at        INTEGER NOT NULL,
    last_confirmed_at INTEGER NOT NULL
);
CREATE INDEX aviso_subscriptions_subject   ON aviso_subscriptions(subject);
CREATE INDEX aviso_subscriptions_confirmed ON aviso_subscriptions(last_confirmed_at);
```

Times are UTC Unix seconds. `id` is 16 random bytes, base64url, never
reused. `vapid_key_id` is base64url SHA-256 of the uncompressed public
key, so a rotated key is visible per row. Pruning and the post-send
confirmation bump both match `id` **and** the `revision` captured when the
batch was selected.

The app merges: `BootSchema = migrate.Merge(sessions.Schema, aviso.Schema, Schema)`.

## 6. Browser module (`js/push.mjs`)

```js
export function capabilities()                       // {serviceWorker, push, notifications, standalone}
export async function status(registration)           // {permission, subscription|null}
export async function enable({registration, publicKey, save})     // from a click
export async function reconcile({registration, publicKey, save})  // on every load
export async function disable({registration, remove})
```

`enable` calls `Notification.requestPermission()` **first**, synchronously
inside the gesture, then awaits `registration.pushManager.subscribe`.
`reconcile` never prompts: with permission already granted it compares
the existing subscription's `applicationServerKey` to `publicKey`,
re-subscribes if they differ (Eleven's self-heal, `push.js`
`sameAppServerKey`), and calls `save` so the server's `last_confirmed_at`
moves. `save(body)` is app-supplied — a same-origin `fetch` to the
subscribe route — and must reject on non-2xx; Eleven's version ignores
the response (`push.js:177`), which is how a failed save goes unnoticed
until the first missed notification. `disable` removes the server row
first, then unsubscribes the browser, so a crash between the two leaves a
harmless orphan rather than a row that sends to nothing. Base64url
conversion stays private. Worker registration is the app's.

## 7. Worker helper (`js/aviso-sw.js`) and the payload contract

A classic script (workers cannot `import` reliably everywhere), loaded by
the app's own `sw.js` via `importScripts`, exposing:

```js
AvisoSW.handlePush(event, {decode, fallback})   // -> Promise
AvisoSW.handleClick(event, {fallbackURL})       // -> Promise
AvisoSW.handleSubscriptionChange(event, {renew, save})
```

The app attaches each to `event.waitUntil` in its own listener, so the
helper never owns the worker's lifecycle.

**Payload contract.** The default decoder accepts JSON
`{"title","body","url","tag"}`; `title` is required, `url` must be a
root-relative path on the app's origin (§10). It yields
`{title, options}` with the validated URL in `options.data.url`. An app
with its own shape (Eleven's encrypted blob, say) supplies `decode`,
which may be async. `fallback` is **required** and produces the
notification shown when the payload is missing or malformed — because
every push shows a notification (§1), and the alternative is WebKit
revoking the subscription.

`handleClick` closes the notification, focuses a client whose URL matches
exactly, else opens the validated destination, else `fallbackURL`.

`handleSubscriptionChange` uses `event.newSubscription` or `renew(event)`
(a `pushManager.subscribe` with the stored key), then `save` posts to the
subscribe route with `mode: "same-origin"`, `credentials: "same-origin"`,
`redirect: "error"`. Fetch's default credentials mode is already
`same-origin`, so eligible cookies — HttpOnly included — go with the
request; what nothing can guarantee is that an installed app still has a
live session. When it does not, renewal fails silently, without
prompting, and `reconcile` repairs it on the next page open after
sign-in. The spec says this out loud so nobody builds a retry loop in a
worker.

## 8. VAPID custody

`Config.PrivateKey` is the unpadded base64url encoding of a 32-byte P-256
scalar (webpush-go's own format). `New` checks it is in range and derives
the public key; empty is `ErrEmptyPrivateKey`, malformed
`ErrInvalidPrivateKey`, and both stop boot.

Provisioning is explicit and once:

```sh
go run amadan.net/rastrillo/aviso/cmd/aviso-key   # prints the private key, nothing else
```

The app reads it the way birthday-alarm reads its instance key
(`BIRTHDAYALARM_INSTANCE_KEY`, refused when empty): one env var,
`<APP>_VAPID_PRIVATE_KEY`. Not a file (Eleven's `<db>.vapid` sidecar has
no equivalent in a `STATE_DIRECTORY` that holds only the DB), not a table
row (Eleven scrubbed exactly that, `push.go:208`, because DB snapshots
travel), and not derived from `InstanceKey` (rotating one secret must not
silently rotate another). Rotation means every browser re-enrols on its
next `reconcile`; rows under the old key are skipped, never sent.

## 9. Retention and revocation

`Sweep(ctx, t)` deletes rows with `last_confirmed_at` before `t`. That
time advances on `reconcile` (a page opened) and on a revision-matched
2xx acceptance. It measures whether the *subscription* is alive, not
whether a person still wants notifications: an accepted send keeps an
unopened device forever, which is the push service's contract too. The
SKILL.md recommends 90 days, run from a `carlos.Tick` handler.

Session expiry does **not** revoke a subscription — the subject still
owns the device. Apps call `disable` before sign-out or account switch,
`DeleteSubject` on account deletion, and re-check that a recipient is
still entitled before every `SendTo`. An accepted notification cannot be
recalled.

## 10. Security rulings

- **SSRF.** Endpoints are https only, no userinfo, no fragment, ≤ 2048
  bytes; the client follows no redirects, uses no environment proxy, and
  its dialer rejects loopback, private, link-local and reserved ranges
  **at connect time** (IPv4-mapped IPv6 included), so DNS rebinding after
  validation still fails. This is Eleven's `guardDialControl`
  (`push.go:25-45`, `unfurl.go:160`) and its `push_ssrf_test.go`, lifted.
- **Bounds.** Plaintext ≤ 3993 bytes (RFC 8291 §4's one-record limit);
  push-service response bodies read to a small cap.
- **Click URLs.** Root-relative paths only, resolved against the origin
  and required to stay under it; protocol-relative (`//`) and
  backslash-bearing paths are rejected. Applied to the default decoder,
  to custom decoder output, and to `fallbackURL`.
- **Redaction.** Endpoints, `p256dh`, `auth`, payloads, `Authorization`
  headers, push-service response bodies and URL-bearing transport errors
  never reach logs; an endpoint is logged as its row `id`.
- **Ownership.** Nothing in a request body names a subject; the session
  does.

## 11. Installability recipe (docs, not code)

What an app does so that a phone can install it and iOS will deliver
push (16.4+, Home Screen only):

1. Serve `manifest.webmanifest` with a stable `id`, `name`, `short_name`,
   `start_url`, `scope`, `display: standalone`, `theme_color`,
   `background_color`, and 192 px and 512 px icons.
2. In the layout `<head>`: `<link rel="manifest">`,
   `<meta name="theme-color">`, a 180 px `apple-touch-icon`.
3. Serve the worker at the scope it should control, `Cache-Control:
   no-cache`, as Eleven's `serveSW` does (`main.go:4168`).
4. Tell the person, in the app, to add it to the Home Screen and sign in
   inside the installed copy before pressing the enable button
   (`capabilities().standalone` decides whether to show that coaching).

Eleven's `serveManifest` (`main.go:4181`) is the twenty-line model.
Rastrillo's scaffold is not changed by this spec.

## 12. Testing

**Go.** A recording transport behind a private seam (production guards
stay on): asserts `TTL`, `Urgency`, `Topic` and a VAPID `Authorization`
header per send; 404/410 prunes only when the revision still matches;
429/503 surface `RetryAfter`; cancellation stops the batch. Handlers:
401/403/409 paths, cross-owner upsert refused, `previousEndpoint` only
deleted when owned, body and endpoint bounds. SSRF: loopback and private
endpoints refused at dial, redirect refused, rebinding case. Keys:
`GenerateKey` round-trips through `New`, invalid scalars refused, the
same key yields the same `VAPIDKeyID` after restart. Eleven's
`push_ssrf_test.go` and `push_token_hash_test.go` are the seed corpus.

**Node.** `js/push.test.mjs` against a fake `registration`/`PushManager`:
permission denied, reconcile with matching and mismatched keys, failed
`save` rejects, `disable` ordering. `js/aviso-sw.test.mjs` against a fake
`self`/`clients`: default decoder, malformed payload falls back, click
focuses vs opens, subscription change renews then saves, expired session
fails silently.

**Browser.** One chromedp test (the `webauthn/browser_test.go` precedent)
that registers a worker, calls `enable` with a **declared subscription
double** — a stubbed `pushManager` — and asserts the row lands through the
real handlers. It proves the wiring, not Chromium's subscription service
or encrypted delivery; nothing short of a real push service does, and
CI must not depend on one. iOS is a manual smoke test, written down.

**Gates.** The repo's own gate (`go vet`, `gofmt -l`, `go test`), the
node tests, and the browser test, mirrored in `Makefile` `ci` and
`.amadan/ci.d/`. The example app under `example/` is its own module and
is tested from its own directory, as `AGENTS.md` requires.

## 13. Out of scope, named

Offline caching and app-shell workers; an installability package or
scaffold change; Badging API and `beforeinstallprompt` UI; Eleven's
native APNs relay (`docs/push-relay.md`); declarative push; payload
encryption above RFC 8291 (Eleven's E2EE blob stays Eleven's `decode`);
recipient policy, mute/block, presence ("skip active users"); durable
queues, delivery receipts, automatic retries; replacing webpush-go.

## 14. The two points argued, and who won

- **Send returns `([]Result, error)`, not `[]Result`.** Claude proposed
  results-only for `SendTo`; Codex objected that a selection failure
  would then read as "no devices". Codex's version stands (§3).
- **The browser test uses a subscription double, not a fake push
  service.** Claude proposed a chromedp test round-tripping through a
  local fake push endpoint; Codex objected that Chromium's own
  subscription path cannot be pointed at it, so the test would prove
  less than it claimed. Codex's version stands (§12).

No open questions remain that the code cannot settle. The one decision
that is Paul's: whether `aviso` is the name.
