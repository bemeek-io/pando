package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func loadYAML(t *testing.T, body string) (*Config, string, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pando.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	t.Setenv("PANDO_DATABASE_URL", "postgres://pando:secret@db/pando")
	cfg, err := Load(path)
	return cfg, path, err
}

// TestR271_AdaptersAndAIFunctionsCanBeDeclaredInTheConfigFile asserts R-271:
// an install kept as code declares its adapters and which AI functions each
// handles, with every declaration's source recorded.
func TestR271_AdaptersAndAIFunctionsCanBeDeclaredInTheConfigFile(t *testing.T) {
	cfg, path, err := loadYAML(t, `
adapters:
  ai_anthropic:
    category: ai
    kind: anthropic
    name: Anthropic
    config:
      model: claude-opus-5-5
    credentials:
      api_key: {env: ANTHROPIC_API_KEY}
    functions:
      repair_plan: {}
      search_audit: {model: claude-haiku-4-5}
  ai_openai:
    category: ai
    kind: openai
    functions: [answer_reference]
`)
	require.NoError(t, err)
	require.Len(t, cfg.Adapters, 2)

	a := cfg.Adapters[0]
	require.Equal(t, "ai_anthropic", a.ID)
	require.Equal(t, Source{Kind: "file", Name: path, Key: "adapters.ai_anthropic"}, a.Source)
	require.Equal(t, CredentialRef{Env: "ANTHROPIC_API_KEY"}, a.Credentials["api_key"])
	require.Equal(t, "claude-opus-5-5", a.Config["model"])
	require.Equal(t, []FunctionDecl{
		{Function: "repair_plan", Source: Source{Kind: "file", Name: path, Key: "adapters.ai_anthropic.functions.repair_plan"}},
		{Function: "search_audit", Model: "claude-haiku-4-5", Source: Source{Kind: "file", Name: path, Key: "adapters.ai_anthropic.functions.search_audit"}},
	}, a.Functions)
	require.Equal(t, "answer_reference", cfg.Adapters[1].Functions[0].Function)

	for _, s := range cfg.Settings {
		require.NotContains(t, s.Key, "adapters", "declared adapters are reported on their own")
	}
}

// TestR271_ConfigDeclaredAIFunctionOverlapStopsStartup asserts R-271 and
// R-259: two declared adapters handling one AI function stop startup, and the
// error names both declarations.
func TestR271_ConfigDeclaredAIFunctionOverlapStopsStartup(t *testing.T) {
	_, _, err := loadYAML(t, `
adapters:
  ai_anthropic:
    category: ai
    kind: anthropic
    functions: [search_audit]
  ai_openai:
    category: ai
    kind: openai
    functions: [search_audit]
`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "adapters.ai_anthropic.functions.search_audit")
	require.Contains(t, err.Error(), "adapters.ai_openai.functions.search_audit")
}

// TestR271_ConfigDeclaredOverlapStopsStartupInEveryCategory asserts R-271:
// two defaults in a category, and two AI adapters of one provider, stop
// startup naming both declarations.
func TestR271_ConfigDeclaredOverlapStopsStartupInEveryCategory(t *testing.T) {
	_, _, err := loadYAML(t, `
adapters:
  rt_one: {category: runtime, kind: docker, default: true}
  rt_two: {category: runtime, kind: docker, default: true}
`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "adapters.rt_one")
	require.Contains(t, err.Error(), "adapters.rt_two")
	require.Contains(t, err.Error(), "one default")

	_, _, err = loadYAML(t, `
adapters:
  ai_a: {category: ai, kind: anthropic}
  ai_b: {category: ai, kind: anthropic}
`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "adapters.ai_a")
	require.Contains(t, err.Error(), "adapters.ai_b")
	require.Contains(t, err.Error(), "one AI adapter per provider")
}

// TestR190_ConfigFileNeverHoldsACredential asserts R-190: a credential
// written into the config file, in either place it might go, stops startup
// with the way to name it instead.
func TestR190_ConfigFileNeverHoldsACredential(t *testing.T) {
	_, _, err := loadYAML(t, `
adapters:
  ai_anthropic:
    category: ai
    kind: anthropic
    credentials:
      api_key: sk-ant-123
`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "written inline")
	require.NotContains(t, err.Error(), "sk-ant-123", "the value is never repeated back")

	_, _, err = loadYAML(t, `
adapters:
  ai_anthropic:
    category: ai
    kind: anthropic
    config: {api_key: sk-ant-123}
`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "looks like a credential")
	require.NotContains(t, err.Error(), "sk-ant-123")
}

func TestUnknownAIFunctionOrCategoryStopsStartup(t *testing.T) {
	_, _, err := loadYAML(t, `
adapters:
  ai_anthropic: {category: ai, kind: anthropic, functions: [read_minds]}
`)
	require.ErrorContains(t, err, `"read_minds" is not an AI function`)

	_, _, err = loadYAML(t, `
adapters:
  x_one: {category: teleport, kind: star}
`)
	require.ErrorContains(t, err, `"teleport" is not an adapter category`)

	_, _, err = loadYAML(t, `
adapters:
  rt_one: {category: runtime, kind: docker, functions: [repair_plan]}
`)
	require.ErrorContains(t, err, "functions are AI functions")
}
