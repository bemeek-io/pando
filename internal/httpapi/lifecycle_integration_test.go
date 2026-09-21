//go:build integration

package httpapi_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/httpapi"
)

// --- deployments -----------------------------------------------------------

func TestADeploymentThatDoesNotExistIsNotFound(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	id := i.createApp(admin, "notes")

	got := i.do(admin, http.MethodGet, "/apps/"+id+"/deployments/dep_nothing", nil)
	require.Equal(t, http.StatusNotFound, got.Code, got.String())
	require.Equal(t, string(errs.NotFound), got.ErrorCode())
}

// R-146: rolling back needs something to roll back to.
func TestR146_RollingBackWithNoEarlierRevisionIsRefusedReadably(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	id := i.createApp(admin, "notes")

	got := i.do(admin, http.MethodPost, "/apps/"+id+"/deployments/rollback", map[string]any{})
	require.GreaterOrEqual(t, got.Code, 400, got.String())
	require.NotEqual(t, http.StatusInternalServerError, got.Code)
	require.NotEmpty(t, got.ErrorCode())
}

func TestRollingBackToARevisionThatDoesNotExist(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	id := i.createApp(admin, "notes")

	got := i.do(admin, http.MethodPost, "/apps/"+id+"/deployments/rollback", map[string]any{"to": 99})
	require.GreaterOrEqual(t, got.Code, 400, got.String())
	require.NotEqual(t, http.StatusInternalServerError, got.Code)
}

// Deploying is behind app.deploy, which a data-plane grant does not carry.
func TestDeployingIsBehindItsOwnVerb(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	other := i.user("ordinary")
	id := i.createApp(admin, "notes")

	got := i.do(other, http.MethodPost, "/apps/"+id+"/deployments", map[string]any{})
	require.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, got.Code, got.String())
}

// --- app status and logs ---------------------------------------------------

func TestStatusOfAnAppThatHasNeverRun(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	id := i.createApp(admin, "notes")

	got := i.do(admin, http.MethodGet, "/apps/"+id+"/status", nil)
	require.NotEqual(t, http.StatusInternalServerError, got.Code, got.String())
	if got.Code == http.StatusOK {
		require.Contains(t, got.String(), "state")
	}
}

// R-170: logs stream. The handler must not buffer the whole response before
// answering, and it must not fail when there is nothing to stream yet.
func TestR170_AppLogsAnswerEvenWhenThereIsNothingToStream(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	id := i.createApp(admin, "notes")

	got := i.do(admin, http.MethodGet, "/apps/"+id+"/logs", nil)
	require.NotEqual(t, http.StatusInternalServerError, got.Code, got.String())
}

func TestDeploymentLogsOfANonexistentDeployment(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	id := i.createApp(admin, "notes")

	got := i.do(admin, http.MethodGet, "/apps/"+id+"/deployments/dep_nothing/logs", nil)
	require.NotEqual(t, http.StatusInternalServerError, got.Code, got.String())
	require.GreaterOrEqual(t, got.Code, 400)
}

// --- lifecycle -------------------------------------------------------------

// Start and stop set desired state; restart is an act rather than a state and
// goes through the runtime.
func TestLifecycleEndpointsAreBehindTheLifecycleVerb(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	other := i.user("ordinary")
	id := i.createApp(admin, "notes")

	for _, action := range []string{"start", "stop", "restart"} {
		got := i.do(other, http.MethodPost, "/apps/"+id+"/"+action, nil)
		require.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, got.Code,
			"%s: %s", action, got.String())
	}
}

func TestStartingAndStoppingRecordTheDesiredState(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	id := i.createApp(admin, "notes")

	stop := i.do(admin, http.MethodPost, "/apps/"+id+"/stop", nil)
	require.NotEqual(t, http.StatusInternalServerError, stop.Code, stop.String())

	start := i.do(admin, http.MethodPost, "/apps/"+id+"/start", nil)
	require.NotEqual(t, http.StatusInternalServerError, start.Code, start.String())

	if start.Code < 400 {
		got := i.do(admin, http.MethodGet, "/apps/"+id, nil)
		var app map[string]any
		got.JSON(t, &app)
		require.Equal(t, "running", app["desired_state"])
	}
}

// TestR151_AHumanCanStartAnAppPandoGaveUpOn asserts R-151.
//
// "A failed app stays failed until a human intervenes." The reconciler has no
// code path that touches a failed app — that absence is the mechanism — so
// desired state alone converges to nothing for one: pressing Start recorded
// `running` and the app sat in `failed` forever, which is a dead end rather
// than an intervention.
//
// Starting one is the human, so it clears the failure count and hands the app
// back to the loop. Nothing here retries on its own, which is what R-151
// forbids.
func TestR151_AHumanCanStartAnAppPandoGaveUpOn(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	id := i.createApp(admin, "notes")

	_, err := i.db.Exec(context.Background(),
		`UPDATE apps SET state = 'failed', consecutive_failures = 10 WHERE id = $1`, id)
	require.NoError(t, err)

	got := i.do(admin, http.MethodPost, "/apps/"+id+"/start", nil)
	require.Less(t, got.Code, 400, got.String())

	var appState string
	var failures int
	require.NoError(t, i.db.QueryRow(context.Background(),
		`SELECT state, consecutive_failures FROM apps WHERE id = $1`, id).Scan(&appState, &failures))

	require.NotEqual(t, "failed", appState, "the loop can look at it again")
	require.Zero(t, failures, "or it gives up again on the first tick")
}

// Stopping one works too, and for the same reason: the loop is never coming.
func TestStoppingAFailedAppIsRecorded(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	id := i.createApp(admin, "notes")

	_, err := i.db.Exec(context.Background(),
		`UPDATE apps SET state = 'failed' WHERE id = $1`, id)
	require.NoError(t, err)

	got := i.do(admin, http.MethodPost, "/apps/"+id+"/stop", nil)
	require.Less(t, got.Code, 400, got.String())

	var desired string
	require.NoError(t, i.db.QueryRow(context.Background(),
		`SELECT desired_state FROM apps WHERE id = $1`, id).Scan(&desired))
	require.Equal(t, "stopped", desired)
}

// --- uploaded source (R-262) -----------------------------------------------

// packed is a gzipped tar the upload endpoint will accept.
func packed(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg,
		}))
		_, err := tw.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

// upload posts a raw gzip body, which the JSON helper cannot do.
func (i *install) upload(s *session, appID string, body []byte) reply {
	i.t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/apps/"+appID+"/source", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/gzip")
	if s != nil && s.cookie != "" {
		req.Header.Set("Cookie", httpapi.SessionCookie+"="+s.cookie)
	}
	if s != nil && s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}

	rec := httptest.NewRecorder()
	i.handler.ServeHTTP(rec, req)
	return reply{Code: rec.Code, Body: rec.Body.Bytes(), Hdr: rec.Header()}
}

// Gated by app.spec.edit rather than app.deploy: replacing the source changes
// what the app *is*.
func TestR262_UploadingSourceIsBehindTheSpecEditVerb(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	other := i.user("ordinary")
	id := i.createApp(admin, "notes")

	archive := packed(t, map[string]string{"main.go": "package main"})

	denied := i.upload(other, id, archive)
	require.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, denied.Code, denied.String())

	allowed := i.upload(admin, id, archive)
	require.NotEqual(t, http.StatusInternalServerError, allowed.Code, allowed.String())
	require.Less(t, allowed.Code, 400, allowed.String())
}

func TestAnUploadThatIsNotAnArchiveIsRefusedRatherThanStored(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	id := i.createApp(admin, "notes")

	got := i.upload(admin, id, []byte("this is not a gzip stream"))
	require.NotEqual(t, http.StatusInternalServerError, got.Code, got.String())
}

func TestUploadingToAnAppThatDoesNotExist(t *testing.T) {
	i := newInstall(t)

	got := i.upload(i.admin(), "app_01HQ8ZZZZZZZZZZZZZZZZZZZZZ",
		packed(t, map[string]string{"main.go": "package main"}))
	require.GreaterOrEqual(t, got.Code, 400, got.String())
	require.NotEqual(t, http.StatusInternalServerError, got.Code)
}

// --- backups ---------------------------------------------------------------

// R-212: backup and restore are install-scoped administration.
func TestR212_BackupEndpointsAreInstallAdministration(t *testing.T) {
	i := newInstall(t)
	other := i.user("ordinary")

	for _, c := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/backups", nil},
		{http.MethodPost, "/backups", map[string]any{"passphrase": "correct horse battery staple"}},
		{http.MethodPost, "/backups/bk_01HQ8/verify", map[string]any{"passphrase": "x"}},
		{http.MethodPost, "/backups/bk_01HQ8/restore", map[string]any{"passphrase": "x", "confirm": true}},
	} {
		got := i.do(other, c.method, c.path, c.body)
		require.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, got.Code,
			"%s %s: %s", c.method, c.path, got.String())
	}
}

func TestListingBackupsOnAFreshInstall(t *testing.T) {
	i := newInstall(t)

	got := i.do(i.admin(), http.MethodGet, "/backups", nil)
	require.Equal(t, http.StatusOK, got.Code, got.String())

	var body struct {
		Backups []map[string]any `json:"backups"`
	}
	got.JSON(t, &body)
	require.Empty(t, body.Backups)
}

// Verify is its own route rather than a flag on restore, because a flag is a
// thing somebody passes wrongly and the wrong value here replaces an
// installation.
func TestVerifyAndRestoreAreSeparateRoutes(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()

	verify := i.do(admin, http.MethodPost, "/backups/bk_nothing/verify",
		map[string]any{"passphrase": "correct horse battery staple"})
	require.GreaterOrEqual(t, verify.Code, 400, verify.String())
	require.NotEqual(t, http.StatusInternalServerError, verify.Code)

	restore := i.do(admin, http.MethodPost, "/backups/bk_nothing/restore",
		map[string]any{"passphrase": "correct horse battery staple", "confirm": true})
	require.GreaterOrEqual(t, restore.Code, 400, restore.String())
	require.NotEqual(t, http.StatusInternalServerError, restore.Code)
}

// The most destructive action in the system does not happen because a field was
// left out of a request body.
func TestRestoringWithoutConfirmationIsRefused(t *testing.T) {
	i := newInstall(t)

	got := i.do(i.admin(), http.MethodPost, "/backups/bk_nothing/restore",
		map[string]any{"passphrase": "correct horse battery staple"})
	require.GreaterOrEqual(t, got.Code, 400, got.String())
	require.NotEqual(t, http.StatusInternalServerError, got.Code)
}

// R-214: Pando does not keep the passphrase, so one has to be given.
func TestR214_ABackupWithNoPassphraseIsRefused(t *testing.T) {
	i := newInstall(t)

	got := i.do(i.admin(), http.MethodPost, "/backups", map[string]any{})
	require.GreaterOrEqual(t, got.Code, 400, got.String())
	require.NotEqual(t, http.StatusInternalServerError, got.Code)
	require.NotEmpty(t, got.ErrorCode())
}

// R-206: restoring one app's data is an app operation, so its owner can do it
// without install.backup.manage.
func TestR206_RestoringOneAppsDataIsGatedOnTheAppNotTheInstall(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	other := i.user("ordinary")
	id := i.createApp(admin, "notes")

	denied := i.do(other, http.MethodPost, "/apps/"+id+"/restore",
		map[string]any{"backup_id": "bk_nothing", "confirm": true})
	require.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, denied.Code, denied.String())

	// The owner gets past authorization and fails on the backup not existing,
	// which is the point: the gate is the app, not the install.
	owner := i.do(admin, http.MethodPost, "/apps/"+id+"/restore",
		map[string]any{"backup_id": "bk_nothing", "confirm": true})
	require.GreaterOrEqual(t, owner.Code, 400, owner.String())
	require.NotEqual(t, http.StatusForbidden, owner.Code, "the owner is not refused by authorization")
	require.NotEqual(t, http.StatusInternalServerError, owner.Code)
}

// --- exec ------------------------------------------------------------------

// R-086: the most privileged action in the system. The verb is checked, then
// host policy, and only then is a session opened.
func TestR086_ExecIsBehindItsOwnVerb(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	other := i.user("ordinary")
	id := i.createApp(admin, "notes")

	denied := i.do(other, http.MethodGet, "/apps/"+id+"/exec", nil)
	require.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, denied.Code, denied.String())

	// The owner gets past the verb and fails for a reason about the app rather
	// than about permission — there is nothing running to open a terminal in.
	owner := i.do(admin, http.MethodGet, "/apps/"+id+"/exec", nil)
	require.GreaterOrEqual(t, owner.Code, 400, owner.String())
	require.NotEqual(t, http.StatusForbidden, owner.Code)
	require.NotEqual(t, http.StatusInternalServerError, owner.Code)
}
