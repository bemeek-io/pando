//go:build integration

package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestR060_ServiceTokensHaveTheirOwnVerb asserts install.tokens.manage: it
// lists, creates and revokes service tokens and does nothing for accounts, and
// install.users.manage alone no longer reaches service tokens. The
// Administrator holds both.
func TestR060_ServiceTokensHaveTheirOwnVerb(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()

	var me struct {
		Verbs []string `json:"verbs"`
	}
	i.do(admin, http.MethodGet, "/me", nil).JSON(t, &me)
	require.Contains(t, me.Verbs, "install.tokens.manage", "the Administrator holds it by migration")

	grant := func(username string, verbs ...string) *session {
		t.Helper()
		role := i.customRole(admin, username+" role", "install", verbs...)
		s := i.user(username)
		require.Equal(t, http.StatusOK,
			i.do(admin, http.MethodPut, "/users/"+i.userID(s)+"/role", map[string]string{"role_id": role}).Code)
		return s
	}
	ci := grant("ci", "install.tokens.manage")
	hr := grant("hr", "install.users.manage", "install.view")

	created := i.do(ci, http.MethodPost, "/tokens/service", map[string]any{"name": "CI deploys"})
	require.Equal(t, http.StatusCreated, created.Code, created.String())
	var issued struct {
		Token struct {
			ID string `json:"id"`
		} `json:"token"`
	}
	created.JSON(t, &issued)
	tok := issued.Token
	require.NotEmpty(t, tok.ID)
	require.Equal(t, http.StatusOK, i.do(ci, http.MethodGet, "/tokens/service", nil).Code)
	require.Equal(t, http.StatusForbidden,
		i.do(ci, http.MethodPost, "/users", map[string]any{"username": "x", "password": "a-long-enough-password"}).Code,
		"managing tokens is not managing people")

	require.Equal(t, http.StatusForbidden, i.do(hr, http.MethodGet, "/tokens/service", nil).Code)
	require.Equal(t, http.StatusForbidden, i.do(hr, http.MethodDelete, "/tokens/"+tok.ID, nil).Code,
		"managing people is not managing service tokens")

	require.Equal(t, http.StatusNoContent, i.do(ci, http.MethodDelete, "/tokens/"+tok.ID, nil).Code)
}
