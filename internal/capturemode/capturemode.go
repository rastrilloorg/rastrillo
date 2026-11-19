// Package capturemode is what a database entry point needs to know
// about rastrillo generate's capture: whether this process is one, and
// where its database goes. It is apart from tmpl so that db, which
// opens databases, does not import html/template to ask.
package capturemode

import (
	"fmt"
	"os"
	"sync"
)

// Env is tmpl.CaptureEnv: set, it names the file a capture writes.
const Env = "RASTRILLO_TEMPLATE_CAPTURE"

var (
	mu    sync.Mutex
	names = map[string]string{} // configured path to the name given it
)

// DBPath is the database path to open for path. In capture mode it is
// a plain name in the capture's working directory, a scratch directory
// generate removes, so the app's startup runs, its migrations
// included, and a real database is never opened.
//
// Nothing of path is kept. SQLite decodes a file: URI, so
// file:%2Fsrv%2Fapp.db names the real database whatever its base name
// reads, and a query string can reopen it read-write. The first path
// gets capture.db and each other path capture-2.db and on, so the same
// database opened through both entry points is one file, as it would
// have been.
func DBPath(path string) string {
	if path == "" || os.Getenv(Env) == "" {
		return path
	}
	mu.Lock()
	defer mu.Unlock()
	if name, ok := names[path]; ok {
		return name
	}
	name := "capture.db"
	if count := len(names); count > 0 {
		name = fmt.Sprintf("capture-%d.db", count+1)
	}
	names[path] = name
	return name
}
