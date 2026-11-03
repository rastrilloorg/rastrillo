//go:build perf

package chattest

import (
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/background"
	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/mail"
	"amadan.net/rastrillo/rastrillo/perf/perftest"

	genlocales "chat/gen/locales"
	"chat/internal/chat"
)

// Every GET screen is held to 150ms to first byte and to its first-load
// weight: a new route fails until measured or in .rastrillo/budgets.txt.
// Never t.Parallel() in this file: a perf test measures its neighbours.
func TestPerfScreens(t *testing.T) {
	t.Setenv("CHAT_ADMINS", "perf@example.com")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	database, err := db.Open(schema.Path(t), logger)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	group := &background.Group{}
	box := &outbox{}
	router, hub, err := chat.Router(
		database, testOrigin, group, box, logger)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.G.Create(&chat.Room{Name: "General"}).Error; err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/", router)
	options := servingOptions(logger)
	chat.Configure(&options, mux, hub, time.Now())
	options.Background = group // closeAll stops it
	handler, closeAll, err := rastrillo.Handler(options)
	if err != nil {
		t.Fatal(err)
	}
	defer closeAll()
	perftest.Screens(t, perftest.ScreenConfig{
		Routes: router, Handler: handler,
		Paths: map[string]string{
			"/rooms/{id}":              "/rooms/1",
			"/admin/rooms/{id}":        "/admin/rooms/1",
			"/admin/rooms/{id}/delete": "/admin/rooms/1/delete",
		},
		Signin: func(client *http.Client, base string) {
			signInOver(t, client, base, box, "perf@example.com")
		},
	})
}

func TestPerfBoot(t *testing.T) {
	perftest.Boot(t, perftest.BootConfig{
		Prepare: schema.Path,
		Build:   boot,
		Path:    "/",
	})
}

func boot(path string) (http.Handler, func(), error) {
	started := time.Now()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	database, err := db.Open(path, logger)
	if err != nil {
		return nil, nil, err
	}
	group := &background.Group{}
	mailer := mail.Logged(logger)
	mux, hub, err := chat.New(
		database, testOrigin, group, mailer, logger)
	if err != nil {
		group.Stop()
		database.Close()
		return nil, nil, err
	}
	options := servingOptions(logger)
	chat.Configure(&options, mux, hub, started)
	options.Background = group // closeAll stops it
	handler, closeAll, err := rastrillo.Handler(options)
	if err != nil {
		database.Close()
		return nil, nil, err
	}
	return handler, func() { closeAll(); database.Close() }, nil
}

// servingOptions are main.go's, so the lane measures what is served.
func servingOptions(logger *slog.Logger) rastrillo.Options {
	return rastrillo.Options{Logger: logger, Locales: genlocales.Locales,
		DefaultLocale: genlocales.DefaultLocale}
}
