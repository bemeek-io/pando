package cli_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// R-046: an account created or reset from the CLI gets the server's generated
// password, printed once, and must change it at first sign-in unless told not
// to.
func TestR046_UserCreateAndResetUseAGeneratedPassword(t *testing.T) {
	api := newAPI(t).reply("POST /passwords/generate", map[string]string{"password": "Zq7!generated-pass"})

	got := run(t, api, "", "user", "create", "dana", "--name", "Dana")
	require.NoError(t, got.err, got.errOut)
	require.Contains(t, got.out, "Zq7!generated-pass")
	require.JSONEq(t,
		`{"username":"dana","display_name":"Dana","email":"","password":"Zq7!generated-pass","must_change_password":true}`,
		api.bodyFor("POST /users"))

	got = run(t, api, "", "user", "reset-password", "usr_01", "--no-change-required")
	require.NoError(t, got.err, got.errOut)
	require.Contains(t, got.out, "Zq7!generated-pass")
	require.JSONEq(t, `{"password":"Zq7!generated-pass","must_change_password":false}`,
		api.bodyFor("POST /users/usr_01/password"))
}
