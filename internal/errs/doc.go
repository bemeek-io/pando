// Package errs defines the error envelope crossing every API boundary.
//
// A stable machine code, a human message, and where relevant a remediation hint. Message text
// is held to the R-105 standard wherever a user might act on it: self-contained, pasteable
// into an assistant, no undefined terms. See design 00 §3.2 for the code taxonomy and the
// named codes the requirements promise.
package errs
