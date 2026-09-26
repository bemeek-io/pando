//go:build integration

package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	adapterapi "github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/config"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/secret"
)

// TestO20_AnAdapterStartsFromAnEncryptedCredentialOnly asserts O-20's
// resolution at startup: an AI adapter whose key is stored only as ciphertext
// comes up — the secrets adapter is configured first, the credential decrypted
// and handed to Configure in memory — and one whose key was written into its
// plain configuration around the API does not come up at all.
func TestO20_AnAdapterStartsFromAnEncryptedCredentialOnly(t *testing.T) {
	ctx := context.Background()
	db := connected(t)
	store := state.NewAdapters(db)
	logger, _ := recorded()
	t.Setenv("ANTHROPIC_API_KEY", "") // a developer's own key must not make this pass

	require.NoError(t, store.Upsert(ctx, state.AdapterConfig{
		ID: "sek_local", Category: string(adapterapi.CategorySecrets), Kind: "local",
		Name: "Local storage", IsDefault: true, Enabled: true,
		Config: json.RawMessage(`{"key_path":` + quoted(t, t.TempDir()+"/secrets.key") + `}`),
	}))
	// Written around the API, the way someone editing the database by hand
	// would. The check constraint only refuses a `credentials` key, so this
	// row exists — and the adapter refuses to start from it.
	_, err := db.Exec(ctx, `
		INSERT INTO adapter_configs (id, category, kind, name, config, enabled)
		VALUES ('ai_plain', 'ai', 'anthropic', 'Plain', '{"api_key":"sk-ant-plain"}', true)`)
	require.NoError(t, err)

	registry, _, err := registerAdapters(ctx, db, store, state.NewNotifications(db), nil, logger)
	require.NoError(t, err)
	_, found := registry.AI("ai_plain")
	require.False(t, found, "a key in plain configuration is refused, not used")

	// One AI adapter per provider (R-259), so the plain one goes before the
	// sealed one is added.
	_, err = db.Exec(ctx, `DELETE FROM adapter_configs WHERE id = 'ai_plain'`)
	require.NoError(t, err)
	require.NoError(t, store.Upsert(ctx, state.AdapterConfig{
		ID: "ai_anthropic", Category: string(adapterapi.CategoryAI), Kind: "anthropic",
		Name: "Anthropic", IsDefault: true, Enabled: true,
	}))

	registry, credentials, err := registerAdapters(ctx, db, store, state.NewNotifications(db), nil, logger)
	require.NoError(t, err)
	_, found = registry.AI("ai_anthropic")
	require.False(t, found, "no key anywhere, so it does not register")

	require.NoError(t, credentials.Put(ctx, "ai_anthropic", "api_key", secret.New("sk-ant-sealed")))

	registry, _, err = registerAdapters(ctx, db, store, state.NewNotifications(db), nil, logger)
	require.NoError(t, err)
	_, found = registry.AI("ai_anthropic")
	require.True(t, found, "the sealed credential reached Configure")
}

// TestR271_ADeclaredAdapterOverridesAStoredOne asserts R-271 and R-190: an
// adapter declared in the config file starts from a credential read from the
// environment, and replaces a stored AI adapter of the same provider.
func TestR271_ADeclaredAdapterOverridesAStoredOne(t *testing.T) {
	ctx := context.Background()
	db := connected(t)
	store := state.NewAdapters(db)
	logger, _ := recorded()
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("PANDO_TEST_ANTHROPIC_KEY", "sk-ant-from-env")

	require.NoError(t, store.Upsert(ctx, state.AdapterConfig{
		ID: "sek_local", Category: string(adapterapi.CategorySecrets), Kind: "local",
		Name: "Local storage", IsDefault: true, Enabled: true,
		Config: json.RawMessage(`{"key_path":` + quoted(t, t.TempDir()+"/secrets.key") + `}`),
	}))
	require.NoError(t, store.Upsert(ctx, state.AdapterConfig{
		ID: "ai_stored", Category: string(adapterapi.CategoryAI), Kind: "anthropic", Name: "Stored", Enabled: true,
	}))

	declared := []config.AdapterDecl{{
		ID: "ai_declared", Category: "ai", Kind: "anthropic", Name: "Declared", Enabled: true,
		Credentials: map[string]config.CredentialRef{"api_key": {Env: "PANDO_TEST_ANTHROPIC_KEY"}},
		Source:      config.Source{Kind: "file", Name: "pando.yaml", Key: "adapters.ai_declared"},
	}}
	registry, _, err := registerAdapters(ctx, db, store, state.NewNotifications(db), declared, logger)
	require.NoError(t, err)
	_, found := registry.AI("ai_declared")
	require.True(t, found, "the credential came from the variable the file names")
	_, found = registry.AI("ai_stored")
	require.False(t, found, "one AI adapter per provider, and the file's wins")

	// A variable that is not set is a failure to configure: skipped, not fatal.
	declared[0].Credentials["api_key"] = config.CredentialRef{Env: "PANDO_TEST_UNSET_KEY"}
	registry, _, err = registerAdapters(ctx, db, store, state.NewNotifications(db), declared, logger)
	require.NoError(t, err)
	_, found = registry.AI("ai_declared")
	require.False(t, found)
}

// TestR271_DeclaredServicesAdaptersThatOverlapStopStartup asserts R-271: two
// declared services adapters that provide the same kind of service stop
// startup, naming both.
func TestR271_DeclaredServicesAdaptersThatOverlapStopStartup(t *testing.T) {
	ctx := context.Background()
	db := connected(t)
	store := state.NewAdapters(db)
	logger, _ := recorded()

	declared := []config.AdapterDecl{
		{ID: "svcs_one", Category: "services", Kind: "docker", Name: "One", Enabled: true,
			Source: config.Source{Kind: "file", Name: "pando.yaml", Key: "adapters.svcs_one"}},
		{ID: "svcs_two", Category: "services", Kind: "docker", Name: "Two", Enabled: true,
			Source: config.Source{Kind: "file", Name: "pando.yaml", Key: "adapters.svcs_two"}},
	}
	_, _, err := registerAdapters(ctx, db, store, state.NewNotifications(db), declared, logger)
	require.Error(t, err)
	require.Contains(t, err.Error(), "adapters.svcs_one")
	require.Contains(t, err.Error(), "adapters.svcs_two")
}
