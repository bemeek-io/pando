package state_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/trypando/pando/internal/core/state"
	"github.com/trypando/pando/internal/errs"
	"github.com/trypando/pando/internal/secret"
)

// Waiting for Postgres honors the caller's context: a canceled one ends the
// wait at once rather than after the minute a Compose start is given.
func TestPreparingOrConnectingACopyStopsWithTheContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	const nobody = "postgres://pando:x@127.0.0.1:1/pando?sslmode=disable"

	started := time.Now()
	_, err := state.PrepareTemplate(ctx, nobody)
	require.ErrorIs(t, err, context.Canceled)
	_, err = state.ConnectCopy(ctx, nobody, secret.New("x"))
	require.ErrorIs(t, err, context.Canceled)
	require.Less(t, time.Since(started), 5*time.Second)
}

func TestMigratingAMalformedURLSaysSo(t *testing.T) {
	err := state.Migrate(context.Background(), "postgres://%zz")
	require.Error(t, err)
	require.Equal(t, errs.Internal, errs.CodeOf(err))
	require.Contains(t, errs.As(err).Message, "malformed")
}
