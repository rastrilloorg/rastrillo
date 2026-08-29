# 🤖 Forms

One rule outranks everything else on this page: never bind a request
body onto a model.

## Why not

No reflection binding, no `PostForm` loop, nothing that walks the
submitted keys and writes whatever it finds. A form is
attacker-supplied, and your model has fields the user must not choose —
`UserID`, `Role`, `PriceCents`, `CreatedAt`.

Read each permitted field by name:

```go
title := r.PostFormValue("Title")
body := r.PostFormValue("Body")
```

and name the columns on the way out:

```go
a.owned(r).Model(&n).Select("Title", "Body", "UpdatedAt").Updates(update)
```

`Select`'s strings are GORM field names, and they are the allowlist that
matters. An unexpected field in the request cannot reach a column,
because no code path exists that would carry it there.

## form.Parse

Declare your fields once and validate them in one pass:

```go
p := form.Parse(r,
	form.Field{Name: "title", Required: true},
	form.Field{Name: "body", Kind: form.Textarea})
if !p.OK() {
	w.WriteHeader(http.StatusUnprocessableEntity)
	renderContent(w, r, "new", formView{
		Errors: p.Errors(),
		Note:   Note{Title: p.String("title"), Body: p.String("body")},
	})
	return
}
```

Write the status before rendering. The render helper writes none.

`p.Errors()` is a `form.Errors` — a map from field name to message,
empty rather than nil when everything validated, which is what a
template renders beside each input. `p.Echo()` gives you the whole
seed-back map at once if your view is map-shaped.

Nothing gets retyped on a validation failure. That is the point of the
echo: a rejected form comes back populated.

## The kinds

The zero value is `Text`, so a plain `form.Field{Name: "Title"}` does
the common thing.

| Kind | Read with | Behaviour |
|---|---|---|
| `Text` (zero value) | `p.String` | Trimmed; value and echo are both the trimmed text |
| `Textarea` | `p.String` | Kept exactly as typed, whitespace included; only the required check trims |
| `Money` | `p.Cents` | Parsed to integer cents; the echo keeps the raw text |
| `Date` | `p.Date` | `2006-01-02`, parsed in `Location` |
| `Time` | `p.Time` | `15:04` — a clock reading, no location |
| `DateTime` | `p.DateTime` | `2006-01-02T15:04`, parsed in `Location` |

`Required` on a blank `Text` or `Textarea` reports "<Field name> is
required", humanized from the field name.

`Required` on `Money` and on the three date kinds is checked against the
raw text rather than the parsed value, so a present but unparseable
value reports the parse error instead of the required message — telling
someone a field is required when they filled it in would be a small lie
the parser is in a position to avoid. For `Money` that also makes `"0"`
a present, valid zero where `""` is required-blank; a date has no such
pair, since `"0"` is simply not a date and reports
`rastrillo.ui.date_invalid`.

`Parse` reads through `r.PostFormValue`, which parses the form on first
use. If you want a body-size cap or a 400 on a malformed body, wrap
`r.Body` and call `r.ParseForm` yourself first, the way the generated
actions do.

## Dates and times

The three date kinds parse one exact wire format each and nothing
looser, because the browser has already normalised whatever the person
typed:

```go
p := form.Parse(r,
	form.Field{Name: "starts", Kind: form.DateTime, Required: true, Location: tz},
	form.Field{Name: "ends", Kind: form.DateTime, Required: true, Location: tz},
	form.Field{Name: "doors", Kind: form.Time})
form.Range(p, "starts", "ends")
```

`Location` is the zone `Date` and `DateTime` parse in; leave it nil and
it is UTC. `Time` is a bare clock reading and ignores it. A timezone is
not a date field's concern — an app that needs one renders a
`field-select` of zones beside the field and hands the chosen location
to `Parse`.

Reading back:

```go
starts := p.DateTime("starts")     // time.Time
h, m, ok := p.Time("doors")        // ok=false when absent or unreadable
```

An empty optional date is the zero `time.Time`, and no error at all. An
unparseable one is the zero `time.Time` too, but it does carry an error:
`rastrillo.ui.date_invalid`. A name you never declared is the zero
`time.Time` as well, and silently — no value, no error. All three read
back identically from `p.Date`, so ask `p.OK()` or read `p.Errors()` to
tell them apart rather than testing `IsZero`. `p.Time` says the same
through its `ok`, and returns `0, 0` rather than half a reading.

### form.Range

`Range` is the two-field check `Parse` cannot do, because `Parse` takes
one declaration per field:

```go
form.Range(p, "starts", "ends")
```

It puts an error on `ends` when its instant precedes `starts`. Equal
instants are fine. If either side is blank or unparseable, `Range` says
nothing at all — `Parse` has already named that field's problem, and
stacking a second message on top of it only crowds the screen. Either
kind works on either side, and the two can be mixed.

### Errors are catalog keys

`Text`, `Textarea` and `Money` report finished English sentences. The
three date kinds report `rastrillo.ui.*` keys instead —
`rastrillo.ui.date_invalid`, `rastrillo.ui.field_required`,
`rastrillo.ui.date_end_before_start` — so a French app's date error is
in French without the app writing one.

Resolving happens at render. The generated actions wrap every field's
error in `T` unconditionally, and a hand-written date field does the
same:

```html
{{template "field-datetime" dict "Name" "Starts" "Label" (T "event.field.starts")
	"Value" .Fields.Starts "Error" (T (index .Errors "Starts"))}}
```

Wrapping in `T` is safe for every field, which is why the generated
template does it without asking what kind it has. `T` hands back a
string it does not recognise as a key exactly as given, so "Title is
required" and your own hand-written sentences pass straight through, and
only the keys get looked up. The wrapping lives in the calling template
rather than in the partial, so a hand-written caller that already passes
a finished sentence keeps rendering it unchanged.

### Daylight saving

`time.ParseInLocation` resolves a wall-clock time that does not exist —
the hour a spring-forward skips — using the offset in force before the
transition, so a time inside the skipped hour and the real time it
collapses onto land on the same instant.

## Money is int64 cents

Never a float. `form.Money` parses through `form.ParseCents`, which is
strict on purpose: at most two decimal places, no `$`, no sign character
at all, both halves ASCII digits. An empty string parses to zero,
because a required money field has already rejected blankness on the raw
text before `ParseCents` runs.

The strictness earns its keep. Handing each half to `strconv.ParseInt`
accepts its own `+`/`-`, so `"12.-5"` would quietly parse to a different
magnitude than its digits suggest instead of being rejected as the
not-a-dollar-amount it is.

There are two formatters, and using the wrong one is a real bug:

```go
form.FormatCents(cents)      // "$12.34" — for display
form.FormatCentsPlain(cents) // "12.34"  — for seeding a form field
```

Seed with the plain one. A browser may resubmit an untouched field
completely unchanged, so the seed has to be exactly what `ParseCents`
accepts back, and `ParseCents` rejects a leading `$`. Seed with
`FormatCents` and resubmitting an unmodified money field always fails.

Both handle negatives correctly, writing the sign once against the
absolute value. Formatting a negative directly produces `"$-1.-50"`,
since Go's `/` and `%` both truncate toward zero.

## After a successful write

```go
flash.Set(w, "notice", "Note created.")
http.Redirect(w, r, "/notes/"+id, http.StatusSeeOther)
```

`flash.Set` writes a one-shot cookie; your render helper calls
`flash.Take(w, r)` once per page and the layout renders it. A flash is
display state, not a record — losing one to a cleared cookie costs a
notice, not data.

303 rather than 302, so the browser follows with a GET and a refresh
does not resubmit.
