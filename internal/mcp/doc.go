// Package mcp exposes Pando's service layer as MCP tools.
//
// An agent holds a token and is a principal like any other (R-262). No tool bypasses
// authorization, and every action lands in the audit log under the token's owner. Exec, secret
// value reads, grant mutation, policy mutation, and user deletion are deliberately not exposed.
package mcp
