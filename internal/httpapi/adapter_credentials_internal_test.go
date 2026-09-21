package httpapi

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestO20_ACredentialInPlainConfigurationIsRefused asserts the API half of
// O-20's resolution. Configuration is stored and exported in the clear, so a
// credential sent there is refused with a message saying where it goes instead.
func TestO20_ACredentialInPlainConfigurationIsRefused(t *testing.T) {
	for _, cfg := range []string{
		`{"api_key":"sk-ant-x"}`,
		`{"credentials":{"api_key":"sk-ant-x"}}`,
		`{"model":"claude-opus-5","Token":"abc"}`,
		`{"password":"hunter2"}`,
	} {
		reason := inlineCredential(json.RawMessage(cfg))
		require.NotEmpty(t, reason, cfg)
		require.NotContains(t, reason, "sk-ant-x", "the refusal does not repeat the value")
	}

	for _, cfg := range []string{``, `{}`, `{"model":"claude-opus-5","api_key_env":"ANTHROPIC_API_KEY"}`} {
		require.Empty(t, inlineCredential(json.RawMessage(cfg)), cfg)
	}
}
