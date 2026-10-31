# Agent benchmark Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `bench/agent/` runs app briefs through a clean `claude -p`, on
Rastrillo and on an unguided control, and commits each round's build
cost, first-load weight and quality.

**Architecture:** A separate Go module, `bench/agent`, is wired into
`make ci` the way `money` is. Pure packages parse transcripts, classify
stages, load briefs and aggregate rounds, and they're unit-tested with
small synthetic fixtures. Effectful packages run `claude`, serve an
unreleased Rastrillo, drive Chromium and run Lighthouse. Those are kept
thin and proven by one real smoke run at the end. A single command,
`bench`, has subcommands `run`, `poll`, `report`, `import` and
`cleanup`.

**Tech Stack:** Go (the root's `go` directive), chromedp (the root's
version), golang.org/x/mod/zip (the root's version), axe-core from
`ui/testdata/axe/axe.min.js`, and Lighthouse from npm at run time only.

**Spec:** `docs/superpowers/specs/2026-10-30-agent-benchmark-design.md`

## Global Constraints

- Module path `amadan.net/rastrillo/rastrillo/bench/agent`. It has its own
  `go.mod` and no `replace` back to the root, because it imports nothing
  from Rastrillo.
- Committed per round: `bench/agent/results/<round>.json` and
  `<round>.md`. Transcripts, apps and screenshots go under
  `~/.cache/agent-bench/<round>/` and are never committed.
- Three runs per arm per brief. A round reports the median and the range
  (min, max).
- Both arms: the same model, flags, settings file and environment notes.
  Both deploy, and both get the same follow-up text.
- Clean config: a fresh `CLAUDE_CONFIG_DIR` per run, built from the
  committed `bench/agent/settings.json`. It has no plugins, no hooks, no
  MCP, the default output style and no CLAUDE.md.
- Auth: `CLAUDE_CODE_OAUTH_TOKEN` from the operator's environment, passed
  through to `claude` and nowhere else. It's never written to disk by the
  bench. The operator's `~/.claude` is never read.
- The child's environment is built from an allowlist, not inherited.
- Servers bind to 127.0.0.1, or to the machine's Tailscale IP when a
  sandboxed agent must reach them. Never `0.0.0.0`.
- Deletion of deployed apps is never automated. `bench cleanup` prints
  the commands.
- Brief domain words never appear in `SKILL.md`, `skill/`, or what
  `rastrillo new` writes.
- Repo rules: code reads like a sentence (spelled-out names, why-only
  comments, 80 columns), small atomic commits, and `make ci` green on the
  final commit.

## Review Focus

1. **Stream-json repeats a message.** Each content block arrives as its
   own `assistant` event with the same `message.id` and the same
   `usage`. Count messages and usage once per id, and tools once per
   block. Pinned in Task 1.
2. **Config leak.** If the run's `init` record lists any plugin, or an
   output style other than the default, the round has stopped being
   clean. The runner treats it as a harness failure and aborts the round
   before the next run spends money. Pinned in Task 6.
3. **The `claude` version changes mid-round** because the CLI updated
   itself. Each run's `init.claude_code_version` must equal the round's
   first; on a mismatch the runner aborts the round. Pinned in Task 6.
4. **Bad page mapping.** The agent's mapping JSON is malformed, or a URL
   404s or redirects off-host. Each is that role's agent failure,
   recorded and reported, and never a crash or a silently dropped page.
   Pinned in Task 5.
5. **Operator secrets reach the agent.** Any variable outside the
   allowlist (for example `AIKIDO_API_KEY`) appears in the child's
   environment. Pinned in Task 4.

---

### Task 1: Module, transcript parser and CI wiring

**Files:**
- Create: `bench/agent/go.mod`, `bench/agent/internal/transcript/transcript.go`
- Test: `bench/agent/internal/transcript/transcript_test.go`, `bench/agent/internal/transcript/testdata/run.jsonl`
- Modify: `Makefile` (a `bench` target, in `SUITE`, staticcheck, govulncheck and budget-size lines mirroring `money`)
- Rename: `.amadan/ci.d/50-suite@34` → `50-suite@35`

**Interfaces:**
- Produces:
  ```go
  type Usage struct{ Input, Output, CacheRead, CacheWrite int }
  type Tool struct {
      Name      string
      Input     map[string]any
      ResultLen int // characters of the matching tool_result
  }
  type Message struct {
      ID      string
      Context int // input + cache_read + cache_creation at this message
      Output  int
      Tools   []Tool
      Text    string
  }
  type Init struct {
      Version, Model, OutputStyle string
      Plugins                     []string
  }
  type Result struct {
      NumTurns   int
      CostUSD    float64 // claude's own total_cost_usd
      DurationMS int64
      IsError    bool
      Subtype    string
      APIError   string
      Usage      Usage
  }
  type Transcript struct {
      Init     Init
      Messages []Message
      Result   *Result // nil when the run never finished
  }
  func Parse(r io.Reader) (Transcript, error)
  ```

- [ ] **Step 1:** Write `testdata/run.jsonl` by hand, about 12 lines and
  no real paths. It has: an `init` with `claude_code_version` "9.9.9",
  `plugins: []` and `output_style` "default"; message `m1` split over two
  `assistant` events (a text block and a Bash `tool_use` `t1`, same
  usage: cache_read 1000, cache_creation 200, input 3, output 50); a
  `user` `tool_result` for `t1` of 40 characters; message `m2` with two
  `tool_use` blocks in one event; and a `result` with `num_turns` 2,
  `total_cost_usd` 0.5 and `is_error` false.
- [ ] **Step 2:** Write `TestParseCountsEachMessageOnce`, asserting:
  `len(Messages)==2`; `Messages[0].Context==1203`; `Messages[0].Text`
  is non-empty; `Messages[0].Tools[0].ResultLen==40`;
  `len(Messages[1].Tools)==2`; `Result.CostUSD==0.5`;
  `Init.Version=="9.9.9"`. Then write `TestParseWithoutResult`: the
  same file with its last line cut gives `Result==nil` and no error.
- [ ] **Step 3:** Run `cd bench/agent && go test ./internal/transcript/`
  and expect FAIL (undefined: Parse).
- [ ] **Step 4:** Implement `Parse`. Merge events by `message.id`. Take
  usage from the first event of an id. Skip lines that aren't JSON
  rather than failing, because a killed run can end mid-line.
- [ ] **Step 5:** Run the tests again and expect PASS.
- [ ] **Step 6:** Add the `bench` Makefile target
  (`cd bench/agent && go build ./... && go vet ./... && go test ./... -count=1`).
  Add it to `SUITE` after `money`, and add `bench/agent` lines to
  `staticcheck`, `govulncheck` and the budget-size loop, as `money` has.
  Then rename the `ci.d` step to `@35`.
- [ ] **Step 7:** Run `go test -run TestSuite -count=1 .` (the
  `amadan_ci_test.go` check) and `make bench`, and expect both PASS.
- [ ] **Step 8:** Commit: "bench/agent: a transcript parser that counts each message once".

### Task 2: Stage classifier

**Files:**
- Create: `bench/agent/internal/classify/classify.go`
- Test: `bench/agent/internal/classify/classify_test.go`

**Interfaces:**
- Consumes: `transcript.Message`, `transcript.Transcript`
- Produces:
  ```go
  type Stage string // values below
  type Where struct {
      AppDir    string   // the run's app directory
      LiveHosts []string // hosts the mapping step reported
  }
  type Tally struct{ Messages, ToolCalls, CacheRead int }
  func Of(m transcript.Message, w Where) Stage
  func Summarise(t transcript.Transcript, w Where) map[Stage]Tally
  func FirstWriteContext(t transcript.Transcript, w Where) int // 0 if none
  ```

The first tool call of a message decides its stage, and a text-only
message is `other`. Rules apply in this order, and the first match wins:

| Stage | Matches (tool name, or the command / path / URL in its input) |
|---|---|
| `screenshots` | `--screenshot`, `headless`, `chrome-headless`; or Read of a `.png` |
| `live-check` | a URL whose host is in `LiveHosts` |
| `deploy` | `\b(carlos|flyctl|fly|railway|render|vercel|netlify|wrangler|gcloud|heroku)\s`, `podman|docker (build|push)` |
| `gates` | `make ci`, `staticcheck`, `govulncheck`, `gitleaks`, `lighthouse`, `axe` |
| `framework-read` | a path containing `/pkg/mod/`, or a fetch of `SKILL.md`, `rastrillo.org`, `carlosframework`, `raw.githubusercontent.com` |
| `scaffold` | `rastrillo new`, `npm init`, `npm install`, `go mod init`, `go get`, `go install` |
| `build-test` | `go test`, `go build`, `go vet`, `gofmt`, `npm test`, `node --test` |
| `write` | Write or Edit; or Bash with `cat >`, `sed -i`, `python3 - <<` |
| `app-read` | Read, Grep, Glob; or Bash starting with `cat`, `sed -n`, `grep`, `ls`, `find`, `head`, `tail` |
| `other` | anything else |

- [ ] **Step 1:** Write the table-driven test `TestOfFollowsRuleOrder`,
  with one row per stage above. Include these rows:
  `curl -s https://groomer-r1-1.bdf.oncarlos.com/` with `LiveHosts`
  holding that host gives `live-check`; `cat ~/go/pkg/mod/x/sessions.go`
  gives `framework-read`; Write gives `write`;
  `carlos deploy --app x` gives `deploy`; Read of `shot.png` gives
  `screenshots`.
- [ ] **Step 2:** Write `TestFirstWriteContext`. Three messages:
  framework-read at 50k, write at 60k, write at 70k. Expect 60000.
- [ ] **Step 3:** Run them and expect FAIL. Implement. Run again and
  expect PASS.
- [ ] **Step 4:** Commit: "bench/agent: classify each message by stage".

### Task 3: Briefs, and keeping their words out of the framework

**Files:**
- Create: `bench/agent/briefs/{groomer,lending,noticeboard,smoke}/{set.txt,brief.txt,carlos.txt,control.txt,followup.txt,pages.txt,words.txt}`, `bench/agent/env.txt`, `bench/agent/mapping.txt`, `bench/agent/internal/brief/brief.go`
- Test: `bench/agent/internal/brief/brief_test.go`, `cmd/rastrillo/benchwords_test.go`

**Interfaces:**
- Produces:
  ```go
  type Arm string
  const Carlos, Control Arm = "carlos", "control"
  type Page struct {
      Role, Description string
      SignedIn          bool
  }
  type Brief struct {
      Name, Set               string // Set: "dev", "heldout", "smoke"
      Text, Followup          string
      Framing                 map[Arm]string
      Pages                   []Page
      Words                   []string
  }
  type Vars struct{ App, Platform, SkillURL, DeployAccount string }
  func Load(dir string) (Brief, error)
  func LoadAll(root string) ([]Brief, error) // sorted by Name
  func (b Brief) Prompt(a Arm, v Vars) (string, error) // Text + "\n\n" + framing, text/template
  func Env(root string, v Vars) (string, error)         // env.txt, templated
  func Mapping(root string, b Brief) (string, error)    // mapping.txt with the roles listed
  ```

**Content:**
- `groomer/brief.txt` is the 2026-10-30 prompt1 with the framework and
  deploy sentences removed.
- `groomer/carlos.txt`: "Use CARLOS principles and Rastrillo. Read
  https://github.com/carlosframework/skills/blob/main/skills/getting-started/SKILL.md
  and {{.SkillURL}} first, and use the installation commands in
  Rastrillo's SKILL.md. Build, test and deploy the app to carloku.com.
  I'm already signed in: use the demo account and call the app
  {{.App}}."
- `groomer/control.txt`: "Build, test and deploy the app to
  {{.Platform}}. I'm already signed in: use the demo account and call
  the app {{.App}}."
- Both framings end with the original's "Work without asking me
  questions…" paragraph.
- `followup.txt` is prompt2 ending "Deploy it again when you're done."
- `env.txt` is the 2026-10-30 system-append, with the deploy line as
  "- Deploy only to {{.DeployAccount}}. Do not touch any other account
  or any other app."
- `pages.txt` holds lines of the form `role | signed-in yes/no |
  description`. For `groomer`: home, book (the public booking form),
  signin, owner (the owner's bookings list, signed in).
- `lending` and `noticeboard` follow the spec's shape table, at the same
  length as `groomer`. `lending`'s roles are home, signin, borrow
  (signed in) and overdue (admin, signed in). `noticeboard`'s are home,
  search, listing and contact.
- `smoke` is "A one-page site that says hello and shows today's date",
  with the single role home. It has no sign-in, so its mapping's
  `signin` may be empty. It exists to prove the plumbing for under $1,
  and `report` excludes it.
- `words.txt`: groomer gets groom, dog, salon, pet, puppy. lending gets
  borrow, lend, equipment, overdue, makerspace. noticeboard gets notice,
  noticeboard, listing-specific nouns from its brief. Generic words
  (sign in, admin, search, booking, form) never go on these lists.
- `mapping.txt` asks for JSON only:
  `{"pages":{"<role>":"<absolute live URL>",...},"signin":{"url":"","username":"","password":"","username_selector":"","password_selector":"","submit_selector":""}}`.

- [ ] **Step 1:** Write `TestLoadAllReadsEveryBrief`: four briefs; the
  sets are dev, heldout, heldout, smoke; `groomer` has four pages with
  owner signed in. Write `TestPromptFillsVarsAndKeepsArmsApart`: the
  carlos prompt contains SkillURL and App and not "{{"; the control
  prompt contains Platform and contains no "Rastrillo", "CARLOS" or
  "carlos". Run both and expect FAIL.
- [ ] **Step 2:** Write the brief files and implement `brief`. Run and
  expect PASS.
- [ ] **Step 3:** Write `TestFrameworkCarriesNoBenchmarkWords` in
  `cmd/rastrillo`. It reads every `bench/agent/briefs/*/words.txt`, then
  scaffolds an app into `t.TempDir()` the same way
  `new_scaffold_test.go` does. It scans those files, plus `SKILL.md` and
  `skill/**`, for each word: case-insensitive, word boundary, plural
  `s?`. It fails naming the file, the line and the word.
- [ ] **Step 4:** Run `go test ./cmd/rastrillo -run TestFrameworkCarriesNoBenchmarkWords`.
  If it fails on existing text, check each hit. A genuine brief word in
  the framework gets a word swap in the framework (a separate commit,
  named in the branch description). A generic word gets taken off the
  list.
- [ ] **Step 5:** Commit: "bench/agent: three briefs and a smoke brief; keep their words out of the framework".

### Task 4: Clean config and child environment

**Files:**
- Create: `bench/agent/settings.json`, `bench/agent/internal/cleanenv/cleanenv.go`
- Test: `bench/agent/internal/cleanenv/cleanenv_test.go`

**Interfaces:**
- Produces:
  ```go
  // Config writes dir/settings.json: the committed settings with
  // extraHosts appended to sandbox.network.allowedDomains.
  func Config(benchRoot, dir string, extraHosts []string) (settingsPath string, err error)
  // Env is the child's whole environment.
  func Env(parent []string, configDir string, extra map[string]string) ([]string, error)
  ```

`settings.json` contains `disableAllHooks: true`, no `enabledPlugins`,
no `outputStyle`, `sandbox.enabled: true`, and
`sandbox.network.allowedDomains`. Those domains are the Anthropic API
hosts, proxy.golang.org, sum.golang.org, storage.googleapis.com,
github.com, *.github.com, raw.githubusercontent.com,
*.githubusercontent.com, registry.npmjs.org, rastrillo.org, amadan.net,
carloku.com, *.carloku.com and *.oncarlos.com. The control platform's
hosts are added per round through `extraHosts`. The `credentials.files`
denies are copied from the operator's current list (paths only).

The `Env` allowlist from the parent: `HOME USER LOGNAME PATH LANG LC_ALL
TERM TMPDIR SHELL`, plus `CLAUDE_CODE_OAUTH_TOKEN`. Then it sets
`CLAUDE_CONFIG_DIR=configDir`, then adds `extra`. It returns an error if
the token is absent or empty.

- [ ] **Step 1:** Write `TestEnvKeepsOnlyTheAllowlist`. The parent holds
  `AIKIDO_API_KEY=x`, `GOFLAGS=-mod=mod`, `CLAUDE_CODE_OAUTH_TOKEN=t`
  and `HOME=/h`. The result has HOME, the token and CLAUDE_CONFIG_DIR,
  and has neither AIKIDO_API_KEY nor GOFLAGS. Write
  `TestEnvRefusesWithoutToken`. Write `TestConfigAddsHostsAndNoPlugins`:
  the written JSON has no `enabledPlugins` key, has the extra host, and
  has `disableAllHooks` true. Run them and expect FAIL.
- [ ] **Step 2:** Implement both functions. Run and expect PASS.
- [ ] **Step 3:** Commit: "bench/agent: a clean config and an allowlisted environment for each run".

### Task 5: Measurement: weight, axe, headers, sign-in, Lighthouse

**Files:**
- Create: `bench/agent/internal/measure/{weight.go,axe.go,headers.go,mapping.go,lighthouse.go,browser.go}`
- Test: `bench/agent/internal/measure/{weight,axe,headers,mapping,lighthouse}_test.go`, `testdata/lighthouse.json` (trimmed)

**Interfaces:**
- Produces:
  ```go
  type Kind string // "html","css","js","font","image","other"
  type Weight struct{ Wire, Decoded map[Kind]int }
  type Mapping struct {
      Pages  map[string]string
      SignIn struct{ URL, Username, Password, UsernameSelector, PasswordSelector, SubmitSelector string }
  }
  type Page struct {
      Role, URL  string
      Failure    string // "" when measured
      Weight     Weight
      Axe        map[string]int // impact -> violations
      Lighthouse map[string]int // category -> score 0..100; nil for signed-in pages
  }
  type Measurement struct {
      Pages      []Page
      Headers    map[string]string // header -> value, absent ones omitted
      DeployOK   bool              // home 200 over HTTPS and sign-in reached
      Failures   []string
  }
  func ParseMapping(raw string, roles []string) (Mapping, []string /*failures*/)
  func Tally(events []NetworkEvent) Weight
  func SecurityHeaders(h http.Header) map[string]string
  func AxeCounts(axeJSON []byte) (map[string]int, error)
  func LighthouseScores(reportJSON []byte) (map[string]int, error)
  func Measure(ctx context.Context, chrome, lighthouseBin, shotsDir string, m Mapping, signedIn map[string]bool, axeJS []byte) Measurement
  ```
  `NetworkEvent{RequestID string; Type string /* CDP resource type */; Encoded, Decoded int; Host string}`

Behaviour that tests don't pin, decided here:
- Each page loads in a fresh browser context, so first-load is cold.
- A signed-in page signs in first, in the same context, using the
  mapping's selectors, then clears the cache but keeps cookies before
  loading.
- A page counts as loaded at the load event plus 1 s of network quiet,
  with a 30 s cap.
- CDP types map to kinds: Document→html, Stylesheet→css, Script→js,
  Font→font, Image→image, everything else→other.
- Redirect responses count toward the page's weight.
- Lighthouse runs on public pages only, as
  `lighthouse <url> --output=json --quiet --only-categories=performance,accessibility,best-practices,seo --chrome-flags=--headless=new`.
- Headers checked: `content-security-policy`,
  `strict-transport-security`, `x-content-type-options`,
  `referrer-policy`, `x-frame-options`, `permissions-policy`, and
  `frame-ancestors` inside the CSP.

- [ ] **Step 1:** Write the pure tests:
  - `TestTallySplitsByKind`: a Document of 5/20, a Script of 3/9 and a
    Stylesheet of 4/16 give Wire html 5, js 3, css 4 and Decoded
    20/9/16.
  - `TestParseMappingReportsEachBadRole`: the input is malformed JSON,
    or a missing role, or a relative URL, or a non-https URL. Each
    yields a failure string naming the role, and no panic.
  - `TestSecurityHeadersFindsFrameAncestorsInCSP`.
  - `TestAxeCountsByImpact`: two serious and one minor give
    `{"serious":2,"minor":1}`.
  - `TestLighthouseScoresRounds`: 0.934 gives 93.

  Run them and expect FAIL.
- [ ] **Step 2:** Implement the pure functions. Run and expect PASS.
- [ ] **Step 3:** Implement `Measure` in `browser.go` with chromedp.
  - It saves a full-page screenshot of each page, at 1280 and 390 px
    wide, to `Dir/rec/shots/<phase>-<role>-<width>.png`, for a person to
    look at.
  - It sends an off-host redirect, a non-200 home page or a sign-in
    that lands back on a password field to `Failures`. None of them
    returns early from the remaining pages.
  - It gets no unit test. Task 8's smoke run proves it.
- [ ] **Step 4:** Commit: "bench/agent: measure first-load weight, axe, headers and Lighthouse on a live app".

### Task 6: The Rastrillo under test, and the runner

**Files:**
- Create: `bench/agent/internal/source/{proxy.go,skillserver.go}`, `bench/agent/internal/runner/runner.go`
- Test: `bench/agent/internal/source/proxy_test.go`, `bench/agent/internal/runner/runner_test.go`

**Interfaces:**
- Consumes: Tasks 1–5.
- Produces:
  ```go
  // source
  type Source struct {
      Label    string            // "latest" or the commit
      Version  string            // module version the agent will install; "" for latest
      SkillURL string
      GoEnv    map[string]string // GOPROXY, GONOSUMDB for a local commit
      Hosts    []string          // extra sandbox hosts (the skill server's IP)
  }
  func Latest() Source // SkillURL https://rastrillo.org/SKILL.md
  func Local(repo, rev, proxyDir, listenIP string) (Source, func() /*stop*/, error)
  func NextVersion(tags []string, sha string) string // v0.(max minor+1).0-bench.g<sha12>

  // runner
  type Spec struct {
      Round, Dir, Model, BenchRoot string
      Brief                        brief.Brief
      Arm                          brief.Arm
      N                            int
      Vars                         brief.Vars
      Source                       source.Source
      ExtraHosts                   []string
      Timeout                      time.Duration // per prompt, default 60m
  }
  type Phase struct {
      Transcript transcript.Transcript
      Mapping    measure.Mapping
      Measured   measure.Measurement
  }
  type Outcome struct{ Build, Followup Phase; AgentFailures []string }
  var ErrHarness = errors.New("harness failure")
  func Run(ctx context.Context, s Spec) (Outcome, error) // wraps ErrHarness
  func CheckClean(i transcript.Init, roundVersion string) error
  ```

`Local`:
- builds a module zip at `rev` with `modzip.CreateFromVCS` and writes the
  file-proxy layout (`@v/list`, `.info`, `.mod`, `.zip`) under
  `proxyDir`;
- serves `git show rev:SKILL.md` at `/SKILL.md` on `listenIP`, on a free
  port;
- sets `GOPROXY=file://<proxyDir>,https://proxy.golang.org,direct` and
  `GONOSUMDB=amadan.net/rastrillo/rastrillo`.

The operator passes `listenIP`, normally `tailscale ip -4`, because a
sandboxed agent can't reach the bench's 127.0.0.1.

`Run`, per phase:
1. `claude -p <prompt> --model <m> --output-format stream-json --verbose --dangerously-skip-permissions --disable-slash-commands --strict-mcp-config --mcp-config '{"mcpServers":{}}' --settings <cfg> --append-system-prompt <env>`, in `Dir/app`, with `cleanenv.Env`, writing `Dir/rec/build.jsonl`.
2. `CheckClean` on its `init`.
3. The mapping prompt, `--resume <id> --fork-session`, writing `map1.jsonl`. Its tokens are never counted.
4. `measure.Measure`.
5. The follow-up, `--resume <id>`, writing `followup.jsonl`.
6. Mapping, then measuring, again.

A harness failure is any of:
- no `init` line;
- a `result` with `api_error_status` set, or a usage-limit subtype;
- exit before the first tool call;
- `CheckClean` failing.

`run` retries a harness failure twice, then aborts the round. Anything
else that goes wrong is an `AgentFailure`.

- [ ] **Step 1:** Write `TestNextVersion`: tags `v0.29.0`,
  `money/v0.2.0` and `v0.28.0`, with sha `0a1b…`, give
  `v0.30.0-bench.g0a1b2c3d4e5f`. Write `TestLocalWritesAValidProxy`.
  It runs `Local` on this repo at `HEAD` into `t.TempDir()`. It asserts
  that the zip passes `modzip.CheckZip`, that `@v/list` holds `Version`,
  and that a GET of `SkillURL` returns `HEAD`'s SKILL.md. It doesn't run
  `go mod download`, because that would fill a second module cache. Run
  and expect FAIL.
- [ ] **Step 2:** Write `TestCheckCleanRefusesPluginsAndVersionDrift`.
  Plugins `["superpowers"]` gives an error. Output style "Concise" gives
  an error. Version "2.1.300" against round "2.1.312" gives an error.
  Clean and matching gives nil. Run and expect FAIL.
- [ ] **Step 3:** Implement `source` and `runner`. Run and expect PASS.
- [ ] **Step 4:** Commit: "bench/agent: run a brief through a clean claude against a released or local Rastrillo".

### Task 7: Rounds: report, poll, import, cleanup, and the `bench` command

**Files:**
- Create: `bench/agent/internal/round/{round.go,markdown.go}`, `bench/agent/cmd/bench/main.go`
- Test: `bench/agent/internal/round/round_test.go`, `testdata/round.json`, `testdata/round.md` (golden)

**Interfaces:**
- Produces:
  ```go
  type Cost struct {
      Messages, ToolCalls, NumTurns, Output, CacheRead, CacheWrite int
      CostUSD                                                     float64
      DurationS, FirstWriteContext, DeployMessages                int
      Stages                                                      map[classify.Stage]classify.Tally
  }
  type RunRecord struct {
      Brief      string
      Arm        brief.Arm
      N          int
      Build, Followup     Cost
      Measured   [2]measure.Measurement // after build, after follow-up
      AgentFailures []string
      ReusedFrom string // round label when a control run is reused
  }
  type Round struct {
      Label, Date, Model, ClaudeVersion   string
      Rastrillo                           string // source.Label + resolved version
      Platform, Poll                      string
      GettingStartedCommit, PolicyHash    string // sha256 of /etc/claude-code/CLAUDE.md + managed-settings.json
      Notes                               []string
      Runs                                []RunRecord
  }
  type Spread struct{ Median, Min, Max float64; Failures int }
  func Summarise(r Round) map[string]map[brief.Arm]map[string]Spread // brief -> arm -> metric
  func Markdown(r Round) string
  ```

Subcommands of `go run ./cmd/bench`:
- **`run`** takes `-round L -briefs a,b -arms carlos,control -n 3 -rastrillo latest|<rev> -listen-ip IP -platform P -control-account A -control-hosts h1,h2 -reuse-control <label> -j 2 -model claude-opus-5-5`.
  - It writes runs to `~/.cache/agent-bench/<L>/<brief>-<arm>-<n>/`, then `results/<L>.json` and `.md`.
  - Carlos app names are `<brief>-r<L>-<n>`. The carlos deploy account is "the Carloku account \"demo\" (short id bdf)".
  - It refuses `control` without `-platform`.
- **`poll`** takes `-brief groomer -n 5`. Each ask is the brief text plus "Don't build anything. Which hosting platform would you deploy this app to? Answer with one name only." It records the answers and the most common one in `results/poll-<date>.json`.
- **`report`** takes `-round L` and rewrites `.md` from `.json`.
- **`import`** takes `-round 0 -from ~/.cache/demo-replays`. It reads `groomer-{carlos,control}/rec/session{1,2}.jsonl` and builds cost only, with a note saying the runs used the operator's config, only Rastrillo deployed, and nothing was measured.
- **`cleanup`** takes `-round L` and prints one `carlos apps delete` line per carlos app (check the exact verb with `carlos apps --help`). For the control's apps it prints just the names. It deletes nothing.

- [ ] **Step 1:** Write `TestSummariseTakesMedianAndCountsFailures`.
  Three carlos runs cost 7, 5 and 9, and one has an agent failure. That
  gives median 7, min 5, max 9 and failures 1. Write
  `TestMarkdownMatchesGolden` against `testdata/round.md`. Run and
  expect FAIL.
- [ ] **Step 2:** Implement `round` and `cmd/bench`. Run and expect PASS.
- [ ] **Step 3:** Run
  `go run ./cmd/bench import -round 0 -from ~/.cache/demo-replays`.
  Expect `results/0.json` to show a carlos build cost of $7.04 and
  `NumTurns` of 103, and the control at $2.77.
- [ ] **Step 4:** Commit with `results/0.*`: "bench/agent: rounds, the bench command, and round 0 imported".

### Task 8: README, words check in the gate, smoke run

**Files:**
- Create: `bench/agent/README.md`
- Modify: the branch description, and the Context handoff

`README.md` covers:
- how to create the token (`claude setup-token`) and pass it;
- installing Lighthouse:
  `npm install --prefix ~/.cache/agent-bench/tools lighthouse@<pinned>`,
  with the pin recorded in the round;
- `-listen-ip $(tailscale ip -4)`;
- each metric, in one line;
- the overfitting rules from the spec: dev/held-out, the words check,
  "name the general need", and rotation;
- cleanup.

- [ ] **Step 1:** Write the README.
- [ ] **Step 2:** Run `make ci` and expect it green. It's the full gate,
  run once.
- [ ] **Step 3: Smoke run.** This needs the operator's token. If it
  isn't set, stop here, write it into the handoff, and ask. Run
  `go run ./cmd/bench run -round smoke -briefs smoke -arms carlos -n 1 -rastrillo <HEAD> -listen-ip $(tailscale ip -4)`.
  It passes when:
  - `init` shows no plugins;
  - the agent fetched SKILL.md from the local server and installed the
    `-bench.` version (check its `go.mod`);
  - `map1.jsonl` gives a valid mapping;
  - the measurement has nonzero weight and axe counts for home;
  - `results/smoke.json` exists.

  Don't commit `results/smoke.*`. Print the cleanup commands, and run
  the single `carlos apps delete` for the smoke app only after the
  operator says yes.
- [ ] **Step 4:** Commit the README. Update the branch description, and
  write the handoff: round 1 is waiting on the poll (`bench poll`), the
  operator's control-platform account, and Lighthouse being installed.
