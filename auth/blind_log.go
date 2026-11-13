package auth

import (
	"context"
	"fmt"
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

// An SMTP or provider error can name its recipient. Legacy records mode
// logs only the type when Blind is set; BlindLinks also strips attributes.
func (auth *Auth) logError(msg string, err error) {
	if auth.cfg.Blind {
		auth.cfg.Logger.Error(msg, "err_type", fmt.Sprintf("%T", err))
		return
	}
	auth.cfg.Logger.Error(msg, "err", err)
}
