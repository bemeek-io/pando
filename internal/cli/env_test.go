package cli_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/cli"
)

// TestR262_AMachineCanAuthenticateFromTheEnvironment asserts that a minted
// token is usable by the things tokens exist for.
//
// `pando login` writes a file under the user's home directory, which is right
// for a person at a terminal and useless to CI, a container, or an MCP client
// that is given a command and an environment block and nothing else. Without
// this, the only way to use a token was to write that file by hand — so the
// documented way to connect an agent (R-262) did not work.
func TestR262_AMachineCanAuthenticateFromTheEnvironment(t *testing.T) {
	// No home directory to read credentials from: this is the CI case.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(cli.EnvServer, "https://pando.example.com/")
	t.Setenv(cli.EnvToken, "tok_01HQ8.secret")

	client, err := cli.New("")
	require.NoError(t, err)
	require.Equal(t, "https://pando.example.com", client.BaseURL, "the trailing slash is trimmed")
	require.Equal(t, "tok_01HQ8.secret", client.Token)
}

// The flag is the most deliberate of the three, so it wins.
func TestTheServerFlagBeatsTheEnvironment(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(cli.EnvServer, "https://from-the-environment.example")
	t.Setenv(cli.EnvToken, "tok_01HQ8.secret")

	client, err := cli.New("https://from-the-flag.example")
	require.NoError(t, err)
	require.Equal(t, "https://from-the-flag.example", client.BaseURL)
}

// With neither a file nor an environment, the error says both ways in.
func TestNotLoggedInSaysHowToFixIt(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(cli.EnvServer, "")
	t.Setenv(cli.EnvToken, "")

	_, err := cli.New("")
	require.ErrorIs(t, err, cli.ErrNotLoggedIn)
	require.Contains(t, err.Error(), cli.EnvServer)
}
