// This file is in package httpapi rather than httpapi_test, which every other
// test here uses. The decision it covers is one unexported method, and reaching
// it from outside means a full sign-in — an identity adapter, a session store
// and a database — to observe one boolean. The deviation buys a test that
// states the rule directly.
package httpapi

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSecureCookieFollowsTheConfiguredScheme covers O-19.
//
// The row that matters is "behind a TLS-terminating proxy": the request arrives
// over plain HTTP, so r.TLS is nil, and before this the cookie went out without
// Secure even though the browser's connection was encrypted. One plaintext
// request to the hostname then put a live session cookie on the wire.
//
// No requirement covers session cookie transport, so this is not named for one.
// See docs/plan/open-decisions.md, O-19.
func TestSecureCookieFollowsTheConfiguredScheme(t *testing.T) {
	must := func(raw string) *url.URL {
		t.Helper()
		u, err := url.Parse(raw)
		require.NoError(t, err)
		return u
	}

	for name, tc := range map[string]struct {
		external   *url.URL
		requestTLS bool
		want       bool
		why        string
	}{
		"behind a TLS-terminating proxy": {
			external: must("https://pando.example.com"), requestTLS: false, want: true,
			why: "the browser's connection is encrypted even though Pando's is not — this is the bug O-19 named",
		},
		"Pando terminates TLS itself": {
			external: must("https://pando.example.com"), requestTLS: true, want: true,
			why: "https either way",
		},
		"operator states plain http": {
			external: must("http://pando.internal"), requestTLS: false, want: false,
			why: "Secure on a plaintext origin stops sign-in working, and the operator has said it is plaintext",
		},
		"unset, Pando serving TLS": {
			external: nil, requestTLS: true, want: true,
			why: "with nothing configured the request still tells the truth",
		},
		"unset, plain localhost install": {
			external: nil, requestTLS: false, want: false,
			why: "the README's default install is http://localhost, where Secure would break sign-in",
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", nil)
			if tc.requestTLS {
				r.TLS = &tls.ConnectionState{}
			}
			s := &Server{ExternalURL: tc.external}
			require.Equal(t, tc.want, s.secureCookie(r), tc.why)
		})
	}
}

// An http external URL must not be read as "no opinion". Falling through to the
// request there would mark the cookie Secure on an install the operator has
// explicitly described as plaintext, and sign-in would stop working the moment
// Pando was put behind TLS for unrelated reasons.
func TestSecureCookieDoesNotFallBackWhenExternalURLIsSet(t *testing.T) {
	u, err := url.Parse("http://pando.internal")
	require.NoError(t, err)

	r := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", nil)
	r.TLS = &tls.ConnectionState{} // Pando is serving TLS...

	s := &Server{ExternalURL: u} // ...but the operator says the outside is http.
	require.False(t, s.secureCookie(r),
		"a configured scheme is the answer, not a hint the request can override")
}
