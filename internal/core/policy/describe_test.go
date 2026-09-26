package policy

import "testing"

// TestEveryPolicyFieldIsDescribed keeps the descriptions the policy drafting
// function reads (R-344) in step with the document: a field added without one
// is a setting AI cannot find by what it does.
func TestEveryPolicyFieldIsDescribed(t *testing.T) {
	for _, key := range Fields() {
		if Describe(key) == "" {
			t.Errorf("policy field %q has no description in describe.go", key)
		}
	}
	for key := range descriptions {
		if _, ok := fields[key]; !ok {
			t.Errorf("describe.go describes %q, which is not a policy field", key)
		}
	}
}

// TestSameValueTreatsAbsentAsZero asserts a field omitted from a document and
// the same field at its zero value are one setting.
func TestSameValueTreatsAbsentAsZero(t *testing.T) {
	cases := []struct {
		key  string
		a, b string
		same bool
	}{
		{"require_backup_before_destroy", "", "false", true},
		{"require_backup_before_destroy", "null", "true", false},
		{"insecure_grace_hours", "", "0", true},
		{"disabled_verbs", "", "[]", true},
		{"disabled_verbs", `["app.exec"]`, `["app.exec"]`, true},
		{"disabled_verbs", "", `["app.exec"]`, false},
		{"not_a_field", `1`, `1`, true},
		{"not_a_field", `1`, `2`, false},
	}
	for _, c := range cases {
		if got := SameValue(c.key, []byte(c.a), []byte(c.b)); got != c.same {
			t.Errorf("SameValue(%s, %q, %q) = %v, want %v", c.key, c.a, c.b, got, c.same)
		}
	}
}
