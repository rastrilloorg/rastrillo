// Package notes is the domain: two GORM models, a handful of
// owner-scoped CRUD handlers, and the wiring (app.go, render.go) that
// turns db, sessions, password, csrf, flash, scope and form into a
// running app. models.go and handlers.go are the ~150 lines this
// example exists to keep small — everything else here is plumbing.
package notes

import "time"

// Models is every model the schema generator manages. Keep it in step
// with the structs below: `rastrillo migration generate` reads it to
// work out what the database should look like, and `rastrillo
// migration check` fails CI when it and the migrations disagree.
var Models = []any{&User{}, &Note{}, &Export{}}

type User struct {
	ID           int64
	Email        string `gorm:"uniqueIndex"`
	PasswordHash string
	CreatedAt    time.Time
}

type Note struct {
	ID        int64
	UserID    int64 `gorm:"index"`
	Title     string
	Body      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Export is a finished "Export notes" document. Its ID is a random
// token minted before the job starts (startExport), so the job's
// Location is known up front. Owner is the session Subject — the same
// string jobs.Jobs keys job ownership by — not Note's numeric UserID:
// an Export lives beside its Job, not beside a Note, and showExport
// keys on both ID and Owner the same way jobs.Get does, so Bob
// fetching Alice's export is a 404, same as the notes.
type Export struct {
	ID        string `gorm:"primaryKey"`
	Owner     string `gorm:"index"`
	Content   string
	CreatedAt time.Time
}
