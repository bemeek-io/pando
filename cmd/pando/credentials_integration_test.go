//go:build integration

package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	adapterapi "github.com/bemeek-io/pando/internal/adapter/api"
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
	require.NoError(t, store.Upsert(ctx, state.AdapterConfig{
		ID: "ai_anthropic", Category: string(adapterapi.CategoryAI), Kind: "anthropic",
		Name: "Anthropic", IsDefault: true, Enabled: true,
	}))

	// Written around the API, the way someone editing the database by hand
	// would. The check constraint only refuses a `credentials` key, so this
	// row exists — and the adapter refuses to start from it.
	_, err := db.Exec(ctx, `
		INSERT INTO adapter_configs (id, category, kind, name, config, enabled)
		VALUES ('ai_plain', 'ai', 'anthropic', 'Plain', '{"api_key":"sk-ant-plain"}', true)`)
	require.NoError(t, err)

	registry, credentials, err := registerAdapters(ctx, db, store, state.NewNotifications(db), logger)
	require.NoError(t, err)
	_, found := registry.AI("ai_anthropic")
	require.False(t, found, "no key anywhere, so it does not register")

	require.NoError(t, credentials.Put(ctx, "ai_anthropic", "api_key", secret.New("sk-ant-sealed")))

	registry, _, err = registerAdapters(ctx, db, store, state.NewNotifications(db), logger)
	require.NoError(t, err)
	_, found = registry.AI("ai_anthropic")
	require.True(t, found, "the sealed credential reached Configure")
	_, found = registry.AI("ai_plain")
	require.False(t, found, "a key in plain configuration is refused, not used")
}
