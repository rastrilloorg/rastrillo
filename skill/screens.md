# Screens, flows and the gate

## Add a screen

1. **Route** in `Router` (`app.go`): `r.Get("/sheets/new", a.newSheet)`;
   an owned screen goes inside the `sess.Require` group.
2. **Handler** in `handlers.go`: load through the `owned(r)` seam, 404
   on a miss, then `render(w, r, "sheet-new", map[string]any{...})`.
   On a failed POST write `422` before rendering; after a mutation
   `flash.Set(w, r, "notice", "...")` and 303 (showing it: skill/forms.md).
3. **Template** `templates/sheet-new.html` defining `"content"`, its
   body starting `{{$L := localizer $}}`, built from `ui` partials
   (skill/partials.md). **Add its name to the page list in
   `render.go`'s `newPages`**, or `render` fails on it.
4. **Perf:** `make perf` measures every GET route the router serves.
   A route with a parameter needs `ScreenConfig.Paths["/sheets/{id}"] =
   "/sheets/1"` with that row seeded first; a screen behind sign-in
   needs `ScreenConfig.Signin` (skill/add-signin.md step 8); one that
   answers other than 200 needs `Expect`. A new GET route fails there
   until measured or recorded in `.rastrillo/budgets.txt`.
5. **Test** it in `internal/<app>test` through the real handler.

## One screen, one job

A screen shows a thing, or asks for one thing — never both: no list
page that also carries a create form, an import panel and a dropzone.

Nearly every interaction is the same four-step flow, as full pages or
as modals (a modal is its own URL, with the same back button):

1. **A link naming the action**, on the screen you are already on: "New
   sheet", "Import a spreadsheet". A link or button, never the form
   itself inlined.
2. **A page for that action alone** — `GET /sheets/new` — one form, one
   primary button.
3. **An interstitial only when the work outlives the request**: the
   jobs status page (skill/background.md). Skip it when the POST
   answers immediately.
4. **A confirmation** — 303 to the new thing's show page, with a flash
   notice. Re-rendering the form is not a confirmation.

An empty list is step 1, not an exception: `empty-state` says what the
screen is for and carries the one link. Destructive actions are the same
shape, with `confirm-form` on its own URL at step 2, never a modal fired
from the row.

A GET handler never changes anything, and no script changes anything
while `document.prerendering` is true: `Serve` prerenders shell
navigation, on a phone before it is tapped (turn it off with
`Options.NoSpeculationRules`).

## Tests and budgets

`make ci` holds each directory to 5,000 source and 8,000 test lines,
each test package to 10s, each GET screen to 150ms to first byte and a
cold boot to 500ms; exceptions go in `.rastrillo/budgets.txt`, each with
a reason. Timing fails only on CI (`AMADAN_CI`/`CI` set). Every test
package's `TestMain` calls `os.Exit(budget.Main(m))`: it prints the time
`rastrillo budget test` judges, cached runs included. Tests start with
`t.Parallel()` (perf tests never) and open a copy of a `dbtest` template
instead of migrating. While editing, test the package you touch plus
`internal/<app>test`; `make ci` before pushing. A test whose inputs Go
cannot see (execs `go build`, reads outside its package, uses the
network) gets its own `-count=1` target. Past 5,000 lines in
`internal/<app>`, split by feature: `internal/<app>/<feature>` exposing
`Mount(r chi.Router, deps)`, called from `Router`; `make budget`
enforces it. docs/site/testing.md

## Error pages

**Never hand-roll an error page.** `view.Fail`/`NotFound`/`Forbidden`
render styled pages inside the shell; a 500 shows a ref matching the
log line's `ref`. Wire `opts.ErrorPage` (and `Ctx.ErrorPage`) to a
`rastrillo.ErrorPageFunc` — `rastrillo new` scaffolds it as
`render.go`'s `ErrorPage` over `templates/errors.html`; panics recover
there. Unwired, errors are bare text. docs/site/templates.md
