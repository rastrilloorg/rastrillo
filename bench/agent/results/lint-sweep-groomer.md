# Lint sweep: what code-quality tools find in agent-written Rastrillo apps

Date 2026-11-25. Brief: groomer only. 18 apps: 12 kept runs (1, accept-1, cost-1, weight-1; carlos 1-3 each; scaffold from before the readable-code work, Rastrillo v0.30.0 bench builds) and 6 read-1 runs (current scaffold, v0.45.0 bench build; carlos 1-3 and carlos-gopls 1-3). Nothing in the agent-bench directories or any repository was modified; all tools ran on copies under ~/.cache/lint-sweep/apps.

## Setup and versions

- go1.26.10 (Makefiles pin go1.26.11, so I ran with GOTOOLCHAIN=local), GOFLAGS=-mod=mod GOPROXY=off, -buildvcs=false, build tags `browser,perf` (the apps' own staticcheck uses them), tests included.
- `go fix`: Go 1.26.10's cmd/fix, 21 analyzers. Counted per analyzer with `go vet -vettool=$GOROOT/pkg/tool/linux_amd64/fix -json`, plus `go fix -diff`.
- gofumpt v0.12.0 (`-l`, `-d`).
- golangci-lint v2.15.0 (built with go1.26.10), config in ~/.cache/lint-sweep/golangci.yml (copied at the bottom). `--fix` run on a second copy (~/.cache/lint-sweep/fixcopy), then `go vet` on it.
- deadcode: golang.org/x/tools v0.53.0, `deadcode -tags browser,perf -test=false ./cmd/...` (every app has one main package, cmd/groomer).
- All 18 apps built; all bench pseudo-versions were in the module cache. No app was "could not build".
- Scaffold baseline: a throwaway program (~/.cache/lint-sweep/scaf, copies of bench/agent internal/authored and internal/transcript) read each run's build transcript, found its `rastrillo new` flags, built `cmd/rastrillo` at the app's pseudo-version and scaffolded again into ~/.cache/lint-sweep/scaffold/<run>/groomer. Flags used per run: `--shell=topbar` and/or `--theme=day`, `--auth=password` for accept-1 and cost-1 (1, 2) and cost-1-3, `--scaffold=single` for read-1 gopls-2 and gopls-3, and none for weight-1 carlos-3 and 1/carlos-2. I gave each scaffold the app's go.mod and go.sum (the scaffold stamps a release version the offline cache lacks), then ran every tool on it too.
- "Agent code" means a finding whose line is in a file the scaffold does not write, or on a line a line-diff (difflib) shows the app changed relative to the scaffold's version of that file. "Scaffold" means the finding sits on a line identical to the fresh scaffold. A finding on a scaffold line that the agent never touched is counted as scaffold even when the agent file differs elsewhere. This is a heuristic; renumbered but unchanged scaffold lines still match because the diff is line-content based.
- The apps already run `go vet`, gofmt and staticcheck in `make ci`, so govet, staticcheck, unused and ineffassign were expected to be quiet. They were.

## Headline

- golangci-lint with the 31-linter set reported 642 findings across the 18 apps: 282 in agent code, 360 in scaffold-written lines. Per app: 26 to 43 findings, 5 to 36 of them in agent code.
- 13 of the 31 linters (plus govet) never fired at all: govet, unused, ineffassign, usestdlibvars, nilerr, sqlclosecheck, rowserrcheck, gochecknoinits, predeclared, nestif, dupl, godot, nakedret.
- By count the noise is huge and the signal is small. The findings that look real in agent code: ignored `Sweep` errors (errcheck, 8 sites in 4 apps), `err.(op.Conflict)` instead of errors.As (1), an unchecked `data["Booking"].(Booking)` (1), one ineffectual assignment, one cognitive-complexity 17 function, and a De Morgan quickfix. Everything else is test-helper style (noctx, bodyclose, unparam), intentional patterns (gosec), or naming/style (revive, errname, prealloc).
- `golangci-lint --fix` is dangerous here: misspell rewrote the correct word `signins` to `signings` 62 times (a rename across files), and the perfsprint and errorlint fixers dropped the `strconv` / `errors` import, so the fixed copy no longer compiled in 3 of 18 apps.
- `go fix` is clean and useful: 13 fixes in agent code across 8 of 18 apps (4 analyzers fire: slicescontains, mapsloop, rangeint, stringsbuilder), all with automatic fixes.
- gofumpt would reformat about 5,900 lines in agent code (3,193 + 913 + 1,007 changed lines, adds and removes counted) in 295 of the apps' non-generated files, almost all of it one rule: closing brace of a multi-line composite literal or call goes on its own line. The scaffold itself is not gofumpt-clean (6 files in 12 of 18 scaffolds, 159 lines in today's scaffold).
- deadcode found nothing in agent code. It found unreachable scaffold functions (tokens.go) in 14 of 18 apps, plus one scaffold `render` left dead by one agent.

## golangci-lint, per linter

Columns: apps in which the linter fired (of 18, agent or scaffold lines); apps with a hit in agent code; totals; agent vs scaffold split; the split by group as agent/scaffold; findings that disappear under `--fix`.

| linter | apps fired (of 18) | apps with agent-code hit | total | agent code | scaffold | kept (12) agent/scaf | read carlos (3) agent/scaf | read gopls (3) agent/scaf | repaired by --fix |
|---|---|---|---|---|---|---|---|---|---|
| bodyclose | 6 | 6 | 21 | 21 | 0 | 17/0 | 4/0 | 0/0 | 0 |
| contextcheck | 1 | 1 | 1 | 1 | 0 | 1/0 | 0/0 | 0/0 | 0 |
| errcheck | 18 | 17 | 236 | 70 | 166 | 56/112 | 6/27 | 8/27 | 0 |
| errname | 7 | 7 | 10 | 10 | 0 | 8/0 | 1/0 | 1/0 | 0 |
| errorlint | 1 | 1 | 1 | 1 | 0 | 0/0 | 0/0 | 1/0 | 1 |
| exhaustive | 7 | 7 | 9 | 9 | 0 | 8/0 | 1/0 | 0/0 | 0 |
| forcetypeassert | 1 | 1 | 1 | 1 | 0 | 1/0 | 0/0 | 0/0 | 0 |
| gocognit | 1 | 1 | 1 | 1 | 0 | 1/0 | 0/0 | 0/0 | 0 |
| gocritic | 10 | 0 | 10 | 0 | 10 | 0/10 | 0/0 | 0/0 | 0 |
| gosec | 18 | 12 | 61 | 24 | 37 | 14/25 | 5/6 | 5/6 | 0 |
| misspell | 6 | 6 | 62 | 32 | 30 | 0/0 | 17/15 | 15/15 | 62 |
| noctx | 18 | 15 | 134 | 52 | 82 | 33/60 | 10/11 | 9/11 | 0 |
| perfsprint | 7 | 2 | 10 | 4 | 6 | 1/0 | 0/3 | 3/3 | 10 |
| prealloc | 14 | 14 | 29 | 29 | 0 | 23/0 | 5/0 | 1/0 | 0 |
| revive | 18 | 9 | 47 | 18 | 29 | 12/8 | 5/10 | 1/11 | 0 |
| staticcheck | 1 | 1 | 1 | 1 | 0 | 1/0 | 0/0 | 0/0 | 1 |
| unparam | 6 | 6 | 7 | 7 | 0 | 6/0 | 1/0 | 0/0 | 0 |
| wastedassign | 1 | 1 | 1 | 1 | 0 | 1/0 | 0/0 | 0/0 | 0 |

Fired zero times on all 18 apps and 18 scaffolds (agent and scaffold code): govet, unused, ineffassign, usestdlibvars, nilerr, sqlclosecheck, rowserrcheck, gochecknoinits, predeclared, nestif, dupl, godot, nakedret

total findings 642 agent 282

The "kept" scaffold is an older one, so scaffold counts there come from a different scaffold than read-1's. read-1 carlos and carlos-gopls share one scaffold, apart from `--scaffold=single` in gopls-2 and gopls-3.

### Per run

| run | golangci total | in agent code | scaffold-only findings in a fresh scaffold of same flags |
|---|---|---|---|
| 1_groomer-carlos-1 | 38 | 36 | 18 |
| 1_groomer-carlos-2 | 27 | 16 | 18 |
| 1_groomer-carlos-3 | 28 | 11 | 18 |
| accept-1_groomer-carlos-1 | 37 | 11 | 28 |
| accept-1_groomer-carlos-2 | 33 | 7 | 28 |
| accept-1_groomer-carlos-3 | 39 | 14 | 28 |
| cost-1_groomer-carlos-1 | 31 | 5 | 28 |
| cost-1_groomer-carlos-2 | 40 | 15 | 28 |
| cost-1_groomer-carlos-3 | 36 | 11 | 28 |
| read-1_groomer-carlos-1 | 42 | 17 | 25 |
| read-1_groomer-carlos-2 | 42 | 18 | 25 |
| read-1_groomer-carlos-3 | 43 | 20 | 25 |
| read-1_groomer-carlos-gopls-1 | 37 | 13 | 25 |
| read-1_groomer-carlos-gopls-2 | 41 | 17 | 25 |
| read-1_groomer-carlos-gopls-3 | 39 | 14 | 25 |
| weight-1_groomer-carlos-1 | 26 | 25 | 18 |
| weight-1_groomer-carlos-2 | 32 | 15 | 18 |
| weight-1_groomer-carlos-3 | 31 | 17 | 18 |

### Examples and judgement (agent code unless stated)

**errcheck** (70 agent / 166 scaffold; 17 of 18 apps). Mixed.
- `internal/groomer/app.go:124` (1_groomer-carlos-1) `signinGuard.Sweep(time.Now())` "Error return value of `signinGuard.Sweep` is not checked". Real, minor: a failing session or guard sweep is silently dropped. 8 such sites in 4 apps (weight-1 carlos 1-3, 1 carlos-1).
- `internal/groomer/render.go:111` (1_groomer-carlos-1) `buffer.WriteTo(response)`. Noise: the scaffold writes the same line; a failed write to the client cannot be handled. 9 agent sites and the scaffold's own.
- Also `defer database.Close()` in main.go and `t.Cleanup(func() { database.Close() })` in tests (50 of the 70 agent hits are in tests, 23 are `database.Close`): style nit; the scaffold does the same.
- Judgement: only Sweep is real; `Close` and `WriteTo` need `exclude-functions`.

**noctx** (52 agent / 82 scaffold; 15 apps). Noise. All 52 agent hits are in `_test.go`: `internal/groomertest/harness_test.go:72 req := httptest.NewRequest(http.MethodGet, target, nil)` and `internal/groomertest/perf_test.go:116 page, err := client.Get(base + "/signin")`. No production code fired. Test-only style.

**bodyclose** (21 agent / 0; 6 apps). False positive. `internal/groomertest/booking_test.go:65 response := book(t, handler, slot, customer())` "response body must be closed": `book` returns an httptest recorder result, which needs no close. All in tests.

**gosec** (24 agent / 37 scaffold; 18 apps). Noise, nothing exploitable.
- `internal/groomer/auth.go:33 const wrongCredentials = "Wrong email or password."` G101 hardcoded credentials: false positive (a message).
- `internal/groomer/owner.go:78 http.Redirect(response, request, bookingPath(id), http.StatusSeeOther)` G710 open redirect: false positive, the path is built from an int id. 4 more of the same, plus scaffold `people.go:89`.
- G203 `template.URL("tel:" + kept)` / `template.HTML(` of a constant svg: intentional, a style flag. G101 on seeded demo passwords (`seed.go:18 DemoPassword = "wag-and-brush"`): intentional demo data. G115 `string(rune('a'+index))` once.

**misspell** (32 agent / 30 scaffold; 6 apps, all read-1). Pure false positive: every one of the 62 findings is `signins` "is a misspelling of `signings`" (`internal/groomer/app.go:97 signins, passwords, doors, err := newSignin(...)`, `internal/groomer/auth.go:41 signins, err := sessions.New(...)`). The read-1 scaffold's own api_test.go uses the same identifier, so it is a Rastrillo vocabulary clash. Its autofix is harmful.

**prealloc** (29 agent / 0; 14 apps). Style nit. `internal/groomer/hours.go:24 var fields []form.Field` "preallocating fields with capacity 3 * len(mondayFirst)"; `internal/groomer/views.go:71 var days, times []map[string]any`. Slices of 7 to 21 items per request; no measurable gain. 28 of 29 are in non-test code.

**revive** default rules (18 agent / 29 scaffold; 18 apps). Style, with a few useful items.
- `internal/groomer/owner_forms.go:26 func (shown serviceForm) show(` "receiver-naming: receiver name shown should be consistent with previous receiver name again for serviceForm". 7 sites in 2 apps where the agent named receivers after their role (`shown`, `again`, `current`) and mixed them. Real inconsistency, low harm, and possibly the agents following the readability spec rather than Go convention.
- `internal/groomer/agenda.go:40 ) ([]agendaDay, error)` "unexported-return: exported method Agenda returns unexported type". 3 sites (read-1 carlos-3). Real API-design smell in an app package; harmless in practice.
- package-comments (7 agent files that start a package comment-less file, plus scaffold api.go) and `exported ... should have comment` (1 agent, 3 scaffold): nit. In a single-package app these add noise.

**errname** (10 agent / 0; 7 apps). Style nit that the repo conventions may want. `internal/groomer/bookings.go:29 var slotTaken = op.Conflict{...}` "the sentinel error name `slotTaken` should conform to the `errXxx` format"; `internal/groomer/public.go:172 var securityCheck = op.Conflict{...}`. These are values of a Rastrillo type, used for flash messages, not wrapped sentinels. Not autofixable.

**exhaustive** (9 agent / 0; 7 apps). Noise. `internal/groomer/appointments.go:36 switch scope {` "missing cases in expression switch: scopeUpcoming" where the switch has a `default:` that handles upcoming deliberately; `internal/groomer/seed.go:88 switch weekday {` lists missing weekdays in a seed with a default. `default-signifies-exhaustive: true` would silence all nine.

**unparam** (7 agent / 0; 6 apps). Nit, all but one in test helpers. `internal/groomertest/booking_test.go:13 func firstFreeTime(t *testing.T, handler http.Handler, service string) string` "service always receives "/book/1""; `internal/groomertest/flows_test.go:48 cookie *http.Cookie` "cookie always receives nil". Mild: a helper with one caller value is normal in tests.

**perfsprint** (4 agent / 6 scaffold; 7 apps). Nit that autofixes, but see the import problem. `internal/groomertest/salon_test.go:325 number := fmt.Sprint(day)`; `internal/groomer/owner_views.go:119 "Value": fmt.Sprint(minutes)`. Style; a faster strconv.Itoa, no real gain. Scaffold: migrations_test.go:26 concat-loop.

**staticcheck** (1 agent). `internal/groomer/hours.go:96 if open && !(okOpens && okCloses) {` QF1001 "could apply De Morgan's law": a quickfix suggestion; style nit. (Real staticcheck checks stay silent because `make staticcheck` already gates them.)

**errorlint** (1). `internal/groomer/handlers.go:60 conflict, gone := err.(op.Conflict)` "type assertion on error will fail on wrapped errors. Use errors.As". Real in principle (op.Conflict could be wrapped); in this code the error comes straight from the store, so low risk. Autofix produced `var conflict op.Conflict; gone := errors.As(err, &conflict)` and dropped the `errors` import.

**forcetypeassert** (1). `internal/groomer/admin_bookings.go:125 ... cancelProblem(data["Booking"].(Booking))` "right hand must be only type assertion". Real: a map[string]any lookup that panics if the key is ever absent. One app.

**wastedassign** (1). `internal/groomer/admin_menu.go:104 echo, price := map[string]string{"Offered": "on"}, form.MoneyInput{}` "assigned to price, but reassigned without using the value". Real but trivial dead initialisation.

**gocognit** at 15 (1). `internal/groomer/seed.go:100 func (salon *Salon) seedComingBookings(` complexity 17. Real but trivial: seed code. Only one function in 18 apps goes over 15, so Rastrillo's own `make code` limits already hold the line.

**contextcheck** (1). `internal/groomer/public.go:108 app.now(), func(tx *gorm.DB) error {` "should pass the context parameter". False positive: the closure passes `request.Context()`.

**gocritic** defaults (0 agent / 10 scaffold; kept scaffold only). The kept scaffold's `cmd/groomer/main.go` `exitAfterDefer: os.Exit will exit, and `defer d.Close()` will not run` (10 sites in the 12 older scaffolds with it). Real but small; fixed in the read-1 scaffold.

### What `golangci-lint --fix` repaired

74 of the 642 findings disappear on the fixed copy: misspell 62, perfsprint 10, errorlint 1, staticcheck 1. The other 568 have no fixer (errcheck, noctx, gosec, revive, prealloc and the rest). Of those 74:
- misspell's 62 are the harmful `signins` to `signings` renames (also inside scaffold-written test files).
- perfsprint and errorlint fixes removed the call but left no import. The fixed copy failed `go vet` in 3 of 18 apps (cost-1 carlos-2: `undefined: strconv` at salon_test.go:325; read-1 gopls-1: `undefined: errors` at handlers.go:61; read-1 gopls-2: `undefined: strconv` at opening.go:85 and owner_views.go:119). It needs goimports after it.
- Only the one staticcheck De Morgan rewrite and the nine perfsprint calls that already had the import are safe as is.

## go fix (Go 1.26.10, `go fix -diff`, analyzers via vet json)

Agent code / scaffold counts. Every finding carries a suggested fix, so `go fix` repairs all 13 agent findings automatically (I ran `-diff`, not an in-place apply).

| analyzer | kept (12) agent/scaffold | read carlos (3) | read gopls (3) | apps with an agent-code hit |
|---|---|---|---|---|
| slicescontains | 3 / 0 | 1 / 0 | 3 / 0 | 7 |
| mapsloop | 3 / 0 | 0 | 0 | 2 |
| rangeint | 1 / 0 | 0 | 1 / 0 | 2 |
| stringsbuilder | 0 | 0 / 3 | 1 / 3 | 1 |
| every other analyzer (any, forvar, minmax, newexpr, omitzero, stringscut, stringscutprefix, stringsseq, ...) | 0 | 0 | 0 | 0 |

8 of 18 apps have at least one agent-code fix. Examples: `internal/groomertest/booking_test.go:39` (1_groomer-carlos-1) "Replace m[k]=v loop with maps.Copy"; `internal/groomer/services.go:49` (read-1 carlos-3) "Loop can be simplified using slices.Contains". Judgement: real modernisation; the code is safe; low volume because agents already write modern Go (any, min/max, range-over-int are not fired).
Scaffold finding: the read-1 scaffold's `internal/groomer/migrations_test.go:26` (`message += "  " + ...` in a loop) trips stringsbuilder in all 6 read-1 apps.

## gofumpt v0.12.0

| group | agent-code changed lines | scaffold-line changed lines | apps with a diff | files with an agent-code diff |
|---|---|---|---|---|
| kept (12) | 3,193 | 518 | 12/12 | 161 |
| read carlos (3) | 913 | 121 | 3/3 | 45 |
| read gopls (3) | 1,007 | 133 | 3/3 | 52 |

Lines are removed plus added lines in the diff. Across all 18 apps, 295 of about 564 non-generated Go files would change (generated gen/ and static/ excluded). Examples: `internal/groomer/bookings_test.go:44-45` `return &diary{database: database.G, hours: hours,\n now: func() time.Time { return now }}` becomes a brace-on-its-own-line literal; `internal/groomer/owner_forms.go:158`, same shape. In a sample of the hunks, nearly all are one rule (the closing `}` or `)` of a multi-line literal or call moves to its own line). Judgement: a real house style, not a defect. It will repeatedly rewrite the hanging-brace style the readable-code spec and golines produce, so adopting it means deciding the style once and also reformatting the scaffold. I did not test whether golines undoes gofumpt's output.
Scaffold: not gofumpt-clean from accept-1 on: 6 files (api.go, api_test.go, app.go, perf_test.go, render.go, routes.go), 159 changed lines in today's scaffold and 468 over 6 older ones. The pre-accept-1 scaffold (kept runs 1 and weight-1) was clean.

## deadcode

Agent code: 0 unreachable functions in 18 apps. Scaffold-written functions that nothing calls:
- read-1 scaffold (6 of 6 scaffolds): `internal/groomer/tokens.go` `NewTokenPage`, `TokenPage.Mount`, `renderTokens`. All 3 read-1 carlos apps and 2 of 3 gopls apps still carry them.
- kept scaffold (12 of 12 scaffolds; 9 of 12 apps still carry it): `tokens.go` `Tokens`, `renderTokens`.
- 1_groomer-carlos-3: `internal/groomer/render.go:73 render`, scaffold's helper, left dead after the agent stopped calling it.
Judgement: no signal about agent quality, one real scaffold smell. Running with `-test=false` means test-only helpers count as dead; it was only used on `./cmd/...`.

## Findings in the scaffold itself (Rastrillo bugs to fix regardless)

Each scaffold was built from the run's own flags at the run's Rastrillo version. Totals under the golangci config: 18 findings in the oldest scaffolds (1 and weight-1 runs), 28 in accept-1 and cost-1 (`--auth=password`), 25 in today's (read-1) scaffold. Today's scaffold, distinct locations (identical in all six read-1 scaffolds):

1. Not gofumpt-clean: api.go, api_test.go, app.go, perf_test.go, render.go, routes.go (159 changed lines). The accept-1 and cost-1 scaffolds were also unclean (6 other files, 468 lines over 6 scaffolds); the oldest ones were clean.
2. errcheck (9): `cmd/groomer/main.go:30 defer database.Close()`; tests `api_test.go:120`, `browser_test.go:32`, `harness_test.go:44`, `perf_test.go:29,43,65,72,75`. (Older scaffolds also had `render.go` `buf.WriteTo` and `main.go` `d.Close`, 21 distinct errcheck sites.)
3. noctx (4): `httptest.NewRequest` at api_test.go:101, harness_test.go:59,71, index_test.go:76. (Older scaffolds: 10, including `client.Get` and `http.NewRequest` in signin_test.go.)
4. misspell (5): `signins` in api_test.go:87,88,92,125,130. The scaffold's own identifier trips the dictionary.
5. revive (4): `internal/groomer/api.go:1` no package comment; `app.go:22` exported `App` without comment; `tokens.go:23,36` exported `NewTokenPage` and `TokenPage.Mount` without comments.
6. gosec (2): `api_test.go:105` G124 cookie without Secure/HttpOnly/SameSite; `vendored_test.go:37` G304 file inclusion. (Older scaffolds also `people.go:89` G710 open redirect.) Both are false positives in context, but every app inherits them.
7. perfsprint and go fix stringsbuilder: `internal/groomer/migrations_test.go:26`, `message +=` in a loop.
8. deadcode: `NewTokenPage`, `TokenPage.Mount`, `renderTokens` in tokens.go are unreachable from main in every read-1 scaffold (older: `Tokens`, `renderTokens`).
9. Older scaffolds only (fixed since): gocritic exitAfterDefer in `cmd/groomer/main.go` (`os.Exit` after `defer d.Close()`), revive unused parameter `ctx` at seed.go:18, unparam `getAs` target in signin_test.go:62.

Any gate that fails on these must be made green in the scaffold first, or every new app fails on day one.

## Recommendation

**Tier 1, autofix in the agent's loop** (apply, then re-run `make ci`)
- `go fix ./...`: 13 safe fixes in 8 of 18 apps, nothing harmful, fast. The only data-backed autofix.
- `goimports` (or gofmt) after any fixer, because the two golangci fixers that matter dropped imports in 3 of 18 apps.
- gofumpt only if the house style is changed to match: it would touch about half the Go files, mostly one brace rule that conflicts with the current hanging style; the scaffold would need reformatting first.
- Not golangci `--fix`: 62 of its 74 repairs are the misspell `signins` rename.

**Tier 2, fail `make ci`** (small, quiet, real findings; configure so the scaffold passes)
- errcheck, with `exclude-functions` for `Close` and `bytes.Buffer.WriteTo`/`http.ResponseWriter` writes: it caught the ignored `Sweep` errors, the only repeating real defect (7 sites, 4 apps).
- forcetypeassert, errorlint, wastedassign: one real hit each in 18 apps, no false positives; cost nothing.
- gocognit at 15 is optional: one hit in 18 apps; `make code` already enforces limits, so this adds little.
- deadcode: not for apps (zero agent hits); run it in Rastrillo's own scaffold check so `tokens.go` is not shipped dead.

**Tier 3, not worth it**
- misspell (all 62 false, rename is harmful), noctx (all tests), bodyclose (all false), gosec (nothing exploitable, 61 noise), exhaustive (all 9 have a `default`), contextcheck (the one hit is false), unparam (test helpers), prealloc (28 nits on tiny slices), perfsprint (nit, breaks imports), errname, revive defaults (package comments and receiver names; style), gocritic (kept scaffold only).
- govet, staticcheck, unused, ineffassign: already in `make ci` or silent.
- Silent on all 36 runs and scaffolds (usestdlibvars, nilerr, sqlclosecheck, rowserrcheck, gochecknoinits, predeclared, nestif, dupl, godot, nakedret): no evidence of value; skip until an app trips them.
- Mutation testing: not run, as instructed.

## golangci config used (also at ~/.cache/lint-sweep/golangci.yml)

```yaml
version: "2"
run:
  timeout: 10m
  tests: true
  build-tags: [browser, perf]
output:
  formats:
    json:
      path: stdout
issues:
  max-issues-per-linter: 0
  max-same-issues: 0
linters:
  default: none
  enable:
    - staticcheck
    - govet
    - errcheck
    - unused
    - ineffassign
    - usestdlibvars
    - errorlint
    - nilerr
    - noctx
    - bodyclose
    - sqlclosecheck
    - rowserrcheck
    - gocritic
    - revive
    - unparam
    - gochecknoinits
    - forcetypeassert
    - predeclared
    - gocognit
    - nestif
    - dupl
    - gosec
    - misspell
    - godot
    - prealloc
    - perfsprint
    - contextcheck
    - wastedassign
    - nakedret
    - errname
    - exhaustive
  settings:
    gocognit:
      min-complexity: 15
    nestif:
      min-complexity: 4
  exclusions:
    generated: lax
    presets: []
    paths:
      - gen/
      - vendor/
      - static/
formatters:
  enable: []
  exclusions:
    generated: lax
    paths:
      - gen/
      - vendor/
      - static/
```
