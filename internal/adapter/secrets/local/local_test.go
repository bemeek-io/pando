package local_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/adapter/secrets/local"
	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/secret"
)

func keyConfig(t *testing.T, path string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(local.Config{KeyPath: path})
	require.NoError(t, err)
	return raw
}

func configured(t *testing.T) (*local.Adapter, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secrets.key")
	a := local.New()
	require.NoError(t, a.Configure(context.Background(), keyConfig(t, path)))
	return a, path
}

func TestIdentity(t *testing.T) {
	a := local.New()
	require.Equal(t, local.Kind, a.Kind())
	require.Equal(t, api.CategorySecrets, a.Category())
}

// R-190: a secret is encrypted at rest and comes back unchanged.
func TestR190_SecretsRoundTripThroughCiphertext(t *testing.T) {
	a, _ := configured(t)
	ref := api.SecretRef{AppID: "app_01HQ8", Key: "DATABASE_URL"}

	stored, err := a.Put(context.Background(), ref, secret.New("postgres://user:pw@db/app"))
	require.NoError(t, err)
	require.Equal(t, ref.AppID, stored.AppID)
	require.Equal(t, ref.Key, stored.Key)
	require.NotContains(t, string(stored.Ciphertext), "postgres://",
		"the plaintext is not in what gets written to the database")

	got, err := a.Get(context.Background(), stored)
	require.NoError(t, err)
	require.Equal(t, "postgres://user:pw@db/app", got.Reveal())
}

// The same plaintext encrypts differently every time, so equal ciphertexts in
// the database do not reveal that two apps share a credential.
func TestEachCiphertextIsDistinct(t *testing.T) {
	a, _ := configured(t)
	ref := api.SecretRef{AppID: "app_01HQ8", Key: "TOKEN"}

	first, err := a.Put(context.Background(), ref, secret.New("same value"))
	require.NoError(t, err)
	second, err := a.Put(context.Background(), ref, secret.New("same value"))
	require.NoError(t, err)

	require.NotEqual(t, first.Ciphertext, second.Ciphertext)
}

// A ciphertext lifted from one app's row must not decrypt as another's. Without
// the app ID and key bound in, a row swap in the database would silently hand
// an app someone else's credential.
func TestACiphertextIsBoundToItsAppAndKey(t *testing.T) {
	a, _ := configured(t)

	stored, err := a.Put(context.Background(),
		api.SecretRef{AppID: "app_01HQ8", Key: "TOKEN"}, secret.New("value"))
	require.NoError(t, err)

	moved := stored
	moved.AppID = "app_OTHER"
	_, err = a.Get(context.Background(), moved)
	require.Error(t, err)

	renamed := stored
	renamed.Key = "OTHER_KEY"
	_, err = a.Get(context.Background(), renamed)
	require.Error(t, err)
}

// A decryption failure says nothing about which of the three causes it was:
// the key changed, the data was tampered with, or the row moved.
func TestADecryptionFailureDoesNotSayWhy(t *testing.T) {
	a, _ := configured(t)
	ref := api.SecretRef{AppID: "app_01HQ8", Key: "TOKEN"}

	stored, err := a.Put(context.Background(), ref, secret.New("value"))
	require.NoError(t, err)

	tampered := stored
	tampered.Ciphertext = append([]byte(nil), stored.Ciphertext...)
	tampered.Ciphertext[len(tampered.Ciphertext)-1] ^= 0xFF

	_, err = a.Get(context.Background(), tampered)
	require.Equal(t, errs.Internal, errs.CodeOf(err))
	require.Contains(t, errs.As(err).Message, "different encryption key")

	// Truncated below the nonce is caught before the AEAD sees it.
	short := stored
	short.Ciphertext = stored.Ciphertext[:2]
	_, err = a.Get(context.Background(), short)
	require.Equal(t, errs.Internal, errs.CodeOf(err))
}

// A secret written under one key cannot be read after the key is replaced —
// which is why losing the key file loses the secrets, and why R-212 puts it in
// the DR bundle.
func TestAReplacedKeyCannotReadOldSecrets(t *testing.T) {
	a, path := configured(t)
	stored, err := a.Put(context.Background(),
		api.SecretRef{AppID: "app_01HQ8", Key: "TOKEN"}, secret.New("value"))
	require.NoError(t, err)

	require.NoError(t, os.Remove(path))
	fresh := local.New()
	require.NoError(t, fresh.Configure(context.Background(), keyConfig(t, path)))

	_, err = fresh.Get(context.Background(), stored)
	require.Error(t, err)
}

func TestTheKeyIsGeneratedOnceAndReused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "secrets.key")

	a := local.New()
	require.NoError(t, a.Configure(context.Background(), keyConfig(t, path)))

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.EqualValues(t, local.KeySize, info.Size())
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm(),
		"the key is the only thing making the ciphertext worth anything")

	first, err := os.ReadFile(path)
	require.NoError(t, err)

	// A second adapter over the same path adopts the key rather than rotating
	// it — restarting Pando must not lose every stored secret.
	b := local.New()
	require.NoError(t, b.Configure(context.Background(), keyConfig(t, path)))
	second, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, first, second)

	stored, err := a.Put(context.Background(),
		api.SecretRef{AppID: "app_01HQ8", Key: "K"}, secret.New("v"))
	require.NoError(t, err)
	got, err := b.Get(context.Background(), stored)
	require.NoError(t, err)
	require.Equal(t, "v", got.Reveal())
}

func TestAKeyOfTheWrongSizeIsRefusedRatherThanPadded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.key")
	require.NoError(t, os.WriteFile(path, []byte("too short"), 0o600))

	err := local.New().Configure(context.Background(), keyConfig(t, path))
	require.Equal(t, errs.Internal, errs.CodeOf(err))
	require.Contains(t, errs.As(err).Remedy, "backup",
		"R-105: the remedy says what to do, and that the secrets are unreadable without it")
}

func TestAnUnreadableKeyPathIsReported(t *testing.T) {
	// A directory where the key file should be: readable as a path, not as a key.
	dir := filepath.Join(t.TempDir(), "secrets.key")
	require.NoError(t, os.Mkdir(dir, 0o700))

	err := local.New().Configure(context.Background(), keyConfig(t, dir))
	require.Equal(t, errs.Internal, errs.CodeOf(err))
}

func TestConfigureRejectsAMalformedDocument(t *testing.T) {
	err := local.New().Configure(context.Background(), json.RawMessage(`{`))
	require.Equal(t, errs.ValidInvalid, errs.CodeOf(err))
}

func TestAnUnconfiguredAdapterIsUnavailableRatherThanPanicking(t *testing.T) {
	a := local.New()
	require.Equal(t, errs.AdapterUnavailable, errs.CodeOf(a.HealthCheck(context.Background())))

	_, err := a.Put(context.Background(), api.SecretRef{AppID: "app_01HQ8", Key: "K"}, secret.New("v"))
	require.Equal(t, errs.AdapterUnavailable, errs.CodeOf(err))

	_, err = a.Get(context.Background(), api.StoredRef{AppID: "app_01HQ8", Key: "K"})
	require.Equal(t, errs.AdapterUnavailable, errs.CodeOf(err))
}

func TestConfiguredIsHealthy(t *testing.T) {
	a, _ := configured(t)
	require.NoError(t, a.HealthCheck(context.Background()))
}

// The ciphertext lives in Pando's own row, so core removes it and the adapter
// has nothing remote to delete.
func TestDeleteIsANoOp(t *testing.T) {
	a, _ := configured(t)
	require.NoError(t, a.Delete(context.Background(), api.StoredRef{AppID: "app_01HQ8", Key: "K"}))
}
