package anthropic

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/secret"
)

func secretOf(s string) secret.Value { return secret.New(s) }

// TestO20_TheKeyResolvesFromEncryptedStorageOrTheEnvironment asserts O-20's
// resolution: a key comes from the credential core decrypted, or from the
// environment, and from nowhere else.
func TestO20_TheKeyResolvesFromEncryptedStorageOrTheEnvironment(t *testing.T) {
	env := map[string]string{"PANDO_AI_KEY": "from-named", "ANTHROPIC_API_KEY": "from-default"}
	getenv := func(k string) string { return env[k] }

	stored := Config{Credentials: Credentials{APIKey: secretOf("stored")}, APIKeyEnv: "PANDO_AI_KEY"}
	require.Equal(t, "stored", resolveKey(stored, getenv).Reveal(),
		"the stored credential wins, because somebody set it deliberately")
	require.Equal(t, "from-named", resolveKey(Config{APIKeyEnv: "PANDO_AI_KEY"}, getenv).Reveal())
	require.Equal(t, "from-default", resolveKey(Config{}, getenv).Reveal(),
		"the SDK's own variable when nothing is named")
	require.True(t, resolveKey(Config{APIKeyEnv: "UNSET"}, getenv).IsZero(),
		"a named variable that is empty is not silently replaced by the default one")
}
