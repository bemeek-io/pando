package state

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// A NUL anywhere in a detection made it unsavable: jsonb refuses `\u0000`
// (issue #55). Everything else in the document survives, including a string
// that spells the escape out with a real backslash.
func TestANULIsDroppedAndNothingElseIs(t *testing.T) {
	in := map[string]any{
		"evidence": []string{"CMD [\"server\"]\x00", "a\x00b"},
		"literal":  `a backslash then \u0000`,
		"quote":    "she said \"hi\"\\",
	}
	encoded, err := json.Marshal(in)
	require.NoError(t, err)

	var out map[string]any
	require.NoError(t, json.Unmarshal(withoutNUL(encoded), &out))
	require.Equal(t, []any{`CMD ["server"]`, "ab"}, out["evidence"])
	require.Equal(t, `a backslash then \u0000`, out["literal"])
	require.Equal(t, "she said \"hi\"\\", out["quote"])

	require.Equal(t, `{}`, string(withoutNUL([]byte(`{}`))))
	require.Equal(t, `"\`, string(withoutNUL([]byte(`"\`))), "a truncated escape is copied, not read past")
}
