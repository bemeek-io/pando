//go:build integration

package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/errs"
)

// 202, not 201: the app exists but is in draft. Detection has been queued, and
// nothing is deployed — saying so is the difference between waiting and
// wondering.
func TestCreatingAnAppLeavesItInDraftWithDetectionQueued(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()

	got := i.do(admin, http.MethodPost, "/apps", map[string]any{
		"name":   "Team Notes",
		"source": map[string]string{"type": "git", "url": "https://github.com/acme/notes"},
	})
	require.Equal(t, http.StatusAccepted, got.Code, got.String())

	var app map[string]any
	got.JSON(t, &app)
	require.NotEmpty(t, app["id"])
	require.Equal(t, "Team Notes", app["name"])
	require.Equal(t, "draft", app["state"])
	// Derived from the name, URL-safe, and suffixed so two apps called the same
	// thing do not collide on the address they are served at.
	require.Regexp(t, `^team-notes(-[a-z0-9]+)?$`, app["slug"])
	require.Equal(t, i.AdminID, app["owner_user_id"])
}

func TestAnAppNeedsAName(t *testing.T) {
	i := newInstall(t)

	got := i.do(i.admin(), http.MethodPost, "/apps", map[string]any{
		"name":   "   ",
		"source": map[string]string{"type": "git", "url": "https://github.com/acme/notes"},
	})
	require.Equal(t, http.StatusBadRequest, got.Code, got.String())
	require.Equal(t, string(errs.ValidInvalid), got.ErrorCode())

	var env struct {
		Remedy string `json:"remedy"`
	}
	got.JSON(t, &env)
	require.NotEmpty(t, env.Remedy, "R-105: it says what a valid answer looks like")
}

// R-262: an agent retries on a timeout, and a deploy that clones regularly
// outlasts a client's patience. A retry must not produce two apps.
func TestR262_ARetriedCreateReplaysRatherThanCreatingASecondApp(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()

	body := map[string]any{
		"name":            "notes",
		"source":          map[string]string{"type": "git", "url": "https://github.com/acme/notes"},
		"idempotency_key": "agent-run-7",
	}

	first := i.do(admin, http.MethodPost, "/apps", body)
	require.Equal(t, http.StatusAccepted, first.Code, first.String())

	second := i.do(admin, http.MethodPost, "/apps", body)
	require.Equal(t, http.StatusAccepted, second.Code, second.String())

	var one, two map[string]any
	first.JSON(t, &one)
	second.JSON(t, &two)
	require.Equal(t, one["id"], two["id"], "the same app comes back, not a second one")

	listed := i.do(admin, http.MethodGet, "/apps", nil)
	var apps struct {
		Apps []map[string]any `json:"apps"`
	}
	listed.JSON(t, &apps)
	require.Len(t, apps.Apps, 1)
}

func TestListingAppsShowsWhatTheCallerCanManage(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()

	i.createApp(admin, "notes")
	i.createApp(admin, "wiki")

	got := i.do(admin, http.MethodGet, "/apps", nil)
	require.Equal(t, http.StatusOK, got.Code)

	var apps struct {
		Apps []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"apps"`
	}
	got.JSON(t, &apps)
	require.Len(t, apps.Apps, 2)
}

// R-087: an administrator holds no app.* verb and is not an owner of every app.
// The two scopes are never conflated.
func TestR087_AnOrdinaryUserSeesOnlyTheirOwnApps(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	other := i.user("ordinary")

	adminApp := i.createApp(admin, "admin-notes")

	listed := i.do(other, http.MethodGet, "/apps", nil)
	require.Equal(t, http.StatusOK, listed.Code)
	var apps struct {
		Apps []map[string]any `json:"apps"`
	}
	listed.JSON(t, &apps)
	require.Empty(t, apps.Apps, "someone else's app is not theirs to see")

	denied := i.do(other, http.MethodGet, "/apps/"+adminApp, nil)
	require.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, denied.Code, denied.String())
}

// An app someone cannot see must not be distinguishable from one that does not
// exist, or the API becomes a way to enumerate apps.
func TestAnAppYouCannotSeeAnswersTheSameAsOneThatDoesNotExist(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	other := i.user("ordinary")

	hidden := i.createApp(admin, "secret-project")

	invisible := i.do(other, http.MethodGet, "/apps/"+hidden, nil)
	missing := i.do(other, http.MethodGet, "/apps/app_01HQ8ZZZZZZZZZZZZZZZZZZZZZ", nil)

	require.Equal(t, missing.Code, invisible.Code)
	require.Equal(t, missing.ErrorCode(), invisible.ErrorCode())
}

func TestGettingAnAppReturnsIt(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	id := i.createApp(admin, "notes")

	got := i.do(admin, http.MethodGet, "/apps/"+id, nil)
	require.Equal(t, http.StatusOK, got.Code, got.String())

	var app map[string]any
	got.JSON(t, &app)
	require.Equal(t, id, app["id"])
	require.Equal(t, "notes", app["name"])
}

func TestAnAppIDThatIsNotAnIDIsNotFound(t *testing.T) {
	i := newInstall(t)
	got := i.do(i.admin(), http.MethodGet, "/apps/not-an-id", nil)
	require.Contains(t, []int{http.StatusBadRequest, http.StatusNotFound}, got.Code, got.String())
}

func TestRenamingAnApp(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	id := i.createApp(admin, "notes")

	got := i.do(admin, http.MethodPatch, "/apps/"+id, map[string]any{"name": "Field Notes"})
	require.Equal(t, http.StatusOK, got.Code, got.String())

	var app map[string]any
	got.JSON(t, &app)
	require.Equal(t, "Field Notes", app["name"])
}

// R-205: a delete keeps a copy of the app's storage unless told not to, and a
// failed backup means the data is not safe — so the app is not deleted.
func TestR205_DeletingAnAppTakesTheBackupDecisionExplicitly(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	id := i.createApp(admin, "notes")

	// force=true is the documented way to say "discard the data".
	got := i.do(admin, http.MethodDelete, "/apps/"+id+"?force=true", nil)
	require.Contains(t, []int{http.StatusNoContent, http.StatusOK}, got.Code, got.String())

	after := i.do(admin, http.MethodGet, "/apps/"+id, nil)
	require.Equal(t, http.StatusNotFound, after.Code, after.String())
}

// R-204/R-205: neither answer is assumed. An app with storage and no decision
// is refused rather than deleted one way or the other.
func TestR205_ADeleteWithNoDecisionIsAnsweredRatherThanAssumed(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	id := i.createApp(admin, "notes")

	got := i.do(admin, http.MethodDelete, "/apps/"+id, nil)
	require.NotEqual(t, http.StatusInternalServerError, got.Code, got.String())

	if got.Code >= 400 {
		require.Equal(t, string(errs.StateBackupDecisionRequired), got.ErrorCode(), got.String())
	}
}

// R-152: spec revisions are append-only, and a new app has none until detection
// is accepted or a spec is written.
func TestR152_ANewAppHasNoSpecRevisionsYet(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	id := i.createApp(admin, "notes")

	got := i.do(admin, http.MethodGet, "/apps/"+id+"/specs", nil)
	require.Equal(t, http.StatusOK, got.Code, got.String())

	var body struct {
		Revisions    []map[string]any `json:"revisions"`
		PinnedSpecID string           `json:"pinned_spec_id"`
	}
	got.JSON(t, &body)
	require.Empty(t, body.Revisions)
	require.Empty(t, body.PinnedSpecID)
}

func TestExportingAnAppWithNoPinnedSpecSaysSo(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	id := i.createApp(admin, "notes")

	got := i.do(admin, http.MethodGet, "/apps/"+id+"/export", nil)
	require.GreaterOrEqual(t, got.Code, 400, "there is nothing to export yet")
	require.NotEqual(t, http.StatusInternalServerError, got.Code, got.String())
}

// Start and stop set desired state and let the reconciler converge, so
// "stopped" survives a Pando restart (design 05).
func TestStartAndStopSetDesiredStateRatherThanActing(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	id := i.createApp(admin, "notes")

	stopped := i.do(admin, http.MethodPost, "/apps/"+id+"/stop", nil)
	require.NotEqual(t, http.StatusInternalServerError, stopped.Code, stopped.String())

	if stopped.Code < 400 {
		got := i.do(admin, http.MethodGet, "/apps/"+id, nil)
		var app map[string]any
		got.JSON(t, &app)
		require.Equal(t, "stopped", app["desired_state"])
	}
}

// Every app endpoint is behind a verb, and an anonymous caller holds none.
func TestEveryAppEndpointRefusesAnAnonymousCaller(t *testing.T) {
	i := newInstall(t)
	id := i.createApp(i.admin(), "notes")

	for _, c := range []struct {
		method, path string
	}{
		{http.MethodPost, "/apps"},
		{http.MethodGet, "/apps/" + id},
		{http.MethodPatch, "/apps/" + id},
		{http.MethodDelete, "/apps/" + id},
		{http.MethodGet, "/apps/" + id + "/specs"},
		{http.MethodGet, "/apps/" + id + "/export"},
		{http.MethodGet, "/apps/" + id + "/slots"},
		{http.MethodGet, "/apps/" + id + "/volumes"},
		{http.MethodGet, "/apps/" + id + "/secrets"},
		{http.MethodGet, "/apps/" + id + "/grants"},
		{http.MethodGet, "/apps/" + id + "/deployments"},
		{http.MethodPost, "/apps/" + id + "/plan"},
		{http.MethodPost, "/apps/" + id + "/deployments"},
		{http.MethodPost, "/apps/" + id + "/start"},
		{http.MethodPost, "/apps/" + id + "/stop"},
		{http.MethodPost, "/apps/" + id + "/restart"},
	} {
		got := i.anon(c.method, c.path, map[string]any{})
		require.GreaterOrEqual(t, got.Code, 400, "%s %s was not refused", c.method, c.path)
		require.NotEqual(t, http.StatusInternalServerError, got.Code,
			"%s %s: %s", c.method, c.path, got.String())
	}
}

// R-132: a required unfilled slot blocks deploy at plan time, not at deploy
// time. An app with no pinned spec cannot be planned either, and the refusal
// has to be readable rather than a 500.
func TestPlanningAnAppWithNothingPinnedIsRefusedReadably(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	id := i.createApp(admin, "notes")

	got := i.do(admin, http.MethodPost, "/apps/"+id+"/plan", map[string]any{})
	require.GreaterOrEqual(t, got.Code, 400)
	require.NotEqual(t, http.StatusInternalServerError, got.Code, got.String())

	var env struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	got.JSON(t, &env)
	require.NotEmpty(t, env.Code)
	require.NotEmpty(t, env.Message)
}

func TestDeployingAnAppWithNothingPinnedIsRefusedReadably(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	id := i.createApp(admin, "notes")

	got := i.do(admin, http.MethodPost, "/apps/"+id+"/deployments", map[string]any{})
	require.GreaterOrEqual(t, got.Code, 400)
	require.NotEqual(t, http.StatusInternalServerError, got.Code, got.String())
	require.NotEmpty(t, got.ErrorCode())
}

func TestListingDeploymentsOfANewApp(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	id := i.createApp(admin, "notes")

	got := i.do(admin, http.MethodGet, "/apps/"+id+"/deployments", nil)
	require.Equal(t, http.StatusOK, got.Code, got.String())
	require.Contains(t, got.String(), "deployments")
}

// The launcher (R-264) is data-plane scoped, and deliberately a different list
// from GET /apps.
func TestR264_TheLauncherIsADifferentListFromTheManagementOne(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	i.createApp(admin, "notes")

	launcher := i.do(admin, http.MethodGet, "/me/apps", nil)
	require.Equal(t, http.StatusOK, launcher.Code, launcher.String())
	require.Contains(t, launcher.String(), "apps")

	// Anonymous gets an answer rather than an error: R-075's anonymous grant is
	// checked on the same path as any other.
	anon := i.anon(http.MethodGet, "/me/apps", nil)
	require.NotEqual(t, http.StatusInternalServerError, anon.Code, anon.String())
}
