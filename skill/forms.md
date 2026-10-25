# Forms

Never bind a request body onto a GORM model. Read each permitted field
by name (`r.PostFormValue("Title")`), write through
`.Select("Title", "Body").Updates(...)` — `Select`'s strings are GORM
field names, the allowlist keeping unexpected fields out of columns.

Validate with `form.Parse(r, form.Field{Name: "title", Required: true},
form.Field{Name: "body", Kind: form.Textarea})` — one declaration per
field. On `!p.OK()`: `w.WriteHeader(422)`, re-render seeding values
(`p.String("title")`; `p.Echo()` is the seed-back map for
map-shaped views). Write the status before rendering; the render helper
writes none.

**Money:** `form.Field{Kind: form.Money, Currency: code, LocaleEcho:
true}`, code from the server; read `p.Minor`, store the code with it;
seed `form.SeedMoney`, draw `field-money`, show `{{money $L .Amount
.Currency}}`. Input is normalised; `Strict: true` keeps the exact
format. On edit set `Stored`: an unchanged `<Name>.seed` keeps it, so
an ambiguous seed saves. No `Currency`: legacy USD cents (`p.Cents`,
`""` = 0, seed `FormatCentsPlain`, show `FormatCents`). A manifest
field of kind `money` does all of this for you (SKILL.md).

**Dates:** `form.Date` and `DateTime` parse fixed layouts only, in
`Field.Location`; `form.Range(p, "starts", "ends")` checks
end-before-start for those two only. `form.Time` reads back as
`p.Time(name) (h, m int, ok bool)`, a time of day, which `Range` does
not compare. Three traps: empty, unparseable and undeclared dates
all read back as the zero time — ask `p.OK()`, never `IsZero`; their
error values are `rastrillo.ui.*` **keys**, not sentences, so render
`"Error" (T $L (index .Errors "Starts"))`; and `field-daterange`'s two
halves need distinct `Name`s. docs/site/forms.md

**Web addresses:** `form.URL` + `field-url` (scheme optional; errors are
keys too). Link a stored one only through `{{with safeHref .Site}}`,
show it with `displayURL`; never `<input type="url">`.

**After a mutation:** `flash.Set(w, r, "notice", "...")`, then 303. The
scaffold does not show flashes until you wire it: in `render.go`'s
`render`, `if f, ok := flash.Take(w, r); ok { page["Flash"] =
f.Message }`, and in `layout.html`, just before `{{template "content"
.}}`, `{{template "notice" (opt $ "Flash")}}`. Take clears the flash
with a header, so it only works before the status is written: a
handler that writes 422 and then renders shows a pending flash without
clearing it. Flashes follow a 303, so take them only on pages rendered
with 200.

**A labelled control is never hand-rolled:** `field-text`,
`field-textarea`, `field-select` (and the date kinds) inside `<form
rst-form>`, closed by `form-foot`. A hand-written `<label>Email
<input></label>` validates fine and renders as ragged inline labels
over an unstyled button — nothing fails, so nothing tells you. A
submit is `rst-btn="primary lg"` (form-foot writes it), never the
toolbar-sized default stretched across the column.

## Public forms

`rastrillo/pow`: sealed single-use challenge, honeypot, proof of work.
Apply `pow.Schema` BEFORE `pow.New` (else `ErrNoSchema`):
`pow.New(Config{InstanceKey, Nonces: pow.SQLNonces(writer), ScriptURL,
WorkerURL})`, the URLs where you mount
`rastrillo.NewAssets(pow.Assets())` (e.g. at `/pow/`): serve the JS from
the module, never vendor it (a copy drifting from the Go verifier fails
silently). The app owns one Guard for every form, `auth` and `password`
included, and sweeps it with `guard.Sweep(now)` from a
`background.Group` loop. Sign-in: `Difficulty: 16` (p95 under 1s at 6x
CPU throttle; default 18), `MinAge: 500ms`. Render `f := g.Form(now,
scope)`: `.Attrs` on the `<form>`, `.Fields` inside, `.Script` once per
page, `"Proof"` to `form-foot` (disabled `data-pow-submit` button,
status line, `<noscript>`). Namespace the scope (`myapp/contact`); it is
sealed and compared. **Never cache a page carrying a challenge**
(`no-store`): every visitor gets one token. `NoProof` (token +
honeypot) needs no JavaScript; proof of work does. In a transaction:
`adm := g.Admit(r, pow.Want{Scope: s})` BEFORE `BeginTx` (one writer
connection), validate, `adm.Commit(ctx, tx)` inside (GORM:
`tx.Statement.ConnPool`); `ErrSpent` is a refusal, never a 500.
Redirect-after-POST, or without a transaction: `g.Check(r, want)`, never
inside an open tx. After a refusal render `g.Recovery(now, scope)`
(trapless; sticky via `adm.Recovered()`): it recovers the challenge, not
the visitor's answers (refilling those is the app's risk and needs an
idempotent write). Tests fill a challenge with `powtest.Fill`.
docs/site/reference/pow.md
