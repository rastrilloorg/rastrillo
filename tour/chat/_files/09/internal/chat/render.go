package chat

import (
	"embed"
	"html/template"
	"maps"
	"net/http"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/flash"
	"amadan.net/rastrillo/rastrillo/live"
	"amadan.net/rastrillo/rastrillo/locales"
	"amadan.net/rastrillo/rastrillo/ui"

	genlocales "chat/gen/locales"
	"chat/internal/chat/icons"
)

//go:embed templates static
var appFS embed.FS

// assets fingerprints static/: {{asset "static/tokens.css"}} is a URL
// with the content's hash in it, so it is cached for a year.
var assets = rastrillo.NewAssets(appFS)

var pages, pagesErr = newPages()

func newPages() (*ui.Pages, error) {
	config, err := locales.CheckConfig(
		genlocales.DefaultLocale, genlocales.Locales)
	if err != nil {
		return nil, err
	}
	files := map[string]ui.PageFile{}
	for _, name := range []string{"index", "errors", "tokens", "admin_rooms",
		"admin_room", "admin_room_delete", "room"} {
		files[name] = ui.PageFile{Path: "templates/" + name + ".html"}
	}
	return ui.NewPages(ui.PagesConfig{
		App: appFS, Layout: "templates/layout.html",
		Pages: files, Locale: config, Assets: assets,
		// Both icon seams, always: icon alone breaks cdn and js icons.
		Funcs: template.FuncMap{"icon": icons.Icon, "iconAssets": icons.Assets,
			"asset": assets.Path},
	})
}

// contextFor is each request's Ctx. Its Render draws through the request
// it was built for, so a page has that request's language and base path.
func (app *App) contextFor(request *http.Request) (*rastrillo.Ctx, error) {
	return &rastrillo.Ctx{
		DB: app.database.Writer(), Logger: app.logger, Assets: assets,
		Authorize: authorize, ErrorPage: ErrorPage,
		Render: func(_ *rastrillo.Ctx, response http.ResponseWriter,
			page string, status int, data any) {
			renderPage(response, request, app.live, page, status, data)
		},
	}, nil
}

func renderPage(
	response http.ResponseWriter, request *http.Request, hub *live.Hub,
	page string, status int, data any,
) {
	ok := status == http.StatusOK
	if ok {
		status = 0 // writes no 200, so a status already sent stands
	}
	err := pages.Render(response, request, status, page,
		func(takeFlash bool) (any, error) {
			values, isMap := data.(map[string]any)
			if !isMap && data != nil {
				return data, nil
			}
			values = withRequest(request, values)
			values["Live"] = hub.View(request)
			if takeFlash && ok { // a flash follows a 303
				notice, _ := flash.Take(response, request)
				values["Notice"] = notice.Message
			}
			return values, nil
		})
	if err != nil {
		http.Error(response, "Something went wrong.",
			http.StatusInternalServerError)
	}
}

// withRequest copies data, so a map the caller reuses never carries one
// request's language into the next.
func withRequest(request *http.Request, data map[string]any) map[string]any {
	values := map[string]any{}
	maps.Copy(values, data)
	localized := ui.ForRequest(request)
	values["L"] = pages.LocalizerFor(request)
	values["Locales"], values["Return"] = localized.Locales, localized.Return
	values["Scheme"], values["Base"] = localized.Scheme, localized.Base
	values["SignedIn"], _ = request.Context().Value(signedInKey{}).(bool)
	return values
}

// ErrorPage draws templates/errors.html for a refusal and, through
// Options.ErrorPage, for a panic.
func ErrorPage(
	response http.ResponseWriter, request *http.Request, status int,
	ref string,
) {
	if pagesErr != nil {
		http.Error(response, "Something went wrong.",
			http.StatusInternalServerError)
		return
	}
	renderPage(response, request, nil, "errors", status,
		map[string]any{"Status": status, "Ref": ref})
}
