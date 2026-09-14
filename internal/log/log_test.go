package log_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/bemeek-io/pando/internal/log"
)

// A context without a logger must not panic. Losing a log line is bad; taking
// down the request path because of one is worse.
func TestFromEmptyContextIsSafe(t *testing.T) {
	require.NotNil(t, log.From(context.Background()))
	require.NotPanics(t, func() { log.From(context.Background()).Info("no-op") })
}

// Design 00 §3.3: fields accumulate as a request descends, so a handler logs
// request_id and app_id without knowing they were ever set.
func TestFieldsAccumulateThroughContext(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	ctx := log.Into(context.Background(), zap.New(core))

	ctx = log.With(ctx, zap.String("request_id", "req_01HQ8"))
	ctx = log.With(ctx, zap.String("principal_id", "usr_01HQ8"))
	ctx = log.With(ctx, zap.String("app_id", "app_01HQ8"))

	log.From(ctx).Info("deploying")

	require.Equal(t, 1, logs.Len())
	fields := logs.All()[0].ContextMap()
	require.Equal(t, "req_01HQ8", fields["request_id"])
	require.Equal(t, "usr_01HQ8", fields["principal_id"])
	require.Equal(t, "app_01HQ8", fields["app_id"])
}

// A derived context must not leak its fields back into its parent.
func TestDerivedContextDoesNotMutateParent(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	parent := log.Into(context.Background(), zap.New(core))
	child := log.With(parent, zap.String("app_id", "app_01HQ8"))

	log.From(parent).Info("parent")
	log.From(child).Info("child")

	entries := logs.All()
	require.NotContains(t, entries[0].ContextMap(), "app_id")
	require.Contains(t, entries[1].ContextMap(), "app_id")
}

func TestNewRejectsBadLevel(t *testing.T) {
	_, err := log.New("chatty", false)
	require.Error(t, err)

	l, err := log.New("debug", true)
	require.NoError(t, err)
	require.NotNil(t, l)
}

// A request-supplied value cannot forge a line of its own.
func TestUntrustedStripsWhatWouldForgeALine(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		want  string
	}{
		{"newline", "/x\nINFO\trequest\t{\"path\": \"/admin\"}", `/x INFO request {"path": "/admin"}`},
		{"carriage return", "/x\rmasked", "/x masked"},
		{"crlf", "/x\r\ntwo", "/x  two"},
		{"tab", "/x\ty", "/x y"},
		{"ansi escape", "/x\x1b[2J", "/x�[2J"},
		{"nul", "/x\x00y", "/x�y"},
		{"an ordinary path is left alone", "/apps/billing?tab=logs", "/apps/billing?tab=logs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			core, logs := observer.New(zapcore.DebugLevel)
			zap.New(core).Info("request", log.Untrusted("path", tc.value))

			require.Equal(t, tc.want, logs.All()[0].ContextMap()["path"])
		})
	}
}

// And the encoder writes one line, without having to escape anything.
//
// Both encoders are checked because the point of sanitizing at the call site is
// that the result does not depend on which one is configured. Each would pass
// this on its own escaping, so the assertion is on the bytes rather than on the
// line count: the value arrives already free of anything to escape.
func TestUntrustedNeedsNoEscapingFromEitherEncoder(t *testing.T) {
	for name, enc := range map[string]zapcore.Encoder{
		"console": zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig()),
		"json":    zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
	} {
		t.Run(name, func(t *testing.T) {
			buf, err := enc.EncodeEntry(
				zapcore.Entry{Level: zapcore.InfoLevel, Message: "request"},
				[]zapcore.Field{log.Untrusted("path", "/x\nINFO\tforged")},
			)
			require.NoError(t, err)

			line := strings.TrimSuffix(buf.String(), "\n")
			require.NotContains(t, line, "\n")
			require.NotContains(t, line, `\n`)
			require.Contains(t, line, "/x INFO forged")
		})
	}
}
