# Forms

Never bind a request body onto a GORM model. Read each permitted field
by name (`parsed.String("Title")`), write through
`.Select("Title", "Body").Updates(...)` — `Select`'s strings are GORM
field names, the allowlist keeping unexpected fields out of columns.

Validate in the action with `parsed, err :=
visit.ReadForm(form.Field{Name: "title", Required: true},
form.Field{Name: "body", Kind: form.Textarea})`, one declaration per
field (over `form.Parse`). A field that does not parse comes back as
`op.Invalid`, and `visit.RefusedShowing(err, again)` calls your form's
`ShowAgain` with 422 (skill/code.md): render at that status, seeding
values (`parsed.String("title")`; `parsed.Echo()` is the seed-back map
for map-shaped views).

**Money:** `form.Field{Kind: form.Money, Currency: code, LocaleEcho:
true}`, code from the server; read `parsed.Minor`, store the code with it;
seed `form.SeedMoney`, draw `field-money`, show `{{money $L .Amount
.Currency}}`. Input is normalised; `Strict: true` keeps the exact
format. On edit set `Stored`: an unchanged `<Name>.seed` keeps it, so
an ambiguous seed saves. No `Currency`: legacy USD cents (`parsed.Cents`,
`""` = 0, seed `FormatCentsPlain`, show `FormatCents`). A manifest
field of kind `money` does all of this for you (SKILL.md).

**Dates:** `form.Date` and `DateTime` parse fixed layouts only, in
`Field.Location`; `form.Range(parsed, "starts", "ends")` checks
end-before-start for those two only. `form.Time` reads back as
`parsed.Time(name) (h, m int, ok bool)`, a time of day, which `Range` does
not compare. Three traps: empty, unparseable and undeclared dates
all read back as the zero time — ask `parsed.OK()`, never `IsZero`; their
error values are `rastrillo.ui.*` **keys**, not sentences, so render
`"Error" (T $L (index .Errors "Starts"))`; and `field-daterange`'s two
halves need distinct `Name`s. docs/site/forms.md

**Web addresses:** `form.URL` + `field-url` (scheme optional; errors are
keys too). Link a stored one only through `{{with safeHref .Site}}`,
show it with `displayURL`; never `<input type="url">`.

**After a mutation:** `visit.Flash("...")`, then `visit.Redirect(path)`,
a 303. The scaffold does not show flashes until you wire it. In
`render.go`'s `renderPage`, replace `data = withRequest(request,
values)` with the lines below (and import `flash`); in `layout.html`,
just before `{{template "content" .}}`, add `{{template "notice" (opt $
"Flash")}}`. Flashes follow a 303, so take them only on pages rendered
with 200.

```go
values = withRequest(request, values)
if status == http.StatusOK {
	if notice, found := flash.Take(response, request); found {
		values["Flash"] = notice.Message
	}
}
data = values
```

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
