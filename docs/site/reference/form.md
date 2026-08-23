# 🤖 form

`github.com/carlosframework/rastrillo/form`

Framework-independent form helpers: field parsing with validation, a
field-error map, and money. Shared by generated and hand-written
handlers alike.

[Forms](/docs/forms) is the guide.

## Parse

```go
func Parse(r *http.Request, fields ...Field) *Parsed
```

Reads and validates every declared field in one pass.

It reads through `r.PostFormValue`, which parses the form on first use.
A caller wanting a body-size cap or a 400 on a malformed body wraps
`r.Body` and calls `r.ParseForm` first, as the generated actions do.

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

`Required` on `Money` is checked against the **raw** text — `""` is
required-blank while `"0"` is a present, valid zero — and a
present-but-unparseable value reports the parse error rather than the
required message.

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
`Errors.Any` reports whether there is at least one.

## Humanize

```go
func Humanize(name string) string
```

Turns a field name into the words a message uses, so a required `title`
reports "Title is required" rather than "title is required". Exported so
a hand-written validation message can match the generated ones.

## Money

Money is `int64` cents throughout. Never a float.

### ParseCents

```go
func ParseCents(s string) (int64, error)
```

Parses decimal dollars into cents. Deliberately strict: at most two
decimal places, no `$`, **no sign character at all**, both halves ASCII
digits. An empty string parses to zero rather than erroring, because a
required money field has already rejected blankness on the raw text
before this runs.

The strictness is not fussiness. Handing each half to
`strconv.ParseInt` accepts its own `+`/`-`, so `"12.-5"` would silently
parse to a different magnitude than its digits suggest instead of being
rejected as the not-a-dollar-amount it is.

### FormatCents and FormatCentsPlain

```go
func FormatCents(cents int64) string      // "$12.34" — for display
func FormatCentsPlain(cents int64) string // "12.34"  — for seeding a form field
```

**Using the wrong one is a real bug.** Seed with the plain one: a
browser may resubmit an untouched field completely unchanged, so the
seed must be exactly what `ParseCents` accepts back — and `ParseCents`
rejects a leading `$`. Seeding with `FormatCents` means resubmitting an
unmodified money field always fails.

Both write the sign once against the absolute value. Formatting a
negative directly would produce `"$-1.-50"`, because Go's `/` and `%`
both truncate toward zero.
