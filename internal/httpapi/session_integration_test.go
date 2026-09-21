//go:build integration

package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/bootstrap"
	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/httpapi"
)

// R-046: the first run creates one administrative account, and it must change
// its password at first sign-in.
func TestR046_TheFirstRunAccountSignsInAndIsToldToChangeItsPassword(t *testing.T) {
	i := newInstall(t)

	s, got := i.signIn(bootstrap.AdminUsername, i.adminPassword)
	require.Equal(t, http.StatusOK, got.Code, got.String())
	require.NotEmpty(t, s.cookie)

	var body struct {
		UserID                  string `json:"user_id"`
		MustChangePassword      bool   `json:"must_change_password"`
		ExpiresAt               string `json:"expires_at"`
		RevocationWindowSeconds int    `json:"revocation_window_seconds"`
	}
	got.JSON(t, &body)

	require.Equal(t, i.AdminID, body.UserID)
	require.True(t, body.MustChangePassword)
	require.NotEmpty(t, body.ExpiresAt)

	// The revocation window is stated rather than implied (design 06 §3.1).
	// Implying revocation is instant is the failure mode here.
	require.Positive(t, body.RevocationWindowSeconds)
}

func TestTheSessionCookieIsHTTPOnlyAndScopedToTheWholeSite(t *testing.T) {
	i := newInstall(t)
	_, got := i.signIn(bootstrap.AdminUsername, i.adminPassword)

	var cookie *http.Cookie
	for _, c := range (&http.Response{Header: got.Hdr}).Cookies() {
		if c.Name == httpapi.SessionCookie {
			cookie = c
		}
	}
	require.NotNil(t, cookie)
	require.True(t, cookie.HttpOnly, "script must not be able to read the session")
	require.Equal(t, "/", cookie.Path)
	require.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
	require.False(t, cookie.Expires.IsZero(), "a session cookie states when it stops working")
}

func TestABadPasswordIsRefusedWithoutSayingWhichHalfWasWrong(t *testing.T) {
	i := newInstall(t)

	_, got := i.signIn(bootstrap.AdminUsername, "not the password")
	require.Equal(t, http.StatusUnauthorized, got.Code)
	require.NotContains(t, got.String(), "password is wrong")

	_, missing := i.signIn("nobody", "not the password")
	require.Equal(t, http.StatusUnauthorized, missing.Code)
	require.Equal(t, got.ErrorCode(), missing.ErrorCode(),
		"an unknown account and a wrong password are the same answer")
}

func TestAMalformedLoginBodyIsRejected(t *testing.T) {
	i := newInstall(t)

	req := i.anon(http.MethodPost, "/sessions", nil)
	require.Equal(t, http.StatusBadRequest, req.Code)
	require.Equal(t, string(errs.ValidInvalid), req.ErrorCode())
}

// A session is a credential: signing out has to stop it working immediately,
// not merely clear the browser's copy.
func TestSigningOutRevokesTheSessionServerSide(t *testing.T) {
	i := newInstall(t)
	s := i.admin()

	require.Equal(t, http.StatusOK, i.do(s, http.MethodGet, "/me", nil).Code)

	out := i.do(s, http.MethodDelete, "/sessions", nil)
	require.Contains(t, []int{http.StatusOK, http.StatusNoContent}, out.Code, out.String())

	// The same cookie, replayed. The browser's copy is not what decides.
	after := i.do(s, http.MethodGet, "/me", nil)
	require.Equal(t, http.StatusUnauthorized, after.Code, after.String())
}

func TestSigningOutWithNoSessionIsNotAnError(t *testing.T) {
	i := newInstall(t)
	got := i.anon(http.MethodDelete, "/sessions", nil)
	require.Contains(t, []int{http.StatusOK, http.StatusNoContent}, got.Code, got.String())
}

// Anonymous is a principal, not an absence: R-075's anonymous grant is checked
// on the same path as any other, and there is no branch that skips the check.
func TestR075_AnAnonymousCallerIsRefusedByAuthorizationRatherThanAtTheDoor(t *testing.T) {
	i := newInstall(t)

	got := i.anon(http.MethodGet, "/me", nil)
	require.Equal(t, http.StatusUnauthorized, got.Code, got.String())

	apps := i.anon(http.MethodGet, "/apps", nil)
	require.Contains(t, []int{http.StatusOK, http.StatusUnauthorized, http.StatusForbidden}, apps.Code)
}

func TestAnUnparseableAuthorizationHeaderIsRefused(t *testing.T) {
	i := newInstall(t)

	got := i.do(&session{token: ""}, http.MethodGet, "/me", nil)
	require.Equal(t, http.StatusUnauthorized, got.Code)

	// Not a bearer scheme at all.
	req := i.anon(http.MethodGet, "/me", nil)
	require.Equal(t, http.StatusUnauthorized, req.Code)
}

func TestAnUnknownTokenIsRefused(t *testing.T) {
	i := newInstall(t)

	got := i.do(&session{token: "tok_does_not_exist"}, http.MethodGet, "/me", nil)
	require.Equal(t, http.StatusUnauthorized, got.Code, got.String())
}

// GET /me is what the console asks to find out who it is talking to and what
// that person may do install-wide.
func TestMeReportsTheSignedInAccount(t *testing.T) {
	i := newInstall(t)

	got := i.do(i.admin(), http.MethodGet, "/me", nil)
	require.Equal(t, http.StatusOK, got.Code, got.String())

	var me map[string]any
	got.JSON(t, &me)
	require.Equal(t, i.AdminID, me["user_id"])
	require.NotContains(t, me, "password_hash", "R-194: nothing that could be a credential")
}

// R-058: a delegated token acts as its owner and holds nothing they do not.
func TestR058_ATokenActsAsItsOwner(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	agent := i.tokenFor(admin)

	got := i.do(agent, http.MethodGet, "/me", nil)
	require.Equal(t, http.StatusOK, got.Code, got.String())

	var me map[string]any
	got.JSON(t, &me)
	require.Equal(t, i.AdminID, me["user_id"], "the token answers as the person who made it")
}

// The secret is shown once, at creation. A token that could be read back would
// make the list endpoint a credential store.
func TestATokenSecretIsShownOnceAndNeverListed(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()

	created := i.do(admin, http.MethodPost, "/tokens", map[string]any{"name": "laptop"})
	require.Equal(t, http.StatusCreated, created.Code, created.String())

	var issued struct {
		Token  struct{ ID string } `json:"token"`
		Secret string              `json:"secret"`
	}
	created.JSON(t, &issued)
	require.NotEmpty(t, issued.Secret)
	require.NotEmpty(t, issued.Token.ID)

	listed := i.do(admin, http.MethodGet, "/tokens", nil)
	require.Equal(t, http.StatusOK, listed.Code)
	require.NotContains(t, listed.String(), issued.Secret,
		"R-194: the secret never comes back out")
}

func TestARevokedTokenStopsWorkingImmediately(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()

	created := i.do(admin, http.MethodPost, "/tokens", map[string]any{"name": "laptop"})
	var issued struct {
		Token  struct{ ID string } `json:"token"`
		Secret string              `json:"secret"`
	}
	created.JSON(t, &issued)
	require.NotEmpty(t, issued.Token.ID)

	agent := &session{token: issued.Secret}
	require.Equal(t, http.StatusOK, i.do(agent, http.MethodGet, "/me", nil).Code)

	revoked := i.do(admin, http.MethodDelete, "/tokens/"+issued.Token.ID, nil)
	require.Contains(t, []int{http.StatusOK, http.StatusNoContent}, revoked.Code, revoked.String())

	after := i.do(agent, http.MethodGet, "/me", nil)
	require.Equal(t, http.StatusUnauthorized, after.Code, after.String())
}

// Self-service, because a token holds nothing its owner does not — it is a
// second credential for power already held, not new power.
func TestOnePersonsTokensAreNotAnothersToSeeOrRevoke(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	other := i.user("ordinary")

	created := i.do(admin, http.MethodPost, "/tokens", map[string]any{"name": "admin laptop"})
	var issued struct {
		Token struct{ ID string } `json:"token"`
	}
	created.JSON(t, &issued)
	require.NotEmpty(t, issued.Token.ID)

	listed := i.do(other, http.MethodGet, "/tokens", nil)
	require.Equal(t, http.StatusOK, listed.Code)
	require.NotContains(t, listed.String(), issued.Token.ID,
		"the list is the caller's own tokens, not the install's")

	denied := i.do(other, http.MethodDelete, "/tokens/"+issued.Token.ID, nil)
	require.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, denied.Code, denied.String())

	// And the token still works.
	require.Equal(t, http.StatusOK, i.do(admin, http.MethodGet, "/tokens", nil).Code)
}

// TestR060_AServiceTokenIsItsOwnPrincipalAndTakesAnInstallVerbToMint asserts the
// line between the two kinds of token.
//
// A delegated token is self-service because it holds what its owner holds
// (R-058). An account token is a new subject on the installation — its own
// principal, in grants and in the audit log under its own name, outliving
// whoever created it (R-060) — so minting one is administration.
func TestR060_AServiceTokenIsItsOwnPrincipalAndTakesAnInstallVerbToMint(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	ordinary := i.user("ordinary")

	// Self-service stops at delegated tokens.
	denied := i.do(ordinary, http.MethodPost, "/tokens/service", map[string]any{"name": "CI deploys"})
	require.Equal(t, http.StatusForbidden, denied.Code, denied.String())
	require.Equal(t, http.StatusForbidden, i.do(ordinary, http.MethodGet, "/tokens/service", nil).Code)

	created := i.do(admin, http.MethodPost, "/tokens/service", map[string]any{"name": "CI deploys"})
	require.Equal(t, http.StatusCreated, created.Code, created.String())

	var issued struct {
		Token struct {
			ID          string `json:"id"`
			Kind        string `json:"kind"`
			OwnerUserID string `json:"owner_user_id"`
		} `json:"token"`
		Secret string `json:"secret"`
	}
	created.JSON(t, &issued)
	require.Equal(t, "account", issued.Token.Kind)
	require.Empty(t, issued.Token.OwnerUserID, "R-060: an account token acts as itself")
	require.NotEmpty(t, issued.Secret)

	// It authenticates as itself rather than as the administrator who made it.
	agent := &session{token: issued.Secret}
	me := i.do(agent, http.MethodGet, "/me", nil)
	require.Equal(t, http.StatusOK, me.Code, me.String())

	var who map[string]any
	me.JSON(t, &who)
	require.Equal(t, issued.Token.ID, who["id"])
	require.Empty(t, who["user_id"], "an account token is nobody's delegate")

	// And it holds nothing until somebody shares something with it: the
	// administrator's power did not come with it.
	apps := i.do(agent, http.MethodGet, "/apps", nil)
	require.Equal(t, http.StatusOK, apps.Code, apps.String())
	require.NotContains(t, apps.String(), "\"id\":\"app_", "a new service token administers nothing")

	listed := i.do(admin, http.MethodGet, "/tokens/service", nil)
	require.Equal(t, http.StatusOK, listed.Code)
	require.Contains(t, listed.String(), issued.Token.ID)
	require.NotContains(t, listed.String(), issued.Secret, "R-194: the secret never comes back out")

	// A service token is not in anybody's personal list, including its
	// creator's — it is not theirs, it is the installation's.
	mine := i.do(admin, http.MethodGet, "/tokens", nil)
	require.NotContains(t, mine.String(), issued.Token.ID)
}

// A token that can mint tokens renews itself forever, and revoking the original
// achieves nothing. The rule holds for the kind that takes a verb, where a
// stolen token holding install.users.manage would otherwise be permanent.
func TestR060_AServiceTokenCannotMintAnotherToken(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()

	created := i.do(admin, http.MethodPost, "/tokens", map[string]any{"name": "admin laptop"})
	var issued struct {
		Secret string `json:"secret"`
	}
	created.JSON(t, &issued)
	require.NotEmpty(t, issued.Secret)

	// This token carries the administrator's install verbs, and still cannot.
	agent := &session{token: issued.Secret}
	refused := i.do(agent, http.MethodPost, "/tokens/service", map[string]any{"name": "second"})
	require.Equal(t, http.StatusForbidden, refused.Code, refused.String())
	require.Contains(t, refused.String(), "cannot create another token")
}

// Every response carries the request ID, so a user reporting a failure hands
// over one string that finds the log line.
func TestEveryAPIResponseCarriesARequestID(t *testing.T) {
	i := newInstall(t)

	for _, got := range []reply{
		i.do(i.admin(), http.MethodGet, "/me", nil),
		i.anon(http.MethodGet, "/me", nil),
		i.anon(http.MethodGet, "/apps/app_nonexistent", nil),
	} {
		require.Regexp(t, `^req_[0-9A-HJKMNP-TV-Z]{26}$`, got.Hdr.Get(httpapi.RequestIDHeader))
	}
}

// An error crossing the API boundary carries the envelope from design 00 §3.2.
func TestAnErrorCarriesTheEnvelopeWithItsRequestID(t *testing.T) {
	i := newInstall(t)

	got := i.anon(http.MethodGet, "/me", nil)
	var env struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	}
	got.JSON(t, &env)

	require.NotEmpty(t, env.Code)
	require.NotEmpty(t, env.Message)
	require.Equal(t, got.Hdr.Get(httpapi.RequestIDHeader), env.RequestID)
}
