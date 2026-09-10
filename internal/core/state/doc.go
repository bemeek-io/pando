// Package state holds sqlc-generated queries and repository types.
//
// This store is the sole record of how every app runs (R-020), which sets the bar: every
// mutation is audited, every object is exportable, nothing important lives only in memory.
// Several requirements are enforced by database constraints rather than by code — see the
// package CLAUDE.md before changing the schema.
package state
