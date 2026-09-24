package detect

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/spec"
)

// A draft with nothing left to run is only backing services when it provisions
// something — every service became a slot. With neither workloads nor slots
// there is nothing to call a database. The compose importer does not produce
// such a draft today, so this is asserted on the function directly.
func TestADraftOfOnlySlotsIsOnlyBackingServices(t *testing.T) {
	require.True(t, onlyBackingServices(Draft{Slots: []spec.Slot{{Key: "DATABASE_URL", Type: spec.SlotPostgres}}}))
	require.False(t, onlyBackingServices(Draft{}))
}

// An import with no repository to read — a compose file handed over on its
// own — has no files under any mounted directory, so nothing is carried and
// the mount stays storage.
func TestAComposeImportWithNoRepositoryListsNoMountedFiles(t *testing.T) {
	c := &composeImport{}
	require.Nil(t, c.repoFiles("./conf"))
	require.False(t, c.isFile("./conf/app.conf"))
}
