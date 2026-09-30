package deploy

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/trypando/pando/internal/adapter/api"
	"github.com/trypando/pando/internal/core/audit"
	"github.com/trypando/pando/internal/core/security"
	"github.com/trypando/pando/internal/core/spec"
	"github.com/trypando/pando/internal/core/state"
	"github.com/trypando/pando/internal/errs"
)

// fakeSecurity answers a deploy's questions about scans as a test needs.
type fakeSecurity struct {
	scannedCommit string
	scannedAt     time.Time
	findings      []api.Finding
	standing      security.Standing
	scans         []api.ScanRequest
	reusedFor     []string
}

func (f *fakeSecurity) Scan(_ context.Context, req api.ScanRequest, _ audit.Event) (state.Scan, error) {
	f.scans = append(f.scans, req)
	score := 90
	return state.Scan{Score: &score}, nil
}

func (f *fakeSecurity) Reuse(_ context.Context, _, specID, source string) (state.Scan, bool, error) {
	if source != "" && source == f.scannedCommit {
		f.reusedFor = append(f.reusedFor, specID)
		return state.Scan{SpecID: specID, Commit: source, RanAt: f.scannedAt, Findings: f.findings}, true, nil
	}
	return state.Scan{}, false, nil
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
	require.Equal(t, []string{"spec_1"}, sec.reusedFor, "and attached to the revision being deployed")
	require.Contains(t, log.String(), "Using the security scan of 3f9a2c1")

	log.Reset()
	require.NoError(t, r.scan(context.Background(), dep, nil, "img", "/src", "def4560000", &log))
	require.Len(t, sec.scans, 1, "a commit nobody scanned is scanned")
	require.Equal(t, "def4560000", sec.scans[0].Commit)
	require.Contains(t, log.String(), "Scanning for known vulnerabilities")
}

// TestR314_AReusedScanStillFacesTheThreshold asserts R-314 holds when the scan
// is reused: skipping the scan never skips the decision, and the refusal names
// the findings that cost the most, as it does after a scan that ran (R-105).
func TestR314_AReusedScanStillFacesTheThreshold(t *testing.T) {
	score := 20
	sec := &fakeSecurity{
		scannedCommit: "3f9a2c1d0000",
		findings:      []api.Finding{{ID: "CVE-2026-0001", Severity: api.SeverityCritical, Title: "Remote code execution"}},
		standing:      security.Standing{Verdict: security.VerdictInsecure, Score: &score, Threshold: 60},
	}
	r := &Runner{security: sec}

	var log strings.Builder
	err := r.scan(context.Background(), state.Deployment{AppID: "app_x"}, nil, "img", "/src", "3f9a2c1d0000", &log)
	require.Equal(t, errs.PlanSecurityBelowThreshold, errs.CodeOf(err))
	require.Empty(t, sec.scans)
	require.Contains(t, errs.As(err).Details, "findings", "the reused scan's findings travel with the refusal")
}

// TestR312_UnchangedImageIsNotRescanned asserts R-312 as amended by issue #84:
// a scan runs when what the app runs changes, not on every deploy. Redeploying
// an upload whose archive is unchanged — as a new revision, which every
// `pando deploy .` is — uses the scan of that archive and says so in words the
// person who uploaded it recognizes.
func TestR312_UnchangedImageIsNotRescanned(t *testing.T) {
	archive := "sha256:" + strings.Repeat("ab", 32)
	sec := &fakeSecurity{
		scannedCommit: archive,
		scannedAt:     time.Date(2026, 9, 29, 8, 0, 0, 0, time.UTC),
		standing:      security.Standing{Verdict: security.VerdictOK},
	}
	r := &Runner{security: sec}
	upload := &spec.AppSpec{Source: spec.Source{Type: spec.SourceUpload, UploadID: "app_x"}}

	for _, specID := range []string{"spec_2", "spec_3"} {
		var log strings.Builder
		dep := state.Deployment{AppID: "app_x", SpecID: specID}
		require.NoError(t, r.scan(context.Background(), dep, upload, "img", "/src", archive, &log))
		require.Contains(t, log.String(),
			"=> Using the security scan of the uploaded source from 2026-09-29T08:00:00Z (source unchanged)")
		require.NotContains(t, log.String(), "Scanning for known vulnerabilities")
	}
	require.Empty(t, sec.scans, "neither deploy ran the scanner")
	require.Equal(t, []string{"spec_2", "spec_3"}, sec.reusedFor, "each revision gets the scan that describes it")

	var log strings.Builder
	dep := state.Deployment{AppID: "app_x", SpecID: "spec_4"}
	require.NoError(t, r.scan(context.Background(), dep, upload, "img", "/src", "sha256:"+strings.Repeat("cd", 32), &log))
	require.Len(t, sec.scans, 1, "a new archive is scanned")
	require.Contains(t, log.String(), "Scanning for known vulnerabilities")
}
