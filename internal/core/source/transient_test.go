package source

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/errs"
)

// A clone that failed because its connection died is tried again; one that
// failed because of the repository is not (issue #55).
func TestAFetchIsRetriedOnlyWhenTheConnectionFailed(t *testing.T) {
	lost := errs.Wrap(errs.ValidInvalid, "Pando could not fetch this app's source.",
		errors.New("http2: client connection lost"))
	require.True(t, transient(lost), "the cause is read through the envelope")

	require.False(t, transient(errs.Wrap(errs.ValidInvalid, "Pando could not fetch this app's source.",
		errors.New("repository not found"))))
	require.False(t, transient(errors.New("reference not found")))
}

// An app with no checkout has an empty source, not the server's filesystem.
// A view rooted at "" resolved "/Dockerfile" and friends against the server's
// own root, which a screener would then have sent to a provider.
func TestAnImageAppsSourceIsEmptyNotTheServersFilesystem(t *testing.T) {
	view := (&Checkout{}).View("")
	_, err := view.Stat("etc/passwd")
	require.Error(t, err)
	_, err = view.Open("etc/hostname")
	require.Error(t, err)
	found, err := view.Glob("*")
	require.NoError(t, err)
	require.Empty(t, found)
}
