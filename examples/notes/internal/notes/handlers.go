package notes

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"amadan.net/rastrillo/rastrillo/flash"
	"amadan.net/rastrillo/rastrillo/form"
	"amadan.net/rastrillo/rastrillo/jobs"
	"amadan.net/rastrillo/rastrillo/scope"
	"amadan.net/rastrillo/rastrillo/sessions"
)

// app holds the dependencies every handler below needs. sess.Require
// (app.go) guarantees a session on every route mounted through it, so
// UserID's ok is never checked past that group boundary.
type app struct {
	db   *gorm.DB
	jobs *jobs.Jobs
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
	if err := a.owned(r).Order("created_at desc").Find(&notes).Error; err != nil {
		http.Error(w, "could not load notes", http.StatusInternalServerError)
		return
	}
	renderContent(w, r, "index", indexView{Notes: notes})
}

func (a *app) newNote(w http.ResponseWriter, r *http.Request) {
	renderContent(w, r, "new", formView{})
}

func (a *app) createNote(w http.ResponseWriter, r *http.Request) {
	uid, _ := sessions.UserID(r)
	title, body := r.PostFormValue("title"), r.PostFormValue("body")
	if title == "" {
		renderStatus(w, r, http.StatusUnprocessableEntity, "new", formView{
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
		renderStatus(w, r, http.StatusUnprocessableEntity, "edit", formView{Note: n, Errors: form.Errors{"Title": "Title is required"}})
		return
	}
	// Never bind a request onto a GORM model: Title/Body came from
	// PostFormValue above, and only those two columns (plus the
	// timestamp GORM's own hook stamps) are ever written back. The
	// write goes through the owner scope too, not just the read that
	// loaded n above — the SQL itself carries WHERE user_id = ? AND
	// id = ?, so a future refactor of find can never turn this into
	// an IDOR by accident.
	update := map[string]any{"Title": title, "Body": body}
	if err := a.owned(r).Model(&n).Select("Title", "Body", "UpdatedAt").Updates(update).Error; err != nil {
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
	// Scoped delete for the same reason the update above is: the write
	// carries WHERE user_id = ? AND id = ? at the SQL level, not just
	// at the read that found n.
	if err := a.owned(r).Delete(&n).Error; err != nil {
		http.Error(w, "could not delete note", http.StatusInternalServerError)
		return
	}
	flash.Set(w, "notice", "Note deleted.")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// startExport kicks off the background export and 303s to the status
// page — the loading state the button's data-busy was missing before
// the job even reaches the server. The export's ID is minted here, not
// inside the job, so the job's Location (and this handler's redirect)
// are known before the goroutine starts.
//
// The notes read inside the job scopes by uid (Note.UserID, resolved
// from the session up front, same as every other handler's a.owned)
// because that is the notes' own owner column; the job itself, and the
// Export row it writes, are keyed by sess.Subject instead, because
// that is what jobs.Jobs and jobs.Handlers key ownership by. Both
// forms name the same signed-in caller — they just speak the two
// different owner vocabularies this app happens to mix.
func (a *app) startExport(w http.ResponseWriter, r *http.Request) {
	sess, _ := sessions.Current(r)
	uid, _ := sessions.UserID(r)
	owner := sess.Subject
	exportID := newToken()
	g := a.db
	job, err := a.jobs.Start(owner, "Export notes", "/exports/"+exportID,
		func(ctx context.Context, progress func(string)) error {
			var notes []Note
			if err := scope.Owned(g.WithContext(ctx), uid).Order("id").Find(&notes).Error; err != nil {
				return errors.New("could not read your notes")
			}
			var b strings.Builder
			b.WriteString("# Notes export\n")
			for i, n := range notes {
				// Simulated pace so a demo's status page is actually
				// visible mid-run — a real export would just be fast,
				// and this sleep would not exist.
				time.Sleep(300 * time.Millisecond)
				progress(fmt.Sprintf("%d of %d", i+1, len(notes)))
				fmt.Fprintf(&b, "\n## %s\n\n%s\n", n.Title, n.Body)
			}
			exp := Export{ID: exportID, Owner: owner, Content: b.String()}
			if err := g.WithContext(ctx).Create(&exp).Error; err != nil {
				return errors.New("could not write the export")
			}
			return nil
		})
	// ErrOwnerBusy in our own words, not err.Error(): the refusal is
	// per owner (four running jobs), so waiting genuinely clears it.
	if err != nil {
		flash.Set(w, "error", "You already have exports running — wait for one to finish.")
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/jobs/"+job.ID, http.StatusSeeOther)
}

// showExport serves the finished document as markdown, keyed on id AND
// Owner — the same someone-else's-row-is-a-404 rule as the notes
// themselves, and as jobs.Handlers.lookup applies to the job that
// wrote this row.
func (a *app) showExport(w http.ResponseWriter, r *http.Request) {
	sess, _ := sessions.Current(r)
	var exp Export
	err := a.db.WithContext(r.Context()).
		Where("id = ? AND owner = ?", chi.URLParam(r, "id"), sess.Subject).
		First(&exp).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "something went wrong", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Write([]byte(exp.Content))
}

// newToken is an Export's ID: the same crypto/rand + base64url idiom
// jobs.newID uses (16 random bytes), reimplemented here rather than
// exported from jobs, which has no reason to expose an ID-minting
// helper beyond its own Start.
func newToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
