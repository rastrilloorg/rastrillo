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

## Two fields on one row

`rst-field-row` is the wrapper for fields that belong side by side — a
city and a postcode, a start and an end. `field-daterange` emits one;
everywhere else you write it yourself around a run of `field` partials:

```html
<div rst-field-row>
  <div class="rst-grow" rst-field>…City…</div>
  <div rst-field>…<input rst-input="short">…</div>
</div>
```

**The row aligns by the control line, not the bottom.** A field carrying
an error is taller than the one beside it, so aligning at the top is
what keeps every control on one line; each message then flows under its
own column and shifts nothing. A field with no label reserves the
label's line anyway, so an unlabelled control still lines up with a
labelled sibling's; a field whose label is long enough to wrap is a
field that wants its own row.

**Every field in a row has an 8rem floor**, and `rst-input="short"` is
8rem wide. A row that runs out of width wraps rather than squeezing its
fields into slivers.

**`rst-grow` is `flex: 1 1 12rem`, not `flex: 1`.** `flex: 1` is
`flex: 1 1 0%` — a grown field with no basis, which collapses on a
narrow screen instead of taking the next line. 12rem is the width it
wraps at.

A long error message never buys its column extra width — the messages
are `contain: inline-size`, so the sentence wraps under its own control
rather than stretching the column and squeezing the field beside it.

### The form owns the block rhythm

`rst-form` is a flex column with a `var(--rst-sp-5)` gap — 24px, the
same rhythm `rst-form-flow` uses — and it zeroes the block margins of
every child it holds. That is one spacing mechanism rather than two:
fields carry margins of their own for use outside a form, and inside one
those margins used to add to the gap. The gap was 8px then, so two
fields sat 40px apart — 8 between them plus 16 above and 16 below — and
the form gained 16px of dead air at each end where nothing was being
separated from anything.

It is stated as a rule over every child rather than a list of the
kinds the library happens to ship, because a list cannot name your own
`<div>` and a child the list forgets lands at exactly the spacing the
rule exists to prevent. The two exceptions are written into the
selector: `rst-form-foot`, the action row `form-foot` emits, and
`rst-form-bar`, the sticky save bar you write by hand, keep their
block-start margin — which is not rhythm but the extra air separating a
closing action row from the last question above it.

The two swapped names in the release that made the vocabulary
attributes. `rst-form-foot` was the save bar and `rst-form__foot` the
partial's row; flattened, both wanted one attribute, so the partial's
row took the name the partial is called and the save bar became
`rst-form-bar`. `rastrillo markup --fix` applies the rename.

If you mean to override it, the shipped selector is
`[rst-form] > *:not([rst-form-foot], [rst-form-bar])`, and a `:not()`
takes the specificity of its most specific argument — so that is
(0,2,0), exactly the weight of `[rst-form] > .whatever`. You win the tie
on source order, and you have it: the shell's `head` block puts your
stylesheet after `tokens.css`.

## The busy button is not a guarantee

`rastrillo.js` gives every submit button a loading state while its form
is out — spinner, `aria-busy`, then `disabled` — and refuses a second
submit from the same form while the first is in flight. It is on by
default; `data-busy="false"` on the form or on one button opts out, and
`data-busy-label` replaces the text. The whole rule, including what it
looks like, is in
[Templates](/docs/templates/#a-button-that-changes-something-says-so).

What it buys you is that the ordinary double click stops posting twice.
What it does not buy you is idempotency, and it is worth being blunt
about the difference: the guard lives in the browser, and the browser is
not where your data is. With JavaScript off there is no guard at all and
the form submits exactly as it always did — twice, if someone clicks
twice. A refresh, a back button, a retried request, two tabs, someone
with a script: none of them go through it.

There is one shape to watch for on the other side of it: a submission
that never navigates — a `204`, or a file handed to the downloads shelf
— leaves the button disabled for good, because nothing arrives to clear
it. Put `data-busy="false"` on those forms. `Templates` has the detail.

So the server still has to be able to see the same write twice and only
do it once. A unique index, an idempotency key on the form, a token you
consume — whichever fits the write. The busy button is manners. The
constraint is the correctness.

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
