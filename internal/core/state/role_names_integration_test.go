//go:build integration

package state_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/core/state"
)

// TestR082_ACustomRoleCannotTakeAnExistingRolesName asserts role names are
// unique however they are written — built-ins included, which are stored
// lowercase and shown capitalized.
func TestR082_ACustomRoleCannotTakeAnExistingRolesName(t *testing.T) {
	ctx := context.Background()
	db := connected(t)
	roles := state.NewRoles(db)
	app := []authz.Verb{authz.AppView}
	install := []authz.Verb{authz.InstallAuditRead}

	for _, name := range []string{"Administrator", "viewer", " OWNER ", "Creator", "operator"} {
		_, err := roles.CreateCustom(ctx, name, "app", app)
		require.Error(t, err, name)
		require.ErrorContains(t, err, "built-in role", name)
	}

	_, err := roles.CreateCustom(ctx, "Auditor", "install", install)
	require.NoError(t, err)
	_, err = roles.CreateCustom(ctx, "auditor ", "install", install)
	require.ErrorContains(t, err, `already a role called "Auditor"`)

	// And the database says so on its own, whatever the code does.
	_, err = db.Exec(ctx,
		`INSERT INTO roles (id, name, builtin, scope, verbs) VALUES ('role_sneaky', 'ADMINISTRATOR', false, 'install', ARRAY['install.view'])`)
	require.Error(t, err)

	// A name with spaces around it is stored trimmed.
	created, err := roles.CreateCustom(ctx, "  Deployer  ", "app", app)
	require.NoError(t, err)
	require.Equal(t, "Deployer", created.Name)
}
