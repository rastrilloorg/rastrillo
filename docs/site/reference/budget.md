# 🤖 budget

`amadan.net/rastrillo/rastrillo/budget`

Measures how long a test package takes, so `rastrillo budget test` can
hold it to its time budget.

```go
func Main(m *testing.M) int

const Prefix = "rastrillo-budget/v1 ran "
```

Call `Main` from the package's `TestMain`:

```go
func TestMain(m *testing.M) { os.Exit(budget.Main(m)) }
```

`Main` runs the tests, prints one line starting with `Prefix` and giving
the time they took, and returns `m.Run`'s exit code unchanged. It never
fails a package itself.

It leaves the judging to `rastrillo budget test` so that Go's test cache
stays trustworthy. Go saves a passing package's output, this line
included, and replays it on a cache hit, so a cached package is still
judged on the time it took when it ran. If `Main` read the budget itself,
Go would not know to rerun the package when the budget changed.

If your package already has a `TestMain`, call `budget.Main(m)` in place
of `m.Run()` and tear down afterwards:

```go
func TestMain(m *testing.M) {
	code := budget.Main(m)
	schema.Remove()
	os.Exit(code)
}
```

[Testing](/docs/testing) covers the budgets and `budgets.txt`.
