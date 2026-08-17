package screens

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	rastrillo "github.com/carlosframework/rastrillo"
)

// The handlers below are what generated actions call — one call per
// screen. Every one resolves its engine per request (cheap: a struct
// over ctx.DB or Deps.Events) and 404s an unknown id rather than
// explaining it.

// List is GET <route>: the list screen — search, filters and paging as
// GET round trips (§9's zero-JS baseline).
func List(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request, res rastrillo.Resource) {
	deps := depsOf(ctx)
	eng, err := engineFor(ctx, deps, res)
	if err != nil {
		fail(ctx, w, "list "+res.Name, err)
		return
	}
	q := r.URL.Query().Get("q")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	filters := map[string]string{}
	for _, name := range res.List.Filter {
		filters[name] = r.URL.Query().Get(rastrillo.SnakeCase(name))
	}
	rows, total, err := eng.list(q, filters, page)
	if err != nil {
		fail(ctx, w, "list "+res.Name, err)
		return
	}
	render(ctx, deps, w, "screen_list", http.StatusOK,
		buildListView(r, res, deps, rows, total, page, q, filters))
}

// NewForm is GET <route>/new: the blank form (Basics only — Advanced
// settings are edited after the row exists, matching the two-action
// invariant).
func NewForm(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request, res rastrillo.Resource) {
	deps := depsOf(ctx)
	base := resolveRoute(res.Route, r)
	render(ctx, deps, w, "screen_form", http.StatusOK, formView{
		CSS:     cssPath(deps),
		Title:   "New " + singularTitle(res),
		BackURL: base,
		Sections: []sectionView{{
			Action: base,
			Fields: buildFields(r, res, res.Form.Basics, nil),
			Submit: "Create",
		}},
	})
}

// Create is POST <route>.
func Create(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request, res rastrillo.Resource) {
	deps := depsOf(ctx)
	if !sameOriginWrite(w, r) {
		return
	}
	eng, err := engineFor(ctx, deps, res)
	if err != nil {
		fail(ctx, w, "create "+res.Name, err)
		return
	}
	vals, err := parseForm(r, res.Form.Basics, deps.Blobs, nil)
	var fe fieldError
	if errors.As(err, &fe) {
		base := resolveRoute(res.Route, r)
		render(ctx, deps, w, "screen_form", http.StatusUnprocessableEntity, formView{
			CSS: cssPath(deps), Title: "New " + singularTitle(res), BackURL: base,
			Error: fe.Error(),
			Sections: []sectionView{{
				Action: base, Fields: buildFields(r, res, res.Form.Basics, nil), Submit: "Create",
			}},
		})
		return
	}
	if err != nil {
		fail(ctx, w, "create "+res.Name, err)
		return
	}
	if _, err := eng.create(vals, ctx.Actor.String()); err != nil {
		fail(ctx, w, "create "+res.Name, err)
		return
	}
	http.Redirect(w, r, resolveRoute(res.Route, r), http.StatusSeeOther)
}

// Show is GET <route>/{id}: the read-only view.
func Show(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request, res rastrillo.Resource) {
	deps := depsOf(ctx)
	eng, err := engineFor(ctx, deps, res)
	if err != nil {
		fail(ctx, w, "show "+res.Name, err)
		return
	}
	row, err := eng.get(r.PathValue("id"))
	if errors.Is(err, errRowNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		fail(ctx, w, "show "+res.Name, err)
		return
	}
	base := resolveRoute(res.Route, r)
	id := url.PathEscape(r.PathValue("id"))
	v := showView{
		CSS:       cssPath(deps),
		Title:     singularTitle(res),
		EditURL:   base + "/" + id + "/edit",
		DeleteURL: base + "/" + id + "/delete",
		BackURL:   base,
	}
	for _, f := range res.Fields() {
		v.Pairs = append(v.Pairs, pairView{Label: label(r, res, f.Name), Value: display(f, row[f.Name])})
	}
	render(ctx, deps, w, "screen_show", http.StatusOK, v)
}

// EditForm is GET <route>/{id}/edit: the Basics form, and the Advanced
// form as its own section with its own action when the manifest
// declares one.
func EditForm(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request, res rastrillo.Resource) {
	deps := depsOf(ctx)
	eng, err := engineFor(ctx, deps, res)
	if err != nil {
		fail(ctx, w, "edit "+res.Name, err)
		return
	}
	row, err := eng.get(r.PathValue("id"))
	if errors.Is(err, errRowNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		fail(ctx, w, "edit "+res.Name, err)
		return
	}
	render(ctx, deps, w, "screen_form", http.StatusOK,
		editFormView(r, res, deps, row, ""))
}

func editFormView(r *http.Request, res rastrillo.Resource, deps Deps, row map[string]any, errMsg string) formView {
	base := resolveRoute(res.Route, r)
	id := url.PathEscape(r.PathValue("id"))
	v := formView{
		CSS:     cssPath(deps),
		Title:   "Edit " + singularTitle(res),
		BackURL: base,
		Error:   errMsg,
	}
	if len(res.Form.Advanced) == 0 {
		v.Sections = []sectionView{{
			Action: base + "/" + id,
			Fields: buildFields(r, res, res.Form.Basics, row),
			Submit: "Save",
		}}
		return v
	}
	// Two independent forms, two independent actions — a basics save
	// can never clobber an advanced setting, by construction (§3).
	v.Sections = []sectionView{
		{
			Title:  "Basics",
			Action: base + "/" + id + "/edit-basics",
			Fields: buildFields(r, res, res.Form.Basics, row),
			Submit: "Save basics",
		},
		{
			Title:  "Advanced",
			Action: base + "/" + id + "/edit-advanced",
			Fields: buildFields(r, res, res.Form.Advanced, row),
			Submit: "Save advanced",
		},
	}
	return v
}

// Save is POST <route>/{id} — the whole-form save for a manifest with
// no Advanced section.
func Save(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request, res rastrillo.Resource) {
	saveFields(ctx, w, r, res, res.Form.Basics)
}

// SaveBasics is POST <route>/{id}/edit-basics.
func SaveBasics(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request, res rastrillo.Resource) {
	saveFields(ctx, w, r, res, res.Form.Basics)
}

// SaveAdvanced is POST <route>/{id}/edit-advanced.
func SaveAdvanced(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request, res rastrillo.Resource) {
	saveFields(ctx, w, r, res, res.Form.Advanced)
}

func saveFields(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request, res rastrillo.Resource, fields []rastrillo.Field) {
	deps := depsOf(ctx)
	if !sameOriginWrite(w, r) {
		return
	}
	eng, err := engineFor(ctx, deps, res)
	if err != nil {
		fail(ctx, w, "save "+res.Name, err)
		return
	}
	id := r.PathValue("id")
	row, err := eng.get(id)
	if errors.Is(err, errRowNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		fail(ctx, w, "save "+res.Name, err)
		return
	}
	vals, err := parseForm(r, fields, deps.Blobs, row)
	var fe fieldError
	if errors.As(err, &fe) {
		render(ctx, deps, w, "screen_form", http.StatusUnprocessableEntity,
			editFormView(r, res, deps, row, fe.Error()))
		return
	}
	if err != nil {
		fail(ctx, w, "save "+res.Name, err)
		return
	}
	if err := eng.update(id, vals, ctx.Actor.String()); err != nil {
		fail(ctx, w, "save "+res.Name, err)
		return
	}
	http.Redirect(w, r, resolveRoute(res.Route, r)+"/"+url.PathEscape(id)+"/edit", http.StatusSeeOther)
}

// ConfirmDelete is GET <route>/{id}/delete: the confirm page — every
// destructive action gets its own URL first (§9), which is also the
// page §8's agent consent gate points a human at.
func ConfirmDelete(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request, res rastrillo.Resource) {
	deps := depsOf(ctx)
	eng, err := engineFor(ctx, deps, res)
	if err != nil {
		fail(ctx, w, "confirm delete "+res.Name, err)
		return
	}
	id := r.PathValue("id")
	if _, err := eng.get(id); errors.Is(err, errRowNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		fail(ctx, w, "confirm delete "+res.Name, err)
		return
	}
	base := resolveRoute(res.Route, r)
	render(ctx, deps, w, "screen_confirm", http.StatusOK, confirmView{
		CSS:       cssPath(deps),
		Title:     "Delete " + singularTitle(res),
		Sentence:  confirmSentence(r, res),
		Action:    base + "/" + url.PathEscape(id) + "/delete",
		CancelURL: base,
	})
}

// Delete is POST <route>/{id}/delete: a real DELETE for Exclusive, a
// tombstone event for Mergeable (§5 — an event-sourced history never
// loses the fact that something existed).
func Delete(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request, res rastrillo.Resource) {
	deps := depsOf(ctx)
	if !sameOriginWrite(w, r) {
		return
	}
	eng, err := engineFor(ctx, deps, res)
	if err != nil {
		fail(ctx, w, "delete "+res.Name, err)
		return
	}
	if err := eng.delete(r.PathValue("id"), ctx.Actor.String()); err != nil && !errors.Is(err, errRowNotFound) {
		fail(ctx, w, "delete "+res.Name, err)
		return
	}
	http.Redirect(w, r, resolveRoute(res.Route, r), http.StatusSeeOther)
}
