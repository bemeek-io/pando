// Package log threads a single structured logger through context.
//
// Design 00 §3.3: every log line inside a request carries request_id,
// principal_id, and where applicable app_id. Threading the logger through
// context rather than passing it explicitly is what makes that automatic — a
// handler deep in a call stack logs with the request's fields without knowing
// they exist.
package log

import (
	"context"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type ctxKey struct{}

// New builds the application logger. Production emits JSON; development emits
// human-readable console output.
func New(level string, development bool) (*zap.Logger, error) {
	lvl, err := zapcore.ParseLevel(level)
	if err != nil {
		return nil, err
	}

	cfg := zap.NewProductionConfig()
	if development {
		cfg = zap.NewDevelopmentConfig()
		cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}
	cfg.Level = zap.NewAtomicLevelAt(lvl)
	// Timestamps are RFC 3339 on the wire and in logs (design 00 §3.5).
	cfg.EncoderConfig.EncodeTime = zapcore.RFC3339TimeEncoder

	return cfg.Build()
}

// Into returns a context carrying l.
func Into(ctx context.Context, l *zap.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// From returns the logger in ctx, or a no-op logger.
//
// It never returns nil, so a caller that forgot to seed the context gets
// silence rather than a panic. Losing a log line is bad; taking down a request
// path because of one is worse.
func From(ctx context.Context) *zap.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*zap.Logger); ok && l != nil {
		return l
	}
	return zap.NewNop()
}

// With returns a context whose logger carries the additional fields. This is
// how request_id, principal_id and app_id accumulate as a request descends
// through the layers that know them.
func With(ctx context.Context, fields ...zap.Field) context.Context {
	return Into(ctx, From(ctx).With(fields...))
}
