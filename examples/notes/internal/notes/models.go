// Package notes is the domain: two GORM models, a handful of
// owner-scoped CRUD handlers, and the wiring (app.go, render.go) that
// turns db, sessions, password, csrf, flash, scope and form into a
// running app. models.go and handlers.go are the ~150 lines this
// example exists to keep small — everything else here is plumbing.
package notes

import "time"

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
