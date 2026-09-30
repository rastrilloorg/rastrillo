# 🤖 form

`amadan.net/rastrillo/rastrillo/form`

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
	Location *time.Location // Date and DateTime only; nil means time.UTC
}
```

`Kind` is how the submitted text is read. The zero value is `Text`, so a
plain `form.Field{Name: "Title"}` does the common thing.

| Constant | Read with | Behaviour |
|---|---|---|
| `Text` (the zero `Kind`) | `Parsed.String` | Trimmed; value and echo are both the trimmed text |
| `Textarea` | `Parsed.String` | Raw, whitespace preserved; only the required check trims |
| `Money` | `Parsed.Cents` | Parsed by `ParseCents`; echo keeps the raw text |
| `Date` | `Parsed.Date` | `time.ParseInLocation` on `2006-01-02` in `Location` |
| `Time` | `Parsed.Time` | `time.Parse` on `15:04` — a clock reading, no location |
| `DateTime` | `Parsed.DateTime` | `time.ParseInLocation` on `2006-01-02T15:04` in `Location` |
| `URL` | `Parsed.String` | `NormaliseURL`; echo keeps the raw text; a refused value reads back as `""` |

`Required` on `Money`, `Date`, `Time` and `DateTime` is checked against
the raw text rather than the parsed value, so a present but unparseable
value reports the parse error instead of the required message. For
`Money` that also makes `"0"` a present, valid zero where `""` is
required-blank; a date has no such pair, because `"0"` is not a date and
reports `rastrillo.ui.date_invalid`.

The three date kinds and `URL` report catalog keys rather than English:
`rastrillo.ui.date_invalid` for an unparseable date,
`rastrillo.ui.url_invalid` or `rastrillo.ui.url_credentials` for a
refused address, and `rastrillo.ui.field_required` for a blank required
one. The renderer resolves them through `T`. See
[Dates and times](#dates-and-times) and [Web addresses](#web-addresses).

## Parsed

```go
type Parsed struct{ /* unexported */ }
```

`Parse`'s result: values, the echo map, and the errors.

| Method | Returns |
|---|---|
| `Parsed.OK` | true when every field validated |
| `Parsed.Errors` | the `Errors` map, empty rather than nil when OK |
| `Parsed.String` | a `Text`, `Textarea` or `URL` field's value |
| `Parsed.Cents` | a `Money` field's value in integer cents |
| `Parsed.Date` | a `Date` field's `time.Time` |
| `Parsed.DateTime` | a `DateTime` field's `time.Time` |
| `Parsed.Time` | a `Time` field's `(h, m int, ok bool)` clock reading |
| `Parsed.Echo` | every field's raw text, for seeding a re-render |

## Dates and times

```go
func (p *Parsed) Date(name string) time.Time
func (p *Parsed) DateTime(name string) time.Time
func (p *Parsed) Time(name string) (h, m int, ok bool)
```

Each kind parses one exact wire format and nothing looser — the format
the matching `<input>` posts, so the browser has already normalised
whatever the person typed.

`Date` and `DateTime` parse in `Field.Location`, defaulting to
`time.UTC`. `Time` is a bare clock reading and ignores it.

`Date` and `DateTime` return the zero `time.Time` for an unknown name,
an empty optional field, and one that failed to parse alike, so check
`OK` (or `Errors`) rather than `IsZero` to tell those apart from a real
midnight-UTC submission. `Time` says the same thing through `ok`, and
reports `0, 0` rather than half a reading when it is false.

An empty optional field is never an error. An unparseable one echoes
back exactly as typed, so nothing is retyped over one bad field.

### Range

```go
func Range(p *Parsed, start, end string)
```

Adds `rastrillo.ui.date_end_before_start` on `end` when its instant
precedes `start`'s. It is a separate call because the check spans two
fields, and `Parse` is one-declaration-per-field by design.

```go
p := form.Parse(r,
	form.Field{Name: "starts", Kind: form.DateTime, Required: true},
	form.Field{Name: "ends", Kind: form.DateTime, Required: true})
form.Range(p, "starts", "ends")
```

Both fields must have been declared as `Date` or `DateTime`; either kind
works, and they can be mixed. Equal instants are fine. If either side is
blank or failed to parse, `Range` adds nothing at all — `Parse` already
recorded that field's own problem, and a second message on top of it
would only be noise.

`Range` compares the fields that actually parsed, not the ones that are
non-zero, so a genuinely submitted zero date takes part like any other
instant instead of being mistaken for unset.

### The daylight-saving corner

`time.ParseInLocation` resolves a wall-clock time that does not exist —
the hour a spring-forward skips — using the offset in force before the
transition, which means a time inside the skipped hour and the real time
it collapses onto land on the same instant. An app scheduling across a
transition should store the zone alongside the value and say so on
screen, rather than trusting that two distinct readings stay distinct.

## Web addresses

```go
func NormaliseURL(s string) (string, error)
func DisplayURL(s string) string
func SafeHref(s string) string
func URLKey(s string) string
```

`NormaliseURL` is what `form.URL` runs. It accepts an address with or
without its scheme and returns the form to store: an http or https URL
with a lowercase host and no bare trailing slash. `https` is added when
no scheme was typed. A path, port, query and fragment are kept as
typed.

| Typed | Stored |
|---|---|
| `example.com` | `https://example.com` |
| `WWW.Example.com/` | `https://www.example.com` |
| `http://example.com` | `http://example.com` |
| `example.com:8443/pricing?plan=team` | `https://example.com:8443/pricing?plan=team` |

Blank is `""` with no error, so `Required` stays the caller's choice.
Every refusal is an `*Error`:

- `rastrillo.ui.url_invalid` for any scheme but http and https
  (`javascript:`, `data:`, `mailto:`, `ftp:`), a host with no dot
  (`localhost`), a character a host cannot hold, or whitespace inside
  the address.
- `rastrillo.ui.url_credentials` for a username or password in the
  address. `https://bank.example@evil.example` reads as one site and
  goes to another.

The refusals are what make a stored value safe to put in an `href`.

`DisplayURL` is a stored address as a reader sees it: no scheme, no
trailing slash, so `https://example.com/pricing/` shows as
`example.com/pricing`. It is for display only.

`SafeHref` is the address if it is an absolute http or https URL with a
host and no credentials, and `""` for anything else. Use it for every
link to a stored address, including one saved before you validated. The
`ui` template functions `displayURL` and `safeHref` are these two.

`URLKey` is for finding duplicates. It drops the scheme, a leading
`www.` and the trailing slash, and lowercases the rest, so
`https://www.Example.com/` and `example.com` have the same key. The port
and path still count. Store what `NormaliseURL` returns and compare the
keys.

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

Parses a decimal amount into cents. Strict on purpose: at most two
decimal places, no `$`, no sign character at all, both halves ASCII
digits. An empty string parses to zero, because a required money field
has already rejected blankness on the raw text before this runs.

The strictness earns its keep. Handing each half to `strconv.ParseInt`
accepts its own `+`/`-`, so `"12.-5"` would quietly parse to a different
magnitude than its digits suggest instead of being rejected as the
not-an-amount it is.

### Error

```go
type Error struct {
    Key string // a rastrillo.ui.* catalog key
    Msg string // the English, and what Error() returns
}

func (e *Error) Error() string
```

Every error `ParseCents` and `NormaliseURL` return is one of these. It names the catalog
key for its message as well as carrying the message, so a caller that
has a translator can render it in the reader's language:

```go
cents, err := form.ParseCents(r.FormValue("price"))
var fe *form.Error
if errors.As(err, &fe) {
    fields["price"] = t(fe.Key) // or fe.Error() for the English
}
```

The key rather than a translated string, because this package imports
nothing from rastrillo and has no request in reach. A package-level
translator hook would be worse than none: the locale is per request, and
a global would hand one request's language to another's error. A caller
that ignores `Key` and prints the error gets English, which is the same
three-step fallback the rest of the framework walks.

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

**`FormatCents` writes a dollar sign and knows no other currency.**
Nothing in the framework stores a currency, so there is nothing for it
to read. Format your own money anywhere that is not what you want, and
store the currency beside the amount when you do — a reader's locale
decides how a price is written, never which currency it is in.
