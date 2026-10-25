# manifest/ — the declarative path

Manifest-driven and code-driven are equal, optional paths: declare the
resources that are pure CRUD, hand-write the ones that aren't, per
resource, in one app. One TOML file here declares a whole screen set —
List → Show → Edit/New plus the confirm-page delete flow — generated
as real committed code under gen/ by `rastrillo generate`:

    name     = "ticket_types"
    route    = "/admin/ticket_types"
    currency = "USD"

    [list]
    columns = [{ field = "Name", kind = "text" }, { field = "Price", kind = "money" }]
    search  = true

    [form]
    basics = [{ name = "Name", required = true }, { name = "Price", kind = "money" }]

The vocabulary covers one flat table per resource, three field kinds,
no relations. Add `scope = "user"` and every generated query is
owner-filtered by the session subject — someone else's row answers
404 — for resources a user owns; mount those routes behind
`sessions.Require`. Add `store = "mergeable"` and the resource lives as
event-log streams (tombstone deletes, derive-on-read) instead of a
sqlc table — it needs no sqlc tool. Relations or a custom flow:
hand-write it.

Adding the first manifest to this app:

1. `go get -tool github.com/sqlc-dev/sqlc/cmd/sqlc` (once — the
   generated store is sqlc input).
2. `rastrillo generate` writes gen/ — committed, never hand-edited.
3. Embed the generated templates. This file goes at the MODULE ROOT,
   not beside the hand templates: a go:embed pattern cannot contain
   "..", and gen/templates is not under internal/%[2]s. Call it
   genassets.go —

       package %[2]sassets

       import "embed"

       //go:embed gen/templates
       var GenTemplatesFS embed.FS

   `rastrillo generate` only ever clears and rewrites gen/actions/,
   never a file sitting beside gen/, so this survives regeneration.

4. Add the generated screens to the pages render.go builds, and give
   the generated actions their Ctx.Render seam. A generated action calls
   Ctx.Render with a page name that is always "<resource>/list",
   "<resource>/show", "<resource>/form" or "<resource>/confirm". In
   internal/%[2]s/render.go's newPages, after the hand pages, add:

       gen, err := fs.Sub(%[2]sassets.GenTemplatesFS, "gen/templates")
       if err != nil {
           return nil, err
       }
       genFiles, err := fs.Glob(gen, "*/*.html")
       if err != nil {
           return nil, err
       }
       for _, f := range genFiles {
           files[strings.TrimSuffix(f, ".html")] = ui.PageFile{FS: gen, Path: f}
       }

   and Catalog: genlocales.BaseCatalog to its ui.PagesConfig, so the
   generated strings are found. Import "io/fs", "strings" and
   %[2]sassets "%[1]s". Then add the seam:

       // GenRender is the Ctx.Render seam every generated action calls.
       func GenRender(ctx *rastrillo.Ctx, w http.ResponseWriter, name string, status int, data any) {
           var buf bytes.Buffer
           if err := pages.Execute(&buf, name, data); err != nil {
               if ctx != nil && ctx.Logger != nil {
                   ctx.Logger.Error("%[2]s: rendering "+name, "err", err)
               }
               http.Error(w, "something went wrong", http.StatusInternalServerError)
               return
           }
           w.Header().Set("Content-Type", "text/html; charset=utf-8")
           w.WriteHeader(status)
           buf.WriteTo(w)
       }

   A generated view carries the request's localizer, switcher items and
   return path, so the layout renders it as it renders a hand page.
   When a generated screen needs this request's flash or signed-in
   state, make GenRender a func(r) returning a rastrillo.RenderFunc
   closed over r, and call it from the ctx factory: RenderFunc carries
   no *http.Request of its own.

5. Apply the generated store's migrations at boot, or the table the
   screens read does not exist. In internal/%[2]s/migrations.go:

       import (
           "fmt"

           bookmarksstore "%[1]s/gen/store/bookmarks"
       )

       func genSchemaSet() *migrate.Set {
           s := new(migrate.Set)
           for i, stmt := range bookmarksstore.Migrations {
               s.Add(migrate.Migration{ID: fmt.Sprintf("bookmarks_gen/%%04d_init", i+1), SQL: stmt})
           }
           return s
       }

       var genSchema = genSchemaSet()

       var BootSchema = migrate.Merge(apitoken.Schema, genSchema, Schema)

   Keep every schema already in BootSchema, apitoken.Schema and any
   other subsystem's. A schema left out is a table boot never creates:
   without api_tokens, every API request fails.

   genSchema goes in BootSchema and never in Schema: Schema is what
   `rastrillo migration check` diffs against Models, and a generated
   table in it reads as one Models forgot.

   generate never edits a statement in gen/store/<name>/migrations.go
   once it has been released. Adding a field appends a statement, and
   genSchemaSet gives it the next ID, so the ledger applies it as a new
   migration.

6. Boot the generated code and mount its router inside the chi router.
   In internal/%[2]s/app.go's Router, after migrate.Apply and the routes,
   add (and import "%[1]s/gen"):

       booted, err := gen.Boot(context.Background(), d.Writer())
       if err != nil {
           return nil, err
       }
       gmux := gen.Router(booted, func(*http.Request) *rastrillo.Ctx {
           return &rastrillo.Ctx{DB: d.Writer(), Logger: logger,
               Actor: rastrillo.Actor{}, Authorize: authorize,
               Render: GenRender}
       })
       r.Handle("/admin/*", gmux)

   gen.Boot upgrades and checks the generated money columns, and
   gen.Router will not build without what it returns. Mounted inside
   the chi router, the generated screens get the same CSRF check and
   the same localizer as the hand ones. Authorize is api.go's
   authorize, so the pages and the API follow the same rules.

7. Commit api/v1.json with gen/. A resource with scope = "user" is on
   the JSON API at /api/v1/<name> after its first generate. A resource
   with no scope stays off it until its manifest adds:

       [api]
       expose = true

   Tokens are still refused there until you allow them in api.go's
   authorize. `make ci` runs `rastrillo generate --check`, which fails
   when gen/ or api/v1.json is out of date.

Drop to a .go manifest in this directory (package manifest, a
`var X = rastrillo.Resource{...}`) the moment you need a function
value — a custom Column.Render, say. To take any single generated
screen over by hand, write your own action file at the same computed
path under actions/ — the generator skips that one from then on.
