// genrender.go is view.go's generated-adapter half: the pieces Render
// needs specifically because a generated action (internal/generate's
// action emitter) and its view model (formView, showView, ...) know
// nothing about this particular app — the app's layout, its locale
// catalog, or a column like `published` that no manifest field
// declares (posts.toml has none; see store.go's Migrations comment).
//
// Task 10 gave this file a duplicated "gen-layout" template and a
// separate genPages tree, because the ejection root
// (templates/<resource>/) didn't exist yet: hand and generated pages
// had to render through two different layouts, since a generated view
// model carries no Head field at all. Task 11 ejected posts/list and
// posts/form (templates/posts/{list,form}.html), which let view.go
// collapse both trees and both layouts into one (see its buildPages/
// Render doc comments) — what remains here is genT (locale resolution
// generated/ejected templates' (T "...") calls still need),
// headFor/genHead (the page-name fallback for the one screen family
// that still has no Head field of its own — posts/show, still fully
// generated), and formStripData (the Edit screen's status-pill/
// publish-unpublish-delete strip, which needs data no generated
// action supplies).
package blog

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"blog/gen/locales"

	"amadan.net/rastrillo/rastrillo"
)

// genT resolves a manifest catalog key against gen/locales.BaseCatalog
// directly — a plain map lookup, not rastrillo.T(r, key)/
// Locales.Middleware. The blog is monolingual today, so wiring
// Options.Locales would install locale-prefix routing (/en/...) that
// nothing in this app asked for; genT reads the same map main.go wires
// as Options.BaseCatalog, so the two can't drift apart even though
// only this function is presently exercised at request time. A
// missing key renders as the key itself, matching (*rastrillo.Locales)
// .T's own fallback, so a typo shows up on the page instead of
// blanking a sentence.
func genT(key string) string {
	if v, ok := locales.BaseCatalog[key]; ok {
		return v
	}
	return key
}

// headFor computes the Head value view.go's "layout" needs for page.
// Every hand view model (HomeView, PostView, AdminListView, ...) and
// this file's own postFormView already declare a Head field of their
// own; headField reflects it out rather than asking every one of them
// to be reached a second, different way. The one remaining case with
// no Head field to reflect is a still-generated view model — today
// that is only posts/show's showView (internal/generate's action
// emitter doesn't know about the app's layout, by design), which falls
// back to a page-name default.
func headFor(page string, data any) Head {
	if h, ok := headField(data); ok {
		return h
	}
	return genHead(page)
}

// headField reads a data value's own "Head" field by reflection, the
// same technique isNewForm/formStripData use for the same reason: a
// generated view model is a distinct, unexported type per action file
// (formView, showView, ...), reachable from this package only by its
// field names, never by a shared interface it was never asked to
// implement.
func headField(data any) (Head, bool) {
	v := reflect.ValueOf(data)
	if v.Kind() != reflect.Struct {
		return Head{}, false
	}
	f := v.FieldByName("Head")
	if !f.IsValid() {
		return Head{}, false
	}
	h, ok := f.Interface().(Head)
	return h, ok
}

// genHead is the page-name default for a generated screen with no
// Head field of its own — today, only posts/show (posts/list and
// posts/form both carry their own Head now: AdminListView already
// did, and formStripData gives postFormView one). A later resource's
// still-generated show screen falls back to the same generic title
// rather than this function growing a case per resource name; nothing
// here depends on which resource page is which.
func genHead(page string) Head {
	if strings.HasSuffix(page, "/show") {
		return Head{Title: "Post"}
	}
	return Head{Title: "Posts"}
}

// postFormView is the flat value templates/posts/form.html (ejected —
// task 11) actually executes "content" against. It restates the
// generated formView contract (internal/generate/actions.go pins
// IsNew/Fields/Errors/BasicsAction/AdvancedAction) field for field, so
// the field-text/field-textarea calls the ejected file kept verbatim
// from the generated one still resolve, and adds the Edit screen's
// status strip fields — populated only when !IsNew, since a new post
// cannot be published, unpublished, deleted or even viewed before it
// is created.
type postFormView struct {
	Head Head

	IsNew          bool
	Fields         map[string]string
	Errors         map[string]string
	BasicsAction   string
	AdvancedAction string

	Published     bool
	StatusTone    string
	StatusLabel   string
	PublishHref   string
	UnpublishHref string
	DeleteHref    string
	ViewHref      string
}

// formStripData builds the ejected form template's data value from
// whatever a generated new.GET/edit.GET/index.POST/edit-basics.POST
// action actually passed Render — always a formView-shaped value by
// the generator's own contract, but a distinct unexported type per
// action file, so every field is read back by reflection rather than
// a type assertion (the same reasoning isNewForm used before this
// task, now folded in here since headFor no longer needs it — see
// this file's package doc).
//
// On the Edit screen (!IsNew), it looks the post back up (by the id
// BasicsAction already carries — an edit.GET/edit-basics.POST always
// sets it to fmt.Sprintf("/admin/posts/%d/edit-basics", id)) to learn
// Published, since posts.toml declares no such field and the
// generated action has no way to know about it at all. EditForm
// (view.go) already had exactly the Tone/Label resolution this needs
// — dead code since task 10 deleted the hand admin_edit.html that was
// its only caller — so this revives it rather than recomputing the
// same pair a second way.
func formStripData(ctx *rastrillo.Ctx, data any) any {
	v := reflect.ValueOf(data)
	out := postFormView{
		IsNew:          boolField(v, "IsNew"),
		Fields:         stringMapField(v, "Fields"),
		Errors:         stringMapField(v, "Errors"),
		BasicsAction:   stringField(v, "BasicsAction"),
		AdvancedAction: stringField(v, "AdvancedAction"),
	}
	if out.IsNew {
		out.Head = Head{Title: "New post"}
		return out
	}
	out.Head = Head{Title: out.Fields["Title"]}

	id, ok := idFromBasicsAction(out.BasicsAction)
	if !ok || ctx == nil {
		// Defensive only: every real caller's BasicsAction carries the
		// id (edit.GET.go, edit-basics.POST.go's 400 branch, both
		// generated). Render's own unknown-page/nil-ctx unit tests
		// never reach "posts/form" with IsNew false, so this path is
		// otherwise untested — falling back to the bare form (no
		// strip) beats a panic if it's ever hit.
		return out
	}
	p, err := Get(ctx.DB, id)
	if err != nil {
		return out
	}

	ef := EditForm(p)
	out.Published = p.Published
	out.StatusTone = ef.StatusTone
	out.StatusLabel = ef.StatusLabel
	out.PublishHref = fmt.Sprintf("/admin/posts/%d/publish", p.ID)
	out.UnpublishHref = fmt.Sprintf("/admin/posts/%d/unpublish", p.ID)
	out.DeleteHref = fmt.Sprintf("/admin/posts/%d/delete", p.ID)
	out.ViewHref = fmt.Sprintf("/posts/%d", p.ID)
	return out
}

// idFromBasicsAction parses the post id out of a BasicsAction string
// shaped exactly "/admin/posts/<id>/edit-basics" — the one format
// every generated caller produces (gen/actions/admin/posts/id/
// edit_get, .../edit_basics_post). Anything else reports false rather
// than guessing.
func idFromBasicsAction(action string) (int64, bool) {
	parts := strings.Split(action, "/")
	if len(parts) != 5 || parts[1] != "admin" || parts[2] != "posts" || parts[4] != "edit-basics" {
		return 0, false
	}
	id, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil || id < 1 {
		return 0, false
	}
	return id, true
}

// boolField, stringField and stringMapField read one named field off
// an arbitrary struct value by reflection, defaulting to the zero
// value rather than panicking when the field is absent or a different
// shape — exactly what lets the same postFormView-building code run
// unchanged against new.GET's formView (no BasicsAction at all) and
// edit.GET's (every field present).
func boolField(v reflect.Value, name string) bool {
	if v.Kind() != reflect.Struct {
		return false
	}
	f := v.FieldByName(name)
	return f.IsValid() && f.Kind() == reflect.Bool && f.Bool()
}

func stringField(v reflect.Value, name string) string {
	if v.Kind() != reflect.Struct {
		return ""
	}
	f := v.FieldByName(name)
	if !f.IsValid() || f.Kind() != reflect.String {
		return ""
	}
	return f.String()
}

func stringMapField(v reflect.Value, name string) map[string]string {
	if v.Kind() != reflect.Struct {
		return nil
	}
	f := v.FieldByName(name)
	if !f.IsValid() || f.Kind() != reflect.Map {
		return nil
	}
	m, ok := f.Interface().(map[string]string)
	if !ok {
		return nil
	}
	return m
}
