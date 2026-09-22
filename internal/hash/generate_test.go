package hash_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/hash"
)

// TestR046_GeneratedPasswordsAreLongAndMixed asserts the generator: 18 to 22
// characters, every class present, nothing that breaks when pasted, and never
// the same twice.
func TestR046_GeneratedPasswordsAreLongAndMixed(t *testing.T) {
	seen := map[string]bool{}
	lengths := map[int]bool{}
	for range 500 {
		v, err := hash.Generate()
		require.NoError(t, err)
		s := v.Reveal()

		require.GreaterOrEqual(t, len(s), hash.GeneratedMin)
		require.LessOrEqual(t, len(s), hash.GeneratedMax)
		require.True(t, strings.ContainsAny(s, "abcdefghijklmnopqrstuvwxyz"), s)
		require.True(t, strings.ContainsAny(s, "ABCDEFGHIJKLMNOPQRSTUVWXYZ"), s)
		require.True(t, strings.ContainsAny(s, "0123456789"), s)
		require.True(t, strings.ContainsAny(s, "!@#$%^&*-_=+?~.,:;"), s)
		require.False(t, strings.ContainsAny(s, "\"'`\\ "), s)
		require.GreaterOrEqual(t, len(s), hash.MinPasswordLength)

		require.False(t, seen[s], "repeated: %s", s)
		seen[s] = true
		lengths[len(s)] = true
	}
	require.Len(t, lengths, hash.GeneratedMax-hash.GeneratedMin+1, "every length in range occurs")
}
