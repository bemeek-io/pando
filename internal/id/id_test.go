package id_test

import (
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/id"
)

func TestNewIsPrefixedAndParseable(t *testing.T) {
	for _, k := range []id.Kind{id.App, id.Spec, id.User, id.Token, id.Volume, id.Grant, id.AdapterRuntime} {
		s := id.New(k)
		require.True(t, id.Is(k, s), "%s should be of kind %s", s, k)

		got, _, err := id.Parse(s)
		require.NoError(t, err)
		require.Equal(t, k, got)
	}
}

// A prefix that means nothing is a prefix that will be ignored. The point of
// carrying one is that a wrong-kind ID is rejected rather than silently missed.
func TestIsRejectsTheWrongKind(t *testing.T) {
	app := id.New(id.App)
	require.True(t, id.Is(id.App, app))
	require.False(t, id.Is(id.Volume, app))
	require.False(t, id.Is(id.User, app))
}

func TestParseRejectsMalformed(t *testing.T) {
	for _, bad := range []string{
		"",
		"app",
		"app_",
		"_01HQ8",
		"app_not-a-ulid",
		"01ARZ3NDEKTSV4RRFFQ69G5FAV", // valid ULID, no prefix
	} {
		_, _, err := id.Parse(bad)
		require.Error(t, err, "should reject %q", bad)
	}
}

// IDs sort chronologically, which cursor pagination depends on.
func TestNewSortsByCreationTime(t *testing.T) {
	var ids []string
	for range 50 {
		ids = append(ids, id.New(id.App))
		time.Sleep(time.Millisecond)
	}

	shuffled := append([]string(nil), ids...)
	sort.Sort(sort.Reverse(sort.StringSlice(shuffled)))
	sort.Strings(shuffled)

	require.Equal(t, ids, shuffled, "lexical sort should match creation order")
}

func TestTimeRecoversGenerationInstant(t *testing.T) {
	before := time.Now().UTC().Add(-time.Second)
	got, err := id.Time(id.New(id.App))
	require.NoError(t, err)
	require.WithinRange(t, got, before, time.Now().UTC().Add(time.Second))
}

func TestIDsAreUnique(t *testing.T) {
	seen := make(map[string]struct{}, 10_000)
	for range 10_000 {
		s := id.New(id.App)
		_, dup := seen[s]
		require.False(t, dup, "duplicate identifier %s", s)
		seen[s] = struct{}{}
	}
}
