//go:build integration

package httpapi_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/config"
	corepolicy "github.com/bemeek-io/pando/internal/core/policy"
)

// TestR271_PolicySetAtStartupIsFixedAndSaysWhere asserts R-271's startup
// configuration for host policy: a field set there applies, cannot be changed
// through the API, is never written into the stored document, and GET /config
// says where it was set.
func TestR271_PolicySetAtStartupIsFixedAndSaysWhere(t *testing.T) {
	overlay, err := corepolicy.NewOverlay([]corepolicy.Setting{
		{Key: "min_security_score", Value: "80", Source: corepolicy.Source{Kind: "env", Name: "PANDO_POLICY_MIN_SECURITY_SCORE"}},
		{Key: "disabled_verbs", Value: []any{"app.exec"}, Source: corepolicy.Source{Kind: "file", Name: "/etc/pando/pando.yaml", Key: "policy.disabled_verbs"}},
	})
	require.NoError(t, err)
	startup := &config.Config{
		File: "/etc/pando/pando.yaml",
		Settings: []config.Setting{
			{Key: "server.base_domain", Value: "example.test", Source: config.Source{Kind: "env", Name: "PANDO_SERVER_BASE_DOMAIN"}},
			{Key: "server.work_dir", Value: "/var/lib/pando/work", Source: config.Source{Kind: "default"}},
		},
	}
	i := newInstallWith(t, overlay, startup)
	admin := i.admin()

	// Read: the startup values apply.
	var doc struct {
		MinSecurityScore int      `json:"min_security_score"`
		DisabledVerbs    []string `json:"disabled_verbs"`
		EgressAllowlist  []string `json:"egress_allowlist"`
	}
	i.do(admin, http.MethodGet, "/policy", nil).JSON(t, &doc)
	require.Equal(t, 80, doc.MinSecurityScore)
	require.Equal(t, []string{"app.exec"}, doc.DisabledVerbs)

	// Changing a fixed field is refused, naming where it is set.
	refused := i.do(admin, http.MethodPut, "/policy", map[string]any{
		"min_security_score": 50, "disabled_verbs": []string{"app.exec"},
	})
	require.Equal(t, http.StatusBadRequest, refused.Code, refused.String())
	require.Contains(t, refused.String(), "PANDO_POLICY_MIN_SECURITY_SCORE")

	// Saving everything else, with the fixed fields as GET gave them, works —
	// and does not write the startup values into the stored document.
	saved := i.do(admin, http.MethodPut, "/policy", map[string]any{
		"min_security_score": 80, "disabled_verbs": []string{"app.exec"},
		"egress_allowlist": []string{"api.example.com"},
	})
	require.Equal(t, http.StatusOK, saved.Code, saved.String())
	saved.JSON(t, &doc)
	require.Equal(t, 80, doc.MinSecurityScore, "the answer is what applies")
	require.Equal(t, []string{"api.example.com"}, doc.EgressAllowlist)

	stored, err := i.PolicyStore.Load(context.Background())
	require.NoError(t, err)
	require.Zero(t, stored.MinSecurityScore, "the startup value is not stored")
	require.Empty(t, stored.DisabledVerbs)
	require.Equal(t, []string{"api.example.com"}, stored.EgressAllowlist)

	// And the policy is enforced as fixed: exec is off for the owner too.
	appID := i.appWithSpec(admin, "notes")
	exec := i.do(admin, http.MethodGet, "/apps/"+appID+"/exec", nil)
	require.Equal(t, http.StatusForbidden, exec.Code, exec.String())

	// GET /config says where everything came from.
	var cfg struct {
		File     string           `json:"file"`
		Settings []config.Setting `json:"settings"`
		Policy   []struct {
			Key    string            `json:"key"`
			Source corepolicy.Source `json:"source"`
		} `json:"policy"`
	}
	got := i.do(admin, http.MethodGet, "/config", nil)
	require.Equal(t, http.StatusOK, got.Code, got.String())
	got.JSON(t, &cfg)
	require.Equal(t, "/etc/pando/pando.yaml", cfg.File)
	require.Len(t, cfg.Settings, 2)
	require.Equal(t, "PANDO_SERVER_BASE_DOMAIN", cfg.Settings[0].Source.Name)
	require.Len(t, cfg.Policy, 2)
	require.Equal(t, "disabled_verbs", cfg.Policy[0].Key)
	require.Equal(t, "policy.disabled_verbs", cfg.Policy[0].Source.Key)

	// Reading it is install.view, like reading policy.
	require.Equal(t, http.StatusForbidden, i.do(i.user("ordinary"), http.MethodGet, "/config", nil).Code)
}
