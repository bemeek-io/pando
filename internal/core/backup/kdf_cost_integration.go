//go:build integration

package backup

// KDF cost in the integration tests: the lowest Decrypt accepts.
//
// Every bundle an integration test writes or opens derives a key, and at the
// production cost — 256 MiB, four passes — that is most of the time those
// tests take under the race detector (issue #31). The cost is in each bundle's
// header, so a bundle made here still opens anywhere, and it still has to pass
// the same floor Decrypt holds every header to.
// TestTheShippedKDFCostIsWrittenIntoTheHeader pins the cost that ships.
const (
	kdfTime        = 1
	kdfMemory      = 8 * 1024 // 8 MiB, Decrypt's floor
	kdfParallelism = 1
)
