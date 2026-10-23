# 🤖 op

`amadan.net/rastrillo/rastrillo/op`

What operations and both of their doors share: the refusals an operation
returns, the input types that remember whether a field was sent, and the
descriptor that names an operation for any transport. `rastrillo
generate` writes one operation per action to `gen/ops/<name>/`;
[JSON API](/docs/api#writing-an-operation-by-hand) says how to replace
one.

## Refusals

```go
type Invalid struct{ Fields map[string]string }
type NotFound struct{}
type Forbidden struct{ Reason string }
type Conflict struct{ Message string }
```

An operation returns one of these to refuse a call. All four are values,
so match them with `errors.As`:

```go
var inv op.Invalid
if errors.As(err, &inv) {
	// inv.Fields names what was wrong
}
```

`Invalid.Fields` is keyed by manifest field name, such as `Title`. The
page door puts each message beside its field, and the API door turns the
key into its wire name. `NotFound` is the answer both for a row that does
not exist and for one that is not the caller's. `Forbidden.Reason` is for
your logs and never reaches the response. `Conflict.Message` is safe to
show.

`Invalid.Error`, `NotFound.Error`, `Forbidden.Error` and `Conflict.Error`
are for log lines: `invalid: Amount, Title` (the field names, sorted,
never the messages), `not found`, `forbidden: <reason>` and
`conflict: <message>`.

## Check and DefaultPolicy

```go
func Check(ctx context.Context, rc *rastrillo.Ctx, o rastrillo.Operation) error
func DefaultPolicy(ctx context.Context, a rastrillo.Actor, o rastrillo.Operation) error
```

`Check` is the one place both doors ask who may do what. Call it first in
any operation you write by hand. It asks, in order:

1. A read-only token on a `Write` operation is refused.
2. A scoped operation with no `Subject` is refused.
3. If `rc.Authorize` is set, its error is the answer. Return `nil` to
   allow, or `Forbidden` or `NotFound` to refuse. `Check` returns your
   error unchanged, and the door that called it answers it the way it
   answers the same error from an operation: the API by its errors
   table, a page with its 403 or 404 page. An error neither recognises
   is a 500.
4. Otherwise `DefaultPolicy` decides.

The first two run before your policy, so no policy can let a read-only
token write or let a caller with no subject reach a scoped row.

`DefaultPolicy` allows a scoped operation, because the owner filter keeps
the caller to their own rows. It allows an unscoped operation for a
signed-in person, an anonymous visitor and the app. It refuses an
unscoped operation for a token, because your pages may guard those tables
with middleware the API cannot see. To let tokens in, say so in your
`Authorize`, and call `DefaultPolicy` from it for everything else.

## Opt and Some

```go
type Opt[T any] struct {
	Value T
	Set   bool
}

func Some[T any](v T) Opt[T]
```

`Opt` is a field a request may or may not carry. After decoding, `Set` is
true when the member was sent, whatever its value, so an explicit `0` is
present and a missing member is not. Use it for every field of a create
or update input: an update then changes only the fields the client sent,
and `{}` changes nothing.

`Opt.UnmarshalJSONFrom` refuses `null`. No manifest field is nullable, so
a `null` is a client bug and is reported on that field.
`Opt.MarshalJSONTo` writes the value, or `null` when unset. `Some` makes a
present `Opt` in Go code.

## Money and Nullable

```go
type Money struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

type Nullable[T any] struct {
	Value T
	Set   bool
	Null  bool
}
```

`Money` is an amount on the wire: `amount` in the currency's minor unit
and `currency` as an ISO 4217 code. Leave `currency` out of a request to
use the manifest's default on a create, or to keep the stored currency on
an update. The operation checks the code; this package does not.

A required money field is `Opt[Money]`. An optional one is
`Nullable[Money]`, which is `Opt` plus a `Null` flag. A JSON `null` sets
both `Set` and `Null`: the member was sent, and it asks to clear the
field. `Nullable.UnmarshalJSONFrom` is the only decoder here that accepts
`null`. `Nullable.MarshalJSONTo` writes `null` when unset or `Null`, and
the value otherwise.

`Nullable.IsNullable` is always true. It is the marker `DecodeInput` reads
to accept `null` for a field. `Opt` does not have it.

`Money.UnmarshalJSONFrom` reads `{"amount": 1490, "currency": "USD"}`.
`amount` is required and `currency` may be left out; any other member is
refused.

## DecodeInput

```go
func DecodeInput(data []byte, dst any) error
```

`DecodeInput` decodes one JSON object into `dst`, a pointer to a struct,
with the rules both doors share. `api.Decode` calls it on the body it
read, and every generated `Descriptor.Invoke` calls it on its
arguments, so the two cannot drift. It refuses an unknown member, a
member sent twice, a wrong type, and `null` anywhere but a `Nullable`
field, each as an `Invalid` keyed by the member's name as sent. A value
that is not one JSON object is an `Invalid` with no fields; the door
says what to send instead.

## EncodeCursor and DecodeCursor

```go
func EncodeCursor(createdAt string, id int64) string
func DecodeCursor(s string) (createdAt string, id int64, ok bool)
```

A list's `next_after` is the last row's `created_at` and `id`, encoded as
one opaque string. `DecodeCursor` reverses it: `""` is the first page,
and anything `EncodeCursor` could not have made answers `ok == false`,
so a hand-edited cursor is refused rather than read as some other place
in the list.

## Descriptor

```go
type Descriptor struct {
	rastrillo.Operation
	Exposed     bool
	Description string
	Confirm     string
	Input       json.RawMessage
	Result      json.RawMessage
	Invoke      func(ctx context.Context, rc *rastrillo.Ctx, input json.RawMessage) (any, error)
}
```

`Descriptor` names one operation for any transport. The embedded
`rastrillo.Operation` gives its ID, resource, scope and access. `Exposed`
says whether the resource is on the API. `Confirm` is the sentence an
agent shows before a write. `Input` and `Result` are JSON Schema, and
both are required. `Invoke` decodes `input` with the API door's rules and
calls the operation.
