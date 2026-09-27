//go:build !integration

package backup_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/trypando/pando/internal/core/backup"
)

// R-213: a bundle is protected by its passphrase alone, and a stolen one is
// ground offline, so what protects it is the KDF cost written into its header. Not built with the integration tag, whose
// tests encrypt at the lowest cost Decrypt accepts (kdf_cost_integration.go),
// so this runs in the build that has the cost a server does.
func TestTheShippedKDFCostIsWrittenIntoTheHeader(t *testing.T) {
	var sealed bytes.Buffer
	require.NoError(t, backup.Encrypt(&sealed, bytes.NewReader([]byte("x")), pass("right")))

	// magic (8) and salt (16), then time, memory and parallelism.
	header := sealed.Bytes()[8+16:]
	require.Equal(t, uint32(4), binary.BigEndian.Uint32(header[0:4]), "time cost")
	require.Equal(t, uint32(256*1024), binary.BigEndian.Uint32(header[4:8]), "memory cost, KiB")
	require.Equal(t, byte(4), header[8], "parallelism")
}
