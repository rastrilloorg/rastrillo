package notes

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/carlosframework/rastrillo/flash"
	"github.com/carlosframework/rastrillo/form"
	"github.com/carlosframework/rastrillo/scope"
	"github.com/carlosframework/rastrillo/sessions"
)

// app holds the one dependency every handler below needs. sess.Require
// (app.go) guarantees a session on every route mounted through it, so
// UserID's ok is never checked past that group boundary.
type app struct {
	db *gorm.DB
}

// owned scopes a.db to the signed-in caller — the single seam every
// note read or write goes through, per the middle layer's rule: a row
// that isn't yours is a row that doesn't exist.
func (a *app) owned(r *http.Request) *gorm.DB {
	uid, _ := sessions.UserID(r)
	return scope.Owned(a.db, uid)
}

// find resolves the {id} path param through the owner scope. Neither
// a malformed id nor someone else's row is distinguishable from the
// outside, so both 404 — never 403 (Bob force-probing Alice's note
// gets the same answer as a random id).
func (a *app) find(r *http.Request) (Note, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		return Note{}, gorm.ErrRecordNotFound
	}
	var n Note
	err = a.owned(r).First(&n, id).Error
	return n, err
}

func (a *app) listNotes(w http.ResponseWriter, r *http.Request) {
	var notes []Note
	a.owned(r).Order("created_at desc").Find(&notes)
	renderContent(w, r, "index", indexView{Notes: notes})
}

func (a *app) newNote(w http.ResponseWriter, r *http.Request) {
	renderContent(w, r, "new", formView{})
}

func (a *app) createNote(w http.ResponseWriter, r *http.Request) {
	uid, _ := sessions.UserID(r)
	title, body := r.PostFormValue("title"), r.PostFormValue("body")
	if title == "" {
		w.WriteHeader(http.StatusUnprocessableEntity)
		renderContent(w, r, "new", formView{
			Note:   Note{Title: title, Body: body},
			Errors: form.Errors{"Title": "Title is required"},
		})
		return
	}
	n := Note{UserID: uid, Title: title, Body: body}
	if err := a.db.Create(&n).Error; err != nil {
		http.Error(w, "could not save note", http.StatusInternalServerError)
		return
	}
	flash.Set(w, "notice", "Note created.")
	http.Redirect(w, r, fmt.Sprintf("/notes/%d", n.ID), http.StatusSeeOther)
}

func (a *app) showNote(w http.ResponseWriter, r *http.Request) {
	n, err := a.find(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	renderContent(w, r, "show", noteView{Note: n})
}

func (a *app) editNote(w http.ResponseWriter, r *http.Request) {
	n, err := a.find(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	renderContent(w, r, "edit", formView{Note: n})
}

func (a *app) updateNote(w http.ResponseWriter, r *http.Request) {
	n, err := a.find(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	title, body := r.PostFormValue("title"), r.PostFormValue("body")
	if title == "" {
		n.Title, n.Body = title, body
		w.WriteHeader(http.StatusUnprocessableEntity)
		renderContent(w, r, "edit", formView{Note: n, Errors: form.Errors{"Title": "Title is required"}})
		return
	}
	// Never bind a request onto a GORM model: Title/Body came from
	// PostFormValue above, and only those two columns (plus the
	// timestamp GORM's own hook stamps) are ever written back.
	update := map[string]any{"Title": title, "Body": body}
	if err := a.db.Model(&n).Select("Title", "Body", "UpdatedAt").Updates(update).Error; err != nil {
		http.Error(w, "could not save note", http.StatusInternalServerError)
		return
	}
	flash.Set(w, "notice", "Note updated.")
	http.Redirect(w, r, fmt.Sprintf("/notes/%d", n.ID), http.StatusSeeOther)
}

func (a *app) deleteNote(w http.ResponseWriter, r *http.Request) {
	n, err := a.find(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := a.db.Delete(&n).Error; err != nil {
		http.Error(w, "could not delete note", http.StatusInternalServerError)
		return
	}
	flash.Set(w, "notice", "Note deleted.")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
