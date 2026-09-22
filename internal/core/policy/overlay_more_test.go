package policy_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/policy"
)

func file(key string, value any) policy.Setting {
	return policy.Setting{Key: key, Value: value, Source: policy.Source{Kind: "file", Name: "/etc/pando.yaml", Key: "policy." + key}}
}

// Values from the config file arrive typed; each has to read as its field.
func TestR271_FileValuesMustReadAsTheirField(t *testing.T) {
	o, err := policy.NewOverlay([]policy.Setting{
		file("insecure_action", "stop"),
		file("egress_allowlist", []any{"api.example.com"}),
		file("require_backup_before_destroy", true),
		file("max_log_disk_bytes", 1_000_000),
	})
	require.NoError(t, err)
	got := o.Apply(policy.Document{})
	require.Equal(t, "stop", got.InsecureAction)
	require.Equal(t, []string{"api.example.com"}, got.EgressAllowlist)
	require.True(t, got.RequireBackupBeforeDestroy)
	require.Equal(t, int64(1_000_000), got.MaxLogDiskBytes)

	for _, tc := range []struct {
		s    policy.Setting
		want string
	}{
		{file("min_security_score", []any{"high"}), "not a valid whole number"},
		{file("egress_allowlist", 7), "not a valid list"},
		{file("require_backup_before_destroy", "yes please"), "not true or false"},
		{file("allow_anonymous_grants", map[string]any{"x": 1}), "not a valid true or false"},
		{file("insecure_action", 3), "not a valid string"},
		{file("min_security_score", func() {}), "unsupported type"},
	} {
		_, err := policy.NewOverlay([]policy.Setting{tc.s})
		require.ErrorContains(t, err, tc.want, tc.s.Key)
		require.ErrorContains(t, err, "/etc/pando.yaml", "the file is named")
	}

	_, err = policy.NewOverlay([]policy.Setting{{Key: "min_security_score", Value: "x"}})
	require.ErrorContains(t, err, "The startup configuration", "a source of no kind still reads")
}

func TestPolicyFieldsAreListed(t *testing.T) {
	fields := policy.Fields()
	require.Contains(t, fields, "min_security_score")
	require.Contains(t, fields, "disable_ai_screening")
	require.IsIncreasing(t, fields)
}

// An overlay with nothing in it, or none at all, changes nothing.
func TestAnEmptyOverlayChangesNothing(t *testing.T) {
	var none *policy.Overlay
	doc := policy.Document{MinSecurityScore: 5}
	require.Nil(t, none.Fixed())
	require.Equal(t, doc, none.Restore(doc, policy.Document{}))
	_, changed := none.Changes(doc)
	require.False(t, changed)

	empty, err := policy.NewOverlay(nil)
	require.NoError(t, err)
	require.Equal(t, doc, empty.Apply(doc))
	require.Equal(t, doc, empty.Restore(doc, policy.Document{MinSecurityScore: 9}))
}

// An empty list and an absent one are the same setting, so sending back an
// empty list for a fixed empty list is not a change.
func TestAnEmptyListIsNotAChange(t *testing.T) {
	o, err := policy.NewOverlay([]policy.Setting{file("egress_allowlist", []any{})})
	require.NoError(t, err)
	_, changed := o.Changes(policy.Document{})
	require.False(t, changed)
	_, changed = o.Changes(policy.Document{EgressAllowlist: []string{"x"}})
	require.True(t, changed)
}

// The wrapped store reads through the overlay and never saves a fixed value.
type memStore struct {
	doc     policy.Document
	loadErr error
}

func (m *memStore) Load(context.Context) (policy.Document, error) { return m.doc, m.loadErr }
func (m *memStore) Save(_ context.Context, doc policy.Document, _ string) error {
	m.doc = doc
	return nil
}

func TestR271_TheWrappedStoreReadsThroughAndSavesAround(t *testing.T) {
	ctx := context.Background()
	o, err := policy.NewOverlay([]policy.Setting{file("min_security_score", 80)})
	require.NoError(t, err)

	inner := &memStore{doc: policy.Document{MinSecurityScore: 60}}
	store := o.Wrap(inner)

	got, err := store.Load(ctx)
	require.NoError(t, err)
	require.Equal(t, 80, got.MinSecurityScore)

	require.NoError(t, store.Save(ctx, policy.Document{MinSecurityScore: 80, EgressAllowlist: []string{"x"}}, "usr_1"))
	require.Equal(t, 60, inner.doc.MinSecurityScore, "the stored value is kept")
	require.Equal(t, []string{"x"}, inner.doc.EgressAllowlist)

	inner.loadErr = errors.New("database down")
	_, err = store.Load(ctx)
	require.Error(t, err)
	require.Error(t, store.Save(ctx, policy.Document{}, "usr_1"), "a save that cannot read what is stored does not guess")

	// With nothing fixed, the store is passed straight through.
	plain, err := policy.NewOverlay(nil)
	require.NoError(t, err)
	inner = &memStore{}
	require.NoError(t, plain.Wrap(inner).Save(ctx, policy.Document{MinSecurityScore: 3}, "usr_1"))
	require.Equal(t, 3, inner.doc.MinSecurityScore)
}
