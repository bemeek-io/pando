package docker

import (
	"strings"
	"testing"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/stretchr/testify/require"
)

// TestR221_TheHealthProbeUsesWhateverTheImageHas asserts that the probe does
// not depend on a package the app's image never promised to carry.
//
// It ran wget alone. An image with curl and no wget answered "/bin/sh: 1:
// wget: not found" every thirty seconds, and the app — which was serving 200s
// the whole time — never came ready. The image belongs to whoever wrote the
// app; the probe asks what is there.
func TestR221_TheHealthProbeUsesWhateverTheImageHas(t *testing.T) {
	cfg := healthConfig(&api.HealthPlan{Port: 3000, Path: "/health", Retries: 3})
	require.NotNil(t, cfg)
	require.Equal(t, "CMD-SHELL", cfg.Test[0])

	probe := cfg.Test[1]
	require.Contains(t, probe, "curl -fsS -o /dev/null http://127.0.0.1:3000/health")
	require.Contains(t, probe, "wget --spider -q http://127.0.0.1:3000/health")
	require.Contains(t, probe, "nc -z 127.0.0.1 3000",
		"a TCP connection says less than a status, and needs nothing installed")
	require.Contains(t, probe, "/dev/tcp/127.0.0.1/3000", "the rung that needs no package at all")
	require.True(t, strings.HasSuffix(probe, "|| exit 1"), "a failed probe is a failed probe")
}

// A command the spec gives is run as written: whoever wrote it knows their own
// image better than this does.
func TestAGivenHealthCommandIsNotRewritten(t *testing.T) {
	cfg := healthConfig(&api.HealthPlan{Command: []string{"/app/healthcheck"}, Port: 3000, Path: "/health"})
	require.NotNil(t, cfg)
	require.Equal(t, []string{"CMD", "/app/healthcheck"}, cfg.Test)
}
