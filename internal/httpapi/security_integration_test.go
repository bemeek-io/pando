//go:build integration

package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestR310_TheScoreIsReadableByAnyoneWhoCanSeeTheApp asserts the two verbs this
// feature is split across.
//
// Reading the score is `app.view`: it is a fact about an app somebody can
// already see, and the person who has to fix a finding is usually not the
// person who can deploy. Asking for a new scan is `app.deploy`, because the
// score decides whether the next deploy is allowed — which makes it a write
// however much it looks like a refresh.
func TestR310_TheScoreIsReadableByAnyoneWhoCanSeeTheApp(t *testing.T) {
	i := newInstall(t)
	owner := i.admin()
	stranger := i.user("stranger")

	app := i.createApp(owner, "notes")

	got := i.do(owner, http.MethodGet, "/apps/"+app+"/security", nil)
	require.Equal(t, http.StatusOK, got.Code, got.String())

	var report struct {
		Standing struct {
			Verdict   string `json:"verdict"`
			Score     *int   `json:"score"`
			Threshold int    `json:"threshold"`
		} `json:"standing"`
		Scanner string `json:"scanner"`
	}
	got.JSON(t, &report)

	// A fresh app has never been scanned, and with no threshold set that is not
	// a problem: the verdict is ok and the score is absent rather than zero.
	require.Nil(t, report.Standing.Score, "no score is not a score of zero (R-318)")
	require.Equal(t, "ok", report.Standing.Verdict)
	require.Equal(t, 0, report.Standing.Threshold)

	// Somebody with no grant on the app sees nothing, the same as every other
	// per-app endpoint.
	denied := i.do(stranger, http.MethodGet, "/apps/"+app+"/security", nil)
	require.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, denied.Code, denied.String())

	deniedScan := i.do(stranger, http.MethodPost, "/apps/"+app+"/security/scan", nil)
	require.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, deniedScan.Code, deniedScan.String())
}
