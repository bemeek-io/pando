package log_test

import (
	"context"
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
