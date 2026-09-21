// Package screening turns an AI adapter's proposed amendments into changes to
// a draft spec, or refuses them.
//
// It is the enforcement point for R-312 and R-313, and it lives in core for the
// same reason authorization does (R-027): an adapter cannot amend a spec, it
// can only propose an amendment to one, and Apply is the only thing in the tree
// that turns the second into the first.
//
// The amendment types live in internal/adapter/api because an adapter returns
// them. Deciding which of them land is not the adapter's, and the split is
// deliberate — a screener that could apply its own amendments would be a
// screener that could write anything the spec can hold.
//
// Nothing here fails a detection. R-315: a screening that cannot run, or whose
// every amendment is refused, leaves the proposal exactly as the deterministic
// pipeline produced it. Design 09 is the argument; §4.1 is this rule.
package screening
