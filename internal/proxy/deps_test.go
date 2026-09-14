package proxy

import (
	"context"
	"encoding/binary"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/errs"
)

func app() state.App { return state.App{ID: "app_01HQ8", Slug: "notes"} }

func specWith(workloads ...spec.Workload) *spec.AppSpec {
	return &spec.AppSpec{Workloads: workloads}
}

// The address comes from the workload's name on the bundle network rather than
// being assembled from a container ID or an IP: how a workload is addressed is
// the provider vocabulary core must not learn (R-251), and a name survives the
// app being recreated.
func TestR251_ThePrimaryAddressIsTheWorkloadNameAndItsPort(t *testing.T) {
	u := NewRuntimeUpstreams(nil)

	got, err := u.PrimaryAddress(context.Background(), app(), specWith(spec.Workload{
		Name: "web", Primary: true,
		Ports: []spec.Port{{Number: 3000, Protocol: "http"}},
	}))

	require.NoError(t, err)
	require.Equal(t, "http://pando-app_01HQ8-web:3000", got)
	require.NotContains(t, got, "127.0.0.1")
}

func TestTheHTTPPortIsPreferredOverWhateverComesFirst(t *testing.T) {
	u := NewRuntimeUpstreams(nil)

	got, err := u.PrimaryAddress(context.Background(), app(), specWith(spec.Workload{
		Name: "web", Primary: true,
		Ports: []spec.Port{
			{Number: 9000, Protocol: "tcp"},
			{Number: 3000, Protocol: "http"},
		},
	}))
	require.NoError(t, err)
	require.Equal(t, "http://pando-app_01HQ8-web:3000", got)

	// A port with no protocol stated is treated as HTTP, which is what a spec
	// that only says "3000" means.
	got, err = u.PrimaryAddress(context.Background(), app(), specWith(spec.Workload{
		Name: "web", Primary: true, Ports: []spec.Port{{Number: 3000}},
	}))
	require.NoError(t, err)
	require.Equal(t, "http://pando-app_01HQ8-web:3000", got)

	// Nothing declares HTTP, so the first port is the best guess available.
	got, err = u.PrimaryAddress(context.Background(), app(), specWith(spec.Workload{
		Name: "web", Primary: true, Ports: []spec.Port{{Number: 9000, Protocol: "tcp"}},
	}))
	require.NoError(t, err)
	require.Equal(t, "http://pando-app_01HQ8-web:9000", got)
}

func TestAnAppWithNothingToProxyToSaysWhichPartIsMissing(t *testing.T) {
	u := NewRuntimeUpstreams(nil)
	ctx := context.Background()

	_, err := u.PrimaryAddress(ctx, app(), nil)
	require.Equal(t, errs.StateInvalid, errs.CodeOf(err))
	require.Contains(t, errs.As(err).Message, "no pinned spec")

	_, err = u.PrimaryAddress(ctx, app(), specWith(spec.Workload{Name: "worker"}))
	require.Equal(t, errs.ValidPrimaryWorkload, errs.CodeOf(err))

	_, err = u.PrimaryAddress(ctx, app(), specWith(spec.Workload{Name: "web", Primary: true}))
	require.Equal(t, errs.StateInvalid, errs.CodeOf(err))
	require.NotEmpty(t, errs.As(err).Remedy, "R-105: it says what to do about it")
}

func TestAWorkloadIsAddressedByNameInsideItsBundle(t *testing.T) {
	require.Equal(t, "pando-app_01HQ8-web", workloadHost("app_01HQ8", "web"))
	require.NotEqual(t, workloadHost("app_01HQ8", "web"), workloadHost("app_01HQ9", "web"))
	require.NotEqual(t, workloadHost("app_01HQ8", "web"), workloadHost("app_01HQ8", "worker"))
}

// R-023 says there is no bypass, and a counter every request increments —
// allowed or denied, authenticated or anonymous — is how that claim is checked
// rather than asserted.
func TestR023_EveryRequestIsCountedWhateverItsOutcome(t *testing.T) {
	c := NewCounters()

	c.Request("app_01HQ8", authz.KindUser, true)
	c.Request("app_01HQ8", authz.KindUser, false)
	c.Request("app_01HQ8", authz.KindAnonymous, true)
	c.Request("app_01HQ8", authz.KindAnonymous, false)
	c.Request("app_01HQ8", authz.KindToken, true)

	total, allowed, denied, anonymous := c.Snapshot("app_01HQ8")
	require.EqualValues(t, 5, total)
	require.EqualValues(t, 3, allowed)
	require.EqualValues(t, 2, denied)
	require.EqualValues(t, 2, anonymous)
	require.EqualValues(t, total, allowed+denied, "every request lands in exactly one of the two")
}

func TestCountsAreKeptPerApp(t *testing.T) {
	c := NewCounters()
	c.Request("app_01HQ8", authz.KindUser, true)
	c.Request("app_01HQ9", authz.KindUser, false)

	total, allowed, _, _ := c.Snapshot("app_01HQ8")
	require.EqualValues(t, 1, total)
	require.EqualValues(t, 1, allowed)

	total, _, denied, _ := c.Snapshot("app_01HQ9")
	require.EqualValues(t, 1, total)
	require.EqualValues(t, 1, denied)

	total, allowed, denied, anonymous := c.Snapshot("app_never_seen")
	require.Zero(t, total+allowed+denied+anonymous, "an app nobody has asked for counts zero, not nothing")
}

func TestCountingIsSafeUnderConcurrentRequests(t *testing.T) {
	c := NewCounters()

	var wg sync.WaitGroup
	for i := range 200 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Request("app_01HQ8", authz.KindUser, i%2 == 0)
			c.Request("app_"+string(rune('a'+i%5)), authz.KindAnonymous, true)
		}()
	}
	wg.Wait()

	total, allowed, denied, _ := c.Snapshot("app_01HQ8")
	require.EqualValues(t, 200, total)
	require.EqualValues(t, 100, allowed)
	require.EqualValues(t, 100, denied)
}

// A policy-violation close frame rather than an abrupt reset, so a client can
// tell revocation from a network fault and does not reconnect in a loop.
func TestTheCloseFrameSaysPolicyViolation(t *testing.T) {
	frame := closeFrame(closePolicyViolation, "access revoked")

	require.Equal(t, byte(0x88), frame[0], "FIN plus opcode 8")
	require.Equal(t, byte(len(frame)-2), frame[1], "the payload length, with no extended length")

	require.EqualValues(t, 1008, binary.BigEndian.Uint16(frame[2:4]))
	require.Equal(t, "access revoked", string(frame[4:]))
}

// Close payloads are capped at 125 bytes, which is also the boundary below
// which the length fits in the header's 7 bits.
func TestALongCloseReasonIsTruncatedToFitTheHeader(t *testing.T) {
	long := make([]byte, 400)
	for i := range long {
		long[i] = 'x'
	}

	frame := closeFrame(closePolicyViolation, string(long))
	require.Len(t, frame, 2+125)
	require.Equal(t, byte(125), frame[1])
	require.EqualValues(t, 1008, binary.BigEndian.Uint16(frame[2:4]))
}
