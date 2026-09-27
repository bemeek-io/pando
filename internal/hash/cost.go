//go:build !integration

package hash

// argon2id's cost, tuned for an interactive login on a single host.
//
// cost_integration.go lowers it for the integration tests, which hash and
// verify a password for every installation they bring up. A binary built
// without that tag — which is every binary that ships — always has these.
const (
	timeCost    = 3
	memoryCost  = 64 * 1024 // 64 MiB
	parallelism = 2
)
