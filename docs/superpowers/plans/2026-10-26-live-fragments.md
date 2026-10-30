# Live pages Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An app calls `Notify("comments")` after a write, and every open page showing comments fetches that region again; forms marked for it post with fetch and swap in the regions that changed; with scripts off nothing changes.

**Architecture:** A new top-level package `live` holds an in-memory hub (per-topic versions, a Server-Sent Events handler, the fragment-request helpers). `ui.Pages` gains region rendering. A new first-party script `ui/live.js`, sibling of `select.js`, owns the stream, region swaps and swapping forms; `busy.js` hands swapping forms over to it. `examples/notes` proves it end to end.

**Tech Stack:** Go 1.26 stdlib (`net/http`, `html/template`), plain ES5-style JS in an IIFE (the house style of `rastrillo.js`), chromedp browser tests through `harness`, node unit tests through `nodetest`.

**Spec:** `docs/superpowers/specs/2026-10-26-live-fragments-design.md` (approved). Read it before any task; this plan names the decisions, the spec gives the reasons.

## Global Constraints

- The scriptless path is the real one: every page and form works with scripts disabled, unchanged from today.
- The stream carries topic names and versions only, never data. Every topic is declared and checked per viewer.
- Versions are per topic. No shared counter ever leaves the server.
- Versions are captured at request start, before the handler queries anything; the worst case is one extra fetch, never a missed change.
- The flash is taken only by a render that shows the notice region.
- A region never renders unless its template is defined as `region:<name>`; no template executes by an arbitrary name.
- Fragment and full responses of a live page send `Cache-Control: no-store` and add `Rastrillo-Fragment` to `Vary`.
- Shutdown ends every stream inside the 10-second drain (`serve.go:382`), including one blocked on a slow client.
- Live streams are off unless the app constructs a hub and mounts its handler; Patrix is not changed on this branch (spec §5, "Until #432 ships").
- The activator and edge integration test (spec §5, "Proven end to end") waits on carlos/platform#432 and is not part of this plan.
- `ui/rastrillo.js` is not edited: it has 284 bytes of headroom under `TestShimIsSmall`'s 18 KB cap. New client code goes in `ui/live.js`.
- No new third-party dependencies, in the root module or the examples.
- User-facing strings go through the `copy-review` skill before they are written into any file, then into all 12 `locales/*.toml` catalogs.
- Comments say why, naming the failure prevented (AGENTS.md). Commit subjects are imperative; bodies explain why.

## Wire formats (shared by Tasks 1–7)

Fixed here so server and client tasks agree:

- **Topic:** `name` or `name/key`. `name` matches `^[a-z][a-z0-9-]{0,31}$`; `key` matches `^[A-Za-z0-9_-]{1,64}$`.
- **Token:** `topic@version`, version a decimal `uint64`. Example: `notes/42@7`.
- **Region attributes:** `id="<region-name>" data-live="<token> <token>…" data-live-boot="<boot>"`. Region names match `^[a-z][a-z0-9-]{0,63}$`, so the jobs shim's legacy header value `1` is never a region name.
- **Boot ID:** 12 lowercase hex characters from `crypto/rand`, new per `live.New`.
- **Page head:** `<meta name="rastrillo-live" content="<stream path>" data-paused="<text>" data-unconfirmed="<text>">`. No meta, no stream: the page is static.
- **Stream request:** `GET <stream path>?boot=<boot>&t=<token>&t=<token>…`; an optional `Last-Event-ID` header overrides the URL's versions when its boot matches.
- **Stream events:** `event: change`, `data: <token>` (the topic's new version), `id: <boot> <token> <token>…` (every subscribed topic's version as this stream now knows it). Heartbeat `: ping`.
- **Fragment request headers:** `Rastrillo-Fragment: <region> <region>…` (space-separated), and on a form's fetch `Rastrillo-Page: <path of the page the form is on>`.
- **Fragment response:** the regions' elements concatenated, plus header `Rastrillo-Regions: <names actually rendered>`. `204` with `Rastrillo-Location: <path>` means "navigate there".
- **Notice region:** reserved name `notice`; the element is `<div id="notice" role="status" …>`, present and empty when there is no flash.
- **Form opt-in:** `data-live-form` on a `<form>`; its value is extra target region names (may be empty). The form's own region is its closest `[data-live]` ancestor.

## Review Focus

1. **A page with a live region but no hub middleware.** Expect versions computed as if the request started at sequence 0: at worst one extra fetch per region on connect, never a missed change. Test: `TestViewWithoutMiddlewareIsConservative` (Task 1).
2. **Two tabs of the same person, one deleting the note the other has focused.** Expect the focused row's replacement gone, focus on the region, never `<body>`. Test: `TestSwapFocusFallsBackToTheRegion` (Task 5).
3. **A stream request for a topic name that is declared but whose key belongs to someone else.** Expect the whole stream refused with 403 before any event, and no subscription left in the hub. Test: `TestStreamRefusesAForeignKey` (Task 2).
4. **A swapping form on a page whose handler redirects elsewhere (create note → `/notes/{id}`).** Expect a full navigation that shows "Note created.", with the flash not consumed by the fetch. Test: `TestAFormWhoseRedirectGoesElsewhereNavigatesWithItsNotice` (Task 9).
5. **The legacy jobs poll (`Rastrillo-Fragment: 1`) on an app that also uses live regions.** Expect the jobs fragment unchanged: `live.Wanted` reports not-a-region-request. Test: `TestWantedIgnoresTheJobsPollHeader` (Task 4).

---

### Task 1: `live` hub core — topics, versions, snapshots

**Files:**
- Create: `live/live.go` (package doc, `Config`, `Hub`, `New`, `Notify`, topic parsing)
- Create: `live/view.go` (`Middleware`, `View`, `Attrs`, `Head`)
- Test: `live/live_test.go`, `live/view_test.go`

**Interfaces:**
- Produces:
  - `type Allow func(r *http.Request, key string) bool`
  - `type Config struct { Path string; Topics map[string]Allow; MaxStreams int; Lifetime, Heartbeat, WriteDeadline time.Duration }` — defaults when zero: `MaxStreams 1000`, `Lifetime 5*time.Minute`, `Heartbeat 15*time.Second`, `WriteDeadline 5*time.Second`. `Path` is required (where the app mounts `Handler`).
  - `func New(cfg Config) (*Hub, error)` — errors on an empty `Path`, a topic name not matching the wire format, or a nil `Allow`.
  - `func (h *Hub) Boot() string`
  - `func (h *Hub) Notify(topics ...string)` — never blocks; an undeclared or malformed topic is logged (`slog`) and skipped, never a panic.
  - `func (h *Hub) Middleware(next http.Handler) http.Handler` — records the hub's internal sequence at request start in the request context.
  - `func (h *Hub) View(r *http.Request) View`
  - `type View struct{ … }` with `func (v View) Attrs(topics ...string) template.HTMLAttr` (emits `data-live="…" data-live-boot="…"`; `""` for a zero View or nil hub) and `func (v View) Head(paused, unconfirmed string) template.HTML` (the meta tag; `""` when zero).
  - unexported, for Task 2: `func (h *Hub) version(topic string, startSeq uint64) uint64`, `func parseTopic(s string) (name, key string, ok bool)`, `func parseToken(s string) (topic string, v uint64, ok bool)`.

Versioning algorithm (the tests don't determine it): the hub keeps one internal `seq uint64`, never exposed. Per topic it keeps `counter` and `changedAt` (the `seq` of its last change). `Notify` takes the lock, bumps `seq` once, and for each topic sets `counter++`, `changedAt = seq`, then wakes subscribers (Task 2). `version(t, start)` returns `counter` if `changedAt <= start`, else `counter-1`. Any value below the current counter is safe; an extra fetch is the only cost.

- [ ] **Step 1: Write the failing tests**

```go
func TestNotifyRaisesOnlyTheNamedTopics(t *testing.T)        // Notify("a") twice, Notify("b") once: version("a")==2, version("b")==1, version("c")==0
func TestVersionsNeverMoveForOtherKeys(t *testing.T)         // Notify("notes/1") ×3: View for a request reading "notes/2" renders data-live="notes/2@0"
func TestAWriteAfterRequestStartReadsAsOlder(t *testing.T)   // Middleware captures start; Notify("a"); View.Attrs("a") renders "a@0" while the counter is 1
func TestViewWithoutMiddlewareIsConservative(t *testing.T)   // Notify("a"); no middleware; Attrs("a") renders "a@0"
func TestNotifySkipsUndeclaredTopics(t *testing.T)           // Notify("nope", "a") raises a, logs, no panic
func TestNewRefusesBadConfig(t *testing.T)                   // empty Path; topic "Bad Name"; nil Allow
func TestAttrsAndHeadAreEmptyWithoutAHub(t *testing.T)       // var v live.View; v.Attrs("a")=="" && v.Head("x","y")==""
func TestHeadEscapesItsText(t *testing.T)                    // Head(`a"<b`, "c") renders the attribute escaped
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./live/ -run 'TestNotify|TestVersions|TestAWrite|TestView|TestNew|TestAttrs|TestHead' -count=1`
Expected: FAIL, package does not compile.

- [ ] **Step 3: Implement `live/live.go` and `live/view.go`** to the interfaces above. The package doc explains why topics are checked per viewer and why no shared counter is exposed (spec §2, Astra round 2 N1).

- [ ] **Step 4: Run them to see them pass, with `-race`**

Run: `CGO_ENABLED=1 go test -race ./live/ -count=1`
Expected: PASS.

- [ ] **Step 5: Add `./live/` to the `race` target** (`Makefile:259`, after `./jobs/`), run `make race`, expect PASS.

- [ ] **Step 6: Commit**

```bash
git add live/ Makefile
git commit -m "Add the live hub: per-topic versions captured at request start"
```

### Task 2: `live` stream handler

**Files:**
- Create: `live/stream.go`
- Test: `live/stream_test.go` (copy the `eventsServer` / `openStream` / `newSSEReader` pattern from `jobs/events_test.go:22-86`; a recorder cannot deliver flushes)

**Interfaces:**
- Consumes: Task 1's `Hub`, `parseTopic`, `parseToken`, `version`.
- Produces:
  - `func (h *Hub) Handler() http.Handler` — the GET stream; the app mounts it at `Config.Path` behind its own auth.
  - `func (h *Hub) Close()` — idempotent: refuses new streams with 503, makes every open stream return within `WriteDeadline`.
  - unexported test seams on `Hub`, as in `jobs.Handlers`: `lifetime`, `heartbeat`, `writeDeadline` copied from Config so tests can shrink them.

Behaviour: parse `boot` and every `t`; refuse with 400 if any token is malformed or `t` is absent; 403 if any topic is undeclared or its `Allow` returns false (checked for every topic before subscribing); 503 over `MaxStreams` or after `Close`. Then **subscribe first**, then send a `change` event for every requested topic whose server version is newer than the client's (all of them if `boot` differs), then relay live changes. A subscriber's wake channel has capacity 1 and `Notify` sends without blocking; the handler re-reads versions on wake, so a dropped wake loses nothing. Each write re-arms `SetWriteDeadline(now+writeDeadline)` (the `serve.go` "# Timeouts" recipe). Return on lifetime, client gone, or `Close`.

- [ ] **Step 1: Write the failing tests**

```go
func TestStreamCatchesUpThenRelaysLive(t *testing.T)         // client sends a@0 while a is 2: first event data "a@2"; then Notify("a") → "a@3"
func TestAQuietReconnectSendsNothing(t *testing.T)          // client sends current versions: no change event within 100ms, only pings
func TestADifferentBootChangesEveryTopic(t *testing.T)      // boot "000000000000": one event per requested topic
func TestLastEventIDOverridesTheURL(t *testing.T)           // URL a@0, Last-Event-ID "<boot> a@2" with a at 2: no event
func TestEventIDsCarryEverySubscribedVersion(t *testing.T)  // subscribe a,b; Notify("b"): id == "<boot> a@N b@M"
func TestStreamRefusesAForeignKey(t *testing.T)             // Allow returns key=="me"; t=notes/other@0 → 403, hub has 0 subscribers
func TestStreamRefusesUndeclaredAndMalformed(t *testing.T)  // t=zzz@0 → 403; t=a → 400; no t → 400
func TestStreamCapRefuses(t *testing.T)                     // MaxStreams 1: second stream → 503
func TestNotifyDoesNotBlockOnAStuckClient(t *testing.T)     // a client that never reads; 10k Notify calls return within 1s
func TestCloseEndsStreamsAndRefusesNew(t *testing.T)        // open 3 streams, Close(): all handlers return within writeDeadline+100ms; new stream → 503
func TestCloseEndsAStreamBlockedOnWrite(t *testing.T)       // a client with a full TCP window (stop reading, flood Notify): handler returns within writeDeadline+100ms of Close
func TestLifetimeEndsTheStream(t *testing.T)                // lifetime 50ms: handler returns, no error event
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./live/ -run 'TestStream|TestAQuiet|TestADifferent|TestLastEventID|TestEventIDs|TestNotifyDoesNot|TestClose|TestLifetime' -count=1`
Expected: FAIL.

- [ ] **Step 3: Implement `live/stream.go`.**

- [ ] **Step 4: Run them to see them pass, with `-race`**

Run: `CGO_ENABLED=1 go test -race ./live/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add live/
git commit -m "Serve the live stream: check every topic, catch up, then relay"
```

### Task 3: End live streams when shutdown starts

**Files:**
- Modify: `serve.go` (Options, beside `Background` at `:217-227`; `Serve` near `:369-386`; `Handler`'s close func `:433-441`)
- Test: `servelive_test.go` (copy `servebackground_test.go:18`'s shape)

**Interfaces:**
- Consumes: Task 2's `(*live.Hub).Close`.
- Produces: `Options.Live *live.Hub`. `Serve` calls `srv.RegisterOnShutdown(opts.Live.Close)` after `newServer`; `Handler`'s close func calls `opts.Live.Close()` before `Background.Stop()`. The field doc says a host embedding `Handler` calls `Live.Close()` before its own `Shutdown`, because the close func runs after the drain.

- [ ] **Step 1: Write the failing tests**

```go
func TestServeShutdownEndsLiveStreamsInsideTheDrain(t *testing.T) // newServer + RegisterOnShutdown path: an open stream, srv.Shutdown(ctx 10s) returns in < 2s
func TestHandlerCloseClosesTheHubBeforeBackground(t *testing.T)   // order recorded via channels, as in servebackground_test.go:18
```

Drive `Serve`'s path through the same helper `Serve` uses (`newServer` plus the registration), not a signal.

- [ ] **Step 2: Run them to see them fail**

Run: `go test . -run 'TestServeShutdownEndsLive|TestHandlerCloseClosesTheHub' -count=1`
Expected: FAIL, `Options.Live` undefined.

- [ ] **Step 3: Implement.** Also correct the stale sentence in `jobs/events.go`'s `Events` doc ("immediately when the request context does (client gone, server draining)"): Go's `Shutdown` does not cancel in-flight contexts. Say so; jobs streams still rely on their own TTL.

- [ ] **Step 4: Run them to see them pass**

Run: `go test . ./jobs/ -count=1 -run 'TestServeShutdownEndsLive|TestHandlerClose|TestEvents'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add serve.go servelive_test.go jobs/events.go
git commit -m "End live streams when shutdown starts, not after the drain"
```

### Task 4: Fragment requests on the server

**Files:**
- Create: `live/fragment.go`
- Modify: `ui/pages.go` (add `ExecuteRegions` beside `Execute` at `:244`)
- Test: `live/fragment_test.go`, `ui/pages_regions_test.go`

**Interfaces:**
- Produces:
  - `const Notice = "notice"`
  - `func Wanted(r *http.Request) (regions []string, ok bool)` — parses `Rastrillo-Fragment`; `ok` is false when absent, or when any name fails the region-name pattern (so `1` stays the jobs poll's).
  - `func Elsewhere(w http.ResponseWriter, r *http.Request) bool` — for a GET or HEAD carrying `Rastrillo-Page` that differs from `r.URL.Path`, writes `204` + `Rastrillo-Location: r.URL.RequestURI()` and returns true. Never fires for other methods.
  - `func NoStore(w http.ResponseWriter)` — sets `Cache-Control: no-store` and appends `Rastrillo-Fragment` to any existing `Vary`.
  - `func TakesFlash(r *http.Request) bool` — true for a full-page request, or a fragment request that names `notice`.
  - `func ExecuteRegions(w io.Writer, t *template.Template, names []string, data any) (rendered []string, err error)` — for each name, executes `region:<name>` if `t.Lookup` finds it, skips it otherwise; buffers and writes nothing on error. For apps with their own template map (Patrix).
  - `func Answer(w http.ResponseWriter, r *http.Request, rendered []string)` — sets `Rastrillo-Regions`; when `rendered` is empty on a GET, writes the 204 + `Rastrillo-Location` instead, and the caller writes no body.
  - `func (p *Pages) ExecuteRegions(w io.Writer, page string, names []string, data any) ([]string, error)` in `ui` — same contract, with `Execute`'s localizer check and page lookup.

The app's render order (documented in the package doc and used by Task 9): `Elsewhere` → (`NoStore`) → `Wanted` → `TakesFlash` decides `flash.Take` → `ExecuteRegions` or `Execute`. For a GET whose requested regions don't exist on this page, the renderer must look them up **before** taking the flash; `ExecuteRegions` with a dry lookup (`t.Lookup`) is how. Expose that as `func Has(t *template.Template, names []string) bool` and `func (p *Pages) HasRegions(page string, names []string) bool`.

- [ ] **Step 1: Write the failing tests**

```go
func TestWantedIgnoresTheJobsPollHeader(t *testing.T)       // "1" → ok false; "" → false; "notes-list notice" → [notes-list notice], true
func TestWantedRefusesBadNames(t *testing.T)                // "Notes", "a/b", "region:x" → ok false
func TestElsewhereOnlyForAGetFromAnotherPage(t *testing.T)  // GET /notes/1 with Rastrillo-Page "/" → 204, Location "/notes/1"; POST same → false; GET / with "/" → false
func TestNoStoreKeepsExistingVary(t *testing.T)             // Vary: Accept-Language stays, Rastrillo-Fragment appended once
func TestTakesFlash(t *testing.T)                           // full page true; "list" false; "list notice" true
func TestRegionsRenderOnlyDefinedRegionTemplates(t *testing.T) // template defines "region:list" and "secret": names [list secret] renders only list; rendered == [list]
func TestRegionsWriteNothingOnError(t *testing.T)           // a region that errors: writer untouched, err returned
func TestAnswerNavigatesWhenNothingRendered(t *testing.T)   // GET, rendered empty → 204 + Location; POST, rendered empty → Rastrillo-Regions "" and no 204
func TestPagesExecuteRegionsNeedsALocalizer(t *testing.T)   // ui: data without L → ErrNoLocalizer
func TestPagesExecuteRegionsRendersFromThePagesOwnSet(t *testing.T) // ui: page "index" defines region:list; page "show" does not → [] for show
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./live/ ./ui/ -run 'TestWanted|TestElsewhere|TestNoStore|TestTakesFlash|TestRegions|TestAnswer|TestPagesExecuteRegions' -count=1`
Expected: FAIL.

- [ ] **Step 3: Implement.**

- [ ] **Step 4: Run them to see them pass**

Run: same command. Expected: PASS. Then `go test ./ui/ -run '^TestPages' -count=1` stays PASS.

- [ ] **Step 5: Commit**

```bash
git add live/fragment.go live/fragment_test.go ui/pages.go ui/pages_regions_test.go
git commit -m "Render named regions for fragment requests, and navigate when there are none"
```

### Task 5: `ui/live.js` — the stream and region swaps

**Files:**
- Create: `ui/live.js`
- Modify: `ui/ui.go` (embed it; `func LiveJS() []byte` beside `ShimJS` at `:358`)
- Create: `ui/live_node.mjs` (node harness with a controllable `EventSource`, resolvable `fetch`, manual timers; copy `shim_node.mjs`'s `vm.runInNewContext` shape)
- Test: `ui/live_test.go` (contract + node tests), `ui/live_browser_test.go` (`//go:build browser`)

**Interfaces:**
- Consumes: the wire formats above; nothing from other JS files.
- Produces: `LiveJS() []byte`. Inert unless the page has `meta[name=rastrillo-live]` and at least one `[data-live]`.

Behaviour, each rule from spec §3:
- One `EventSource` per page for the union of topics, each at the **lowest** version any region holds; `boot` from any region.
- The shim owns retries: on `error`, `close()` the EventSource, then reopen after backoff 1s doubling to 60s.
- On `change`, fetch every region naming that topic, with `Rastrillo-Fragment: <id>`; never the notice.
- Ordering: each region has a counter of fetches started; a response applies only if it is the latest started for that region; at most one in flight and one queued.
- Immediately before applying: the region is still in the document and not being edited (contains `document.activeElement` that is a form control, or a form whose current values differ from their defaults). Being edited → mark stale, fetch on `focusout` leaving the region.
- Swap with `replaceWith` of the parsed element whose `id` matches. Focus: if the focused element's `id` exists in the new element, focus it; else if focus was inside the old region, give the new region `tabindex="-1"` and focus it.
- The notice region's contents are replaced in place; the element itself is never replaced.
- A failed fetch retries 3 times with backoff (1s, 2s, 4s), then shows the paused note.
- The paused note: when the stream has been down 10s, or a region's retries are exhausted, write `data-paused`'s text into the notice element. Clear it when the stream opens again.
- Hidden for 60s → close the stream; `visibilitychange` to visible → reopen (versions from regions).
- `pagehide` → close the stream, abandon fetches (ignore their responses). `pageshow` with `persisted` → reopen.
- Prerendered → start on `prerenderingchange`, as `rastrillo.js:348`.

- [ ] **Step 1: Write the failing contract and node tests**

```go
func TestLiveIsSmallAndSelfContained(t *testing.T)            // len(LiveJS()) <= 16*1024; starts "/*"; one IIFE; no eval(, new Function, http://, import 
func TestLiveIsInertWithoutTheMeta(t *testing.T)              // node: regions but no meta → 0 EventSources, 0 fetches
func TestLiveOpensOneStreamForManyRegions(t *testing.T)       // node: 3 regions, topics a,b,a → 1 EventSource, URL has t=a@<min>&t=b@<v>
func TestLiveUsesTheLowestVersionPerTopic(t *testing.T)       // node: regions a@3 and a@5 → t=a@3
func TestLiveOwnsReconnects(t *testing.T)                     // node: fire error → close() called, no second EventSource until the 1s timer runs
func TestLiveWaitsForAPrerenderedPage(t *testing.T)           // node: document.prerendering → 0 EventSources until prerenderingchange
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./ui/ -run 'TestLive' -count=1`
Expected: FAIL.

- [ ] **Step 3: Write the failing browser tests** (use `sizingMux` from `touch_browser_test.go:32`, adding `/live.js` and a fake stream endpoint that pushes events from a channel; allow expected statuses with `rig.Allow`)

```go
func TestALiveRegionSwapsOnAChangeEvent(t *testing.T)
func TestAStaleResponseNeverOverwritesANewerOne(t *testing.T)    // delay the first fetch's response; second applies; first is dropped
func TestARegionBeingEditedIsNotSwapped(t *testing.T)            // focus an input in the region, push change: DOM unchanged, data marks stale; blur → swapped
func TestSwapFocusFallsBackToTheRegion(t *testing.T)             // focused button's id absent from the new region → document.activeElement is the region, never body
func TestTheNoticeElementSurvivesASwap(t *testing.T)             // same node identity before and after (compare a JS-set property)
func TestThePausedNoteShowsWhenTheStreamStaysDown(t *testing.T)  // stream answers 503 for 11s → notice text == data-paused
func TestBackForwardRestoreCatchesUp(t *testing.T)               // navigate away and back (bfcache): stream reopens with the regions' versions
```

- [ ] **Step 4: Run them to see them fail**

Run: `go test -tags browser ./ui/ -run 'TestALiveRegion|TestAStale|TestARegionBeing|TestSwapFocus|TestTheNotice|TestThePaused|TestBackForward' -count=1`
Expected: FAIL.

- [ ] **Step 5: Implement `ui/live.js`.** Header comment in `rastrillo.js`'s style: what it answers, the vocabulary, that it is app-owned once vendored.

- [ ] **Step 6: Run all of Task 5's tests to see them pass**

Run: `go test ./ui/ -run TestLive -count=1 && go test -tags browser ./ui/ -run 'TestALiveRegion|TestAStale|TestARegionBeing|TestSwapFocus|TestTheNotice|TestThePaused|TestBackForward' -count=1`
Expected: PASS. If bfcache needs it, add the test name to `FULL_CHROMIUM` (`Makefile:333`).

- [ ] **Step 7: Commit**

```bash
git add ui/live.js ui/ui.go ui/live_node.mjs ui/live_test.go ui/live_browser_test.go Makefile
git commit -m "Add live.js: one stream per page, ordered region swaps that respect editing and focus"
```

### Task 6: Swapping forms, and the handover from `busy.js`

**Files:**
- Modify: `ui/live.js` (forms section)
- Modify: `ui/busy.js` (skip `data-live-form` beside the `data-busy="false"` check at `:101` and in the window listener at `:192-193`)
- Modify: `ui/shim_test.go` (`TestBusyContract`'s pinned counts, if the edit changes them — keep the meaning, update the number, say why in the test)
- Test: `ui/liveform_browser_test.go`

**Interfaces:**
- Consumes: Task 5's fetch/order/swap machinery (internal to `live.js`).
- Produces: the `data-live-form` vocabulary.

Behaviour, spec §4: a `submit` listener on `document` (capture) for `form[data-live-form]`. It submits natively (does nothing) when the form has a `target`, a cross-origin action, or method GET, or when `fetch` is missing. Otherwise: `preventDefault`; build `new FormData(form, e.submitter)`; honour `formaction`/`formmethod`/`formenctype`; send with `Rastrillo-Fragment: <own region> <targets…> notice` and `Rastrillo-Page: location.pathname`. Busy: set the submitter `aria-busy="true"`, then `disabled` in a `setTimeout` (busy.js's pattern), hold at least 650 ms, refuse a second submit while out. Response:
- `204` + local `Rastrillo-Location` → `location.assign`.
- `Rastrillo-Regions` non-empty → apply each region (supersedes in-flight stream fetches); focus the first `[aria-invalid="true"]` in the form's region if the status is 400 or 422, else apply Task 5's focus rule.
- A 2xx/4xx full document without `Rastrillo-Regions` → replace the document with it (`document.open(); document.write(html); document.close()`), as the browser would have shown it.
- Network error or a 5xx without regions → keep the form's values, write `data-unconfirmed`'s text into the notice, fetch the form's regions again. Never resubmit.

- [ ] **Step 1: Write the failing browser tests**

```go
func TestASwappingFormSwapsItsRegionAndNotice(t *testing.T)
func TestASwappingFormSendsItsSubmitter(t *testing.T)            // two buttons name=do value=a|b: server sees do=b
func TestValidationErrorsSwapInPlaceAndTakeFocus(t *testing.T)   // 422 with the form region; activeElement is the invalid field
func TestAnUnknownOutcomeKeepsInputAndNeverResubmits(t *testing.T) // server records the POST, then drops the connection: one POST recorded, input value intact, notice == data-unconfirmed
func TestASecondSubmitWhileOutIsRefused(t *testing.T)            // two clicks → one POST
func TestBusyLeavesSwappingFormsToLive(t *testing.T)            // both script orders (busy.js before and after live.js): button stays aria-busy for the whole request
func TestFormsLiveCannotSendSubmitNatively(t *testing.T)        // target="_blank" form: a normal navigation request, no Rastrillo-Fragment header
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test -tags browser ./ui/ -run 'TestASwappingForm|TestValidationErrors|TestAnUnknown|TestASecondSubmit|TestBusyLeaves|TestFormsLiveCannot' -count=1`
Expected: FAIL.

- [ ] **Step 3: Implement** the forms section of `live.js` and the `busy.js` skip.

- [ ] **Step 4: Run them, plus the existing busy and shim tests, to see them pass**

Run: `go test ./ui/ -run 'TestBusy|TestShim|TestLive' -count=1 && go test -tags browser ./ui/ -run 'TestASwappingForm|TestValidationErrors|TestAnUnknown|TestASecondSubmit|TestBusyLeaves|TestFormsLiveCannot|TestAPostItemsSpinner|TestBrowserFastClick' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add ui/live.js ui/busy.js ui/shim_test.go ui/liveform_browser_test.go
git commit -m "Let forms swap in place, and hand them from busy.js to live.js"
```

### Task 7: The two strings, reviewed and translated

**Files:**
- Modify: `locales/*.toml` (all 12: `en ga zh-Hans es hi pt-PT bn ru ja yue vi ar`)
- Modify: `basecatalog_test.go` (a `TestTheLiveStringsAreInEveryCatalog`, modelled on `TestTheShellAndRowMenuStringsAreInEveryCatalog` at `:114`)

**Interfaces:**
- Produces: keys `rastrillo.ui.live.paused` and `rastrillo.ui.live.unconfirmed`, read by templates and passed to `View.Head` (Task 1).

- [ ] **Step 1: Draft the English and run it through the `copy-review` skill before writing it anywhere.** Drafts: paused "Live updates have paused. Refresh the page to start them again."; unconfirmed "We couldn't confirm that change. Check the page before you try again." Apply Paul's edits verbatim.
- [ ] **Step 2: Write the failing parity test** naming both keys. Run `go test . -run TestTheLiveStrings -count=1`. Expected: FAIL.
- [ ] **Step 3: Add the approved English and the eleven translations** (translate the approved copy yourself; translations are not reviewed).
- [ ] **Step 4: Run** `go test . -run 'TestBaseCatalogs|TestTheLiveStrings' -count=1`. Expected: PASS.
- [ ] **Step 5: Commit**

```bash
git add locales/ basecatalog_test.go
git commit -m "Add the live pages' paused and unconfirmed notices in every language"
```

### Task 8: Deliver `live.js` to apps

**Files:**
- Modify: `ui/vendored.go` (`vendoredNames` at `:22`, `VendoredAssets` at `:42`: add `"live.js": LiveJS()`)
- Modify: `cmd/rastrillo/new.go` (the scaffolded layout gets `<script defer src="{{asset "static/live.js"}}"></script>` after `busy.js`; the generated `TestVendoredAssetsMatchTheLibrary` at `:1080` picks the file up through `vendoredNames`)
- Modify: `cmd/rastrillo/new_test.go:147-162` (assert the new file and script tag)
- Modify: `internal/designsystem/page.go:1715` only if a gallery preview uses `data-live`; otherwise leave it.
- Modify: `CHANGELOG.md` ("Added: live pages. Run `rastrillo doctor --fix` to vendor `live.js` and re-copy `busy.js`.")

**Interfaces:**
- Consumes: `ui.LiveJS()`.

- [ ] **Step 1: Write the failing assertions** in `new_test.go` (file exists, verbatim, script tag present, after busy.js). Run `go test ./cmd/rastrillo/ -run TestNew -count=1`. Expected: FAIL.
- [ ] **Step 2: Implement.**
- [ ] **Step 3: Run** `go test ./cmd/rastrillo/ ./ui/ -count=1 -run 'TestNew|TestVendored|TestDoctor'` and `make scaffold-smoke`. Expected: PASS.
- [ ] **Step 4: Commit**

```bash
git add ui/vendored.go cmd/rastrillo/ CHANGELOG.md
git commit -m "Vendor live.js into every app, inert until a page opts in"
```

### Task 9: Prove it in `examples/notes`

**Files:**
- Modify: `examples/notes/internal/notes/app.go` (construct the hub with topic `notes` keyed by owner; `Options.Live`; `hub.Middleware` in the router before handlers; mount `hub.Handler()` at `/live` inside the `sess.Require` group; serve `/static/live.js` from `ui.LiveJS()` beside `:257-265`)
- Modify: `examples/notes/internal/notes/render.go` (`renderStatus` follows Task 4's render order; `page` gains `Live live.View`)
- Modify: `examples/notes/internal/notes/handlers.go` (`hub.Notify("notes/"+uid)` after each committed create, update and delete; delete keeps its 303 to `/`)
- Modify: `templates/layout.html` (persistent `<div id="notice" role="status" class="flash …">`; `{{.Live.Head (T $L "rastrillo.ui.live.paused") (T $L "rastrillo.ui.live.unconfirmed")}}` in `<head>`; `<script defer src="/static/live.js">`)
- Modify: `templates/index.html` (`{{define "region:notes-list"}}` around the list, with `id="notes-list" {{.Live.Attrs …}}`; a per-row delete `<form method="post" action="/notes/{{.ID}}/delete" data-live-form>`)
- Test: `examples/notes/internal/notestest/live_test.go`

**Interfaces:**
- Consumes: everything above.
- The `notes` topic's `Allow`: `func(r *http.Request, key string) bool { uid, ok := sessions.UserID(r); return ok && strconv.FormatInt(uid, 10) == key }`.

- [ ] **Step 1: Write the failing tests** (httptest, as in `notestest/crud_test.go`; clients that don't follow redirects unless a test needs to)

```go
func TestTheListRegionRendersAloneOnAFragmentRequest(t *testing.T) // GET / with Rastrillo-Fragment notes-list: body has id="notes-list", no <html>; Rastrillo-Regions "notes-list"; Cache-Control no-store
func TestAStreamFetchDoesNotTakeTheFlash(t *testing.T)            // delete sets the flash; GET / with Rastrillo-Fragment notes-list: no clearing Set-Cookie; then a full GET shows "Note deleted."
func TestDeletingFromTheListSwapsIt(t *testing.T)                 // POST delete with headers, following the 303: one response with Rastrillo-Regions "notes-list notice", the note gone, the notice text present
func TestAFormWhoseRedirectGoesElsewhereNavigatesWithItsNotice(t *testing.T) // POST /notes with Rastrillo-Page "/" following the 303 to /notes/{id}: 204 + Rastrillo-Location; then GET it shows "Note created."
func TestAnotherOwnersNotesStreamIsRefused(t *testing.T)          // user A opens /live?t=notes/<B>@0 → 403
func TestANoteAddedElsewhereReachesTheStream(t *testing.T)        // user A's stream open; A creates a note in another client → change event "notes/<A>@1"
func TestScriptsOffStillWork(t *testing.T)                        // the CRUD tests in crud_test.go pass unchanged (run them; no edit)
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd examples/notes && go test ./... -run 'TestTheListRegion|TestAStreamFetch|TestDeletingFromTheList|TestAFormWhose|TestAnotherOwners|TestANoteAdded' -count=1`
Expected: FAIL.

- [ ] **Step 3: Implement.**

- [ ] **Step 4: Run the whole example to see it pass**

Run: `make example-notes`
Expected: PASS (build, vet, every test).

- [ ] **Step 5: Commit**

```bash
git add examples/notes/
git commit -m "Make the notes list live and deletable in place"
```

### Task 10: Document it

**Files:**
- Create: `skill/live.md` (under `skillTopicBudget` 9,000 bytes: the vocabulary, the render order, the wire formats an app touches, the CARLOS caveat)
- Modify: `SKILL.md` (one index line naming `skill/live.md`; 392 bytes of headroom under 8,000 — trim redundant prose elsewhere if needed, never a load-bearing fact)
- Create: `docs/site/live.md` (a reader's guide, written with the `humane-docs` skill; it is user-facing prose, so it goes through `copy-review`)
- Modify: `README.md` (one bullet beside the jobs bullet)
- Modify: `docs/superpowers/specs/2026-10-26-live-fragments-design.md` (Non-goals: "Declarative partial updates (`<template for>`, `streamHTMLUnsafe`): not yet across browsers; the fragment response is one element per region, so moving to them later changes only the response format.")

- [ ] **Step 1: Write them.**
- [ ] **Step 2: Run** `go test . -run 'TestSkill' -count=1` and the markup spelling test `go test . -run TestMarkupSpelling -count=1`. Expected: PASS.
- [ ] **Step 3: Commit**

```bash
git add skill/live.md SKILL.md docs/site/live.md README.md docs/superpowers/specs/
git commit -m "Document live pages"
```

### Task 11: Gate and review

- [ ] **Step 1: Run the whole gate once** — `make ci` (GOFLAGS=-mod=mod is in the Makefile). Expected: PASS. Run each new test against the code before its change (check out the commit before the task's implementation step) and record both runs for the branch description (CONTRIBUTING).
- [ ] **Step 2: Cross-provider review of the whole branch** with Astra (`codex exec -m gpt-6-astra -c model_reasoning_effort=high -s read-only`), against the spec and this plan. Post the prompt, the whole reply and what was done about each finding on the branch's review discussion. Stopping rule as for the spec: fix what leaks, loses an update or a write, keeps an instance awake, or breaks the scriptless path; record the rest.
- [ ] **Step 3: Update the branch description** (model and reasoning setting, the before/after test runs, the review link) and the handoff.
