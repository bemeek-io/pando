// Package local implements the secrets adapter, encrypted at rest with the key on disk (R-190).
//
// Values cross this boundary wrapped in secret.Value, which refuses to render (R-194). No
// plaintext column exists anywhere in the schema.
package local
