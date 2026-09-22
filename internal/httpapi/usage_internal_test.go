package httpapi

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/spec"
)

// TestR245_UsageFollowsTheSpec asserts the pairing: the spec's order and
// primary, each volume under the part that mounts it, a declared part that is
// not running shown with nothing in use, and a part the runtime runs that the
// spec does not declare still listed.
func TestR245_UsageFollowsTheSpec(t *testing.T) {
	s := &spec.AppSpec{
		Workloads: []spec.Workload{
			{Name: "web", Primary: true, Mounts: []spec.Mount{{VolumeID: "vol_data", Path: "/data"}}},
			{Name: "worker"},
		},
		Volumes: []spec.Volume{{ID: "vol_data", Name: "data"}},
	}
	reading := api.BundleUsage{
		Workloads: []api.WorkloadUsage{
			{Workload: "db", Running: true, CPUMillis: 20, MemoryBytes: 1 << 20, DiskBytes: 5},
			{Workload: "web", Running: true, CPUMillis: 150, CPULimitMillis: 500, MemoryBytes: 64 << 20, MemoryLimitBytes: 256 << 20, DiskBytes: 1024},
		},
		Volumes: []api.VolumeUsage{{VolumeID: "vol_data", Bytes: 4096}},
	}

	got := workloadUsages(s, reading)
	require.Len(t, got, 3)

	require.Equal(t, "web", got[0].Name)
	require.True(t, got[0].Primary)
	require.Equal(t, 150, got[0].CPUMillis)
	require.Equal(t, 500, got[0].CPULimitMillis)
	require.Equal(t, int64(256<<20), got[0].MemoryLimit)
	require.Equal(t, []VolumeUsage{{ID: "vol_data", Name: "data", Path: "/data", Bytes: 4096}}, got[0].Volumes)

	require.Equal(t, "worker", got[1].Name)
	require.False(t, got[1].Running)
	require.Equal(t, int64(-1), got[1].DiskBytes, "not running: disk unknown, not zero")

	require.Equal(t, "db", got[2].Name)
	require.False(t, got[2].Primary)
	require.Equal(t, 20, got[2].CPUMillis)
}
