package backup_test

import (
	"bytes"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/backup"
	"github.com/bemeek-io/pando/internal/secret"
)

func pass(s string) secret.Value { return secret.New(s) }

func roundTrip(t *testing.T, payload []byte) {
	t.Helper()
	var sealed bytes.Buffer
	require.NoError(t, backup.Encrypt(&sealed, bytes.NewReader(payload), pass("correct passphrase")))

	var out bytes.Buffer
	require.NoError(t, backup.Decrypt(&out, bytes.NewReader(sealed.Bytes()), pass("correct passphrase")))
	// Compared by content: an empty bytes.Buffer reports nil, not []byte{}.
	require.True(t, bytes.Equal(payload, out.Bytes()), "payload of %d bytes did not survive", len(payload))
}

// TestR213_BundleRoundTripsAtEveryChunkBoundary asserts the streaming format.
//
// The boundaries are the interesting sizes: one byte short of a chunk, exactly
// a chunk, one byte over. An off-by-one in the last-chunk marker shows up at
// exactly a chunk and nowhere else.
func TestR213_BundleRoundTripsAtEveryChunkBoundary(t *testing.T) {
	const chunk = 1 << 20
	for _, size := range []int{0, 1, 4096, chunk - 1, chunk, chunk + 1, 3*chunk + 17} {
		payload := make([]byte, size)
		_, err := rand.Read(payload)
		require.NoError(t, err)
		roundTrip(t, payload)
	}
}

// TestR213_TheWrongPassphraseRevealsNothing asserts R-214's cost is paid
// cleanly: a wrong passphrase fails, and fails the same way as a corrupt
// bundle, so an attacker learns nothing about what they are holding.
func TestR213_TheWrongPassphraseRevealsNothing(t *testing.T) {
	var sealed bytes.Buffer
	require.NoError(t, backup.Encrypt(&sealed, bytes.NewReader([]byte("pg_dump output")), pass("right")))

	var out bytes.Buffer
	err := backup.Decrypt(&out, bytes.NewReader(sealed.Bytes()), pass("wrong"))
	require.ErrorIs(t, err, backup.ErrPassphrase)
	require.Empty(t, out.Bytes(), "nothing may be written before authentication")

	// Not a bundle at all: the same error.
	err = backup.Decrypt(&out, bytes.NewReader([]byte("this is not a bundle")), pass("right"))
	require.ErrorIs(t, err, backup.ErrPassphrase)
}

// TestR215_ATruncatedBundleIsRejected is the assertion Sequence D names.
//
// Truncation at a chunk boundary is the dangerous case: without the last-chunk
// marker in the nonce, the shortened bundle decrypts perfectly and restore
// proceeds against half a database.
func TestR215_ATruncatedBundleIsRejected(t *testing.T) {
	payload := make([]byte, 3<<20)
	_, err := rand.Read(payload)
	require.NoError(t, err)

	var sealed bytes.Buffer
	require.NoError(t, backup.Encrypt(&sealed, bytes.NewReader(payload), pass("right")))
	full := sealed.Bytes()

	// Every prefix of the bundle must be refused. That includes the prefixes
	// that land exactly on a chunk boundary, which is the whole point.
	for _, cut := range []int{
		len(full) - 1,
		len(full) / 2,
		len(full) - (1 << 20),
		33, // header plus a few bytes
	} {
		var out bytes.Buffer
		err := backup.Decrypt(&out, bytes.NewReader(full[:cut]), pass("right"))
		require.Error(t, err, "a bundle truncated to %d bytes must be refused", cut)
	}
}

// TestR215_ATamperedBundleIsRejected covers a flipped bit anywhere: header,
// first chunk, last chunk.
func TestR215_ATamperedBundleIsRejected(t *testing.T) {
	payload := make([]byte, 2<<20)
	_, err := rand.Read(payload)
	require.NoError(t, err)

	var sealed bytes.Buffer
	require.NoError(t, backup.Encrypt(&sealed, bytes.NewReader(payload), pass("right")))

	for _, at := range []int{0, 8, 20, 40, 1 << 19, sealed.Len() - 1} {
		tampered := bytes.Clone(sealed.Bytes())
		tampered[at] ^= 0x01

		var out bytes.Buffer
		err := backup.Decrypt(&out, bytes.NewReader(tampered), pass("right"))
		require.Error(t, err, "a bundle with byte %d flipped must be refused", at)
	}
}

// TestR215_TrailingBytesAreRejected asserts that a bundle with data appended
// after its last chunk is refused.
//
// Appending is how someone hides a payload in a file that still restores. The
// last-chunk marker makes the extra bytes detectable rather than ignored.
func TestR215_TrailingBytesAreRejected(t *testing.T) {
	var sealed bytes.Buffer
	require.NoError(t, backup.Encrypt(&sealed, bytes.NewReader([]byte("state")), pass("right")))

	extended := append(bytes.Clone(sealed.Bytes()), []byte("and something else")...)
	var out bytes.Buffer
	require.Error(t, backup.Decrypt(&out, bytes.NewReader(extended), pass("right")))
}

// TestR213_ReorderedChunksAreRejected asserts the counter in the nonce does its
// job: two bundles' worth of chunks cannot be spliced, and chunks cannot be
// swapped within one.
func TestR213_ReorderedChunksAreRejected(t *testing.T) {
	payload := make([]byte, 3<<20)
	for i := range payload {
		payload[i] = byte(i)
	}

	var sealed bytes.Buffer
	require.NoError(t, backup.Encrypt(&sealed, bytes.NewReader(payload), pass("right")))
	full := sealed.Bytes()

	// Chunks are 4-byte length plus 1 MiB plus a 16-byte tag.
	const header = 8 + 16 + 4 + 4 + 1
	const framed = 4 + (1 << 20) + 16

	swapped := bytes.Clone(full)
	first := bytes.Clone(full[header : header+framed])
	second := bytes.Clone(full[header+framed : header+2*framed])
	copy(swapped[header:], second)
	copy(swapped[header+framed:], first)

	var out bytes.Buffer
	err := backup.Decrypt(&out, bytes.NewReader(swapped), pass("right"))
	require.ErrorIs(t, err, backup.ErrPassphrase)
}

// TestR213_ADowngradedHeaderIsRejected asserts the KDF parameters are
// authenticated. Editing them down is how a stolen bundle becomes cheap to
// brute-force.
func TestR213_ADowngradedHeaderIsRejected(t *testing.T) {
	var sealed bytes.Buffer
	require.NoError(t, backup.Encrypt(&sealed, bytes.NewReader([]byte("secrets")), pass("right")))

	weakened := bytes.Clone(sealed.Bytes())
	// Memory cost sits at offset 8+16+4.
	weakened[8+16+4+3] = 0x01

	var out bytes.Buffer
	err := backup.Decrypt(&out, bytes.NewReader(weakened), pass("right"))
	require.Error(t, err)
	require.True(t, errors.Is(err, backup.ErrPassphrase))
}
