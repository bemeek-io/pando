// Package id generates prefixed, sortable, opaque identifiers: app_01HQ8..., spec_..., usr_...
//
// ULID body. The prefix is load-bearing — it makes log lines self-describing and copy-paste
// mistakes visible, which is why IDs are text rather than uuid in the schema.
package id
