// Package db opens the application's SQLite database the way a CARLOS
// app needs it, exposed as one *gorm.DB.
//
// One file, two pools: writes go through a pool capped at one
// connection (SQLite allows one writer; queueing in the pool beats
// SQLITE_BUSY at the call site), reads through a pool of several (WAL
// supports many readers, and serialising them behind one connection
// turns an open *sql.Rows plus any second query into a silent
// deadlock). gorm.io/plugin/dbresolver routes each statement, so app
// code never picks a pool.
//
// The DSN keeps OpenDB's hard-won pragma order: busy_timeout before
// journal_mode=WAL (the reverse crashes under concurrent open), plus
// foreign_keys(1). The eager Ping keeps hibernation replication happy:
// the file exists on disk from boot.
package db

import (
	"database/sql"
	"fmt"
	"log/slog"
	"runtime"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/plugin/dbresolver"

	"github.com/carlosframework/rastrillo/gormlite"
)

// slogWriter adapts the app's *slog.Logger to GORM's logger.Writer.
// Everything GORM emits at the configured level (warnings, slow
// queries, real errors) lands in the app's own log stream instead of
// a second stdout format.
type slogWriter struct{ log *slog.Logger }

func (w slogWriter) Printf(format string, args ...any) {
	w.log.Warn(fmt.Sprintf(format, args...))
}

type DB struct {
	G *gorm.DB

	writer *sql.DB
	reader *sql.DB
}

func Open(path string, log *slog.Logger) (*DB, error) {
	if log == nil {
		log = slog.Default()
	}
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"

	w, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	if err := w.Ping(); err != nil {
		w.Close()
		return nil, fmt.Errorf("rastrillo/db: ping %s: %w", path, err)
	}

	r, err := sql.Open("sqlite", dsn)
	if err != nil {
		w.Close()
		return nil, err
	}
	readers := runtime.NumCPU()
	if readers < 4 {
		readers = 4
	}
	r.SetMaxOpenConns(readers)
	if err := r.Ping(); err != nil {
		w.Close()
		r.Close()
		return nil, fmt.Errorf("rastrillo/db: ping reader %s: %w", path, err)
	}

	// IgnoreRecordNotFoundError: a scoped by-ID miss (the 404-not-403
	// contract) surfaces as ErrRecordNotFound on every not-yours URL —
	// routine control flow, not an error worth a log line per hit.
	gl := logger.New(slogWriter{log}, logger.Config{
		SlowThreshold:             200 * time.Millisecond,
		LogLevel:                  logger.Warn,
		IgnoreRecordNotFoundError: true,
	})
	g, err := gorm.Open(gormlite.Dialector{Conn: w}, &gorm.Config{
		Logger:  gl,
		NowFunc: func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		w.Close()
		r.Close()
		return nil, err
	}
	if err := g.Use(dbresolver.Register(dbresolver.Config{
		Replicas: []gorm.Dialector{gormlite.Dialector{Conn: r}},
	})); err != nil {
		w.Close()
		r.Close()
		return nil, err
	}
	return &DB{G: g, writer: w, reader: r}, nil
}

func (d *DB) Close() error {
	rerr := d.reader.Close()
	if werr := d.writer.Close(); werr != nil {
		return werr
	}
	return rerr
}
