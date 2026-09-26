package anthropic_test

import (
	"context"
	"encoding/json"
	"io"
	"path"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	anthropicadapter "github.com/bemeek-io/pando/internal/adapter/ai/anthropic"
	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/secret"
)

type memSource map[string]string

func (m memSource) Open(name string) (io.ReadCloser, error) {
	content, ok := m[path.Clean(name)]
	if !ok {
		return nil, io.EOF
	}
	return io.NopCloser(strings.NewReader(content)), nil
}

func (m memSource) Stat(name string) (api.FileInfo, error) {
	if content, ok := m[path.Clean(name)]; ok {
		return api.FileInfo{Name: path.Base(name), Size: int64(len(content))}, nil
	}
	return api.FileInfo{}, io.EOF
}

func (m memSource) Glob(pattern string) ([]string, error) {
	var out []string
	for name := range m {
		if ok, _ := path.Match(pattern, name); ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, nil
}

func configured(t *testing.T, cfg string) *anthropicadapter.Adapter {
	t.Helper()
	a := anthropicadapter.New()
	require.NoError(t, a.Configure(context.Background(), json.RawMessage(cfg)))
	return a
}

// TestR194_TheAPIKeyNeverRenders asserts R-194.
//
// secret.Value is the mechanism: String, MarshalJSON and the zap marshaler all
// return [redacted], so a config that is logged cannot carry the credential.
func TestR194_TheAPIKeyNeverRenders(t *testing.T) {
	var cfg anthropicadapter.Config
	require.NoError(t, json.Unmarshal([]byte(`{"credentials":{"api_key":"sk-ant-secret-value"}}`), &cfg))
	require.Equal(t, "sk-ant-secret-value", cfg.Credentials.APIKey.Reveal(), "the value is there")

	body, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NotContains(t, string(body), "sk-ant-secret-value")
	require.Contains(t, string(body), secret.Redacted)
}

// TestR259_CapabilitiesAreDataNotATypeAssertion asserts R-259 and R-254.
func TestR259_CapabilitiesAreDataNotATypeAssertion(t *testing.T) {
	a := configured(t, `{"credentials":{"api_key":"sk-ant-test"}}`)

	caps, err := a.Capabilities(context.Background())
	require.NoError(t, err)
	require.True(t, caps.Does(api.AIFunctionRepairPlan))
	require.True(t, caps.Does(api.AIFunctionAnswerQuestions))
	require.True(t, caps.Does(api.AIFunctionRevisePlan))
	require.False(t, caps.Does(api.AIFunctionReadReadme), "not built, and it says so")
	require.Equal(t, anthropicadapter.DefaultModel, caps.Model)
}

// TestR336_BothFunctionsAreOnWhenTheAdapterIsConfigured asserts R-336.
//
// Configuring the adapter meant supplying a credential, and that was the
// decision. Asking a second time would charge the setup cost twice (R-002).
// When each function is called is core's business, not the adapter's.
func TestR336_BothFunctionsAreOnWhenTheAdapterIsConfigured(t *testing.T) {
	on := configured(t, `{"credentials":{"api_key":"sk-ant-test"}}`)
	caps, err := on.Capabilities(context.Background())
	require.NoError(t, err)
	require.True(t, caps.Does(api.AIFunctionRepairPlan))
	require.True(t, caps.Does(api.AIFunctionAnswerQuestions))

	off := configured(t, `{"credentials":{"api_key":"sk-ant-test"},"screen_plans":false}`)
	caps, err = off.Capabilities(context.Background())
	require.NoError(t, err)
	require.Empty(t, caps.Functions, "an install may keep the adapter and turn both functions off")
}

// TestAnAdapterWithNoCredentialRefusesToConfigure asserts design 10 §7.
//
// Registering it would put a permanently unhealthy adapter in the console with
// no way to tell it from a provider outage.
func TestAnAdapterWithNoCredentialRefusesToConfigure(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "") // a developer's own key must not make this pass by accident
	a := anthropicadapter.New()
	err := a.Configure(context.Background(), json.RawMessage(`{"model":"claude-opus-5"}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "api_key")
}

// TestO20_AKeyInTheStoredConfigurationIsRefused asserts O-20's resolution.
//
// The stored configuration is unencrypted. A key found there is refused rather
// than used, so a row written around the API cannot quietly work.
func TestO20_AKeyInTheStoredConfigurationIsRefused(t *testing.T) {
	err := anthropicadapter.New().Configure(context.Background(),
		json.RawMessage(`{"api_key":"sk-ant-plaintext"}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "unencrypted")
	require.NotContains(t, err.Error(), "sk-ant-plaintext", "and the error does not repeat it")
}

// TestR335_AnUnconfiguredAdapterFailsRatherThanPretends asserts R-335.
//
// The failure is an error the caller turns into a skipped screening, which
// leaves the deterministic proposal exactly as it was.
func TestR335_AnUnconfiguredAdapterFailsRatherThanPretends(t *testing.T) {
	_, err := anthropicadapter.New().RepairPlan(context.Background(), api.ScreenRequest{
		Source: memSource{"a.txt": "x"},
	})
	require.Error(t, err)
}

// TestR020_ScreeningNeedsAReadableRepository asserts R-020.
func TestR020_ScreeningNeedsAReadableRepository(t *testing.T) {
	a := configured(t, `{"credentials":{"api_key":"sk-ant-test"}}`)
	_, err := a.RepairPlan(context.Background(), api.ScreenRequest{Source: nil})
	require.Error(t, err)
	require.Contains(t, err.Error(), "repository")
}

// TestR339_TheBudgetIsTheLowerOfWhatCoreAsksAndWhatTheAdapterWillDo asserts
// R-339: neither side raises the other's.
func TestR339_TheBudgetIsTheLowerOfWhatCoreAsksAndWhatTheAdapterWillDo(t *testing.T) {
	a := configured(t, `{"credentials":{"api_key":"sk-ant-test"},"max_files":5,"max_bytes":1024}`)
	caps, err := a.Capabilities(context.Background())
	require.NoError(t, err)
	require.Equal(t, 5, caps.MaxFiles)
	require.Equal(t, int64(1024), caps.MaxBytes)
	require.Less(t, caps.MaxFiles, anthropicadapter.DefaultMaxFiles,
		"an install narrows its own adapter, and core narrows it again")
}
