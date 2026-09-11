//go:build integration

package state_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/audit"
	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/core/bootstrap"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/hash"
	"github.com/bemeek-io/pando/internal/id"
	"github.com/bemeek-io/pando/internal/secret"
)

func connected(t *testing.T) *state.DB {
	t.Helper()
	db, err := state.Connect(context.Background(), state.ConnectOptions{OwnerURL: startPostgres(t)})
	require.NoError(t, err)
	t.Cleanup(db.Close)
	return db
}

func seedUser(t *testing.T, db *state.DB, username string) state.User {
	t.Helper()
	ctx := context.Background()
	users := state.NewUsers(db)
	require.NoError(t, users.EnsureLocalAdapter(ctx))

	digest, err := hash.New(secret.New("correct-password"))
	require.NoError(t, err)

	u, err := users.Create(ctx, state.LocalAdapterID, username, username+"@corp.com", username, digest, false)
	require.NoError(t, err)
	return u
}

func seedApp(t *testing.T, db *state.DB, ownerID string) string {
	t.Helper()
	appID := id.New(id.App)
	_, err := db.Exec(context.Background(),
		`INSERT INTO apps (id, name, slug, owner_user_id, state) VALUES ($1, $2, $3, $4, 'draft')`,
		appID, "notes", appID, ownerID)
	require.NoError(t, err)
	return appID
}

// TestR081_BuiltInRolesAreImmutable asserts R-081 at the database, which is
// where the requirement says the enforcement belongs.
//
// Built-in role contents change only by migration. The trigger refuses every
// runtime path, so a bug in an API handler cannot widen a role.
func TestR081_BuiltInRolesAreImmutable(t *testing.T) {
	ctx := context.Background()
	db := connected(t)

	for _, roleID := range []string{authz.RoleViewer, authz.RoleOperator, authz.RoleOwner} {
		_, err := db.Exec(ctx,
			`UPDATE roles SET verbs = ARRAY['app.delete'] WHERE id = $1`, roleID)
		require.Error(t, err, "UPDATE on built-in role %s must be refused", roleID)

		_, err = db.Exec(ctx, `DELETE FROM roles WHERE id = $1`, roleID)
		require.Error(t, err, "DELETE of built-in role %s must be refused", roleID)
	}

	// Custom roles remain editable — the trigger guards built-ins only (R-082).
	_, err := db.Exec(ctx,
		`INSERT INTO roles (id, name, builtin, verbs) VALUES ('role_custom', 'deployer', false, ARRAY['app.deploy'])`)
	require.NoError(t, err)

	_, err = db.Exec(ctx, `UPDATE roles SET verbs = ARRAY['app.deploy','app.view'] WHERE id = 'role_custom'`)
	require.NoError(t, err, "custom roles must stay editable")

	_, err = db.Exec(ctx, `DELETE FROM roles WHERE id = 'role_custom'`)
	require.NoError(t, err)
}

// TestR081_SeededVerbSetsMatchTheRequirement asserts the migration seeded what
// R-080 and R-184 specify, including that the three *.override verbs are
// Owner-only.
func TestR081_SeededVerbSetsMatchTheRequirement(t *testing.T) {
	ctx := context.Background()
	db := connected(t)
	store := state.NewAuthzStore(db)

	viewer, err := store.Role(ctx, authz.RoleViewer)
	require.NoError(t, err)
	require.ElementsMatch(t, []authz.Verb{authz.AppView, authz.AppLogsRead}, viewer.Verbs)

	operator, err := store.Role(ctx, authz.RoleOperator)
	require.NoError(t, err)
	require.False(t, operator.Has(authz.AppSecretsRead), "R-083")
	require.False(t, operator.Has(authz.AppExec), "R-084")
	for _, v := range []authz.Verb{authz.AppRoutingOverride, authz.AppResourceOverride, authz.AppEgressOverride} {
		require.False(t, operator.Has(v), "%s is Owner-only", v)
	}

	owner, err := store.Role(ctx, authz.RoleOwner)
	require.NoError(t, err)
	require.Len(t, owner.Verbs, len(authz.Verbs), "owner holds every verb in the catalog")
	for _, v := range authz.Verbs {
		require.True(t, owner.Has(v), "owner is missing %s", v)
	}
}

// TestR073_TwoPlanesAreTwoIndependentlyRevocableRows asserts R-073.
func TestR073_TwoPlanesAreTwoIndependentlyRevocableRows(t *testing.T) {
	ctx := context.Background()
	db := connected(t)
	alice := seedUser(t, db, "alice")
	appID := seedApp(t, db, alice.ID)

	for _, g := range []struct{ plane, role string }{
		{"control", authz.RoleOwner},
		{"data", ""},
	} {
		_, err := db.Exec(ctx, `
			INSERT INTO grants (id, app_id, plane, principal_kind, principal_id, role_id, created_by)
			VALUES ($1, $2, $3, 'user', $4, $5, $4)`,
			id.New(id.Grant), appID, g.plane, alice.ID, nullIfEmpty(g.role))
		require.NoError(t, err)
	}

	store := state.NewAuthzStore(db)
	p := authz.Principal{Kind: authz.KindUser, ID: alice.ID, UserID: alice.ID, Status: "active"}

	has, err := store.HasDataGrant(ctx, appID, p)
	require.NoError(t, err)
	require.True(t, has)

	// Revoking the data grant leaves the control grant standing.
	_, err = db.Exec(ctx, `DELETE FROM grants WHERE app_id = $1 AND plane = 'data'`, appID)
	require.NoError(t, err)

	has, err = store.HasDataGrant(ctx, appID, p)
	require.NoError(t, err)
	require.False(t, has)

	grants, err := store.ControlGrantsFor(ctx, appID, p)
	require.NoError(t, err)
	require.Len(t, grants, 1, "the control grant is independently revocable")
}

// TestR075_AnonymousGrantIsARowAndCannotBeDuplicated asserts R-074/R-075 and the
// NULLS NOT DISTINCT index — default NULL handling would let duplicates through.
func TestR075_AnonymousGrantIsARowAndCannotBeDuplicated(t *testing.T) {
	ctx := context.Background()
	db := connected(t)
	alice := seedUser(t, db, "alice")
	appID := seedApp(t, db, alice.ID)

	insert := func() error {
		_, err := db.Exec(ctx, `
			INSERT INTO grants (id, app_id, plane, principal_kind, principal_id, created_by)
			VALUES ($1, $2, 'data', 'anonymous', NULL, $3)`, id.New(id.Grant), appID, alice.ID)
		return err
	}
	require.NoError(t, insert())
	require.Error(t, insert(), "the anonymous grant must not be insertable twice")

	has, err := state.NewAuthzStore(db).HasAnonymousGrant(ctx, appID)
	require.NoError(t, err)
	require.True(t, has)
}

// TestGrantShapesAreEnforcedByTheDatabase asserts the CHECK constraints: a
// data-plane grant has no role (R-070), and a non-anonymous grant has a
// principal (R-074).
func TestGrantShapesAreEnforcedByTheDatabase(t *testing.T) {
	ctx := context.Background()
	db := connected(t)
	alice := seedUser(t, db, "alice")
	appID := seedApp(t, db, alice.ID)

	_, err := db.Exec(ctx, `
		INSERT INTO grants (id, app_id, plane, principal_kind, principal_id, role_id, created_by)
		VALUES ($1, $2, 'data', 'user', $3, $4, $3)`,
		id.New(id.Grant), appID, alice.ID, authz.RoleOwner)
	require.Error(t, err, "a data-plane grant must not carry a role (R-070)")

	_, err = db.Exec(ctx, `
		INSERT INTO grants (id, app_id, plane, principal_kind, principal_id, created_by)
		VALUES ($1, $2, 'data', 'user', NULL, $3)`, id.New(id.Grant), appID, alice.ID)
	require.Error(t, err, "only an anonymous grant may have a null principal (R-074)")
}

// TestR059_OrphanedDelegatedTokenEndToEnd asserts R-059 against the database,
// closing phase 1's Done when: the check is a live lookup, so suspending the
// owner is enough — no cascade runs and no grant is rewritten.
func TestR059_OrphanedDelegatedTokenEndToEnd(t *testing.T) {
	ctx := context.Background()
	db := connected(t)
	alice := seedUser(t, db, "alice")
	appID := seedApp(t, db, alice.ID)

	tokens := state.NewTokens(db)
	issued, err := tokens.Create(ctx, state.TokenDelegated, "ci", alice.ID, alice.ID, nil)
	require.NoError(t, err)

	tok, err := tokens.Authenticate(ctx, issued.Secret)
	require.NoError(t, err)
	require.Equal(t, alice.ID, tok.OwnerUserID)

	store := state.NewAuthzStore(db)
	a := authz.New(store, nil, nil)
	principal := authz.Principal{Kind: authz.KindToken, ID: tok.ID, UserID: tok.OwnerUserID, TokenID: tok.ID}

	require.NoError(t, a.CheckData(ctx, principal, appID), "the token acts as its owner")

	require.NoError(t, state.NewUsers(db).SetStatus(ctx, alice.ID, "suspended"))

	err = a.CheckData(ctx, principal, appID)
	require.Error(t, err)
	require.Equal(t, errs.AuthTokenOrphaned, errs.CodeOf(err),
		"suspending the owner orphans the token with no cascade")
}

// TestR063_TokenSecretIsShownOnceAndStoredHashed asserts R-063.
func TestR063_TokenSecretIsShownOnceAndStoredHashed(t *testing.T) {
	ctx := context.Background()
	db := connected(t)
	alice := seedUser(t, db, "alice")

	issued, err := state.NewTokens(db).Create(ctx, state.TokenDelegated, "ci", alice.ID, alice.ID, nil)
	require.NoError(t, err)

	var stored string
	require.NoError(t, db.QueryRow(ctx, `SELECT hash FROM tokens WHERE id = $1`, issued.Token.ID).Scan(&stored))
	require.NotContains(t, stored, issued.Secret.Reveal(), "the secret must not be recoverable from the row")
	require.Contains(t, stored, "$argon2id$")
}

// TestTokenAuthenticationFailuresAreIndistinguishable asserts that a caller
// cannot enumerate valid token IDs by comparing errors.
func TestTokenAuthenticationFailuresAreIndistinguishable(t *testing.T) {
	ctx := context.Background()
	db := connected(t)
	alice := seedUser(t, db, "alice")
	tokens := state.NewTokens(db)

	issued, err := tokens.Create(ctx, state.TokenDelegated, "ci", alice.ID, alice.ID, nil)
	require.NoError(t, err)

	expired := time.Now().UTC().Add(-time.Hour)
	stale, err := tokens.Create(ctx, state.TokenAccount, "old", "", alice.ID, &expired)
	require.NoError(t, err)

	revoked, err := tokens.Create(ctx, state.TokenAccount, "revoked", "", alice.ID, nil)
	require.NoError(t, err)
	require.NoError(t, tokens.Revoke(ctx, revoked.Token.ID))

	var messages []string
	for name, presented := range map[string]secret.Value{
		"garbage":       secret.New("nonsense"),
		"unknown id":    secret.New(id.New(id.Token) + ".whatever"),
		"wrong secret":  secret.New(issued.Token.ID + ".wrong"),
		"expired token": stale.Secret,
		"revoked token": revoked.Secret,
	} {
		_, err := tokens.Authenticate(ctx, presented)
		require.Error(t, err, name)
		require.Equal(t, errs.AuthTokenInvalid, errs.CodeOf(err), name)
		messages = append(messages, errs.As(err).Message)
	}
	for _, m := range messages {
		require.Equal(t, messages[0], m, "every token failure must read identically")
	}
}

// TestTokenShapesAreEnforced asserts R-058 and R-060: a delegated token needs an
// owner, an account token must not have one.
func TestTokenShapesAreEnforced(t *testing.T) {
	ctx := context.Background()
	db := connected(t)
	alice := seedUser(t, db, "alice")
	tokens := state.NewTokens(db)

	_, err := tokens.Create(ctx, state.TokenDelegated, "no owner", "", alice.ID, nil)
	require.Error(t, err, "a delegated token needs an owner (R-058)")

	_, err = tokens.Create(ctx, state.TokenAccount, "has owner", alice.ID, alice.ID, nil)
	require.Error(t, err, "an account token is its own principal (R-060)")

	account, err := tokens.Create(ctx, state.TokenAccount, "ci", "", alice.ID, nil)
	require.NoError(t, err)

	tok, err := tokens.Authenticate(ctx, account.Secret)
	require.NoError(t, err)
	require.Empty(t, tok.OwnerUserID)
}

// TestR046_FirstRunCreatesOneAdminAndIsIdempotent asserts R-046.
func TestR046_FirstRunCreatesOneAdminAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := connected(t)
	users := state.NewUsers(db)
	auditor := audit.New(db.Pool)

	first, err := bootstrap.Run(ctx, users, db, auditor)
	require.NoError(t, err)
	require.True(t, first.Created)
	require.NotEmpty(t, first.Password.Reveal())
	require.True(t, first.User.MustChangePassword, "the initial credential must be changed on first login")

	// The password is displayed once and never stored in the clear.
	var storedHash string
	require.NoError(t, db.QueryRow(ctx,
		`SELECT password_hash FROM users WHERE id = $1`, first.User.ID).Scan(&storedHash))
	require.NotContains(t, storedHash, first.Password.Reveal())

	again, err := bootstrap.Run(ctx, users, db, auditor)
	require.NoError(t, err)
	require.False(t, again.Created, "first run must not repeat")

	var count int
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&count))
	require.Equal(t, 1, count)
}

// TestR079_GroupMembershipIsResolvedLive asserts R-079 against the database: a
// grant made to a group is honored through membership, and removing membership
// revokes access without touching the grant.
func TestR079_GroupMembershipIsResolvedLive(t *testing.T) {
	ctx := context.Background()
	db := connected(t)
	alice := seedUser(t, db, "alice")
	bob := seedUser(t, db, "bob")
	appID := seedApp(t, db, alice.ID)

	groupID := id.New(id.Group)
	_, err := db.Exec(ctx, `INSERT INTO groups (id, name) VALUES ($1, 'engineering')`, groupID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)`, groupID, bob.ID)
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO grants (id, app_id, plane, principal_kind, principal_id, created_by)
		VALUES ($1, $2, 'data', 'group', $3, $4)`, id.New(id.Grant), appID, groupID, alice.ID)
	require.NoError(t, err)

	store := state.NewAuthzStore(db)
	bobPrincipal := authz.Principal{Kind: authz.KindUser, ID: bob.ID, UserID: bob.ID, Status: "active"}

	has, err := store.HasDataGrant(ctx, appID, bobPrincipal)
	require.NoError(t, err)
	require.True(t, has)

	// Remove membership only. The grant is untouched.
	_, err = db.Exec(ctx, `DELETE FROM group_members WHERE group_id = $1 AND user_id = $2`, groupID, bob.ID)
	require.NoError(t, err)

	has, err = store.HasDataGrant(ctx, appID, bobPrincipal)
	require.NoError(t, err)
	require.False(t, has, "membership is resolved live, so removing it revokes access")
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
