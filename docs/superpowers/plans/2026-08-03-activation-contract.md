# Plan: close the activation-contract gap — `rastrillo.Run`

**Goal:** a rastrillo app binary works in all three of the platform's route
homes without a hand-written systemd unit: plain always-on instance, agent
exec child (`-hibernate` routes), and `carlos-app@.service` unit tenant.

**The gap** (found deploying hello world for real, 2026-08-02; contract
verified against platform source 2026-08-03): `rastrillo.Serve`'s listener
resolution is already complete — LISTEN_FDS fd 3 (with LISTEN_PID check)
wins, then `-socket` unix, then `-addr` TCP, then `:8080` — but the
*entrypoint* isn't: the scaffolded `main.go` parses only `-socket`/`-addr`.
Two argv shapes the platform actually uses therefore fail or lose data:

1. **Agent exec child (hibernate routes, default backing).** The activator
   spawns exactly `<OptDir>/live/<host> --socket <rt.Socket> --db <dbPath>`
   (platform `internal/activator/backend_exec.go:259`). A scaffolded app's
   `flag.Parse` with no `-db` flag registered **exits 2** on the
   unrecognized flag — the app cannot boot as a hibernating tenant at all.
2. **Unit tenant (`carlos-app@.service`).** `ExecStart=/opt/carlos/live/%i
   serve` — a bare `serve` subcommand, no flags. Go's `flag` package treats
   `serve` as a positional arg and parses nothing after it; the app happens
   to limp along today only because the flags all default. State lives in
   `$STATE_DIRECTORY` (`/var/lib/carlos-app/<host>`, `DynamicUser=yes`,
   cwd is unset ≈ `/`), so a relative DB path silently lands in the wrong
   place.

**The fix:** a new exported entrypoint `rastrillo.Run(opts Options) error`
that resolves both argv shapes plus `$STATE_DIRECTORY`, then calls the
unchanged `Serve`. The scaffold and the example switch to it.

**What hibernation does NOT require of the app** (so the plan deliberately
excludes it): the activator owns the entire litestream restore/replicate
cycle. The app only needs to (a) accept `-db` and create its SQLite file
there when absent — `openDB` already does, `sql.Open` creates missing
files; (b) drain gracefully on SIGTERM within ~10s — `Serve` already
shuts down with a 10s timeout, inside the activator's 20s SIGKILL budget;
(c) answer `GET /` (any non-5xx) for the activator's wake probe and
`GET /healthz` 200 for vet — both already true. Optional `GET
/api/next-due` (due-time scheduler index) is explicitly out of scope for
this slice.

## Global constraints

- **Contract facts are fixed; do not reinterpret them.** Exec-child argv is
  `--socket <path> --db <path>` (Go flags, so `-socket`/`--socket` are
  identical); unit ExecStart is `<binary> serve` with no further args;
  systemd fd arrives as LISTEN_FDS=1/fd 3; exec-child sockets default to
  `/run/carlos/<host>.sock`, unit sockets to `/run/carlos/apps/<host>.sock`
  (the app never hardcodes either — they arrive via flag or fd).
- **Additive only.** `Serve`'s signature and behavior are unchanged;
  existing apps calling `Serve` directly keep compiling and behaving
  identically. `Run` is new API. No new flag is ever *required* — a bare
  `<binary>` invocation still serves on `:8080` (dev convenience) exactly
  as today.
- **Never register a flag the platform doesn't pass.** The platform
  deliberately delivers new per-child config via environment, not flags,
  because pre-existing binaries exit 2 on unknown flags
  (backend_exec.go's control-socket comment). Mirror that caution: `Run`
  registers exactly `-socket`, `-addr`, `-db`.
- **Do not touch `cmd/rastrillo/dev.go` or `dev_test.go`** — a parallel
  branch (`dev-warts`) owns those files right now.
- Comments state constraints the code can't show, never narration. Match
  existing file style (see serve.go).
- Sweep before every commit, all clean:
  `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go build ./...`,
  same env `go vet ./...`, same env `go test ./... -count=1`, and
  `gofmt -l .` (empty). If the toolchain reports "read-only file system",
  rerun the command with the sandbox disabled.
- Commit messages: short imperative subject; body explains why; end with
  the trailer line `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

## Task 1: `rastrillo.Run` — entrypoint that speaks the contract

**Files:** `run.go` (new), `run_test.go` (new), both in the repo root
package `rastrillo`.

`run.go` — exact code:

```go
package rastrillo

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// Run is the process entrypoint for a rastrillo app: it resolves the
// platform's activation argv, then serves. The platform invokes an app
// binary in two shapes (see carlosframework/platform,
// internal/activator/backend_exec.go and internal/host/units/):
//
//	<binary> [-socket p] [-addr a] [-db p]  agent exec child — hibernate
//	                                        routes; the activator spawns
//	                                        `<live> --socket <s> --db <d>`
//	<binary> serve                          carlos-app@.service unit
//	                                        tenant — no flags; the listener
//	                                        arrives via LISTEN_FDS (fd 3)
//	                                        and state lives in
//	                                        $STATE_DIRECTORY
//
// Flags override the corresponding Options fields. A relative
// Options.DBPath (or -db value) is resolved inside $STATE_DIRECTORY when
// systemd provides one — a unit tenant's cwd is not its state dir — so
// the same binary and the same Options work in a dev checkout, as an
// exec child, and as a unit tenant. Hibernation needs nothing further
// from the app: the activator owns the restore/replicate cycle, and
// Serve's SIGTERM drain (10s) fits inside the activator's 20s budget.
func Run(opts Options) error {
	opts, err := resolveInvocation(opts, os.Args[1:], os.Getenv("STATE_DIRECTORY"))
	if err != nil {
		return err
	}
	return Serve(opts)
}

// resolveInvocation applies one activation argv to opts. Split from Run
// so the argv/state-dir contract is testable without opening sockets.
func resolveInvocation(opts Options, args []string, stateDir string) (Options, error) {
	// The unit template execs `<binary> serve` (no flags); tolerate
	// flags after the subcommand anyway — a drop-in override may add
	// them.
	if len(args) > 0 && args[0] == "serve" {
		args = args[1:]
	}
	fs := flag.NewFlagSet("rastrillo app", flag.ContinueOnError)
	socket := fs.String("socket", "", "unix socket to listen on (platform activation contract)")
	addr := fs.String("addr", "", "TCP host:port to listen on")
	db := fs.String("db", "", "SQLite database path (activator passes the route's path here)")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if n := fs.NArg(); n > 0 {
		return opts, fmt.Errorf("unexpected argument %q — a rastrillo app accepts an optional leading \"serve\" and the -socket/-addr/-db flags", fs.Arg(0))
	}
	if *socket != "" {
		opts.Socket = *socket
	}
	if *addr != "" {
		opts.Addr = *addr
	}
	if *db != "" {
		opts.DBPath = *db
	}
	if stateDir != "" && opts.DBPath != "" && !filepath.IsAbs(opts.DBPath) {
		opts.DBPath = filepath.Join(stateDir, opts.DBPath)
	}
	return opts, nil
}
```

`run_test.go` — table-driven over `resolveInvocation`. Base opts for every
case: `Options{DBPath: base}` where the table sets `base`. Cases (exact
expectations):

| name | base DBPath | args | stateDir | want |
|---|---|---|---|---|
| exec child argv | `""` | `--socket /run/carlos/x.sock --db /data/x.db` | `""` | Socket=`/run/carlos/x.sock`, DBPath=`/data/x.db` |
| single-dash spelling | `""` | `-socket /run/carlos/x.sock -db /data/x.db` | `""` | same as above |
| unit tenant, relative db | `app.db` | `serve` | `/var/lib/carlos-app/x` | DBPath=`/var/lib/carlos-app/x/app.db` |
| unit tenant, no db | `""` | `serve` | `/var/lib/carlos-app/x` | DBPath stays `""` (no db conjured for db-less apps) |
| dev bare invocation | `app.db` | (none) | `""` | DBPath stays `app.db` (relative, cwd) |
| absolute db + stateDir | `/data/x.db` | (none) | `/var/lib/carlos-app/x` | DBPath stays `/data/x.db` |
| -db wins over Options | `app.db` | `--db /data/y.db` | `/var/lib/carlos-app/x` | DBPath=`/data/y.db` (absolute → stateDir not applied) |
| flags after serve | `""` | `serve -addr :9000` | `""` | Addr=`:9000` |
| addr override | `""` | `-addr :9000` | `""` | Addr=`:9000` |
| unknown flag errors | `""` | `-control-socket /x` | `""` | error (from FlagSet) |
| stray positional errors | `""` | `bogus` | `""` | error mentioning `"bogus"` |

Also assert in the error cases that opts came back unmodified where the
doc says so is NOT required — only that err != nil and, for the stray
positional, that the message contains `bogus`.

**Verify:** the sweep from Global constraints. Commit.

## Task 2: scaffold, example, README speak `Run`

**Files:** `cmd/rastrillo/new.go` (mainTemplate), scaffold tests if any
assert the template, `examples/helloworld/cmd/helloworld/main.go`,
`README.md`.

1. **`mainTemplate` in new.go**: drop the `flag` import and the
   `-socket`/`-addr` parsing; call `rastrillo.Run` instead of
   `rastrillo.Serve`, passing only `Mux` and `Logger` (keep the existing
   `Ctx`/`gen.Router` wiring and the shared-Ctx comment verbatim). Replace
   the `-socket/-addr mirror…` comment with one stating that `Run` speaks
   the platform's activation contract: `-socket`/`-addr`/`-db` flags (agent
   exec children) and the bare `serve` subcommand (`carlos-app@` unit
   tenants). Check `new_test.go`/`generate` tests for template assertions
   and update them to match.
2. **`examples/helloworld/cmd/helloworld/main.go`**: apply the identical
   change by hand (the example is checked in, not regenerated). Its content
   must match what `rastrillo new` now emits, module path aside — that
   equivalence is the example's whole point.
3. **README.md**:
   - In the `rastrillo.Serve` bullet, delete the "**Covers only the plain
     'always-on instance' route kind**…" caveat sentence and instead state
     the contract is covered end to end, naming `rastrillo.Run`.
   - Add a `rastrillo.Run` bullet right before the `rastrillo.Serve` one:
     the two argv shapes (exec child `--socket/--db`; unit tenant `serve`
     subcommand), `$STATE_DIRECTORY` resolution for relative DB paths, and
     that hibernation requires nothing else from the app (activator owns
     restore/replicate; SIGTERM drain fits the budget).
   - In the Live section, keep history honest: the deployed hello world
     predates `Run` — change "predates `-hibernate`/`-backing unit`
     support noted above" to say that support landed with `rastrillo.Run`
     (2026-08-03) after this deploy; don't claim the live instance uses it.

**Verify:** sweep; additionally scaffold a throwaway app end to end and
boot it both ways (this is the task's acceptance test — run it and paste
the output in the report):

```sh
cd $(mktemp -d) && /tmp/claude-1001/rastrillo-serve-cli new smoke && cd smoke
go mod edit -replace amadan.net/rastrillo/rastrillo=/tmp/claude-1001/rastrillo-serve
GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go mod tidy
GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go build ./cmd/smoke
./smoke --socket /tmp/smoke.sock --db /tmp/smoke.db &   # exec-child shape
curl -s --unix-socket /tmp/smoke.sock http://x/healthz   # expect: ok
kill %1                                                  # SIGTERM drain
./smoke serve -addr :18080 &                             # unit shape (TCP fallback)
curl -s http://localhost:18080/healthz                   # expect: ok
kill %1
test -f /tmp/smoke.db && echo db-created
```

(The controller builds `/tmp/claude-1001/rastrillo-serve-cli` from this
branch before dispatching Task 2 — do not build it yourself; if it is
missing, report BLOCKED.) Commit.
