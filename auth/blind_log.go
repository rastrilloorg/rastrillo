package auth

import (
	"context"
	"log/slog"
)

type blindLog struct{ next slog.Handler }

func (handler blindLog) Enabled(ctx context.Context, level slog.Level) bool {
	return handler.next.Enabled(ctx, level)
}

func (handler blindLog) Handle(ctx context.Context, record slog.Record) error {
	// Errors, paths and bound attributes can contain an address.
	clean := slog.NewRecord(
		record.Time, record.Level, record.Message, record.PC,
	)
	return handler.next.Handle(ctx, clean)
}

// A derived logger must not bypass the same attribute boundary.
func (handler blindLog) WithAttrs([]slog.Attr) slog.Handler { return handler }
func (handler blindLog) WithGroup(string) slog.Handler      { return handler }
