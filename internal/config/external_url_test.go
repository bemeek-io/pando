package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/config"
)

// TestExternalURLIsCheckedAtStartup covers O-19's configuration half.
//
// The setting decides whether session cookies are marked Secure, and the
// symptom of getting it wrong is an absent cookie attribute that nobody looks
// at until it matters. So a malformed value has to stop the process at startup
// rather than be discovered at the first sign-in.
//
// No requirement covers session cookie transport, which is why this is not
// named for one. See docs/plan/open-decisions.md, O-19.
func TestExternalURLIsCheckedAtStartup(t *testing.T) {
	valid := map[string]string{
		"https":                "https://pando.example.com",
		"http":                 "http://pando.example.com",
		"with a port":          "https://pando.example.com:8443",
		"with a path":          "https://example.com/pando",
		"surrounded by spaces": "  https://pando.example.com  ",
	}
	for name, in := range valid {
		t.Run("accepts "+name, func(t *testing.T) {
			u, err := config.Server{ExternalURL: in}.External()
			require.NoError(t, err)
			require.NotNil(t, u)
		})
	}

	t.Run("unset is not an error", func(t *testing.T) {
		u, err := config.Server{}.External()
		require.NoError(t, err)
		require.Nil(t, u, "unset must fall back to the request, not to a zero URL")
	})

	rejected := map[string]string{
		"no scheme":                     "pando.example.com",
		"a scheme Pando does not serve": "ftp://pando.example.com",
		"no host":                       "https://",
		"a bare path":                   "/pando",
	}
	for name, in := range rejected {
		t.Run("rejects "+name, func(t *testing.T) {
			_, err := config.Server{ExternalURL: in}.External()
			require.Error(t, err)
			require.Contains(t, err.Error(), "PANDO_SERVER_EXTERNAL_URL",
				"the message must name the setting, so it can be acted on without reading the source")
		})
	}
}
