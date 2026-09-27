//go:build !integration

package hash_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/trypando/pando/internal/hash"
	"github.com/trypando/pando/internal/secret"
)

// Parameters live in the hash, so raising them later does not invalidate
// credentials already stored.
//
// Also what pins the cost that ships. Not built with the integration tag,
// whose tests hash at the lowest cost argon2 accepts (cost_integration.go), so
// this runs in the build that has the cost a server does.
func TestParametersAreEncodedInTheHash(t *testing.T) {
	h, err := hash.New(secret.New("x"))
	require.NoError(t, err)
	require.Contains(t, h, "m=65536,t=3,p=2")
}
