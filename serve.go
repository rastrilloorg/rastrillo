// Package rastrillo is the CARLOS web framework — the shape of a CARLOS
// app, the way carlosframework/platform is the shape of the deployment
// substrate it runs on. See the design doc for the full picture:
// https://github.com/carlosframework/platform/blob/main/docs/superpowers/specs/2026-08-01-carlos-framework-design.md
//
// The root package holds the process shape (Run/Serve/Handler, the
// activation contract, the SQLite opener, migrations), the action
// vocabulary (Ctx, Actor), the manifest vocabulary (Resource, Tool),
// fingerprinted assets, and localization. The subsystems live beside
// it: crypto (the family envelope), auth (keymail sign-in with the
// magic-link fallback), webauthn, eventlog (the Mergeable store),
// blobs, mail, carlos (the platform's scheduled-work contract), tools
// (agent dispatch), and ui (the component partials). README.md keeps
// the honest status list.
package rastrillo

import (
	"context"
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
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/carlosframework/rastrillo/carlos"

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

	// Wrap, if set, wraps the app's mux — the one seam for app
	// middleware: sessions, CSRF, panic pages, authorization
	// (gleester's friction, James 2026-08-04; also the friction behind
	// amadan's outer-catch-all-mux workaround). It runs inside the
	// framework's chrome: GET /healthz and GET /api/version are
	// answered outside it (platform probes never traverse app
	// middleware), and locale-prefix stripping happens before it,
	// so middleware sees the same paths routes match on. Nil means
	// no wrapping. Returning nil is a boot error.
	Wrap func(http.Handler) http.Handler

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

	// CSP replaces the value of the Content-Security-Policy header the
	// framework sets on every response (see the package's default
	// below: same-origin everything, inline styles allowed because
	// ui's partials carry style attributes, framing denied). Empty
	// keeps the default. The other baseline headers have no Options
	// field on purpose: all of them — this one included — are set
	// before any app code runs, so an app that wants different values
	// sets (or deletes) its own in a handler or Options.Wrap middleware
	// and simply wins.
	CSP string

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

	// BaseCatalog optionally supplies a base catalog that sits UNDER
	// every app catalog (Locales' own doc comment: requested locale's
	// app catalog, then the default locale's app catalog, then this) —
	// normally the generated gen/locales/locales.go var BaseCatalog a
	// manifest resource's field labels and shared ui.* chrome strings
	// compile to (design doc §9's manifest system; internal/generate's
	// EmitLocales emits it from the same map as the human-readable
	// gen/locales/en.toml, so the two cannot drift). Nil is legal — an
	// app with no manifest resources has nothing to layer.
	BaseCatalog Catalog

	// Logger defaults to slog.Default() if nil.
	Logger *slog.Logger

	// ErrorPage renders the app's own error page for a failure the app
	// never saw: today, a panic that reached the framework's recovery
	// wrapper. Nil answers a plain "Something went wrong." — correct,
	// and ugly enough that most apps will want to set it.
	//
	// It is the same function an app puts on Ctx.ErrorPage, which is
	// what view.Fail/NotFound/Forbidden call, so a 500 from a handler
	// and a 500 from a panic look identical to the person reading it.
	// ui's error-page partial is the body; ref is the reference the
	// failure was logged under.
	//
	// It is called only when nothing has been written yet in the
	// common case; see recoverPanics for the mid-stream caveat.
	ErrorPage ErrorPageFunc

	// ReadHeaderTimeout bounds how long a client may take to send its
	// request headers. Zero uses defaultReadHeaderTimeout. This is the
	// slowloris bound: it costs a legitimate client nothing, because
	// headers are small and sent up front.
	ReadHeaderTimeout time.Duration

	// IdleTimeout bounds how long an idle keep-alive connection is kept
	// open between requests. Zero uses defaultIdleTimeout. It can never
	// interrupt an in-flight request — only a connection doing nothing.
	IdleTimeout time.Duration

	// ReadTimeout and WriteTimeout are OFF by default (zero), and an app
	// should think before setting them, because net/http applies them as
	// TOTAL deadlines measured from the start of the request — not idle
	// deadlines. A 40 MB upload over a slow link, a git pack streaming
	// for minutes, a Server-Sent Events feed and a WebSocket are all
	// legitimate and all unbounded in duration, and any of them is cut
	// mid-flight by a total deadline no matter how healthy the peer is.
	//
	// Set these only for an app whose every request is known to be short
	// (a JSON API with small bodies, say). To bound a STALLED peer on a
	// long-lived request without capping a slow-but-healthy one, the tool
	// is a per-handler idle deadline via http.ResponseController's
	// SetReadDeadline/SetWriteDeadline, re-armed as bytes move — not
	// these fields. See the package docs on Serve.
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// Default server timeouts. Both bound connection-level behaviour that no
// legitimate request depends on, which is what makes them safe to apply
// to every rastrillo app without an opt-out.
const (
	defaultReadHeaderTimeout = 20 * time.Second
	defaultIdleTimeout       = 120 * time.Second
)

// newServer builds the http.Server Serve runs.
//
// It exists because the zero-value &http.Server{Handler: handler} this
// replaced set no timeouts at all: a peer that stopped reading, or that
// vanished without a FIN, tied up a connection — and whatever the handler
// held — for as long as the process lived, with nothing anywhere in the
// stack able to fire.
//
// What this does and does not buy is worth being precise about, because
// the gap is where the real incidents live. It bounds two things at the
// connection level: a client that never finishes its headers, and an idle
// keep-alive connection. It does NOT bound a peer that stalls midway
// through a request body or a response — that needs an idle deadline
// around the streaming span itself, which only the handler can place,
// because only the handler knows where that span is. See ReadTimeout's
// doc comment for why the total-deadline fields are not that tool.
func newServer(opts Options, handler http.Handler) *http.Server {
	readHeader := opts.ReadHeaderTimeout
	if readHeader == 0 {
		readHeader = defaultReadHeaderTimeout
	}
	idle := opts.IdleTimeout
	if idle == 0 {
		idle = defaultIdleTimeout
	}
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: readHeader,
		IdleTimeout:       idle,
		ReadTimeout:       opts.ReadTimeout,
		WriteTimeout:      opts.WriteTimeout,
	}
}

// Serve opens the database (if configured), applies migrations, resolves
// the platform's activation contract for a listener, and serves until the
// process receives SIGTERM/SIGINT. It always answers GET /healthz itself
// — the manifest/action layer never has to remember to.
//
// # Timeouts
//
// The server bounds two things for every app: how long a client may take
// to send its request headers (Options.ReadHeaderTimeout) and how long an
// idle keep-alive connection is kept open (Options.IdleTimeout). Neither
// can interrupt an in-flight request, which is what makes them safe as
// defaults.
//
// Nothing here bounds a peer that stalls PART-WAY through a request body
// or a response. That is deliberate: net/http's ReadTimeout and
// WriteTimeout are total deadlines, so using them for that would cut off
// slow-but-healthy clients — a large upload, a git pack, an SSE feed, a
// WebSocket — along with the stalled ones. They are available on Options
// for apps that genuinely have only short requests, and off otherwise.
//
// An app that streams must therefore bound its own streaming span, with
// an idle deadline re-armed as bytes move:
//
//	rc := http.NewResponseController(w)
//	rc.SetWriteDeadline(time.Now().Add(idle)) // before each write
//
// Set it on the side that can actually block. A handler copying from a
// subprocess pipe blocks on the READ of that pipe, not on the write to
// the client, and a write deadline never fires there — the pipe needs its
// own deadline (os.File supports one) and the child needs a process-group
// kill to reap grandchildren still holding it open. This is not
// hypothetical: it wedged a production app for 158 minutes on 2026-08-19.
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

	srv := newServer(opts, handler)
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
// serving handler — framework endpoints, Wrap, locales and all. The
// returned close func releases the database handle (a no-op without
// one).
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
		// buildHandler's error sources (NewLocales, the Wrap nil-handler
		// check) already carry the rastrillo: prefix, so no re-wrap here.
		closeDB()
		return nil, closeNothing, err
	}
	return handler, closeDB, nil
}

// defaultCSP is the Content-Security-Policy every rastrillo response
// carries unless Options.CSP replaces it: same-origin scripts, styles,
// images, fetches and form targets; inline STYLES allowed because ui's
// partials set style attributes (meter's fill percentage) — inline
// scripts are not, which is why the shim ships as a file; data: images
// allowed for embedded favicons; framing denied.
const defaultCSP = "default-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"

// securityHeaders is the outermost layer of the serving handler: the
// baseline headers a security audit expects on every response, set
// BEFORE anything else runs so that any header an app sets itself —
// in a handler or Options.Wrap middleware — replaces the default, and
// a Del removes it. Framework-owned for the same reason csrf is:
// hand-rolled per-app hygiene is where apps ship gaps.
func securityHeaders(csp string, next http.Handler) http.Handler {
	if csp == "" {
		csp = defaultCSP
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

// recoverPanics turns a panicking handler into a logged 500. It is the
// OUTERMOST wrapper — outside securityHeaders, and so outside the locale
// middleware and Options.Wrap too — because a panic in a middleware is
// exactly as fatal to the connection as a panic in a handler, and the
// only wrapper that can catch one is the one above it. Headers that the
// inner wrappers already set on the ResponseWriter are still there when
// the recovery writes, so a recovered response is a hardened one.
//
// http.ErrAbortHandler goes straight back up: it is net/http's own
// "abandon this response, quietly" signal, and answering it with a 500
// page would append a bogus body to a stream the handler deliberately
// abandoned.
//
// There is no ResponseWriter wrapper here, and so no "did we already
// write?" check. A panic AFTER the first byte leaves a broken page: the
// status and headers are gone, and the error page's markup lands
// appended to whatever was written. That is precisely what net/http
// does today (minus the log line and the page), and the alternative —
// wrapping every response in a recording writer — costs every request
// to improve the rarest one. An app that streams should not panic
// mid-stream.
func recoverPanics(errorPage ErrorPageFunc, logger *slog.Logger, next http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if v == http.ErrAbortHandler {
				panic(v)
			}
			ref := NewRef()
			logger.Error("panic", "ref", ref, "err", v, "stack", string(debug.Stack()))
			if errorPage != nil {
				errorPage(w, r, http.StatusInternalServerError, ref)
				return
			}
			http.Error(w, "Something went wrong.", http.StatusInternalServerError)
		}()
		next.ServeHTTP(w, r)
	})
}

// buildHandler assembles the serving handler: the framework's own
// endpoints, the app mux (wrapped by Options.Wrap when set), and
// — when Options.Locales is set — the locale middleware wrapped around
// the whole thing, so a locale prefix strips before routing and the
// translator rides the request context (§10). The security-header
// wrapper goes around all of it, and panic recovery around that. Split
// from Handler so the assembly is testable without a database.
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
	app := http.Handler(opts.Mux)
	if opts.Wrap != nil {
		if app = opts.Wrap(opts.Mux); app == nil {
			return nil, errors.New("rastrillo: Options.Wrap returned a nil handler")
		}
	}
	mux.Handle("/", app)

	if len(opts.Locales) == 0 {
		return recoverPanics(opts.ErrorPage, opts.Logger, securityHeaders(opts.CSP, mux)), nil
	}
	def := opts.DefaultLocale
	if def == "" {
		def = opts.Locales[0]
	}
	// The framework base catalog (rastrillo.ui.* keys) is the third
	// fallback layer §10 reserved — passing it here, rather than nil, is
	// what lets ui's partials resolve correctly-worded English defaults
	// through T for an app that ships no catalog of its own at all. An
	// app's own Options.BaseCatalog (normally the manifest-generated
	// gen/locales var) shares that layer: it overlays the framework's
	// keys, app winning on any shared key, still under every per-locale
	// app catalog.
	base := BaseCatalog()
	for k, v := range opts.BaseCatalog {
		base[k] = v
	}
	loc, err := NewLocales(opts.Locales, def, base, opts.LocaleFS)
	if err != nil {
		return nil, err
	}
	// Registered after mux.Handle("/", app) only for readability:
	// ServeMux prefers the more specific pattern whatever the order.
	mux.Handle("POST "+LocaleSwitchPath, loc.SwitchHandler())
	return recoverPanics(opts.ErrorPage, opts.Logger, securityHeaders(opts.CSP, loc.Middleware(mux))), nil
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
// Options.NextDue). It presents the same secret a scheduled tick does —
// $CARLOS_ADMIN_TOKEN, the instance-local token the platform's exec
// backend delivers in the overlay env file — so it authenticates with
// carlos.Tick rather than a second, subtly different compare of its
// own. That is not tidiness: the version here compared the raw header
// bytes, and ConstantTimeCompare returns 0 immediately on a length
// mismatch, so it leaked the token's length by timing. One token, one
// check, and no token in the environment still fails closed.
func nextDueHandler(nextDue func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !carlos.Tick(r) {
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
