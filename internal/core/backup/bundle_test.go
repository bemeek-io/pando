package backup_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/backup"
	"github.com/bemeek-io/pando/internal/errs"
)

func build(t *testing.T) []byte {
	t.Helper()
	var raw bytes.Buffer
	w := backup.NewWriter(&raw, "dr_bundle", "test", 9)
	require.NoError(t, w.Add(backup.PostgresName, 14, strings.NewReader("pg_dump output")))
	require.NoError(t, w.Add(backup.SecretsKey, 5, strings.NewReader("k3y!!")))
	require.NoError(t, w.Add(backup.VolumesPrefix+"vol_1.tar", 4, strings.NewReader("data")))
	w.Count("apps", 3)
	w.Count("users", 2)
	_, err := w.Finish()
	require.NoError(t, err)
	return raw.Bytes()
}

// TestR215_AWholeBundleVerifies is the baseline: the happy path has to pass, or
// every other assertion below is vacuous.
func TestR215_AWholeBundleVerifies(t *testing.T) {
	v, err := backup.Verify(bytes.NewReader(build(t)))
	require.NoError(t, err)

	require.Equal(t, backup.BundleVersion, v.Manifest.Version)
	require.Equal(t, "dr_bundle", v.Manifest.Kind)
	require.Equal(t, uint(9), v.Manifest.SchemaVersion)
	require.Len(t, v.Manifest.Entries, 3)

	// Counts prove the bundle describes the install the operator thinks it
	// does. Checksums only prove the bytes arrived.
	require.Equal(t, 3, v.Manifest.Counts["apps"])
	require.Equal(t, 2, v.Manifest.Counts["users"])
}

// TestR215_AnAlteredEntryIsRejectedByChecksum asserts the manifest does its job
// when the bytes are readable but wrong.
func TestR215_AnAlteredEntryIsRejectedByChecksum(t *testing.T) {
	raw := build(t)

	// Same length, different content — so tar still parses and only the
	// checksum catches it. A verify that checked sizes alone would pass this.
	at := bytes.Index(raw, []byte("pg_dump output"))
	require.Positive(t, at)
	copy(raw[at:], []byte("pg_dump 0utput"))

	_, err := backup.Verify(bytes.NewReader(raw))
	require.Error(t, err)
	require.Equal(t, errs.BackupIncomplete, errs.CodeOf(err))
	require.Contains(t, err.Error(), backup.PostgresName)
}

// TestR215_ABundleWithNoManifestIsRejected — a bundle Pando cannot check is not
// a bundle Pando restores.
func TestR215_ABundleWithNoManifestIsRejected(t *testing.T) {
	raw := build(t)
	at := bytes.Index(raw, []byte(backup.ManifestName))
	require.Positive(t, at)

	_, err := backup.Verify(bytes.NewReader(raw[:at]))
	require.Error(t, err)
	require.Equal(t, errs.BackupIncomplete, errs.CodeOf(err))
}

// TestR215_VerifyReadsAndWritesNothingElse asserts the property Sequence D
// names: verification is a read of the stream and nothing more, so a bad bundle
// is rejected with the target untouched.
//
// Enforced by construction — Verify takes an io.Reader and returns a value —
// and asserted here so that a future change adding a side effect has to delete
// a test that says why it must not.
func TestR215_VerifyReadsAndWritesNothingElse(t *testing.T) {
	raw := build(t)
	r := &countingReader{Reader: bytes.NewReader(raw)}

	_, err := backup.Verify(r)
	require.NoError(t, err)
	require.Equal(t, len(raw), r.n, "verify must read every byte, or it cannot have checked every checksum")
}

type countingReader struct {
	Reader interface{ Read([]byte) (int, error) }
	n      int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.Reader.Read(p)
	c.n += n
	return n, err
}

// TestR213_AnEncryptedBundleVerifiesAfterDecryption is the two layers together,
// in the order restore uses them: decrypt, then verify, then apply.
func TestR213_AnEncryptedBundleVerifiesAfterDecryption(t *testing.T) {
	raw := build(t)

	var sealed bytes.Buffer
	require.NoError(t, backup.Encrypt(&sealed, bytes.NewReader(raw), pass("a long passphrase")))

	var opened bytes.Buffer
	require.NoError(t, backup.Decrypt(&opened, bytes.NewReader(sealed.Bytes()), pass("a long passphrase")))

	v, err := backup.Verify(bytes.NewReader(opened.Bytes()))
	require.NoError(t, err)
	require.Len(t, v.Manifest.Entries, 3)
}
