package local_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/adapter/identity/local"
	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/hash"
	"github.com/bemeek-io/pando/internal/secret"
)

type users map[string]local.Record

func (u users) ByUsername(_ context.Context, name string) (local.Record, bool, error) {
	r, ok := u[name]
	return r, ok, nil
}

func fixture(t *testing.T) users {
	t.Helper()
	h, err := hash.New(secret.New("correct-password"))
	require.NoError(t, err)
	return users{
		"alice":     {ExternalID: "alice", Email: "alice@corp.com", PasswordHash: h, Status: "active"},
		"suspended": {ExternalID: "suspended", PasswordHash: h, Status: "suspended"},
	}
}

func TestR044_AuthenticateReturnsASubjectWithNoPermissions(t *testing.T) {
	a := local.New(fixture(t))

	subject, err := a.Authenticate(context.Background(), api.Credential{
		Username: "alice", Password: secret.New("correct-password"),
	})
	require.NoError(t, err)
	require.Equal(t, "alice", subject.ExternalID)
	require.Equal(t, "alice@corp.com", subject.Email)

	// R-044/R-078: the adapter authenticates only. Nothing it returns says what
	// the subject may do.
	require.Empty(t, subject.Groups, "the local adapter has no group source")
}

// Distinguishing "no such user" from "wrong password" turns the login form into
// an account enumeration oracle.
func TestFailuresAreIndistinguishable(t *testing.T) {
	a := local.New(fixture(t))
	ctx := context.Background()

	cases := map[string]api.Credential{
		"no such user":   {Username: "nobody", Password: secret.New("correct-password")},
		"wrong password": {Username: "alice", Password: secret.New("wrong-password")},
		"empty password": {Username: "alice", Password: secret.Value{}},
		"suspended user": {Username: "suspended", Password: secret.New("correct-password")},
	}

	var messages []string
	for name, cred := range cases {
		_, err := a.Authenticate(ctx, cred)
		require.Error(t, err, name)
		require.Equal(t, errs.AuthInvalid, errs.CodeOf(err), name)
		messages = append(messages, errs.As(err).Message)
	}

	for _, m := range messages {
		require.Equal(t, messages[0], m, "every failure must read identically")
	}
}

// R-194: a failed login must not put the attempted password in the error.
func TestR194_CredentialsDoNotAppearInErrors(t *testing.T) {
	a := local.New(fixture(t))
	_, err := a.Authenticate(context.Background(), api.Credential{
		Username: "alice", Password: secret.New("hunter2-THE-ACTUAL-SECRET"),
	})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "hunter2-THE-ACTUAL-SECRET")
}

// R-047: each adapter declares its own session behavior.
func TestR047_AdapterDeclaresItsOwnSessionPolicy(t *testing.T) {
	a := local.New(fixture(t))
	p := a.SessionPolicy()
	require.Equal(t, api.RevocationPush, p.RevocationMode,
		"the local adapter owns its users, so it can revoke immediately")
	require.Positive(t, p.MaxLifetime)

	require.NoError(t, a.Configure(context.Background(), []byte(`{"session_max_lifetime":"1h"}`)))
	require.Equal(t, "1h0m0s", a.SessionPolicy().MaxLifetime.String())

	require.Error(t, a.Configure(context.Background(), []byte(`{"session_max_lifetime":"eventually"}`)))
}

func TestBeginIsNilForAnInlineAdapter(t *testing.T) {
	r, err := local.New(fixture(t)).Begin(context.Background(), "/")
	require.NoError(t, err)
	require.Nil(t, r, "an inline adapter has nowhere to redirect")
}
