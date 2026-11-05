# Screens, flows and the gate

## Before the first screen: polish and personality

A first slice built without a target comes out plain. The design system
is the base the app's personality goes on, not the finished look, and
the personality comes from the operator, at the start. Before the
first screen of a new app or a port, send them one message that asks:

1. **How polished?** A working draft to click through, ready for
   customers, or a showcase that has to impress.
2. **Its personality.** A logo, brand colours and typefaces if they
   have them, and three words for how it should feel ("calm, warm,
   local"; "bold, fast, playful").
3. **A screenshot of one key flow** as it should work: the old app's
   screen when porting, or any product they admire.
4. **With no screenshot, a base to start from.** A shipped theme
   (`day`, `plain`, `signal`) and shell (`column`, `topbar`, `sidebar`,
   `console`, `stage`), with a link to see them,
   fiesta.rastrillo.org, and the practices you will apply, all
   of which ship: a dashboard opening with stat cells; empty states
   that say what a screen is for; whole-row links with row menus; hints
   and inline errors on fields; searchable selects and natural-language
   dates; busy buttons, flash notices and confirm pages; live regions;
   view transitions between pages.
5. **An offer of quick mockups**: two or three takes on the key screen,
   each wearing the personality, as a real page with fixed data in a
   throwaway `rastrillo new --theme=... --shell=...` scaffold, real
   partials and icons, sent as screenshots or a link to pick from.

Then build to the answer: the base as `rastrillo new --theme --shell`
flags, the personality as `--rst-*` overrides for colour, type and
shape in the app's own stylesheet (skill/design.md), and the practices
above. Until an answer arrives, propose a personality from what the
product is for, apply it the same way and tell the operator, rather
than shipping the stock theme.

## Add a screen

1. **Route** in `Routes` (`routes.go`): name the path,
   `const newSheetPage action.Path = "/sheets/new"`, and mount it in
   the page group, `guarded.Get(newSheetPage, app.newSheet)`; an owned
   screen goes in the `Group` behind the session guard.
2. **Action** in `handlers.go`, `func (app *App) newSheet(visit
   *action.Visit)` (its shape: skill/code.md): load through
   `app.owned(visit.Request())`, `op.NotFound{}` on a miss, then
   `visit.Render("sheet-new", http.StatusOK, map[string]any{...})`. A
   failed POST shows the form again with `visit.RefusedShowing` (422);
   after a mutation, `visit.Flash("...")` and `visit.Redirect(path)`,
   a 303 (showing the flash: skill/forms.md).
3. **Template** `templates/sheet-new.html` defining `"content"`, its
   body starting `{{$L := localizer $}}`, built from `ui` partials
   (skill/partials.md). **Add its name to the page list in
   `render.go`'s `newPages`**, or rendering it fails.
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
a reason. Timing fails only on CI (`AMADAN_CI`/`CI` set). `make perf`
also holds each page's framework CSS and JS to 16 kB gzipped, plus each
component script it loads, on every machine (a live page has its
shell's own limit; one whose layout links the whole `tokens.css`,
40 kB); a failure names the file that grew (`rastrillo doctor`). It
also fails a page when its slice lacks a rule for a hook that the whole
`tokens.css` would apply to the page, such as one in HTML built in Go:
add `{{needs "rst-card"}}` to the page's template. Every test
package's `TestMain` calls `os.Exit(budget.Main(m))`: it prints the time
`rastrillo budget test` judges, cached runs included. Tests start with
`t.Parallel()` (perf tests never) and open a copy of a `dbtest` template
instead of migrating. While editing, test the package you touch plus
`internal/<app>test`; `make ci` before pushing. A test whose inputs Go
cannot see (execs `go build`, reads outside its package, uses the
network) gets its own `-count=1` target. Past 5,000 lines in
`internal/<app>`, split by feature: `internal/<app>/<feature>` exposing
`func Mount(routes *action.Routes, deps *Deps)`, called from the app's
`Routes`, the one function of that name (`rastrillo routes` finds it);
`make budget` enforces it. docs/site/testing.md

## Error pages

**Never hand-roll an error page.** `visit.Refused(err)`, over
`view.Fail`/`NotFound`/`Forbidden`, renders styled pages inside the
shell; a 500 shows a ref matching the log line's `ref`. Wire
`options.ErrorPage` (and `Ctx.ErrorPage`) to a
`rastrillo.ErrorPageFunc` — `rastrillo new` scaffolds it as
`render.go`'s `ErrorPage` over `templates/errors.html`; panics recover
there. Unwired, errors are bare text. docs/site/templates.md
