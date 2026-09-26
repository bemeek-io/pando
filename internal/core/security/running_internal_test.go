package security

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestR310_AScanIsRunningUntilTheLastOfItsScansEnds asserts the running state
// the API reports: an app with two scans under way — a deploy's and somebody's
// "Scan now" — is scanning until both have ended, and since the first began.
func TestR310_AScanIsRunningUntilTheLastOfItsScansEnds(t *testing.T) {
	s := &Service{}

	_, running := s.Scanning("app_1")
	require.False(t, running, "nothing has started")

	first := s.begin("app_1")
	since, running := s.Scanning("app_1")
	require.True(t, running)

	second := s.begin("app_1")
	again, running := s.Scanning("app_1")
	require.True(t, running)
	require.Equal(t, since, again, "since the first scan began, not the newest")

	_, running = s.Scanning("app_2")
	require.False(t, running, "one app's scan is not another's")

	first()
	first() // ending the same scan twice does not end the other
	_, running = s.Scanning("app_1")
	require.True(t, running, "the second scan is still running")

	second()
	_, running = s.Scanning("app_1")
	require.False(t, running)
}
