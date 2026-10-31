# manifest/: the declarative path

Manifests and hand-written code are equal, optional paths: declare a
resource that is pure CRUD here, and hand-write the rest. One TOML file
defines a screen set (list, show, edit, new and a confirm-page delete).
`rastrillo generate` writes it as committed code under gen/:

    name     = "ticket_types"
    route    = "/admin/ticket_types"
    currency = "USD"

    [list]
    columns = [
        { field = "Name", kind = "text" },
        { field = "Price", kind = "money" },
    ]
    search  = true

    [form]
    basics = [
        { name = "Name", required = true },
        { name = "Price", kind = "money" },
    ]

Use one flat table per resource and choose from three field kinds;
manifests do not support relations. With `scope = "user"`, every
generated query is filtered to the signed-in owner (another's row is a
404). `store = "mergeable"` keeps the resource as event streams instead
of a sqlc table, and needs no sqlc. For relations or a custom flow,
hand-write the code.

Adding the first manifest, the one above as
manifest/ticket_types.toml:

1. `go get -tool github.com/sqlc-dev/sqlc/cmd/sqlc` (run once; the
   generated store is sqlc input).
2. `rastrillo generate` writes gen/ as committed code; do not edit it by
   hand.
3. Embed the generated templates in genassets.go at the module root.
   `go:embed` cannot reach up to gen/ from internal/%[2]s:

       package %[2]sassets

       import "embed"

       //go:embed gen/templates
       var GenTemplatesFS embed.FS

   `generate` never touches a file beside gen/.

4. In internal/%[2]s/render.go's newPages, before its return, add the
   generated screens:

       generated, err := fs.Sub(
           %[2]sassets.GenTemplatesFS, "gen/templates")
       if err != nil {
           return nil, err
       }
       names, err := fs.Glob(generated, "*/*.html")
       if err != nil {
           return nil, err
       }
       for _, name := range names {
           page := strings.TrimSuffix(name, ".html")
           files[page] = ui.PageFile{FS: generated, Path: name}
       }

   and Catalog: genlocales.BaseCatalog to its ui.PagesConfig, so the
   generated strings are found. Import "io/fs", "strings" and
   %[2]sassets "%[1]s". A generated action renders "<resource>/list",
   "/show", "/form" or "/confirm" through contextFor's Render, as a
   hand page does.

5. In internal/%[2]s/migrations.go, add generatedSchema() to
   BootSchema's Merge, just before Schema, then add:

       import (
           "fmt"

           ticket_typesstore "%[1]s/gen/store/ticket_types"
       )

       func generatedSchema() *migrate.Set {
           set := new(migrate.Set)
           for index, statement := range ticket_typesstore.Migrations {
               set.Add(migrate.Migration{
                   ID:  fmt.Sprintf("ticket_types_gen/%%04d_init", index+1),
                   SQL: statement,
               })
           }
           return set
       }

   Keep it out of Schema: migration check diffs Schema against Models.
   When you add a field, append a statement with the next ID.

6. Boot the generated code and mount its screens. In
   internal/%[2]s/app.go, add a field to App:

       generated http.Handler

   In Router, after app is built and before the Routes call, add this
   and import "%[1]s/gen":

       booted, err := gen.Boot(context.Background(), database.Writer())
       if err != nil {
           return nil, err
       }
       app.generated = gen.Router(booted, app.generatedContext)

   Add this at the end of app.go:

       func (app *App) generatedContext(request *http.Request) *rastrillo.Ctx {
           ctx, err := app.contextFor(request)
           if err != nil {
               panic(err)
           }
           return ctx
       }

   In routes.go, after guarded.Get(homePage, app.home), add:

       guarded.Mount("/admin", app.generated, "generated")

   For a resource with scope = "user", use the owned group (owned.Mount)
   behind the session guard. A plain app has no sign-in. Mount every
   prefix the manifests' routes use.

   gen.Boot upgrades and checks the generated money columns, and
   gen.Router will not build without what it returns. contextFor gives
   a generated screen the request's language and base path, and
   Authorize: authorize, so api.go's one policy rules the pages and the
   API alike. gen.Router's context has no error to return, so a failing
   contextFor panics: a logged 500 with its cause. Generated screens do
   not show sign-in state yet.

   In internal/%[2]stest/perf_test.go, add to its perftest.ScreenConfig,
   with a screen for each resource's list:

       Opaque: []perftest.Opaque{{
           Patterns: []string{"/admin/*"},
           Screens: []perftest.OpaqueScreen{{
               Key: "GET /admin/ticket_types", Path: "/admin/ticket_types",
           }},
       }},

7. Commit api/v1.json with gen/. A resource with scope = "user" is on
   the JSON API at /api/v1/<name> after its first generate. A resource
   with no scope stays off the API until its manifest adds:

       [api]
       expose = true

   Allow tokens in api.go's authorize before the API accepts them.

8. Run `rastrillo generate` again to compile the generated templates
   with the app's templates. `make ci` runs
   `rastrillo generate --check`, which fails when anything `generate`
   writes is out of date.

For a function value such as a custom Column.Render, write a .go
manifest here (package manifest, `var X = rastrillo.Resource{...}`). To
take over a generated screen, write an action file at its path under
actions/, tagged `//go:build rastrillo_actions`. `generate` skips that
file.
