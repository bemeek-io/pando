// Package reconciler runs the loop that converges observed state toward pinned specs.
//
// The dividing line: the reconciler may create and start things; it may not destroy anything
// a human may have wanted (R-148, R-028). A failed app stays failed — R-151 is true because
// there is no code path here that touches the failed state, not because a flag is checked.
// See design 05.
package reconciler
