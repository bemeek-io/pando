// Package local implements the local identity adapter: username and password, argon2id.
//
// Identity adapters authenticate only (R-044). Subject carries no roles, no verbs, and no
// permissions — group names cross the boundary, but what a group can do is Pando's (R-078).
package local
