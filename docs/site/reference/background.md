# 🤖 background

`amadan.net/rastrillo/rastrillo/background`

Work your app starts and nobody waits for: an email sent after the
response, a sweep on a timer, a reminder due in a minute. A `Group`
keeps track of it, so shutting down can stop it before the database
closes.

Work still running when the database closes fails with "sql: database is
closed". In a test, that failure can panic a later, unrelated test.
Start background work through a `Group`, never with a bare `go`
statement or `time.AfterFunc`, and there is always one thing to stop.

## Starting work

```go
type Group struct { /* unexported; the zero value is ready */ }

func (g *Group) Go(fn func()) bool
func (g *Group) After(d time.Duration, fn func()) *Timer
func (g *Group) Loop(ctx context.Context, every time.Duration, pass func())
func (g *Group) Done() <-chan struct{}
```

`Go` runs `fn` on its own goroutine. Once the group has stopped, it runs
nothing and returns `false`. If you keep a flag saying the work is
running, clear it when `Go` returns `false`, or the work looks busy
forever.

`After` runs `fn` once `d` has passed, like `time.AfterFunc`. Once the
group has stopped, it returns `nil`.

```go
type Timer struct { /* unexported */ }

func (t *Timer) Stop() bool
```

`After` returns a `Timer`. To cancel it, call the `Timer`'s own `Stop`,
which also removes it from the group. `Stop` is safe on the `nil` a
stopped group returns.

`Loop` runs `pass` once straight away, then every `every`, until `ctx`
is cancelled or the group stops. A pass that has started always
finishes.

`Done` is closed when the group stops. Use it in work that waits on
something else and must notice the stop.

## Stopping

```go
func (g *Group) Stop()
```

`Stop` refuses new work, cancels timers that have not fired, and waits
for work already running to finish. Call it before you close the
database. It is safe to call more than once.

Set `Options.Background` and `Serve` calls `Stop` for you, after the
last request finishes and before the database closes. The close function
`Handler` returns does the same, so tests shut down in the same order.

## Untracked

```go
func Untracked(dir string, allow ...string) ([]string, error)
```

`Untracked` lists every `go` statement and `time.AfterFunc` call in a
directory's Go files, skipping tests and any files you name in `allow`.
Fail a test on anything it returns, and nobody can start work the group
cannot stop.

```go
func TestBackgroundWorkIsTracked(t *testing.T) {
	offences, err := background.Untracked(".", "serve.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range offences {
		t.Error(o)
	}
}
```

The goroutine that runs your HTTP server is the usual file to allow. It
ends when the server shuts down, not when the group stops.

It reads the code as written, so a renamed `time` import, or a goroutine
started inside another package, gets past it.
