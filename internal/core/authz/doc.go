// Package authz evaluates the two authorization planes.
//
// Control plane is role-scoped and governs managing an app. Data plane is binary and
// governs using one. They are never conflated: CheckData contains exactly one cross-plane
// implication, ownership (R-072), and this was reversed once during design.
//
// Evaluation order is fixed and each step can only deny. See design 06 §2 and the package
// CLAUDE.md before changing anything here.
package authz
