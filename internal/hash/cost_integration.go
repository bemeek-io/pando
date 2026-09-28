//go:build integration

package hash

// argon2id's cost in the integration tests: the lowest the algorithm accepts.
//
// Every installation an integration test brings up hashes its administrator's
// password, and every sign-in verifies one. At the production cost that is
// tens of milliseconds each, a large share of the suite's run (issue #31).
// Nothing the tests assert depends on the cost, and because a hash records the
// parameters it was made with, this changes nothing about a hash made
// anywhere else. TestParametersAreEncodedInTheHash pins the cost that ships.
const (
	timeCost    = 1
	memoryCost  = 8 // KiB; the floor for parallelism 1
	parallelism = 1
)
