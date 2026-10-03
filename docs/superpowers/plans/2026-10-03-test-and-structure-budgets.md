# Test, CI, directory-size and screen-time budgets: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give every Rastrillo app mechanical budgets for directory size, package test time and screen time, enforced by `make ci`, and teach them in SKILL.md and the docs.

**Architecture:** One exemptions parser (`internal/budgetfile`) feeds three enforcers: `rastrillo budget size` (directory line counts), `rastrillo budget test` (runs `go test -json`, judges the per-package time each test binary prints through `budget.Main`), and `perf/perftest` (screen and cold-boot timing over real HTTP, in a serial perf lane). The scaffold wires all three into `make ci`; Rastrillo's own gate dogfoods `budget size`.

**Tech Stack:** Go 1.26 (module `go 1.25.0`), `testing`, `cmd/test2json` event format, `net/http/httptest`, `net/http/httptrace`, `github.com/go-chi/chi/v5` v5.3.2, existing `dbtest`, `perf`, `rastrillo.Handler`.

**Spec:** `docs/superpowers/specs/2026-10-03-test-and-structure-budgets-design.md`. Read it before any task; this plan argues from it and does not repeat its reasoning.

## Global Constraints

- Module path is `amadan.net/rastrillo/rastrillo`. Never `github.com/carlosframework/rastrillo`.
- Defaults: 5,000 non-test lines and 8,000 test lines per directory; 10s package test time; `perf.DefaultBudget` (150ms) warm screen; `perf.DefaultColdBudget` (500ms) cold boot; 60s test-step target, reported only.
- Exemptions file: `.rastrillo/budgets.txt` beside the module's `go.mod`.
- Timing budgets fail only when `AMADAN_CI` or `CI` is non-empty; otherwise they report.
- The measurement line is exactly `rastrillo-budget/v1 ran <duration>`; the boot marker is exactly `rastrillo-budget/v1 boot-first-byte`; the child variable is `RASTRILLO_BOOT_CHILD`.
- `budget` and `perf/perftest` are GORM-free and join `GORM_FREE` in the Makefile.
- Gate before every push: `go vet ./... && gofmt -l . && go test ./...` (`gofmt -l` prints nothing). Locally use `GOFLAGS=-mod=mod` (memory: scratch-module tests need it).
- Go build cache: the default `~/.cache/go-build`. Never set `GOCACHE`/`GOTMPDIR` under `/tmp`.
- Comments say why, naming the failure prevented (AGENTS.md). Commit bodies explain why.
- No em dashes in any user-facing copy (CLI messages, docs prose). Use colons, commas or full stops.
- Git runs unsandboxed here; commit with `amadan commit` in the `rastrillo-test-budgets` worktree.

## Review Focus

1. **A cached package must be judged on its replayed time.** Run the same over-budget fixture twice under `AMADAN_CI=1`; the second run is `(cached)` and must still fail. Pinned in Task 5.
2. **A `go test` that dies must never read green.** A fake `go` that prints one passing package then kills itself must make `budget test` exit non-zero. Pinned in Task 5.
3. **The perf lane must not pass having measured nothing.** `-require` with a missing test fails. Pinned in Task 4 (judge) and Task 9 (scaffold make perf).
4. **A handler that ignores its context must not hang the perf lane.** Pinned in Task 6.
5. **The scaffold's whole `make ci` passes offline on a fresh app**, with `GOFLAGS=-mod=readonly` and `GOPROXY=off`. Pinned in Task 10.

---

## File structure

| File | Responsibility |
|---|---|
| `internal/budgetfile/budgetfile.go` | Exemptions grammar, defaults, module root and module path lookup |
| `internal/budgetsize/size.go` | Walk a module, count lines per directory, judge against the file |
| `internal/budgetrun/events.go` | Read a `go test -json` stream into per-package state |
| `internal/budgetrun/judge.go` | Time judging, `-require`, staleness, measurement checks |
| `internal/budgetrun/report.go` | Failure output first, then the receipt |
| `budget/budget.go` | `budget.Main`: time `m.Run`, print the measurement line |
| `perf/perftest/screens.go` | `Screens`: inventory, measurement over HTTP, verdicts |
| `perf/perftest/boot.go` | `Boot`: re-exec child protocol, cold first byte |
| `perf/perftest/file.go` | Shared exemptions lookup and the CI switch for perftest |
| `cmd/rastrillo/budget.go` | `rastrillo budget size` and `rastrillo budget test` |
| `cmd/rastrillo/new.go` | Scaffold templates (modify) |
| `.rastrillo/budgets.txt` | Rastrillo's own two size exemptions |
| `docs/site/reference/budget.md`, `docs/site/reference/perftest.md` | API pages |
| `docs/site/testing.md`, `SKILL.md`, `CHANGELOG.md`, `docs/site/reference/perf.md` | Prose |

---

### Task 1: `internal/budgetfile`, the exemptions grammar

**Files:**
- Create: `internal/budgetfile/budgetfile.go`
- Test: `internal/budgetfile/budgetfile_test.go`

**Interfaces:**
- Produces:
  ```go
  const (
      FileName      = ".rastrillo/budgets.txt"
      DefaultSource = 5000
      DefaultTest   = 8000
      DefaultTime   = 10 * time.Second
      BootChildEnv  = "RASTRILLO_BOOT_CHILD"
  )
  type Size struct { Dir string; Source, Test int; Line int; Reason string } // 0 means "-" (default)
  type Time struct { Dir string; Limit time.Duration; Line int; Reason string }
  type Perf struct { Screen string; Limit time.Duration; Skip bool; Line int; Reason string }
  type Boot struct { Limit time.Duration; Line int; Reason string }
  type File struct {
      Sizes map[string]Size   // by Dir
      Times map[string]Time   // by Dir
      Perfs map[string]Perf   // by Screen, "GET /notes/{id}"
      Boot  *Boot
  }
  func Parse(r io.Reader) (*File, error)
  func Load(root string) (*File, error)          // missing file: empty File, nil error
  func ModuleRoot(dir string) (string, error)    // nearest ancestor (or dir) holding go.mod
  func ModulePath(root string) (string, error)   // the module line of root/go.mod
  func Enforce(getenv func(string) string) bool  // AMADAN_CI or CI non-empty
  ```

- [ ] **Step 1: Write the failing tests**

```go
package budgetfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseValidFile(t *testing.T) {
	src := `# comment line

size internal/designsystem 10500 -  generated samples; split owed
size ui                    -    20500 one test per component, by design
time internal/importer     25s        replays a 40MB fixture
perf GET /notes/export     1200ms     streams every row
perf GET /jobs/{id}/events skip       server-sent events, never finishes
boot                       800ms      parses 12 locales at start
`
	f, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if s := f.Sizes["internal/designsystem"]; s.Source != 10500 || s.Test != 0 || s.Line != 3 {
		t.Errorf("designsystem size = %+v", s)
	}
	if s := f.Sizes["ui"]; s.Source != 0 || s.Test != 20500 {
		t.Errorf("ui size = %+v", s)
	}
	if tm := f.Times["internal/importer"]; tm.Limit != 25*time.Second || tm.Reason != "replays a 40MB fixture" {
		t.Errorf("importer time = %+v", tm)
	}
	if p := f.Perfs["GET /notes/export"]; p.Limit != 1200*time.Millisecond || p.Skip {
		t.Errorf("export perf = %+v", p)
	}
	if p := f.Perfs["GET /jobs/{id}/events"]; !p.Skip {
		t.Errorf("events perf = %+v, want skip", p)
	}
	if f.Boot == nil || f.Boot.Limit != 800*time.Millisecond {
		t.Errorf("boot = %+v", f.Boot)
	}
}

// Each refusal is its own case so a parser that stops checking one rule
// cannot hide behind another case's failure.
func TestParseRefusals(t *testing.T) {
	cases := map[string]string{
		"unknown kind":             "speed internal/x 5s why",
		"missing reason":           "time internal/x 5s",
		"bad number":               "size internal/x lots - why",
		"bad duration":             "time internal/x soon why",
		"zero":                     "time internal/x 0s why",
		"negative":                 "size internal/x -5 - why",
		"both dashes":              "size internal/x - - why",
		"source not above default": "size internal/x 5000 - why",
		"test not above default":   "size internal/x - 8000 why",
		"leading dot slash":        "time ./internal/x 5s why",
		"trailing slash":           "time internal/x/ 5s why",
		"absolute":                 "time /internal/x 5s why",
		"dot dot":                  "time ../x 5s why",
		"unclean":                  "time internal/a/../b 5s why",
		"duplicate":                "time internal/x 5s why\ntime internal/x 6s why",
		"lower-case method":        "perf get /x 1s why",
		"perf missing pattern":     "perf GET 1s why",
		"boot missing reason":      "boot 900ms",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(src))
			if err == nil {
				t.Fatalf("accepted %q", src)
			}
			if !strings.Contains(err.Error(), "line ") {
				t.Errorf("error %q does not name a line", err)
			}
		})
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	f, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Sizes)+len(f.Times)+len(f.Perfs) != 0 || f.Boot != nil {
		t.Fatalf("missing file should mean no exemptions, got %+v", f)
	}
}

func TestModuleRootAndPath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(root, "internal", "app")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := ModuleRoot(deep)
	if err != nil || got != root {
		t.Fatalf("ModuleRoot = %q, %v; want %q", got, err, root)
	}
	mp, err := ModulePath(root)
	if err != nil || mp != "example.com/app" {
		t.Fatalf("ModulePath = %q, %v", mp, err)
	}
}

func TestEnforce(t *testing.T) {
	env := map[string]string{}
	get := func(k string) string { return env[k] }
	if Enforce(get) {
		t.Fatal("enforcing with neither variable set")
	}
	env["AMADAN_CI"] = "1"
	if !Enforce(get) {
		t.Fatal("AMADAN_CI=1 must enforce")
	}
	env = map[string]string{"CI": "true"}
	if !Enforce(get) {
		t.Fatal("CI=true must enforce")
	}
}
```

- [ ] **Step 2: Run to see them fail**

Run: `GOFLAGS=-mod=mod go test ./internal/budgetfile/`
Expected: FAIL, build errors (`undefined: Parse`).

- [ ] **Step 3: Implement**

```go
// Package budgetfile reads .rastrillo/budgets.txt, the one place an app
// records the exceptions to its budgets. One parser serves the CLI and
// perf/perftest, so the two can never disagree about what a line means.
//
// The defaults appear here and nowhere else. A record exists only where
// something cannot meet them, and every record carries a reason, because
// an exemption nobody can explain is one nobody will ever remove.
package budgetfile

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	FileName      = ".rastrillo/budgets.txt"
	DefaultSource = 5000
	DefaultTest   = 8000
	DefaultTime   = 10 * time.Second
	// BootChildEnv marks perftest.Boot's child process. budget.Main reads
	// it to stay silent there: the child's own measurement line would
	// otherwise be judged as a second run of the package.
	BootChildEnv = "RASTRILLO_BOOT_CHILD"
)

type Size struct {
	Dir          string
	Source, Test int // 0 means the record wrote "-": that column uses its default
	Line         int
	Reason       string
}

type Time struct {
	Dir    string
	Limit  time.Duration
	Line   int
	Reason string
}

type Perf struct {
	Screen string // "GET /notes/{id}", the name perf gives the route in production
	Limit  time.Duration
	Skip   bool
	Line   int
	Reason string
}

type Boot struct {
	Limit  time.Duration
	Line   int
	Reason string
}

type File struct {
	Sizes map[string]Size
	Times map[string]Time
	Perfs map[string]Perf
	Boot  *Boot
}

func empty() *File {
	return &File{Sizes: map[string]Size{}, Times: map[string]Time{}, Perfs: map[string]Perf{}}
}

// Parse refuses rather than guesses: every malformed line is reported
// with its number, all at once, so one fix-up pass clears the file.
func Parse(r io.Reader) (*File, error) {
	f := empty()
	var errs []error
	sc := bufio.NewScanner(r)
	n := 0
	for sc.Scan() {
		n++
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if err := f.add(n, fields); err != nil {
			errs = append(errs, fmt.Errorf("%s line %d: %w", FileName, n, err))
		}
	}
	if err := sc.Err(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return f, nil
}

func (f *File) add(n int, fields []string) error {
	reason := func(from int) (string, error) {
		if len(fields) <= from {
			return "", errors.New("missing reason: every record says why it exists")
		}
		return strings.Join(fields[from:], " "), nil
	}
	switch fields[0] {
	case "size":
		if len(fields) < 4 {
			return errors.New("want: size <dir> <source>|- <test>|- <reason>")
		}
		dir := fields[1]
		if err := canonical(dir); err != nil {
			return err
		}
		src, err := column(fields[2], DefaultSource, "source")
		if err != nil {
			return err
		}
		tst, err := column(fields[3], DefaultTest, "test")
		if err != nil {
			return err
		}
		if src == 0 && tst == 0 {
			return errors.New("both columns are -: a record that raises nothing should not exist")
		}
		why, err := reason(4)
		if err != nil {
			return err
		}
		if _, dup := f.Sizes[dir]; dup {
			return fmt.Errorf("second size record for %s", dir)
		}
		f.Sizes[dir] = Size{Dir: dir, Source: src, Test: tst, Line: n, Reason: why}
	case "time":
		if len(fields) < 3 {
			return errors.New("want: time <dir> <duration> <reason>")
		}
		dir := fields[1]
		if err := canonical(dir); err != nil {
			return err
		}
		d, err := duration(fields[2])
		if err != nil {
			return err
		}
		why, err := reason(3)
		if err != nil {
			return err
		}
		if _, dup := f.Times[dir]; dup {
			return fmt.Errorf("second time record for %s", dir)
		}
		f.Times[dir] = Time{Dir: dir, Limit: d, Line: n, Reason: why}
	case "perf":
		if len(fields) < 4 || !strings.HasPrefix(fields[2], "/") {
			return errors.New("want: perf <METHOD> <pattern> <duration>|skip <reason>")
		}
		method := fields[1]
		if method != strings.ToUpper(method) {
			return fmt.Errorf("method %q must be upper case, as perf names it", method)
		}
		p := Perf{Screen: method + " " + fields[2], Line: n}
		if fields[3] == "skip" {
			p.Skip = true
		} else {
			d, err := duration(fields[3])
			if err != nil {
				return err
			}
			p.Limit = d
		}
		why, err := reason(4)
		if err != nil {
			return err
		}
		p.Reason = why
		if _, dup := f.Perfs[p.Screen]; dup {
			return fmt.Errorf("second perf record for %s", p.Screen)
		}
		f.Perfs[p.Screen] = p
	case "boot":
		if len(fields) < 2 {
			return errors.New("want: boot <duration> <reason>")
		}
		d, err := duration(fields[1])
		if err != nil {
			return err
		}
		why, err := reason(2)
		if err != nil {
			return err
		}
		if f.Boot != nil {
			return errors.New("second boot record")
		}
		f.Boot = &Boot{Limit: d, Line: n, Reason: why}
	default:
		return fmt.Errorf("unknown record %q (size, time, perf, boot)", fields[0])
	}
	return nil
}

// canonical refuses every spelling but one, so a directory can never
// have two records that both look right.
func canonical(dir string) error {
	switch {
	case dir == "..", strings.HasPrefix(dir, "../"), strings.HasPrefix(dir, "/"),
		strings.Contains(dir, `\`), path.Clean(dir) != dir:
		return fmt.Errorf("directory %q is not canonical: write it relative to the module root, slash-separated, as path.Clean gives it (%q)", dir, path.Clean(dir))
	}
	return nil
}

func column(s string, def int, name string) (int, error) {
	if s == "-" {
		return 0, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("%s column %q: want a positive line count or -", name, s)
	}
	if v <= def {
		return 0, fmt.Errorf("%s column %d is not above the default %d: write -", name, v, def)
	}
	return v, nil
}

func duration(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("duration %q: want a positive Go duration such as 25s or 1200ms", s)
	}
	return d, nil
}

// Load reads root's file. A missing file is no exemptions, not an
// error: a new app has none and should not need an empty file to say so.
func Load(root string) (*File, error) {
	fh, err := os.Open(filepath.Join(root, FileName))
	if errors.Is(err, os.ErrNotExist) {
		return empty(), nil
	}
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	return Parse(fh)
}

func ModuleRoot(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no go.mod in this directory or any parent")
		}
		dir = parent
	}
}

func ModulePath(root string) (string, error) {
	b, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, l := range strings.Split(string(b), "\n") {
		if f := strings.Fields(l); len(f) >= 2 && f[0] == "module" {
			return strings.Trim(f[1], `"`), nil
		}
	}
	return "", fmt.Errorf("%s/go.mod has no module line", root)
}

// Enforce says whether timing budgets fail or only report. Timing is
// evidence on the CI runner and noise on a loaded laptop, so the switch
// is the variable every CI runner sets (amadan's AMADAN_CI, everyone
// else's CI), not a flag someone has to remember.
func Enforce(getenv func(string) string) bool {
	return getenv("AMADAN_CI") != "" || getenv("CI") != ""
}
```

- [ ] **Step 4: Run to see them pass**

Run: `GOFLAGS=-mod=mod go test ./internal/budgetfile/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/budgetfile
amadan commit -m "budgetfile: one grammar for the budgets' exemptions" -summary "Parser, defaults, module root lookup and the CI switch every budget shares." -body "The CLI and perf/perftest both read budgets.txt; two parsers would eventually disagree about what a record means, and a refused line must name its line number or nobody finds it.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: `internal/budgetsize` and `rastrillo budget size`

**Files:**
- Create: `internal/budgetsize/size.go`, `internal/budgetsize/size_test.go`
- Create: `cmd/rastrillo/budget.go`
- Modify: `cmd/rastrillo/main.go` (dispatch and usage)
- Test: `cmd/rastrillo/budget_test.go`

**Interfaces:**
- Consumes: `budgetfile.File`, `budgetfile.Load`, `DefaultSource`, `DefaultTest`.
- Produces:
  ```go
  // internal/budgetsize
  type Count struct { Dir string; Source, Test int }
  func Measure(root string) ([]Count, error)                // sorted by Dir
  func Check(counts []Count, f *budgetfile.File) []string    // one message per violation, sorted
  // cmd/rastrillo
  func runBudget(args []string) error                        // "size" | "test"
  func budgetSize(args []string, out io.Writer) error
  ```

- [ ] **Step 1: Write the failing tests** (`internal/budgetsize/size_test.go`)

```go
package budgetsize

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/internal/budgetfile"
)

func write(t *testing.T, root, rel string, lines int) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(strings.Repeat("//\n", lines)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func module(t *testing.T) string {
	root := t.TempDir()
	write(t, root, "go.mod", 1)
	return root
}

func file(t *testing.T, src string) *budgetfile.File {
	f, err := budgetfile.Parse(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func check(t *testing.T, root, records string) []string {
	t.Helper()
	counts, err := Measure(root)
	if err != nil {
		t.Fatal(err)
	}
	return Check(counts, file(t, records))
}

func TestAtTheCeilingPasses(t *testing.T) {
	root := module(t)
	write(t, root, "internal/app/a.go", 5000)
	write(t, root, "internal/app/a_test.go", 8000)
	if got := check(t, root, ""); len(got) != 0 {
		t.Fatalf("at exactly the ceiling: %v", got)
	}
}

func TestOneOverSourceFails(t *testing.T) {
	root := module(t)
	write(t, root, "internal/app/a.go", 3000)
	write(t, root, "internal/app/b.go", 2001)
	got := check(t, root, "")
	if len(got) != 1 || !strings.Contains(got[0], "internal/app") || !strings.Contains(got[0], "5001") {
		t.Fatalf("got %v", got)
	}
}

func TestOneOverTestFails(t *testing.T) {
	root := module(t)
	write(t, root, "internal/app/a_test.go", 8001)
	if got := check(t, root, ""); len(got) != 1 || !strings.Contains(got[0], "8001") {
		t.Fatalf("got %v", got)
	}
}

func TestRecordRaisesOneColumn(t *testing.T) {
	root := module(t)
	write(t, root, "ui/a_test.go", 9500)
	if got := check(t, root, "size ui - 9800 one test per component\n"); len(got) != 0 {
		t.Fatalf("test-only directory with a test-column record: %v", got)
	}
}

func TestStaleRecordFails(t *testing.T) {
	root := module(t)
	got := check(t, root, "size internal/gone 6000 - was big once\n")
	if len(got) != 1 || !strings.Contains(got[0], "no Go files") {
		t.Fatalf("got %v", got)
	}
}

func TestRatchetFails(t *testing.T) {
	root := module(t)
	write(t, root, "internal/app/a.go", 5500)
	got := check(t, root, "size internal/app 10000 - split owed\n")
	if len(got) != 1 || !strings.Contains(got[0], "lower it") {
		t.Fatalf("5500 is under 60%% of 10000: got %v", got)
	}
}

func TestColumnBackAtDefaultAsksForDash(t *testing.T) {
	root := module(t)
	write(t, root, "internal/app/a.go", 4000)
	got := check(t, root, "size internal/app 5100 - split owed\n")
	if len(got) != 1 || !strings.Contains(got[0], "write -") {
		t.Fatalf("got %v", got)
	}
}

func TestNestedModuleIsNotCountedIntoItsParent(t *testing.T) {
	root := module(t)
	write(t, root, "examples/notes/go.mod", 1)
	write(t, root, "examples/notes/internal/notes/a.go", 6000)
	if got := check(t, root, ""); len(got) != 0 {
		t.Fatalf("parent counted a nested module: %v", got)
	}
	got := check(t, filepath.Join(root, "examples", "notes"), "")
	if len(got) != 1 || !strings.Contains(got[0], "internal/notes") {
		t.Fatalf("nested module measured from its own root: %v", got)
	}
}

func TestSkipsWhatGoSkips(t *testing.T) {
	root := module(t)
	for _, d := range []string{".hidden", "_old", "testdata", "vendor", "node_modules"} {
		write(t, root, d+"/x/a.go", 9000)
	}
	if got := check(t, root, ""); len(got) != 0 {
		t.Fatalf("counted a skipped directory: %v", got)
	}
}
```

- [ ] **Step 2: Run to see them fail**

Run: `GOFLAGS=-mod=mod go test ./internal/budgetsize/`
Expected: FAIL (`undefined: Measure`).

- [ ] **Step 3: Implement `internal/budgetsize/size.go`**

```go
// Package budgetsize holds each directory of a module to a line budget.
//
// It counts directories, not compiled packages: every .go file, whatever
// its build tags, and internal and external test files together. That is
// a deterministic proxy for how much an edit there makes the toolchain
// reread, and Tito Go's internal/instance (957k lines, 72s to link its
// test binary after a one-line edit) is what it is a proxy against.
package budgetsize

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"amadan.net/rastrillo/rastrillo/internal/budgetfile"
)

type Count struct {
	Dir          string
	Source, Test int
}

func Measure(root string) ([]Count, error) {
	by := map[string]*Count{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p == root {
				return nil
			}
			name := d.Name()
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
				name == "testdata" || name == "vendor" || name == "node_modules" {
				return filepath.SkipDir
			}
			// A nested module is its own budget: counting it into the
			// parent would charge the parent for code it does not build.
			if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		lines := bytes.Count(b, []byte("\n"))
		if len(b) > 0 && b[len(b)-1] != '\n' {
			lines++
		}
		rel, err := filepath.Rel(root, filepath.Dir(p))
		if err != nil {
			return err
		}
		dir := filepath.ToSlash(rel)
		c := by[dir]
		if c == nil {
			c = &Count{Dir: dir}
			by[dir] = c
		}
		if strings.HasSuffix(p, "_test.go") {
			c.Test += lines
		} else {
			c.Source += lines
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]Count, 0, len(by))
	for _, c := range by {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dir < out[j].Dir })
	return out, nil
}

func Check(counts []Count, f *budgetfile.File) []string {
	var msgs []string
	seen := map[string]bool{}
	for _, c := range counts {
		seen[c.Dir] = true
		rec := f.Sizes[c.Dir]
		msgs = append(msgs, judge(c.Dir, "source", c.Source, budgetfile.DefaultSource, rec.Source, rec.Line)...)
		msgs = append(msgs, judge(c.Dir, "test", c.Test, budgetfile.DefaultTest, rec.Test, rec.Line)...)
	}
	for dir, rec := range f.Sizes {
		if !seen[dir] {
			msgs = append(msgs, fmt.Sprintf("%s: line %d records a size exemption, but the directory has no Go files: delete the record", dir, rec.Line))
		}
	}
	sort.Strings(msgs)
	return msgs
}

func judge(dir, kind string, measured, def, stated, line int) []string {
	if stated == 0 {
		if measured > def {
			return []string{fmt.Sprintf("%s: %d %s lines, ceiling %d. Split it by feature (docs/site/testing.md), or record an exemption with a reason in %s",
				dir, measured, kind, def, budgetfile.FileName)}
		}
		return nil
	}
	switch {
	case measured > stated:
		return []string{fmt.Sprintf("%s: %d %s lines, over the %d its exemption (line %d) allows", dir, measured, kind, stated, line)}
	case measured <= def:
		return []string{fmt.Sprintf("%s: %d %s lines no longer needs an exemption: write - in the %s column of line %d", dir, measured, kind, kind, line)}
	case measured*10 < stated*6:
		return []string{fmt.Sprintf("%s: measured %d %s lines, exemption says %d: lower it (line %d)", dir, measured, kind, stated, line)}
	}
	return nil
}
```

- [ ] **Step 4: Run the package tests**

Run: `GOFLAGS=-mod=mod go test ./internal/budgetsize/`
Expected: `ok`.

- [ ] **Step 5: Write the CLI test** (`cmd/rastrillo/budget_test.go`)

```go
package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBudgetSizeExitCodes(t *testing.T) {
	root := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/a\n"), 0o644))
	must(os.MkdirAll(filepath.Join(root, "internal", "a"), 0o755))
	must(os.WriteFile(filepath.Join(root, "internal", "a", "a.go"), []byte(strings.Repeat("//\n", 10)), 0o644))

	var out bytes.Buffer
	if err := budgetSize([]string{root}, &out); err != nil {
		t.Fatalf("clean module: %v\n%s", err, out.String())
	}

	must(os.WriteFile(filepath.Join(root, "internal", "a", "b.go"), []byte(strings.Repeat("//\n", 5000)), 0o644))
	out.Reset()
	var ex exitError
	if err := budgetSize([]string{root}, &out); !errors.As(err, &ex) || ex.code != 1 {
		t.Fatalf("over budget: err = %v, want exit 1\n%s", err, out.String())
	}

	must(os.MkdirAll(filepath.Join(root, ".rastrillo"), 0o755))
	must(os.WriteFile(filepath.Join(root, ".rastrillo", "budgets.txt"), []byte("size ./internal/a 6000 - x\n"), 0o644))
	out.Reset()
	if err := budgetSize([]string{root}, &out); !errors.As(err, &ex) || ex.code != 2 {
		t.Fatalf("malformed file: err = %v, want exit 2", err)
	}
}
```

- [ ] **Step 6: Implement `cmd/rastrillo/budget.go` (size half) and dispatch**

```go
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"amadan.net/rastrillo/rastrillo/internal/budgetfile"
	"amadan.net/rastrillo/rastrillo/internal/budgetsize"
)

func runBudget(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: rastrillo budget <size|test> ...")
	}
	switch args[0] {
	case "size":
		return budgetSize(args[1:], os.Stdout)
	case "test":
		return budgetTest(args[1:], ".", os.Stdout, os.Stderr, os.Getenv)
	default:
		return fmt.Errorf("unknown budget subcommand %q (size, test)", args[0])
	}
}

// budgetSize exits 1 for a violation and 2 for a malformed budgets.txt,
// so a runner's log distinguishes "the code grew" from "the file is
// wrong" without anyone reading the output.
func budgetSize(args []string, out io.Writer) error {
	root := "."
	if len(args) > 0 {
		root = args[0]
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return fmt.Errorf("budget size: %s has no go.mod; run it at a module root", root)
	}
	f, err := budgetfile.Load(root)
	if err != nil {
		return exitError{code: 2, msg: err.Error()}
	}
	counts, err := budgetsize.Measure(root)
	if err != nil {
		return err
	}
	msgs := budgetsize.Check(counts, f)
	for _, m := range msgs {
		fmt.Fprintln(out, m)
	}
	if len(msgs) > 0 {
		return exitError{code: 1, msg: fmt.Sprintf("budget size: %d problem(s)", len(msgs))}
	}
	fmt.Fprintf(out, "budget size: %d directories within budget\n", len(counts))
	return nil
}
```

In `cmd/rastrillo/main.go`, add to the switch:

```go
	case "budget":
		err = runBudget(os.Args[2:])
```

and to `usage()` after the `vectors` lines:

```
  rastrillo budget size [dir]                  hold each directory to 5,000 source and 8,000 test lines (default dir: .)
                                                exits 1 over budget, 2 for a malformed .rastrillo/budgets.txt
  rastrillo budget test [-no-time] [-require T,...] [go test args]
                                                run go test -json, judge each package's time, print a receipt
```

Add a stub so the package compiles until Task 5 (Task 5 replaces it):

```go
func budgetTest(args []string, dir string, stdout, stderr io.Writer, getenv func(string) string) error {
	return errors.New("budget test: not yet implemented")
}
```

- [ ] **Step 7: Run**

Run: `GOFLAGS=-mod=mod go test ./cmd/rastrillo/ -run 'TestBudgetSize' && GOFLAGS=-mod=mod go test ./internal/budgetsize/`
Expected: `ok` for both.

- [ ] **Step 8: Commit**

```bash
git add internal/budgetsize cmd/rastrillo/budget.go cmd/rastrillo/budget_test.go cmd/rastrillo/main.go
amadan commit -m "budget size: hold each directory to 5,000 source and 8,000 test lines" -summary "rastrillo budget size walks a module, counts lines per directory, and judges them against budgets.txt with a size ratchet." -body "Every app on this machine is growing one internal package without limit; Tito Go's reached 957k lines and 72s to link a test binary. A ceiling that only fails the gate is the cheapest point to split.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: `budget.Main`, the in-binary measurement

**Files:**
- Create: `budget/budget.go`, `budget/budget_test.go`

**Interfaces:**
- Consumes: `budgetfile.BootChildEnv`.
- Produces: `func Main(m *testing.M) int`; `const Prefix = "rastrillo-budget/v1 ran "`.

- [ ] **Step 1: Failing test** (a real `go test` of a fixture, because `Main` is only meaningful as a binary's `TestMain`)

```go
package budget_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(file))
}

// fixture writes a one-package module whose TestMain calls budget.Main,
// replacing rastrillo with this checkout.
func fixture(t *testing.T, testBody string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/fx\n\ngo 1.25.0\n\nrequire amadan.net/rastrillo/rastrillo v0.0.0\n\nreplace amadan.net/rastrillo/rastrillo => " + repoRoot(t) + "\n",
		"fx_test.go": `package fx

import (
	"os"
	"testing"

	"amadan.net/rastrillo/rastrillo/budget"
)

func TestMain(m *testing.M) { os.Exit(budget.Main(m)) }

` + testBody,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = dir
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}
	return dir
}

func goTest(t *testing.T, dir string, env ...string) (string, error) {
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

var line = regexp.MustCompile(`(?m)^rastrillo-budget/v1 ran (\S+)$`)

func TestMainPrintsOneMeasurementAndKeepsTheCode(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a fixture module")
	}
	dir := fixture(t, "func TestOK(t *testing.T) {}\n")
	out, err := goTest(t, dir)
	if err != nil {
		t.Fatalf("passing fixture failed: %v\n%s", err, out)
	}
	if got := line.FindAllString(out, -1); len(got) != 1 {
		t.Fatalf("want exactly one measurement line, got %q\n%s", got, out)
	}

	dir = fixture(t, "func TestBad(t *testing.T) { t.Fatal(\"no\") }\n")
	out, err = goTest(t, dir)
	if err == nil {
		t.Fatalf("budget.Main turned a failing package green:\n%s", out)
	}
	if !line.MatchString(out) {
		t.Fatalf("a failing package still reports its time:\n%s", out)
	}
}

func TestMainIsSilentInABootChild(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a fixture module")
	}
	dir := fixture(t, "func TestOK(t *testing.T) {}\n")
	out, err := goTest(t, dir, "RASTRILLO_BOOT_CHILD=/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "rastrillo-budget/v1 ran") {
		t.Fatalf("child printed a measurement that would be judged twice:\n%s", out)
	}
}
```

The fixture needs the sandbox-safe Go environment the CLI tests use. If `go mod tidy` reaches for the network, add before `tidy`:

```go
	modcache, _ := exec.Command("go", "env", "GOMODCACHE").Output()
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOPROXY", "file://"+strings.TrimSpace(string(modcache))+"/cache/download")
	t.Setenv("GOSUMDB", "off")
```

(Put these three `t.Setenv` lines at the top of `fixture` unconditionally; it mirrors `setSandboxGoEnv` in `cmd/rastrillo/migration_test.go`.)

- [ ] **Step 2: Run, see it fail**

Run: `GOFLAGS=-mod=mod go test ./budget/`
Expected: FAIL (no package `budget`).

- [ ] **Step 3: Implement `budget/budget.go`**

```go
// Package budget measures how long each test binary takes, so
// `rastrillo budget test` can hold every package to its time budget.
//
//	func TestMain(m *testing.M) { os.Exit(budget.Main(m)) }
//
// Main judges nothing and reads no file. Go records a test's file and
// environment reads in its cache key only between m.Run's start and end;
// a verdict computed out here would leave budgets.txt and the CI switch
// out of that key, and a cached pass could then be replayed under a
// tighter budget. So the binary only prints its time. Go caches that
// line with the rest of a passing package's output and replays it on a
// hit, which is what lets the wrapper judge a cached package on the time
// it took when it really ran: a retry cannot turn a slow package green.
package budget

import (
	"fmt"
	"os"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/internal/budgetfile"
)

// Prefix starts the one line Main prints. The version is there so the
// wrapper can refuse a format it does not understand instead of
// misreading it.
const Prefix = "rastrillo-budget/v1 ran "

// Main runs the package's tests and returns m.Run's code unchanged.
func Main(m *testing.M) int {
	start := time.Now()
	code := m.Run()
	// perftest.Boot's child is the same binary: its line would be judged
	// as a second run of this package, so it stays silent.
	if os.Getenv(budgetfile.BootChildEnv) == "" {
		fmt.Printf("%s%s\n", Prefix, time.Since(start).Round(time.Millisecond))
	}
	return code
}
```

- [ ] **Step 4: Run**

Run: `GOFLAGS=-mod=mod go test ./budget/`
Expected: `ok`.

- [ ] **Step 5: Add `./budget` to `GORM_FREE` in the root `Makefile`** (the line ending `./perf ./lastsignin`): append ` ./budget`. Run `make gorm-free`. Expected: no output, exit 0.

- [ ] **Step 6: Commit**

```bash
git add budget Makefile
amadan commit -m "budget: each test binary prints its own time" -summary "budget.Main times m.Run and prints one versioned line; it never judges." -body "The verdict cannot live in the binary: Go records file and environment reads only inside m.Run, so a budget read in TestMain would be missing from the cache key. A printed time is cached and replayed with the output, so the wrapper can judge cached packages honestly.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: `internal/budgetrun`, reading and judging the event stream

**Files:**
- Create: `internal/budgetrun/events.go`, `internal/budgetrun/judge.go`, `internal/budgetrun/report.go`
- Test: `internal/budgetrun/budgetrun_test.go`

**Interfaces:**
- Consumes: `budgetfile.File`, `budgetfile.DefaultTime`, `budget.Prefix` (copied as a constant here to keep the wrapper free of the `testing` import: `const measurePrefix = "rastrillo-budget/v1 ran "`).
- Produces:
  ```go
  type Package struct {
      ImportPath string
      Result     string        // "pass", "fail", "skip", or "" if never finished
      Cached     bool
      Ran        time.Duration // from the measurement line
      Measured   int           // how many measurement lines were seen
      BadLine    string        // a rastrillo-budget line in an unknown format
      HasTests   bool          // any event with Test set
      Output     string        // package-level output, reassembled
      FailedTests []string
  }
  type Report struct {
      Packages  map[string]*Package
      Order     []string                  // first-seen order
      TestOut   map[string]string         // "pkg TestName" -> output
      Passed    map[string]bool           // top-level test name -> passed somewhere
      Builds    map[string]string         // ImportPath -> build-output
      BuildFail []string
      Malformed int
      Tests     []TestTime                // top-level pass/fail with Elapsed
  }
  type TestTime struct { Name string; Elapsed time.Duration }
  func Read(r io.Reader, raw io.Writer) *Report   // malformed lines are written to raw
  type Options struct {
      File        *budgetfile.File
      ModulePath  string
      NoTime      bool
      Enforce     bool
      Require     []string
      WholeModule bool
  }
  type Verdict struct { Problems, Notes []string }
  func Judge(r *Report, o Options) Verdict
  func Print(w io.Writer, r *Report, v Verdict, wall time.Duration)
  ```

- [ ] **Step 1: Failing tests**

```go
package budgetrun

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/internal/budgetfile"
)

// ev builds one go test -json line.
func ev(action, pkg, test, output string, elapsed float64) string {
	b, _ := json.Marshal(map[string]any{"Action": action, "Package": pkg, "Test": test, "Output": output, "Elapsed": elapsed})
	return string(b) + "\n"
}

func pkgRun(pkg, ran string, cached bool) string {
	summary := "ok  \t" + pkg + "\t0.5s\n"
	if cached {
		summary = "ok  \t" + pkg + "\t(cached)\n"
	}
	return ev("start", pkg, "", "", 0) +
		ev("run", pkg, "TestA", "", 0) +
		ev("pass", pkg, "TestA", "", 0.1) +
		ev("output", pkg, "", "PASS\n", 0) +
		ev("output", pkg, "", "rastrillo-budget/v1 ran "+ran+"\n", 0) +
		ev("output", pkg, "", summary, 0) +
		ev("pass", pkg, "", "", 0.5)
}

func judge(t *testing.T, stream, records string, o Options) Verdict {
	t.Helper()
	f, err := budgetfile.Parse(strings.NewReader(records))
	if err != nil {
		t.Fatal(err)
	}
	o.File = f
	if o.ModulePath == "" {
		o.ModulePath = "example.com/app"
	}
	return Judge(Read(strings.NewReader(stream), new(bytes.Buffer)), o)
}

func TestOverBudgetFailsOnlyWhenEnforcing(t *testing.T) {
	s := pkgRun("example.com/app/internal/a", "12.3s", false)
	if v := judge(t, s, "", Options{Enforce: true}); len(v.Problems) != 1 {
		t.Fatalf("enforcing: %+v", v)
	}
	v := judge(t, s, "", Options{})
	if len(v.Problems) != 0 || len(v.Notes) == 0 {
		t.Fatalf("reporting: %+v", v)
	}
}

// Review focus 1: a cached package is judged on the time it replays.
func TestCachedPackageIsJudgedOnItsReplayedTime(t *testing.T) {
	r := Read(strings.NewReader(pkgRun("example.com/app/internal/a", "12.3s", true)), new(bytes.Buffer))
	if p := r.Packages["example.com/app/internal/a"]; !p.Cached || p.Ran != 12300*time.Millisecond {
		t.Fatalf("package = %+v", p)
	}
	if v := judge(t, pkgRun("example.com/app/internal/a", "12.3s", true), "", Options{Enforce: true}); len(v.Problems) != 1 {
		t.Fatalf("a cached over-budget package passed: %+v", v)
	}
}

func TestTimeRecordRaisesTheLimit(t *testing.T) {
	s := pkgRun("example.com/app/internal/a", "12.3s", false)
	if v := judge(t, s, "time internal/a 15s slow fixture\n", Options{Enforce: true}); len(v.Problems) != 0 {
		t.Fatalf("%+v", v)
	}
}

func TestMissingMeasurementFails(t *testing.T) {
	s := ev("run", "example.com/app/x", "TestA", "", 0) + ev("pass", "example.com/app/x", "TestA", "", 0) + ev("pass", "example.com/app/x", "", "", 0.1)
	v := judge(t, s, "", Options{})
	if len(v.Problems) != 1 || !strings.Contains(v.Problems[0], "budget.Main") {
		t.Fatalf("%+v", v)
	}
}

func TestTwoMeasurementsFail(t *testing.T) {
	s := pkgRun("example.com/app/x", "1s", false)
	s = strings.Replace(s, ev("pass", "example.com/app/x", "", "", 0.5), ev("output", "example.com/app/x", "", "rastrillo-budget/v1 ran 1s\n", 0)+ev("pass", "example.com/app/x", "", "", 0.5), 1)
	if v := judge(t, s, "", Options{}); len(v.Problems) != 1 {
		t.Fatalf("%+v", v)
	}
}

func TestFragmentedMeasurementIsReassembled(t *testing.T) {
	pkg := "example.com/app/x"
	s := ev("run", pkg, "TestA", "", 0) + ev("pass", pkg, "TestA", "", 0) +
		ev("output", pkg, "", "rastrillo-budget/v1 r", 0) + ev("output", pkg, "", "an 2s\n", 0) +
		ev("pass", pkg, "", "", 0.1)
	r := Read(strings.NewReader(s), new(bytes.Buffer))
	if r.Packages[pkg].Ran != 2*time.Second {
		t.Fatalf("%+v", r.Packages[pkg])
	}
}

func TestNoTestFilesAndEmptyRunAreExempt(t *testing.T) {
	s := ev("start", "example.com/app/nofiles", "", "", 0) + ev("output", "example.com/app/nofiles", "", "?   \texample.com/app/nofiles\t[no test files]\n", 0) + ev("skip", "example.com/app/nofiles", "", "", 0) +
		ev("output", "example.com/app/none", "", "ok  \texample.com/app/none\t0.01s [no tests to run]\n", 0) + ev("pass", "example.com/app/none", "", "", 0.01)
	if v := judge(t, s, "", Options{}); len(v.Problems) != 0 {
		t.Fatalf("%+v", v)
	}
}

// Review focus 3: a lane that ran nothing must not pass.
func TestRequire(t *testing.T) {
	s := pkgRun("example.com/app/x", "1s", false)
	if v := judge(t, s, "", Options{Require: []string{"TestA"}}); len(v.Problems) != 0 {
		t.Fatalf("present: %+v", v)
	}
	if v := judge(t, s, "", Options{Require: []string{"TestPerfScreens"}}); len(v.Problems) != 1 {
		t.Fatalf("missing: %+v", v)
	}
	skipped := ev("run", "example.com/app/x", "TestPerfScreens", "", 0) + ev("skip", "example.com/app/x", "TestPerfScreens", "", 0)
	if v := judge(t, skipped, "", Options{Require: []string{"TestPerfScreens"}}); len(v.Problems) != 1 {
		t.Fatalf("skipped: %+v", v)
	}
}

func TestNoTimeTurnsJudgingOff(t *testing.T) {
	s := pkgRun("example.com/app/x", "99s", false)
	if v := judge(t, s, "", Options{NoTime: true, Enforce: true}); len(v.Problems) != 0 {
		t.Fatalf("%+v", v)
	}
}

func TestStaleTimeRecordOnlyOnWholeModule(t *testing.T) {
	s := pkgRun("example.com/app/x", "1s", false)
	rec := "time internal/gone 20s once slow\n"
	if v := judge(t, s, rec, Options{WholeModule: true}); len(v.Problems) != 1 {
		t.Fatalf("whole module: %+v", v)
	}
	if v := judge(t, s, rec, Options{}); len(v.Problems) != 0 {
		t.Fatalf("narrow run: %+v", v)
	}
}

func TestMalformedLinesAreCountedAndEchoed(t *testing.T) {
	var raw bytes.Buffer
	r := Read(strings.NewReader("not json\n"+pkgRun("example.com/app/x", "1s", false)), &raw)
	if r.Malformed != 1 || !strings.Contains(raw.String(), "not json") {
		t.Fatalf("malformed=%d raw=%q", r.Malformed, raw.String())
	}
	if v := judge(t, "not json\n", "", Options{}); len(v.Problems) != 1 {
		t.Fatalf("%+v", v)
	}
}

func TestBuildFailureOutputIsKept(t *testing.T) {
	b, _ := json.Marshal(map[string]any{"Action": "build-output", "ImportPath": "example.com/app/x [example.com/app/x.test]", "Output": "x.go:3:1: syntax error\n"})
	f, _ := json.Marshal(map[string]any{"Action": "build-fail", "ImportPath": "example.com/app/x [example.com/app/x.test]"})
	r := Read(strings.NewReader(string(b)+"\n"+string(f)+"\n"), new(bytes.Buffer))
	var out bytes.Buffer
	Print(&out, r, Verdict{}, time.Second)
	if !strings.Contains(out.String(), "syntax error") {
		t.Fatalf("build output dropped:\n%s", out.String())
	}
}

func TestFailedTestOutputLeadsTheReport(t *testing.T) {
	pkg := "example.com/app/x"
	s := ev("run", pkg, "TestBad", "", 0) + ev("output", pkg, "TestBad", "    x_test.go:9: boom\n", 0) + ev("fail", pkg, "TestBad", "", 0.1) +
		ev("output", pkg, "", "rastrillo-budget/v1 ran 1s\n", 0) + ev("fail", pkg, "", "", 0.2)
	r := Read(strings.NewReader(s), new(bytes.Buffer))
	var out bytes.Buffer
	Print(&out, r, Verdict{}, time.Second)
	if !strings.HasPrefix(strings.TrimSpace(out.String()), "--- FAIL") || !strings.Contains(out.String(), "boom") {
		t.Fatalf("report does not lead with the failure:\n%s", out.String())
	}
}
```

- [ ] **Step 2: Run, see them fail**

Run: `GOFLAGS=-mod=mod go test ./internal/budgetrun/`
Expected: FAIL (undefined symbols).

- [ ] **Step 3: Implement `events.go`**

```go
// Package budgetrun reads a `go test -json` stream and judges it: each
// package's time against its budget, the tests a lane requires, and the
// measurement every package must print.
package budgetrun

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

const measurePrefix = "rastrillo-budget/v1 ran "

var measureLine = regexp.MustCompile(`^rastrillo-budget/v1 ran (\S+)$`)

type Package struct {
	ImportPath  string
	Result      string
	Cached      bool
	Ran         time.Duration
	Measured    int
	BadLine     string
	HasTests    bool
	Output      string
	FailedTests []string
}

type TestTime struct {
	Name    string
	Elapsed time.Duration
}

type Report struct {
	Packages  map[string]*Package
	Order     []string
	TestOut   map[string]string
	Passed    map[string]bool
	Builds    map[string]string
	BuildFail []string
	Malformed int
	Tests     []TestTime
}

type event struct {
	Action     string
	Package    string
	Test       string
	Output     string
	Elapsed    float64
	ImportPath string
}

func Read(r io.Reader, raw io.Writer) *Report {
	rep := &Report{
		Packages: map[string]*Package{},
		TestOut:  map[string]string{},
		Passed:   map[string]bool{},
		Builds:   map[string]string{},
	}
	outs := map[string]*strings.Builder{}
	testOuts := map[string]*strings.Builder{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		var e event
		if err := json.Unmarshal(line, &e); err != nil || (e.Action == "" && e.ImportPath == "") {
			// Something other than go test wrote to stdout. Pass it on so
			// nothing is hidden, and count it so the step fails: a stray
			// writer could just as well have printed a fake event.
			rep.Malformed++
			fmt.Fprintf(raw, "%s\n", line)
			continue
		}
		switch e.Action {
		case "build-output":
			rep.Builds[e.ImportPath] += e.Output
			continue
		case "build-fail":
			rep.BuildFail = append(rep.BuildFail, e.ImportPath)
			continue
		}
		if e.Package == "" {
			continue
		}
		p := rep.Packages[e.Package]
		if p == nil {
			p = &Package{ImportPath: e.Package}
			rep.Packages[e.Package] = p
			rep.Order = append(rep.Order, e.Package)
			outs[e.Package] = new(strings.Builder)
		}
		if e.Test != "" {
			p.HasTests = true
			key := e.Package + " " + e.Test
			if e.Action == "output" {
				if testOuts[key] == nil {
					testOuts[key] = new(strings.Builder)
				}
				testOuts[key].WriteString(e.Output)
			}
			top := !strings.Contains(e.Test, "/")
			switch e.Action {
			case "pass":
				if top {
					rep.Passed[e.Test] = true
					rep.Tests = append(rep.Tests, TestTime{e.Package + " " + e.Test, seconds(e.Elapsed)})
				}
			case "fail":
				if top {
					p.FailedTests = append(p.FailedTests, e.Test)
					rep.Tests = append(rep.Tests, TestTime{e.Package + " " + e.Test, seconds(e.Elapsed)})
				}
			}
			continue
		}
		switch e.Action {
		case "output":
			outs[e.Package].WriteString(e.Output)
		case "pass", "fail", "skip":
			p.Result = e.Action
		}
	}
	for key, b := range testOuts {
		rep.TestOut[key] = b.String()
	}
	for pkg, b := range outs {
		p := rep.Packages[pkg]
		p.Output = b.String()
		// test2json may split a long line across events, so the
		// measurement is read from the reassembled output, a whole line
		// at a time, never from one event's fragment.
		for _, l := range strings.Split(p.Output, "\n") {
			if m := measureLine.FindStringSubmatch(l); m != nil {
				d, err := time.ParseDuration(m[1])
				if err != nil {
					p.BadLine = l
					continue
				}
				p.Ran = d
				p.Measured++
			} else if strings.HasPrefix(l, "rastrillo-budget/") && !strings.HasPrefix(l, "rastrillo-budget/v1 boot-first-byte") {
				p.BadLine = l
			}
			// The (cached) suffix on the package summary is the only
			// reliable signal: on a hit, Elapsed measures the replay.
			if strings.HasPrefix(l, "ok  \t") && strings.HasSuffix(strings.TrimSpace(l), "(cached)") {
				p.Cached = true
			}
		}
	}
	return rep
}

func seconds(f float64) time.Duration { return time.Duration(f * float64(time.Second)) }
```

- [ ] **Step 4: Implement `judge.go`**

```go
package budgetrun

import (
	"fmt"
	"sort"
	"strings"

	"amadan.net/rastrillo/rastrillo/internal/budgetfile"
)

type Options struct {
	File        *budgetfile.File
	ModulePath  string
	NoTime      bool
	Enforce     bool
	Require     []string
	WholeModule bool
}

type Verdict struct {
	Problems []string
	Notes    []string
}

// dirOf turns an import path into the budgets.txt key: the directory
// relative to the module root, "." for the root package.
func dirOf(modulePath, importPath string) string {
	if importPath == modulePath {
		return "."
	}
	return strings.TrimPrefix(importPath, modulePath+"/")
}

func Judge(r *Report, o Options) Verdict {
	var v Verdict
	if r.Malformed > 0 {
		v.Problems = append(v.Problems, fmt.Sprintf("%d line(s) on stdout were not go test events: something else is writing to stdout", r.Malformed))
	}
	seen := map[string]bool{}
	for _, name := range r.Order {
		p := r.Packages[name]
		if !p.HasTests {
			continue // no test files, or -run matched nothing: nothing ran to measure
		}
		dir := dirOf(o.ModulePath, name)
		seen[dir] = true
		if p.BadLine != "" {
			v.Problems = append(v.Problems, fmt.Sprintf("%s: unrecognised budget line %q: is this rastrillo newer than the CLI running it?", name, p.BadLine))
			continue
		}
		if p.Result == "fail" {
			continue // already a failure; go test's own exit status carries it
		}
		switch {
		case p.Measured == 0:
			v.Problems = append(v.Problems, fmt.Sprintf("%s ran tests but printed no time: add func TestMain(m *testing.M) { os.Exit(budget.Main(m)) }", name))
			continue
		case p.Measured > 1:
			v.Problems = append(v.Problems, fmt.Sprintf("%s printed %d time lines; budget.Main must run once per binary", name, p.Measured))
			continue
		}
		if o.NoTime {
			continue
		}
		limit, why := budgetfile.DefaultTime, "the default"
		if rec, ok := o.File.Times[dir]; ok {
			limit, why = rec.Limit, fmt.Sprintf("its record on line %d", rec.Line)
		}
		if p.Ran > limit {
			msg := fmt.Sprintf("%s took %s, over the %s %s allows", dir, p.Ran, limit, why)
			if p.Cached {
				msg += " (measured on the run this cached result came from)"
			}
			if o.Enforce {
				v.Problems = append(v.Problems, msg)
			} else {
				v.Notes = append(v.Notes, msg+"; reported only, timing fails on CI")
			}
		}
	}
	if o.WholeModule && !o.NoTime {
		for dir, rec := range o.File.Times {
			if !seen[dir] {
				v.Problems = append(v.Problems, fmt.Sprintf("line %d: time record for %s, but no package there ran tests: delete it", rec.Line, dir))
			}
		}
	}
	for _, name := range o.Require {
		if !r.Passed[name] {
			v.Problems = append(v.Problems, fmt.Sprintf("required test %s did not pass in this run (missing, skipped or failed)", name))
		}
	}
	sort.Strings(v.Problems)
	return v
}
```

- [ ] **Step 5: Implement `report.go`**

```go
package budgetrun

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// Print writes what failed before anything else, because a runner's log
// is read from the top and Tito Go once printed 200 lines of PASS above
// its only FAIL. Passing output is dropped.
func Print(w io.Writer, r *Report, v Verdict, wall time.Duration) {
	for _, ip := range r.BuildFail {
		fmt.Fprintf(w, "--- BUILD FAILED %s\n%s\n", ip, r.Builds[ip])
	}
	for _, name := range r.Order {
		p := r.Packages[name]
		for _, test := range p.FailedTests {
			fmt.Fprintf(w, "--- FAIL %s %s\n%s\n", name, test, r.TestOut[name+" "+test])
		}
		if p.Result == "fail" && len(p.FailedTests) == 0 {
			// Setup failure, panic, timeout: no named test failed, so
			// the package's own output is the only explanation.
			fmt.Fprintf(w, "--- FAIL %s (no failing test named)\n%s\n", name, p.Output)
		}
	}
	for _, msg := range v.Problems {
		fmt.Fprintf(w, "budget: %s\n", msg)
	}
	for _, msg := range v.Notes {
		fmt.Fprintf(w, "budget note: %s\n", msg)
	}

	var ran, cached, skipped, failed int
	type slow struct {
		name string
		d    time.Duration
	}
	var pkgs []slow
	for _, name := range r.Order {
		p := r.Packages[name]
		switch {
		case p.Result == "fail":
			failed++
		case p.Result == "skip" || !p.HasTests:
			skipped++
		case p.Cached:
			cached++
		default:
			ran++
		}
		if p.Measured == 1 {
			pkgs = append(pkgs, slow{name, p.Ran})
		}
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].d > pkgs[j].d })
	tests := append([]TestTime(nil), r.Tests...)
	sort.Slice(tests, func(i, j int) bool { return tests[i].Elapsed > tests[j].Elapsed })

	fmt.Fprintf(w, "\nreceipt: test step %s against a 60s target (the gate's other steps are not in this figure)\n", wall.Round(time.Millisecond))
	fmt.Fprintf(w, "receipt: %d packages ran, %d cached, %d without tests, %d failed\n", ran, cached, skipped, failed)
	for i := 0; i < len(pkgs) && i < 5; i++ {
		fmt.Fprintf(w, "receipt: slow package %s %s\n", pkgs[i].d, pkgs[i].name)
	}
	for i := 0; i < len(tests) && i < 5; i++ {
		fmt.Fprintf(w, "receipt: slow test %s %s\n", tests[i].Elapsed.Round(time.Millisecond), strings.TrimSpace(tests[i].Name))
	}
}
```

- [ ] **Step 6: Run**

Run: `GOFLAGS=-mod=mod go test ./internal/budgetrun/ && go vet ./internal/budgetrun/`
Expected: `ok`.

- [ ] **Step 7: Commit**

```bash
git add internal/budgetrun
amadan commit -m "budgetrun: judge a go test -json stream" -summary "Per-package time from the replayed measurement line, required tests, stale time records, and a receipt that leads with failures." -body "Judging outside the binary is what keeps the cache honest: the measurement is replayed on a hit, so the verdict is recomputed on every run, including retries.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: `rastrillo budget test`, owning go test's exit status

**Files:**
- Modify: `cmd/rastrillo/budget.go` (replace the stub)
- Modify: `cmd/rastrillo/budget_test.go`

**Interfaces:**
- Consumes: `budgetrun.Read`, `budgetrun.Judge`, `budgetrun.Print`, `budgetfile.ModuleRoot/ModulePath/Load/Enforce`.
- Produces: `func budgetTest(args []string, dir string, stdout, stderr io.Writer, getenv func(string) string) error`; `var goCommand = "go"` (tests point it at a fake).

- [ ] **Step 1: Failing tests** (append to `cmd/rastrillo/budget_test.go`)

```go
// budgetFixture writes a module with one package whose TestMain calls
// budget.Main and whose test sleeps for sleep, replacing rastrillo with
// this checkout.
func budgetFixture(t *testing.T, sleep string, records string) string {
	t.Helper()
	setSandboxGoEnv(t)
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/fx\n\ngo 1.25.0\n\nrequire amadan.net/rastrillo/rastrillo v0.0.0\n\nreplace amadan.net/rastrillo/rastrillo => " + repoRoot(t) + "\n",
		"internal/a/a_test.go": `package a

import (
	"os"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/budget"
)

func TestMain(m *testing.M) { os.Exit(budget.Main(m)) }

func TestSleep(t *testing.T) { d, _ := time.ParseDuration("` + sleep + `"); time.Sleep(d) }
`,
	}
	if records != "" {
		files[".rastrillo/budgets.txt"] = records
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = dir
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}
	return dir
}

func env(kv ...string) func(string) string {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return func(k string) string { return m[k] }
}

// Review focus 1: the retry of an over-budget package, now cached,
// fails exactly as the first run did.
func TestBudgetTestCachedRetryStillFails(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on a fixture module")
	}
	dir := budgetFixture(t, "1200ms", "time internal/a 1s fixture sleeps on purpose\n")
	// A real 1s limit with a 1.2s sleep, so the test stays quick.
	for run := 1; run <= 2; run++ {
		var out, errOut bytes.Buffer
		err := budgetTest([]string{"./..."}, dir, &out, &errOut, env("AMADAN_CI", "1"))
		if err == nil {
			t.Fatalf("run %d passed an over-budget package:\n%s", run, out.String())
		}
		if run == 2 && !strings.Contains(out.String(), "cached") {
			t.Fatalf("second run should be served from the cache and say so:\n%s", out.String())
		}
	}
	var out bytes.Buffer
	if err := budgetTest([]string{"./..."}, dir, &out, io.Discard, env()); err != nil {
		t.Fatalf("without CI it must only report: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "reported only") {
		t.Fatalf("no report line:\n%s", out.String())
	}
}

func TestBudgetTestLoweredRecordFailsTheCachedPackage(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on a fixture module")
	}
	dir := budgetFixture(t, "300ms", "time internal/a 2s fixture\n")
	if err := budgetTest([]string{"./..."}, dir, io.Discard, io.Discard, env("CI", "true")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".rastrillo", "budgets.txt"), []byte("time internal/a 100ms tightened\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := budgetTest([]string{"./..."}, dir, &out, io.Discard, env("CI", "true")); err == nil {
		t.Fatalf("a tightened record must fail the cached package:\n%s", out.String())
	}
}

// Review focus 2: go test's own status wins over whatever the stream
// managed to say before it stopped.
func TestBudgetTestKilledGoTestIsNotGreen(t *testing.T) {
	fake := filepath.Join(t.TempDir(), "go")
	script := "#!/bin/sh\n" +
		`echo '{"Action":"start","Package":"example.com/fx/a"}'` + "\n" +
		`echo '{"Action":"output","Package":"example.com/fx/a","Output":"rastrillo-budget/v1 ran 10ms\n"}'` + "\n" +
		`echo '{"Action":"pass","Package":"example.com/fx/a"}'` + "\n" +
		"kill -9 $$\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/fx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := goCommand
	goCommand = fake
	t.Cleanup(func() { goCommand = old })
	if err := budgetTest([]string{"./..."}, dir, io.Discard, io.Discard, env()); err == nil {
		t.Fatal("a go test killed mid-run read as green")
	}
}

func TestBudgetTestMissingMainFails(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on a fixture module")
	}
	dir := budgetFixture(t, "1ms", "")
	src := filepath.Join(dir, "internal", "a", "a_test.go")
	b, _ := os.ReadFile(src)
	b = []byte(strings.Replace(string(b), "func TestMain(m *testing.M) { os.Exit(budget.Main(m)) }", "var _ = budget.Prefix\nvar _ = os.Exit", 1))
	if err := os.WriteFile(src, b, 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := budgetTest([]string{"./..."}, dir, &out, io.Discard, env()); err == nil || !strings.Contains(out.String(), "budget.Main") {
		t.Fatalf("err=%v\n%s", err, out.String())
	}
}
```

Add `"io"` and `"os/exec"` to the test file's imports.

- [ ] **Step 2: Run, see them fail**

Run: `GOFLAGS=-mod=mod go test ./cmd/rastrillo/ -run 'TestBudgetTest'`
Expected: FAIL ("not yet implemented").

- [ ] **Step 3: Replace the stub in `cmd/rastrillo/budget.go`**

```go
// goCommand is the go binary budget test runs. A variable only so a test
// can stand in a go that dies mid-run.
var goCommand = "go"

// budgetTest runs go test -json itself rather than reading a pipe: the
// shell reports only a pipe's last status, so `go test -json | x` would
// read a go test killed after one passing package as a clean, short
// stream. Here go test's own exit status is the floor of the verdict.
func budgetTest(args []string, dir string, stdout, stderr io.Writer, getenv func(string) string) error {
	var o budgetrun.Options
	for len(args) > 0 {
		switch args[0] {
		case "-no-time":
			o.NoTime = true
			args = args[1:]
			continue
		case "-require":
			if len(args) < 2 {
				return errors.New("budget test: -require needs a comma-separated list of test names")
			}
			o.Require = strings.Split(args[1], ",")
			args = args[2:]
			continue
		}
		break
	}
	if len(args) == 0 {
		args = []string{"./..."}
	}
	o.WholeModule = len(args) == 1 && args[0] == "./..."
	o.Enforce = budgetfile.Enforce(getenv)

	root, err := budgetfile.ModuleRoot(dir)
	if err != nil {
		return err
	}
	if o.File, err = budgetfile.Load(root); err != nil {
		return exitError{code: 2, msg: err.Error()}
	}
	if o.ModulePath, err = budgetfile.ModulePath(root); err != nil {
		return err
	}

	cmd := exec.Command(goCommand, append([]string{"test", "-json"}, args...)...)
	cmd.Dir = dir
	cmd.Stderr = stderr
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return err
	}
	rep := budgetrun.Read(pipe, stdout)
	waitErr := cmd.Wait()
	wall := time.Since(start)

	v := budgetrun.Judge(rep, o)
	budgetrun.Print(stdout, rep, v, wall)
	if waitErr != nil {
		code := 1
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) && ee.ExitCode() > 0 {
			code = ee.ExitCode()
		}
		return exitError{code: code, msg: "budget test: go test failed"}
	}
	if len(v.Problems) > 0 {
		return exitError{code: 1, msg: fmt.Sprintf("budget test: %d problem(s)", len(v.Problems))}
	}
	return nil
}
```

Add imports `"os/exec"`, `"strings"`, `"time"`, `"amadan.net/rastrillo/rastrillo/internal/budgetrun"`.

- [ ] **Step 4: Run**

Run: `GOFLAGS=-mod=mod go test ./cmd/rastrillo/ -run 'TestBudget'`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add cmd/rastrillo/budget.go cmd/rastrillo/budget_test.go
amadan commit -m "budget test: run go test and own its verdict" -summary "rastrillo budget test execs go test -json, keeps its exit status, judges package time and required tests, and prints a receipt." -body "A pipe reports only its last command, so a go test killed after one passing package would read as green. Running go test as a child makes its exit status the floor of the verdict.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: `perftest.Screens`

**Files:**
- Create: `perf/perftest/file.go`, `perf/perftest/screens.go`
- Test: `perf/perftest/screens_test.go`

**Interfaces:**
- Consumes: `budgetfile.ModuleRoot/Load/Enforce`, `perf.DefaultBudget`, `chi.Walk`.
- Produces:
  ```go
  type ScreenConfig struct {
      Routes  chi.Routes
      Handler http.Handler
      Paths   map[string]string       // chi pattern -> concrete path
      Expect  map[string]int          // chi pattern -> status (default 200)
      Signin  func(c *http.Client, base string)
      Opaque  []Opaque
  }
  type Opaque struct { Patterns []string; Screens []OpaqueScreen }
  type OpaqueScreen struct { Key, Path string; Expect int } // Key "GET /bookmarks/{id}"
  func Screens(t testing.TB, cfg ScreenConfig)
  ```
- Internal for tests: `func screens(t testing.TB, cfg ScreenConfig, f *budgetfile.File, enforce bool)`.

- [ ] **Step 1: Failing tests**

```go
package perftest

import (
	"net/http"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"amadan.net/rastrillo/rastrillo/internal/budgetfile"
)

// fakeT records failures without stopping the real test, so a fence
// can be shown going red. FailNow ends the goroutine Screens runs on,
// exactly as testing.T's does.
type fakeT struct {
	testing.TB
	mu     sync.Mutex
	errs   []string
	logs   []string
	failed bool
}

func (f *fakeT) Helper()                      {}
func (f *fakeT) Errorf(s string, a ...any)    { f.mu.Lock(); f.errs = append(f.errs, sprintf(s, a...)); f.failed = true; f.mu.Unlock() }
func (f *fakeT) Fatalf(s string, a ...any)    { f.Errorf(s, a...); runtime.Goexit() }
func (f *fakeT) Fatal(a ...any)               { f.Errorf("%v", a...); runtime.Goexit() }
func (f *fakeT) FailNow()                     { f.failed = true; runtime.Goexit() }
func (f *fakeT) Logf(s string, a ...any)      { f.mu.Lock(); f.logs = append(f.logs, sprintf(s, a...)); f.mu.Unlock() }
func (f *fakeT) Skip(...any)                  { runtime.Goexit() }
func (f *fakeT) Cleanup(fn func())            { fn() }
func (f *fakeT) TempDir() string              { return "" }

func run(cfg ScreenConfig, records string, enforce bool) *fakeT {
	f, err := budgetfile.Parse(strings.NewReader(records))
	if err != nil {
		panic(err)
	}
	ft := &fakeT{}
	done := make(chan struct{})
	go func() { defer close(done); screens(ft, cfg, f, enforce) }()
	<-done
	return ft
}

func router(routes map[string]http.HandlerFunc) chi.Router {
	r := chi.NewRouter()
	for p, h := range routes {
		r.Get(p, h)
	}
	return r
}

func ok(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }

func sleepy(d time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { time.Sleep(d); w.Write([]byte("ok")) }
}

func cfg(r chi.Router) ScreenConfig { return ScreenConfig{Routes: r, Handler: r} }

func TestFastScreenPasses(t *testing.T) {
	if ft := run(cfg(router(map[string]http.HandlerFunc{"/": ok})), "", true); ft.failed {
		t.Fatal(ft.errs)
	}
}

func TestSlowScreenFailsWhenEnforcing(t *testing.T) {
	r := router(map[string]http.HandlerFunc{"/slow": sleepy(200 * time.Millisecond)})
	if ft := run(cfg(r), "", true); !ft.failed {
		t.Fatal("200ms screen passed a 150ms budget")
	}
	if ft := run(cfg(r), "", false); ft.failed {
		t.Fatalf("without CI it must report, not fail: %v", ft.errs)
	}
}

func TestOneSlowRequestInAPassingMedianFails(t *testing.T) {
	var n int
	var mu sync.Mutex
	h := func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		k := n
		mu.Unlock()
		if k == 6 {
			time.Sleep(500 * time.Millisecond) // over 3 x 150ms
		}
		w.Write([]byte("ok"))
	}
	if ft := run(cfg(router(map[string]http.HandlerFunc{"/": h})), "", true); !ft.failed {
		t.Fatal("a 500ms request inside a fast median passed")
	}
}

// TTFB is what the client sees: a header written at once does not
// excuse a body that arrives 300ms later.
func TestEarlyHeaderLateBodyFails(t *testing.T) {
	h := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		time.Sleep(300 * time.Millisecond)
		w.Write([]byte("late"))
	}
	if ft := run(cfg(router(map[string]http.HandlerFunc{"/": h})), "", true); !ft.failed {
		t.Fatal("early WriteHeader hid a 300ms body")
	}
}

func TestErrorPageIsNotMeasured(t *testing.T) {
	h := func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/signin", http.StatusSeeOther) }
	c := cfg(router(map[string]http.HandlerFunc{"/": h}))
	ft := run(c, "", true)
	if !ft.failed || !strings.Contains(strings.Join(ft.errs, " "), "error page") {
		t.Fatalf("303 was measured: %v", ft.errs)
	}
	c.Expect = map[string]int{"/": http.StatusSeeOther}
	if ft := run(c, "", true); ft.failed {
		t.Fatalf("Expect did not accept its status: %v", ft.errs)
	}
}

func TestOpaqueMountMustBeGroupedOrSkipped(t *testing.T) {
	r := chi.NewRouter()
	inner := http.NewServeMux()
	inner.HandleFunc("GET /bookmarks/{id}", ok)
	r.Handle("/bookmarks/*", inner)
	if ft := run(cfg(r), "", true); !ft.failed {
		t.Fatal("an opaque mount with no group passed")
	}
	c := cfg(r)
	c.Opaque = []Opaque{{Patterns: []string{"/bookmarks/*"}, Screens: []OpaqueScreen{{Key: "GET /bookmarks/{id}", Path: "/bookmarks/1"}}}}
	if ft := run(c, "perf GET /bookmarks/{id} 300ms generated screen\n", true); ft.failed {
		t.Fatalf("grouped opaque mount, with a record on its key: %v", ft.errs)
	}
	if ft := run(cfg(r), "perf GET /bookmarks/* skip generated, measured elsewhere\n", true); ft.failed {
		t.Fatalf("skipped opaque mount: %v", ft.errs)
	}
}

func TestStaleGroupPatternFails(t *testing.T) {
	c := cfg(router(map[string]http.HandlerFunc{"/": ok}))
	c.Opaque = []Opaque{{Patterns: []string{"/gone/*"}}}
	if ft := run(c, "", true); !ft.failed {
		t.Fatal("a group naming a pattern chi never reported passed")
	}
}

func TestStalePerfRecordFails(t *testing.T) {
	if ft := run(cfg(router(map[string]http.HandlerFunc{"/": ok})), "perf GET /gone 1s removed screen\n", true); !ft.failed {
		t.Fatal("a record naming no screen passed")
	}
}

func TestParamWithoutPathFails(t *testing.T) {
	if ft := run(cfg(router(map[string]http.HandlerFunc{"/notes/{id}": ok})), "", true); !ft.failed {
		t.Fatal("a {param} route with no Paths entry was skipped silently")
	}
}

func TestEventStreamIsMeasuredToFirstByteAndClosed(t *testing.T) {
	h := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}
	start := time.Now()
	if ft := run(cfg(router(map[string]http.HandlerFunc{"/events": h})), "", true); ft.failed {
		t.Fatalf("SSE: %v", ft.errs)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("SSE route hung the sweep")
	}
}

// Review focus 4.
func TestHandlerIgnoringItsContextStopsTheSweep(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	h := func(w http.ResponseWriter, r *http.Request) { <-block }
	start := time.Now()
	ft := run(cfg(router(map[string]http.HandlerFunc{"/stuck": h, "/zzz": ok})), "", true)
	if !ft.failed {
		t.Fatal("a stuck handler passed")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("sweep took %s; the deadline is 3 x 150ms + 1s plus a 2s close bound", time.Since(start))
	}
}
```

Add a tiny helper at the bottom of the test file: `func sprintf(s string, a ...any) string { return fmt.Sprintf(s, a...) }` and import `"fmt"`.

- [ ] **Step 2: Run, see them fail**

Run: `GOFLAGS=-mod=mod go test ./perf/perftest/`
Expected: FAIL (undefined `screens`).

- [ ] **Step 3: Implement `file.go`**

```go
// Package perftest is the CI half of package perf: it fails a branch
// that makes a screen or a cold start slower than perf's production
// budgets, measured over real HTTP in a serial lane of its own.
package perftest

import (
	"os"
	"testing"

	"amadan.net/rastrillo/rastrillo/internal/budgetfile"
)

// load reads the module's budgets.txt. go test runs a package with its
// directory as the working directory, and the file is read inside the
// test, where Go records it in the cache key (the perf lane runs
// uncached anyway).
func load(t testing.TB) *budgetfile.File {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := budgetfile.ModuleRoot(wd)
	if err != nil {
		t.Fatal(err)
	}
	f, err := budgetfile.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
```

- [ ] **Step 4: Implement `screens.go`**

```go
package perftest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"amadan.net/rastrillo/rastrillo/internal/budgetfile"
	"amadan.net/rastrillo/rastrillo/perf"
)

type ScreenConfig struct {
	Routes  chi.Routes
	Handler http.Handler
	Paths   map[string]string
	Expect  map[string]int
	Signin  func(c *http.Client, base string)
	Opaque  []Opaque
}

type Opaque struct {
	Patterns []string
	Screens  []OpaqueScreen
}

type OpaqueScreen struct {
	Key    string
	Path   string
	Expect int
}

var called atomic.Bool

// Screens is the module's one screen inventory: every GET route the
// router serves is measured, exempted, or skipped with a reason, and a
// record naming no screen fails here, because only here is the list
// complete.
func Screens(t testing.TB, cfg ScreenConfig) {
	t.Helper()
	if !called.CompareAndSwap(false, true) {
		t.Fatal("perftest.Screens called twice in one test binary: it is the module's one inventory, so stale records could not be judged")
	}
	if testing.Short() {
		t.Skip("perf: screens are measured in the perf lane, never under -short")
	}
	screens(t, cfg, load(t), budgetfile.Enforce(os.Getenv))
}

type target struct {
	screen string // "GET /notes/{id}"
	path   string
	expect int
	limit  time.Duration
	exempt bool
}

func screens(t testing.TB, cfg ScreenConfig, f *budgetfile.File, enforce bool) {
	t.Helper()
	targets, errs := inventory(cfg, f)
	for _, e := range errs {
		t.Errorf("%s", e)
	}
	if len(errs) > 0 {
		t.FailNow()
	}

	srv := httptest.NewServer(cfg.Handler)
	defer closeBounded(t, srv)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if cfg.Signin != nil {
		cfg.Signin(client, srv.URL)
	}

	for _, tg := range targets {
		med, worst, err := measure(client, srv.URL+tg.path, tg.expect, tg.limit)
		if errors.Is(err, errHung) {
			// The handler is still running and nothing can stop it; every
			// number after this would include its goroutine.
			srv.CloseClientConnections()
			t.Errorf("%s: no response within %s; a handler that ignores its context stops the sweep here", tg.screen, 3*tg.limit+time.Second)
			t.FailNow()
		}
		if err != nil {
			t.Errorf("%s: %v", tg.screen, err)
			continue
		}
		over := med > tg.limit || worst > 3*tg.limit
		msg := fmt.Sprintf("%s: median %s, worst %s, budget %s", tg.screen, med.Round(time.Millisecond), worst.Round(time.Millisecond), tg.limit)
		switch {
		case over && enforce:
			t.Errorf("%s: over budget", msg)
		case over:
			t.Logf("%s: over budget (reported only; timing fails on CI)", msg)
		case tg.exempt && med*10 < tg.limit*6:
			t.Logf("%s: under 60%% of its record, consider lowering it", msg)
		default:
			t.Logf("%s", msg)
		}
	}
}

func inventory(cfg ScreenConfig, f *budgetfile.File) ([]target, []string) {
	var errs []string
	grouped := map[string]bool{}
	for _, g := range cfg.Opaque {
		for _, p := range g.Patterns {
			grouped[p] = true
		}
	}
	walked := map[string]bool{}
	var routes []string
	err := chi.Walk(cfg.Routes, func(method, route string, h http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method != http.MethodGet {
			return nil
		}
		if !walked[route] {
			walked[route] = true
			routes = append(routes, route)
		}
		return nil
	})
	if err != nil {
		errs = append(errs, "chi.Walk: "+err.Error())
	}
	sort.Strings(routes)

	known := map[string]bool{}
	limit := func(screen string) (time.Duration, bool, bool) {
		if rec, ok := f.Perfs[screen]; ok {
			return rec.Limit, rec.Skip, true
		}
		return perf.DefaultBudget, false, false
	}
	var targets []target
	for _, route := range routes {
		screen := "GET " + route
		known[screen] = true
		lim, skip, exempt := limit(screen)
		if skip {
			continue
		}
		if strings.HasSuffix(route, "/*") || grouped[route] {
			if !grouped[route] {
				errs = append(errs, fmt.Sprintf("%s is mounted handler chi cannot see inside: list its screens in an Opaque group, or record `perf %s skip <reason>`", screen, screen))
			}
			continue
		}
		p := route
		if strings.Contains(route, "{") {
			if cfg.Paths[route] == "" {
				errs = append(errs, fmt.Sprintf("%s has a parameter and no Paths entry: give it a concrete path whose row the test seeds", screen))
				continue
			}
			p = cfg.Paths[route]
		}
		exp := http.StatusOK
		if s, ok := cfg.Expect[route]; ok {
			exp = s
		}
		targets = append(targets, target{screen, p, exp, lim, exempt})
	}
	for _, g := range cfg.Opaque {
		for _, p := range g.Patterns {
			if !walked[p] {
				errs = append(errs, fmt.Sprintf("Opaque group names %s, which the router does not serve: the group is stale", p))
			}
		}
		for _, s := range g.Screens {
			known[s.Key] = true
			lim, skip, exempt := limit(s.Key)
			if skip {
				continue
			}
			exp := s.Expect
			if exp == 0 {
				exp = http.StatusOK
			}
			targets = append(targets, target{s.Key, s.Path, exp, lim, exempt})
		}
	}
	for screen, rec := range f.Perfs {
		if !known[screen] {
			errs = append(errs, fmt.Sprintf("%s line %d: perf record for %s, which is not a screen any more: delete it", budgetfile.FileName, rec.Line, screen))
		}
	}
	sort.Strings(errs)
	return targets, errs
}

var errHung = errors.New("handler did not respond")

// measure makes 10 requests, discards 2, and returns the median and the
// worst of the other 8. TTFB is httptrace's GotFirstResponseByte: what a
// client observes, so an early WriteHeader buys nothing.
func measure(c *http.Client, url string, expect int, limit time.Duration) (time.Duration, time.Duration, error) {
	var got []time.Duration
	for i := 0; i < 10; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 3*limit+time.Second)
		var first time.Time
		ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{GotFirstResponseByte: func() { first = time.Now() }})
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		start := time.Now()
		resp, err := c.Do(req)
		if err != nil {
			cancel()
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return 0, 0, errHung
			}
			return 0, 0, err
		}
		if resp.Header.Get("Content-Type") != "text/event-stream" {
			_, err = io.Copy(io.Discard, resp.Body)
		}
		resp.Body.Close()
		cancel()
		if resp.StatusCode != expect {
			return 0, 0, fmt.Errorf("answered %d, want %d: not measuring an error page (seed its rows, sign in with Signin, or set Expect)", resp.StatusCode, expect)
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			return 0, 0, err
		}
		if i >= 2 {
			got = append(got, first.Sub(start))
		}
	}
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	return (got[3] + got[4]) / 2, got[len(got)-1], nil
}

// closeBounded closes the test server without waiting forever on a
// handler that will not return.
func closeBounded(t testing.TB, srv *httptest.Server) {
	done := make(chan struct{})
	go func() { srv.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Logf("perftest: test server still had a running handler after 2s; leaving it")
	}
}
```

- [ ] **Step 5: Run**

Run: `GOFLAGS=-mod=mod go test ./perf/perftest/ -run 'Test' -count=1`
Expected: `ok`. If `TestEarlyHeaderLateBodyFails` passes the 300ms body, `GotFirstResponseByte` is firing on headers flushed early by the server: in that case set `first` in the trace only when the body's first byte is read instead (wrap `resp.Body` and stamp on the first `Read` returning n>0), and keep the test.

- [ ] **Step 5b: Pin what `chi.Walk` reports for `Mount`.** `r.Mount("/bookmarks", someServeMux)` registers stub routes (`/bookmarks`, `/bookmarks/`) beside `/bookmarks/*` (chi v5.3.2 `mux.go` `Mount`). Read `walk` in `tree.go` to see whether stubs are reported, then add a test mounting an `http.ServeMux` with `r.Mount` and assert the inventory error names every pattern that must go in the `Opaque` group. If stubs are reported, treat a walked route whose handler is the same mount handler as the `/*` route as opaque too, and say so in a comment; the docs (Task 11) list the patterns an app writes for a `Mount`.

- [ ] **Step 6: Add `./perf/perftest` to `GORM_FREE`**, then `make gorm-free`. Run `go mod tidy` at the root (chi moves from indirect to direct) and check `git diff go.mod` shows only that.

- [ ] **Step 7: Commit**

```bash
git add perf/perftest go.mod go.sum Makefile
amadan commit -m "perftest: gate every screen at perf's budget in CI" -summary "Screens walks the chi router, measures each GET over real HTTP, and fails over-budget screens on CI with opaque mounts, Expect and stale records handled." -body "perf already measures 150ms in production but nothing failed a branch that made a screen slow. Measuring over HTTP charges a handler for what the client actually waits for.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: `perftest.Boot`

**Files:**
- Create: `perf/perftest/boot.go`
- Test: `perf/perftest/boot_test.go`

**Interfaces:**
- Consumes: `budgetfile.BootChildEnv`, `perf.DefaultColdBudget`, `load`.
- Produces:
  ```go
  type BootConfig struct {
      Prepare func(t testing.TB) string                          // a migrated, closed database; parent only
      Build   func(dbPath string) (http.Handler, func(), error)  // child only
      Path    string
      Expect  int                                                // default 200
  }
  func Boot(t *testing.T, cfg BootConfig)
  const BootMarker = "rastrillo-budget/v1 boot-first-byte"
  ```

- [ ] **Step 1: Failing tests** (the test binary is its own child, so these tests are top-level tests that call `Boot`)

```go
package perftest

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var bootCounter int // package-level: zero in every fresh process

func prepare(t testing.TB) string {
	p := filepath.Join(t.TempDir(), "db")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func fastBuild(string) (http.Handler, func(), error) {
	bootCounter++
	if bootCounter != 1 {
		return nil, nil, errNotFresh
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }), func() {}, nil
}

var errNotFresh = os.ErrExist

func TestBootFreshProcessPasses(t *testing.T) {
	if os.Getenv("PERFTEST_BOOT_SLOW") != "" || os.Getenv("PERFTEST_BOOT_HANG") != "" {
		t.Skip("parent of another case")
	}
	Boot(t, BootConfig{Prepare: prepare, Build: fastBuild, Path: "/"})
}

// The fixtures below run their own child; the parent asserts on the
// outcome by running this test binary with a selector variable.
func TestBootSlowChild(t *testing.T) {
	if os.Getenv("PERFTEST_BOOT_SLOW") == "" {
		t.Skip("run by TestBootFailsSlowStartUnderCI")
	}
	Boot(t, BootConfig{Prepare: prepare, Path: "/", Build: func(string) (http.Handler, func(), error) {
		time.Sleep(600 * time.Millisecond)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), func() {}, nil
	}})
}

func TestBootHangingChild(t *testing.T) {
	if os.Getenv("PERFTEST_BOOT_HANG") == "" {
		t.Skip("run by TestBootKillsAHungChild")
	}
	Boot(t, BootConfig{Prepare: prepare, Path: "/", Build: func(string) (http.Handler, func(), error) {
		// Sleep, not select {}: with every goroutine blocked the runtime
		// would kill the child as a deadlock at once, and the parent's
		// 10s kill path would go untested.
		time.Sleep(time.Hour)
		return nil, nil, nil
	}})
}

func runSelf(t *testing.T, name string, env ...string) (string, error) {
	cmd := exec.Command(os.Args[0], "-test.run=^"+name+"$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestBootFailsSlowStartUnderCI(t *testing.T) {
	if testing.Short() || os.Getenv(budgetfileBootChildEnv()) != "" {
		t.Skip("spawns processes")
	}
	out, err := runSelf(t, "TestBootSlowChild", "PERFTEST_BOOT_SLOW=1", "CI=true")
	if err == nil {
		t.Fatalf("a 600ms start passed a 500ms budget under CI:\n%s", out)
	}
	if _, err := runSelf(t, "TestBootSlowChild", "PERFTEST_BOOT_SLOW=1", "CI=", "AMADAN_CI="); err != nil {
		t.Fatal("without CI a slow start must only report")
	}
}

func TestBootKillsAHungChild(t *testing.T) {
	if testing.Short() || os.Getenv(budgetfileBootChildEnv()) != "" {
		t.Skip("spawns processes")
	}
	start := time.Now()
	out, err := runSelf(t, "TestBootHangingChild", "PERFTEST_BOOT_HANG=1")
	if err == nil {
		t.Fatalf("a child that never answered passed:\n%s", out)
	}
	if time.Since(start) > 40*time.Second {
		t.Fatalf("hung child was not killed at 10s (took %s)", time.Since(start))
	}
}
```

Add imports `"os/exec"` and a helper `func budgetfileBootChildEnv() string { return budgetfile.BootChildEnv }` with the `budgetfile` import.

`TestBootFreshProcessPasses` proves freshness: `fastBuild` errors if the counter is not 1, which can only hold in a new process for each of the three children.

- [ ] **Step 2: Run, see them fail**

Run: `GOFLAGS=-mod=mod go test ./perf/perftest/ -run 'TestBoot' -count=1`
Expected: FAIL (undefined `Boot`).

- [ ] **Step 3: Implement `boot.go`**

```go
package perftest

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/internal/budgetfile"
	"amadan.net/rastrillo/rastrillo/perf"
)

const BootMarker = "rastrillo-budget/v1 boot-first-byte"

type BootConfig struct {
	Prepare func(t testing.TB) string
	Build   func(dbPath string) (http.Handler, func(), error)
	Path    string
	Expect  int
}

// Boot measures a cold start: a new process, from exec to the first byte
// of its first response. It re-executes this test binary running only
// the calling test; the child sees BootChildEnv and does nothing but
// start. Call it from a top-level test.
func Boot(t *testing.T, cfg BootConfig) {
	t.Helper()
	if cfg.Expect == 0 {
		cfg.Expect = http.StatusOK
	}
	if p := os.Getenv(budgetfile.BootChildEnv); p != "" {
		child(t, cfg, p) // the child branch never spawns: the recursion guard
		return
	}
	if testing.Short() {
		t.Skip("perf: boot is measured in the perf lane, never under -short")
	}
	f := load(t)
	limit := perf.DefaultColdBudget
	if f.Boot != nil {
		limit = f.Boot.Limit
	}
	// Prepared here, in the parent, so no child migrates or builds a
	// dbtest template inside the window being measured.
	src := cfg.Prepare(t)
	var got []time.Duration
	for i := 0; i < 3; i++ {
		dst := filepath.Join(t.TempDir(), fmt.Sprintf("boot-%d.db", i))
		if err := copyFile(src, dst); err != nil {
			t.Fatal(err)
		}
		d, err := spawn(t.Name(), dst)
		if err != nil {
			t.Fatalf("boot child %d: %v", i, err)
		}
		got = append(got, d)
	}
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	med := got[1]
	msg := fmt.Sprintf("cold boot: median %s of %v, budget %s", med.Round(time.Millisecond), got, limit)
	switch {
	case med > limit && budgetfile.Enforce(os.Getenv):
		t.Errorf("%s: over budget", msg)
	case med > limit:
		t.Logf("%s: over budget (reported only; timing fails on CI)", msg)
	default:
		t.Logf("%s", msg)
	}
}

func spawn(test, db string) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+regexp.QuoteMeta(test)+"$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), budgetfile.BootChildEnv+"="+db)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, err
	}
	var rest strings.Builder
	cmd.Stderr = &rest
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	var at time.Duration
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		if sc.Text() == BootMarker && at == 0 {
			at = time.Since(start)
			continue
		}
		rest.WriteString(sc.Text() + "\n")
	}
	io.Copy(io.Discard, stdout)
	werr := cmd.Wait()
	switch {
	case ctx.Err() != nil:
		return 0, fmt.Errorf("killed after 10s without finishing:\n%s", rest.String())
	case at == 0:
		return 0, fmt.Errorf("never printed its first-byte line (status %v):\n%s", werr, rest.String())
	case werr != nil:
		return 0, fmt.Errorf("exited %v after its first byte:\n%s", werr, rest.String())
	}
	return at, nil
}

func child(t *testing.T, cfg BootConfig, db string) {
	h, cleanup, err := cfg.Build(db)
	if err != nil {
		t.Fatalf("boot child: build: %v", err)
	}
	defer cleanup()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h}
	go srv.Serve(ln)
	defer srv.Close()
	var once bool
	ctx := httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{GotFirstResponseByte: func() {
		if !once {
			once = true
			// Printed straight to stdout, unbuffered: the parent's clock
			// stops when it reads this line.
			fmt.Fprintln(os.Stdout, BootMarker)
		}
	}})
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+ln.Addr().String()+cfg.Path, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("boot child: request: %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != cfg.Expect {
		t.Fatalf("boot child: %s answered %d, want %d", cfg.Path, resp.StatusCode, cfg.Expect)
	}
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}
```

- [ ] **Step 4: Run**

Run: `GOFLAGS=-mod=mod go test ./perf/perftest/ -count=1`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add perf/perftest
amadan commit -m "perftest: gate cold boot at perf's 500ms" -summary "Boot re-executes the test binary as a fresh child and times exec to first response byte, three times, median against the budget." -body "A hibernated instance pays its whole start on someone's click. Measuring in-process would miss package init and reuse warm globals, so the only honest cold start is a new process.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: scaffold templates

**Files:**
- Modify: `cmd/rastrillo/new.go` (`appTemplate`, `mainTemplate`, `harnessTemplate`, `indexTestTemplate`, `makefileTemplate`, `agentsMDTemplate`, the files map, the `ciScripts` map, the printed summary)
- Create in `new.go`: `perfTestTemplate`, `budgetsTxtTemplate`
- Modify: `cmd/rastrillo/new_scaffold_test.go`, `cmd/rastrillo/new_test.go` (assertions)

**Interfaces:**
- Consumes: `budget.Main`, `dbtest.FromSet`, `perftest.Screens/Boot`, `perf.Middleware`, `rastrillo.Handler`.
- Produces (in every scaffolded app's `internal/<pkg>` package):
  ```go
  func Router(d *db.DB, origin string, logger *slog.Logger) (chi.Router, error)
  func Mux(r http.Handler) *http.ServeMux
  func App(d *db.DB, origin string, logger *slog.Logger) (*http.ServeMux, error) // Router then Mux
  func Configure(opts *rastrillo.Options, mux *http.ServeMux, started time.Time)
  ```

- [ ] **Step 1: Failing assertions** in `cmd/rastrillo/new_scaffold_test.go` `TestNewScaffoldsCIAndManifest`: change the exact string to

```go
	if !strings.Contains(string(mk), "ci: vet fmt-check staticcheck budget test perf migration-check") {
```

and add `.amadan/ci.d/15-budget` and `.amadan/ci.d/35-perf` to both lists (existence and executable). Add:

```go
	for _, want := range []string{
		".NOTPARALLEL:",
		"budget test ./...",
		"budget test -no-time -require TestPerfScreens,TestPerfBoot -tags perf -count=1 -p 1 -parallel 1 -run '^TestPerf' ./...",
		"-tags browser,perf ./...",
		"budget size",
	} {
		if !strings.Contains(string(mk), want) {
			t.Errorf("Makefile missing %q", want)
		}
	}
	for _, rel := range []string{".rastrillo/budgets.txt", "internal/demoapptest/perf_test.go"} {
		if _, err := os.Stat(filepath.Join("demoapp", rel)); err != nil {
			t.Errorf("scaffold missing %s: %v", rel, err)
		}
	}
	harness, _ := os.ReadFile(filepath.Join("demoapp", "internal", "demoapptest", "harness_test.go"))
	for _, want := range []string{"dbtest.FromSet(demoapp.BootSchema)", "budget.Main(m)", "schema.Remove()"} {
		if !strings.Contains(string(harness), want) {
			t.Errorf("harness missing %q", want)
		}
	}
	mainGo, _ := os.ReadFile(filepath.Join("demoapp", "cmd", "demoapp", "main.go"))
	if !strings.Contains(string(mainGo), "demoapp.Configure(&opts, mux, started)") {
		t.Errorf("main.go must set serving options through Configure:\n%s", mainGo)
	}
```

Run: `GOFLAGS=-mod=mod go test ./cmd/rastrillo/ -run TestNewScaffoldsCIAndManifest`
Expected: FAIL.

- [ ] **Step 2: `appTemplate`**: replace the `App` function with the three functions below and add imports `"time"`, `"amadan.net/rastrillo/rastrillo"`, `"amadan.net/rastrillo/rastrillo/perf"`. Keep the existing comments on BootSchema and static files where they apply.

```go
// App wires the whole app: schema, router, static files. Router and Mux
// are its two halves, separate only so the perf test can walk the chi
// routes while measuring through the same handler production serves.
func App(d *db.DB, origin string, logger *slog.Logger) (*http.ServeMux, error) {
	r, err := Router(d, origin, logger)
	if err != nil {
		return nil, err
	}
	return Mux(r), nil
}

// Router applies the schema and builds the routes.
func Router(d *db.DB, origin string, logger *slog.Logger) (chi.Router, error) {
	// (existing BootSchema comment)
	migrated, err := migrate.Apply(context.Background(), d, BootSchema)
	if err != nil {
		return nil, err
	}
	// (existing migrate log comment)
	logger.Info("migrate", "applied", migrated.Applied,
		"skipped", migrated.Skipped, "adopted", migrated.Adopted)

	a := &app{db: d, logger: logger}

	r := chi.NewRouter()
	// (existing csrf comment)
	r.Use(csrf.Protect(origin))
	r.Get("/", a.index)
	return r, nil
}

// Mux mounts static files beside the router.
func Mux(r http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	// (existing static comment)
	mux.Handle("GET /static/", assets.Handler())
	mux.Handle("/", r)
	return mux
}

// Configure is the one place the serving options are set, so main.go and
// the perf test cannot drift apart: what the perf lane measures is what
// production serves. perf.Middleware times every request against 150ms to
// first byte (500ms for the first after start) and logs what is over.
func Configure(opts *rastrillo.Options, mux *http.ServeMux, started time.Time) {
	opts.Mux = mux
	opts.ErrorPage = ErrorPage
	var rec perf.Recorder
	opts.Wrap = perf.Middleware(&rec, perf.Options{Started: started, Logger: opts.Logger})
}
```

In the template string, `%` must be written `%%` and `%[1]s` stays the package name. The block above contains no `%`. Check `perf.Middleware` with a nil `Options.Logger` (read `perf/perf.go`): if it does not fall back to `slog.Default()`, pass `opts.Logger` only when non-nil.

- [ ] **Step 3: `mainTemplate`**: add `"time"` to imports; first line of `main()` becomes `started := time.Now()` with the comment `// first, so perf's cold budget counts the whole start`; replace

```go
	opts.Mux = mux
	opts.DBPath = ""
	// ... ErrorPage comment ...
	opts.ErrorPage = %[2]s.ErrorPage
```

with

```go
	%[2]s.Configure(&opts, mux, started)
	opts.DBPath = ""
```

and move the ErrorPage comment into `Configure`'s doc in `appTemplate`.

- [ ] **Step 4: `harnessTemplate`**: add imports `"os"`, `"amadan.net/rastrillo/rastrillo/budget"`, `"amadan.net/rastrillo/rastrillo/dbtest"`; remove `"path/filepath"`; add before `newApp`:

```go
// schema is migrated once per test binary and copied for each test:
// migrating a fresh database in every test is the cost Tito Go measured
// at 207ms a test against 2.5ms for a copy.
var schema = dbtest.FromSet(%[2]s.BootSchema)

// TestMain prints this package's time for rastrillo budget test, then
// removes the template, which outlives every test that copied it.
func TestMain(m *testing.M) {
	code := budget.Main(m)
	schema.Remove()
	os.Exit(code)
}
```

and in `newApp` replace `db.Open(filepath.Join(t.TempDir(), "app.db"), logger)` with `db.Open(schema.Path(t), logger)`.

- [ ] **Step 5: `indexTestTemplate`**: insert `t.Parallel()` as the first statement of each of the four tests, preceded once (above `TestIndexRenders`) by:

```go
// t.Parallel() is each test's first statement: anything before it runs
// serially, and a fixture built there is held for the whole run.
```

- [ ] **Step 6: new `perfTestTemplate`** (written to `internal/<pkg>test/perf_test.go`; args `%[1]s` module name, `%[2]s` package):

```go
const perfTestTemplate = `//go:build perf

package %[2]stest

import (
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/perf/perftest"

	%[2]s "%[1]s/internal/%[2]s"
)

// The perf lane: make perf, serial and uncached, failing only on CI.
// Every GET screen the router serves is held to 150ms to first byte;
// a new route fails here until it is measured or has a record in
// .rastrillo/budgets.txt. Never t.Parallel() in this file: a perf test
// sharing the machine measures its neighbours.
func TestPerfScreens(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d, err := db.Open(schema.Path(t), logger)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	r, err := %[2]s.Router(d, testOrigin, logger)
	if err != nil {
		t.Fatal(err)
	}
	opts := rastrillo.Options{Logger: logger}
	%[2]s.Configure(&opts, %[2]s.Mux(r), time.Now())
	h, closeAll, err := rastrillo.Handler(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer closeAll()
	perftest.Screens(t, perftest.ScreenConfig{Routes: r, Handler: h})
}

// A cold start, from exec to the first byte of "/", held to 500ms.
func TestPerfBoot(t *testing.T) {
	perftest.Boot(t, perftest.BootConfig{
		Prepare: schema.Path,
		Build: func(path string) (http.Handler, func(), error) {
			started := time.Now()
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			d, err := db.Open(path, logger)
			if err != nil {
				return nil, nil, err
			}
			mux, err := %[2]s.App(d, testOrigin, logger)
			if err != nil {
				d.Close()
				return nil, nil, err
			}
			opts := rastrillo.Options{Logger: logger}
			%[2]s.Configure(&opts, mux, started)
			h, closeAll, err := rastrillo.Handler(opts)
			if err != nil {
				d.Close()
				return nil, nil, err
			}
			return h, func() { closeAll(); d.Close() }, nil
		},
		Path: "/",
	})
}
`
```

- [ ] **Step 7: `budgetsTxtTemplate`** (written to `.rastrillo/budgets.txt`; create the `.rastrillo` dir in the `dirs` list):

```go
const budgetsTxtTemplate = `# Exceptions to this app's budgets. Empty is the goal.
#
# Defaults: 5,000 source and 8,000 test lines per directory; 10s per
# test package; 150ms to first byte per GET screen; 500ms cold boot.
# A record exists only where something cannot meet them, and every
# record says why. docs/site/testing.md has the full grammar.
#
#   size  <dir>             <source>|-  <test>|-  <reason>
#   time  <dir>             <duration>            <reason>
#   perf  <METHOD> <path>   <duration>|skip       <reason>
#   boot                    <duration>            <reason>
`
```

- [ ] **Step 8: `makefileTemplate`**: add `.NOTPARALLEL:` with its comment directly above `.PHONY`, add `budget perf` to `.PHONY`, and replace the `test:` target, staticcheck tags and `ci:` line:

```make
# One target at a time, even under make -j: the perf lane measures time,
# and a suite running beside it would be measured too.
.NOTPARALLEL:

RASTRILLO := go run amadan.net/rastrillo/rastrillo/cmd/rastrillo

# budget size holds each directory to 5,000 source and 8,000 test lines.
# Past that, split by feature (docs/site/testing.md); a record in
# .rastrillo/budgets.txt with a reason is the exception, not the fix.
budget:
	$(RASTRILLO) budget size

# budget test runs go test itself, so its exit status survives, and holds
# each package to 10s using the time budget.Main prints in the package's
# TestMain. Timing fails only on CI (AMADAN_CI or CI set); here it reports.
test:
	$(RASTRILLO) budget test ./...

# The perf lane: every GET screen at 150ms to first byte, cold boot at
# 500ms. Serial (-p 1 -parallel 1) and uncached (-count=1), because a
# measurement taken beside other work, or replayed from a cache, is not
# one. -require fails the lane if the perf tests did not both run.
perf:
	$(RASTRILLO) budget test -no-time -require TestPerfScreens,TestPerfBoot -tags perf -count=1 -p 1 -parallel 1 -run '^TestPerf' ./...
```

`staticcheck:` recipe becomes `go run $(STATICCHECK) -tags browser,perf ./...` with the comment amended: "-tags browser,perf so the browser drive and the perf tests are read too".

`ci:` becomes `ci: vet fmt-check staticcheck budget test perf migration-check`.

- [ ] **Step 9: files and steps maps**: add to `files`:

```go
		filepath.Join(name, "internal", pkg+"test", "perf_test.go"): fmt.Sprintf(perfTestTemplate, name, pkg),
		filepath.Join(name, ".rastrillo", "budgets.txt"):            budgetsTxtTemplate,
```

add `filepath.Join(name, ".rastrillo")` to `dirs`; add to `ciScripts`:

```go
		filepath.Join(name, ".amadan", "ci.d", "15-budget"): amadanStep("budget"),
		filepath.Join(name, ".amadan", "ci.d", "35-perf"):   amadanStep("perf"),
```

and update the printed summary line for the Makefile to `(make ci = vet + fmt + staticcheck + budget + test + perf + migration check, the one gate definition;`.

- [ ] **Step 10: `agentsMDTemplate`**: add a `## Testing` section (find the section in the template that mentions `make ci`, put this after it):

```markdown
## Testing

- While editing: `go test -short ./internal/<app>/... ./internal/<app>test/`.
- Before pushing: `make ci`. If it is too slow to run before every push,
  a budget has already failed: fix that, never skip the gate.
- Budgets: 5,000 source and 8,000 test lines per directory (`make budget`),
  10s per test package, 150ms per GET screen and 500ms cold boot (`make
  perf`). Exceptions go in `.rastrillo/budgets.txt`, each with a reason.
- Past 5,000 lines, split `internal/<app>` by feature:
  `internal/<app>/<feature>` with `Mount(r chi.Router, deps)`.
- Every test package has `func TestMain(m *testing.M) {
  os.Exit(budget.Main(m)) }`, and every test starts with `t.Parallel()`
  (perf tests excepted).
```

Write `<app>` literally only if the template has no package placeholder in scope; otherwise use the template's package argument.

- [ ] **Step 11: Run**

Run: `GOFLAGS=-mod=mod go test ./cmd/rastrillo/ -run 'TestNew|TestScaffold' -count=1`
Expected: `ok`, including `TestScaffoldedAppTestsPass` (the scaffold's own `go test ./...` now runs through `dbtest` and `budget.Main`) and `TestScaffoldMigratesAndPassesCheck`.

- [ ] **Step 12: Commit**

```bash
git add cmd/rastrillo
amadan commit -m "rastrillo new: budgets, dbtest and the perf lane from the first commit" -summary "Scaffolded apps split App into Router/Mux/Configure, mount perf.Middleware, migrate once per test binary, print package time, and run budget, test and perf in make ci." -body "Every app on this machine reached its slow suite the same way: one package, a fresh migration per test, nothing parallel, and no ceiling. A new app now starts inside its budgets, with the gate that keeps it there.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: Rastrillo's own gate

**Files:**
- Create: `.rastrillo/budgets.txt`, `.amadan/ci.d/12-budget` (executable)
- Modify: `Makefile`

- [ ] **Step 1: Measure today's numbers**

Run: `go run ./cmd/rastrillo budget size`
Expected: exit 1 naming `internal/designsystem` (source) and `ui` (test) and nothing else. Note the two counts it prints.

- [ ] **Step 2: Write `.rastrillo/budgets.txt`** with each stated ceiling about 8% above its count (so ordinary growth does not trip it and the 60% ratchet still holds), for example, with today's counts 9,720 and 19,251:

```
# Rastrillo's own exceptions. The ratchet holds both: a stated ceiling
# more than 40% above its count fails until it is lowered.
size internal/designsystem 10500 -     the design-system site generator: one sample per ui component, split by section owed
size ui                    -     20800 one golden test per component partial; grows with the vocabulary, by design
```

Run: `go run ./cmd/rastrillo budget size`
Expected: `budget size: N directories within budget`, exit 0.

- [ ] **Step 3: Makefile**: add `budget` to `.PHONY` and to the front of `ci:` (after `gofmt`), and add:

```make
# Each directory at 5,000 source and 8,000 test lines, the budget every
# scaffolded app carries; .rastrillo/budgets.txt records the exceptions.
# The examples are separate modules, each measured from its own root.
budget:
	go run ./cmd/rastrillo budget size
	@for e in $(EXAMPLES); do go run ./cmd/rastrillo budget size examples/$$e || exit 1; done
```

- [ ] **Step 4: Step file** `.amadan/ci.d/12-budget`:

```sh
#!/bin/sh
exec make budget
```

`chmod +x .amadan/ci.d/12-budget`.

- [ ] **Step 5: Run**

Run: `make budget`
Expected: exit 0. If an example is over, record it in that example's own `.rastrillo/budgets.txt` with a reason, and say so in the commit body.

- [ ] **Step 6: Commit**

```bash
git add .rastrillo .amadan/ci.d/12-budget Makefile
amadan commit -m "make budget: Rastrillo holds itself to the scaffold's ceiling" -summary "budget size over the root module and each example, with two recorded exceptions." -body "A framework that tells apps to stay under 5,000 lines a directory should be measured the same way; the two directories over it today are recorded with reasons and ratcheted.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 10: the scaffold's whole `make ci`, offline

**Files:**
- Test: `cmd/rastrillo/new_scaffold_test.go` (new test)

- [ ] **Step 1: Write the test**

```go
// TestScaffoldWholeGatePassesOffline is the end-to-end proof the budgets
// spec asks for: a fresh app's entire make ci (vet, fmt, staticcheck,
// budget, test, perf, migration-check) passes with only the Go toolchain
// on PATH, a readonly module graph, and no network. Anything a step
// fetched at run time, or a step that quietly did nothing, fails here.
func TestScaffoldWholeGatePassesOffline(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a scaffolded app's whole gate")
	}
	setSandboxGoEnv(t)
	root := repoRoot(t)
	t.Chdir(t.TempDir())
	if err := runNew([]string{"gateapp"}); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join("gateapp", "go.mod"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("\nreplace amadan.net/rastrillo/rastrillo => " + root + "\n")
	f.Close()
	for _, args := range [][]string{{"mod", "tidy"}, {"mod", "download"}} {
		c := exec.Command("go", args...)
		c.Dir = "gateapp"
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("go %v: %v\n%s", args, err, out)
		}
	}
	// staticcheck is fetched by go run at a pinned version: warm it into
	// the module cache while the proxy is still reachable.
	warm := exec.Command("go", "run", "honnef.co/go/tools/cmd/staticcheck@"+staticcheckVersion, "-version")
	warm.Dir = "gateapp"
	if out, err := warm.CombinedOutput(); err != nil {
		t.Fatalf("warming staticcheck: %v\n%s", err, out)
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	mk := exec.Command("make", "ci")
	mk.Dir = "gateapp"
	mk.Env = append(os.Environ(),
		"PATH="+filepath.Dir(goBin)+":/usr/bin:/bin",
		"GOFLAGS=-mod=readonly",
		"GOPROXY=off",
		// Timing reports rather than fails here: this proves the gate's
		// wiring, and a dev box's load is not evidence about speed.
		"AMADAN_CI=",
		"CI=",
	)
	out, err := mk.CombinedOutput()
	if err != nil {
		t.Fatalf("make ci on a fresh scaffold:\n%s", out)
	}
	if !strings.Contains(string(out), "budget size:") {
		t.Errorf("make ci did not run budget size:\n%s", out)
	}
	// One receipt from the suite and one from the perf lane: both steps
	// ran through budget test, and -require passed in the perf lane.
	if n := strings.Count(string(out), "receipt: test step"); n != 2 {
		t.Errorf("want 2 receipts (test, perf), got %d:\n%s", n, out)
	}
	if strings.Contains(string(out), "required test") {
		t.Errorf("the perf lane's required tests did not both pass:\n%s", out)
	}
}
```

- [ ] **Step 2: Run**

Run: `GOFLAGS=-mod=mod go test ./cmd/rastrillo/ -run TestScaffoldWholeGatePassesOffline -count=1 -timeout 15m`
Expected: `ok`. A failure here is a real gap in Task 8: fix the template, not the test.

- [ ] **Step 3: Commit**

```bash
git add cmd/rastrillo/new_scaffold_test.go
amadan commit -m "Prove a fresh scaffold's whole make ci passes offline" -summary "End-to-end: vet, fmt, staticcheck, budget, test, perf and migration-check on a new app, readonly and with GOPROXY=off." -body "Each step had a narrower test, and none of them proved the gate as a whole: a step that fetched at run time, or ran nothing, would have passed them all.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 11: docs

**Files:**
- Create: `docs/site/reference/budget.md`, `docs/site/reference/perftest.md`
- Modify: `internal/docsite/symbols_test.go` (`referencePages`), `docs/site/testing.md`, `docs/site/reference/perf.md`, `SKILL.md`, `CHANGELOG.md`, `docs/site/nav.json` if reference pages are listed there

Before writing prose: load the `humane-docs` skill, and run every user-facing string past the user's no-em-dash rule. CHANGELOG and docs prose go to the user for copy review per the `copy-review` skill before the final commit.

- [ ] **Step 1: Map the packages** in `referencePages`:

```go
	"budget":        "reference/budget",
	"perf/perftest": "reference/perftest",
```

Run: `GOFLAGS=-mod=mod go test ./internal/docsite/`
Expected: FAIL, the two pages do not exist or do not name every symbol.

- [ ] **Step 2: `docs/site/reference/budget.md`**: title `# 🤖 budget`, the import path, what `Main` does and why it judges nothing (the cache window), the one-line `TestMain`, the variant for an app that already has a `TestMain` (save the code, tear down, `os.Exit`), and `Prefix`. Every exported symbol (`Main`, `Prefix`) named.

- [ ] **Step 3: `docs/site/reference/perftest.md`**: title `# 🤖 perftest`; `Screens`, `ScreenConfig` and each field, `Opaque`, `OpaqueScreen`, `Boot`, `BootConfig` and each field, `BootMarker`; the protocol (10 requests, discard 2, median of 8, 3× ceiling); TTFB over HTTP; opaque mounts; the CI switch; the perf lane's flags and why each is there; that a `1xx` response counts as the first byte.

- [ ] **Step 4: `perf.md`**: one paragraph after "Budgets": production measures and logs; `perftest` fails a branch in CI at the same numbers.

- [ ] **Step 5: `testing.md`**: replace "A database per test" and "make ci" sections and add the rest, in this order: the edit loop and pre-push; the budgets table; `budgets.txt` grammar, refusals, staleness and the ratchet; `budget.Main` and the cache; the perf lane (flags table from spec § 4a); splitting by feature (recipe with the `Mount(r chi.Router, deps)` shape and the generated-code note); practices (`t.Parallel()` first, `sync.Once` for binaries, the `-count=1` rule, browser tests, scoped `-race`, missing tools fail, `GOMAXPROCS=1`, fences watched failing); runner prerequisites; adopting the budgets in an existing app. Cite measurements as the spec does (Tito Go 207ms vs 2.5ms; amadan 26.85s to 8.5s).

- [ ] **Step 6: SKILL.md**: add one line at the end of §1 ("Past 5,000 lines in `internal/<app>`, split by feature: `internal/<app>/<feature>` exposing `Mount(r chi.Router, deps)`; `make budget` enforces it.") and a new section before "## 8. What NOT to do":

```markdown
## 7a. Tests and budgets

`make ci` holds each directory to 5,000 source and 8,000 test lines,
each test package to 10s, each GET screen to 150ms to first byte and a
cold boot to 500ms; exceptions go in `.rastrillo/budgets.txt` with a
reason. Every test package's `TestMain` is `os.Exit(budget.Main(m))`
(it prints the time `rastrillo budget test` judges, cached runs
included). Tests start with `t.Parallel()` and copy a `dbtest` template
instead of migrating. Timing fails only on CI. While editing, test the
package you touch plus `internal/<app>test`; `make ci` before pushing.
A test whose inputs Go cannot see (execs `go build`, reads outside its
package, uses the network) gets its own `-count=1` target.
docs/site/testing.md
```

Run: `GOFLAGS=-mod=mod go test -run TestSkillMD .` Expected: `ok` (under 30,000 bytes; it was 27,514).

- [ ] **Step 7: CHANGELOG.md** Unreleased entry: `### Added: budgets for directory size, test time and screen time`, what a new app's `make ci` now runs, `budget`, `perftest`, `rastrillo budget size|test`, and the adoption recipe for an existing app (add `budget.Main`, add the three targets and steps, run `budget size`, record what is over with reasons).

- [ ] **Step 8: Run the whole gate**

Run: `GOFLAGS=-mod=mod go vet ./... && test -z "$(gofmt -l .)" && GOFLAGS=-mod=mod go test ./...`
Expected: all `ok`, `gofmt` silent.

- [ ] **Step 9: Commit**

```bash
git add docs SKILL.md CHANGELOG.md internal/docsite
amadan commit -m "Docs: the budgets, the perf lane and how to stay inside them" -summary "Reference pages for budget and perftest, testing.md's full treatment, a SKILL.md section and the changelog entry." -body "SKILL.md is what an agent loads instead of the source; a budget it does not mention is one an agent will meet only as a red gate.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

## Self-review notes

- Spec coverage: §1 → Task 2; §2 → Task 3; §3 → Tasks 4–5; §4 → Tasks 6–7; §4a → Task 8 (Makefile) and Task 10; §5 → Task 8; §6 → Task 9; §7 → Task 11; Proving → each task's tests plus Task 10.
- Names used across tasks: `budgetfile.BootChildEnv`, `budgetfile.Enforce`, `budget.Prefix`, `budgetrun.Read/Judge/Print`, `perftest.Screens/Boot/ScreenConfig/BootConfig/Opaque/OpaqueScreen/BootMarker`, scaffold `Router/Mux/App/Configure`.
