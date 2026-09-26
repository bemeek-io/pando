package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestR271_MalformedAdapterDeclarationsStopStartup asserts R-271: a
// declaration Pando cannot read as written stops startup, and the error names
// the file, the key, and what would be accepted.
func TestR271_MalformedAdapterDeclarationsStopStartup(t *testing.T) {
	cases := []struct {
		name, yaml string
		want       []string
	}{
		{"section not a map", `
adapters: [ai_anthropic]
`, []string{"not a map of adapter IDs to adapters", "Valid answer"}},
		{"bad ID", `
adapters:
  ai-anthropic: {category: ai, kind: anthropic}
`, []string{`"ai-anthropic" is not an adapter ID`, "lowercase letters"}},
		{"body not a map", `
adapters:
  ai_anthropic: anthropic
`, []string{"adapters.ai_anthropic", "a map with at least a category and a kind"}},
		{"default not a bool", `
adapters:
  ai_anthropic: {category: ai, kind: anthropic, default: sometimes}
`, []string{"default must be true or false"}},
		{"enabled not a bool", `
adapters:
  ai_anthropic: {category: ai, kind: anthropic, enabled: [yes]}
`, []string{"enabled must be true or false"}},
		{"config not a map", `
adapters:
  ai_anthropic: {category: ai, kind: anthropic, config: claude-opus-5-5}
`, []string{"config must be a map"}},
		{"unknown field", `
adapters:
  ai_anthropic: {category: ai, kind: anthropic, region: us-east}
`, []string{`"region" is not an adapter setting`, "category, kind, name"}},
		{"missing category", `
adapters:
  ai_anthropic: {kind: anthropic}
`, []string{"needs a category and a kind"}},
		{"missing kind", `
adapters:
  ai_anthropic: {category: ai}
`, []string{"needs a category and a kind"}},
		{"functions of the wrong type", `
adapters:
  ai_anthropic: {category: ai, kind: anthropic, functions: repair_plan}
`, []string{"adapters.ai_anthropic.functions", "a list of AI function names"}},
		{"a list entry that is not a name", `
adapters:
  ai_anthropic: {category: ai, kind: anthropic, functions: [{repair_plan: {}}]}
`, []string{"each function in a list is a name"}},
		{"a function that is not {} or {model}", `
adapters:
  ai_anthropic:
    category: ai
    kind: anthropic
    functions:
      repair_plan: claude-opus-5-5
`, []string{"repair_plan must be {} or {model: NAME}"}},
		{"a function with an unknown setting", `
adapters:
  ai_anthropic:
    category: ai
    kind: anthropic
    functions:
      repair_plan: {temperature: 0}
`, []string{`repair_plan has "temperature"`, "only setting is model"}},
		{"an unknown function in a map", `
adapters:
  ai_anthropic:
    category: ai
    kind: anthropic
    functions:
      read_minds: {}
`, []string{`"read_minds" is not an AI function`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, path, err := loadYAML(t, tc.yaml)
			require.Error(t, err)
			require.Contains(t, err.Error(), path)
			for _, want := range tc.want {
				require.Contains(t, err.Error(), want)
			}
		})
	}
}

// TestR190_DeclaredCredentialsAreReferencesOnly asserts R-190: each declared
// credential names exactly one place to read it from, and nothing else.
func TestR190_DeclaredCredentialsAreReferencesOnly(t *testing.T) {
	cases := []struct {
		name, yaml, want string
	}{
		{"credentials not a map", `
adapters:
  ai_anthropic: {category: ai, kind: anthropic, credentials: [api_key]}
`, "credentials must be a map"},
		{"an unknown source", `
adapters:
  ai_anthropic:
    category: ai
    kind: anthropic
    credentials:
      api_key: {vault: kv/anthropic}
`, `credentials.api_key has "vault"; a credential is read from env or file`},
		{"both env and file", `
adapters:
  ai_anthropic:
    category: ai
    kind: anthropic
    credentials:
      api_key: {env: ANTHROPIC_API_KEY, file: /run/secrets/anthropic}
`, "credentials.api_key must name exactly one of env or file"},
		{"neither env nor file", `
adapters:
  ai_anthropic:
    category: ai
    kind: anthropic
    credentials:
      api_key: {}
`, "credentials.api_key must name exactly one of env or file"},
		{"a secrets adapter", `
adapters:
  secrets_local:
    category: secrets
    kind: local
    credentials:
      key: {file: /run/secrets/key}
`, "a secrets adapter cannot be given credentials"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := loadYAML(t, tc.yaml)
			require.ErrorContains(t, err, tc.want)
		})
	}

	cfg, _, err := loadYAML(t, `
adapters:
  ai_anthropic:
    category: ai
    kind: anthropic
    credentials:
      api_key: {file: /run/secrets/anthropic}
`)
	require.NoError(t, err)
	require.Equal(t, CredentialRef{File: "/run/secrets/anthropic"}, cfg.Adapters[0].Credentials["api_key"])
}

// An adapter declared without a name is named after its ID, and is enabled
// unless the file says otherwise.
func TestADeclaredAdapterDefaultsItsNameAndIsEnabled(t *testing.T) {
	cfg, _, err := loadYAML(t, `
adapters:
  ai_local: {category: ai, kind: local}
  ai_openai: {category: ai, kind: openai, name: OpenAI, enabled: false, default: true}
`)
	require.NoError(t, err)
	require.Len(t, cfg.Adapters, 2)
	require.Equal(t, "ai_local", cfg.Adapters[0].Name)
	require.True(t, cfg.Adapters[0].Enabled)
	require.Equal(t, "OpenAI", cfg.Adapters[1].Name)
	require.False(t, cfg.Adapters[1].Enabled)
	require.True(t, cfg.Adapters[1].Default)
}

// TestR271_ADisabledDeclarationDoesNotOverlap asserts R-271: only a
// declaration that takes effect can contradict another, so a disabled one is
// left out of every overlap check.
func TestR271_ADisabledDeclarationDoesNotOverlap(t *testing.T) {
	_, _, err := loadYAML(t, `
adapters:
  ai_a:
    category: ai
    kind: anthropic
    default: true
    functions: [search_audit]
  ai_b:
    category: ai
    kind: anthropic
    default: true
    enabled: false
    functions: [search_audit]
`)
	require.NoError(t, err)
}
