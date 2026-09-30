package httpapi

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/trypando/pando/internal/core/clock"
	"github.com/trypando/pando/internal/core/detection"
	"github.com/trypando/pando/internal/core/state"
	"github.com/trypando/pando/internal/errs"
)

// `wait` reads as seconds or as a duration, is held to detection.MaxWait, and
// what cannot be read is refused saying what can (R-105).
func TestWaitParamReadsSecondsOrADuration(t *testing.T) {
	for raw, want := range map[string]time.Duration{
		"":     0,
		"30":   30 * time.Second,
		"30s":  30 * time.Second,
		"1m":   time.Minute,
		"3600": detection.MaxWait,
	} {
		got, err := waitParam(raw)
		require.NoError(t, err, raw)
		require.Equal(t, want, got, raw)
	}

	for raw, says := range map[string]string{
		"soon": "not a length of time Pando can read",
		"-5":   "cannot be negative",
		"-5s":  "cannot be negative",
	} {
		_, err := waitParam(raw)
		require.Equal(t, errs.ValidInvalid, errs.CodeOf(err), raw)
		require.ErrorContains(t, err, says, raw)
	}
}

// While detection runs the response names the stage beside the status and
// says how long it has been going by the server's clock (issue #80); once it
// has finished, neither.
func TestTheDetectionSaysItsStageAndHowLongItHasRun(t *testing.T) {
	c := clock.NewFake(time.Time{})
	s := &Server{Clock: c}
	started := c.Now().Add(-80 * time.Second)

	running := s.detectionResponse(state.Detection{
		Status: state.DetectionRunning, Body: []byte(`{"stage":"trying"}`), StartedAt: started,
	})
	require.Equal(t, "trying", running["stage"])
	require.Equal(t, 80, running["elapsed_seconds"])

	// Before the first stage is recorded there is no stage to name.
	fresh := s.detectionResponse(state.Detection{Status: state.DetectionRunning, Body: []byte(`{}`), StartedAt: started})
	require.NotContains(t, fresh, "stage")
	require.Contains(t, fresh, "elapsed_seconds")

	done := s.detectionResponse(state.Detection{Status: state.DetectionReady, Body: []byte(`{}`), StartedAt: started})
	require.NotContains(t, done, "stage")
	require.NotContains(t, done, "elapsed_seconds")
}

func TestTheServerClockDefaultsToTheSystemClock(t *testing.T) {
	require.Equal(t, clock.System{}, (&Server{}).clock())
	fake := clock.NewFake(time.Time{})
	require.Equal(t, fake, (&Server{Clock: fake}).clock())
}
