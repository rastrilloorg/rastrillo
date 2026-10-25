# 🤖 provision Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A rastrillo package, `provision`, that lets a central app (a home or a hub) create a tenant instance through the CARLOS console, tell when it is really serving, publish a hub's whole path map, and prove the product path reaches the tenant rather than falling through to the hub.

**Architecture:** One `Console` value per service credential. This client deliberately uses app-bound credentials (the platform also mints account-wide ones, services.go:157-166, which reach every app): the tenant app's `instance` credential creates and reads instances, and the hub app's own credential publishes paths. `Create` turns the create door's answers into five outcomes (created, landed, pick a new label, refused for good, outcome unknown so try again) and reconciles with a `GET` before it ever treats a retried host as someone else's. `Ready` and `CheckRoute` are single probes of `/api/version`, so a stateless centre advances one step per page refresh, with blocking `Wait*` loops for scripts and tests. A tenant that opts in names itself on `/api/version` with `Rastrillo-Host: $CARLOS_HOST`, which is the proof of which instance answered. `provision/provisiontest` fakes the console and the edge with the platform's real answers, so Plans 6 and 7 and ppl test against it.

**Tech Stack:** Go stdlib only (`net/http`, `encoding/json`, `crypto/rand`, `encoding/base32`). No new dependencies.

**Spec:** pegamento `docs/superpowers/specs/2026-09-27-user-models-design.md` (branch `design-user-models`, `db90b492`) §5, §7.1, §7.2, §7.4, §7.5, §8, §9, §11. Roadmap: same branch, `docs/superpowers/plans/2026-09-28-user-models-roadmap.md`, Plan 4. First consumer: ppl `docs/superpowers/specs/2026-10-17-ppl-design.md` §4.2, and rastrillo discussion #75.

**Platform reference:** carlos/platform `origin/main` at `04819f9b`. Its published reference is `docs/site/cli.md`, which documents CLI verbs and not the HTTP API (see "Platform gaps"). Every answer below is therefore cited from the handlers that give it. Paths are relative to the platform repo unless they start with a rastrillo package.

## Global Constraints

- Module `amadan.net/rastrillo/rastrillo`; `go 1.25.0` in go.mod; `CGO_ENABLED=0`; `GOFLAGS=-mod=mod` (the Makefile sets both).
- No new dependencies. `provision` imports only the stdlib and `amadan.net/rastrillo/rastrillo/carlos`. It must not import the root package, so a hub's tests can use it without SQLite.
- The gate is `make ci`, green on every commit, not only the last. The new packages are tested by the root `go test ./...`, so `SUITE` and `.amadan/ci.d/50-suite@33` do not change.
- Every exported symbol and exported method of `provision` and `provision/provisiontest` appears on its reference page, and both are mapped in `referencePages` (`internal/docsite/symbols_test.go`, which scans methods too, lines 253-264). Tasks 1-5 document what they export as an API listing only (names and signatures in code blocks, no prose) and give a new page a placeholder nav blurb, `Draft, replaced after copy review`, which nav_test accepts as non-empty. Every commit passes the docs test; the prose lands in Task 6 after Task 0.
- `SKILL.md` is at 31,615 of `skillBudget` 31,630 bytes. Trim genuinely redundant prose to fit the new sentence; if there is none, raise `skillBudget` and say why in `skillmd_test.go`.
- Comments say why, naming the failure prevented. Commits: imperative subject, body gives the why.
- `provision` ships no user-facing copy. Its errors are for logs and records; the app maps them to sentences that go through its own copy review. The prose this plan adds (reference pages, nav blurbs, the SKILL.md sentence, the CHANGELOG entries) goes through the `copy-review` skill in Task 0, which runs after Task 5 and before Task 6 (run the tasks in the order 1, 2, 3, 4, 5, 0, 6, 7). Tasks 1-5 write none of that prose, so nothing unreviewed is written to a file except the placeholder blurbs, and Task 6 replaces those. No em dashes in that prose.
- Before and after (CONTRIBUTING.md §3): each task's Step 2 adds stubs with the Interfaces' signatures that return zero values, so the new tests fail with `--- FAIL: <name>` and a message, not a build error. Run the new tests against `bfc8ade7` plus those stubs in a separate worktree, and post the commit, the command and both outputs in a discussion on the branch. A test that passes on its stub proves nothing before the change (Task 1's `TestVersionNamesNothingByDefault` and `TestVersionOffCarlosNamesNothing`, whose stub `HostHeader` is `""`, and `Get("")` is always `""`); name each such test in the discussion as vacuous before the change. Task 6, which has no code of its own, is run against the Step 2 stubs of Tasks 3-5 for the same reason.
- Use the platform's API as its handlers give it; never work around it. Where it lacks something, the plan says so and proposes a carlos/platform request ("Platform gaps").
- No delete call. Deleting an instance stays a person's `carlos instances delete` (spec §7.5).
- CONTRIBUTING.md: name the model and reasoning setting in the branch description; a cross-provider review of the branch before merging.
- Land with `amadan branch merge -squash` (AGENTS.md), then `make mirror`.
- The live check (Task 7) runs only after Paul's scoped approval.

## Review Focus

1. **A create whose outcome is unknown actually landed.** A timeout, a 5xx, a reset connection or a 202 leaves the instance possibly created. The record must keep that host, and the next call must reconcile with `GET` before it posts again, or the person gets a second instance and the first is an orphan nobody owns. Pinned in Task 3 (`TestCreateRetryAfterTimeoutLands`, `TestCreateResumeReconcilesFirst`, `TestCreateTransportErrorIsUnknown`) and Task 6 (`TestHomeSequenceResumesAfterACrash`).
2. **A 409 that is not a collision.** "instances are not enabled" is a 409 with no `Retry-After` (apiinstances.go:130-133); "another operation is in progress" is a 409 with `Retry-After: 2` (claim_response.go:23-33). Neither may lead to a new label: the first is refused for good, the second is try-later. Only a 409 whose host `GET` finds in this app is a collision. Pinned in Task 3 (`TestCreateNotEnabledIsRefused`, `TestCreateBusyIsRetryLater`).
3. **Something other than the tenant answers.** A redirect from the tenant's direct host, the edge's "pending" 503 for a host it has not heard of, a wildcard catch-all's 200, or the hub answering `/<slug>/api/version` through its locale prefix must not read as ready or routed. Redirects are never followed, and only a 200 whose `Rastrillo-Host` names the expected host counts. Pinned in Task 5.
4. **Hub and tenant carry the same release identifier.** Release versions are chosen per app and can coincide (a release's version defaults to the git short sha, cli.go:2161, 2171-2174, so a scaffold that ships both binaries from one commit gives both the same one), so `X-Carlos-Version` alone cannot tell them apart. A fall-through must then read as pending, never as routed. Pinned in Task 5 (`TestCheckRouteSameVersionStillNeedsTheHostHeader`).
5. **A path-map listing that is empty, partial or failed.** `PUT …/paths` is wholesale (apidomains.go:762-770), so publishing an empty map clears every tenant's route, and publishing the first page of a listing drops the rest. A build error, an empty map, or a listing that stopped early must send nothing. Pinned in Task 4 (`TestPublishRefusesAnEmptyMap`, `TestPublishSendsNothingWhenTheBuildFails`) and Task 6 (`TestHubBuildFailsOnALaterPage`).

## Decisions

### Who writes the records

`provision` does not write records; it returns typed outcomes, and the app writes them. Four reasons. The keys are not one shape: the home and the hub differ (spec §5), and the first consumer uses `handles/<handle>.json` (ppl §4.2), so a package that owned keys would be wrong for its first user. A stateless centre advances the sequence one probe per refresh of a zero-JS "getting your space ready" page (spec §7.2 step 4), with the record as its only memory, so the state machine lives in the app's handlers, not in one call `provision` could own. The uid and slug are minted by Plan 2 and the app, and the record's other fields (display name, owner uid) are theirs. And keeping `provision` free of `objrec` lets a home that stores elsewhere use it. The cost is that every caller must keep the record contract below. Task 6 pins it on `objrec.Dir` in tests Plans 6 and 7 copy.

**The record contract** (what Task 6's tests hold the app to):

- **Order.** Write the record `creating`, with its host, before the first `Create` (spec §7.2 step 2). Keep that host until `Create` returns `ErrHostTaken`.
- **One first call.** Only the signup request that wrote the record calls `Create` without `Resume`, and only it relabels. Every later call (a refresh, the retry button, a repair) passes `Resume: true`. The host is then the operation's identity: two refreshes that both post it get one 201 and one 409 that a `GET` turns into `Landed`, never `ErrHostTaken`.
- **Every transition is a compare-and-swap.** Read, decide, `UpdateJSON` with the version read; on `objrec.ErrChanged` re-read and decide again. Two refreshes of one signup then advance it once.
- **Relabelling** (in the signup request only). A hub record is keyed by slug (`tenants/<slug>.json`), so a new label is an update of its host. A home record is keyed by label (`users/<uid>/instances/<label>.json`), so a new label is a new record created first and the old one deleted after; the old one never named an instance of ours, because `ErrHostTaken` means a `GET` found the host belonging to another creation.
- **Persisted counters.** The record carries `relabels`, and the app stops at `MaxRelabels`, because a refresh is a new request and a counter in memory resets.
- **Phases.** `creating` until `Ready`; then `live` (spec §7.2 step 5). A hub record also carries `routed`, false until `CheckRoute` says `Routed`; the hub redirects only to a routed tenant. A tenant that is still not routed at the app's deadline becomes `failed` with the error's text.
- **What a hub publishes.** Every record that is `live`, routed or not: the new tenant has to be in the map before `CheckRoute` can pass, and a `creating` tenant is left out until its own instance serves. The build reads every page of `tenants/` and fails if any page or record cannot be read.
- **Repair.** A daily `carlos schedule` tick republishes the map from the records (spec §4), which repairs a prefix lost to a concurrent publish (gap 2).

### What this plan assumes of Plan 5 (`base-path`)

1. A tenant served under `/<slug>` answers `GET /<slug>/api/version` from the framework's own `/api/version` handler (the one `buildHandler` registers in serve.go), with status 200 and that handler's headers. Task 1 adds `Rastrillo-Host` to that handler, so Plan 5 carries it with no work of its own.
2. On the tenant's direct host, `GET /api/version` and `GET /healthz` are not redirected to the product path, even in the default redirect mode Paul chose (#75; spec §7.3 has the direct host redirect everything else). `provision` never follows redirects: before step 6 publishes the map, the product path falls through to the hub, and a followed redirect would read the hub's answer as the tenant's. If Plan 5 redirects them, `Ready` stays false (`TestReadyIgnoresARedirect`).
3. Nothing else. `provisiontest.Tenant(slug)` stands in for a tenant after Plan 5 (`slug` set) and before it (`""`), so this plan is tested without Plan 5 landing. The fake cannot prove Plan 5's real handler meets 1 and 2; Plan 5 carries a test for each, and Plan 7's end-to-end tests run `provision` against the real handler.

### Why the instance names itself, and only when it opts in

Spec §7.2 step 6 says the prefix "must answer with the tenant app's version header". `X-Carlos-Version` is stamped by the edge from the route that served the request: the target's on a path hit (edge.go:1253-1254), the hostname's own on a fall-through (edge.go:1238-1240). It names a release, not an instance, and two apps' releases can carry the same identifier (Review Focus 4). Nothing else the platform sends identifies the instance that answered; `GET …/instances/{host}` reports `served_by` boxes (apiinstances.go:886-902), which is placement evidence, not proof of a request's path. So the proof is the tenant's own `Rastrillo-Host: $CARLOS_HOST` on a 200, and the version header is kept as the diagnosis: it tells `FellThrough` (§7.4, the hub answered) from `NoBasePath` (the tenant answered 404 at `/<slug>/api/version`).

The header is a disclosure. On a product path or a custom domain it tells any visitor the instance's platform hostname, which names the account's namespace and, for a hub, the handle-to-host mapping ppl lists as hub residue (ppl §4.3). So it is off by default: `rastrillo.Options.NameInstance` turns it on, and the reference page tells a hub-model product to list it in its published leak profile (spec §9). `Ready` and `CheckRoute` need it, so a tenant that `provision` creates must set it; a hub sets it too, so a hub answering the prefix is recognised by name.

### Platform facts this plan depends on

| Fact | Where |
|---|---|
| `POST /api/cli/apps/{account}/{app}/instances` and `GET …/instances/{host}` admit role `instance` (and `admin`, which every door admits; svcrole.go:94-126); a person passes the role gate | authcli.go:455, 696-697 |
| `PUT /api/cli/apps/{account}/{app}/domains/{host}/paths` admits `publish` or `instance` | authcli.go:826 |
| `Authorization: Bearer <credential>` | authcli.go:1146-1153 |
| A service credential's account segment matches as a sqid only; an app-bound one reaches exactly one app, and anything else is a text/plain 404 | apicli.go:36-40, 70-79 |
| 401 is one JSON body for every bad credential; 403 names the role a door needs | authcli.go:447-478, 1161-1163 |
| Error bodies are `{"error": "<sentence>"}` | authcli.go:1069-1071 |
| A JSON body needs `Content-Type: application/json` (415); create's body is at most 8 KiB, the paths body 8 MiB (413) | authcli.go:1080-1083, 1117-1140; apidomains.go:750-757 |
| 60 instance calls a minute per credential, then 429 with no `Retry-After` | authcli.go:101, apiinstances.go:95-99 |
| Create body: `host` (one DNS label plus the manifest's domain, else 400), optional `region`, `channel`, `environment`, `message` | apiinstances.go:1015-1067, 1089-1093 |
| `message` is valid UTF-8, at most 240 runes, no control characters (else 400) | cli.md:42; activity.go:27-44 |
| 409 "instances are not enabled", no `Retry-After`, before the host is read | apiinstances.go:122-139 |
| 409 for a host held as another app's apex, by another app, by a claim, or holding a deleted instance's data | apiinstances.go:1140-1150, 1170-1189; createleftovers.go:153-157 |
| 409 "instance <host> already exists" on a duplicate or a lost race; with host claims on, an existing claim can instead answer the busy 409 | apiinstances.go:1666-1672; createleftovers.go:43-50; claim_response.go:23-25 |
| 409 "another operation is in progress" carries `Retry-After: 2`; 503 "no box has room" carries `Retry-After` (60) | claim_response.go:16-33; nocapacity.go:36, 116-120 |
| 202 `{host, operation_id, phase}`, with no `Retry-After`, when the host-claim path accepted a create whose effects are not finished; `phase` is opaque (`creating` or a later phase) | apiinstances.go:1641-1644; claim_create.go:117-136; hostclaim/claim.go:23; only with `CARLOS_HOST_CLAIM` on, cmd/console/main.go:287 |
| 500 or 502 "try again" when a check or the write fails | apiinstances.go:1138, 1174, 1185, 1674-1675 |
| 201 with the record on success | apiinstances.go:1717-1723 |
| `GET …/instances/{host}` answers 200 with the record plus `served_by` and `gated`, or a text/plain 404 | apiinstances.go:859-902 |
| Paths: `{"paths": {...}}` required (400 without it); at most 10,000 prefixes; a prefix is one segment of `[A-Za-z0-9._~-]`, not `.` or `..`, at most 255 bytes; a target must already be a normalized hostname (labels of 1-63 bytes, at most 253 in all, an interior dot); 422 otherwise; 404 when this app does not hold the hostname; 403 on a platform hostname; 200 `{host, prefixes, changed}` | apidomains.go:771-888; claims.go:614-641, 716, 738-755, 771-821 |
| Paths is last-writer-wins: the PUT carries no condition | apidomains.go:762-770, 830-833 |
| A path target is used only when it is the same account's route on the same box; otherwise the prefix falls through to the hostname's own route | claims.go:779-783; aliases.go:324-329, 395-408; edge.go:1238-1240 |
| 180 domains calls a minute per principal | authcli.go:191; apidomains.go:256-263 |
| A host under the deployment's own domains that the edge has not heard of gets 503, `Retry-After: 5`, `Cache-Control: no-store`, HTML; any other unknown host a 302 to the fallback or a 404 | edge.go:2188-2209; pending.go:61, 210-218 |
| `X-Carlos-Version` is set on the route's answers, not on the unknown-host answers | edge.go:2354-2364; genproxy.go:298-302 |
| The edge's response cache keys on the requested host | edge.go:1255-1257 |
| `$CARLOS_HOST` is the instance's hostname, exactly as `carlos instances list` shows it; an instance is started as `<binary> --socket <path> --db <path>` | cli.md:814-830 |
| With `--private-storage`, the host's first label becomes the environment name, at most 32 characters | cli.md:877-880 |

### Platform gaps

Nothing here waits on these; `provision` uses what the platform gives today. File as carlos/platform discussions after Paul agrees:

1. **Publish the instances and paths API, with codes a client can branch on.** `docs/site/cli.md` documents no HTTP answers and no path routes. The status table spec §7.2 relies on is in the skills repo (`platform.md`, "Provisioning instances from your own app"), and it lacks the 202, the 503 with `Retry-After`, the 403 and the "not enabled" 409 the handlers give. Every 409 has the same shape, so a client tells "pick another label" from "instances are not enabled" only by an extra `GET`. Request: an HTTP reference page for both doors, and a stable `code` beside `error` (`host_exists`, `not_enabled`, `host_held`, `busy`, `no_room`).
2. **A conditional paths publish.** Two publishers that list and then publish can drop each other's new prefix, even after one of them has verified it. `provision` serializes publishes for one hostname inside one process (Task 4) and the daily tick republishes, but nothing prevents the loss across processes. The claim write is already a compare-and-swap inside the console (claims.go:813-816). Request: an `ETag` on `GET …/domains/{host}` and `If-Match` on `PUT …/paths`.
3. **A credential that can only publish paths** (a comment on #185). The hub holds two credentials: `instance` on the tenant app, and `publish` or `instance` on the hub app for paths. Either hub-app role reaches far more than a path map, and an account-wide credential reaches every app. That belongs in the hub's leak profile until a narrower role exists.

---

### Task 1: An instance can name itself on `/api/version`

**Files:**
- Create: `carlos/host.go`, `carlos/host_test.go`
- Modify: `serve.go` (`Options`; the `GET /api/version` handler in `buildHandler`, line 561)
- Modify: `docs/site/reference/carlos.md`, `docs/site/reference/rastrillo.md`
- Test: `seams_test.go` (beside the existing `/api/version` check at line 78)

**Interfaces:**
- Produces (package `carlos`):
  - `const HostHeader = "Rastrillo-Host"`
  - `func Host() string`: `$CARLOS_HOST`, `""` off CARLOS
- Produces (package `rastrillo`): `Options.NameInstance bool`

- [ ] **Step 1: Write the failing tests**

`carlos/host_test.go`:

```go
func TestHostReadsCarlosHost(t *testing.T) {
	t.Setenv("CARLOS_HOST", "acme-k3x9qa.bdf.oncarlos.com")
	if got := carlos.Host(); got != "acme-k3x9qa.bdf.oncarlos.com" {
		t.Fatalf("Host() = %q", got)
	}
	t.Setenv("CARLOS_HOST", "")
	if got := carlos.Host(); got != "" {
		t.Fatalf("Host() off CARLOS = %q, want empty", got)
	}
}
```

`seams_test.go`:

```go
func TestVersionNamesTheInstanceWhenAsked(t *testing.T) {
	t.Setenv("CARLOS_HOST", "acme-k3x9qa.bdf.oncarlos.com")
	h, err := buildHandler(Options{Mux: helloMux(&captured{}), NameInstance: true})
	if err != nil {
		t.Fatal(err)
	}
	w := doGet(t, h, "/api/version", nil)
	if got := w.Header().Get(carlos.HostHeader); got != "acme-k3x9qa.bdf.oncarlos.com" {
		t.Fatalf("%s = %q: a hub cannot tell which instance answered", carlos.HostHeader, got)
	}
	if w.Body.String() != BuildVersion {
		t.Fatalf("body = %q: the platform's build probe reads the body, which must not change", w.Body.String())
	}
}

func TestVersionNamesNothingByDefault(t *testing.T) {
	t.Setenv("CARLOS_HOST", "acme-k3x9qa.bdf.oncarlos.com")
	h, _ := buildHandler(Options{Mux: helloMux(&captured{})})
	if got := doGet(t, h, "/api/version", nil).Header().Get(carlos.HostHeader); got != "" {
		t.Fatalf("%s = %q without NameInstance: the platform hostname leaked", carlos.HostHeader, got)
	}
}

func TestVersionOffCarlosNamesNothing(t *testing.T) {
	t.Setenv("CARLOS_HOST", "")
	h, _ := buildHandler(Options{Mux: helloMux(&captured{}), NameInstance: true})
	if got := doGet(t, h, "/api/version", nil).Header().Get(carlos.HostHeader); got != "" {
		t.Fatalf("%s = %q off CARLOS, want absent", carlos.HostHeader, got)
	}
}
```

- [ ] **Step 2: Stub, and run them to see them fail**

Stubs: `HostHeader` as `""`, `Host()` returning `""`, the `NameInstance` field unused (`seams_test.go` imports `carlos` for `HostHeader`). `TestVersionNamesNothingByDefault` and `TestVersionOffCarlosNamesNothing` pass on this stub; list them as vacuous before the change.
Run: `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./carlos -run TestHost; CGO_ENABLED=0 GOFLAGS=-mod=mod go test . -run TestVersion`
Expected: `--- FAIL: TestHostReadsCarlosHost` and `--- FAIL: TestVersionNamesTheInstanceWhenAsked`.

- [ ] **Step 3: Implement**

`carlos/host.go`: `HostHeader` and `Host()`. The comment on `HostHeader` says why it exists: `X-Carlos-Version` names a release, two apps' releases can share an identifier, so a hub proving its path reaches the tenant needs the instance to say which host it is. The comment on `NameInstance` says what it discloses and why it is off by default. In serve.go, the `/api/version` handler sets `carlos.HostHeader` to `carlos.Host()` when `NameInstance` is set and the host is not empty, read per request so a test can set it per case. The body stays `BuildVersion`.

Docs, as a listing only (Task 0 has not run): `HostHeader` and `Host()` as a code-block listing in `reference/carlos.md`; the `NameInstance bool` line in `reference/rastrillo.md` wherever it lists `Options` fields, if it does. The prose for both lands in Task 6.

- [ ] **Step 4: Run them to verify they pass**

Run: the Step 2 commands, then `CGO_ENABLED=0 GOFLAGS=-mod=mod go test . ./internal/docsite -run 'Version|Wrap|Seam|Symbols|Reference'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add carlos/host.go carlos/host_test.go serve.go seams_test.go docs/site/reference/carlos.md docs/site/reference/rastrillo.md
git commit -m "Let an instance name itself on /api/version" -m "A hub proves its path reaches the tenant by asking the product path who answered. X-Carlos-Version cannot say: two apps' releases can share an identifier, and a scaffold ships hub and tenant from one commit. It is opt-in because the header tells any visitor the platform hostname behind a product path or custom domain. The body stays the build, because the platform's build probe reads it."
```

---

### Task 2: `provisiontest`, a fake console and edge with the platform's answers

**Files:**
- Create: `provision/provisiontest/platform.go`
- Create: `docs/site/reference/provisiontest.md`
- Modify: `internal/docsite/symbols_test.go` (`"provision/provisiontest": "reference/provisiontest"`), `docs/site/nav.json` (its entry, after `reference/objrec`, with the placeholder blurb; nav_test.go requires every page in nav and a non-empty blurb)
- Test: `provision/provisiontest/platform_test.go`

The fake is the platform every later test believes, so its answers are the cited ones, not guesses. It is an `http.RoundTripper` that routes on `req.URL.Host`: the console origin goes to the console half, every other host to the edge half. No sockets and no TLS. Apps are keyed by account and app, as the platform keys them.

**Interfaces:**
- Consumes: `carlos.HostHeader` (Task 1).
- Produces (package `amadan.net/rastrillo/rastrillo/provision/provisiontest`):
  - `type Platform struct { /* unexported */ }`, `func New() *Platform`, `func (p *Platform) RoundTrip(*http.Request) (*http.Response, error)`
  - `func (p *Platform) Client() *http.Client`: transport is `p`, no timeout of its own
  - `func (p *Platform) Console() string`: `"https://console.test"`
  - `const AppsDomain = "oncarlos.test"`: the deployment's own domain, whose unknown hosts get the pending 503
  - `func (p *Platform) Enable(account, app, domain string)`: what `carlos instances enable` writes
  - `func (p *Platform) Credential(account, app, role string) string`: an app-bound service credential
  - `func (p *Platform) Release(account, app, version string)`: the `X-Carlos-Version` the edge stamps on the app's routes
  - `func (p *Platform) Serve(account, app string, h func(host string) http.Handler)`: what each of the app's instances runs
  - `func (p *Platform) Attach(account, app, host string)`: a hostname the app holds, whose own route serves the app
  - `func (p *Platform) OtherBox(host string)`: that instance is homed on another box
  - `func (p *Platform) Unready(n int)`: every new instance gets the pending 503 for its first n requests
  - `type Reply struct { Status int; RetryAfter string; Hang, Record bool }`
  - `func (p *Platform) Script(replies ...Reply)`, `func (p *Platform) ScriptPaths(replies ...Reply)`: the next creates (or paths PUTs) answer these, in order, before the real logic
  - `func (p *Platform) Instances(account, app string) []string`, `func (p *Platform) Paths(host string) map[string]string`
  - `func (p *Platform) Calls() []string`: every console call as `"POST /api/cli/apps/bdf/tenant/instances"`; every edge request as `"GET https://<host><path>"`
  - `func Tenant(slug string) func(host string) http.Handler`: `GET /api/version` answers 200, body `"test"`, `Rastrillo-Host: <host>`; with `slug` set, `GET /<slug>/api/version` answers the same; everything else 404
  - `func Hub() func(host string) http.Handler`: `GET /api/version` the same way; everything else 404

`Reply` semantics: `Hang` blocks until the request's context ends and returns its error, so the client's own deadline fires. `Record` writes the instance before answering, as a 502, a timeout or a 202 after the write does. A scripted 202 answers `{"host": <host>, "operation_id": "op1", "phase": "creating"}` with no `Retry-After`; any other scripted status answers `{"error": "scripted <status>"}`; a non-empty `RetryAfter` sets that header.

The fake's rules, each from the platform:

| Request | Answer | Platform |
|---|---|---|
| No or unknown bearer | 401 `{"error":"not signed in …"}` | authcli.go:447-453, 1161-1163 |
| Credential's role not allowed at the door | 403 JSON | authcli.go:466-478 |
| Account segment or app not the credential's | 404 `text/plain` | apicli.go:70-79 |
| Create without `Content-Type: application/json` | 415 | authcli.go:1117-1121 |
| Create on an app not enabled | 409, no `Retry-After` | apiinstances.go:130-133 |
| Create with a host that is not one label plus the domain | 400 | apiinstances.go:1089-1093 |
| Create with an invalid `message` | 400 | activity.go:27-44 |
| Create of a host the app already has | 409 "instance <host> already exists" | apiinstances.go:1670 |
| Create ok | 201, the record (`host`, `app`, `account`, `channel`, `created_at`, `created_by`) | apiinstances.go:1717-1723 |
| Get a host the app has / has not | 200 record with `served_by` and `gated` / 404 `text/plain` | apiinstances.go:859-902 |
| Paths without the `paths` key | 400 | apidomains.go:823-829 |
| Paths with a bad prefix or target, or over 10,000 | 422 | apidomains.go:834-848; claims.go:614-641, 738-755 |
| Paths on a hostname this app does not hold | 404 `text/plain` | apidomains.go:849-855 |
| Paths ok | 200 `{"host","prefixes","changed"}`, `changed` false when the map is the same | apidomains.go:882-888; claims.go:797-799 |
| Edge: an unknown host under `AppsDomain`, or an instance still `Unready` | 503, `Retry-After: 5`, `Cache-Control: no-store`, HTML, no `X-Carlos-Version` | edge.go:2196-2203; pending.go:210-218 |
| Edge: any other unknown host | 404, no `X-Carlos-Version` | edge.go:2205-2209 |
| Edge: an instance's host | the app's handler, with the app's `X-Carlos-Version` | edge.go:2354-2364 |
| Edge: an attached hostname whose first segment names a ready instance of the same account on this box | the target's handler, path unchanged, the target's `X-Carlos-Version` | edge.go:1241-1254; aliases.go:395-408 |
| Edge: any other path on an attached hostname (an unlisted prefix, an off-box or other-account target) | the hostname's own route, its own `X-Carlos-Version` | edge.go:1238-1240; claims.go:779-783 |

- [ ] **Step 1: Write the failing tests**

`provision/provisiontest/platform_test.go`, one test per row group, using `p.Client()` directly with `net/http`:

- `TestFakeRefusesBadCredentials`: no bearer → 401; a `publish` credential at create → 403; a credential for `bdf/tenant` naming `bdf/other` → 404 with `Content-Type` starting `text/plain`.
- `TestFakeCreateAnswers`: not enabled → 409 with `Retry-After` empty; host `"Acme.bdf.oncarlos.test"` → 400; message `"a\nb"` → 400; first create → 201 with `host` in the JSON; same host again → 409 whose `error` contains `"already exists"`; get → 200 with `"gated":false`, unknown host → 404.
- `TestFakeScriptRecordsThenFails`: `Script(Reply{Status: 502, Record: true})`, create → 502, then `Instances("bdf", "tenant")` contains the host.
- `TestFakeScripted202`: `Script(Reply{Status: 202, Record: true})` → 202, `phase` `"creating"`, no `Retry-After`; the instance exists.
- `TestFakePathsAnswers`: `{}` body → 400; `{"paths":{"/a":"x.test"}}` → 422; target `"x"` (no dot) → 422; unattached product → 404; ok → 200 `changed:true`, same map again → `changed:false`.
- `TestFakeEdgeRoutesAndFallsThrough`: releases `hub` v1 and `tenant` v2 in `bdf`; after publishing `{"acme": host}`, `GET https://product.test/acme/api/version` → `X-Carlos-Version: v2`, `Rastrillo-Host: <host>` (tenant served with `Tenant("acme")`); after `OtherBox(host)` → `X-Carlos-Version: v1`, no `Rastrillo-Host` (the hub's 404).
- `TestFakeEdgeIgnoresAnotherAccountsTarget`: the same map naming an instance of account `zzz` → falls through to the hub.
- `TestFakeUnknownHosts`: `Unready(2)`; the first two `GET https://<host>/api/version` → 503 with `Retry-After: 5` and no `X-Carlos-Version`, the third → 200; `GET https://nowhere.example/` → 404.

- [ ] **Step 2: Stub, and run them to see them fail**

Stubs: every method a no-op, `RoundTrip` answering 418.
Run: `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./provision/provisiontest`
Expected: `--- FAIL` for each test, with the status it got.

- [ ] **Step 3: Implement `platform.go`, and document it**

One mutex guards all state. The console half is an `http.ServeMux` with the platform's own patterns (`POST /api/cli/apps/{account}/{app}/instances`, `GET …/instances/{host}`, `PUT …/domains/{host}/paths`), served through `httptest.NewRecorder`. The package comment names the platform commit the rules were read from (`04819f9b`) and says to change the table in this plan, the rules and that comment together.

`docs/site/reference/provisiontest.md`: a code-block listing of every exported name, including each `Platform` method and `RoundTrip`, and no prose; its nav blurb is the placeholder. Map the package in `referencePages`. Task 6 writes the approved prose and blurb.

- [ ] **Step 4: Run them to verify they pass**

Run: `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./provision/... ./internal/docsite`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add provision docs/site/reference/provisiontest.md docs/site/nav.json internal/docsite/symbols_test.go
git commit -m "provisiontest: a fake console and edge with the platform's real answers" -m "Every provision test, and Plans 6 and 7, believe this fake, so each rule is read from the platform's handlers and cited. A fake that answered every unknown host with 404, or routed another account's target, would let a readiness or routing bug pass."
```

---

### Task 3: `Console`, labels, and the create door

**Files:**
- Create: `provision/provision.go` (with the package comment) (`Config`, `Console`, `New`, errors, the request plumbing), `provision/label.go`, `provision/create.go`
- Create: `docs/site/reference/provision.md`
- Modify: `internal/docsite/symbols_test.go` (`"provision": "reference/provision"`), `docs/site/nav.json` (its entry, before `reference/provisiontest`, with the placeholder blurb)
- Test: `provision/label_test.go`, `provision/create_test.go`

**Interfaces:**
- Consumes: `provisiontest` (Task 2), in tests only.
- Produces (package `amadan.net/rastrillo/rastrillo/provision`):
  - `type Config struct { Console, Account, App, Token string; HTTP *http.Client; Timeout time.Duration; Observe func(Exchange) }`: `Account` is the sqid; `Timeout` bounds one console attempt, default `DefaultTimeout`; `Observe`, when set, sees every console exchange
  - `type Exchange struct { Method, Path string; Status int; Header http.Header; Message string; Err error }`: `Status` 0 when the attempt got no answer; `Header` is the response's; `Message` the error sentence or the body's first 512 bytes
  - `const DefaultTimeout = 30 * time.Second`
  - `type Console struct { /* cfg and a no-redirect copy of the client */ }`, `func New(cfg Config) (*Console, error)`
  - `func NewLabel(slug string) (string, error)`, `const MaxSlug = 25`, `var ErrSlug error`
  - `type CreateRequest struct { Host, Region, Message string; Resume bool }`
  - `type Created struct { Host string; Landed, Accepted bool; Operation, Phase string }`
  - `func (c *Console) Create(ctx context.Context, req CreateRequest) (Created, error)`
  - `type Box struct { Box, Region string }`
  - `type Instance struct { Host, App, Account, Region, Channel, CreatedBy string; CreatedAt time.Time; ServedBy []Box; Gated bool }`
  - `func (c *Console) Instance(ctx context.Context, host string) (Instance, error)`
  - `const MaxRelabels = 2`
  - `var ErrHostTaken, ErrConflict, ErrNotFound, ErrUnauthorized, ErrForbidden, ErrBadRequest error`
  - `type APIError struct { Status int; Message string }` with `Error()` and `Is(target error) bool`: 400, 413, 415 and 422 are `ErrBadRequest`, 401 `ErrUnauthorized`, 403 `ErrForbidden`, 404 `ErrNotFound`, 409 `ErrConflict`
  - `type RetryError struct { Status int; After time.Duration; Message string; Err error }` with `Error()` and `Unwrap()`: the outcome is unknown or the platform said try later; keep the host and call again with `Resume`
- Produces (unexported, in `provision.go`, defined once so `create.go`, `paths.go` and `probe.go` carry no copies: `retryError` serves `create.go` and `paths.go`, `noRedirect` serves `New` and `probe.go`):
  - `func retryError(status int, hdr http.Header, msg string) *RetryError`: `After` from `Retry-After` in seconds, 0 if absent
  - `func noRedirect(hc *http.Client) *http.Client`: a copy of `hc` (`http.DefaultClient` if nil) whose `CheckRedirect` returns `http.ErrUseLastResponse`

- [ ] **Step 1: Write the failing label tests**

`provision/label_test.go`:

```go
func TestNewLabelShape(t *testing.T) {
	l, err := provision.NewLabel("acme")
	if err != nil || !regexp.MustCompile(`^acme-[a-z2-7]{6}$`).MatchString(l) {
		t.Fatalf("NewLabel(acme) = %q, %v", l, err)
	}
	if l2, _ := provision.NewLabel("acme"); l2 == l {
		t.Fatal("two labels for one slug are equal: the suffix is not random")
	}
}

func TestNewLabelFitsAnEnvironmentName(t *testing.T) {
	l, err := provision.NewLabel(strings.Repeat("a", 24) + "-bcd")
	if err != nil || len(l) > 32 || strings.Contains(l, "--") {
		t.Fatalf("NewLabel(long) = %q, %v: a private-storage environment name is at most 32", l, err)
	}
}

func TestNewLabelRefusesWhatCannotBeADNSLabel(t *testing.T) {
	for _, s := range []string{"", "-a", "a-", "Acme", "a.b", "a_b", "a b", "ä"} {
		if _, err := provision.NewLabel(s); !errors.Is(err, provision.ErrSlug) {
			t.Errorf("NewLabel(%q): got %v, want ErrSlug", s, err)
		}
	}
}
```

- [ ] **Step 2: Write the failing create tests**

`provision/create_test.go`, with a helper `newTenant(t) (*provisiontest.Platform, *provision.Console, string /*domain*/)` that enables `bdf/tenant` on domain `bdf.oncarlos.test`, mints an `instance` credential, and builds a `Console` with `HTTP: p.Client()` and `Timeout: 200 * time.Millisecond`. `posts(p)` counts the create POSTs in `p.Calls()`.

- `TestCreateCreates`: `Create` → `Created{Host: h}`, not `Landed`; `p.Instances("bdf", "tenant")` holds h.
- `TestCreateFirstDuplicateIsHostTaken`: create h, then `Create` h again → `errors.Is(err, ErrHostTaken)`; `errors.As(err, &apiErr)` with `apiErr.Status == 409`; the calls are POST, GET.
- `TestCreateNotEnabledIsRefused`: an app never `Enable`d → `errors.Is(err, ErrConflict)`, not `ErrHostTaken`; the message names "not enabled"; 1 POST.
- `TestCreateRetryAfterTimeoutLands`: `Script(Reply{Hang: true, Record: true})` → `Created{Landed: true}`, 1 POST (the reconcile `GET` found it, so nothing is posted twice).
- `TestCreateRetryAfter502Lands`: `Script(Reply{Status: 502, Record: true})` → `Landed`.
- `TestCreateRetryAfter502WithoutTheWritePostsAgain`: `Script(Reply{Status: 502})` → `Created{Host: h}`, 2 POSTs.
- `TestCreateRetriesOnceOnly`: `Script(Reply{Status: 502}, Reply{Status: 502})` → `*RetryError` with `Status` 502; 2 POSTs.
- `TestCreateTransportErrorIsUnknown`: a client whose transport fails every POST with `io.ErrUnexpectedEOF` → `*RetryError` wrapping it, never `ErrHostTaken` or a permanent error.
- `TestCreateBodyReadErrorIsUnknown`: a transport that returns a canned 201 whose body fails mid-read with `io.ErrUnexpectedEOF`, and never reaches the fake (the fake's own answer would end the test as `Landed`), twice → `*RetryError`, never a permanent error.
- `TestCreateResumeReconcilesFirst`: create h; `Create(CreateRequest{Host: h, Resume: true})` → `Landed`, and the only new call is a GET.
- `TestCreateResumeOfAMissingHostPosts`: `Resume: true` on a host never created → `Created{Host: h}` after GET, POST.
- `TestCreateResumeWithABadToken`: `Resume: true` with a wrong token → `ErrUnauthorized` (an `*APIError`, not a `*RetryError`), zero POSTs. Spec §7.2 step 3 has no retry for an auth failure, so a revoked credential must not show a retry button forever.
- `TestCreateResumeWhenTheGetFails`: `Resume: true` with a transport that fails the GET → `*RetryError`, zero POSTs.
- `TestCreateBusyIsRetryLater`: `Script(Reply{Status: 409, RetryAfter: "2"})` → `*RetryError{Status: 409, After: 2 * time.Second}`, not `ErrHostTaken`; 1 POST.
- `TestCreateRateLimitedNeverLoops`: `Script(Reply{Status: 429})` → `*RetryError{Status: 429}`; 1 POST.
- `TestCreateNoRoomIsRetryLater`: `Script(Reply{Status: 503, RetryAfter: "60"})` → `*RetryError{After: time.Minute}`.
- `TestCreateAccepted`: `Script(Reply{Status: 202})` → a `Created` whose `Host` is the requested host, `Accepted` true, `Operation` `"op1"` and `Phase` `"creating"`, compared field by field (the algorithm also sets `Host`, so a literal without it would fail).
- `TestCreateRefusals`: wrong token → `ErrUnauthorized`; a `publish` credential → `ErrForbidden`; another app → `ErrNotFound`; host `"acme.example.org"` → `ErrBadRequest` from the platform's 400.
- `TestCreateChecksBeforeSending`: `Host: "Acme X"`, a `Message` with `"\n"`, 241 runes, or invalid UTF-8 → `ErrBadRequest` with zero calls.
- `TestCreateStopsWhenTheCallerGivesUp`: `Script(Reply{Hang: true})` with the parent context cancelled at 50ms → `context.Canceled`, 1 POST (only the attempt's own deadline earns the retry).
- `TestCreateObserves`: with `Observe` set, a 201 create reports one `Exchange{Method: "POST", Status: 201}`, and the token appears in no field.
- `TestInstanceGets`: after create, `Instance(h)` → `Host == h`, `App == "tenant"`, non-zero `CreatedAt`, `Gated` false; unknown → `ErrNotFound`.

- [ ] **Step 3: Stub, and run them to see them fail**

Stubs: `NewLabel` returns `"", nil`; `New` returns an empty `Console`; `Create` and `Instance` return zero values and nil.
Run: `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./provision`
Expected: `--- FAIL` for each test, saying what it got.

- [ ] **Step 4: Implement**

`label.go`: `slug` must match `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`; cut to `MaxSlug` and trim trailing hyphens; append `-` and 6 characters of lowercase RFC 4648 base32 from `crypto/rand`. The comment says why 25: with `--private-storage` the label becomes the environment name, at most 32 characters (cli.md:877-880), and 30 random bits make a collision a first-call event (spec §7.2 step 1).

`provision.go`: `New` refuses an empty `Console`, `Account`, `App` or `Token` (and no other empty field: `HTTP` nil and `Timeout` 0 take their defaults), a console that is not `https://` with no path, and a negative `Timeout`. It builds its client with `noRedirect(cfg.HTTP)`, defined here once, and the comment says why: a redirect is never an answer these calls asked for. `retryError` is defined here too, so the 429 and 5xx mapping is written once for `Create` and `PublishPaths`. One unexported `call(ctx, method, path string, body any) (status int, hdr http.Header, msg string, raw []byte, err error)` sets the bearer, `Content-Type` and `Accept: application/json`, reads at most 64 KiB, and takes `msg` from `{"error"}` or, for the text/plain 404s, the body collapsed to one line of at most 512 bytes. Each attempt runs under `context.WithTimeout(ctx, cfg.Timeout)` and is reported to `Observe` without the token.

`create.go`, the algorithm the tests pin:

```
check host (first label is a DNS label, then a dot and more) and message (valid UTF-8, ≤240 runes,
  no control characters): else ErrBadRequest, no call
if req.Resume: reconcile
reconcile: Instance(host): found → Created{Host, Landed}; ErrNotFound → post;
  a transport error, 5xx or 429 → RetryError; any other *APIError (400, 401, 403) → returned as it is
post (at most twice per Create):
  201 → Created{Host}
  202 → Created{Host, Accepted, Operation, Phase}
  409 with Retry-After → retryError
  409 → Instance(host): found → (req.Resume, or an earlier post in this call had an unknown outcome ? Created{Landed} : ErrHostTaken wrapping the APIError)
                        ErrNotFound → the APIError (Is ErrConflict): not enabled, held elsewhere, leftover data
                        a transport error, 5xx or 429 → RetryError; any other *APIError → returned as it is
  429, 503 → retryError (After from Retry-After seconds, 0 if absent)
  500, 502, the attempt's own deadline, any transport error, or a body that fails mid-read, while ctx is alive →
        first post: reconcile (which posts again only if the host is not found); second post: RetryError
  anything else → *APIError
```

The comment on `reconcile` says why an auth failure passes through: spec §7.2 step 3's table has no retry for it, and a revoked token reported as `RetryError` shows the person a retry button for ever. The comment on the 409 branch cites spec §7.2 step 3 and says why a `GET` decides it: every 409 has one shape (gap 1), "already exists" is the only one a `GET` can see, and an existing host behind a busy claim answers the busy 409 instead (createleftovers.go:43-50). The comment on `RetryError` says the record keeps its host, because the create may have landed. The same holds when the caller's own context ends after a post was sent: `Create` returns the context's error, and the reference page says to keep the host and resume.

Docs: `docs/site/reference/provision.md` as a code-block listing of every name and method this task exports (`Console.Create`, `Console.Instance`, `APIError.Error`, `APIError.Is`, `RetryError.Error`, `RetryError.Unwrap`), no prose; the nav blurb is the placeholder. Map the package in `referencePages`. Task 6 writes the approved opening and outcome table.

- [ ] **Step 5: Run them to verify they pass**

Run: `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./provision/... ./internal/docsite`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add provision docs/site/reference/provision.md docs/site/nav.json internal/docsite/symbols_test.go
git commit -m "provision: create an instance and read the platform's answers" -m "A create whose outcome is unknown has often landed, and treating the next 409 as a collision leaves an orphan instance the person never sees, so a retry reconciles with GET first. A 409 the GET cannot explain is refused for good rather than relabelled, because 'instances are not enabled' would fail under every label. Redirects are never followed, because none is an answer to these calls."
```

---

### Task 4: Publish the whole path map

**Files:**
- Create: `provision/paths.go`
- Modify: `docs/site/reference/provision.md` (the paths listing)
- Modify: `Makefile` (add `./provision/` to the first `go test -race` line of the `race` target)
- Test: `provision/paths_test.go`

**Interfaces:**
- Consumes: `Console`, `call`, `retryError`, `APIError`, `RetryError`, `ErrBadRequest` (Task 3); `provisiontest.ScriptPaths` (Task 2).
- Produces:
  - `type Published struct { Prefixes int; Changed bool }`
  - `func (c *Console) PublishPaths(ctx context.Context, product string, build func(context.Context) (map[string]string, error)) (Published, error)`
  - `const MaxPaths = 10000`
  - `var ErrEmptyMap error`

`PublishPaths` takes a builder, not a map, so the listing and the PUT happen under one lock, held per product hostname in a package-level map (not per `Console`), so two `Console` values in one process serialize too. Across processes, see gap 2 and the daily tick.

- [ ] **Step 1: Write the failing tests**

`provision/paths_test.go`, with a helper that attaches `product.test` to `bdf/hub`, mints a `publish` credential bound to `hub`, and builds a second `Console` for it:

- `TestPublishSendsTheWholeMap`: build returns `{"acme": h1, "globex": h2}` → `Published{Prefixes: 2, Changed: true}`, `p.Paths("product.test")` equals it; again → `Changed: false`.
- `TestPublishRefusesAnEmptyMap`: build returns `{}` → `ErrEmptyMap`, zero calls, the stored map unchanged.
- `TestPublishSendsNothingWhenTheBuildFails`: build returns an error → that error, zero calls.
- `TestPublishChecksBeforeSending`: prefixes `"/acme"`, `"a/b"`, `".."`, `""`, 256 bytes; targets `"Acme.test"`, `"acme"`, `"a..b.test"`, a 64-byte label → `ErrBadRequest`, zero calls. `MaxPaths+1` entries → `ErrBadRequest`, zero calls.
- `TestPublishNeedsTheHubsCredential`: the tenant app's `instance` credential → `ErrNotFound` (it is bound to the tenant app).
- `TestPublishOnAnUnheldHostname`: `other.test` → `ErrNotFound`.
- `TestPublishSerializesBuildsAcrossConsoles`: two `Console` values for the hub credential, 8 goroutines alternating between them, each appending its own slug to a shared slice under its own mutex and building the map from the whole slice; afterwards the stored map has all 8 prefixes.
- `TestPublishRetries502Once`: `ScriptPaths(Reply{Status: 502})` → success, 2 PUTs.
- `TestPublishSecond5xxIsRetryLater`: `ScriptPaths(Reply{Status: 502}, Reply{Status: 500})` → `*RetryError`, 2 PUTs.
- `TestPublishRateLimitedIsRetryLater`: `ScriptPaths(Reply{Status: 429})` → `*RetryError{Status: 429}`, 1 PUT.

- [ ] **Step 2: Stub, and run them to see them fail**

Stub: `PublishPaths` returns `Published{}, nil` without calling `build`.
Run: `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./provision -run Publish`
Expected: `--- FAIL` for each test.

- [ ] **Step 3: Implement `paths.go`**

The prefix rule exactly as claims.go:738-755; the target rule as `NormalizeAliasHost` with its result required to equal the input (claims.go:614-641, 782-784): lowercase, labels of 1-63 bytes of `[a-z0-9-]` not starting or ending with `-`, at most 253 bytes, at least one dot, no trailing dot. 200 → decode `{prefixes, changed}`. 500 or 502 → one more PUT, since a wholesale PUT of the same map is safe to repeat. A second 500 or 502, or a 429 → `retryError`. The comment on `ErrEmptyMap` says why there is no override: an empty build is far more often a failed or empty listing than a decision, and publishing it removes every tenant's address at once. The cost: when the last live tenant fails, its prefix stays published and may still reach that instance until a person clears the map with a call to the same door (`{"paths": {}}`); the reference page says so. That is a rare and visible state, where a wrongly empty publish is a silent outage of every tenant.

`Makefile`: `./provision/` joins the `race` target's package list, so `TestPublishSerializesBuildsAcrossConsoles` (and Task 6's `TestTwoRefreshesAdvanceOnce`) run under `-race` in `make ci`. `race` is already a `SUITE` role, so the `50-suite@N` count does not change and `.amadan/ci.d` needs nothing.

Docs: `Console.PublishPaths`, `Published`, `MaxPaths`, `ErrEmptyMap` as a listing on the reference page, no prose. The paths paragraph, with the last-tenant note above, is in Task 0's list and lands in Task 6.

- [ ] **Step 4: Run them to verify they pass**

Run: `CGO_ENABLED=1 GOFLAGS=-mod=mod go test -race ./provision -run Publish` (`-race` needs cgo, as the Makefile's `race` target does), then `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./internal/docsite`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add provision docs/site/reference/provision.md Makefile
git commit -m "provision: publish a hub's whole path map, never an empty one" -m "The platform's paths PUT replaces the map. A listing that failed or came back empty would clear every tenant's address, so neither is sent. The list and the PUT share one lock per hostname, so two signups in one process cannot drop each other's prefix."
```

---

### Task 5: Probe `/api/version`: ready, and routed

**Files:**
- Create: `provision/probe.go`
- Modify: `docs/site/reference/provision.md` (the probes listing)
- Test: `provision/probe_test.go`

**Interfaces:**
- Consumes: `carlos.HostHeader` (Task 1); `noRedirect` (Task 3); `provisiontest.Tenant`, `Hub`, `Unready`, `OtherBox`, `Release`, `Serve`, `Attach`, `Credential` (Task 2); `provision.New` (Task 3) and `Console.PublishPaths` (Task 4), which the routing tests use to publish a map (a `Console` on the hub app's `publish` credential, with `product.test` attached to `bdf/hub`).
- Produces:
  - `const ProbeTimeout = 5 * time.Second`
  - `type Answer struct { URL string; Status int; Version, Build, Instance string }`: `Version` is `X-Carlos-Version`, `Build` the body's first 128 bytes trimmed, `Instance` the `Rastrillo-Host` header
  - `func Ready(ctx context.Context, hc *http.Client, host string) (bool, Answer, error)`
  - `func WaitReady(ctx context.Context, hc *http.Client, host string, every time.Duration) (Answer, error)`
  - `type RouteState int` with `RoutePending`, `Routed`, `FellThrough`, `NoBasePath`, and `String()`
  - `type Route struct { State RouteState; Prefix, Tenant, Hub Answer }`
  - `func (r Route) Err() error`: `FellThrough` → `ErrFellThrough`, `NoBasePath` → `ErrNoBasePath`, `RoutePending` → `ErrNotRouted`, `Routed` → nil. `WaitRouted` uses it, and so does a per-refresh caller (Task 6's `advance`), who would otherwise copy the switch
  - `func CheckRoute(ctx context.Context, hc *http.Client, product, slug, host string) (Route, error)`
  - `func WaitRouted(ctx context.Context, hc *http.Client, product, slug, host string, every time.Duration) (Route, error)`
  - `var ErrFellThrough, ErrNoBasePath, ErrNotRouted error`

`hc` nil means `http.DefaultClient`. Either way the probe uses `noRedirect(hc)`, so redirects are never followed, under `ProbeTimeout` per request. Every probe adds `?provision=<8 random hex>`: the edge's response cache keys on the requested host (edge.go:1255-1257), and an answer cached before the publish must not be read as one after it.

- [ ] **Step 1: Write the failing tests**

`provision/probe_test.go`:

- `TestReadyWaitsForTheInstance`: `Unready(2)`; `Ready` false twice with `Status == 503` and no error, then true with `Instance == host` and `Build == "test"`.
- `TestReadyIgnoresARedirect`: the tenant's handler answers `/api/version` with `302 Location: https://product.test/acme/api/version` → false, `Status == 302`, and `p.Calls()` has no request to `product.test`.
- `TestReadyRefusesAnotherInstancesAnswer`: a handler answering 200 with `Rastrillo-Host: other.bdf.oncarlos.test` → false, with `Status == 200` and `Instance == "other.bdf.oncarlos.test"` (so the test fails on a stub that returns the zero `Answer`).
- `TestReadyRefusesAnAnonymous200`: 200 with no `Rastrillo-Host` (a catch-all, or a tenant without `NameInstance`) → false, with `Status == 200` and `Instance == ""`.
- `TestWaitReadyGivesUpWithTheContext`: never ready; `WaitReady` with a 300ms context and `every` 50ms → `errors.Is(err, context.DeadlineExceeded)`, last `Answer.Status == 503`.
- `TestCheckRouteRouted`: releases `hub` v1 and `tenant` v2, `Tenant("acme")`, published `{"acme": h}` → `Routed`.
- `TestCheckRouteFallsThroughOffBox`: as above with `OtherBox(h)` → `FellThrough`, `Prefix.Version == "v1"`.
- `TestCheckRouteSameVersionStillNeedsTheHostHeader`: both releases `abc1234`, `OtherBox(h)` → `RoutePending`, never `Routed`; without `OtherBox` → `Routed`.
- `TestCheckRouteHubNamedByItsHeader`: both releases `abc1234`, the hub's handler answers `/acme/api/version` with 200 and its own `Rastrillo-Host` (a locale-prefix catch), `OtherBox(h)` → `FellThrough`.
- `TestCheckRouteWithoutBasePath`: `Tenant("")`, distinct versions → `NoBasePath` (the tenant answered 404 at the prefix).
- `TestCheckRouteThirdInstanceIsPending`: the prefix answers 404 with the hub's version but `Rastrillo-Host` naming neither host nor hub → `RoutePending`.
- `TestCheckRouteTenantErrorIsPending`: the tenant answers `/acme/api/version` with 503 under distinct versions → `RoutePending`, not `NoBasePath`.
- `TestCheckRouteBeforePublishIsFallThrough`: no map published, distinct versions → `FellThrough`.
- `TestRouteErrMapsEachState`: `Route{State: FellThrough}.Err()` is `ErrFellThrough`, `NoBasePath` is `ErrNoBasePath`, `RoutePending` is `ErrNotRouted`, `Routed` is nil.
- `TestProbesCarryAFreshQuery`: a handler served by `Serve` records `r.URL.RawQuery` per request (`Calls()` has no query); two `Ready` probes of one host each carry `provision=` and 8 hex digits, and the two values differ; the `CheckRoute` probes carry it too.
- `TestWaitRoutedReportsWhyAtTheDeadline`: `OtherBox(h)` with a 300ms context → `ErrFellThrough`; `Tenant("")` → `ErrNoBasePath`; same versions and off-box → `ErrNotRouted`; each wraps `context.DeadlineExceeded`.

- [ ] **Step 2: Stub, and run them to see them fail**

Stubs: `Ready` returns `false, Answer{}, nil`; `CheckRoute` returns `Route{State: -1}` (the zero `Route` has `State == RoutePending`, which would pass the pending tests); `Route.Err` returns nil; the waits return `ctx.Err()` at once.
Run: `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./provision -run 'Ready|Route'`
Expected: `--- FAIL` for each test.

- [ ] **Step 3: Implement `probe.go`**

`Ready` is true only for a 200 whose `Instance` equals `host`. A transport error comes back as the error with `false`; callers treat it as not ready yet. `CheckRoute`, the decision the tests pin:

```
prefix := probe https://<product>/<slug>/api/version
if prefix.Status == 200 && prefix.Instance == host → Routed
tenant := probe https://<host>/api/version
hub    := probe https://<product>/api/version
distinct := tenant.Version != "" && hub.Version != "" && tenant.Version != hub.Version
switch {
case prefix.Instance != "" && prefix.Instance == hub.Instance:                      FellThrough
case prefix.Instance != "":                                                          RoutePending  (a third instance answered)
case distinct && prefix.Version == hub.Version:                                     FellThrough
case distinct && prefix.Version == tenant.Version && prefix.Status == 404:          NoBasePath
default:                                                                            RoutePending
}
```

The comment on `FellThrough` cites spec §7.4 and edge.go:1238-1240 and says the state is not final on its own: a publish takes a few seconds to go live, so callers keep checking until their own deadline. `Route.Err` holds the state-to-error mapping once. `WaitRouted` returns the last route's `Err()` at the deadline, wrapping `ctx.Err()`.

Docs: every name and method this task exports (`Route.Err` and `RouteState.String` included) as a listing, no prose. The probes paragraph is in Task 0's list and lands in Task 6.

- [ ] **Step 4: Run them to verify they pass**

Run: `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./provision ./internal/docsite`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add provision docs/site/reference/provision.md
git commit -m "provision: tell when a tenant is ready, and whether its path reaches it" -m "Only a 200 from the instance that names itself counts. The edge's pending page, a redirect, a catch-all's 200 and the hub answering the prefix would all otherwise pass for the tenant. The version header tells a fall-through from a tenant that does not serve its base path, which is what the record's failure has to say."
```

---

### Task 0: Copy review of the plan's prose (runs after Task 5, before Task 6)

**Files:** none in the repo. The copy-review skill's string index only.

Tasks 1-5 write only API listings and placeholder nav blurbs, so the review sees what was built and Task 6 writes what it approves. Start it when Task 5 is committed. The number stays 0 because the plan was written with it first.

- [ ] **Step 1: Draft every user-facing string this plan adds, and review it before writing any of it**

Draft, from the API listings and bullet lists in Tasks 1-6: the `Host`/`HostHeader` section of `docs/site/reference/carlos.md`; the `NameInstance` sentence for `docs/site/reference/rastrillo.md`; `docs/site/reference/provision.md`'s prose (opening, the outcome table, the paths paragraph from Task 4 including the last-tenant note, the probes paragraph from Task 5, the record contract in reader terms, the credential and leak paragraph, and the line that a tenant must be built with `NameInstance` or `Ready` and `CheckRoute` never see it); `docs/site/reference/provisiontest.md`'s prose; the two nav blurbs, which replace the placeholders; the SKILL.md sentence; the two CHANGELOG entries, in the file's own style (a bold paragraph, then `- New ...` lines). Run the `copy-review` skill on them. Apply Paul's edits verbatim. Task 6 writes the approved strings into their files. Code blocks and API listings are not copy.

---

### Task 6: The record contract, pinned, and the remaining docs

**Files:**
- Create: `provision/sequence_test.go`
- Modify: the approved prose from Task 0 goes into `docs/site/reference/carlos.md`, `docs/site/reference/rastrillo.md`, `docs/site/reference/provision.md` (opening, outcome table, paths, probes, record contract), `docs/site/reference/provisiontest.md`, and `docs/site/nav.json` (both blurbs, replacing the placeholders)
- Modify: `SKILL.md` (§2, after the objrec paragraph), `skillmd_test.go` (only if the budget must rise), `CHANGELOG.md` (`## Unreleased`)

**Interfaces:**
- Consumes: everything from Tasks 1-5, including `Route.Err`; `objrec.Dir`, `objrec.Store` with its `List` and `Delete`, `objrec.CreateJSON`, `objrec.UpdateJSON`, `objrec.GetJSON`, `objrec.ErrExists`, `objrec.ErrChanged`, `objrec.ErrNotFound` (main).

- [ ] **Step 1: Write the sequence tests**

`provision/sequence_test.go` holds the record contract ("Decisions") on `objrec.Dir(t.TempDir())`. Its record type and its `advance(ctx, key)` step (one refresh; it reads a hub's route with `CheckRoute` and takes the failure text from `Route.Err()`) are local to the test, because the record belongs to the app: `{Label, Host, State, Routed, Relabels, LastError, Created}`.

- `TestHomeSequenceWithRecords`: `NewLabel` → `CreateJSON` at `users/u1/instances/<label>.json` as `creating` → `Unready(3)` → `advance` per refresh until `live`. The record existed before the fake saw the POST (compare `len(p.Calls())` at write time).
- `TestHomeSequenceRelabels`: a first label whose host already exists in the app → `ErrHostTaken`; a new record at the new label is created, then the old one deleted; `Relabels` is 1 and persists across `advance` calls; at `MaxRelabels` the record is `failed`.
- `TestHomeSequenceNotEnabledFails`: an app never enabled → the record is `failed` with the platform's sentence after one POST, no new label.
- `TestHomeSequenceResumesAfterACrash`: `Script(Reply{Hang: true, Record: true})` on a `Console` whose client also fails every `GET …/instances/` with a transport error, so the reconcile cannot see the write: `Create` returns `*RetryError`, the record stays `creating` with its host. The next `advance`, on a working client, calls `Create` with `Resume: true` → `Landed`; `p.Instances` holds exactly one host.
- `TestHomeSequenceAcceptedThenServes`: `Script(Reply{Status: 202, Record: true})` with `Unready(2)` → `Created{Accepted: true}`, the record stays `creating`, and later `advance` calls reach `live` with no second POST.
- `TestHomeSequenceCrashBeforeThePost`: the record is written and the process stops before `Create`; the next `advance` resumes, `GET` finds nothing, posts once.
- `TestTwoRefreshesAdvanceOnce`: after a create whose outcome was unknown (`Script(Reply{Status: 502}, Reply{Status: 502})`), two goroutines `advance` the same `creating` record at once, both with `Resume: true`. A `Store` wrapper holds both goroutines after their read until both have read, so they truly overlap, and counts the successful writes that set `live`. The test asserts only the invariants, never which goroutine met `ErrChanged`: `p.Instances` ends with exactly one host, neither goroutine sees `ErrHostTaken`, and exactly one write to `live` succeeded.
- `TestHubSequenceWithRecords`: `acme` and `globex` interleaved; each writes `tenants/<slug>.json` as `creating`, creates, becomes `live` on `Ready`, then publishes with a build that pages `tenants/` with limit 1 and maps every `live` record, then `CheckRoute` per refresh until `Routed`. Both end `live` and routed, and `p.Paths("product.test")` has both.
- `TestHubLeavesCreatingTenantsOut`: while `globex` is still `creating`, `acme`'s publish maps `acme` only.
- `TestHubBuildFailsOnALaterPage`: a store wrapper that fails `List` on the second page → `PublishPaths` returns that error, zero PUTs, the stored map unchanged.
- `TestHubSequenceMarksAFallThroughFailed`: `OtherBox` for `globex`; after the caller's deadline (three refreshes) its record is `failed` with `LastError` the text of `ErrFellThrough` (from `Route.Err()`); `acme` stays routed.

`TestHubSequenceWithRecords`, `TestHubLeavesCreatingTenantsOut` and `TestHubBuildFailsOnALaterPage` are worked examples: the build function that lists `tenants/` and maps the `live` records lives in the test file, because the records are the app's. What they pin in `provision` is that a build which fails or lists only `live` records leads to zero PUTs or the right map. Plans 6 and 7 copy the build, not the assertion on it. For `TestHubBuildFailsOnALaterPage` the pinned effect is zero PUTs and an unchanged stored map.

- [ ] **Step 2: Run them on the stubs, then on the code**

Before: in the separate worktree at `bfc8ade7`, apply the Step 2 stubs of Tasks 1-5 (including `CheckRoute` returning `Route{State: -1}`), add `sequence_test.go`, and run the command below. Expected: `--- FAIL` for each test (a test that still passes on the stubs goes in the discussion as vacuous). Post the commit, the command and both outputs.

After: on the branch, run: `CGO_ENABLED=1 GOFLAGS=-mod=mod go test -race ./provision -run 'Sequence|Refreshes|Hub'`
Expected: PASS. These tests pin the API's order of use. A failure here means an earlier task's outcome does not map to one record transition, so fix that task, not this test.

- [ ] **Step 3: Docs, nav, SKILL.md, changelog**

Write the strings Task 0 approved, and nothing else, into every file in this task's Files list, and replace both placeholder nav blurbs. Then `grep -rn 'Draft, replaced after copy review' docs/site` finds nothing. The record-contract section on `reference/provision.md`: write the record first; keep the host; every transition a compare-and-swap; the counters in the record; what a hub publishes; credentials and the leak profile (spec §8.1, §9, gap 3); `NameInstance` on tenant and hub, with the line that a tenant `provision` creates must be built with `NameInstance`, because `Ready` and `CheckRoute` compare `Rastrillo-Host` (spec §7.2 compares a version header, which two apps can share); leave `Region` empty when the tenant app's region is the hub's, otherwise pass the hub's `$CARLOS_REGION`; no delete.

`SKILL.md` §2, after the objrec paragraph, the approved sentence, about this long:

```markdown
A home or hub creates tenants with `provision`: write the record first,
`Create`, then `Ready`; a hub then runs `PublishPaths` and `CheckRoute`.
Test with `provisiontest`. docs/site/reference/provision.md
```

Run: `CGO_ENABLED=0 GOFLAGS=-mod=mod go test -run TestSkillMD .`
Expected: PASS after trimming redundant prose elsewhere in SKILL.md. If nothing is genuinely redundant, raise `skillBudget` in `skillmd_test.go` by the sentence's size and add the reason beside it.

`CHANGELOG.md` under `## Unreleased`: the two approved entries in the file's style: a bold paragraph for `rastrillo/provision` followed by `- New ...` lines for each new name, and a bold paragraph or `- New option ...` line for `Options.NameInstance` (see the colour-scheme entry). Not "Added:".

- [ ] **Step 4: Run the gate**

Run: `make ci`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add provision/sequence_test.go docs/site/reference/carlos.md docs/site/reference/rastrillo.md docs/site/reference/provision.md docs/site/reference/provisiontest.md docs/site/nav.json SKILL.md skillmd_test.go CHANGELOG.md
git commit -m "provision: pin the record contract, and finish its docs" -m "provision leaves the records to the app, so the contract that keeps them repairable has to live where a scaffold can copy it: record before POST, the same host until a GET says otherwise, every transition a compare-and-swap, counters in the record, and a full listing before a publish."
```

---

### Task 7: Check the create door on Carloku

The fake encodes the handlers as read at `04819f9b`. Only the running console shows which of them Carloku takes: whether `CARLOS_HOST_CLAIM` is on (201 or 202), what the edge answers before the route exists, and that a repeated create is the 409 Task 3 reads. Path publishing is not checked here, because it needs a custom domain attached to a hub app, so it waits for Plan 7's end-to-end run unless Paul names a domain. **Stop before Step 3 and get Paul's scoped approval.**

**Files:**
- Create: `hack/provision-live/main.go` (under `hack/`, which the docs test skips)

**Interfaces:**
- Consumes: `provision.New`, `Config.Observe`, `NewLabel`, `Create`, `Created.Landed`, `Instance`, `Ready`, `ErrHostTaken` (Tasks 3, 5); `provisiontest` (Task 2), in the test only; `rastrillo.Resolve`, `rastrillo.Serve`, `Options.NameInstance` (Task 1).
- Produces (unexported): `check(ctx context.Context, cfg checkConfig, hc *http.Client) (result, error)`, the seam the fake test drives. `checkConfig` carries the five `PROVISION_*` values and `Every time.Duration` (the `Ready` interval, 2 seconds in `main`, milliseconds in the test); `result` is the JSON object below; `main` reads the environment, passes `http.DefaultClient`, and prints.

- [ ] **Step 1: Write the check tool**

Serving is the default. Run with no subcommand, as the platform starts it (`<binary> --socket <path> --db <path>`, cli.md:814-816), it is a minimal rastrillo app: `Resolve` then `Serve` with `NameInstance: true` and the default locale pack, so `CheckConfig` passes (the failure #61 hit). `provision-live check`, run from DOCS-T1, reads `PROVISION_CONSOLE`, `PROVISION_ACCOUNT`, `PROVISION_APP`, `PROVISION_TOKEN` and `PROVISION_DOMAIN`, records every console exchange through `Observe` (method, path, status, response headers, message; never the token), and prints one JSON object:

1. `NewLabel("live")`, `Create`.
2. `Ready` every 2 seconds for up to 3 minutes, recording each status seen (the pending 503 is expected first).
3. `Instance(host)`.
4. `Create` the same host again; expect `ErrHostTaken`.
5. `Create` with `Resume: true`; expect `Landed`.
6. Print `{"ok", "host", "exchanges": [...], "probes": [...], "repeat", "resume"}`. It never deletes: that is a person's act.

Two tests in `hack/provision-live`: one calls `check(ctx, cfg, p.Client())` on a `provisiontest.Platform` (`Enable` and `Credential(..., "instance")` for the app, `Serve` with `Tenant("")`, `Unready(1)`, a millisecond `Every`) and asserts `ok`, `repeat` `"host taken"` and `resume` `"landed"`, so the test never reaches `https://console.test` over a network; one starts the binary's default path as the platform does, with `--socket <tmp>/s.sock --db <tmp>/d.db` and `CARLOS_HOST` set, and asserts `GET /api/version` over that socket answers 200 naming the host. Run: `CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./hack/provision-live && make ci`
Expected: PASS. Commit: `hack/provision-live: check the create door on Carloku from outside`.

- [ ] **Step 2: Post the approval request, then wait**

`amadan discuss new rastrillo/rastrillo -branch provision -type Question -title "Approve: one live provision check on Carloku" -body -`, modelled on #61:

- **Account, console and region:** Carloku, `--console https://console.carloku.com`, account `demo` (`bdf`), the app's default region.
- **Who runs what:** setup and cleanup are a person's acts, signed in as an owner of `demo`: enabling instances and minting or revoking a credential are closed to every service credential (authcli.go:633-647, 874-888; apiservices.go:114-123). The check itself runs with the minted credential only.
- **Setup, in order:**
  1. `carlos apps create --console https://console.carloku.com --account demo --app provision-live`
  2. `carlos instances enable --console https://console.carloku.com --account demo --app provision-live`
  3. `carlos ship --console https://console.carloku.com --account demo --app provision-live --version <short sha> ./provision-live-linux-arm64`, then `carlos promote --console https://console.carloku.com --account demo --app provision-live <short sha>`
  4. `carlos services create --console https://console.carloku.com --account demo --role instance --app provision-live provision-live`. The secret prints once and lives only in the check's shell environment on DOCS-T1.
- **The check:** `provision-live check`, creating one instance, `live-<6 random>.bdf.oncarlos.com`, with about 6 console calls (well under 60 a minute) and about 90 probes of its `/api/version`.
- **Permissions during the check:** an `instance` credential bound to `provision-live` only. No store, no domains, no paths.
- **Cost:** negligible: one hibernating instance for minutes.
- **Cleanup, also on failure:** `carlos instances delete --console https://console.carloku.com --account demo --app provision-live --host <host>`; `carlos services revoke --console https://console.carloku.com --account demo provision-live`; `carlos apps delete --console https://console.carloku.com --account demo provision-live` (flags before the name: the parser stops at the first positional argument, apps.go:228-235). Deleting the instance keeps its data by design; deleting the app moves it to the Trash with its releases and name for 30 days (cli.md:137-145). I report what is left.
- **Verification:** the JSON pasted in the thread. Expected `ok: true`, a create of 201 or 202, `repeat: "host taken"`, `resume: "landed"`, and probes that end in a 200 naming the host.

- [ ] **Step 3: Run it (after approval only)**

```bash
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 GOFLAGS=-mod=mod go build -o provision-live-linux-arm64 ./hack/provision-live
# setup 1-4 from Step 2, then:
PROVISION_CONSOLE=https://console.carloku.com PROVISION_ACCOUNT=bdf PROVISION_APP=provision-live \
PROVISION_DOMAIN=bdf.oncarlos.com PROVISION_TOKEN=… go run ./hack/provision-live check
```

If an answer differs from the fake (a status, a header, a body shape), stop: change `provisiontest`'s rule and this plan's table to the observed answer, fix Task 3 or 5 to match, and file the difference as a carlos/platform discussion if it contradicts the handler. Either way, clean up as listed in Step 2 and paste what ran.

---

## Self-review notes

- Spec coverage. §5 (home and hub records, `creating|live|failed`): the record contract and Task 6. §7.1 (credential on the central app only): the reference page and gap 3. §7.2 steps 1 and 3: Task 3. Steps 2 and 5: the app's, pinned in Task 6. Step 4: Task 5's `Ready`. Step 6: Tasks 4 and 5; the owner grant and redirect are Plan 7's. §7.4: `FellThrough`, `TestHubSequenceMarksAFallThroughFailed`. §7.5: no delete call, `ErrEmptyMap`. §8.1 and §9: the reference page, `NameInstance`'s disclosure, gap 3. §11: Task 7, scoped like #61.
- Review Focus pins: 1 in `TestCreateRetryAfterTimeoutLands`, `TestCreateResumeReconcilesFirst`, `TestCreateTransportErrorIsUnknown`, `TestHomeSequenceResumesAfterACrash`; 2 in `TestCreateNotEnabledIsRefused`, `TestCreateBusyIsRetryLater`; 3 in `TestReadyIgnoresARedirect`, `TestReadyRefusesAnAnonymous200`, `TestReadyRefusesAnotherInstancesAnswer`, `TestCheckRouteHubNamedByItsHeader`; 4 in `TestCheckRouteSameVersionStillNeedsTheHostHeader`; 5 in `TestPublishRefusesAnEmptyMap`, `TestPublishSendsNothingWhenTheBuildFails`, `TestHubBuildFailsOnALaterPage`.
- Deviations from the spec, to raise against it. §7.2 steps 4 and 6 say to poll for a 200 and compare the version header; this plan requires a 200 whose `Rastrillo-Host` equals the host, and keeps the version header as the diagnosis, because two apps' releases can share an identifier (Review Focus 4). So a tenant that `provision` creates must be built with `NameInstance`, or it never reads ready; the reference page says so. §7.2 step 3 says a first-call 409 means pick a new label; this plan relabels only when the reconciling `GET` finds the host, and refuses any other 409 for good, because "instances are not enabled" fails under every label and cannot be told from "held elsewhere" without matching the message text. The cost is that a leftover-data collision (about 2^-30 per label) fails the signup instead of relabelling.
- Order of work. Task 0, the copy review, runs after Task 5 and before Task 6, so Tasks 1-5 carry only API listings and placeholder blurbs and Task 6 writes the reviewed prose into every page.
- Plan 5 may also edit `buildHandler` and `Options`. Task 1's change is one field and one header line; whichever branch lands second rebases over it.
