// Package gormfn lets a Go migration be written against GORM without
// the migrate package itself linking an ORM.
//
// migrate hands a Go migration a migrate.Tx: the pinned connection,
// already inside the BEGIN IMMEDIATE that writes the migration's ledger
// row. Fn builds a *gorm.DB on exactly that connection, so a
// GORM-bodied migration keeps the guarantee a SQL one has — its writes
// and its ledger row commit or roll back together.
//
//	migrate.MustFromFS(fs, "notes").Add(migrate.Migration{
//		ID: "0003_backfill",
//		Fn: gormfn.Fn(func(g *gorm.DB) error { ... }),
//	})
package gormfn

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"amadan.net/rastrillo/rastrillo/gormlite"
	"amadan.net/rastrillo/rastrillo/migrate"
)

// Fn adapts a GORM-bodied migration to migrate.Migration.Fn.
func Fn(f func(*gorm.DB) error) func(context.Context, migrate.Tx) error {
	return func(ctx context.Context, tx migrate.Tx) error {
		// The *gorm.DB is backed by tx itself, never the app's writer
		// pool: that pool has exactly one connection (SQLite allows one
		// writer) and Apply already holds it for the whole run, so
		// building on it would deadlock.
		//
		// SkipDefaultTransaction is required, not an optimisation:
		// without it GORM wraps every Create/Update/Delete in its own
		// BeginTransaction, and a *sql.Conn satisfies gorm.TxBeginner,
		// so that issues a real nested BEGIN inside BEGIN IMMEDIATE —
		// which SQLite refuses ("cannot start a transaction within a
		// transaction").
		g, err := gorm.Open(gormlite.Dialector{Conn: tx}, &gorm.Config{
			Logger:                 logger.Default.LogMode(logger.Silent),
			SkipDefaultTransaction: true,
		})
		if err != nil {
			return fmt.Errorf("gormfn: open gorm on the pinned connection: %w", err)
		}
		// Binds the boot deadline Apply was given, so the migration's
		// own GORM calls inherit it instead of gorm.Open's
		// context.Background().
		return f(g.WithContext(ctx))
	}
}
