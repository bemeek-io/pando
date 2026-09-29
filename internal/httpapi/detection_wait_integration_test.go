//go:build integration

package httpapi_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/trypando/pando/internal/core/state"
	"github.com/trypando/pando/internal/detect"
	"github.com/trypando/pando/internal/errs"
)

// TestR261_DetectionCanBeWaitedOn asserts R-261 for issue #80: a client can
// wait for detection through the API — the one mechanism the console, the CLI
// and MCP all use — rather than guessing how often to poll. A wait on a
// running detection is answered when it finishes, not when the wait runs out,
// and the answer while running says which stage it is on.
func TestR261_DetectionCanBeWaitedOn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	i := newInstall(t)
	admin := i.admin()
	appID := i.createApp(admin, "notes")
	detections := state.NewDetections(i.db)

	require.NoError(t, detections.Start(ctx, appID))
	require.NoError(t, detections.Save(ctx, appID, state.DetectionRunning,
		map[string]any{"stage": detect.StageTrying}, ""))

	got := i.do(admin, http.MethodGet, "/apps/"+appID+"/detection", nil)
	require.Equal(t, http.StatusOK, got.Code, got.String())
	var running struct {
		Status  string `json:"status"`
		Stage   string `json:"stage"`
		Elapsed *int   `json:"elapsed_seconds"`
	}
	got.JSON(t, &running)
	require.Equal(t, state.DetectionRunning, running.Status)
	require.Equal(t, detect.StageTrying, running.Stage)
	require.NotNil(t, running.Elapsed, "a running detection says how long it has been going")

	// Finished a moment into the wait, from elsewhere, the way the detector
	// records its outcome.
	finished := make(chan error, 1)
	time.AfterFunc(300*time.Millisecond, func() {
		finished <- detections.Save(ctx, appID, state.DetectionReady, detect.Proposal{Status: detect.StatusReady}, "")
	})

	began := time.Now()
	got = i.do(admin, http.MethodGet, "/apps/"+appID+"/detection?wait=30", nil)
	held := time.Since(began)
	require.NoError(t, <-finished)
	require.Equal(t, http.StatusOK, got.Code, got.String())
	var done struct {
		Status string  `json:"status"`
		Stage  *string `json:"stage"`
	}
	got.JSON(t, &done)
	require.Equal(t, state.DetectionReady, done.Status)
	require.Nil(t, done.Stage, "a finished detection has no stage")
	require.Less(t, held, 20*time.Second, "answered when detection finished, not when the wait ran out")

	// Nothing to wait for now, so a wait is answered at once.
	began = time.Now()
	got = i.do(admin, http.MethodGet, "/apps/"+appID+"/detection?wait=30s", nil)
	require.Equal(t, http.StatusOK, got.Code, got.String())
	require.Less(t, time.Since(began), 5*time.Second)
}

// A wait Pando cannot read is refused with what it can, not ignored.
func TestR105_AnUnreadableWaitSaysWhatIsValid(t *testing.T) {
	t.Parallel()
	i := newInstall(t)
	admin := i.admin()
	appID := i.createApp(admin, "notes")
	require.NoError(t, state.NewDetections(i.db).Start(context.Background(), appID))

	got := i.do(admin, http.MethodGet, "/apps/"+appID+"/detection?wait=soon", nil)
	require.Equal(t, http.StatusBadRequest, got.Code, got.String())
	require.Equal(t, string(errs.ValidInvalid), got.ErrorCode())
	require.Contains(t, got.String(), "wait=30")
}

// TestR261_AnAppSaysWhereItsDetectionIs asserts R-261 for issue #80: the app
// itself, and its row in the list, carry its detection's status and stage, so
// a draft says why it is still a draft without a second request.
func TestR261_AnAppSaysWhereItsDetectionIs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	i := newInstall(t)
	admin := i.admin()
	appID := i.createApp(admin, "notes")
	detections := state.NewDetections(i.db)

	type summary struct {
		Status string `json:"status"`
		Stage  string `json:"stage"`
	}

	// Before detection has run at all, there is nothing to say.
	got := i.do(admin, http.MethodGet, "/apps/"+appID, nil)
	require.Equal(t, http.StatusOK, got.Code, got.String())
	var before struct {
		Detection *summary `json:"detection"`
	}
	got.JSON(t, &before)
	require.Nil(t, before.Detection)

	require.NoError(t, detections.Start(ctx, appID))
	require.NoError(t, detections.Save(ctx, appID, state.DetectionRunning,
		map[string]any{"stage": detect.StageFetching}, ""))

	got = i.do(admin, http.MethodGet, "/apps/"+appID, nil)
	require.Equal(t, http.StatusOK, got.Code, got.String())
	var one struct {
		Detection *summary `json:"detection"`
	}
	got.JSON(t, &one)
	require.NotNil(t, one.Detection)
	require.Equal(t, summary{Status: state.DetectionRunning, Stage: detect.StageFetching}, *one.Detection)

	require.NoError(t, detections.Save(ctx, appID, state.DetectionNeedsAnswers,
		detect.Proposal{Status: detect.StatusNeedsAnswers}, ""))

	got = i.do(admin, http.MethodGet, "/apps", nil)
	require.Equal(t, http.StatusOK, got.Code, got.String())
	var list struct {
		Apps []struct {
			ID        string   `json:"id"`
			Detection *summary `json:"detection"`
		} `json:"apps"`
	}
	got.JSON(t, &list)
	require.Len(t, list.Apps, 1)
	require.Equal(t, appID, list.Apps[0].ID)
	require.NotNil(t, list.Apps[0].Detection)
	require.Equal(t, summary{Status: state.DetectionNeedsAnswers}, *list.Apps[0].Detection,
		"a finished detection carries no stage")
}
