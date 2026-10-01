package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestR187_TheGatewayRefusesToStartOnRulesItCannotEnforce asserts R-187: a
// gateway that started without its rules, or with part of them unreadable,
// would let out what they were written to stop. It does not start.
func TestR187_TheGatewayRefusesToStartOnRulesItCannotEnforce(t *testing.T) {
	for name, c := range map[string]struct{ rules, listen, says string }{
		"no rules":           {"", "", "without its rules"},
		"not JSON":           {"allowlist", "", "could not read its rules"},
		"an unknown field":   {`{"layers":[],"allow_everything":true}`, "", "allow_everything"},
		"an entry it cannot": {`{"layers":[{"mode":"denylist","list":["not valid"]}]}`, "", "rule it cannot use"},
		"an unknown mode":    {`{"layers":[{"mode":"sometimes","list":["a.example"]}]}`, "", "sometimes"},
		"a bad address":      {`{"block_private":true}`, "nowhere", "not an address the egress gateway can listen on"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(envEgressRules, c.rules)
			t.Setenv(envEgressListen, c.listen)
			cmd := egressGatewayCmd()
			cmd.SetArgs(nil)
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			err := cmd.Execute()
			require.Error(t, err)
			require.Contains(t, err.Error(), c.says)
		})
	}
}

func TestTheEgressGatewayCommandIsHidden(t *testing.T) {
	root := rootCmd()
	cmd, _, err := root.Find([]string{"egress-gateway"})
	require.NoError(t, err)
	require.Equal(t, "egress-gateway", cmd.Name())
	require.True(t, cmd.Hidden, "started by the runtime, never by a person")
}
