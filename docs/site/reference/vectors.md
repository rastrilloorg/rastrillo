# 🤖 vectors

`github.com/carlosframework/rastrillo/vectors`

The golden vectors that pin your app's JS derivation engine to its Go
one.

Any derivation over sealed content runs client-side, but the sidecar,
operator tools and tests want the same derivation in Go. So the engine
exists twice, and two engines drifting apart is the most dangerous E2EE
bug class there is: a wrong answer with nothing looking broken.

Treat this as a treaty file between the two engines. The key names in a
vector's fields are part of the contract — the JS suite consumes them by
name, changing one means changing both sides in the same commit, and
nothing mechanical checks the key sets agree. Time values round-trip
through RFC 3339 to `new Date(v.now)`, so put times in as `time.Time`
and never as pre-formatted strings.

A Set is built by your own `cmd/genvectors` (scaffolded by
`rastrillo vectors -init`), written to `test/vectors.json` by
`rastrillo vectors`, and consumed by your `test/parity.test.mjs`
through the vendored helper this package embeds.

## Building a Set

```go
type Set struct { /* ordered cases */ }
func New() *Set
func (s *Set) Add(name, why string, fields map[string]any)
func (s *Set) WriteTo(w io.Writer) (int64, error)
```

Start with `New()` for an empty Set, then `Add` your vectors in order.
They stay ordered so the file reads in the order the rules were written,
and so two runs over the same cases write identical bytes. `name`
identifies the vector and `why` names the rule it pins; the JS test
titles come out as `"name — why"`.

`name` and `why` are reserved for the envelope, and `WriteTo` refuses
them as field keys.

`WriteTo` emits the set as a JSON array with two-space indent and a
trailing newline, which is `test/vectors.json`'s exact contract.

Normalisation reaches top-level field values only. A nil slice or nil
map sitting directly in `fields` marshals as `[]` or `{}` and never
`null`, because "no bests" has to look the same on both sides. It does
not walk inside app-typed values: `json` tags, `omitempty` and custom
marshalers would make a generic deep walk a lie, so nil-versus-empty in
there stays your own discipline.

## The JS helper

```go
func JS() []byte
```

The vendored ES module helper — `loadVectors` and `canonical` — that
scaffolded apps copy to `test/vectors.mjs` and own from then on, on the
same terms as `tokens.css` and the shim.

`loadVectors(path)` reads and parses a `vectors.json` written by
`rastrillo vectors`. Resolve the path from your suite's own URL with
`fileURLToPath(new URL("./vectors.json", import.meta.url))`, so the
suite does not care which directory node started in.

`canonical(value)` is the comparison rule. It sorts object keys
recursively, drops `undefined` and `null` members, and drops scalar
zeros — `0`, `false`, `""` — to match Go's `omitempty`.

That last part has a blind spot worth knowing. Without the zero-strip,
Go dropping a zero field while JS computes it would fail the diff on
encoder behaviour instead of arithmetic. With it, an explicit zero on
one side and a missing field on the other compare equal. The parity
template scaffolds a belt section covering that hole: put
explicit-value assertions beside the loop and they catch what the rule
misses.

Empty arrays stay `[]` on both sides, because `rastrillo vectors`
supplies them from Go by normalising top-level nils.
