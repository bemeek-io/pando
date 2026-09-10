// Package secret provides Value, a wrapper that refuses to render.
//
// String, MarshalJSON, and MarshalLogObject all return [redacted]. This is how R-194 is
// enforced structurally rather than by review: a secret cannot be accidentally logged because
// the type will not print. All three must redact — a type that redacts in String but not in
// MarshalJSON will leak through an error envelope.
package secret
