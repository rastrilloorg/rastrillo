package rastrillo

import (
	"database/sql"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/background"
)

// The close func Handler returns — the same one Serve defers — must stop
// the app's background work BEFORE it closes the database. Work still
// running when the handle closes fails with "sql: database is closed",
// which is the teardown bug the background package exists for; the
// order here is the whole guarantee.
func TestHandlerCloseStopsBackgroundBeforeTheDatabase(t *testing.T) {
	var g background.Group
	var db *sql.DB
	_, closeAll, err := Handler(Options{
		DBPath:     filepath.Join(t.TempDir(), "app.db"),
		Background: &g,
		Router: func(d *sql.DB) (*http.ServeMux, error) {
			db = d
			return http.NewServeMux(), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	started, release := make(chan struct{}), make(chan struct{})
	var workErr error
	g.Go(func() {
		close(started)
		<-release
		// The write that records the work happened, arriving after
		// shutdown began.
		_, workErr = db.Exec(`CREATE TABLE IF NOT EXISTS sent (id INTEGER)`)
	})
	<-started

	closed := make(chan error, 1)
	go func() { closed <- closeAll() }()
	select {
	case <-closed:
		t.Fatal("close returned while background work was still running")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-closed; err != nil {
		t.Fatalf("close: %v", err)
	}
	if workErr != nil {
		t.Fatalf("background work lost its database: %v", workErr)
	}
	if g.Go(func() {}) {
		t.Fatal("the Group still accepts work after close")
	}
}
