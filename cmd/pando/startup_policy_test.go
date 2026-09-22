package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/config"
)

// R-271: a startup policy that would not do what it says stops startup — an
// unknown field, a value that will not read, and a disabled verb Pando does
// not have, which would deny nothing while looking like a rule that works.
func TestR271_StartupPolicyIsCheckedBeforePandoStarts(t *testing.T) {
	setting := func(key string, value any) config.PolicySetting {
		return config.PolicySetting{Key: key, Value: value, Source: config.Source{Kind: "env", Name: "PANDO_POLICY_X"}}
	}

	o, err := startupPolicy(&config.Config{Policy: []config.PolicySetting{
		setting("disabled_verbs", "app.exec"),
		setting("agent_disabled_verbs", "app.delete"),
		setting("min_security_score", "70"),
	}})
	require.NoError(t, err)
	require.Len(t, o.Fixed(), 3)

	_, err = startupPolicy(&config.Config{Policy: []config.PolicySetting{setting("disabled_verbs", "app.exce")}})
	require.ErrorContains(t, err, `"app.exce"`)

	_, err = startupPolicy(&config.Config{Policy: []config.PolicySetting{setting("agent_disabled_verbs", "nope")}})
	require.ErrorContains(t, err, "not a permission")

	_, err = startupPolicy(&config.Config{Policy: []config.PolicySetting{setting("no_such_field", "1")}})
	require.ErrorContains(t, err, "not a host policy setting")

	o, err = startupPolicy(&config.Config{})
	require.NoError(t, err)
	require.Empty(t, o.Fixed())
}
