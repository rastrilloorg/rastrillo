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
| `Money` with a `Currency` | `p.Minor` | An amount in that currency's smallest unit; see [Money](#money) |
| `Money` without a `Currency` | `p.Cents` | Integer cents, read strictly; the echo keeps the raw text |
| `Date` | `p.Date` | `2006-01-02`, parsed in `Location` |
| `Time` | `p.Time` | `15:04`, a clock reading, no location |
| `DateTime` | `p.DateTime` | `2006-01-02T15:04`, parsed in `Location` |
| `URL` | `p.String` | A web address, scheme optional; stored as an http(s) URL |

`Required` on a blank `Text` or `Textarea` reports "<Field name> is
required", humanized from the field name.

`Required` on `Money` and on the three date kinds is checked against the
raw text rather than the parsed value, so a present but unparseable
value reports the parse error instead of the required message. For
`Money` that also makes `"0"` a present, valid zero where `""` is
required-blank. A date has no such pair, since `"0"` is not a date and
reports `rastrillo.ui.date_invalid`. A `Money` field with a `Currency`
treats text of only spaces or direction marks as blank, and its required
message is the key `rastrillo.ui.field_required`.

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

`Text` and `Textarea` report finished English sentences, and so does
`Money` without a `Currency`. The three date kinds, `URL` and `Money`
with a `Currency` report `rastrillo.ui.*` keys instead, such as
`rastrillo.ui.date_invalid`, `rastrillo.ui.field_required` and
`rastrillo.ui.money_ambiguous`. A French app's error is in French
without the app writing one.

Some money keys carry values: the currency for `money_wrong_currency`,
the two amounts for `money_ambiguous`. `p.ErrorArgs()` holds them, per
field. Pass the key and its values to `Tf` when you render the error.
The generated form does this for every field:

```html
{{template "field-text" dict "L" $L "Name" "Name" "Label" (T $L "resource.ticket_types.field.name") "Value" .Fields.Name "Error" (Tf $L (index .Errors "Name") (index .ErrorArgs "Name")) "Required" true}}
```

`Tf` gives back a string that is not a key exactly as it was given, so
"Title is required" and your own sentences pass through unchanged. That
is why the generated form wraps every field's error without asking what
kind it is. Do the same in your own templates.

### Daylight saving

`time.ParseInLocation` resolves a wall-clock time that does not exist —
the hour a spring-forward skips — using the offset in force before the
transition, so a time inside the skipped hour and the real time it
collapses onto land on the same instant.

## Web addresses

`form.URL` takes a web address however it was typed and gives you one
you can link to:

```go
p := form.Parse(r, form.Field{Name: "website", Kind: form.URL})
site := p.String("website") // "example.com" arrives as "https://example.com"
```

It adds `https://` when there is no scheme, lowercases the host and
drops a bare trailing slash. It refuses any scheme but http and https,
a username or password in the address, a host with no dot, and
whitespace. A refused address reads back as `""`, so it cannot reach
your database even if you forget to check `p.OK()`. The echo keeps what
was typed.

Its errors are catalog keys, like the date kinds':
`rastrillo.ui.url_invalid`, `rastrillo.ui.url_credentials`, and
`rastrillo.ui.field_required` when a required field is blank. Wrap them
in `T` the same way.

Render the input with `field-url`, and show a stored address with
`displayURL` and `safeHref`; see
[Templates](/docs/templates#web-address-fields). To tell whether two
addresses are the same site, compare `form.URLKey` of each.

## Money

Store an amount as two plain fields: a whole number of the currency's
smallest unit, and its ISO 4217 code. Cents for USD, yen for JPY, fils
for KWD. Never a float.

Give a money field its `Currency`:

```go
p := form.Parse(r,
	form.Field{Name: "Name", Required: true},
	form.Field{Name: "Price", Kind: form.Money, Currency: "USD", LocaleEcho: true, Required: true},
	form.Field{Name: "Status"},
	form.Field{Name: "MaxPerOrder"},
)
```

`Currency` is the currency the amount is stored in, and it always comes
from the server: the record's on an edit, your default on a create. The
field accepts that currency's decimals: none for JPY, two for USD, three
for KWD.

Read the amount with `p.Minor`, and store the code beside it:

```go
func amount(p *form.Parsed, name, code string) (*int64, *string) {
	minor, ok := p.Minor(name)
	if !ok {
		return nil, nil
	}
	return &minor, &code
}
```

`ok` is false for a blank field and for a field with an error, so a
refused amount never reaches your database. A field with no amount
stores no currency either.

### What a money field accepts

A money field reads any text that has one plausible reading, and refuses
text that could mean two different amounts. It ignores spaces and
direction marks. It accepts the field's own currency written any way
CLDR spells it, before or after the amount. It reads group marks where
they can go, either decimal mark where only one reading fits, and a sign
or parentheses. Nothing is ever rounded.

| Page | Typed | Field | Stored | With `Strict` |
|---|---|---|---|---|
| en | `US$ 5` | USD | 500 | `money_currency` |
| en | `1.284,50` | USD | 128450 | `money_decimal_point` |
| en | `1,23,456` | USD | 12345600 | `money_decimal_point` |
| en | `12.5000` | USD | 1250 | `money_precision` |
| en | `1,500` | USD | 150000 | 150000 |
| en | `1.500` | USD | `money_ambiguous`: 1.5 USD or 1500 USD | `money_precision` |
| en | `(5.00)` | USD | -500, if the field allows negatives | `money_invalid` |
| en | `€5` | USD | `money_wrong_currency` | `money_currency` |
| en | `$5` | CAD | 500 | `money_currency` |
| en | `1.500` | KWD | `money_ambiguous`: 1.5 KWD or 1500 KWD | 1500 |
| en | `1.500` | JPY | `money_ambiguous_precision`: 1500 JPY | `money_precision_0` |
| de | `1.500` | EUR | 150000 | 150000 |
| de | `1,500` | KWD | `money_ambiguous`: 1,5 KWD or 1500 KWD | 1500 |
| fr | `1 284,50 €` | EUR | 128450 | 128450 |
| ja | `￥1,285` | JPY | 1285 | 1285 |
| ja | `1285円` | JPY | `money_invalid` | `money_currency` |

Zeros past the currency's decimals change nothing, so `12.5000` is 12.50
in a USD field. A nonzero digit past them is `money_precision`. `円` is
not how CLDR writes the yen in Japanese, so `1285円` is refused like any
other word.

### When the separator could be either mark

Text with one `.` or `,` and three digits after it has two readings.
When the page's language groups digits with that mark, the field takes
the grouping reading: on an English page `1,500` is fifteen hundred, and
on a German page `1.500` is fifteen hundred. Otherwise the field asks
which amount was meant: `1.500` on an English page could be 1.5 or 1500.

This has a cost. A German amount pasted into an English page, `1,500`
meaning 1.5 KWD, is stored as 1500 KWD, a thousand times too much. The
page shows the stored amount after the save, and that is where the
person can see it.

### Symbols shared by several currencies

A symbol that several currencies use, such as `$`, is read as the
field's own currency. The field always shows its ISO code beside the
input, so the person can see which currency `$` means there. A spelling
that names a different currency is refused with `money_wrong_currency`.

| Symbol | Shared by | Accepted | Refused |
|---|---|---|---|
| `$` | 30 dollars and pesos, among them USD, CAD, AUD and MXN | `$5` in a USD, CAD, AUD or MXN field | `US$5` in a CAD field; `CA$5` or `A$5` in a USD field |
| `¥` | JPY and CNY | `¥5` in a JPY or CNY field | `CN¥5` in a JPY field; `JP¥5` in a CNY field |
| `£` | GBP, FKP, GIP, SHP, SSP and SYP | `£5` in a GBP or GIP field | `£5` in an EGP field, whose symbol is `E£` |
| `kr` | SEK, NOK, DKK and ISK | `5 kr` in a SEK, NOK or DKK field | `SEK 5` in a NOK field; `kr 5` in a EUR field |
| `Rs` | PKR, LKR, NPR and MUR | `Rs 5` in a PKR or LKR field | `Rs 5` in an INR field, whose symbol is `₹` |

So a USD price pasted with a bare `$` into a CAD field is stored as CAD.
The ISO code beside the input is what lets the person catch it.

### Strict fields

Set `Strict: true` for a field that must take exactly the page
language's own format, such as an import column. A strict field
accepts one sign and one marker for its currency: the symbol in that
language, or the ISO code. It reads only that language's decimal and
group marks, with groups where they belong. More decimals than the
currency has, zeros included, is an error, and so is any other currency
sign (`money_currency`).

### The field's options

| Option | What it does |
|---|---|
| `Currency` | The ISO 4217 code the amount is stored in. Empty keeps the cents reading, below |
| `Negative` | Allows an amount below zero. Without it, `-5` and `(5)` are `money_negative` |
| `Strict` | Reads only the page language's exact format |
| `LocaleEcho` | Reads the language the text was typed in from the form, and checks the currency the form showed. The generated forms set it |
| `Locale` | The language to read the text in when `LocaleEcho` is off: for a hand-written handler or an API client |
| `Stored` | The record's current amount, on an edit form. Set it, or an unchanged amount is read again as text |

### Errors

Every money error is a `rastrillo.ui.*` key, translated in all twelve
shipped languages.

| Key | English | When |
|---|---|---|
| `field_required` | This field is required. | The field is `Required` and blank |
| `money_invalid` | Enter a valid amount. | The text is not an amount |
| `money_precision` | Enter an amount with no more than 2 decimal places. | Too many decimals for a two-decimal currency |
| `money_precision_0` | Enter a whole amount, with no decimal places. | Any decimals for a currency without them, such as JPY |
| `money_precision_3` | Enter an amount with no more than 3 decimal places. | Too many decimals for a three-decimal currency, such as KWD |
| `money_decimal_comma` | Use a comma for decimals, like 12,50. | The separators cannot be read, on a page that writes decimals with a comma |
| `money_decimal_point` | Use a point for decimals, like 12.50. | The same, on a page that writes decimals with a point |
| `money_wrong_currency` | Enter the amount in {currency}. | The text names another currency |
| `money_ambiguous` | This could be {a} or {b}. Enter the one you mean. | The text could be two amounts |
| `money_ambiguous_precision` | This could be {b}. {a} has too many decimal places for {currency}. Enter the amount you mean. | The text could be two amounts, and one has too many decimals |
| `money_currency` | Enter the amount without a currency sign. | A `Strict` field was given a currency sign it does not take |
| `money_negative` | Enter an amount of 0 or more. | Below zero, and the field does not allow it |
| `money_currency_changed` | The currency changed while you were editing. Check the amount and save again. | The currency changed between showing the form and saving it |
| `money_reenter` | Enter the amount again. | The form's hidden language or seed was refused |

`{currency}`, `{a}` and `{b}` are in `p.ErrorArgs()`. Each amount is
written with its ISO code and no group marks, so it cannot be misread.

### Edit forms

An edit form fills the field from the record and sends that text back in
a hidden `<Name>.seed`. When the posted text is exactly the seed, the
person left the field alone, and it keeps the record's amount without
reading the text. That is how `1.500` for 1.5 KWD saves on an English
page, though the text alone could mean two amounts.

The seed is never a value. Load the record in the POST handler and set
`Stored` to its amount. If the record no longer shows that seed, because
it changed or the seed was forged, the field reports `money_reenter` and
stores nothing. A create form never has a seed, and one posted to it is
refused the same way.

Fill the input with `form.SeedMoney`:

```go
func seed(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request, id int64, field string, minor *int64, code string) (form.MoneyInput, bool) {
	in, err := form.SeedMoney(rastrillo.FormatLocale(r), minor, code, false)
	if err != nil {
		view.Fail(ctx, w, r, fmt.Sprintf("%s %d: %s in %s", "ticket_types", id, field, code), err)
		return in, false
	}
	return in, true
}
```

After a failed POST, show the person's own text again with
`p.MoneyInput`. When it returns `reseed` true, the text could not be
trusted, so fill the field from the record instead:

```go
		priceInput, reseed := p.MoneyInput("Price", rastrillo.FormatLocale(r))
		if reseed {
			if priceInput, ok = seed(ctx, w, r, id, "Price", n.PriceMinor, priceCode); !ok {
				return
			}
		}
```

### Drawing the field

Render a money input with `field-money`. Give it the four values
`SeedMoney` or `p.MoneyInput` returned:

```html
{{template "field-money" dict "L" $L "Name" "Price" "Label" (T $L "resource.ticket_types.field.price") "Value" (index .Money "Price").Value "Locale" (index .Money "Price").Locale "Currency" (index .Money "Price").Currency "Seed" (index .Money "Price").Seed "Error" (Tf $L (index .Errors "Price") (index .ErrorArgs "Price")) "Required" true}}
```

It shows the ISO code in the label, and sends three hidden inputs with
the text:

| Input | Holds |
|---|---|
| `<Name>.locale` | The language the text is written in. Read in another, `1.500` can mean fifteen hundred |
| `<Name>.currency` | The currency the form showed. If it changed before the save, the field reports `money_currency_changed` and keeps the text |
| `<Name>.seed` | The text the server filled in, or empty. See [Edit forms](#edit-forms) |

The server compares the currency and then ignores it. The amount is
always stored in the server's currency, so a forged value can only cause
an error.

### Fields without a currency

A `Money` field with no `Currency` works as it always has. It reads
integer cents through `form.ParseCents`: no more than two decimal
places, no `$`, no sign. Read it with `p.Cents`, and seed it with
`form.FormatCentsPlain`, never `form.FormatCents`, which adds a `$` the
parser refuses. See [form](/docs/reference/form#money).

## What a form looks like

Everything above is about reading a form. This is about drawing one,
and it is the half that gets hand-rolled by accident: `<label>Email
<input></label>` compiles, submits and validates perfectly well, so
nothing fails — it just renders as a ragged column of labels sitting
inline beside inputs of a dozen different widths, with an unstyled
button under it.

**A labelled control is never hand-written.** There is a partial for
every field kind, and each draws the label above the control, wires
`aria-describedby` to the lines that actually rendered, and sets
`aria-invalid` when there is an error. `field-text` and
`field-textarea` are the two you reach for most:

```html
{{define "content"}}{{$L := localizer $}}
{{template "page-header" dict "L" $L "Title" "New note"}}
{{with .Content}}
<form rst-form method="post" action="/notes">
{{template "field-text" dict "L" $L "Name" "title" "Label" "Title" "Value" .Note.Title "Required" true "Autofocus" true "Error" (index .Errors "Title")}}
{{template "field-textarea" dict "L" $L "Name" "body" "Label" "Body" "Value" .Note.Body "Error" (index .Errors "Body")}}
{{template "form-foot" dict "L" $L "Submit" "Create" "CancelHref" "/" "CancelLabel" "Cancel"}}
</form>
{{end}}
{{end}}
```

Every partial call passes `"L" $L`, the page's localizer, so the
partial's own words come out in the reader's language. See
[Templates](/docs/templates#template-functions).

The rest are `field-money`, `field-select`, `field-check`, `field-date`,
`field-datetime`, `field-time` and `field-daterange`. `field` is the
older, more configurable text control. Use it when you need `Pattern`,
`Maxlength` or `Placeholder`.

**Read the partial's own doc comment before your first call to it;
the keys are not uniform.** `field-text` derives the control's `id`
from `Name`, and its `Hint` is the muted line under the control.
`field-select` and `field` take `ID` *and* `Name` separately, put
`Hint` in parentheses after the label, and use `Help` for the line
underneath. Copying one call shape onto the other silently renders
`for=""` and drops the guidance.

`rst-form` is yours to write, as `rst-page` and `rst-list` are. It is
what makes the column a column: a `44rem` maximum, so lines stay
readable, and a consistent gap between fields. Without it the fields
are loose in the page and inherit whatever the surrounding layout does.

`form-foot` closes the form. It emits one primary submit and, given
`CancelHref`, a cancel that is a real `<a>` — leaving a form is
navigation, so it must survive middle-click, a new tab and no JS.
Destructive actions do not belong there; they get a confirm route of
their own.

### Buttons have a size

`rst-btn` comes in three steps — `sm`, the default, and `lg` — and the
size composes with the variant, so a form's submit is
`rst-btn="primary lg"`. `form-foot` and `confirm-form` both write that
for you; the one place a submit stays at the default step is the sticky
save bar (`rst-form-bar`), which is persistent chrome pinned to the
viewport rather than the end of a form, and where a taller button just
eats the page.

The default step is sized for a control that sits beside other controls:
a page-header action, a button in a row. It is the wrong size for a
form's submit, and the failure is easy to recognise once you have seen
it — a 34px-tall button with a 12.5px label stretched the full width of
a 44rem column reads as a skinny blue bar rather than the thing the
screen is asking you to press. If you are reaching for a full-width
button, that is `rst-btn="primary lg block"`, and `block` centres its
own label.

## The main field

Most forms have one main field that is required to be filled and
typically labels the record.

Mark it `Primary` to get a bigger input; the label stays the same, so
the emphasis lands on what someone types. One per form. A primary field
almost always sits on its own row (except in cases like first/last name).

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

`busy.js` gives every submit button a loading state while its form is
out — a spinner in place of the label for at least 650ms, `aria-busy`,
then `disabled` — and refuses a second submit from the same form while
the first is in flight. It is on by
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
