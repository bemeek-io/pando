package detect_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/trypando/pando/internal/adapter/builder/buildkit"
	"github.com/trypando/pando/internal/core/source"
	"github.com/trypando/pando/internal/core/spec"
	"github.com/trypando/pando/internal/detect"
)

// TestR094_AJekyllSourceTreeIsNotBidAsAFinishedSite asserts R-094.
//
// A GitHub Pages site keeps an index.html at the root beside Markdown pages
// that only exist once Jekyll has built them. Bid as a finished site, it was
// served as committed and every Markdown page failed (issue #67).
func TestR094_AJekyllSourceTreeIsNotBidAsAFinishedSite(t *testing.T) {
	for name, src := range map[string]memSource{
		"a layout": {
			"index.html": "<p>hi</p>", "_config.yml": "title: x\n",
			"_layouts/default.html": "{{ content }}",
		},
		"a remote theme": {
			"index.html": "<p>hi</p>", "_config.yml": "remote_theme: pages-themes/midnight\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			bid, err := detect.StaticDetector{}.Bid(context.Background(), src)
			require.NoError(t, err)
			require.InDelta(t, 0.2, bid.Confidence, 0.001)
		})
	}

	// A _config.yml with nothing else Jekyll about it says nothing.
	bid, err := detect.StaticDetector{}.Bid(context.Background(),
		memSource{"index.html": "<p>hi</p>", "_config.yml": "title: x\n"})
	require.NoError(t, err)
	require.InDelta(t, 0.7, bid.Confidence, 0.001)
}

// TestR094_AJekyllSiteIsBuiltRatherThanServedAsCommitted asserts R-094.
//
// The repository in issue #67, through the auction with the real builder as
// the planner: the static bid steps aside, the buildpack bid carries a plan
// that runs Jekyll, and nobody is asked anything.
func TestR094_AJekyllSiteIsBuiltRatherThanServedAsCommitted(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"_config.yml":                           "remote_theme: pages-themes/midnight@v0.2.0\n",
		"_layouts/solution.html":                "---\nlayout: default\n---\n{{ content }}\n",
		"index.html":                            "---\nlayout: default\n---\n<a href=\"solutions\">Solutions</a>\n",
		"solutions/index.html":                  "<p>list</p>\n",
		"solutions/copy-role-and-acls/index.md": "# Copy role and ACLs\n",
	} {
		full := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
	}
	checkout := &source.Checkout{Dir: root}

	result, err := detect.NewAuction(
		detect.StaticDetector{},
		detect.BuildpackDetector{Planner: buildkit.New()},
	).Run(context.Background(), checkout.View(""))
	require.NoError(t, err)

	require.Equal(t, spec.BuildBuildpack, result.Winner.Strategy)
	require.Empty(t, detect.Asked(result.Questions))
	build := result.Winner.Draft.Build
	require.Contains(t, build.GeneratedFiles[build.Dockerfile], "jekyll build")
	require.Equal(t, 80, result.Winner.Draft.Workloads[0].Ports[0].Number)
}
