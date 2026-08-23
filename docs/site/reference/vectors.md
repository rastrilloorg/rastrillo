# 🤖 vectors

`github.com/carlosframework/rastrillo/vectors`

The golden vectors that pin an app's JS derivation engine to its Go one. Any derivation over sealed content runs client-side, but the sidecar, operator tools and tests want the same derivation in Go — so the engine exists twice, and two engines drifting is the most dangerous E2EE bug class: a wrong answer with nothing looking broken.

**This is a treaty file between the Go and JS engines**, not just a test fixture. The key names in a vector's fields are part of the contract — the JS suite consumes them by name, changing one means changing both sides in the same commit, and nothing mechanical checks the key sets agree. Time values round-trip via RFC 3339 → `new Date(v.now)`: put times in as `time.Time`, never pre-formatted strings.

A Set is built by the app's own `cmd/genvectors` (scaffolded by `rastrillo vectors -init`), written to `test/vectors.json` by `rastrillo vectors`, and consumed by the app's `test/parity.test.mjs` through the vendored helper this package embeds.

## Building a Set

```go
type Set struct { /* ordered cases */ }
func New() *Set
func (s *Set) Add(name, why string, fields map[string]any)
func (s *Set) WriteTo(w io.Writer) (int64, error)
```

Start with `New()` to get an empty Set, then `Add` vectors in order — ordered so the file reads in the order the rules were written, and two runs over the same cases write identical bytes. The `name` identifies the vector; `why` names the rule it pins. The JS test titles become `"name — why"`.

`WriteTo` emits the whole set as a JSON array with 2-space indent and a trailing newline — `test/vectors.json`'s exact contract. Normalisation is scoped to **top-level fields values only**: a nil slice or nil map sitting directly in fields marshals as `[]`/`{}`, never `null` ("no bests has to look the same on both sides"). Inside app-typed values, this method never walks — `json` tags, `omitempty` and custom marshalers make a generic deep walk a lie, so inner nil-vs-empty stays the app's own discipline.

The `name` and `why` field keys are reserved for the vector's envelope; `WriteTo` refuses them.

## The JS Helper

```go
func JS() []byte
```

The vendored ES module helper (`loadVectors` + `canonical`) that scaffolded apps copy as `test/vectors.mjs` and own from then on — the `tokens.css`/shim contract. It exports two functions:

- `loadVectors(path)` — reads and parses a `vectors.json` written by `rastrillo vectors`; resolve the path from your suite's own URL with `fileURLToPath(new URL("./vectors.json", import.meta.url))` so the suite is independent of the directory node started in.

- `canonical(value)` — the comparison rule, blind spot included. Sorts object keys recursively, drops `undefined`/`null` members and drops **scalar zeros** (0, `false`, `""`) to match Go's `omitempty`. Without the zero-strip, Go dropping a zero field while JS computes it fails the diff on encoder behaviour rather than arithmetic — but the same rule means an explicit zero on one side and a missing field on the other compare equal. That hole is documented and covered by the belt section the parity template scaffolds; explicit-value assertions beside the loop catch what the rule misses.

Empty arrays are kept as `[]` on both sides — `rastrillo vectors`' generator supplies them from Go by normalising top-level nils.
