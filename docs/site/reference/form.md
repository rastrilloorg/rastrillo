# 🤖 form

`github.com/carlosframework/rastrillo/form`

Framework-independent form helpers: field parsing with validation, a
field-error map, and money. Generated and hand-written handlers use the
same ones. [Forms](/docs/forms) is the guide.

## Parse

```go
func Parse(r *http.Request, fields ...Field) *Parsed
```

Reads and validates every declared field in one pass.

It reads through `r.PostFormValue`, which parses the form on first use.
If you want a body-size cap or a 400 on a malformed body, wrap `r.Body`
and call `r.ParseForm` first, as the generated actions do.

## Field and Kind

```go
type Field struct {
	Name     string
	Kind     Kind
	Required bool
}
```

`Kind` is how the submitted text is read. The zero value is `Text`, so a
plain `form.Field{Name: "Title"}` does the common thing.

| Constant | Read with | Behaviour |
|---|---|---|
| `Text` (the zero `Kind`) | `Parsed.String` | Trimmed; value and echo are both the trimmed text |
| `Textarea` | `Parsed.String` | Raw, whitespace preserved; only the required check trims |
| `Money` | `Parsed.Cents` | Parsed by `ParseCents`; echo keeps the raw text |

`Required` on `Money` is checked against the raw text, so `""` is
required-blank while `"0"` is a present, valid zero. A present but
unparseable value reports the parse error instead of the required
message.

## Parsed

```go
type Parsed struct{ /* unexported */ }
```

`Parse`'s result: values, the echo map, and the errors.

| Method | Returns |
|---|---|
| `Parsed.OK` | true when every field validated |
| `Parsed.Errors` | the `Errors` map, empty rather than nil when OK |
| `Parsed.String` | a `Text` or `Textarea` field's value |
| `Parsed.Cents` | a `Money` field's value in integer cents |
| `Parsed.Echo` | every field's raw text, for seeding a re-render |

## Errors

```go
type Errors map[string]string
```

Field name to message — the shape a template renders beside each input.
`Errors.Any` tells you whether there is at least one.

## Humanize

```go
func Humanize(name string) string
```

Turns a field name into the words a message uses, so a required `title`
reports "Title is required". Exported so your own validation messages
can match the generated ones.

## Money

Money is `int64` cents throughout. Never a float.

### ParseCents

```go
func ParseCents(s string) (int64, error)
```

Parses decimal dollars into cents. Strict on purpose: at most two
decimal places, no `$`, no sign character at all, both halves ASCII
digits. An empty string parses to zero, because a required money field
has already rejected blankness on the raw text before this runs.

The strictness earns its keep. Handing each half to `strconv.ParseInt`
accepts its own `+`/`-`, so `"12.-5"` would quietly parse to a different
magnitude than its digits suggest instead of being rejected as the
not-a-dollar-amount it is.

### FormatCents and FormatCentsPlain

```go
func FormatCents(cents int64) string      // "$12.34" — for display
func FormatCentsPlain(cents int64) string // "12.34"  — for seeding a form field
```

Using the wrong one is a real bug. Seed with the plain one: a browser
may resubmit an untouched field completely unchanged, so the seed has to
be exactly what `ParseCents` accepts back, and `ParseCents` rejects a
leading `$`. Seed with `FormatCents` and resubmitting an unmodified
money field always fails.

Both write the sign once against the absolute value. Formatting a
negative directly produces `"$-1.-50"`, because Go's `/` and `%` both
truncate toward zero.
