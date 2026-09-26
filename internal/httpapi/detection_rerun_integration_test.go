//go:build integration

package httpapi_test

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/detect"
	"github.com/bemeek-io/pando/internal/errs"
)

// fakeDetector stands in for the detection runner, so the endpoints can be
// asserted without cloning anything.
type fakeDetector struct {
	checkErr  error
	detectErr error
	detected  atomic.Int32
}

func (f *fakeDetector) Check(context.Context, string) error { return f.checkErr }

func (f *fakeDetector) Detect(context.Context, string) (state.Detection, error) {
	f.detected.Add(1)
	return state.Detection{}, f.detectErr
}

func (f *fakeDetector) Revise(context.Context, string, string) (state.Detection, error) {
	return state.Detection{}, f.detectErr
}

// TestR092_ARefusedRerunWritesNothing asserts R-092. A source the allowlist no
// longer permits is refused before anything is recorded, so the previous
// outcome is not replaced by a detection that was never going to run.
func TestR092_ARefusedRerunWritesNothing(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	appID := i.createApp(admin, "notes")

	detector := &fakeDetector{checkErr: errs.New(errs.PolicySourceNotAllowed,
		"This install only deploys from github.com/acme, and this app's repository is elsewhere.")}
	i.Server.Detector = detector

	got := i.do(admin, http.MethodPost, "/apps/"+appID+"/detection/rerun", map[string]any{})
	require.Equal(t, errs.PolicySourceNotAllowed, errs.Code(got.ErrorCode()), got.String())
	require.Zero(t, detector.detected.Load(), "nothing was started")

	_, err := state.NewDetections(i.db).Get(context.Background(), appID)
	require.Equal(t, errs.NotFound, errs.CodeOf(err), "and nothing was recorded")
}

// TestR022_ARerunRunsInTheBackgroundAndRecordsItsFailure asserts R-022. The
// request returns at once with the detection marked running, and a failure the
// detector returned without recording is recorded for it, so the detection is
// not left running for good.
func TestR022_ARerunRunsInTheBackgroundAndRecordsItsFailure(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	appID := i.createApp(admin, "notes")

	detector := &fakeDetector{detectErr: errs.New(errs.ValidInvalid, "The repository could not be cloned.")}
	i.Server.Detector = detector

	got := i.do(admin, http.MethodPost, "/apps/"+appID+"/detection/rerun", map[string]any{})
	require.Equal(t, http.StatusAccepted, got.Code, got.String())
	var started struct {
		Status string `json:"status"`
	}
	got.JSON(t, &started)
	require.Equal(t, state.DetectionRunning, started.Status)

	var d state.Detection
	require.Eventually(t, func() bool {
		var err error
		d, err = state.NewDetections(i.db).Get(context.Background(), appID)
		return err == nil && d.Status == state.DetectionFailed
	}, 10*time.Second, 20*time.Millisecond, "the failure is recorded, not left running")
	require.Contains(t, string(d.Body), "The repository could not be cloned.")
	require.Equal(t, int32(1), detector.detected.Load())
}

// TestR338_AnAnswerThatCannotBecomeASpecIsRefusedWhenGiven asserts R-338's
// counterpart for a person's answer. A build_method answer naming no reading a
// detector made is refused at once, rather than recorded and refused at accept
// as "This app has no workloads" (issue #55).
func TestR338_AnAnswerThatCannotBecomeASpecIsRefusedWhenGiven(t *testing.T) {
	ctx := context.Background()
	i := newInstall(t)
	admin := i.admin()
	appID := i.createApp(admin, "notes")

	workloads := []spec.Workload{{Name: "web", Primary: true, Exposed: true}}
	proposal := detect.Proposal{
		Status: detect.StatusNeedsAnswers,
		Winner: detect.Candidate{Detector: "dockerfile", Strategy: spec.BuildDockerfile,
			Draft: detect.Draft{Workloads: workloads}},
		DraftSpec: spec.AppSpec{SchemaVersion: spec.SchemaVersion, AppID: appID, Workloads: workloads},
		Questions: []detect.Question{{Key: detect.KeyBuildMethod, Kind: "choice",
			Prompt: "Pando could not tell how this app is built."}},
	}
	require.NoError(t, state.NewDetections(i.db).Save(ctx, appID, detect.StatusNeedsAnswers, proposal, ""))

	got := i.do(admin, http.MethodPost, "/apps/"+appID+"/detection/answers", map[string]any{
		"answers": map[string]string{detect.KeyBuildMethod: "serve the repository root with php -S"},
	})
	require.Equal(t, http.StatusBadRequest, got.Code, got.String())
	require.Equal(t, string(errs.ValidInvalid), got.ErrorCode())
	require.Contains(t, got.String(), "is not one of the ways Pando found to build this app")

	stored, err := state.NewDetections(i.db).Get(ctx, appID)
	require.NoError(t, err)
	require.Empty(t, stored.Answers, "the refused answer is not recorded")
}

// Deleting an app asks for its bundle to be torn down at once, when the
// install can, rather than on the next reconcile.
func TestDeletingAnAppAsksForItsTeardownAtOnce(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	appID := i.createApp(admin, "notes")

	var teardowns atomic.Int32
	i.Server.TeardownNow = func() { teardowns.Add(1) }

	got := i.do(admin, http.MethodDelete, "/apps/"+appID, nil)
	require.Less(t, got.Code, 300, got.String())
	require.Equal(t, int32(1), teardowns.Load())
}
