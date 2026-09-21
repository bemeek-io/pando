package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/secret"
)

// TestO20_CredentialsReachTheAdapterInMemoryOnly asserts the startup half of
// O-20's resolution: decrypted credentials are added under `credentials` for
// Configure, beside the stored configuration and without altering it.
func TestO20_CredentialsReachTheAdapterInMemoryOnly(t *testing.T) {
	stored := json.RawMessage(`{"model":"claude-opus-5"}`)

	merged, err := withCredentials(stored, map[string]secret.Value{"api_key": secret.New("sk-ant-x")})
	require.NoError(t, err)

	var got struct {
		Model       string            `json:"model"`
		Credentials map[string]string `json:"credentials"`
	}
	require.NoError(t, json.Unmarshal(merged, &got))
	require.Equal(t, "claude-opus-5", got.Model)
	require.Equal(t, "sk-ant-x", got.Credentials["api_key"],
		"revealed for Configure — secret.Value would otherwise marshal as [redacted]")
	require.JSONEq(t, `{"model":"claude-opus-5"}`, string(stored), "the stored configuration is untouched")

	same, err := withCredentials(stored, nil)
	require.NoError(t, err)
	require.Equal(t, stored, same, "an adapter with no credentials gets its configuration as stored")
}
