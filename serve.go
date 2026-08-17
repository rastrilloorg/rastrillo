// Package rastrillo is the CARLOS web framework — the shape of a CARLOS
// app, the way carlosframework/platform is the shape of the deployment
// substrate it runs on. See the design doc for the full picture:
// https://github.com/carlosframework/platform/blob/main/docs/superpowers/specs/2026-08-01-carlos-framework-design.md
//
// The root package holds the process shape (Run/Serve/Handler, the
// activation contract, the SQLite opener, migrations), the action
// vocabulary (Ctx, Actor), the manifest vocabulary (Resource, Kind,
// Tool), and localization. The subsystems live beside it: crypto (the
// family envelope), auth (keymail sign-in with the magic-link
// fallback), webauthn, eventlog (the Mergeable store), blobs, mail,
// screens (the manifest runtime), tools (agent dispatch), and ui (the
// component partials). README.md keeps the honest status list.
package rastrillo

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

// BuildVersion is set via -ldflags at build time (see cmd/rastrillo).
// The platform's deploy verification polls GET /api/version on every
// instance socket expecting exactly this — see blueprint.md, "The carlos
// core": "every instance must also serve GET /api/version reporting its
// build sha."
var BuildVersion = "dev"

// Options configures Serve.
type Options struct {
	// Mux is the app's router — normally gen/router.go's output (design
	// doc §4). Exactly one of Mux and Router must be set.
	Mux *http.ServeMux

	// Router, if set, builds the app's mux after the database opens:
	// Serve calls it with the *sql.DB opened from DBPath — pragmas,
	// eager ping, and Migrations already applied — and serves the mux
	// it returns. This is how an app puts the framework-opened handle
	// in its per-request Ctx without hand-copying the DSN (the blog's
	// friction log, F4):
	//
	//	Router: func(db *sql.DB) (*http.ServeMux, error) {
	//		return gen.Router(func(*http.Request) *rastrillo.Ctx {
	//			return &rastrillo.Ctx{DB: db, Logger: logger}
	//		}), nil
	//	},
	//
	// Exactly one of Mux and Router must be set. With DBPath empty,
	// Router is called with a nil db — an app without a database can
	// still defer its mux construction. Serve owns the handle and
	// closes it when Serve returns; do not retain it past that. An app
	// that needs a handle outside Serve's lifetime calls OpenDB itself.
	Router func(db *sql.DB) (*http.ServeMux, error)

	// DBPath, if set, opens a SQLite database with the pragma ordering
	// and connection settings the survey found hand-propagated,
	// error-prone, repo to repo (design doc §5): busy_timeout set
	// *before* journal_mode=WAL, then SetMaxOpenConns(1).
	DBPath string

	// Migrations are applied in order at boot, idempotently: each must
	// be safe to run against a database that already has it applied
	// (CREATE TABLE IF NOT EXISTS, or an ALTER whose "duplicate column"
	// error is ignored) — additive-only, per the family's hard-won rule.
	Migrations []string

	// Socket and Addr mirror the platform's activation contract (see
	// testdata/echoapp in carlosframework/platform): a unix socket path,
	// or a TCP host:port for local dev. If both are empty, Serve checks
	// for a systemd-activated listener (LISTEN_FDS) before falling back
	// to Addr ":8080".
	Socket string
	Addr   string

	// Wrap, if set, wraps the app's mux — middleware, the usual
	// net/http way. It runs inside the framework's own endpoints
	// (/healthz, /api/version and friends stay unwrapped; a probe never
	// depends on app middleware) and inside the locale middleware, so a
	// wrapped handler sees the locale-stripped path. Before this seam,
	// a consumer wanting one security-header middleware had to build an
	// outer catch-all mux just to satisfy Router's return type — see
	// amadan's internal/hub/server.go, the friction this closes.
	Wrap func(http.Handler) http.Handler

	// NextDue, if set, answers the platform's scheduled-wake poll: the
	// activator asks a running instance GET /api/next-due (bearer
	// $CARLOS_ADMIN_TOKEN) and hibernates knowing when to wake it —
	// carlosframework/platform internal/activator/backend_exec.go. The
	// returned time is the next moment the app has work; zero means
	// nothing scheduled. Unset, the route does not exist and the
	// activator treats the app as having no schedule (unit tenants
	// never get the poll at all).
	NextDue func() time.Time

	// Sidecar is the app's sidecar pass — the wake → read since
	// bookmark → decide → act loop's body (design doc §8). When the
	// platform spawns `<binary> sidecar run` (it does exactly that when
	// the host's sidecar env file exists), Run calls Sidecar in a loop:
	// each pass returns when it has caught up, reporting when it next
	// has scheduled work (zero: nothing scheduled — Run re-runs after
	// a default poll interval). A pass error is logged and retried with
	// backoff, never fatal: a sidecar outliving a flaky dependency is
	// the point of having one. SIGTERM/SIGINT cancels the context and
	// ends the loop. Nil with a `sidecar run` invocation is a loud
	// startup error, not a silent serve.
	Sidecar func(ctx context.Context) (time.Time, error)

	// Locales declares the app's locale codes (design doc §10) — the
	// catalogs LocaleFS carries as locales/<code>.toml. Empty means a
	// monolingual app: no locale middleware is installed and requests
	// pay nothing.
	Locales []string

	// DefaultLocale is the locale for unprefixed requests that match
	// nothing else, and the first fallback layer for missing keys.
	// Empty defaults to Locales[0].
	DefaultLocale string

	// LocaleFS provides the locales/<code>.toml catalog files —
	// normally an embed.FS rooted at the app directory. Nil is legal:
	// lookups fall back to the key itself, which keeps a missing
	// catalog visible instead of silently blank (§10).
	LocaleFS fs.FS

	// Logger defaults to slog.Default() if nil.
	Logger *slog.Logger
}

// Serve opens the database (if configured), applies migrations, resolves
// the platform's activation contract for a listener, and serves until the
// process receives SIGTERM/SIGINT. It always answers GET /healthz itself
// — the manifest/action layer never has to remember to.
func Serve(opts Options) error {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	handler, closeDB, err := Handler(opts)
	if err != nil {
		return err
	}
	defer closeDB()

	ln, err := listen(opts.Socket, opts.Addr)
	if err != nil {
		return fmt.Errorf("rastrillo: listen: %w", err)
	}
	logger.Info("rastrillo: serving", "addr", ln.Addr().String(), "version", BuildVersion)

	srv := &http.Server{Handler: handler}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-sigCh:
		logger.Info("rastrillo: shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Handler is everything Serve builds short of the listener and the
// signal handling: it opens the database (if configured), applies
// migrations, resolves the Mux/Router choice, and assembles the full
// serving handler — framework endpoints included. The returned close
// func releases the database handle (a no-op without one).
//
// Exported for test harnesses: before this seam, every app's harness
// hand-duplicated /healthz, /api/version and the DSN pragma ordering
// because Serve blocks on a real listener (vitogo's vitotest says so in
// its own comments; seapointish copied the same shape). Now a harness is
// httptest.NewServer around this.
func Handler(opts Options) (http.Handler, func() error, error) {
	closeNothing := func() error { return nil }
	if (opts.Mux == nil) == (opts.Router == nil) {
		return nil, closeNothing, errors.New("rastrillo: exactly one of Options.Mux and Options.Router must be set")
	}

	var db *sql.DB
	var err error
	if opts.DBPath != "" {
		db, err = OpenDB(opts.DBPath, opts.Migrations)
		if err != nil {
			return nil, closeNothing, fmt.Errorf("rastrillo: open database: %w", err)
		}
	}
	closeDB := closeNothing
	if db != nil {
		closeDB = db.Close
	}

	opts.Mux, err = buildMux(opts, db)
	if err != nil {
		closeDB()
		return nil, closeNothing, err
	}

	handler, err := buildHandler(opts)
	if err != nil {
		// buildHandler's only error source is NewLocales, whose errors
		// already carry the "rastrillo:" prefix — wrapping again here
		// would read "rastrillo: rastrillo: ...".
		closeDB()
		return nil, closeNothing, err
	}
	return handler, closeDB, nil
}

// buildHandler assembles the serving handler: the framework's own
// endpoints, the app mux (wrapped by Options.Wrap when set), and —
// when Options.Locales is set — the locale middleware wrapped around
// the whole thing, so a locale prefix strips before routing and the
// translator rides the request context (§10). Split from Handler so
// the assembly is testable without a database.
func buildHandler(opts Options) (http.Handler, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "ok")
	})
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, BuildVersion)
	})
	if opts.NextDue != nil {
		mux.HandleFunc("GET /api/next-due", nextDueHandler(opts.NextDue))
	}
	var app http.Handler = opts.Mux
	if opts.Wrap != nil {
		app = opts.Wrap(app)
	}
	mux.Handle("/", app)

	if len(opts.Locales) == 0 {
		return mux, nil
	}
	def := opts.DefaultLocale
	if def == "" {
		def = opts.Locales[0]
	}
	loc, err := NewLocales(opts.Locales, def, nil, opts.LocaleFS)
	if err != nil {
		return nil, err
	}
	return loc.Middleware(mux), nil
}

// buildMux resolves the Mux/Router choice. Router runs after the
// database opens so the app can close over the framework-opened handle
// — the entire point of the seam (F4).
func buildMux(opts Options, db *sql.DB) (*http.ServeMux, error) {
	if opts.Router == nil {
		return opts.Mux, nil
	}
	mux, err := opts.Router(db)
	if err != nil {
		return nil, fmt.Errorf("rastrillo: build router: %w", err)
	}
	if mux == nil {
		return nil, errors.New("rastrillo: Options.Router returned a nil mux")
	}
	return mux, nil
}

// nextDueHandler answers the activator's scheduled-wake poll (see
// Options.NextDue). The bearer token is $CARLOS_ADMIN_TOKEN — the
// instance-local secret the platform's exec backend delivers in the
// overlay env file. No token in the environment means nobody can
// authenticate: fail closed, don't fail open.
func nextDueHandler(nextDue func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := os.Getenv("CARLOS_ADMIN_TOKEN")
		auth := r.Header.Get("Authorization")
		if token == "" || subtle.ConstantTimeCompare([]byte(auth), []byte("Bearer "+token)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var due int64
		if t := nextDue(); !t.IsZero() {
			due = t.Unix()
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int64{"due": due})
	}
}

// listen resolves the platform's activation contract, exactly matching
// carlosframework/platform's testdata/echoapp: a systemd-activated
// listener (LISTEN_FDS=1, fd 3 — the carlos-app@.socket contract) takes
// priority; otherwise Socket (unix) or Addr (TCP).
func listen(socket, addr string) (net.Listener, error) {
	if n, _ := strconv.Atoi(os.Getenv("LISTEN_FDS")); n >= 1 {
		if pid := os.Getenv("LISTEN_PID"); pid == "" || pid == strconv.Itoa(os.Getpid()) {
			return net.FileListener(os.NewFile(3, "listen-fd"))
		}
	}
	if socket != "" {
		os.Remove(socket)
		return net.Listen("unix", socket)
	}
	if addr == "" {
		addr = ":8080"
	}
	return net.Listen("tcp", addr)
}

// OpenDB applies the SQLite convention the survey found hand-propagated,
// with fixes, repo to repo (design doc §5): busy_timeout set *before*
// journal_mode=WAL — the reverse order crashes with SQLITE_BUSY under
// concurrent open, titogo's real fix — then SetMaxOpenConns(1), then an
// eager ping so the file exists on disk from boot, then migrate.
//
// Exported so tests and non-Serve contexts get the corrected opener
// instead of reproducing the DSN by hand (the blog's F4).
func OpenDB(path string, migrations []string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	// database/sql's Open is lazy — it never touches the driver, so with
	// zero migrations the file would never materialize. A hibernate
	// route's activator starts replicating this path from boot, so a
	// zero-migration app must still create it: Ping forces the
	// connection open now, at boot, instead of on the first request.
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping %s: %w", path, err)
	}

	for i, m := range migrations {
		if _, err := db.Exec(m); err != nil {
			if isDuplicateColumn(err) {
				continue
			}
			db.Close()
			return nil, fmt.Errorf("migration %d: %w", i, err)
		}
	}
	return db, nil
}

// isDuplicateColumn matches the one class of migration error the family
// convention treats as success: an additive ALTER TABLE ... ADD COLUMN
// re-run against a database that already has it.
func isDuplicateColumn(err error) bool {
	return strings.Contains(err.Error(), "duplicate column")
}
