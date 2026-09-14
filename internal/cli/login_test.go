package cli_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/cli"
)

// R-058: `pando login` stores a token, not a session. A session is bound to a
// browser's lifetime and dies on a password change, which is right for a
// browser and wrong for a script that runs at 3am.
func TestR058_LoginExchangesASessionForAStoredToken(t *testing.T) {
	api := newAPI(t).
		reply("POST /sessions", map[string]any{"user_id": "usr_01HQ8"}).
		reply("POST /tokens", map[string]any{"secret": "tok_secret_value"})

	got := run(t, api, "ben\nhunter2\n", "login")
	require.NoError(t, got.err)

	require.JSONEq(t, `{"username":"ben","password":"hunter2"}`, api.bodyFor("POST /sessions"))
	require.Contains(t, api.bodyFor("POST /tokens"), "CLI", "the token is named for the machine")

	// The session has done its job. Revoking it leaves the token in the file as
	// the only credential on this machine.
	require.True(t, api.sawPath("/sessions"))
	var sawDelete bool
	for _, c := range api.calls {
		if c.method == http.MethodDelete && c.path == "/api/v1/sessions" {
			sawDelete = true
		}
	}
	require.True(t, sawDelete, "the session is revoked once the token exists")

	creds, err := cli.LoadCredentials()
	require.NoError(t, err)
	require.Equal(t, "tok_secret_value", creds.Token)
	require.Equal(t, "ben", creds.User)
	require.Equal(t, api.url, creds.URL)

	require.Contains(t, got.out, "Signed in to "+api.url)
	require.Contains(t, got.out, "credentials.json")
}

func TestLoginTakesTheUsernameFromAFlagAndOnlyPromptsForThePassword(t *testing.T) {
	api := newAPI(t).
		reply("POST /sessions", map[string]any{"user_id": "usr_01HQ8"}).
		reply("POST /tokens", map[string]any{"secret": "tok_secret_value"})

	got := run(t, api, "hunter2\n", "login", "--username", "ben")
	require.NoError(t, got.err)
	require.JSONEq(t, `{"username":"ben","password":"hunter2"}`, api.bodyFor("POST /sessions"))
	require.NotContains(t, got.errOut, "Username:")
}

func TestLoginTakesTheServerAsAPositionalArgument(t *testing.T) {
	api := newAPI(t).
		reply("POST /sessions", map[string]any{"user_id": "usr_01HQ8"}).
		reply("POST /tokens", map[string]any{"secret": "tok_secret_value"})

	got := run(t, api, "ben\nhunter2\n", "login", api.url+"/")
	require.NoError(t, got.err)

	creds, err := cli.LoadCredentials()
	require.NoError(t, err)
	require.Equal(t, api.url, creds.URL, "the trailing slash is trimmed")
}

// An account still on the password Pando generated cannot mint a token: the
// person has to choose their own first, and the console is where that happens.
func TestLoginStopsOnAnAccountThatMustChangeItsPassword(t *testing.T) {
	api := newAPI(t).reply("POST /sessions",
		map[string]any{"user_id": "usr_01HQ8", "must_change_password": true})

	got := run(t, api, "ben\ngenerated-password\n", "login")
	require.ErrorContains(t, got.err, "password Pando generated")
	require.ErrorContains(t, got.err, "web console")

	require.False(t, api.sawPath("/tokens"), "no token is minted")
	_, err := cli.LoadCredentials()
	require.ErrorIs(t, err, cli.ErrNotLoggedIn, "nothing is stored")
}

func TestLoginReportsBadCredentialsAndStoresNothing(t *testing.T) {
	api := newAPI(t).fail("POST /sessions", http.StatusUnauthorized, map[string]string{
		"code":    "AUTH_INVALID",
		"message": "That username and password do not match an account.",
	})

	got := run(t, api, "ben\nwrong\n", "login")
	require.ErrorContains(t, got.err, "do not match an account")

	_, err := cli.LoadCredentials()
	require.ErrorIs(t, err, cli.ErrNotLoggedIn)
}

func TestLoginReportsATokenThatCouldNotBeMinted(t *testing.T) {
	api := newAPI(t).
		reply("POST /sessions", map[string]any{"user_id": "usr_01HQ8"}).
		fail("POST /tokens", http.StatusForbidden, map[string]string{
			"code":    "PERM_DENIED",
			"message": "This install does not allow tokens.",
		})

	got := run(t, api, "ben\nhunter2\n", "login")
	require.ErrorContains(t, got.err, "does not allow tokens")

	_, err := cli.LoadCredentials()
	require.ErrorIs(t, err, cli.ErrNotLoggedIn)
}

func TestLoginReportsAnUnreachableServer(t *testing.T) {
	dead := &fakeAPI{t: t, url: "http://127.0.0.1:1", responses: map[string]any{}, status: map[string]int{}}
	got := run(t, dead, "ben\nhunter2\n", "login")
	require.Error(t, got.err)
}

// A prompt that reaches end of input must say so rather than proceeding with an
// empty username.
func TestLoginStopsWhenThereIsNoInputToRead(t *testing.T) {
	got := run(t, newAPI(t), "", "login")
	require.Error(t, got.err)
}

func TestNameFromURLHandlesTheFormsPeopleActuallyPaste(t *testing.T) {
	// Exercised through `app add`, which is the only caller.
	for _, tc := range []struct{ url, want string }{
		{"https://github.com/ben/notes.git", "notes"},
		{"https://github.com/ben/notes", "notes"},
		{"https://github.com/ben/notes/", "notes"},
		{"git@github.com:ben/notes.git", "notes"},
		{"notes", "notes"},
		// The last path segment, whatever it is. A bare host has none, so the
		// host itself is the best guess available — and --name overrides it.
		{"https://github.com/", "github.com"},
		// Nothing to name it after at all.
		{"/", "app"},
	} {
		api := newAPI(t).reply("POST /apps", map[string]any{"id": "app_01HQ8"})
		got := run(t, api, "", "app", "add", tc.url)
		require.NoError(t, got.err, tc.url)
		require.Contains(t, api.bodyFor("POST /apps"), `"name":`+jsonString(tc.want), tc.url)
	}
}
