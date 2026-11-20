package main

import "fmt"

// shellPages are the pages a scaffold starts with, beside the error and
// token pages every app has: templates/index.html, render.go's list of
// page names, and for a shell with a second page, its file and
// template, and the path, route, action and test that serve it.
type shellPages struct {
	index, names              string
	file, second              string
	path, route, action, test string
}

// twoPageShells are the shells whose phone layout is an index page and
// an up link, and the pages each starts with. A scaffold of one with a
// single page would open on a phone to a title and an empty list, so
// each starts with an index and a page it opens. The sidebar and
// console make the home page the index, with one section, Overview; the
// topbar's home page is a content page whose way up is the Menu page,
// its index at /menu.
var twoPageShells = map[string]func(brand string) shellPages{
	"sidebar": sectionPages, "console": sectionPages, "topbar": menuPages,
}

// pagesFor is the pages a scaffold on shell starts with. brand is the
// app's name, HTML-escaped: on a phone the index's heading is the
// brand, and the layout's default, Home, would title the app "Home".
func pagesFor(shell, brand string) shellPages {
	if pages, ok := twoPageShells[shell]; ok {
		return pages(brand)
	}
	return shellPages{index: indexTemplate,
		names: `"index", "errors", "tokens"`}
}

func sectionPages(brand string) shellPages {
	return shellPages{
		index: fmt.Sprintf(sectionsIndexTemplate, scaffoldOverview,
			brand),
		names:  `"index", "overview", "errors", "tokens"`,
		file:   "overview.html",
		second: fmt.Sprintf(overviewTemplate, scaffoldOverview, brand),
		path:   overviewPath, route: overviewRoute,
		action: overviewAction, test: overviewTest,
	}
}

func menuPages(brand string) shellPages {
	return shellPages{
		index: fmt.Sprintf(homeTemplate, scaffoldHome, brand),
		names: `"index", "menu", "errors", "tokens"`,
		file:  "menu.html",
		second: fmt.Sprintf(menuTemplate, scaffoldHome, brand,
			scaffoldMenu),
		path: menuPath, route: menuRoute,
		action: menuAction, test: menuTest,
	}
}

// scaffoldHome is the topbar's one section, the home page, and
// scaffoldMenu the Menu page's heading. They are user-facing copy, so a
// change to either goes through the copy review.
const (
	scaffoldHome = "Home"
	scaffoldMenu = "Menu"
)

// homeTemplate is templates/index.html for the topbar: a content page,
// whose way up is its row on the Menu page. %[1]s is the row's name and
// %[2]s the app's, which is the brand.
const homeTemplate = `{{/* The home page. On a phone its way up goes to
     the Menu page, to this page's row there. */}}
{{define "up"}}{{path $ "/menu#nav-home"}}{{end}}
{{define "brand"}}<a rst-shell-brand href="{{path $ "/"}}">%[2]s</a>{{end}}
{{define "nav"}}
<a id="nav-home" href="{{path $ "/"}}" aria-current="page">%[1]s</a>
{{end}}
{{define "content"}}
<h1>Hello, World — this is a rastrillo app.</h1>
{{end}}
`

// menuTemplate is templates/menu.html, the topbar's index, with the
// same two arguments and %[3]s its heading. On a desktop the shell
// draws nothing special here, so the page lists the links itself.
const menuTemplate = `{{/* The Menu page, one level up from every page:
     on a phone the bar's nav is its rows, and on a desktop the list
     below. A new page gets a nav link with an id, a row here, and an
     up block naming {{path $ "/menu#<that id>"}}. */}}
{{define "view"}}index{{end}}
{{define "brand"}}<a rst-shell-brand href="{{path $ "/"}}">%[2]s</a>{{end}}
{{define "nav"}}
<a id="nav-home" href="{{path $ "/"}}">%[1]s</a>
{{end}}
{{define "content"}}
<header rst-page-header>
<div rst-page-header-titles><h1>%[3]s</h1></div>
</header>
<div rst-list>
<div rst-row><span rst-row-main><a class="rst-nm"
     href="{{path $ "/"}}">%[1]s</a></span></div>
</div>
{{end}}
`

// The topbar's Menu page: its path, route and action.
const (
	menuPath   = "\nconst menuPage action.Path = \"/menu\""
	menuRoute  = "\n\t\tguarded.Get(menuPage, app.menu)"
	menuAction = `
func (app *App) menu(visit *action.Visit) {
	visit.Render("menu", http.StatusOK, nil)
}
`
)

// menuTest is appended to index_test.go for the topbar: the Menu page
// is the index and holds the home row, and the home page's way up
// lands on that row.
const menuTest = `
func TestMenuRenders(t *testing.T) {
	t.Parallel()
	handler := newApp(t)
	recorder := get(t, handler, "/menu")
	body := recorder.Body.String()
	_, index, ok := strings.Cut(body, ` + "`" +
	`<div rst-shell-topbar="index">` + "`" + `)
	if recorder.Code != http.StatusOK || !ok ||
		!strings.Contains(index, ` + "`" + `id="nav-home"` + "`" + `) {
		t.Fatalf("GET /menu: status %d, want 200 with the home row "+
			"in the index:\n%s", recorder.Code, body)
	}
	page := get(t, handler, "/").Body.String()
	if !strings.Contains(page, ` + "`" +
	`href="/menu#nav-home" rel="up"` + "`" + `) {
		t.Errorf("GET /: no way up to the Menu's home row:\n%s", page)
	}
}
`
