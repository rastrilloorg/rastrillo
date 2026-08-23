# `store = "sealed"` — the blind-server resource shape

**Issue:** rastrilloorg/rastrillo#77 · **Date:** 2026-08-23 ·
**Status:** DRAFT — joint rastrillo+platform design; needs the platform side
of the conversation before implementation. Depends on #79 (keyring) for the
grant envelope.

## 0. The gap

Rastrillo's headline productivity — manifests, `ui`, server-rendered screens,
`scope` — assumes the server can read the data. A full-E2EE app can't use any
of it for content: the server stores opaque envelopes and the client holds
the fold. Kass hand-wrote the whole server half in ~200 lines plus stores —
sealed blobs, a sealed append-only log, coaching grants, ~20 routes — all
mechanical once the shape is named. This is the E2EE equivalent of what
manifests did for CRUD.

## 1. The candidate design move, stated honestly

**The sealed log could ride the `eventlog` table — with a mandatory encoding
convention, not for free.** What genuinely holds: eventlog's merge order
`(lamport, ts, writer, seq)` never reads `payload`, and `Ingest` is already
documented as "the platform transport's seam," already idempotent, already
refusing divergence (`ErrDiverged`). What does not hold: `payload` is
`json.RawMessage` over a `TEXT` column, and `Append` **rejects** invalid JSON
by design (the `json.Valid` check exists so a bad byte can't poison every
future read) — raw ciphertext cannot pass. Riding eventlog therefore means a
pinned envelope convention (base64 ciphertext inside a JSON value), ~33%
wire and storage overhead, and closing a real hole: `Ingest` does *not*
validate payloads today and would store poison bytes. Issue #77 called
eventlog "structurally wrong for E2EE" because the server folds plaintext;
the transport-and-ordering half is structurally right — but only the half.

The alternative is kass's actual shape: a per-owner log table
`(seq AUTOINCREMENT, sealed BLOB, at, created_at)` — no lamport, no writer,
server-serialized, clients syncing incrementally by global `seq`. Blunter,
proven, no encoding tax, and **kass has no multi-writer log at all** (the
coach only reads). Which shape the generator emits is not rastrillo's call
alone — it is the client-write-path question in §5, and the answer decides
whether eventlog's multi-writer machinery is load-bearing here or cosplay.

## 2. Proposed manifest vocabulary

A sealed resource can't declare fields (the server never sees them). Two
sealed kinds, mirroring what kass actually needed:

```toml
# manifest/journal.toml
name = "journal"
store = "sealed"
kind = "log"          # append-only ciphertext stream per owner

# manifest/profile.toml
name = "profile"
store = "sealed"
kind = "blob"         # versioned named singleton per owner
```

This is surgery on the manifest system, not a clean fit, and the plan must
budget for it: `Validate` today *requires* a route and at least one list
column or form field — both rules invert for sealed resources (no screens,
API-path routes derived from `name`); `kind` is a new `Resource` field under
strict TOML decoding; and every binary `Store != Mergeable` branch in the
generator (store emission, sqlc gating) grows a third arm. `scope` is
implicitly per-owner for sealed resources; `list`/`form` are rejected.

## 3. Generated server half (transport and ordering, nothing else)

Per sealed resource, generated routes mounted behind the session middleware:

- **blob**: `GET /api/<name>` → `{sealed, version}` (sealed base64 in JSON,
  kass's own convention); `PUT /api/<name>` with expected version → 409 on
  mismatch (kass's `ErrStale`: "this changed on another device"), version =
  current+1 in one tx. Closed namespace: only declared blob resources exist.
- **log**: `GET /api/<name>` (with a since-cursor) and `POST /api/<name>`
  (batched append, one tx, capped per request — kass's 500). The backing
  store and cursor semantics follow the §5 write-path answer. One shape kass
  carries that must be decided consciously: a client-chosen cleartext event
  time (`at`) — deliberate metadata-in-the-clear, indexed for range reads.

Generated once per app when any sealed resource exists, **conditional on the
§5.3 topology answer**: a membership/grant surface. Two honest options:

- *Single-instance* (rastrillo's team doctrine): one `members` table (ref,
  public key, wrapped grant, created/claimed/revoked) inside the instance.
  Simple, but it is a re-architecture, not kass generalised.
- *Kass's actual split*: memberships and public keys in a shared registry,
  wrapped grants in the granting owner's instance, revocation deliberately
  two-halved ("both matter and neither is enough") — because coach and
  client live in **different instances**. If the family's E2EE apps keep
  that topology, the registry half is platform/app territory and the
  generator emits only the instance half.

Either way, the access discipline generalises from kass: **one**
membership-check helper is the only entry point into another owner's data;
claimed-and-not-revoked required; membership misses answer 404 with
"doesn't exist" and "not yours" identical (anti-enumeration). One kass
subtlety kept deliberately: a *legitimate* member with no grant row gets a
403 ("hasn't shared with you") — membership and key possession are different
facts, and collapsing them loses a real UX distinction.

- **invites**: mint (membership row with invite hash, member null), view,
  claim (transactional claimed-at-NULL check — single-use without deleting
  the row, which the listings need). The fragment-secret client half is #82;
  `crypto.DeriveInvite` is the primitive.

Generated code follows eject-by-existence like every manifest emission, but
with the issue's caveat made a rule: **wire formats here are forever** — the
generated routes' request/response shapes and the stored envelope layouts are
byte-pinned by the app's own vectors (#78 machinery) from day one.

## 4. What stays app-side / client-side

The fold (#78 keeps its two engines honest), all rendering, key lifecycle
(#79), the offline outbox and sealed-store client (#82), anonymisation and
sidecar membership (#81). The server half generated here has no opinion
about payload contents beyond "bytes."

## 5. Questions for the platform side (the reason this is a draft)

1. **The client write path** — the biggest one, and prior to transport: which
   primitive backs the generated POST? Server-assigned ordering (kass's
   model: every device of one owner collapses into one writer, global seq is
   the sync cursor) or client-as-writer (`Ingest` semantics: clients supply
   `(writer, seq, lamport)`, which is a sync protocol — writer-identity
   auth, can device A claim device B's writer, and what replaces the global
   cursor once ingested history can interleave below it)?
2. **Transport**: given that answer, is `Ingest` as-is sufficient as the seam
   CARLOS calls for edge sync of sealed streams — auth, backpressure, size
   caps (which the base64 envelope inflates by a third)?
3. **Cleartext metadata policy**: what may sit beside the ciphertext in the
   clear — client-chosen event time, kind labels, stream names? Kass chose
   `at` in the clear, deliberately and documented; the generator needs the
   family's rule, not kass's exception.
4. **Instance topology**: instance-per-team doctrine vs kass's
   instance-per-person + registry split. Cross-instance grants (coach ↔
   client) need the platform's routing or a registry the platform blesses —
   §3's membership surface cannot be finalised before this answer.
5. **Blobs**: sealed blobs are versioned, not CRDTs. Does the platform want
   a sync story for them, or do blobs stay strictly client-fetched with
   platform involvement limited to backup?
6. **Backup/restore**: `Ingest` is "directly usable for backup replay" — is
   the platform's backup of a sealed instance file-level or event-level?

## 6. Sequencing

Blocked on: the platform conversation (§5) and #79 (grant envelope). The
manifest-side generator work (§2–§3) can be planned once §5.1 and §5.4 have
answers; nothing here starts in wave 1.
