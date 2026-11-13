# Blind auth store

Implementation contract for issue #2 and the agents integrating Contra
(carlos/contra, signin-then-unlock Slice 0) and Oficina Home (oficina/home).
Paul approved this work on 2026-11-13. This branch is not a release.

## Boundary and API

Opt in with `auth.Config.BlindLinks: &auth.BlindLinks{...}`. Nil preserves
the existing SQL and records flows. `BlindLinks` is separate from the old
`Config.Blind`, whose records-mode links contain a decryptable address.

The consumer implements `BlindLinkStore`:

```go
Put(context.Context, auth.BlindLink) error
Take(context.Context, string) (auth.BlindLink, bool, error)
Spend(context.Context, string) (int, error)
Sweep(context.Context, time.Time) error
```

`BlindLink` contains `Hash`, `LookupKey`, `Purpose`, and `Expires` only.
`Put` must reject duplicate hashes. `Take` must atomically consume by hash,
with at most one successful taker across goroutines, processes, replicas
and restarts. SQL adapters can use `DELETE ... RETURNING`; an object store
needs a globally consistent conditional consume operation. A local mutex,
SELECT followed by DELETE, or an eventually consistent used marker does
not satisfy this contract. `Spend` removes every outstanding link for the
lookup key; `Sweep` removes expired records. All methods are concurrent.

The consumer also supplies:

```go
LookupKey func(context.Context, string) (string, error)
ResolveSubject func(context.Context, string) (string, error)
```

LookupKey receives the lowercase, trimmed email address before any write.
It returns the consumer's existing peppered HMAC index. ResolveSubject runs
only after consumption and validation, receives that index, enforces
admission, and returns the opaque account/person subject. Provisioning, if
needed, belongs there and must be idempotent across separate valid links.
Return sessions.ErrNoUser to refuse admission with 403 or Config.Refused;
Refused receives the verified lookup key in Identity.Address. Any wrapped
error text is discarded. Other resolver errors remain operational failures.
Errors never fall back to the email or lookup key as a session subject.
The resolved subject reaches SecondFactor before session minting.

The adapter checks the returned record's hash, purpose, expiry strictly
after now, and lookup key. Invalid presented records are already spent.
Encode HMAC indexes as hex or base64url, not raw binary strings. Keys and
subjects must be nonempty, unchanged by trimming, and contain no `@`. These guards catch common accidental address returns; they cannot
prove opacity. Consumers must never return encoded or encrypted addresses,
or an unkeyed address digest susceptible to dictionary enumeration.

## Persistence and delivery

The existing sessions store already persists a token hash, opaque subject,
method and authentication/creation/expiry timestamps. It needs no new
interface or migration. A blind consumer applies `sessions.Schema` plus
its own link-store schema and passes `Config.DB`; it need not install
`auth.Schema`. There are no writes to auth_links or auth_sessions.

The upstream signin.Flow continues to own token generation, hashing,
expiry assignment, rate limits, emailed links and redemption. Its private
adapter returns the opaque lookup key through signin.Identity.Address;
auth resolves that key before constructing a session. Public SessionFrom,
From and sessions.Current expose the resolved subject. No ceremony is
forked and the dependency is unchanged.

Email is available transiently to request parsing, normalization, LookupKey
and Mailer.Send. The in-memory limiter receives HMAC keys under a separately
derived instance key. Blind mode disables the Keymail classifier and
client, and refuses pending Keymail callbacks, including cookies issued
before switching modes. No classifier address cache or Keymail pending
address blob is created. SigninScreen is rejected because its sealed
attempt/remember cookies retain addresses. Nonempty KeymailServers,
Authorize, SubjectFor, Records, legacy Blind/Pepper/Signed are rejected.
Admission belongs in ResolveSubject. Proof and CSRF behavior are unchanged.

A real mail sender is required: nil, mail.Logged and upstream
signin.LogMailer fail at construction.
The consumer must ensure its sender does not keep a database queue,
request-body logs, recipient logs or a server-decryptable address payload.
The framework cannot police custom hooks, middleware, log handlers, SMTP
servers or backups that the consumer controls. Ordinary HTTP access logs
must not record submitted bodies or credentials. Go does not promise
immediate zeroization of garbage-collected input strings.

## Log audit

Blind mode wraps the configured logger before wiring auth and sessions.
It retains constant event messages and drops structured attributes,
including attributes added through With and WithGroup. A logger passed
already carrying an address, or a handler reading addresses from context,
is a consumer violation and cannot be stripped by this wrapper.

Audited call sites:

- auth.New: mail fallback is rejected before logging or sending.
- auth.handlers: begin, callback, subject, second-factor and session errors;
  path-routing diagnostics; proof refusal reason/also; seal failures;
  the invalid-authorize-URL event. Dynamic attributes are dropped.
- auth.attempt, auth.confirm: random/seal/render errors use the same logger.
- auth.signin: the advisory is constant; screen mode is rejected anyway.
- sessions: adopt, lookup, signout, path-routing and refresh diagnostics
  share the wrapped logger. Signed refresh is not enabled in blind mode.
- auth.recordlinks: unreachable in this mode; Records is rejected.
- signin.LogMailer: logs recipients and live links through log.Printf or
  Logf; rejected by type before the flow is wired.
- mail.SMTP: returns recipient-bearing errors but does not log them.
  Begin logs only a sanitized event. SendLink returns a generic failure
  except the safe, existing bad-address and rate-limit sentinels.

Consumer store and resolver errors are replaced before returning. Spend
and Sweep also sanitize failures. No unwrapping exposes the original error.
Custom RenderConfirm, Refused, SecondFactor, Mailer and application logging
remain the consumer's responsibility; SecondFactor receives no address.

## Compatibility and adoption

No default behavior changes and no shipped migration changes. Existing
email-subject apps retain their session identity and Keymail behavior.
Existing SubjectFor and records-mode tests remain regression coverage.
Only the opt-in consumer schema must be created. Paul owns releases and
tags; this change belongs under Unreleased until he releases it.

A previously plaintext app must stop old writers, invalidate outstanding
links, and migrate existing subjects and every subject-keyed relationship
before enabling blind mode. Existing sessions should be revoked if their
subject mapping cannot be safely migrated. Auth's session lookup and guards
refuse pre-switch address subjects without deleting or rewriting those rows;
that prevents a blind handler from treating an address as an opaque person.
Direct users of the shared sessions package must apply the same migration. Switching back does not make
blind links redeemable through the old SQL store. Re-enabling old links
or retaining historical plaintext backups defeats the new guarantee.

secure_delete remains enabled by Home but cannot remove historical WAL
frames or replicated objects. This option prevents new address writes;
it cannot retroactively clean a database, WAL, snapshot or backup.

## Consumer integration

Contra supplies its Slice 0 HMAC email index in LookupKey, its token-hash
link table through BlindLinkStore, and an index-to-account-id resolver.
Send through its transient delivery sender. Keep unlock independent of
sign-in, and leave SubjectFor/Authorize unset. Continue using Auth.Begin,
Auth.Verify, SessionFrom/RequireSession and Signout. Auth.SendLink is
available for an already trusted caller; it intentionally has no HTTP
proof or CSRF check. It shares Begin's rate limits.

Home supplies its home-held peppered email index in LookupKey and resolves
it to an opaque person ref. Keep secure_delete enabled, omit the stock
auth schema for a new installation, and use the same framework handlers.
When a password or passkey flow has only an index, call the consumer store's
Spend(ctx, index) directly; Auth.SpendLinks takes a transient address.
It must choose the blind email-only flow for this guarantee; retaining
Keymail's address-bearing pending ceremony would need separate design.

## Evidence

The baseline is f908c5f. Declaration-only API stubs allow the new tests to
compile there; the before run must contain named assertion failures, not
compiler errors. The leakage test scans the live database, WAL, SHM and
captured logs before consumption, after a fresh Auth instance redeems,
and after a checkpoint. It checks the canary's raw, trimmed, lowercase
and lowercase-trimmed forms, percent/base64/hex encodings and local-part variants. The injected
logger, slog.Default and the standard logger are captured, and HTTP headers,
cookies, response bodies and mailed link bodies are checked too. Scanning
cannot recognize arbitrary encryption; exact record values and the empty
legacy tables supplement the byte checks. Further checks inspect serialized link
objects, stored hashes, resolver failures, second-factor subjects, wrong
purpose, expiry including the exact boundary, reuse, concurrent takers,
maintenance, unsafe configuration and malicious store results.

Guard mutation runs and both test outputs are posted on the branch with
the final CI result. The test SQL adapter demonstrates the required
atomic consume contract; each production adapter needs the same tests
against its own backend, including cross-process races where applicable.
