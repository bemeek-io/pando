package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/config"
	"github.com/bemeek-io/pando/internal/core/spec"
)

// TestR240_TheHostSetsWhatEveryNewAppIsGiven asserts R-240.
//
// Every app was given one CPU, with nothing that could change it, so a 12-CPU
// host refused its thirteenth app however idle the first twelve were (issue
// #55).
func TestR240_TheHostSetsWhatEveryNewAppIsGiven(t *testing.T) {
	t.Setenv("PANDO_DATABASE_URL", "postgres://pando@localhost/pando")
	t.Setenv("PANDO_APPS_CPU_MILLIS", "250")

	cfg, err := config.Load("")
	require.NoError(t, err)

	shipped := spec.StandardDefaults().Resources
	got := cfg.Apps.Resources(shipped)
	require.Equal(t, 250, got.CPUMillis)
	require.Equal(t, shipped.MemoryBytes, got.MemoryBytes, "what is not set keeps the shipped default")
	require.Equal(t, shipped.DiskBytes, got.DiskBytes)
}

// TestR240_TheHostSetsMemoryAndDiskForEveryNewApp asserts R-240 for the two
// limits the CPU test leaves at their shipped values.
func TestR240_TheHostSetsMemoryAndDiskForEveryNewApp(t *testing.T) {
	shipped := spec.StandardDefaults().Resources
	got := config.Apps{MemoryBytes: 2 << 30, DiskBytes: 40 << 30}.Resources(shipped)
	require.Equal(t, shipped.CPUMillis, got.CPUMillis)
	require.Equal(t, int64(2<<30), got.MemoryBytes)
	require.Equal(t, int64(40<<30), got.DiskBytes)
}

func TestAppLimitsAreTheShippedOnesWhenUnset(t *testing.T) {
	t.Setenv("PANDO_DATABASE_URL", "postgres://pando@localhost/pando")
	cfg, err := config.Load("")
	require.NoError(t, err)
	require.Equal(t, spec.StandardDefaults().Resources, cfg.Apps.Resources(spec.StandardDefaults().Resources))
}
