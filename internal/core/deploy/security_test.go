package deploy

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/audit"
	"github.com/bemeek-io/pando/internal/core/security"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/errs"
)

// fakeSecurity answers a deploy's questions about scans as a test needs.
type fakeSecurity struct {
	scannedCommit string
	scannedAt     time.Time
	standing      security.Standing
	scans         []api.ScanRequest
}

func (f *fakeSecurity) Scan(_ context.Context, req api.ScanRequest, _ audit.Event) (state.Scan, error) {
	f.scans = append(f.scans, req)
	score := 90
	return state.Scan{Score: &score}, nil
}

func (f *fakeSecurity) ScannedAt(_ context.Context, _, commit string) (time.Time, bool, error) {
	if commit != "" && commit == f.scannedCommit {
		return f.scannedAt, true, nil
	}
	return time.Time{}, false, nil
}

func (f *fakeSecurity) Allows(context.Context, string, string) (security.Standing, error) {
	return f.standing, nil
}

func (f *fakeSecurity) Configured() (string, bool) { return "scn_test", true }

// TestR312_ADeployOfAScannedCommitDoesNotScanItAgain asserts design 09 §4.1:
// the source is scanned once, not once per deploy. A deploy of a commit with a
// scan uses it and says so; a commit nobody scanned is scanned, with its
// commit recorded so the next deploy of it does not.
func TestR312_ADeployOfAScannedCommitDoesNotScanItAgain(t *testing.T) {
	sec := &fakeSecurity{
		scannedCommit: "3f9a2c1d0000",
		scannedAt:     time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC),
		standing:      security.Standing{Verdict: security.VerdictOK},
	}
	r := &Runner{security: sec}
	dep := state.Deployment{AppID: "app_x", SpecID: "spec_1"}

	var log strings.Builder
	require.NoError(t, r.scan(context.Background(), dep, nil, "img", "/src", "3f9a2c1d0000", &log))
	require.Empty(t, sec.scans, "the scan of that commit is reused")
	require.Contains(t, log.String(), "Using the security scan of 3f9a2c1")

	log.Reset()
	require.NoError(t, r.scan(context.Background(), dep, nil, "img", "/src", "def4560000", &log))
	require.Len(t, sec.scans, 1, "a commit nobody scanned is scanned")
	require.Equal(t, "def4560000", sec.scans[0].Commit)
	require.Contains(t, log.String(), "Scanning for known vulnerabilities")
}

// TestR314_AReusedScanStillFacesTheThreshold asserts R-314 holds when the scan
// is reused: skipping the scan never skips the decision.
func TestR314_AReusedScanStillFacesTheThreshold(t *testing.T) {
	score := 20
	sec := &fakeSecurity{
		scannedCommit: "3f9a2c1d0000",
		standing:      security.Standing{Verdict: security.VerdictInsecure, Score: &score, Threshold: 60},
	}
	r := &Runner{security: sec}

	var log strings.Builder
	err := r.scan(context.Background(), state.Deployment{AppID: "app_x"}, nil, "img", "/src", "3f9a2c1d0000", &log)
	require.Equal(t, errs.PlanSecurityBelowThreshold, errs.CodeOf(err))
	require.Empty(t, sec.scans)
}
