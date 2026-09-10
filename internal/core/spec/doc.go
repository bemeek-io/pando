// Package spec defines the AppSpec: the sole record of how an app runs.
//
// Nothing is read from the repo at deploy time (R-020). Specs are immutable and versioned;
// editing produces a new revision and rollback is repointing (R-152). A spec carries no
// policy, no grants, and no secret values, which is what makes export safe to hand to
// someone. See design 01.
package spec
