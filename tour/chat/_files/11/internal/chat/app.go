package chat

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/action"
	"amadan.net/rastrillo/rastrillo/auth"
	"amadan.net/rastrillo/rastrillo/background"
	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/live"
	"amadan.net/rastrillo/rastrillo/mail"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/perf"

	// Here, not in main, so the app's tests link the packs too.
	_ "chat/gen/locales"
)

type App struct {
	database *db.DB
	logger   *slog.Logger
	origin   string
	api      http.Handler
	mcp      http.Handler
	live     *live.Hub
	auth     *auth.Auth
}

// New migrates the database and builds the app, as one router inside the
// ServeMux that rastrillo.Options.Mux takes.
// Stop group before closing the database: sign-in's sweeps run on it.
func New(
	database *db.DB, origin string, group *background.Group,
	mailer mail.Sender, logger *slog.Logger,
) (*http.ServeMux, *live.Hub, error) {
	router, hub, err := Router(database, origin, group, mailer, logger)
	if err != nil {
		return nil, nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/", router)
	return mux, hub, nil
}

// Router is New's router, apart so that the perf test can walk it.
func Router(
	database *db.DB, origin string, group *background.Group,
	mailer mail.Sender, logger *slog.Logger,
) (chi.Router, *live.Hub, error) {
	if pagesErr != nil {
		return nil, nil, pagesErr
	}
	// BootSchema, not Schema: migrations.go says why.
	migrated, err := migrate.Apply(context.Background(), database, BootSchema)
	if err != nil {
		return nil, nil, err
	}
	logger.Info("migrate", "applied", migrated.Applied,
		"skipped", migrated.Skipped, "adopted", migrated.Adopted)
	api, mcp, err := newDoors(database, logger)
	if err != nil {
		return nil, nil, err
	}
	app := &App{database: database, logger: logger, origin: origin,
		api: api, mcp: mcp}
	if err := app.startAuth(group, mailer); err != nil {
		return nil, nil, err
	}
	app.live, err = live.New(live.Config{
		Path: string(livePage),
		Topics: map[string]live.Allow{
			"room": anyoneSignedIn,
		},
	})
	if err != nil {
		return nil, nil, err
	}
	router := chi.NewRouter()
	// First, so a page never misses a write.
	router.Use(app.live.Middleware)
	Routes(action.NewRoutes(router, app.contextFor), app)
	return router, app.live, nil
}

// Configure sets the serving options for main.go and the perf test, so
// what the perf lane measures is what production serves.
func Configure(
	options *rastrillo.Options, mux *http.ServeMux, hub *live.Hub,
	started time.Time,
) {
	options.Mux = mux
	options.Live = hub
	options.ErrorPage = ErrorPage
	var recorder perf.Recorder
	options.Wrap = perf.Middleware(&recorder,
		perf.Options{Started: started, Logger: options.Logger})
}
