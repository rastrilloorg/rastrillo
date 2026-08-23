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

## The three kinds

The zero value is `Text`, so a plain `form.Field{Name: "Title"}` does
the common thing.

| Kind | Read with | Behaviour |
|---|---|---|
| `Text` (zero value) | `p.String` | Trimmed; value and echo are both the trimmed text |
| `Textarea` | `p.String` | Kept exactly as typed, whitespace included; only the required check trims |
| `Money` | `p.Cents` | Parsed to integer cents; the echo keeps the raw text |

`Required` on a blank `Text` or `Textarea` reports "<Field name> is
required", humanized from the field name.

`Required` on `Money` is checked against the raw text, so `""` is
required-blank while `"0"` is a present, valid zero. A present but
unparseable value reports the parse error instead of the required
message — telling someone a field is required when they filled it in
would be a small lie the parser is in a position to avoid.

`Parse` reads through `r.PostFormValue`, which parses the form on first
use. If you want a body-size cap or a 400 on a malformed body, wrap
`r.Body` and call `r.ParseForm` yourself first, the way the generated
actions do.

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
