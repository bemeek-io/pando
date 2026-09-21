package cli

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAuditTimesTakeATimeOrADurationAgo(t *testing.T) {
	got, err := whenFlag("2026-09-21T09:00:00+02:00")
	require.NoError(t, err)
	require.Equal(t, "2026-09-21T07:00:00Z", got, "a time is passed on in UTC")

	got, err = whenFlag("24h")
	require.NoError(t, err)
	at, err := time.Parse(time.RFC3339, got)
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().Add(-24*time.Hour), at, time.Minute)

	_, err = whenFlag("yesterday")
	require.Error(t, err)
}
