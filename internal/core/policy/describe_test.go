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
