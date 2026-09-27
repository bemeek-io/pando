package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
)

// edgeRuntimeFake is a runtime with edge volumes. Methods a backup never calls
// are the embedded interface's, and panic if reached.
type edgeRuntimeFake struct {
	api.RuntimeAdapter
	handles  []api.VolumeHandle
	restored map[string]string
}

func (*edgeRuntimeFake) Kind() string                                     { return "fake" }
func (*edgeRuntimeFake) Category() api.Category                           { return api.CategoryRuntime }
func (*edgeRuntimeFake) Configure(context.Context, json.RawMessage) error { return nil }
func (*edgeRuntimeFake) HealthCheck(context.Context) error                { return nil }
func (*edgeRuntimeFake) Capabilities(context.Context) (api.RuntimeCapabilities, error) {
	return api.RuntimeCapabilities{SupportsEdge: true}, nil
}
func (f *edgeRuntimeFake) EdgeVolumes(context.Context) ([]api.VolumeHandle, error) {
	return f.handles, nil
}
func (f *edgeRuntimeFake) SnapshotVolume(_ context.Context, h api.VolumeHandle, dst io.Writer) error {
	_, err := io.WriteString(dst, "certificates of "+h.Handle)
	return err
}
func (f *edgeRuntimeFake) RestoreVolume(_ context.Context, h api.VolumeHandle, src io.Reader) error {
	body, err := io.ReadAll(src)
	f.restored[h.Handle] = string(body)
	return err
}

func withEdgeRuntime(t *testing.T, rt api.RuntimeAdapter) *Service {
	t.Helper()
	reg := api.NewRegistry()
	require.NoError(t, reg.Register("rt_docker", rt))
	require.NoError(t, reg.SetDefault(api.CategoryRuntime, "rt_docker"))
	return &Service{Registry: reg, WorkDir: t.TempDir()}
}

// TestR212_TheEdgesCertificatesAreInTheBundle asserts R-212 for the edge
// Pando runs (R-174): a rebuilt host gets its certificates back rather than
// asking Let's Encrypt for every one again.
func TestR212_TheEdgesCertificatesAreInTheBundle(t *testing.T) {
	rt := &edgeRuntimeFake{handles: []api.VolumeHandle{{Handle: "pando-edge-rte_traefik-acme"}}, restored: map[string]string{}}
	s := withEdgeRuntime(t, rt)

	var buf bytes.Buffer
	w := NewWriter(&buf, "dr_bundle", "test", 1)
	n, err := s.addEdgeVolumes(context.Background(), w)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	m, err := w.Finish()
	require.NoError(t, err)
	require.True(t, listed(m.Entries, "edges/pando-edge-rte_traefik-acme.tar"))

	require.NoError(t, s.restoreEdgeVolume(context.Background(), "edges/pando-edge-rte_traefik-acme.tar",
		strings.NewReader("certificates of pando-edge-rte_traefik-acme")))
	require.Equal(t, "certificates of pando-edge-rte_traefik-acme", rt.restored["pando-edge-rte_traefik-acme"])
}

// noEdges is a runtime that runs no edges.
type noEdges struct{ edgeRuntimeFake }

func (*noEdges) Capabilities(context.Context) (api.RuntimeCapabilities, error) {
	return api.RuntimeCapabilities{}, nil
}

type brokenEdges struct {
	edgeRuntimeFake
	listErr, snapErr error
}

func (b *brokenEdges) EdgeVolumes(ctx context.Context) ([]api.VolumeHandle, error) {
	if b.listErr != nil {
		return nil, b.listErr
	}
	return b.edgeRuntimeFake.EdgeVolumes(ctx)
}
func (b *brokenEdges) SnapshotVolume(ctx context.Context, h api.VolumeHandle, dst io.Writer) error {
	if b.snapErr != nil {
		return b.snapErr
	}
	return b.edgeRuntimeFake.SnapshotVolume(ctx, h, dst)
}

// TestABundleFromARuntimeWithoutEdgesHasNoEdgeEntries: nothing to capture is
// not an error, and nothing to restore into is not either — certificates are
// re-issued, which costs time rather than data.
func TestABundleFromARuntimeWithoutEdgesHasNoEdgeEntries(t *testing.T) {
	ctx := context.Background()
	for name, s := range map[string]*Service{
		"no registry":   {},
		"no default":    {Registry: api.NewRegistry()},
		"runs no edges": withEdgeRuntime(t, &noEdges{}),
	} {
		n, err := s.addEdgeVolumes(ctx, NewWriter(io.Discard, "dr_bundle", "test", 1))
		require.NoError(t, err, name)
		require.Zero(t, n, name)
		require.NoError(t, s.restoreEdgeVolume(ctx, "edges/pando-edge-x-acme.tar", strings.NewReader("x")), name)
	}
}

func TestAnEdgeVolumeThatCannotBeReadStopsTheBundle(t *testing.T) {
	ctx := context.Background()
	s := withEdgeRuntime(t, &brokenEdges{listErr: errors.New("daemon down")})
	_, err := s.addEdgeVolumes(ctx, NewWriter(io.Discard, "dr_bundle", "test", 1))
	require.Error(t, err)

	s = withEdgeRuntime(t, &brokenEdges{
		edgeRuntimeFake: edgeRuntimeFake{handles: []api.VolumeHandle{{Handle: "pando-edge-x-acme"}}},
		snapErr:         errors.New("tar failed"),
	})
	_, err = s.addEdgeVolumes(ctx, NewWriter(io.Discard, "dr_bundle", "test", 1))
	require.ErrorContains(t, err, "tar failed", "a bundle missing what it promises is not written as if whole")
}

// TestAnEdgeHandleThatCouldEscapeIsNeitherWrittenNorRestored: the handle
// becomes a path in the bundle.
func TestAnEdgeHandleThatCouldEscapeIsNeitherWrittenNorRestored(t *testing.T) {
	rt := &edgeRuntimeFake{handles: []api.VolumeHandle{{Handle: "../../etc"}}, restored: map[string]string{}}
	s := withEdgeRuntime(t, rt)

	_, err := s.addEdgeVolumes(context.Background(), NewWriter(io.Discard, "dr_bundle", "test", 1))
	require.Error(t, err)

	require.Error(t, s.restoreEdgeVolume(context.Background(), "edges/../../etc.tar", strings.NewReader("x")))
	require.Empty(t, rt.restored)
}
