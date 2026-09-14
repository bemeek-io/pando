package local_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/adapter/identity/local"
	"github.com/bemeek-io/pando/internal/errs"
)

// R-041: the local adapter is what a fresh install authenticates against, and
// it has no external dependency to be unhealthy.
func TestIdentityAndHealth(t *testing.T) {
	a := local.New(nil)

	require.Equal(t, local.Kind, a.Kind())
	require.Equal(t, api.CategoryIdentity, a.Category())
	require.NoError(t, a.HealthCheck(context.Background()),
		"the store it reads is Pando's own")
}

// R-047: each adapter declares its own session policy, so the console can show
// the real revocation window per adapter rather than implying a global one.
func TestR047_TheSessionPolicyIsDeclaredAndConfigurable(t *testing.T) {
	a := local.New(nil)
	require.NoError(t, a.Configure(context.Background(), nil))

	policy := a.SessionPolicy()
	require.Equal(t, 12*time.Hour, policy.MaxLifetime)
	require.Equal(t, api.RevocationPush, policy.RevocationMode)

	// True in the trivial sense: changes originate here, so revocation is
	// immediate rather than eventual.
	require.True(t, a.SupportsPush())

	require.NoError(t, a.Configure(context.Background(),
		json.RawMessage(`{"session_max_lifetime":"1h"}`)))
	require.Equal(t, time.Hour, a.SessionPolicy().MaxLifetime)
}

func TestConfigureRefusesADurationItCannotRead(t *testing.T) {
	a := local.New(nil)

	err := a.Configure(context.Background(), json.RawMessage(`{`))
	require.Equal(t, errs.ValidInvalid, errs.CodeOf(err))

	err = a.Configure(context.Background(), json.RawMessage(`{"session_max_lifetime":"a while"}`))
	require.Equal(t, errs.ValidInvalid, errs.CodeOf(err))
	require.Contains(t, errs.As(err).Message, `"12h"`, "R-105: it shows a valid answer")

	require.Equal(t, 12*time.Hour, a.SessionPolicy().MaxLifetime, "the bad value was not adopted")
}

// This adapter authenticates inline: there is nowhere to redirect to.
func TestBeginRedirectsNowhere(t *testing.T) {
	redirect, err := local.New(nil).Begin(context.Background(), "/apps")
	require.NoError(t, err)
	require.Nil(t, redirect)
}
