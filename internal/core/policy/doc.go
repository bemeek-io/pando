// Package policy evaluates host policy.
//
// Policy is a floor, not an override (R-272), and is evaluated before grants — a policy that
// disables exec install-wide denies the owner too. It is a single versioned document rather
// than scattered columns, so applying policy to a running install is one transaction and one
// audit event (R-274).
package policy
