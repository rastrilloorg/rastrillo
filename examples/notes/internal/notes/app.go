package notes

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"notes/gen"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/api"
	"amadan.net/rastrillo/rastrillo/apitoken"
	"amadan.net/rastrillo/rastrillo/background"
	"amadan.net/rastrillo/rastrillo/csrf"
	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/jobs"
	"amadan.net/rastrillo/rastrillo/locales"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/op"
	"amadan.net/rastrillo/rastrillo/password"
	"amadan.net/rastrillo/rastrillo/pow"
	"amadan.net/rastrillo/rastrillo/sessions"
	"amadan.net/rastrillo/rastrillo/ui"
)

// App wires the example: schema, the shared session core, the
// password identity plugin, and the chi router. It returns a
// *http.ServeMux because rastrillo.Options.Mux is typed that way —
// the chi router mounts inside it rather than being served directly.
// cfg is locales.CheckConfig's own result, not a value read back from
// rastrillo.Handler: this app opens its own *gorm.DB (db.Open) and
// hands Serve a finished Options.Mux (main.go:84-87), so there is no
// Options.Router callback for Handler to call App from after
// validating. main.go checks the configuration itself, before this
// runs, so the three ui.Pages below never build from one Handler
// would have refused; rastrillo.Serve's own Handler call then
// re-validates the same configuration, redundantly but harmlessly, as
// part of serving it (Task 9, 13, 24).
//
// models.go and handlers.go are the only files this example asks a
// reader to actually study; everything here is plumbing to get there.
func App(d *db.DB, origin, instanceKey string, bg *background.Group, logger *slog.Logger, cfg locales.Config) (*http.ServeMux, error) {
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
	// Boot comes straight after migrate.Apply: the generated bookmarks
	// table must exist for it to upgrade and check, and nothing may
	// serve before it has. It vouches for writer alone, which is why
	// the generated router's contexts below carry writer too.
	booted, err := gen.Boot(context.Background(), writer)
	if err != nil {
		return nil, err
	}
	sess, err := sessions.New(sessions.Config{DB: writer, Origin: origin, Logger: logger})
	if err != nil {
		return nil, err
	}

	// The front door for sign-in and sign-up. The Guard is built after
	// migrate.Apply above, because New checks pow's table exists; the
	// app owns it, so the app sweeps it.
	powAssets := rastrillo.NewAssets(pow.Assets())
	guard, err := pow.New(pow.Config{
		InstanceKey: instanceKey,
		Nonces:      pow.SQLNonces(writer),
		// 16 bits: the largest difficulty whose p95 solve stays under 1s at
		// 6x CPU throttle in Chromium (pow/measure_browser_test.go,
		// 2026-10-04: p95 796ms, p99 879ms); a one-tap visitor waits the
		// whole solve.
		Difficulty: 16,
		MinAge:     500 * time.Millisecond,
		ScriptURL:  "/pow" + powAssets.Path("pow.js"),
		WorkerURL:  "/pow" + powAssets.Path("pow-worker.js"),
	})
	if err != nil {
		return nil, err
	}
	bg.Loop(context.Background(), 10*time.Minute, func() {
		if err := guard.Sweep(time.Now()); err != nil {
			logger.Warn("sweep spent challenges", "err", err)
		}
	})

	// The three ui.Pages this app renders through (render.go's and
	// genrender.go's own doc comments on why there are three), built
	// once here from the validated locale configuration, before any
	// handler can reach them.
	pages, err := newPages(cfg)
	if err != nil {
		return nil, err
	}
	genPages, err := newGenPages(cfg)
	if err != nil {
		return nil, err
	}
	fragPages, err := newFragPages(cfg)
	if err != nil {
		return nil, err
	}

	a := &app{db: d.G, jobs: jobs.New(logger), pages: pages, genPages: genPages, fragPages: fragPages}

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
		Proof:        guard,
		Lookup:       lookupUser(d.G),
		Create:       createUser(d.G),
		RenderSignin: a.renderSignin,
		RenderSignup: a.renderSignup,
	})
	if err != nil {
		return nil, err
	}

	// Personal tokens: the store verifies API requests and the page
	// mints and revokes. The prefix is derived from the app name so a
	// leaked token reads "notes_..." and a secret scanner can match it.
	tokens, err := apitoken.New(apitoken.Config{DB: writer, Prefix: apitoken.DerivePrefix("notes")})
	if err != nil {
		return nil, err
	}
	th, err := apitoken.NewHandlers(apitoken.HandlersConfig{
		Store:  tokens,
		Path:   "/tokens",
		Render: a.renderTokens,
		// Name turns the session subject (the users row id) into the
		// email, so the page reads "Claude for alice@example.com"
		// rather than "Claude for 1".
		Name: nameUser(d.G),
	})
	if err != nil {
		return nil, err
	}

	// One ctx factory for both doors, the generated pages and the API
	// tree, so they share one policy. Authorize is op.DefaultPolicy,
	// named on purpose rather than left nil: a token may do whatever
	// its owner can with their own bookmarks, and nothing on an
	// unscoped resource. This app's pages add no rule beyond ownership
	// (no roles, no suspension), so the default is the whole truth; an
	// app whose pages did would write those rules here, because the
	// API cannot see page middleware. Actor stays zero: the generated
	// router fills it from the session, and the tree from the token.
	// The factory runs per request, which is what lets Render be a
	// closure over r (GenRender's doc).
	ctxFactory := func(r *http.Request) *rastrillo.Ctx {
		return &rastrillo.Ctx{DB: writer, Logger: logger, Actor: rastrillo.Actor{}, Authorize: op.DefaultPolicy, Render: a.GenRender(r)}
	}

	// The API tree: the same generated operations the bookmarks pages
	// call, behind a bearer token instead of a session. Redact keeps a
	// token quoted in an error out of the log.
	apiTree, err := api.New(api.Config{
		Verify:    tokens,
		Ctx:       ctxFactory,
		Authorize: op.DefaultPolicy,
		Logger:    logger,
		Redact:    tokens.Redact,
	}, gen.APIRoutes()...)
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
	// session subject (sessions.Current, which Require stashes). chi's
	// Handle (not Mount, which strips the prefix) passes the full path
	// through to the generated Go 1.22 mux patterns.
	gmux := gen.Router(booted, ctxFactory)

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
		// The token page, behind csrf.Protect like every POST here.
		// Minting needs a sign-in within ten minutes: a token outlives
		// the session that made it, so this is the moment to be sure
		// of who is at the keyboard. The revokes do not, because a
		// kill switch must be easy to throw.
		r.Get("/tokens", th.Page)
		r.With(sess.RequireFresh(10*time.Minute)).Post("/tokens", th.Create)
		r.Post("/tokens/revoke", th.Revoke)
		r.Post("/tokens/revoke-agent", th.RevokeAgent)
		r.Post("/tokens/revoke-all", th.RevokeAll)
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

	// pow's script and worker, outside Require for the same reason: a
	// visitor solving the challenge is not signed in yet.
	r.Handle("/pow/*", http.StripPrefix("/pow/", powAssets.Handler()))

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
	// The busy rule, beside the shim and served the same way.
	r.Get("/static/busy.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Write(ui.BusyJS())
	})

	// The stylesheet, served the same way and for the same reason. It
	// is one response rather than two files because tokens.css styles
	// nothing without a theme to supply the colours: the tokens are
	// written in terms of custom properties a theme defines, so an
	// example that served only the first would render as bare HTML and
	// look like the design system had failed. day is the default and
	// the reference theme.
	//
	// Tokens first, theme second — the order a scaffolded app's <head>
	// is required to use (cmd/rastrillo/new_test.go gates it), so the
	// theme has the last word on any property both declare.
	r.Get("/static/app.css", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		theme, ok := ui.ThemeCSS("day")
		if !ok {
			http.Error(w, "theme missing", http.StatusInternalServerError)
			return
		}
		w.Write(ui.TokensCSS())
		w.Write([]byte("\n"))
		w.Write(theme)
		w.Write([]byte(notesCSS))
	})

	// a.pages.Middleware puts the request's localizer on the context
	// (ui.Pages.Middleware) for every handler on r, hand and generated
	// alike — any of this app's three ui.Pages would do (they share
	// cfg's offered locales and formats; render.go's newPages doc
	// explains why there are three at all), so the hand screens' own
	// one wraps here.
	mux := http.NewServeMux()
	mux.Handle("/", a.pages.Middleware(r))
	// The API tree is outside r on purpose: no sessions, no CSRF, no
	// page middleware. A bearer token is its only credential, so a page
	// cannot be made to call it with the browser's cookies. Both
	// patterns, so the outer mux never answers a bare /api/v1 with a
	// redirect of its own instead of the tree's JSON 404.
	mux.Handle("/api/v1", apiTree)
	mux.Handle("/api/v1/", apiTree)
	return mux, nil
}

// notesCSS is the app's own stylesheet, served last so it wins ties with
// the theme. It holds what would otherwise be a style attribute: the
// framework's default CSP blocks those, so an inline style="display:inline"
// on the delete form would be dropped and the button would fall onto its
// own line.
const notesCSS = `
nav form { display: inline; }
`

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

// nameUser is apitoken.HandlersConfig.Name: the session subject is the
// users row id as text (password's Lookup returns the int64), and the
// email is the name a person recognises. Anything that cannot be
// found answers "", which makes the page fall back to the subject,
// so an agent is never shown "for" nobody.
func nameUser(g *gorm.DB) func(context.Context, string) string {
	return func(ctx context.Context, subject string) string {
		id, err := strconv.ParseInt(subject, 10, 64)
		if err != nil {
			return ""
		}
		var u User
		if err := g.WithContext(ctx).Select("email").First(&u, id).Error; err != nil {
			return ""
		}
		return u.Email
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
