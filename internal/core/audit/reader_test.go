package audit_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/audit"
)

// The page size is bounded by the cap whatever the caller asks for, and is what
// the layer above must compare a returned page against.
//
// The cap is the reason List can size its result slice up front: the number
// comes from a query string, and without a ceiling a request for a billion
// records is a request for the server to allocate room for a billion records
// before reading the first one.
func TestPageSizeIsBounded(t *testing.T) {
	for _, tc := range []struct {
		requested int
		want      int
	}{
		{requested: 0, want: 100},
		{requested: -1, want: 100},
		{requested: 1, want: 1},
		{requested: 100, want: 100},
		{requested: 500, want: 500},
		{requested: 501, want: 500},
		{requested: 1_000_000_000, want: 500},
	} {
		require.Equal(t, tc.want, audit.PageSize(tc.requested),
			"PageSize(%d)", tc.requested)
	}
}
