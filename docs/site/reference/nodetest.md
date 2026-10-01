# 🤖 nodetest

`amadan.net/rastrillo/rastrillo/nodetest`

Run a Node script from a Go test. Use it for JavaScript that has a Go
twin or logic that needs no browser, such as a crypto envelope both
languages must open or a date parser.

Node is a development tool here, not a dependency of your app. Anything
that needs a real page still belongs in a browser test.

## Node

```go
const RequireEnv = "RASTRILLO_TEST_REQUIRE_NODE"

func Node(t testing.TB) string
```

`Node` returns the path to `node`. If there is none on your PATH, it
skips the test and names it, so you can see what went unchecked.

Set `RASTRILLO_TEST_REQUIRE_NODE` on CI. A missing `node` then fails the
test instead of skipping it, so the JavaScript checks cannot disappear
without anyone noticing. Rastrillo's own `make ci` sets it.

## Run

```go
type Cmd struct {
	Args    []string
	Dir     string
	Stdin   []byte
	Env     []string
	Timeout time.Duration
}

const DefaultTimeout = 2 * time.Minute

func Run(t testing.TB, c Cmd) []byte
```

```go
out := nodetest.Run(t, nodetest.Cmd{
	Args: []string{"--test", "js/golden.test.mjs"},
	Env:  []string{"TZ=UTC"},
})
```

`Run` runs `node` with `Args` and returns what it wrote to stdout. `Dir`
is the working directory; leave it empty for the test's package
directory. `Stdin` is fed to the script. `Env` adds to the test's
environment, so name only what you change, such as `TZ=UTC`.

If `node` exits with an error, the test fails with both stdout and
stderr. `node --test` reports failures on stdout, and a crash lands on
stderr.

A script that has not finished after `Timeout` fails the test by name.
The default, `DefaultTimeout`, is two minutes.
