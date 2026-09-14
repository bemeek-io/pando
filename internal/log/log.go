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
	"strings"
	"unicode"

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

// Untrusted is zap.String for a value that came from a request.
//
// A request path reaches a handler percent-decoded, so a caller can put a
// newline in one. A newline in a log line ends it, and what follows is a second
// line indistinguishable from one Pando wrote — the caller choosing what the
// record of its own request says.
//
// Both of zap's encoders happen to prevent that today: the JSON encoder
// escapes control characters, and the console encoder writes the field block as
// JSON and escapes them too. That makes this belt and braces, and it is worth
// having because the property is currently owned by a dependency's choice of
// encoder rather than by any code here. Sanitizing at the call site means the
// guarantee survives a different encoder, a different logger, or a value that
// reaches an io.Writer some other way.
//
// Newlines, carriage returns and tabs become spaces — the three control
// characters with an obvious readable substitute. Everything else unprintable
// becomes U+FFFD: an ANSI escape in a path is not a forged line, but it can
// repaint the terminal of whoever is reading the log, and nothing Pando logs
// has a use for one.
func Untrusted(key, value string) zap.Field {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\t", " ")
	value = strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return '�'
	}, value)
	return zap.String(key, value)
}
