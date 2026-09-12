package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/config"
)

// TestReconcilerTuningComesFromTheEnvironment asserts the knobs that make the
// acceptance suite finish in minutes instead of forty.
//
// Worth a test rather than trust: this is the second time a setting has been
// bound to an environment variable and silently not applied, because viper
// resolves an env var only for keys it already knows about. A setting that does
// nothing and says nothing is the failure mode, and it is invisible.
func TestReconcilerTuningComesFromTheEnvironment(t *testing.T) {
	t.Setenv("PANDO_DATABASE_URL", "postgres://pando@localhost/pando")
	t.Setenv("PANDO_RECONCILER_BACKOFF", "0s,1s,2s,3s,4s")
	t.Setenv("PANDO_RECONCILER_FAILURE_WINDOW", "2m")
	t.Setenv("PANDO_RECONCILER_FAILURE_THRESHOLD", "4")

	cfg, err := config.Load("")
	require.NoError(t, err)

	schedule, err := cfg.Reconciler.BackoffSchedule()
	require.NoError(t, err)
	require.Equal(t, []time.Duration{
		0, time.Second, 2 * time.Second, 3 * time.Second, 4 * time.Second,
	}, schedule)

	require.Equal(t, 2*time.Minute, cfg.Reconciler.FailureWindow)
	require.Equal(t, 4, cfg.Reconciler.FailureThreshold)
}

// TestReconcilerTuningIsUnsetByDefault — the shipped numbers are the
// requirement, so an install that sets nothing gets them.
func TestReconcilerTuningIsUnsetByDefault(t *testing.T) {
	t.Setenv("PANDO_DATABASE_URL", "postgres://pando@localhost/pando")

	cfg, err := config.Load("")
	require.NoError(t, err)

	schedule, err := cfg.Reconciler.BackoffSchedule()
	require.NoError(t, err)
	require.Nil(t, schedule, "no schedule configured means R-149's default")
	require.Zero(t, cfg.Reconciler.FailureWindow)
	require.Zero(t, cfg.Reconciler.FailureThreshold)
}

// TestABadBackoffIsRefusedAtStartup — a typo must not silently fall back to the
// default, because the person who set it would never know.
func TestABadBackoffIsRefusedAtStartup(t *testing.T) {
	t.Setenv("PANDO_DATABASE_URL", "postgres://pando@localhost/pando")
	t.Setenv("PANDO_RECONCILER_BACKOFF", "0s,5,15s")

	cfg, err := config.Load("")
	require.NoError(t, err)

	_, err = cfg.Reconciler.BackoffSchedule()
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a duration")
}

// TestBaseDomainComesFromTheEnvironment guards the setting that found this trap.
//
// PANDO_SERVER_BASE_DOMAIN was bound and still arrived empty, and the symptom
// appeared much later and elsewhere: an app configured for its own hostname,
// with no hostname, refused at plan time. Nothing pointed at the config.
func TestBaseDomainComesFromTheEnvironment(t *testing.T) {
	t.Setenv("PANDO_DATABASE_URL", "postgres://pando@localhost/pando")
	t.Setenv("PANDO_SERVER_BASE_DOMAIN", "apps.example.com")

	cfg, err := config.Load("")
	require.NoError(t, err)
	require.Equal(t, "apps.example.com", cfg.Server.BaseDomain)
}
