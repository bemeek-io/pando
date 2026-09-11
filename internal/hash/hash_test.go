package hash_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/hash"
	"github.com/bemeek-io/pando/internal/secret"
)

func TestRoundTrip(t *testing.T) {
	h, err := hash.New(secret.New("correct horse battery staple"))
	require.NoError(t, err)

	ok, err := hash.Verify(secret.New("correct horse battery staple"), h)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = hash.Verify(secret.New("wrong password"), h)
	require.NoError(t, err)
	require.False(t, ok)
}

// The stored hash must not contain the password, and must be salted so that two
// users with the same password do not share a hash.
func TestHashIsSaltedAndDoesNotContainTheSecret(t *testing.T) {
	const password = "hunter2-THE-ACTUAL-SECRET"

	a, err := hash.New(secret.New(password))
	require.NoError(t, err)
	b, err := hash.New(secret.New(password))
	require.NoError(t, err)

	require.NotContains(t, a, password)
	require.NotEqual(t, a, b, "identical passwords must not produce identical hashes")
	require.True(t, strings.HasPrefix(a, "$argon2id$"))
}

// A corrupted row must deny access, not crash the login path.
func TestMalformedHashDeniesRatherThanPanics(t *testing.T) {
	for _, bad := range []string{
		"",
		"not-a-hash",
		"$argon2id$v=19$m=65536,t=3,p=2$onlyfourfields",
		"$bcrypt$v=19$m=65536,t=3,p=2$c2FsdA$a2V5",
		"$argon2id$v=999$m=65536,t=3,p=2$c2FsdA$a2V5",
	} {
		ok, err := hash.Verify(secret.New("x"), bad)
		require.False(t, ok, "must not verify %q", bad)
		require.Error(t, err)
	}
}

// Parameters live in the hash, so raising them later does not invalidate
// credentials already stored.
func TestParametersAreEncodedInTheHash(t *testing.T) {
	h, err := hash.New(secret.New("x"))
	require.NoError(t, err)
	require.Contains(t, h, "m=65536,t=3,p=2")
}
