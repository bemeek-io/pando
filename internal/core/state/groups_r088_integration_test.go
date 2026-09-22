//go:build integration

package state_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/id"
)

// TestR088_DeletingTheGroupThatIsTheOnlyAdministratorIsRefused asserts R-088
// for a group. The API grants install roles to people only, so this is set up
// in the database: a group holding the administrator role is the installation's
// only way to manage accounts, and deleting it would leave nobody who can.
func TestR088_DeletingTheGroupThatIsTheOnlyAdministratorIsRefused(t *testing.T) {
	ctx := context.Background()
	db := connected(t)
	groups := state.NewGroups(db)

	admins, err := groups.Create(ctx, "admins")
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO grants (id, app_id, plane, role_scope, principal_kind, principal_id, role_id, created_by)
		VALUES ($1, NULL, 'control', 'install', 'group', $2, $3, 'system')`,
		id.New(id.Grant), admins.ID, authz.RoleAdministrator)
	require.NoError(t, err)

	err = groups.Delete(ctx, admins.ID)
	require.Error(t, err)
	require.Equal(t, errs.ValidInvalid, errs.CodeOf(err))
	require.ErrorContains(t, err, "only administrators")

	// Nothing was taken: the group and its grant are still there.
	var n int
	require.NoError(t, db.QueryRow(ctx,
		`SELECT count(*) FROM grants WHERE principal_kind = 'group' AND principal_id = $1`, admins.ID).Scan(&n))
	require.Equal(t, 1, n)

	// With another way to manage accounts, it can go.
	other, err := groups.Create(ctx, "backup admins")
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO grants (id, app_id, plane, role_scope, principal_kind, principal_id, role_id, created_by)
		VALUES ($1, NULL, 'control', 'install', 'group', $2, $3, 'system')`,
		id.New(id.Grant), other.ID, authz.RoleAdministrator)
	require.NoError(t, err)
	require.NoError(t, groups.Delete(ctx, admins.ID))
}

// TestR078_ManyGroupsCanBeMadeAndNamesAreUnique asserts that an installation
// can hold more than one Pando-made group — it could not, because the uniqueness
// meant for synced groups counted every Pando-made one as the same — and that a
// name is still taken once.
func TestR078_ManyGroupsCanBeMadeAndNamesAreUnique(t *testing.T) {
	ctx := context.Background()
	groups := state.NewGroups(connected(t))

	for _, name := range []string{"engineering", "design", "support"} {
		_, err := groups.Create(ctx, name)
		require.NoError(t, err, name)
	}
	_, err := groups.Create(ctx, "design")
	require.ErrorContains(t, err, `already a group called "design"`)

	listed, err := groups.List(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 3)
}
