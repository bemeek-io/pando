//go:build !integration

package backup

// KDF cost. Deliberately higher than the interactive login in internal/hash:
// unlocking a bundle happens once, in a disaster, and an attacker holding a
// stolen bundle has unlimited time. Encoded in the header so raising these
// later does not orphan existing bundles.
//
// kdf_cost_integration.go lowers it for the integration tests. A binary built
// without that tag — which is every binary that ships — always has these.
const (
	kdfTime        = 4
	kdfMemory      = 256 * 1024 // 256 MiB
	kdfParallelism = 4
)
