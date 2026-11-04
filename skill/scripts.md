# JavaScript: enhance pages, freely

The framework's partials work with scripts off wherever they can (a
proof-of-work form cannot). That is the framework's floor, not a limit
on your app. Rastrillo apps in production use small vanilla scripts for
frictionless navigation, live regions, searchable selects, drag and
drop and rich text editing. Build each screen from links and forms,
then make it as rich as the product needs with scripts. A rich interaction is never a reason to drop a
feature or tell the operator it is unsupported.

## Check what ships first

Scaffolded into `static/`, app-owned, inert until markup opts in:
`select.js` (a `field-select` past ten options becomes a searchable
combobox; `data-rst-select` on any `<select>`), `datetime.js` and
`calendar.js` (date fields), `live.js` (skill/live.md), `rastrillo.js`
(polling, menus), `busy.js` (submit feedback), `shell.js` and
`transition.js` (view transitions between pages).

## Write your own

1. **The form first.** A drag-and-drop board still has a "Move to
   Tuesday" form for each card, posting to its own route. The script
   then does the same POST from a drop, with `fetch` from the same
   origin (`csrf.Protect` passes it), and swaps in the HTML the route
   answers or reloads the region.
2. **One file per behaviour** in `static/`, opted into by a `data-`
   attribute, loaded with `<script defer src="{{path $ (asset
   "static/board.js")}}">` only on the pages that use it. Your scripts
   are outside the 16 kB framework budget; keep them small anyway.
3. **Modules that import each other** load with `<script type="module"
   src>`, the entry point through `asset` as above, and import by
   relative path (`./board-drag.js`): the assets handler serves those
   unhashed with no-cache, so there is still no build.
4. **An import map** when vendored packages import each other by bare
   name. It is an inline script, which the default CSP blocks: set
   `Options.CSP` to the default policy (`defaultCSP` in `serve.go`)
   plus `script-src 'self' 'sha256-<hash of the map>'`.
5. **A build step** when the graph outgrows that: TypeScript, JSX, or a
   library with many dependencies. Use esbuild (`go run
   github.com/evanw/esbuild/cmd/esbuild@<version>`, so no Node in the
   app), bundle to one ES module in `static/`, commit the output,
   and add a `make js` target that rebuilds it.

## Rules that still hold

- No inline `<script>` but a hashed import map, and no `on*`
  attributes: the CSP drops them. Wire handlers with
  `addEventListener`.
- No `style` attributes in markup or via `setAttribute("style")`: set a
  class, or `element.style.setProperty` (CSSOM, which the CSP allows)
  for a drag's position.
- No script changes anything while `document.prerendering` is true.
- live.js and `data-poll` replace a region's elements, and listeners
  on them with it: delegate from `document` instead.
- Where a form can do the job, it still does with the script deleted.
