package notes

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"notes/gen"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/csrf"
	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/jobs"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/password"
	"amadan.net/rastrillo/rastrillo/sessions"
	"amadan.net/rastrillo/rastrillo/ui"
)

// App wires the example: schema, the shared session core, the
// password identity plugin, and the chi router. It returns a
// *http.ServeMux because rastrillo.Options.Mux is typed that way —
// the chi router mounts inside it rather than being served directly.
//
// models.go and handlers.go are the only files this example asks a
// reader to actually study; everything here is plumbing to get there.
func App(d *db.DB, origin string, logger *slog.Logger) (*http.ServeMux, error) {
	// BootSchema (migrations.go) is this app's own Schema plus every
	// framework subsystem's — sessions' shared core and the generated
	// bookmarks store — merged in apply order. One call replaces what
	// used to be AutoMigrate for the GORM models here plus a separate
	// d.G.Exec loop for sessions' and the generated store's raw SQL.
	if _, err := migrate.Apply(context.Background(), d, BootSchema); err != nil {
		return nil, err
	}

	// sessions wants the writer *sql.DB directly: its statements are a
	// mix of reads and writes, and dbresolver's Get() on the top-level
	// handle returns the source (writer) pool.
	writer, err := d.G.DB()
	if err != nil {
		return nil, err
	}
	sess, err := sessions.New(sessions.Config{DB: writer, Origin: origin, Logger: logger})
	if err != nil {
		return nil, err
	}

	a := &app{db: d.G, jobs: jobs.New(logger)}

	jh, err := jobs.NewHandlers(jobs.Config{
		Jobs:           a.jobs,
		Render:         a.renderJobPage,
		RenderFragment: a.renderJobFragment,
	})
	if err != nil {
		return nil, err
	}

	ph, err := password.New(password.Config{
		Sessions:     sess,
		Lookup:       lookupUser(d.G),
		Create:       createUser(d.G),
		RenderSignin: renderSignin,
		RenderSignup: renderSignup,
	})
	if err != nil {
		return nil, err
	}

	r := chi.NewRouter()
	r.Use(csrf.Protect(origin))
	// App-wide, not just the Require group: the signin/signup pages
	// render the shared layout too, and its nav reads sessions.Current
	// — without the resolve here a signed-in visitor loading /signin
	// would see signed-out chrome. Require trusts this resolution, so
	// the guarded group still costs one lookup per request.
	r.Use(sess.Middleware)
	r.Get("/signin", ph.SigninPage)
	r.Post("/signin", ph.Signin)
	r.Get("/signup", ph.SignupPage)
	r.Post("/signup", ph.Signup)
	r.Post("/signout", ph.Signout)
	// The declarative half: manifest/bookmarks.toml's generated router,
	// mounted inside the same Require group as the hand-written notes
	// CRUD — one app, both paths, chosen per resource. The manifest
	// declares scope = "user", so every generated query filters by the
	// session subject (sessions.Current, which Require stashes); the
	// ctx factory runs per request, which is what lets Render be a
	// closure over r (GenRender's doc). chi's Handle (not Mount, which
	// strips the prefix) passes the full path through to the generated
	// Go 1.22 mux patterns.
	gmux := gen.Router(func(r *http.Request) *rastrillo.Ctx {
		return &rastrillo.Ctx{DB: writer, Logger: logger, Actor: rastrillo.Actor{Human: true}, Render: GenRender(r)}
	})

	r.Group(func(r chi.Router) {
		r.Use(sess.Require)
		r.Get("/", a.listNotes)
		r.Get("/notes/new", a.newNote)
		r.Post("/notes", a.createNote)
		r.Get("/notes/{id}", a.showNote)
		r.Get("/notes/{id}/edit", a.editNote)
		r.Post("/notes/{id}", a.updateNote)
		r.Post("/notes/{id}/delete", a.deleteNote)
		r.Handle("/bookmarks", gmux)
		r.Handle("/bookmarks/*", gmux)
		// The jobs demo: startExport mints the Export row's id and
		// starts the background job; the status page and its fragment
		// are jobs.Handlers' own — this app only supplies Render and
		// RenderFragment (render.go) above.
		r.Post("/export", a.startExport)
		r.Get("/exports/{id}", a.showExport)
		r.Get("/jobs/{id}", jh.StatusPage)
		r.Get("/jobs/{id}/fragment", jh.Fragment)
		r.Get("/jobs/{id}/events", jh.Events)
	})

	// The fragment shim, outside Require: it is a static asset, not a
	// protected route, the same way a scaffolded app's static/
	// rastrillo.js is served by the platform rather than a handler. A
	// generated app owns its own copy on disk from rastrillo new;
	// this hand-wired example just serves ui's embedded bytes directly
	// so there is one fewer file for the demo to keep in sync.
	r.Get("/static/rastrillo.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Write(ui.ShimJS())
	})

	// The stylesheet, served the same way and for the same reason. It
	// is one response rather than two files because tokens.css styles
	// nothing without a theme to supply the colours: the tokens are
	// written in terms of custom properties a theme defines, so an
	// example that served only the first would render as bare HTML and
	// look like the design system had failed. day is the default and
	// the reference theme.
	r.Get("/static/app.css", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		theme, ok := ui.ThemeCSS("day")
		if !ok {
			http.Error(w, "theme missing", http.StatusInternalServerError)
			return
		}
		w.Write(theme)
		w.Write([]byte("\n"))
		w.Write(ui.TokensCSS())
	})

	mux := http.NewServeMux()
	mux.Handle("/", r)
	return mux, nil
}

// lookupUser is password.Config.Lookup: scope-free by design — a
// visitor signing in isn't owned by anyone yet, so there is nothing
// to scope.Owned against.
func lookupUser(g *gorm.DB) func(context.Context, string) (int64, string, error) {
	return func(ctx context.Context, email string) (int64, string, error) {
		var u User
		err := g.WithContext(ctx).Where("email = ?", email).First(&u).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, "", sql.ErrNoRows
		}
		if err != nil {
			return 0, "", err
		}
		return u.ID, u.PasswordHash, nil
	}
}

// createUser is password.Config.Create. Any error it returns (a
// UNIQUE violation on Email, in practice) is treated by password.go
// as a duplicate-email failure and reported to the visitor that way.
func createUser(g *gorm.DB) func(context.Context, string, string) (int64, error) {
	return func(ctx context.Context, email, hash string) (int64, error) {
		u := User{Email: email, PasswordHash: hash}
		if err := g.WithContext(ctx).Create(&u).Error; err != nil {
			return 0, err
		}
		return u.ID, nil
	}
}
