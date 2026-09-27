package edge

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/clock"
	"github.com/bemeek-io/pando/internal/errs"
)

// failingRuntime fails in whichever step a test names.
type failingRuntime struct {
	fakeRuntime
	capsErr, observeErr, edgesErr, removeErr error
}

func (f *failingRuntime) Capabilities(ctx context.Context) (api.RuntimeCapabilities, error) {
	if f.capsErr != nil {
		return api.RuntimeCapabilities{}, f.capsErr
	}
	return f.fakeRuntime.Capabilities(ctx)
}
func (f *failingRuntime) ObserveEdge(ctx context.Context, name string) (api.EdgeState, error) {
	if f.observeErr != nil {
		return api.EdgeState{}, f.observeErr
	}
	return f.fakeRuntime.ObserveEdge(ctx, name)
}
func (f *failingRuntime) Edges(ctx context.Context) ([]string, error) {
	if f.edgesErr != nil {
		return nil, f.edgesErr
	}
	return f.fakeRuntime.Edges(ctx)
}
func (f *failingRuntime) RemoveEdge(ctx context.Context, name string) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	return f.fakeRuntime.RemoveEdge(ctx, name)
}

type brokenRouting struct{ fakeRouting }

func (*brokenRouting) Edge(context.Context, api.EdgeRequest) (api.EdgePlan, bool, error) {
	return api.EdgePlan{}, false, errors.New("config directory is read-only")
}

func registry(t *testing.T, rt api.RuntimeAdapter, routes map[string]api.RoutingAdapter) *api.Registry {
	t.Helper()
	reg := api.NewRegistry()
	if rt != nil {
		require.NoError(t, reg.Register("rt_docker", rt))
	}
	for ref, r := range routes {
		require.NoError(t, reg.Register(ref, r))
	}
	return reg
}

func TestWithoutARuntimeNothingCanRun(t *testing.T) {
	s := &Service{Registry: registry(t, nil, nil)}
	err := s.Reconcile(context.Background())
	require.Equal(t, errs.AdapterUnavailable, errs.CodeOf(err))
}

func TestTheOnlyRuntimeIsUsedWhenNoneIsTheDefault(t *testing.T) {
	rt := &fakeRuntime{supports: true, edges: map[string]api.EdgePlan{}}
	s := &Service{Registry: registry(t, rt, map[string]api.RoutingAdapter{"rte_t": &fakeRouting{needs: true}}),
		ProxyUpstream: "http://pando:8080"}
	require.NoError(t, s.Reconcile(context.Background()))
	require.Contains(t, rt.edges, "rte_t")
}

func TestEachFailingStepIsReportedAndTheRestGoOn(t *testing.T) {
	ctx := context.Background()

	caps := &failingRuntime{fakeRuntime: fakeRuntime{edges: map[string]api.EdgePlan{}}, capsErr: errors.New("daemon down")}
	s := &Service{Registry: registry(t, caps, nil)}
	require.Error(t, s.Reconcile(ctx))

	broken := &failingRuntime{fakeRuntime: fakeRuntime{supports: true, edges: map[string]api.EdgePlan{}}}
	s = &Service{Registry: registry(t, broken, map[string]api.RoutingAdapter{
		"rte_bad": &brokenRouting{}, "rte_ok": &fakeRouting{needs: true},
	}), ProxyUpstream: "http://pando:8080"}
	require.Error(t, s.Reconcile(ctx))
	st, _ := s.Status("rte_bad")
	require.Equal(t, "Pando could not start the edge: config directory is read-only", st.Message,
		"an error without the envelope still reads as a sentence")
	require.Contains(t, broken.edges, "rte_ok", "one adapter's failure does not stop another's edge")

	broken.observeErr = errs.New(errs.AdapterUnavailable, "Docker is not responding.")
	require.Error(t, s.Reconcile(ctx))
	st, _ = s.Status("rte_ok")
	require.Equal(t, "Docker is not responding.", st.Message)

	broken.observeErr = nil
	broken.edgesErr = errors.New("cannot list")
	require.Error(t, s.Reconcile(ctx))

	broken.edgesErr = nil
	broken.edges["rte_stale"] = api.EdgePlan{}
	broken.removeErr = errors.New("busy")
	require.Error(t, s.Reconcile(ctx), "rte_bad still fails")
	require.Contains(t, broken.edges, "rte_stale", "a removal that failed is tried again next time")
}

func TestRunReconcilesAtOnceAndOnEveryTick(t *testing.T) {
	rt := &fakeRuntime{supports: true, edges: map[string]api.EdgePlan{}}
	fake := clock.NewFake(time.Time{})
	s := &Service{Registry: registry(t, rt, map[string]api.RoutingAdapter{"rte_t": &fakeRouting{needs: true}}),
		ProxyUpstream: "http://pando:8080", Clock: fake}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx, 0) // zero means once a minute
		close(done)
	}()

	// Watched through Status, which is locked, rather than through the fake
	// runtime's map, which Run writes on its own goroutine.
	var first time.Time
	require.Eventually(t, func() bool {
		st, ok := s.Status("rte_t")
		first = st.CheckedAt
		return ok
	}, time.Second, time.Millisecond)

	// Advance until the tick fires: Run may not be waiting yet.
	require.Eventually(t, func() bool {
		fake.Advance(time.Minute)
		st, _ := s.Status("rte_t")
		return st.CheckedAt.After(first)
	}, time.Second, 5*time.Millisecond, "the next tick reconciles again")

	cancel()
	<-done
}

func TestTheDefaultsNeedNothingSet(t *testing.T) {
	s := &Service{}
	require.NotNil(t, s.logger())
	require.IsType(t, clock.System{}, s.clock())
	require.False(t, s.now().IsZero())
	require.Equal(t, "", hostOf("://nope"))
	_, ok := s.Status("anything")
	require.False(t, ok)
}
