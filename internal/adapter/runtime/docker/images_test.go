package docker

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnImagesOwnershipRepositoryIsStableAndTagSafe(t *testing.T) {
	for _, ref := range []string{"postgres:16", "registry.example.com:5000/team/app:1.2", "redis@sha256:" + strings.Repeat("a", 64)} {
		repo := ownedRepo(ref)
		require.Equal(t, repo, ownedRepo(ref), "the same reference always owns the same repository")
		require.True(t, strings.HasPrefix(repo, pulledRepo+"/"))
		require.NotContains(t, strings.TrimPrefix(repo, pulledRepo+"/"), ":")
	}
	require.NotEqual(t, ownedRepo("postgres:16"), ownedRepo("postgres:17"))
}

func TestABundleIDBecomesATagOnlyWhenItIsOne(t *testing.T) {
	tag, ok := bundleTag("app_01HQ8ABC")
	require.True(t, ok)
	require.Equal(t, "app_01hq8abc", tag)

	for _, bad := range []string{"", pulledMarker, ".x", "-x", "a/b", "a:b", strings.Repeat("a", 129)} {
		_, ok := bundleTag(bad)
		require.False(t, ok, bad)
	}
}

// TestR224_APulledImageIsRemovedOnlyWhenPandoFetchedItAndNobodyClaimsIt
// asserts R-224 without letting it reach past what Pando owns: an image
// that was on the host before Pando asked for it, or that another app — on
// this install or another — still runs, is kept.
func TestR224_APulledImageIsRemovedOnlyWhenPandoFetchedItAndNobodyClaimsIt(t *testing.T) {
	repo := ownedRepo("postgres:16")

	refs, fetched, claimed := unclaimedRefs([]string{"postgres:16", repo + ":" + pulledMarker}, repo)
	require.Equal(t, []string{"postgres:16"}, refs)
	require.True(t, fetched)
	require.False(t, claimed, "nobody claims it: removed")

	_, _, claimed = unclaimedRefs([]string{"postgres:16", repo + ":pulled", repo + ":app_01other"}, repo)
	require.True(t, claimed, "another app still runs it: kept")

	_, fetched, _ = unclaimedRefs([]string{"postgres:16"}, repo)
	require.False(t, fetched, "on the host before Pando fetched it: kept")

	refs, _, _ = unclaimedRefs([]string{"postgres:16", "postgres:latest", repo + ":pulled"}, repo)
	require.Equal(t, []string{"postgres:16"}, refs, "only the reference Pando fetched, not another name for the same image")
}

func TestAnImagePandoBuiltIsLeftToItsLabel(t *testing.T) {
	require.True(t, builtByPando("pando/app-01hq8:latest"))
	require.False(t, builtByPando("postgres:16"))
}
