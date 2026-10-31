package corpus

import (
	"embed"
	"html/template"
	"strconv"

	"amadan.net/rastrillo/rastrillo/tmpl"
)

// The TodoMVC bake-off page: testdata/todomvc holds the templates of
// todomvc-bakeoff/apps/go-rastrillo/internal/todo/templates, and the
// types below are that package's view, item and filter, with the methods
// the page calls and newView, which builds the data its handlers render.
// Typed compiled code (a later phase) finds them here.

//go:embed testdata/todomvc/*.html
var todoFS embed.FS

// todoFuncs stands in for the app's FuncMap: asset fingerprints a static
// path there, and plural is the app's own.
var todoFuncs = template.FuncMap{
	"asset": func(path string) string { return "/" + path + "?v=0123abcd" },
	"plural": func(count int) string {
		if count == 1 {
			return "item"
		}
		return "items"
	},
}

// filter is which slice of the list a page shows. Its name is what forms
// post back as `filter`; its path is where the 303 goes.
type filter string

const (
	filterAll       filter = "all"
	filterActive    filter = "active"
	filterCompleted filter = "completed"
)

func (filter filter) path() string {
	if filter == filterAll {
		return "/"
	}
	return "/" + string(filter)
}

func (filter filter) shows(item todo) bool {
	switch filter {
	case filterActive:
		return !item.Completed
	case filterCompleted:
		return item.Completed
	}
	return true
}

// todo is the app's model, reduced to what newView reads.
type todo struct {
	ID        int64
	Title     string
	Completed bool
}

// view is everything the page template draws.
type view struct {
	Filter    filter
	Items     []item
	Total     int
	Left      int
	Completed int
	Editing   bool
}

type item struct {
	ID        int64
	Title     string
	Completed bool
	Editing   bool
	EditHref  string
}

// newView sorts the list into what the page shows. Counts cover the
// whole list whichever filter is on; editing is the item whose editor is
// open, or 0.
func newView(todos []todo, filter filter, editing int64) view {
	view := view{Filter: filter, Total: len(todos)}
	for _, task := range todos {
		if task.Completed {
			view.Completed++
		} else {
			view.Left++
		}
		if !filter.shows(task) {
			continue
		}
		it := item{ID: task.ID, Title: task.Title, Completed: task.Completed,
			Editing:  task.ID == editing,
			EditHref: filter.path() + "?edit=" + strconv.FormatInt(task.ID, 10)}
		view.Editing = view.Editing || it.Editing
		view.Items = append(view.Items, it)
	}
	return view
}

// Class is the li's class attribute: completed, editing, both or none.
func (it item) Class() string {
	switch {
	case it.Completed && it.Editing:
		return "completed editing"
	case it.Completed:
		return "completed"
	case it.Editing:
		return "editing"
	}
	return ""
}

// AllCompleted is toggle-all's checked state.
func (view view) AllCompleted() bool { return view.Total > 0 && view.Left == 0 }

// todos is count items, every third completed, with HTML specials in some
// titles.
func todos(count int) []todo {
	out := make([]todo, count)
	for index := range out {
		title := "Todo " + strconv.Itoa(index+1)
		if index%7 == 3 {
			title = specials
		}
		out[index] = todo{
			ID:        int64(index + 1),
			Title:     title,
			Completed: index%3 == 2,
		}
	}
	return out
}

// todoViews are the views the handlers render: every filter on no
// todos, one and fifty, and some edited, odd or empty.
func todoViews() []any {
	var views []any
	filters := []filter{filterAll, filterActive, filterCompleted}
	for _, count := range []int{0, 1, 50} {
		for _, filter := range filters {
			views = append(views, newView(todos(count), filter, 0))
		}
	}
	return append(views,
		newView(todos(3), filterAll, 2),
		newView(todos(50), filterActive, 4),
		newView([]todo{{ID: 1, Title: "done", Completed: true}}, filterAll, 1),
		view{},
		&view{Filter: "<odd>", Total: 1},
	)
}

// todoMVC is the page as the app renders it: index for a whole page, app
// for a fragment, errors for a failure, each the way the handlers and
// ErrorPage call them.
func todoMVC() []Case {
	views := todoViews()
	page := func(entry string, data ...any) Case {
		return Case{
			Name:  "todomvc-" + entry,
			Entry: entry,
			Data:  data,
			Build: func() (*tmpl.Template, error) {
				return tmpl.New("").
					Funcs(todoFuncs).
					ParseFS(todoFS, "testdata/todomvc/*.html")
			},
			std: func() (*template.Template, error) {
				return template.New("").
					Funcs(todoFuncs).
					ParseFS(todoFS, "testdata/todomvc/*.html")
			},
		}
	}
	return []Case{
		page("index", views...),
		page("app", views...),
		page(
			"errors",
			map[string]any{
				"Title": "Something went wrong on our side",
				"Ref":   "r-123",
			},
			map[string]any{"Title": "Not Found"},
			map[string]any{"Title": specials, "Ref": specials},
			map[string]any{"Title": template.HTML("<b>trusted</b>"), "Ref": ""},
			map[string]any{},
		),
	}
}
